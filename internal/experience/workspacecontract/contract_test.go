package contract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fixtureSourceRecord returns a SourceRecord with a deliberately sensitive
// field ("nationalId") that no test's visibility map ever allows, plus a
// deliberately unauthorized action ("force_execute") that no test allows
// either. It stands in for what a capability would resolve server-side
// before authorization narrows it -- see the package doc for why this lane
// does not import internal/domains for that data.
func fixtureSourceRecord() SourceRecord {
	return SourceRecord{
		WorkspaceID: "wf-promo-1048",
		Title:       "Promotion: Jane Rivera",
		WorkerID:    "worker-1048",
		WorkerName:  "Jane Rivera",
		AllFields: []RequestField{
			{ID: "currentJobTitle", Label: "Current job title", Kind: FieldKindReadOnly, Value: "Registered Nurse II"},
			{ID: "proposedJobTitle", Label: "Proposed job title", Kind: FieldKindLookup, Value: "Registered Nurse III", Validation: FieldValidation{Required: true}},
			{ID: "proposedCompensation", Label: "Proposed base pay", Kind: FieldKindMoney, Value: "$98,000.00", Validation: FieldValidation{Required: true}},
			{ID: "effectiveDate", Label: "Effective date", Kind: FieldKindDate, Value: "2026-10-01", Validation: FieldValidation{Required: true}},
			{ID: "businessReason", Label: "Business reason", Kind: FieldKindTextarea, Value: "Scope of practice increase; retention risk.", Validation: FieldValidation{Required: true}},
			// Sensitive/out-of-band field: never included in any test's
			// FieldVisibility allowlist below.
			{ID: "nationalId", Label: "National ID", Kind: FieldKindText, Value: "555-11-2222"},
		},
		Preflight: []PreflightFinding{
			{ID: "band-check", Severity: SeveritySuccess, Label: "Compensation band", Detail: "Proposed amount is within the approved band for the target level."},
			{ID: "payroll-cutoff", Severity: SeverityWarning, Label: "Payroll cutoff", Detail: "Effective date falls inside the next payroll window."},
		},
		Simulation: SimulationResult{
			Status:      SimulationReady,
			Summary:     "Simulation completed with no blocking findings.",
			GeneratedAt: time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
			Checks: []SimulationCheck{
				{Label: "Compensation band", Status: SeveritySuccess, Detail: "Within approved band."},
				{Label: "Payroll cutoff", Status: SeverityWarning, Detail: "Inside next payroll window."},
			},
		},
		Timeline: []TimelineEvent{
			{At: time.Date(2026, 9, 1, 9, 5, 0, 0, time.UTC), Actor: "Alex Manager", Label: "Submitted request"},
			{At: time.Date(2026, 9, 1, 10, 20, 0, 0, time.UTC), Actor: "Riley HRBP", Label: "Approved HRBP review"},
		},
		AllActions: []AvailableAction{
			{ID: "approve", Label: "Approve", Transition: "approve", Variant: ActionPrimary},
			{ID: "reject", Label: "Reject", Transition: "reject", Variant: ActionDanger, RequiresReason: true},
			{ID: "request_more_information", Label: "Request info", Transition: "request_more_information", Variant: ActionSecondary},
			// Unauthorized-for-this-actor action: never included in any
			// test's ActionVisibility allowlist below.
			{ID: "force_execute", Label: "Force execute", Transition: "force_execute", Variant: ActionDanger},
		},
		Provenance: Provenance{
			CapabilityID:      "people.promote",
			CapabilityVersion: "v1",
			SourceSystem:      "hcm-next",
			AsOf:              time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
		},
	}
}

func fixtureVisibleFields() FieldVisibility {
	return Allow("currentJobTitle", "proposedJobTitle", "proposedCompensation", "effectiveDate", "businessReason")
}

func fixtureVisibleActions() ActionVisibility {
	return Allow("approve", "reject", "request_more_information")
}

// TestTodo_UX_001 is the primary test for planning/todos.md UX-001: the
// server-resolved Promotion workspace contract contains only field-masked
// data, provenance, validation, available actions, and capability/version
// refs -- a masked field/action is structurally absent, not blanked.
func TestTodo_UX_001(t *testing.T) {
	source := fixtureSourceRecord()
	got := NewWorkspaceContract(source, fixtureVisibleFields(), fixtureVisibleActions())

	if got.WorkspaceID != source.WorkspaceID || got.Title != source.Title {
		t.Fatalf("workspace identity not preserved: %+v", got)
	}

	if len(got.Request.Fields) != 5 {
		t.Fatalf("want 5 visible fields, got %d: %+v", len(got.Request.Fields), got.Request.Fields)
	}

	// Order is preserved from AllFields.
	wantOrder := []string{"currentJobTitle", "proposedJobTitle", "proposedCompensation", "effectiveDate", "businessReason"}
	for i, id := range wantOrder {
		if got.Request.Fields[i].ID != id {
			t.Fatalf("field order mismatch at %d: want %s got %s", i, id, got.Request.Fields[i].ID)
		}
	}

	for _, f := range got.Request.Fields {
		if f.ID == "nationalId" {
			t.Fatalf("masked field nationalId present in contract fields: %+v", got.Request.Fields)
		}
	}

	if len(got.Actions) != 3 {
		t.Fatalf("want 3 visible actions, got %d: %+v", len(got.Actions), got.Actions)
	}
	for _, a := range got.Actions {
		if a.ID == "force_execute" {
			t.Fatalf("unauthorized action force_execute present in contract actions: %+v", got.Actions)
		}
	}

	if got.Provenance.CapabilityID == "" || got.Provenance.CapabilityVersion == "" {
		t.Fatalf("provenance/capability refs missing: %+v", got.Provenance)
	}

	// RED case: masking is structural, not textual. Marshal to JSON (a
	// stand-in for "any channel: HTTP/gRPC/GWC resolve identical semantic
	// results") and assert the masked field/action never appears as a key
	// or value anywhere in the wire representation.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal contract: %v", err)
	}
	text := string(blob)
	for _, needle := range []string{"nationalId", "555-11-2222", "force_execute", "Force execute"} {
		if strings.Contains(text, needle) {
			t.Fatalf("masked value %q leaked into JSON contract: %s", needle, text)
		}
	}

	// Validation metadata for required fields survives the projection.
	for _, f := range got.Request.Fields {
		if f.ID == "proposedJobTitle" && !f.Validation.Required {
			t.Fatalf("expected proposedJobTitle to stay required after masking: %+v", f)
		}
	}
}

