package widgetreg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// WorkflowPagePaletteEntry is one control the page author may choose for a
// compiled input. It contains no node, capability, or renderer source: the
// compiled workflow remains the only input authority.
type WorkflowPagePaletteEntry struct {
	InputPath      string
	InputType      workflow.ValueType
	Classification string
	Kind           pagedef.WorkflowWidgetKind
	Role           string
	OptionKeys     []string
}

// WorkflowPageBindingRequest is the bounded edit surface for the binding
// inspector. The refusal fields make accidental expansion of the page schema
// explicit instead of silently accepting browser-provided workflow material.
type WorkflowPageBindingRequest struct {
	Plan           *workflow.CompiledWorkflow
	PageID         string
	SectionID      string
	InputPath      string
	Kind           pagedef.WorkflowWidgetKind
	Classification string
	Ceiling        string
	Label          string
	Help           string
	Prefill        string
	Default        string
	ReadOnly       bool
	NewInput       string
	NewNode        string
	Capability     string
	FreeHTML       string
}

// WorkflowPageBinding is the inspector's immutable projection. Options are
// presentation metadata; they never become workflow inputs or executable
// page content.
type WorkflowPageBinding struct {
	Widget   pagedef.WorkflowPageWidget
	Prefill  string
	Default  string
	ReadOnly bool
}

var workflowPageOptionKeys = []string{"label", "help", "prefill", "read_only", "default"}

// PagePalette lists only compatible controls for existing compiled inputs at
// or below the supplied classification ceiling.
func (r WorkflowInputRegistry) PagePalette(plan *workflow.CompiledWorkflow, classifications map[string]string, ceiling string) []WorkflowPagePaletteEntry {
	if plan == nil || !classificationRank(ceiling).ok {
		return nil
	}
	ceilingRank := classificationRank(ceiling).rank
	entries := make([]WorkflowPagePaletteEntry, 0)
	for _, field := range plan.Inputs {
		classification := strings.ToLower(strings.TrimSpace(classifications[field.Path]))
		fieldRank := classificationRank(classification)
		if !fieldRank.ok || fieldRank.rank > ceilingRank {
			continue
		}
		for _, registration := range r.widgets {
			if !pageWidgetCompatible(plan, field.Path, registration.Kind) {
				continue
			}
			entries = append(entries, WorkflowPagePaletteEntry{
				InputPath: field.Path, InputType: field.Type, Classification: classification,
				Kind: registration.Kind, Role: registration.Role, OptionKeys: append([]string(nil), workflowPageOptionKeys...),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].InputPath != entries[j].InputPath {
			return entries[i].InputPath < entries[j].InputPath
		}
		return entries[i].Kind < entries[j].Kind
	})
	return entries
}

// BindPageWidget validates an inspector edit against the same page-definition
// mapping used by ValidateAgainst. A binding can point only at an existing
// compiled input and a closed widget kind.
func (r WorkflowInputRegistry) BindPageWidget(request WorkflowPageBindingRequest) (WorkflowPageBinding, error) {
	if request.Plan == nil {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: compiled workflow is required")
	}
	if strings.TrimSpace(request.NewInput) != "" || strings.TrimSpace(request.NewNode) != "" ||
		strings.TrimSpace(request.Capability) != "" || strings.TrimSpace(request.FreeHTML) != "" {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: page edits cannot add inputs, nodes, capabilities, or HTML")
	}
	field, found := compiledInput(request.Plan, request.InputPath)
	if !found {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: binding %q is not a compiled workflow input", request.InputPath)
	}
	if rank := classificationRank(request.Classification); !rank.ok {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: unknown input classification %q", request.Classification)
	} else if ceiling := classificationRank(request.Ceiling); !ceiling.ok || rank.rank > ceiling.rank {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: input classification %q exceeds page ceiling %q", request.Classification, request.Ceiling)
	}
	_, found = r.Lookup(request.Kind)
	if !found || !pageWidgetCompatible(request.Plan, field.Path, request.Kind) {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: widget %q cannot represent compiled input %q", request.Kind, request.InputPath)
	}
	if strings.TrimSpace(request.PageID) == "" || strings.TrimSpace(request.SectionID) == "" || strings.TrimSpace(request.Label) == "" {
		return WorkflowPageBinding{}, fmt.Errorf("widgetreg: page, section, and label are required")
	}
	return WorkflowPageBinding{Widget: pagedef.WorkflowPageWidget{
		ID: request.PageID + "." + request.InputPath, SectionID: request.SectionID, Column: 1,
		Kind: request.Kind, Binding: field.Path, Type: field.Type.String(), Label: request.Label,
		Description: request.Help, Required: !field.Type.Nullable,
	}, Prefill: request.Prefill, Default: request.Default, ReadOnly: request.ReadOnly}, nil
}

type classificationResult struct {
	rank int
	ok   bool
}

func classificationRank(value string) classificationResult {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public":
		return classificationResult{rank: 1, ok: true}
	case "internal":
		return classificationResult{rank: 2, ok: true}
	case "confidential":
		return classificationResult{rank: 3, ok: true}
	case "restricted":
		return classificationResult{rank: 4, ok: true}
	default:
		return classificationResult{}
	}
}

func compiledInput(plan *workflow.CompiledWorkflow, path string) (workflow.Field, bool) {
	for _, field := range plan.Inputs {
		if field.Path == path {
			return field, true
		}
	}
	return workflow.Field{}, false
}

func pageWidgetCompatible(plan *workflow.CompiledWorkflow, path string, kind pagedef.WorkflowWidgetKind) bool {
	field, found := compiledInput(plan, path)
	if !found {
		return false
	}
	definition := pagedef.WorkflowPageDefinition{
		Schema: pagedef.WorkflowPageSchema, SchemaVersion: pagedef.WorkflowPageSchemaVersion,
		WorkflowKey: plan.WorkflowID, WorkflowVersion: plan.Version, PageVersion: 1, PageID: "preview",
		Sections:      []pagedef.WorkflowPageSection{{ID: "main", Label: "Main", Columns: 1}},
		Widgets:       []pagedef.WorkflowPageWidget{{ID: "preview." + path, SectionID: "main", Column: 1, Kind: kind, Binding: path, Type: field.Type.String(), Label: "Preview"}},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"main"}, LiveRegion: pagedef.LiveRegionOff},
	}
	for _, violation := range definition.ValidateAgainst(plan) {
		if strings.HasSuffix(violation.Path, ".kind") {
			return false
		}
	}
	return true
}
