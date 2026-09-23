package impact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestDeterministicFixtureVectors(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) != 12 {
		t.Fatalf("fixture count = %d, want 12", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fixture Fixture
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		report, err := Plan(fixture.Request)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := assertFixture(report, fixture.Expected); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

func TestUnknownFrontierContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "C05-missing-provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	report, err := Plan(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.Unresolved {
		if item.Status == StatusUnknown {
			for field, value := range map[string]string{
				"stage": item.Stage, "step": item.Step, "reason": item.Reason,
				"unknown_class": item.UnknownClass, "next_operation": item.NextOperation,
			} {
				if value == "" {
					t.Errorf("unknown frontier %s is missing %s", item.NodeID, field)
				}
			}
			if len(item.BlockedBy) == 0 {
				t.Errorf("unknown frontier %s has no blocked_by", item.NodeID)
			}
		}
	}
}

func TestSelectionDoesNotClaimExecutionOrScore(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "C10-no-op-candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	report, err := Plan(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics.ExecutedChecks != 0 || report.Execution.CacheHitsAreExecution {
		t.Fatalf("selection plan claimed execution: %+v", report.Execution)
	}
	data, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "score") {
		t.Fatalf("scalar score appeared in report: %s", data)
	}
}

func TestInventoryAndAuthorityCounters(t *testing.T) {
	request := Request{
		Scenario: "inventory-test",
		Previous: Snapshot{Nodes: []Node{{ID: "a", Authority: "old", Digest: "a", Provenance: "p"}}},
		Candidate: Snapshot{Nodes: []Node{
			{ID: "a", Authority: "new", Digest: "a", Provenance: "p", Indicator: "FOUNDATION"},
			{ID: "b", Authority: "", Digest: "b", Provenance: "p", Indicator: "COHERENCE"},
		}},
	}
	report, err := Plan(request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Inventory.GoooFiles != 3 || report.Inventory.ConformanceCells != 12 || report.Inventory.Activities != 12 {
		t.Fatalf("unexpected inventory: %+v", report.Inventory)
	}
	if report.Inventory.RootReadmeExcluded != true || report.Inventory.CrossProjectRequiredGates != 0 || report.Inventory.LocalValidationCount != 0 || report.Inventory.RuntimeRepositoryWrites != 0 {
		t.Fatalf("unexpected guardrail inventory: %+v", report.Inventory)
	}
	if !reflect.DeepEqual(report.AuthorityCounters, AuthorityCounters{Known: 1, Unknown: 1, Changed: 1}) {
		t.Fatalf("unexpected authority counters: %+v", report.AuthorityCounters)
	}
}

func TestRejectsDuplicateEvidenceLockIDs(t *testing.T) {
	req := Request{
		Scenario: "duplicate-lock-id",
		Previous: Snapshot{Locks: []EvidenceLock{
			{ID: "lock-1", NodeID: "node-a", CheckID: "check-a", Status: StatusClosed},
			{ID: "lock-1", NodeID: "node-a", CheckID: "check-b", Status: StatusClosed},
		}},
		Candidate: Snapshot{Nodes: []Node{{ID: "node-a", Digest: "digest-a", Provenance: "source-a"}}},
	}
	if _, err := Plan(req); err == nil {
		t.Fatal("expected duplicate evidence lock IDs to be rejected")
	}
}

func assertFixture(report Report, expected FixtureExpected) error {
	actualUnresolved := make([]string, 0, len(report.Unresolved))
	for _, item := range report.Unresolved {
		actualUnresolved = append(actualUnresolved, item.NodeID)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"semantic_nodes", report.Metrics.SemanticNodes, expected.SemanticNodes},
		{"changed_nodes", report.ChangedNodes, expected.ChangedNodes},
		{"impacted_nodes", report.ImpactedNodes, expected.ImpactedNodes},
		{"selected_checks", report.SelectedChecks, expected.SelectedChecks},
		{"reusable_locks", report.ReusableLocks, expected.ReusableLocks},
		{"invalidated_locks", report.InvalidatedLocks, expected.InvalidatedLocks},
		{"unresolved_nodes", actualUnresolved, expected.UnresolvedNodes},
		{"executed_checks", report.Metrics.ExecutedChecks, expected.ExecutedChecks},
		{"generated_artifacts", report.Metrics.GeneratedArtifacts, expected.GeneratedArtifacts},
		{"statuses", report.StatusByNode, expected.Statuses},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			return fmt.Errorf("%s got %v want %v", check.name, check.got, check.want)
		}
	}
	return nil
}
