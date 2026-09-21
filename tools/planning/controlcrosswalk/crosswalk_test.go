package controlcrosswalk

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func TestTodo_GOV_030(t *testing.T) {
	revision := realRevision(t)
	if len(revision.Controls) != 51 {
		t.Fatalf("control count = %d, want 51 matrix rows", len(revision.Controls))
	}
	if findControl(revision, "E-05").Status != Implemented {
		t.Fatalf("E-05 status = %q, want %q", findControl(revision, "E-05").Status, Implemented)
	}
	if findControl(revision, "E-16").Status != Implemented {
		t.Fatalf("E-16 status = %q, want %q", findControl(revision, "E-16").Status, Implemented)
	}
	if err := Verify(revision); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_GOV_030_Golden(t *testing.T) {
	revision := realRevision(t)
	data, err := json.MarshalIndent(struct {
		SchemaVersion int    `json:"schema_version"`
		Version       string `json:"version"`
		Controls      int    `json:"controls"`
		Digest        string `json:"digest"`
	}{revision.SchemaVersion, revision.Version, len(revision.Controls), revision.Digest}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "real-registry.golden.json")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(data, '\n'), want) {
		t.Fatalf("real-registry golden mismatch:\n got %s\nwant %s", data, want)
	}
}

func TestTodo_GOV_030_Security(t *testing.T) {
	definition, todos := realInputs(t)
	for index := range definition.Controls {
		if definition.Controls[index].ID == "F-13" {
			definition.Controls[index].DeclaredStatus = string(Missing)
		}
	}
	_, err := Regenerate(definition, todos, nil)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Regenerate error = %v, want typed validation error", err)
	}
	found := false
	for _, finding := range validation.Findings {
		if finding.Code == "STATUS_NOT_DERIVED" && strings.Contains(finding.Field, ".status") {
			found = true
		}
	}
	if !found {
		t.Fatalf("status refusal did not name the status field: %+v", validation.Findings)
	}
	revision := realRevision(t)
	explanation := revision.Explain()
	for _, raw := range []string{"F-13", "PRIV-008", "internal/"} {
		if strings.Contains(explanation, raw) {
			t.Fatalf("Explain leaked raw identifier %q: %s", raw, explanation)
		}
	}
}

func TestTodo_GOV_030_Integration(t *testing.T) {
	definition, todos := realInputs(t)
	revision, err := NewCompiler(definition, todos).Regenerate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Controls) != len(definition.Controls) {
		t.Fatalf("compiled controls = %d, seed controls = %d", len(revision.Controls), len(definition.Controls))
	}
}

func TestTodo_GOV_030_Conformance(t *testing.T) {
	revision := realRevision(t)
	for _, control := range revision.Controls {
		frameworks := []string{control.Frameworks.NIST80053, control.Frameworks.CSF20, control.Frameworks.ISO27001, control.Frameworks.SOC2, control.Frameworks.CIS, control.Frameworks.ASVS}
		for _, value := range frameworks {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("control %s has an empty framework reference", control.ID)
			}
		}
		if control.OwnerPackage == "" || len(control.TodoIDs) == 0 || len(control.Evidence) == 0 || control.Digest == "" {
			t.Fatalf("incomplete resolved control: %+v", control)
		}
		if control.Status != Implemented && control.Status != Partial && control.Status != Missing {
			t.Fatalf("invalid status %q", control.Status)
		}
		for _, evidence := range control.Evidence {
			if evidence.TestName == "" {
				t.Fatalf("evidence %s has no registry-resolved test", control.ID)
			}
		}
	}
}

func TestTodo_GOV_030_Mutation(t *testing.T) {
	definition, todos := realInputs(t)
	before, err := Regenerate(definition, todos, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutated := append([]todoregistry.Todo(nil), todos...)
	for index := range mutated {
		if mutated[index].ID == "GOV-030" {
			mutated[index].Done = false
		}
	}
	after, err := Regenerate(definition, mutated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest == after.Digest {
		t.Fatal("revision digest did not change when cited todo status changed")
	}
	if findControl(after, "E-16").Status != Missing {
		t.Fatalf("mutated E-16 status = %q, want %q", findControl(after, "E-16").Status, Missing)
	}
}

func realRevision(t *testing.T) Revision {
	t.Helper()
	definition, todos := realInputs(t)
	revision, err := Regenerate(definition, todos, nil)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func realInputs(t *testing.T) (Definition, []todoregistry.Todo) {
	t.Helper()
	definition, err := Load(filepath.Join("testdata", "security-control-crosswalk.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..")
	todos, err := LoadRegistry(filepath.Join(root, "definitions", "planning", "todo-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	return definition, todos
}

func findControl(revision Revision, id string) Control {
	for _, control := range revision.Controls {
		if control.ID == id {
			return control
		}
	}
	return Control{}
}

func TestRevisionJSONAndExplainAreDeterministicAndIdentifierFree(t *testing.T) {
	revision := realRevision(t)
	first, err := revision.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	second, err := revision.JSON()
	if err != nil || !bytes.Equal(first, second) || first[len(first)-1] != '\n' {
		t.Fatalf("JSON must be deterministic and newline-terminated: %v", err)
	}
	if !json.Valid(first) {
		t.Fatal("JSON must be valid")
	}
	explain := revision.Explain()
	if !strings.Contains(explain, "controls=51") || !strings.Contains(explain, "digest="+revision.Digest) {
		t.Fatalf("Explain must summarise counts and digest without identifiers, got %q", explain)
	}
	for _, control := range revision.Controls {
		if strings.Contains(explain, control.ID) {
			t.Fatalf("Explain leaks control id %s", control.ID)
		}
	}
	if Explain() == "" {
		t.Fatal("package Explain must describe the contract")
	}
}

func TestFindingAndValidationErrorRenderAndSortDeterministically(t *testing.T) {
	findings := []Finding{
		{ControlID: "E-05", Field: "status", Code: "INVALID", Detail: "b"},
		{ControlID: "E-05", Field: "owner", Code: "MISSING", Detail: "a"},
		{Field: "controls", Code: "EMPTY", Detail: "no controls"},
		{ControlID: "A-01", Field: "status", Code: "INVALID", Detail: "c"},
	}
	sortFindings(findings)
	if findings[0].ControlID != "" || findings[1].ControlID != "A-01" || findings[2].Field != "owner" || findings[3].Field != "status" {
		t.Fatalf("findings must sort by control, field, code: %+v", findings)
	}
	if got := findings[0].Error(); got != "controls: EMPTY: no controls" {
		t.Fatalf("unscoped finding renders %q", got)
	}
	if got := findings[1].Error(); got != "A-01.status: INVALID: c" {
		t.Fatalf("scoped finding renders %q", got)
	}
	empty := &ValidationError{}
	if empty.Error() != "controlcrosswalk: validation failed" {
		t.Fatalf("empty validation error renders %q", empty.Error())
	}
	full := &ValidationError{Findings: findings}
	msg := full.Error()
	if !strings.Contains(msg, "A-01.status: INVALID: c") || !strings.Contains(msg, "controls: EMPTY: no controls") {
		t.Fatalf("validation error must list every finding, got %q", msg)
	}
	var err error = full
	var target *ValidationError
	if !errors.As(err, &target) || len(target.Findings) != 4 {
		t.Fatal("ValidationError must be usable through errors.As")
	}
}