// TestTodo_UX_001_Security is the masking-focused companion in this
// package: it asserts MaskedFieldIDs agrees with NewWorkspaceContract about
// what got dropped, for a policy that hides everything.
func TestTodo_UX_001_Security(t *testing.T) {
	source := fixtureSourceRecord()
	noFields := FieldVisibility{}
	noActions := ActionVisibility{}

	got := NewWorkspaceContract(source, noFields, noActions)
	if len(got.Request.Fields) != 0 {
		t.Fatalf("want zero fields when nothing is authorized, got %+v", got.Request.Fields)
	}
	if len(got.Actions) != 0 {
		t.Fatalf("want zero actions when nothing is authorized, got %+v", got.Actions)
	}

	masked := MaskedFieldIDs(source, noFields)
	if len(masked) != len(source.AllFields) {
		t.Fatalf("want all %d fields reported masked, got %d: %v", len(source.AllFields), len(masked), masked)
	}
}

// TestTodo_UX_001_Golden pins the semantic Promotion workspace payload after
// field and action authorization have been applied. It protects the contract
// shape and the server-provided validation, action, provenance, and timestamp
// values independently of either renderer.
func TestTodo_UX_001_Golden(t *testing.T) {
	source := SourceRecord{
		WorkspaceID: "ws-promotion-1",
		Title:       "Promotion: Jane Rivera",
		WorkerID:    "worker-1",
		WorkerName:  "Jane Rivera",
		AllFields: []RequestField{{
			ID: "proposedJobTitle", Label: "Proposed job title", Kind: FieldKindLookup,
			Value: "Registered Nurse III", Validation: FieldValidation{Required: true},
		}},
		Preflight: []PreflightFinding{{
			ID: "band-check", Severity: SeveritySuccess, Label: "Compensation band", Detail: "Within the approved band.",
		}},
		Simulation: SimulationResult{
			Status: SimulationReady, Summary: "Simulation completed.",
			GeneratedAt: time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
			Checks:      []SimulationCheck{{Label: "Compensation band", Status: SeveritySuccess, Detail: "Within band."}},
		},
		Timeline:   []TimelineEvent{{At: time.Date(2026, 9, 1, 9, 5, 0, 0, time.UTC), Actor: "Alex Manager", Label: "Submitted request"}},
		AllActions: []AvailableAction{{ID: "approve", Label: "Approve", Transition: "approve", Variant: ActionPrimary}},
		Provenance: Provenance{
			CapabilityID: "people.promote", CapabilityVersion: "v1", SourceSystem: "hcm-next",
			AsOf: time.Date(2026, 9, 1, 11, 10, 0, 0, time.UTC),
		},
	}
	got, err := json.Marshal(NewWorkspaceContract(source, Allow("proposedJobTitle"), Allow("approve")))
	if err != nil {
		t.Fatalf("marshal Promotion workspace contract: %v", err)
	}
	want := `{"WorkspaceID":"ws-promotion-1","Title":"Promotion: Jane Rivera","Request":{"WorkerID":"worker-1","WorkerName":"Jane Rivera","Fields":[{"ID":"proposedJobTitle","Label":"Proposed job title","Kind":"lookup","Value":"Registered Nurse III","Validation":{"Required":true,"Message":""}}]},"Preflight":[{"ID":"band-check","Severity":"success","Label":"Compensation band","Detail":"Within the approved band."}],"Simulation":{"Status":"ready","Summary":"Simulation completed.","GeneratedAt":"2026-09-01T11:10:00Z","Checks":[{"Label":"Compensation band","Status":"success","Detail":"Within band."}]},"Timeline":[{"At":"2026-09-01T09:05:00Z","Actor":"Alex Manager","Label":"Submitted request"}],"Actions":[{"ID":"approve","Label":"Approve","Transition":"approve","Variant":"primary","RequiresReason":false}],"Provenance":{"CapabilityID":"people.promote","CapabilityVersion":"v1","SourceSystem":"hcm-next","AsOf":"2026-09-01T11:10:00Z"}}`
	if string(got) != want {
		t.Fatalf("Promotion workspace contract changed:\n got: %s\nwant: %s", got, want)
	}
}
