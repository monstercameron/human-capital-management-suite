package userflow

import (
	"encoding/json"
	"reflect"
	"testing"
)

func validRecord() UserFlowRecord {
	return UserFlowRecord{
		FlowID: "UF-001", Version: "1", Status: string(FlowDraft), Owner: "experience", Title: "Submit leave",
		JobToBeDone: "request leave", SuccessDefinition: "request is accepted", RootBusinessIntent: "CreateLeaveRequest",
		ChildBusinessIntents: []string{"AttachLeaveEvidence"}, References: SemanticReferences{BusinessIntent: "CreateLeaveRequest", Workflow: "LeaveWorkflow", VerticalSlice: "LeaveSlice", Action: "CreateLeaveRequest", Result: "LeaveRequestCreated"},
		PrimaryParticipant: Participant{ID: "employee", Role: "HUMAN_SELF", Persona: "employee requestor", Relationship: "subject", DecisionRights: "submit leave request", Representation: "self", Delegation: "none"},
		OtherParticipants:  []Participant{{ID: "manager", Role: "MANAGER", Persona: "line manager", Relationship: "approver", DecisionRights: "approve or decline", Representation: "self", Delegation: "none"}},
		IdentityAssurance:  "session-bound", SessionAssumptions: "active authenticated session",
		Entry:    EntryPaths{Entry: "portal", Discovery: "inbox", Resume: "task", DeepLink: "task link", Notification: "secure inbox"},
		Surfaces: []string{"GUIDED_FORM"}, Channels: []string{"web"}, Devices: []string{"desktop", "mobile"}, Preconditions: "active employment", UnavailableAction: "explain unmet prerequisite",
		VisibleFacts: "requested dates and current leave balance", Provenance: "balance from authoritative leave ledger", Freshness: "balance timestamp shown",
		MaskedFacts: "medical details masked", HiddenFacts: "unrelated personnel facts hidden", SummaryOnlyFacts: "restricted evidence summarized",
		RequestedInput: "requested dates", ServerResolvedTruth: "eligibility and available balance", Forms: "leave request form", Documents: "optional supporting document", EvidenceCompartments: "restricted medical evidence compartment",
		Stages:       []FlowStage{{ID: "s1", Stage: Collect, ParticipantGoal: "provide dates", Surface: "GUIDED_FORM", SystemState: "draft", ParticipantVisibleState: "Draft saved", AvailableActions: "save or continue", Input: "requested dates", Validation: "dates must be valid", CapabilityTransition: "CreateLeaveRequest", SemanticActionReference: "CreateLeaveRequest", SemanticResultReference: "LeaveRequestDrafted", VisibleResult: "draft saved", Evidence: "draft receipt", Decision: "no decision at this stage", ErrorRecovery: "correct dates or resume draft"}},
		StateMatrix:  []StatePresentation{{State: "Loading", Understand: "what is resolving", Behavior: "bounded progress and safe retry"}, {State: "Failed", Understand: "what failed", Behavior: "show cause and recovery"}},
		Cancellation: "cancel before approval", Correction: "submit correction intent", Supersession: "new request supersedes prior request", Completion: "show decision and retained evidence", FollowUp: "notify requester and manager",
		Locale: "locale-aware names, dates, money and RTL", Accessibility: "keyboard, screen reader and contrast support", Accommodation: "assisted/manual submission route", Responsive: "mobile, kiosk and desktop layouts", Offline: "preserve draft and explain unavailable submit", Privacy: "least disclosure and purpose limits", Safety: "no hidden disclosure of medical details",
		AnalyticsEvents: "record stage transitions without sensitive values", ProhibitedTelemetry: "raw medical details and requested dates",
		Scenarios: []string{"valid request", "invalid dates", "manager declines"}, Oracles: []string{"exact draft state and enabled actions", "no restricted evidence disclosure"},
		TodoLinks: []string{"UXFLOW-001"}, Evidence: []string{"unit:valid-record"}, Phase: "MAX-v1", MaximalConfiguration: "all supported participant and channel variants",
	}
}

