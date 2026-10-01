package widgetreg

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

func wfpageK030Plan() *workflow.CompiledWorkflow {
	return &workflow.CompiledWorkflow{WorkflowID: "new-hire", Version: 7, Inputs: []workflow.Field{
		{Path: "legal_name", Type: workflow.ValueType{Kind: workflow.KindString}},
		{Path: "headcount", Type: workflow.ValueType{Kind: workflow.KindInteger}},
		{Path: "worker_id", Type: workflow.ValueType{Kind: workflow.KindString, Brand: "PersonID"}},
		{Path: "restricted_note", Type: workflow.ValueType{Kind: workflow.KindString}},
	}}
}

func TestTodo_WFPAGE_030(t *testing.T) {
	registry := NewWorkflowInputRegistry()
	entries := registry.PagePalette(wfpageK030Plan(), map[string]string{
		"legal_name": "internal", "headcount": "internal", "worker_id": "confidential", "restricted_note": "restricted",
	}, "confidential")
	if len(entries) != 4 {
		t.Fatalf("palette entries = %d, want text + integer + person picker variants", len(entries))
	}
	for _, entry := range entries {
		if entry.InputPath == "restricted_note" {
			t.Fatalf("restricted input escaped ceiling: %+v", entry)
		}
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.InputPath+":"+string(entry.Kind)] = true
	}
	for _, want := range []string{"legal_name:text", "headcount:integer", "worker_id:person_picker"} {
		if !seen[want] {
			t.Fatalf("palette omitted %q: %v", want, seen)
		}
	}
	bound, err := registry.BindPageWidget(WorkflowPageBindingRequest{
		Plan: wfpageK030Plan(), PageID: "new-hire", SectionID: "identity", InputPath: "legal_name",
		Kind: pagedef.WorkflowWidgetText, Classification: "internal", Ceiling: "confidential",
		Label: "Legal name", Help: "Use the name on the identity document", Prefill: "person.legal_name", Default: "", ReadOnly: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Widget.Binding != "legal_name" || bound.Widget.Description == "" || bound.Prefill == "" || bound.ReadOnly {
		t.Fatalf("binding inspector projection = %+v", bound)
	}
}

func TestTodo_WFPAGE_030_Browser(t *testing.T) {
	entries := NewWorkflowInputRegistry().PagePalette(wfpageK030Plan(), map[string]string{
		"legal_name": "internal", "headcount": "internal", "worker_id": "confidential",
	}, "confidential")
	for _, entry := range entries {
		if len(entry.OptionKeys) != 5 || entry.Role == "" {
			t.Fatalf("browser palette projection lacks inspector options or semantic role: %+v", entry)
		}
	}
}

func TestTodo_WFPAGE_030_Security(t *testing.T) {
	registry := NewWorkflowInputRegistry()
	base := WorkflowPageBindingRequest{Plan: wfpageK030Plan(), PageID: "new-hire", SectionID: "identity", InputPath: "legal_name", Kind: pagedef.WorkflowWidgetText, Classification: "internal", Ceiling: "internal", Label: "Name"}
	for _, mutation := range []struct {
		name string
		edit func(*WorkflowPageBindingRequest)
	}{
		{"unknown input", func(request *WorkflowPageBindingRequest) { request.InputPath = "not_compiled" }},
		{"restricted input", func(request *WorkflowPageBindingRequest) {
			request.InputPath = "restricted_note"
			request.Classification = "restricted"
		}},
		{"wrong widget", func(request *WorkflowPageBindingRequest) { request.Kind = pagedef.WorkflowWidgetInteger }},
		{"new input", func(request *WorkflowPageBindingRequest) { request.NewInput = "salary" }},
		{"new node", func(request *WorkflowPageBindingRequest) { request.NewNode = "node-2" }},
		{"capability", func(request *WorkflowPageBindingRequest) { request.Capability = "people.read" }},
		{"free html", func(request *WorkflowPageBindingRequest) { request.FreeHTML = "<input>" }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			request := base
			mutation.edit(&request)
			if _, err := registry.BindPageWidget(request); err == nil {
				t.Fatal("forbidden edit was accepted")
			} else if strings.Contains(err.Error(), "not_compiled") == false && mutation.name == "unknown input" {
				t.Fatalf("unknown input error = %v", err)
			}
		})
	}
}
