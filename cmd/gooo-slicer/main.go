package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/kimjooyoon/gooo-semantic-impact-slicer/internal/generated"
	"github.com/kimjooyoon/gooo-semantic-impact-slicer/internal/impact"
)

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: gooo-slicer <plan|conformance|replay>"))
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = planCommand(os.Args[2:])
	case "conformance":
		err = conformanceCommand(os.Args[2:])
	case "replay":
		err = replayCommand(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func planCommand(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	input := flags.String("input", "", "JSON request path")
	measure := flags.Bool("measure", false, "record slicer wall time")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("plan requires --input")
	}
	req, err := readRequest(*input)
	if err != nil {
		return err
	}
	started := time.Now()
	report, err := impact.Plan(req)
	if err != nil {
		return err
	}
	if *measure {
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		report.Performance.WallMS = &elapsed
	}
	return writeJSON(report)
}

func conformanceCommand(args []string) error {
	flags := flag.NewFlagSet("conformance", flag.ContinueOnError)
	root := flags.String("fixtures", "fixtures", "fixture directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	fixtures, err := loadFixtures(*root)
	if err != nil {
		return err
	}
	result := conformanceResult{Mode: "conformance", FixtureCount: len(fixtures), Passed: 0, Failed: 0, CellsByCategory: map[string]int{}, Vectors: []conformanceVector{}}
	wantScenarios := map[string]bool{}
	for _, cell := range generated.ConformanceCells {
		wantScenarios[cell.Scenario] = true
		result.CellsByCategory[cell.Category]++
	}
	if len(fixtures) != generated.ConformanceCellCount {
		result.Failed++
		result.Errors = append(result.Errors, fmt.Sprintf("expected %d fixtures, got %d", generated.ConformanceCellCount, len(fixtures)))
	}
	seenScenarios := map[string]bool{}
	for _, item := range fixtures {
		vector := conformanceVector{Fixture: item.name, Scenario: item.fixture.Request.Scenario, Status: "PASS"}
		cell, ok := cellForScenario(item.fixture.Request.Scenario)
		if !ok {
			vector.Status = "FAIL"
			vector.Error = "fixture scenario is not projected by a Gooo conformance cell"
		} else {
			seenScenarios[item.fixture.Request.Scenario] = true
			vector.Cell = cell.ID
			report, planErr := impact.Plan(item.fixture.Request)
			if planErr != nil {
				vector.Status = "FAIL"
				vector.Error = planErr.Error()
			} else if compareExpected(report, item.fixture.Expected) != nil {
				vector.Status = "FAIL"
				vector.Error = compareExpected(report, item.fixture.Expected).Error()
			}
		}
		if vector.Status == "PASS" {
			result.Passed++
		} else {
			result.Failed++
			result.Errors = append(result.Errors, item.name+": "+vector.Error)
		}
		result.Vectors = append(result.Vectors, vector)
	}
	for scenario := range wantScenarios {
		if !seenScenarios[scenario] {
			result.Failed++
			result.Errors = append(result.Errors, "missing fixture for scenario "+scenario)
		}
	}
	sort.Slice(result.Vectors, func(i, j int) bool { return result.Vectors[i].Fixture < result.Vectors[j].Fixture })
	sort.Strings(result.Errors)
	if err := writeJSON(result); err != nil {
		return err
	}
	if result.Failed != 0 {
		return fmt.Errorf("conformance failed: %d failure(s)", result.Failed)
	}
	return nil
}

func replayCommand(args []string) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	root := flags.String("fixtures", "fixtures", "fixture directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	fixtures, err := loadFixtures(*root)
	if err != nil {
		return err
	}
	result := replayResult{Mode: "replay", FixtureCount: len(fixtures), Reports: []replayVector{}}
	aggregateInput := strings.Builder{}
	for _, item := range fixtures {
		report, planErr := impact.Plan(item.fixture.Request)
		if planErr != nil {
			return fmt.Errorf("%s: %w", item.name, planErr)
		}
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		hash := hex.EncodeToString(digest[:])
		result.Reports = append(result.Reports, replayVector{
			Fixture: item.name, Scenario: item.fixture.Request.Scenario, ReportSHA256: hash,
			SelectedChecks: report.Metrics.SelectedChecks, UnresolvedNodes: report.Metrics.UnresolvedNodes,
		})
		aggregateInput.WriteString(item.name)
		aggregateInput.WriteByte(0)
		aggregateInput.WriteString(hash)
		aggregateInput.WriteByte('\n')
	}
	sort.Slice(result.Reports, func(i, j int) bool { return result.Reports[i].Fixture < result.Reports[j].Fixture })
	aggregate := sha256.Sum256([]byte(aggregateInput.String()))
	result.AggregateSHA256 = hex.EncodeToString(aggregate[:])
	return writeJSON(result)
}

type loadedFixture struct {
	name    string
	fixture impact.Fixture
}

func loadFixtures(root string) ([]loadedFixture, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	result := make([]loadedFixture, 0, len(paths))
	for _, path := range paths {
		var fixture impact.Fixture
		if err := readJSON(path, &fixture); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		result = append(result, loadedFixture{name: filepath.Base(path), fixture: fixture})
	}
	return result, nil
}

func cellForScenario(scenario string) (generated.ConformanceCell, bool) {
	for _, cell := range generated.ConformanceCells {
		if cell.Scenario == scenario {
			return cell, true
		}
	}
	return generated.ConformanceCell{}, false
}

func compareExpected(report impact.Report, expected impact.FixtureExpected) error {
	if report.Metrics.SemanticNodes != expected.SemanticNodes {
		return fmt.Errorf("semantic_nodes: got %d want %d", report.Metrics.SemanticNodes, expected.SemanticNodes)
	}
	if !reflect.DeepEqual(report.ChangedNodes, expected.ChangedNodes) {
		return fmt.Errorf("changed_nodes: got %v want %v", report.ChangedNodes, expected.ChangedNodes)
	}
	if !reflect.DeepEqual(report.ImpactedNodes, expected.ImpactedNodes) {
		return fmt.Errorf("impacted_nodes: got %v want %v", report.ImpactedNodes, expected.ImpactedNodes)
	}
	if !reflect.DeepEqual(report.SelectedChecks, expected.SelectedChecks) {
		return fmt.Errorf("selected_checks: got %v want %v", report.SelectedChecks, expected.SelectedChecks)
	}
	if !reflect.DeepEqual(report.ReusableLocks, expected.ReusableLocks) {
		return fmt.Errorf("reusable_locks: got %v want %v", report.ReusableLocks, expected.ReusableLocks)
	}
	if !reflect.DeepEqual(report.InvalidatedLocks, expected.InvalidatedLocks) {
		return fmt.Errorf("invalidated_locks: got %v want %v", report.InvalidatedLocks, expected.InvalidatedLocks)
	}
	actualUnresolved := make([]string, 0, len(report.Unresolved))
	for _, item := range report.Unresolved {
		actualUnresolved = append(actualUnresolved, item.NodeID)
	}
	if !reflect.DeepEqual(actualUnresolved, expected.UnresolvedNodes) {
		return fmt.Errorf("unresolved_nodes: got %v want %v", actualUnresolved, expected.UnresolvedNodes)
	}
	if report.Metrics.ExecutedChecks != expected.ExecutedChecks {
		return fmt.Errorf("executed_checks: got %d want %d", report.Metrics.ExecutedChecks, expected.ExecutedChecks)
	}
	if report.Metrics.GeneratedArtifacts != expected.GeneratedArtifacts {
		return fmt.Errorf("generated_artifacts: got %d want %d", report.Metrics.GeneratedArtifacts, expected.GeneratedArtifacts)
	}
	if !reflect.DeepEqual(report.StatusByNode, expected.Statuses) {
		return fmt.Errorf("statuses: got %v want %v", report.StatusByNode, expected.Statuses)
	}
	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeJSON(data, target)
}

func readRequest(path string) (impact.Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return impact.Request{}, err
	}
	var root map[string]json.RawMessage
	if err := decodeJSON(data, &root); err != nil {
		return impact.Request{}, err
	}
	if _, wrapped := root["request"]; wrapped {
		var fixture impact.Fixture
		if err := decodeJSON(data, &fixture); err != nil {
			return impact.Request{}, err
		}
		return fixture.Request, nil
	}
	var request impact.Request
	if err := decodeJSON(data, &request); err != nil {
		return impact.Request{}, err
	}
	return request, nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

type conformanceResult struct {
	Mode            string              `json:"mode"`
	FixtureCount    int                 `json:"fixture_count"`
	Passed          int                 `json:"passed"`
	Failed          int                 `json:"failed"`
	CellsByCategory map[string]int      `json:"cells_by_category"`
	Vectors         []conformanceVector `json:"vectors"`
	Errors          []string            `json:"errors,omitempty"`
}

type conformanceVector struct {
	Fixture  string `json:"fixture"`
	Cell     string `json:"cell,omitempty"`
	Scenario string `json:"scenario"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

type replayResult struct {
	Mode            string         `json:"mode"`
	FixtureCount    int            `json:"fixture_count"`
	AggregateSHA256 string         `json:"aggregate_sha256"`
	Reports         []replayVector `json:"reports"`
}

type replayVector struct {
	Fixture         string `json:"fixture"`
	Scenario        string `json:"scenario"`
	ReportSHA256    string `json:"report_sha256"`
	SelectedChecks  int    `json:"selected_checks"`
	UnresolvedNodes int    `json:"unresolved_nodes"`
}
