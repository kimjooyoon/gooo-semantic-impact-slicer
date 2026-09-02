package impact

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kimjooyoon/gooo-semantic-impact-slicer/internal/generated"
)

type unknownCause struct {
	Stage         string
	Step          string
	Reason        string
	Class         string
	NextOperation string
	BlockedBy     []string
}

type lockDecision struct {
	Reusable bool
	Reason   string
}

// Plan applies the semantics projected from the .gooo sources.
func Plan(req Request) (Report, error) {
	if err := validateRequest(req); err != nil {
		return Report{}, err
	}
	if err := requireRules(); err != nil {
		return Report{}, err
	}

	previous := indexNodes(req.Previous.Nodes)
	candidate := indexNodes(req.Candidate.Nodes)
	changed := changedNodes(previous, candidate)
	changedSet := makeSet(changed)

	causes := map[string][]unknownCause{}
	status := map[string]string{}
	for _, node := range req.Candidate.Nodes {
		status[node.ID] = normalizeStatus(node.Status)
		if node.UnknownDependency {
			addCause(causes, node.ID, unknownCause{
				Stage: "graph", Step: "dependency-resolution", Reason: "candidate declares an unknown dependency", Class: "unknown-dependency",
				NextOperation: "resolve-dependency-and-rerun-slice", BlockedBy: []string{"candidate:" + node.ID},
			})
		}
		if len(node.Provenance) == 0 {
			addCause(causes, node.ID, unknownCause{
				Stage: "evidence", Step: "provenance-check", Reason: "candidate node has no provenance", Class: "missing-provenance",
				NextOperation: "attach-provenance-and-rerun-selected-check", BlockedBy: []string{"candidate:" + node.ID},
			})
		}
		if len(node.Authority) == 0 {
			addCause(causes, node.ID, unknownCause{
				Stage: "authority", Step: "authority-resolution", Reason: "candidate node has no authority", Class: "unknown-authority",
				NextOperation: "resolve-authority-and-rerun-selected-check", BlockedBy: []string{"candidate:" + node.ID},
			})
		}
		if prior, ok := previous[node.ID]; ok && prior.Authority != node.Authority {
			addCause(causes, node.ID, unknownCause{
				Stage: "authority", Step: "authority-reconciliation", Reason: "candidate authority differs from pinned authority", Class: "changed-authority",
				NextOperation: "reconcile-authority-and-rerun-selected-check", BlockedBy: []string{"previous:" + node.ID, "candidate:" + node.ID},
			})
		}
		for _, dep := range node.Deps {
			if dep == "" || dep == "?" || candidate[dep] == nil {
				addCause(causes, node.ID, unknownCause{
					Stage: "graph", Step: "dependency-resolution", Reason: "candidate dependency is not resolved", Class: "unknown-dependency",
					NextOperation: "resolve-dependency-and-rerun-slice", BlockedBy: []string{"candidate:" + node.ID},
				})
			}
		}
	}

	lockGroups := groupLocks(req.Previous.Locks)
	for key, locks := range lockGroups {
		if len(distinctEvidence(locks)) > 1 {
			nodeID := strings.SplitN(key, "\x00", 2)[0]
			addCause(causes, nodeID, unknownCause{
				Stage: "evidence", Step: "lock-reconciliation", Reason: "multiple evidence locks disagree for one node/check", Class: "conflicting-lock",
				NextOperation: "reconcile-conflicting-locks-and-rerun-selected-check", BlockedBy: lockIDs(locks),
			})
		}
	}
	for _, lock := range req.Previous.Locks {
		if node := candidate[lock.NodeID]; node != nil {
			if lock.Toolchain != req.Candidate.Toolchain || lock.Evaluator != req.Candidate.Evaluator {
				addCause(causes, lock.NodeID, unknownCause{
					Stage: "evidence", Step: "scope-check", Reason: "evidence lock uses a stale toolchain or evaluator", Class: "stale-scope",
					NextOperation: "rerun-selected-check-with-candidate-scope", BlockedBy: []string{lock.ID},
				})
			}
			if lock.Authority != node.Authority {
				addCause(causes, lock.NodeID, unknownCause{
					Stage: "authority", Step: "authority-reconciliation", Reason: "evidence lock authority differs from candidate authority", Class: "changed-authority",
					NextOperation: "reconcile-authority-and-rerun-selected-check", BlockedBy: []string{lock.ID},
				})
			}
			if lock.Provenance == "" || node.Provenance == "" {
				addCause(causes, lock.NodeID, unknownCause{
					Stage: "evidence", Step: "provenance-check", Reason: "evidence lock or candidate node has no provenance", Class: "missing-provenance",
					NextOperation: "attach-provenance-and-rerun-selected-check", BlockedBy: []string{lock.ID},
				})
			}
			if lock.EvidenceDigest == "" {
				addCause(causes, lock.NodeID, unknownCause{
					Stage: "evidence", Step: "evidence-digest-check", Reason: "evidence lock has no evidence digest", Class: "missing-evidence-digest",
					NextOperation: "produce-evidence-digest-and-rerun-selected-check", BlockedBy: []string{lock.ID},
				})
			}
		}
		status[lock.NodeID] = highestStatus(status[lock.NodeID], lock.Status)
	}
	for nodeID := range causes {
		status[nodeID] = highestStatus(status[nodeID], StatusUnknown)
	}

	seed := makeSet(changed)
	for nodeID := range causes {
		seed[nodeID] = true
	}
	for nodeID, value := range status {
		if value != StatusClosed {
			seed[nodeID] = true
		}
	}
	impacted := projectDependents(req.Candidate.Nodes, seed)

	decisions := make(map[string]lockDecision, len(req.Previous.Locks))
	reusable := []string{}
	invalidated := []string{}
	for _, lock := range req.Previous.Locks {
		decision := lockDecision{Reason: "exact closed scope"}
		node := candidate[lock.NodeID]
		if node == nil {
			decision.Reason = "candidate node removed"
		} else if status[lock.NodeID] != StatusClosed {
			decision.Reason = "node status is not CLOSED"
		} else if !impacted[lock.NodeID] {
			if lock.Toolchain != req.Candidate.Toolchain || lock.Evaluator != req.Candidate.Evaluator {
				decision.Reason = "stale toolchain or evaluator"
			} else if lock.Authority != node.Authority {
				decision.Reason = "authority changed"
			} else if lock.Provenance == "" || node.Provenance == "" {
				decision.Reason = "missing provenance"
			} else if lock.EvidenceDigest == "" {
				decision.Reason = "missing evidence digest"
			} else if len(distinctEvidence(lockGroups[lockKey(lock.NodeID, lock.CheckID)])) > 1 {
				decision.Reason = "conflicting locks"
			} else if lock.Status != StatusClosed {
				decision.Reason = "lock status is not CLOSED"
			} else {
				decision.Reusable = true
			}
		} else {
			decision.Reason = "impacted node"
		}
		decisions[lock.ID] = decision
		if decision.Reusable {
			reusable = append(reusable, lock.ID)
		} else {
			invalidated = append(invalidated, lock.ID)
		}
	}
	sort.Strings(reusable)
	sort.Strings(invalidated)

	selectedChecks := []string{}
	checkIndicator := map[string]string{}
	for _, check := range req.Candidate.Checks {
		if impacted[check.NodeID] {
			selectedChecks = append(selectedChecks, check.ID)
		}
		indicator := strings.ToUpper(check.Indicator)
		if indicator == "" {
			indicator = strings.ToUpper(candidate[check.NodeID].Indicator)
		}
		checkIndicator[check.ID] = indicator
	}
	sort.Strings(selectedChecks)

	changed = sortedUnique(changed)
	impactedIDs := make([]string, 0, len(impacted))
	for nodeID := range impacted {
		impactedIDs = append(impactedIDs, nodeID)
	}
	sort.Strings(impactedIDs)
	unresolved := buildFrontier(req.Candidate.Nodes, status, causes)
	indicators := buildIndicatorVectors(req, candidate, changedSet, impacted, selectedChecks, reusable, invalidated, unresolved, checkIndicator)
	authorities := countAuthorities(previous, req.Candidate.Nodes)
	operational := []OperationalRefutedEvent{}
	if req.ReportedExecutedChecks > 0 {
		operational = append(operational, OperationalRefutedEvent{
			Code:   "OPERATIONAL_REFUTED",
			Reason: "the input claims executed checks, but this selection-only planner cannot certify execution",
		})
	}

	return Report{
		SchemaVersion: "1",
		Scenario:      req.Scenario,
		SelectionMode: "selection-only",
		Metrics: Metrics{
			SemanticNodes:      len(req.Candidate.Nodes),
			ChangedNodes:       len(changed),
			SelectedChecks:     len(selectedChecks),
			ReusableLocks:      len(reusable),
			InvalidatedLocks:   len(invalidated),
			UnresolvedNodes:    len(unresolved),
			ExecutedChecks:     0,
			GeneratedArtifacts: generated.GeneratedArtifactCount,
		},
		ChangedNodes:      changed,
		ImpactedNodes:     impactedIDs,
		SelectedChecks:    selectedChecks,
		ReusableLocks:     reusable,
		InvalidatedLocks:  invalidated,
		Unresolved:        unresolved,
		StatusByNode:      status,
		IndicatorVectors:  indicators,
		AuthorityCounters: authorities,
		Inventory: Inventory{
			GoooFiles:                 generated.GoooFileCount,
			ConformanceCells:          generated.ConformanceCellCount,
			Activities:                generated.ActivityCount,
			RootReadmeExcluded:        generated.RootReadmeExcluded,
			CrossProjectRequiredGates: generated.CrossProjectRequiredGates,
			LocalValidationCount:      0,
			RuntimeRepositoryWrites:   0,
			TempOutputScope:           "caller-owned",
		},
		Execution: Execution{Mode: "selection-only", CacheHitsAreExecution: false},
		Performance: Performance{
			Status: "UNKNOWN",
			Reason: "build/test time or memory improvement requires an exact same-scenario, toolchain, and evaluator before/after pair",
		},
		OperationalRefuted: operational,
	}, nil
}