func cloneRecord(t *testing.T, source UserFlowRecord) UserFlowRecord {
	t.Helper()
	b, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone UserFlowRecord
	if err := json.Unmarshal(b, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func TestUserFlowRecordRejectsMissingParticipantStateRecoveryOrSemanticReference(t *testing.T) {
	cases := []struct {
		name string
		edit func(*UserFlowRecord)
	}{
		{"participant", func(r *UserFlowRecord) { r.PrimaryParticipant.ID = "" }},
		{"state", func(r *UserFlowRecord) { r.StateMatrix = nil }},
		{"recovery", func(r *UserFlowRecord) { r.Stages[0].ErrorRecovery = "" }},
		{"semantic reference", func(r *UserFlowRecord) { r.References.Workflow = "" }},
		{"stage vocabulary", func(r *UserFlowRecord) { r.Stages[0].Stage = "UNKNOWN" }},
		{"stage action reference", func(r *UserFlowRecord) { r.Stages[0].SemanticActionReference = "" }},
		{"visible state", func(r *UserFlowRecord) { r.Stages[0].ParticipantVisibleState = "" }},
		{"oracle", func(r *UserFlowRecord) { r.Oracles = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := cloneRecord(t, validRecord())
			tc.edit(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestTodo_UXFLOW_001_Property(t *testing.T) {
	r := validRecord()
	bytes1, err := CanonicalBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip UserFlowRecord
	if err := json.Unmarshal(bytes1, &roundTrip); err != nil {
		t.Fatal(err)
	}
	bytes2, err := CanonicalBytes(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bytes1, bytes2) {
		t.Fatalf("canonical JSON changed after round trip:\n%s\n%s", bytes1, bytes2)
	}
	d1, err := r.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := roundTrip.CanonicalDigest()
	if err != nil || d1 != d2 {
		t.Fatalf("canonical digest changed: %s != %s (%v)", d1, d2, err)
	}
	stages := Stages()
	stages[0] = "MUTATED"
	if !Stages()[0].Valid() {
		t.Fatal("caller mutated the shared stage vocabulary")
	}
}

func TestTodo_UXFLOW_001_Golden(t *testing.T) {
	want := []Stage{Discover, Orient, Collect, Validate, Simulate, Compare, Confirm, Submit, Review, Wait, Track, Replan, Execute, Reconcile, Repair, Complete, Correct, Appeal}
	if got := Stages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stage vocabulary/order changed: %#v", got)
	}
	b, err := CanonicalBytes(validRecord())
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"flow_id", "root_business_intent", "state_presentation_matrix", "maximal_configuration_applicability", "not_applicable"} {
		if _, ok := wire[key]; !ok {
			t.Fatalf("canonical wire format missing %q", key)
		}
	}
	if string(b) == "" {
		t.Fatal("canonical representation is empty")
	}
	digest, err := validRecord().CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "a9eee58ab378229ff92da253f8d65219a2621b7d37fcaa6f1fd8c5a62367c316"
	if digest != wantDigest {
		t.Fatalf("canonical record changed: got %s, want %s", digest, wantDigest)
	}
}

func TestTodo_UXFLOW_001_Security(t *testing.T) {
	r := validRecord()
	r.NotApplicable = map[string]NotApplicable{"offline_behavior": {Reason: "offline submission is unsupported", Owner: "experience"}}
	r.Offline = ""
	if err := r.Validate(); err != nil {
		t.Fatalf("owned NOT_APPLICABLE rejected: %v", err)
	}
	r.NotApplicable["offline_behavior"] = NotApplicable{Reason: "offline submission is unsupported"}
	if err := r.Validate(); err == nil {
		t.Fatal("NOT_APPLICABLE without owner accepted")
	}
	r = validRecord()
	r.NotApplicable = map[string]NotApplicable{"unmodeled_field": {Reason: "unmodeled", Owner: "experience"}}
	if err := r.Validate(); err == nil {
		t.Fatal("NOT_APPLICABLE for unknown field accepted")
	}
	r = validRecord()
	r.Status = string(FlowContracted)
	r.OpenFindings = []string{"unknown authority result"}
	if err := r.Validate(); err == nil {
		t.Fatal("contracted flow with open finding accepted")
	}
	r = validRecord()
	r.ProhibitedTelemetry = "raw medical details"
	if err := r.Validate(); err != nil {
		t.Fatalf("prohibited telemetry must be representable: %v", err)
	}
}

func TestTodo_UXFLOW_001_Conformance(t *testing.T) {
	if err := validRecord().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, stage := range Stages() {
		if !stage.Valid() {
			t.Fatalf("vocabulary stage invalid: %s", stage)
		}
	}
	for _, status := range []Status{FlowCandidate, FlowDraft, FlowContracted, FlowImplemented, FlowVerified} {
		if !status.Valid() {
			t.Fatalf("vocabulary status invalid: %s", status)
		}
	}
	r := validRecord()
	r.Status = string(FlowContracted)
	r.OpenFindings = nil
	if err := r.Validate(); err != nil {
		t.Fatalf("resolved contracted flow rejected: %v", err)
	}
}

func TestTodo_UXFLOW_001_Mutation(t *testing.T) {
	cases := []func(*UserFlowRecord){
		func(r *UserFlowRecord) { r.FlowID = "" },
		func(r *UserFlowRecord) { r.Stages[0].ID = "" },
		func(r *UserFlowRecord) { r.PrimaryParticipant.ID = "" },
		func(r *UserFlowRecord) { r.Status = "UNKNOWN" },
		func(r *UserFlowRecord) {
			r.NotApplicable = map[string]NotApplicable{"privacy": {Reason: "no reason owner"}}
		},
	}
	for i, mutate := range cases {
		r := cloneRecord(t, validRecord())
		mutate(&r)
		if err := r.Validate(); err == nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
}
