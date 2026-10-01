package projectboard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

var ErrUnknownPreset = errors.New("projectboard: unknown project preset")

type PresetKind string

const (
	PresetFeature   PresetKind = "FEATURE"
	PresetBug       PresetKind = "BUG"
	PresetDiscovery PresetKind = "DISCOVERY"
)

// BoardPreset is an editable starting configuration over the ordinary task
// and view model. It is not a second task engine and contains no Scrum or
// deployment semantics.
type BoardPreset struct {
	Kind             PresetKind
	Label            string
	Description      string
	Workflow         projectworkflow.Config
	Views            []BoardView
	EditableDefaults bool
}

func Preset(kind PresetKind) (BoardPreset, error) {
	var preset BoardPreset
	switch kind {
	case PresetFeature:
		preset = featurePreset()
	case PresetBug:
		preset = bugPreset()
	case PresetDiscovery:
		preset = discoveryPreset()
	default:
		return BoardPreset{}, fmt.Errorf("%w: %q", ErrUnknownPreset, kind)
	}
	if err := ValidatePreset(preset); err != nil {
		return BoardPreset{}, err
	}
	return preset, nil
}

func ValidatePreset(preset BoardPreset) error {
	if preset.Kind != PresetFeature && preset.Kind != PresetBug && preset.Kind != PresetDiscovery {
		return ErrUnknownPreset
	}
	if strings.TrimSpace(preset.Label) == "" || strings.TrimSpace(preset.Description) == "" || !preset.EditableDefaults || len(preset.Views) == 0 {
		return fmt.Errorf("%w: preset metadata is incomplete", ErrUnknownPreset)
	}
	if errs := projectworkflow.Validate(preset.Workflow); len(errs) != 0 {
		return errs
	}
	for _, view := range preset.Views {
		if err := Validate(view); err != nil {
			return err
		}
	}
	return nil
}

func featurePreset() BoardPreset {
	return presetWithFields(PresetFeature, "Features", "Feature work with acceptance and area fields.", "feature", "Acceptance criteria", "acceptance", projectworkflow.FieldText, "Area", "area", projectworkflow.FieldEnum, []string{"product", "operations", "platform"})
}

func bugPreset() BoardPreset {
	return presetWithFields(PresetBug, "Bugs", "Bug work with severity, reproduction and environment fields.", "bug", "Reproduction steps", "reproduction", projectworkflow.FieldText, "Severity", "severity", projectworkflow.FieldEnum, []string{"low", "medium", "high", "urgent"})
}

func discoveryPreset() BoardPreset {
	return presetWithFields(PresetDiscovery, "Discovery", "Discovery work with a question, evidence and decision fields.", "discovery", "Research question", "question", projectworkflow.FieldText, "Decision", "decision", projectworkflow.FieldEnum, []string{"unknown", "validated", "rejected"})
}

func presetWithFields(kind PresetKind, label, description, taskTypeID, textName, textID string, textType projectworkflow.FieldType, enumName, enumID string, enumType projectworkflow.FieldType, options []string) BoardPreset {
	statuses := []projectworkflow.Status{
		{ID: "planned", Name: "Planned", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"active", "cancelled"}},
		{ID: "active", Name: "In progress", Category: projectworkflow.CategoryActive, AllowedNextStatusIDs: []string{"blocked", "done", "cancelled"}},
		{ID: "blocked", Name: "Blocked", Category: projectworkflow.CategoryBlocked, AllowedNextStatusIDs: []string{"active", "cancelled"}},
		{ID: "done", Name: "Done", Category: projectworkflow.CategoryDone},
		{ID: "cancelled", Name: "Cancelled", Category: projectworkflow.CategoryCancelled},
	}
	workflow := projectworkflow.Config{
		TaskTypes:   []projectworkflow.TaskType{{ID: taskTypeID, Name: label[:len(label)-1], FieldIDs: []string{"owner", textID, enumID}, InitialStatus: "planned", RequiredFields: []string{"owner"}}},
		Statuses:    statuses,
		Transitions: []projectworkflow.Transition{{From: "planned", To: "active"}, {From: "planned", To: "cancelled"}, {From: "active", To: "blocked"}, {From: "active", To: "done"}, {From: "active", To: "cancelled"}, {From: "blocked", To: "active"}, {From: "blocked", To: "cancelled"}},
		Fields: []projectworkflow.Field{
			{ID: "owner", Name: "Owner", Type: projectworkflow.FieldPerson, Classification: "INTERNAL"},
			{ID: textID, Name: textName, Type: textType, Classification: "INTERNAL"},
			{ID: enumID, Name: enumName, Type: enumType, Classification: "INTERNAL", Validation: projectworkflow.FieldValidation{Options: options}},
		},
		Columns: []projectworkflow.Column{{ID: "planned", Name: "Planned", StatusIDs: []string{"planned"}}, {ID: "active", Name: "In progress", StatusIDs: []string{"active", "blocked"}}, {ID: "complete", Name: "Complete", StatusIDs: []string{"done", "cancelled"}}},
	}
	view := BoardView{ID: string(kind) + "-board", Name: label + " board", Version: 1, Audience: AudienceProject, Columns: []Column{{ID: "planned", Label: "Planned", StatusIDs: []string{"planned"}}, {ID: "active", Label: "In progress", StatusIDs: []string{"active", "blocked"}}, {ID: "complete", Label: "Complete", StatusIDs: []string{"done", "cancelled"}}}, Grouping: Grouping{Kind: GroupAssignee}, OrderBy: OrderTaskID, CardFields: []string{CardTitle, CardStatus, CardAssignee, CardPriority, CardType, CardDueDate}}
	return BoardPreset{Kind: kind, Label: label, Description: description, Workflow: workflow, Views: []BoardView{view}, EditableDefaults: true}
}
