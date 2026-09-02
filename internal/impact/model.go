package impact

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kimjooyoon/gooo-semantic-impact-slicer/internal/generated"
)

const (
	StatusClosed  = "CLOSED"
	StatusUnknown = "UNKNOWN"
	StatusRefuted = "REFUTED"
)

// Request is a pinned graph/evidence-lock snapshot paired with a candidate.
// The planner only selects work; it does not execute checks.
type Request struct {
	Scenario               string   `json:"scenario"`
	Previous               Snapshot `json:"previous"`
	Candidate              Snapshot `json:"candidate"`
	ReportedExecutedChecks int      `json:"reported_executed_checks,omitempty"`
}

type Snapshot struct {
	Toolchain string         `json:"toolchain"`
	Evaluator string         `json:"evaluator"`
	Nodes     []Node         `json:"nodes"`
	Locks     []EvidenceLock `json:"locks"`
	Checks    []Check        `json:"checks"`
}

type Node struct {
	ID                string   `json:"id"`
	Digest            string   `json:"digest"`
	Deps              []string `json:"deps"`
	Authority         string   `json:"authority"`
	Provenance        string   `json:"provenance"`
	Indicator         string   `json:"indicator"`
	Status            string   `json:"status,omitempty"`
	UnknownDependency bool     `json:"unknown_dependency,omitempty"`
}

type Check struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	CellID    string `json:"cell_id"`
	Indicator string `json:"indicator"`
}

type EvidenceLock struct {
	ID             string `json:"id"`
	NodeID         string `json:"node_id"`
	CheckID        string `json:"check_id"`
	Status         string `json:"status"`
	EvidenceDigest string `json:"evidence_digest"`
	Toolchain      string `json:"toolchain"`
	Evaluator      string `json:"evaluator"`
	Authority      string `json:"authority"`
	Provenance     string `json:"provenance"`
}

type Report struct {
	SchemaVersion      string                     `json:"schema_version"`
	Scenario           string                     `json:"scenario"`
	SelectionMode      string                     `json:"selection_mode"`
	Metrics            Metrics                    `json:"metrics"`
	ChangedNodes       []string                   `json:"changed_nodes"`
	ImpactedNodes      []string                   `json:"impacted_nodes"`
	SelectedChecks     []string                   `json:"selected_checks"`
	ReusableLocks      []string                   `json:"reusable_locks"`
	InvalidatedLocks   []string                   `json:"invalidated_locks"`
	Unresolved         []UnknownFrontier          `json:"unresolved"`
	StatusByNode       map[string]string          `json:"status_by_node"`
	IndicatorVectors   map[string]IndicatorVector `json:"indicator_vectors"`
	AuthorityCounters  AuthorityCounters          `json:"authority_counters"`
	Inventory          Inventory                  `json:"inventory"`
	Execution          Execution                  `json:"execution"`
	Performance        Performance                `json:"performance"`
	OperationalRefuted []OperationalRefutedEvent  `json:"operational_refuted"`
}

type Metrics struct {
	SemanticNodes      int `json:"semantic_nodes"`
	ChangedNodes       int `json:"changed_nodes"`
	SelectedChecks     int `json:"selected_checks"`
	ReusableLocks      int `json:"reusable_locks"`
	InvalidatedLocks   int `json:"invalidated_locks"`
	UnresolvedNodes    int `json:"unresolved_nodes"`
	ExecutedChecks     int `json:"executed_checks"`
	GeneratedArtifacts int `json:"generated_artifacts"`
}

type UnknownFrontier struct {
	NodeID        string   `json:"node_id"`
	Status        string   `json:"status"`
	Stage         string   `json:"stage"`
	Step          string   `json:"step"`
	Reason        string   `json:"reason"`
	UnknownClass  string   `json:"unknown_class"`
	NextOperation string   `json:"next_operation"`
	BlockedBy     []string `json:"blocked_by"`
}

type IndicatorVector struct {
	SemanticNodes    int `json:"semantic_nodes"`
	ChangedNodes     int `json:"changed_nodes"`
	SelectedChecks   int `json:"selected_checks"`
	ReusableLocks    int `json:"reusable_locks"`
	InvalidatedLocks int `json:"invalidated_locks"`
	UnresolvedNodes  int `json:"unresolved_nodes"`
	ExecutedChecks   int `json:"executed_checks"`
}

type AuthorityCounters struct {
	Known   int `json:"known"`
	Unknown int `json:"unknown"`
	Changed int `json:"changed"`
}

