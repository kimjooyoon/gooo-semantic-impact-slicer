package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type record struct {
	ID     string
	Fields map[string]string
}

type sourceModel struct {
	Files        []string
	Rules        []record
	Cells        []record
	Activities   []record
	RuleKinds    map[string]bool
	CellCategory map[string]int
	ActCategory  map[string]int
}

func main() {
	input := flag.String("input", "gooo", "directory containing .gooo files")
	output := flag.String("output", "internal/generated/model.gen.go", "generated Go file")
	flag.Parse()

	model, err := readModel(*input)
	if err != nil {
		fail(err)
	}
	if err := validate(model); err != nil {
		fail(err)
	}
	if err := writeProjection(*output, model); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func readModel(root string) (sourceModel, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*.gooo"))
	if err != nil {
		return sourceModel{}, err
	}
	sort.Strings(paths)
	model := sourceModel{
		RuleKinds:    map[string]bool{},
		CellCategory: map[string]int{},
		ActCategory:  map[string]int{},
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return sourceModel{}, err
		}
		lines := strings.Split(string(data), "\n")
		kind := ""
		for lineNumber, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "gooo" {
				if fields[1] != "1" {
					return sourceModel{}, fmt.Errorf("%s:%d: unsupported gooo version %s", path, lineNumber+1, fields[1])
				}
				continue
			}
			if len(fields) == 2 && fields[0] == "kind" {
				kind = fields[1]
				continue
			}
			if len(fields) < 2 {
				return sourceModel{}, fmt.Errorf("%s:%d: malformed record", path, lineNumber+1)
			}
			values := record{ID: fields[1], Fields: map[string]string{}}
			for _, field := range fields[2:] {
				parts := strings.SplitN(field, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return sourceModel{}, fmt.Errorf("%s:%d: malformed field %q", path, lineNumber+1, field)
				}
				values.Fields[parts[0]] = parts[1]
			}
			switch fields[0] {
			case "rule":
				if kind != "rules" {
					return sourceModel{}, fmt.Errorf("%s:%d: rule outside rules source", path, lineNumber+1)
				}
				model.Rules = append(model.Rules, values)
				model.RuleKinds[values.Fields["domain"]+":"+values.Fields["operation"]] = true
			case "cell":
				if kind != "conformance" {
					return sourceModel{}, fmt.Errorf("%s:%d: cell outside conformance source", path, lineNumber+1)
				}
				model.Cells = append(model.Cells, values)
				model.CellCategory[values.Fields["category"]]++
			case "activity":
				if kind != "activities" {
					return sourceModel{}, fmt.Errorf("%s:%d: activity outside activities source", path, lineNumber+1)
				}
				model.Activities = append(model.Activities, values)
				model.ActCategory[values.Fields["category"]]++
			default:
				return sourceModel{}, fmt.Errorf("%s:%d: unknown record %q", path, lineNumber+1, fields[0])
			}
		}
		model.Files = append(model.Files, filepath.Base(path))
	}
	return model, nil
}

func validate(model sourceModel) error {
	if len(model.Files) != 3 {
		return fmt.Errorf("expected exactly 3 .gooo files, got %d", len(model.Files))
	}
	if len(model.Rules) != 12 {
		return fmt.Errorf("expected exactly 12 rules, got %d", len(model.Rules))
	}
	if len(model.Cells) != 12 {
		return fmt.Errorf("expected exactly 12 conformance cells, got %d", len(model.Cells))
	}
	if len(model.Activities) != 12 {
		return fmt.Errorf("expected exactly 12 activities, got %d", len(model.Activities))
	}
	for _, category := range []string{"FOUNDATION", "COHERENCE", "REGRESSION"} {
		if model.CellCategory[category] != 4 {
			return fmt.Errorf("expected 4 %s conformance cells, got %d", category, model.CellCategory[category])
		}
	}
	for _, category := range []string{"DRIVER", "OUTCOME", "GUARDRAIL"} {
		if model.ActCategory[category] != 4 {
			return fmt.Errorf("expected 4 %s activities, got %d", category, model.ActCategory[category])
		}
	}
	requiredRules := []string{
		"dependency:transitive_dependents",
		"invalidation:semantic_change",
		"invalidation:authority_change",
		"invalidation:environment_scope",
		"reuse:closed_exact_scope",
		"reuse:provenance",
	}
	for _, rule := range requiredRules {
		if !model.RuleKinds[rule] {
			return fmt.Errorf("missing required rule %s", rule)
		}
	}
	return nil
}

func writeProjection(path string, model sourceModel) error {
	sort.Slice(model.Rules, func(i, j int) bool { return model.Rules[i].ID < model.Rules[j].ID })
	sort.Slice(model.Cells, func(i, j int) bool { return model.Cells[i].ID < model.Cells[j].ID })
	sort.Slice(model.Activities, func(i, j int) bool { return model.Activities[i].ID < model.Activities[j].ID })
	sort.Strings(model.Files)
	var b strings.Builder
	b.WriteString("// Code generated by gooo-gen; DO NOT EDIT.\n\n")
	b.WriteString("package generated\n\n")
	b.WriteString("type Rule struct { ID, Domain, Operation, Condition, Outcome string }\n")
	b.WriteString("type ConformanceCell struct { ID, Category, Scenario, Focus string }\n")
	b.WriteString("type Activity struct { ID, Category, Name string }\n\n")
	b.WriteString("var Rules = []Rule{\n")
	for _, item := range model.Rules {
		fmt.Fprintf(&b, "\t{ID: %q, Domain: %q, Operation: %q, Condition: %q, Outcome: %q},\n", item.ID, item.Fields["domain"], item.Fields["operation"], item.Fields["condition"], item.Fields["outcome"])
	}
	b.WriteString("}\n\nvar ConformanceCells = []ConformanceCell{\n")
	for _, item := range model.Cells {
		fmt.Fprintf(&b, "\t{ID: %q, Category: %q, Scenario: %q, Focus: %q},\n", item.ID, item.Fields["category"], item.Fields["scenario"], item.Fields["focus"])
	}
	b.WriteString("}\n\nvar Activities = []Activity{\n")
	for _, item := range model.Activities {
		fmt.Fprintf(&b, "\t{ID: %q, Category: %q, Name: %q},\n", item.ID, item.Fields["category"], item.Fields["name"])
	}
	b.WriteString("}\n\n")
	b.WriteString("const GoooFileCount = ")
	b.WriteString(strconv.Itoa(len(model.Files)))
	b.WriteString("\nconst ConformanceCellCount = ")
	b.WriteString(strconv.Itoa(len(model.Cells)))
	b.WriteString("\nconst ActivityCount = ")
	b.WriteString(strconv.Itoa(len(model.Activities)))
	b.WriteString("\nconst GeneratedArtifactCount = 1\nconst RootReadmeExcluded = true\nconst CrossProjectRequiredGates = 0\n")
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return err
	}
	return nil
}
