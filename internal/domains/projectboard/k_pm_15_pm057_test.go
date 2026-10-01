package projectboard

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

func TestTodo_PM_057(t *testing.T) {
	for _, kind := range []PresetKind{PresetFeature, PresetBug, PresetDiscovery} {
		preset, err := Preset(kind)
		if err != nil {
			t.Fatalf("preset %q: %v", kind, err)
		}
		if !preset.EditableDefaults || len(preset.Workflow.TaskTypes) != 1 || len(preset.Views) != 1 {
			t.Fatalf("preset %q is not a common-model editable configuration: %+v", kind, preset)
		}
		if errs := projectworkflow.Validate(preset.Workflow); len(errs) != 0 {
			t.Fatalf("preset %q workflow invalid: %v", kind, errs)
		}
	}
}

func TestTodo_PM_057_Browser(t *testing.T) {
	for _, kind := range []PresetKind{PresetFeature, PresetBug, PresetDiscovery} {
		preset, err := Preset(kind)
		if err != nil {
			t.Fatal(err)
		}
		if preset.Label == "" || preset.Views[0].Name == "" || preset.Views[0].Columns[0].Label == "" {
			t.Fatalf("preset %q has an unlabeled view projection: %+v", kind, preset.Views[0])
		}
		if preset.Views[0].ID == "release" || preset.Views[0].ID == "scrum" {
			t.Fatalf("preset %q claims unsupported board behavior: %q", kind, preset.Views[0].ID)
		}
	}
}

func TestTodo_PM_057_Conformance(t *testing.T) {
	if _, err := Preset(PresetKind("SCRUM")); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("unsupported method error = %v", err)
	}
	preset, err := Preset(PresetFeature)
	if err != nil {
		t.Fatal(err)
	}
	preset.Views[0].Columns[0].Label = "Customer-ready"
	if err := ValidatePreset(preset); err != nil {
		t.Fatalf("editable view label rejected: %v", err)
	}
	if preset.Workflow.Fields[1].Type != projectworkflow.FieldText || preset.Workflow.Fields[2].Type != projectworkflow.FieldEnum {
		t.Fatalf("typed fields lost their common-model types: %+v", preset.Workflow.Fields)
	}
}