type Inventory struct {
	GoooFiles                 int    `json:"gooo_files"`
	ConformanceCells          int    `json:"conformance_cells"`
	Activities                int    `json:"activities"`
	RootReadmeExcluded        bool   `json:"root_readme_excluded"`
	CrossProjectRequiredGates int    `json:"cross_project_required_gates"`
	LocalValidationCount      int    `json:"local_validation_count"`
	RuntimeRepositoryWrites   int    `json:"runtime_repository_writes"`
	TempOutputScope           string `json:"temp_output_scope"`
}

type Execution struct {
	Mode                  string `json:"mode"`
	CacheHitsAreExecution bool   `json:"cache_hits_are_execution"`
}

type Performance struct {
	Status string   `json:"status"`
	Reason string   `json:"reason"`
	WallMS *float64 `json:"wall_ms,omitempty"`
}

type OperationalRefutedEvent struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type Fixture struct {
	Request  Request         `json:"request"`
	Expected FixtureExpected `json:"expected"`
}

type FixtureExpected struct {
	SemanticNodes      int               `json:"semantic_nodes"`
	ChangedNodes       []string          `json:"changed_nodes"`
	ImpactedNodes      []string          `json:"impacted_nodes"`
	SelectedChecks     []string          `json:"selected_checks"`
	ReusableLocks      []string          `json:"reusable_locks"`
	InvalidatedLocks   []string          `json:"invalidated_locks"`
	UnresolvedNodes    []string          `json:"unresolved_nodes"`
	ExecutedChecks     int               `json:"executed_checks"`
	GeneratedArtifacts int               `json:"generated_artifacts"`
	Statuses           map[string]string `json:"statuses"`
}

func normalizeStatus(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return StatusClosed
	}
	return value
}

func statusRank(value string) int {
	switch normalizeStatus(value) {
	case StatusRefuted:
		return 2
	case StatusUnknown:
		return 1
	default:
		return 0
	}
}

func highestStatus(values ...string) string {
	status := StatusClosed
	for _, value := range values {
		value = normalizeStatus(value)
		if statusRank(value) > statusRank(status) {
			status = value
		}
	}
	return status
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Scenario) == "" {
		return fmt.Errorf("scenario is required")
	}
	if err := validateNodes("previous", req.Previous.Nodes); err != nil {
		return err
	}
	if err := validateNodes("candidate", req.Candidate.Nodes); err != nil {
		return err
	}
	if err := validateChecks(req.Candidate); err != nil {
		return err
	}
	for _, lock := range req.Previous.Locks {
		if strings.TrimSpace(lock.ID) == "" || strings.TrimSpace(lock.NodeID) == "" || strings.TrimSpace(lock.CheckID) == "" {
			return fmt.Errorf("previous locks require id, node_id, and check_id")
		}
		if status := normalizeStatus(lock.Status); status != StatusClosed && status != StatusUnknown && status != StatusRefuted {
			return fmt.Errorf("previous lock %s has invalid status %q", lock.ID, lock.Status)
		}
	}
	return nil
}

func validateNodes(label string, nodes []Node) error {
	seen := map[string]bool{}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("%s node has empty id", label)
		}
		if seen[node.ID] {
			return fmt.Errorf("%s has duplicate node %s", label, node.ID)
		}
		seen[node.ID] = true
		status := normalizeStatus(node.Status)
		if status != StatusClosed && status != StatusUnknown && status != StatusRefuted {
			return fmt.Errorf("%s node %s has invalid status %q", label, node.ID, node.Status)
		}
	}
	return nil
}

func validateChecks(snapshot Snapshot) error {
	seen := map[string]bool{}
	nodes := map[string]bool{}
	for _, node := range snapshot.Nodes {
		nodes[node.ID] = true
	}
	cells := map[string]bool{}
	for _, cell := range generated.ConformanceCells {
		cells[cell.ID] = true
	}
	for _, check := range snapshot.Checks {
		if check.ID == "" || check.NodeID == "" || check.CellID == "" {
			return fmt.Errorf("checks require id, node_id, and cell_id")
		}
		if seen[check.ID] {
			return fmt.Errorf("candidate has duplicate check %s", check.ID)
		}
		if !nodes[check.NodeID] {
			return fmt.Errorf("check %s references unknown node %s", check.ID, check.NodeID)
		}
		if !cells[check.CellID] {
			return fmt.Errorf("check %s references unknown conformance cell %s", check.ID, check.CellID)
		}
		seen[check.ID] = true
	}
	return nil
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