func requireRules() error {
	required := map[string]bool{
		"R01": false, "R02": false, "R03": false, "R04": false, "R05": false, "R06": false,
		"R07": false, "R08": false, "R09": false, "R10": false, "R11": false, "R12": false,
	}
	for _, rule := range generated.Rules {
		if _, ok := required[rule.ID]; ok {
			required[rule.ID] = true
		}
	}
	for id, present := range required {
		if !present {
			return fmt.Errorf("generated Gooo rule %s is missing", id)
		}
	}
	return nil
}

func indexNodes(nodes []Node) map[string]*Node {
	result := make(map[string]*Node, len(nodes))
	for i := range nodes {
		result[nodes[i].ID] = &nodes[i]
	}
	return result
}

func changedNodes(previous, candidate map[string]*Node) []string {
	changed := []string{}
	for id, node := range candidate {
		prior, ok := previous[id]
		if !ok || semanticFingerprint(*prior) != semanticFingerprint(*node) {
			changed = append(changed, id)
		}
	}
	for id := range previous {
		if _, ok := candidate[id]; !ok {
			changed = append(changed, id)
		}
	}
	return sortedUnique(changed)
}

func semanticFingerprint(node Node) string {
	deps := append([]string(nil), node.Deps...)
	sort.Strings(deps)
	return strings.Join([]string{node.Digest, strings.Join(deps, ","), node.Authority, node.Provenance, strings.ToUpper(node.Indicator), normalizeStatus(node.Status)}, "\x00")
}

func makeSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func projectDependents(nodes []Node, seed map[string]bool) map[string]bool {
	dependents := map[string][]string{}
	for _, node := range nodes {
		for _, dependency := range node.Deps {
			dependents[dependency] = append(dependents[dependency], node.ID)
		}
	}
	frontier := make([]string, 0, len(seed))
	impacted := map[string]bool{}
	for nodeID := range seed {
		if nodeID != "" {
			frontier = append(frontier, nodeID)
		}
	}
	sort.Strings(frontier)
	for len(frontier) > 0 {
		nodeID := frontier[0]
		frontier = frontier[1:]
		if impacted[nodeID] {
			continue
		}
		impacted[nodeID] = true
		for _, dependent := range dependents[nodeID] {
			if !impacted[dependent] {
				frontier = append(frontier, dependent)
			}
		}
		sort.Strings(frontier)
	}
	return impacted
}

func groupLocks(locks []EvidenceLock) map[string][]EvidenceLock {
	groups := map[string][]EvidenceLock{}
	for _, lock := range locks {
		key := lockKey(lock.NodeID, lock.CheckID)
		groups[key] = append(groups[key], lock)
	}
	return groups
}

func lockKey(nodeID, checkID string) string {
	return nodeID + "\x00" + checkID
}

func distinctEvidence(locks []EvidenceLock) []string {
	values := []string{}
	seen := map[string]bool{}
	for _, lock := range locks {
		if !seen[lock.EvidenceDigest] {
			seen[lock.EvidenceDigest] = true
			values = append(values, lock.EvidenceDigest)
		}
	}
	return values
}

func lockIDs(locks []EvidenceLock) []string {
	ids := make([]string, 0, len(locks))
	for _, lock := range locks {
		ids = append(ids, lock.ID)
	}
	return sortedUnique(ids)
}

func addCause(causes map[string][]unknownCause, nodeID string, cause unknownCause) {
	causes[nodeID] = append(causes[nodeID], cause)
}

func buildFrontier(nodes []Node, status map[string]string, causes map[string][]unknownCause) []UnknownFrontier {
	ids := []string{}
	for _, node := range nodes {
		if normalizeStatus(status[node.ID]) != StatusClosed || len(causes[node.ID]) > 0 {
			ids = append(ids, node.ID)
		}
	}
	sort.Strings(ids)
	result := make([]UnknownFrontier, 0, len(ids))
	for _, nodeID := range ids {
		items := causes[nodeID]
		if len(items) == 0 {
			items = []unknownCause{{
				Stage: "evidence", Step: "status-reconciliation", Reason: "evidence status is not CLOSED", Class: "non-closed-evidence",
				NextOperation: "run-selected-check-and-reconcile-evidence", BlockedBy: []string{"node:" + nodeID},
			}}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Class != items[j].Class {
				return items[i].Class < items[j].Class
			}
			return items[i].Reason < items[j].Reason
		})
		blocked := []string{}
		classes := []string{}
		seenClasses := map[string]bool{}
		for _, item := range items {
			blocked = append(blocked, item.BlockedBy...)
			if !seenClasses[item.Class] {
				seenClasses[item.Class] = true
				classes = append(classes, item.Class)
			}
		}
		result = append(result, UnknownFrontier{
			NodeID:        nodeID,
			Status:        normalizeStatus(status[nodeID]),
			Stage:         items[0].Stage,
			Step:          items[0].Step,
			Reason:        joinReasons(items),
			UnknownClass:  strings.Join(classes, "+"),
			NextOperation: items[0].NextOperation,
			BlockedBy:     sortedUnique(blocked),
		})
	}
	return result
}

func joinReasons(items []unknownCause) string {
	reasons := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		if !seen[item.Reason] {
			seen[item.Reason] = true
			reasons = append(reasons, item.Reason)
		}
	}
	return strings.Join(reasons, "; ")
}

func buildIndicatorVectors(req Request, nodes map[string]*Node, changed, impacted map[string]bool, selected, reusable, invalidated []string, unresolved []UnknownFrontier, checkIndicator map[string]string) map[string]IndicatorVector {
	result := map[string]IndicatorVector{}
	for _, node := range req.Candidate.Nodes {
		indicator := strings.ToUpper(node.Indicator)
		if indicator == "" {
			indicator = "UNKNOWN"
		}
		vector := result[indicator]
		vector.SemanticNodes++
		if changed[node.ID] {
			vector.ChangedNodes++
		}
		result[indicator] = vector
	}
	for _, checkID := range selected {
		indicator := checkIndicator[checkID]
		if indicator == "" {
			indicator = "UNKNOWN"
		}
		vector := result[indicator]
		vector.SelectedChecks++
		result[indicator] = vector
	}
	for _, lockID := range append(append([]string{}, reusable...), invalidated...) {
		for _, lock := range req.Previous.Locks {
			if lock.ID == lockID {
				indicator := "UNKNOWN"
				if node := nodes[lock.NodeID]; node != nil {
					indicator = strings.ToUpper(node.Indicator)
				}
				vector := result[indicator]
				if contains(reusable, lockID) {
					vector.ReusableLocks++
				} else {
					vector.InvalidatedLocks++
				}
				result[indicator] = vector
			}
		}
	}
	for _, item := range unresolved {
		indicator := "UNKNOWN"
		if node := nodes[item.NodeID]; node != nil {
			indicator = strings.ToUpper(node.Indicator)
		}
		vector := result[indicator]
		vector.UnresolvedNodes++
		result[indicator] = vector
	}
	for indicator, vector := range result {
		vector.ExecutedChecks = 0
		result[indicator] = vector
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func countAuthorities(previous map[string]*Node, candidate []Node) AuthorityCounters {
	result := AuthorityCounters{}
	for _, node := range candidate {
		if node.Authority == "" {
			result.Unknown++
		} else {
			result.Known++
		}
		if prior, ok := previous[node.ID]; ok && prior.Authority != node.Authority {
			result.Changed++
		}
	}
	return result
}
