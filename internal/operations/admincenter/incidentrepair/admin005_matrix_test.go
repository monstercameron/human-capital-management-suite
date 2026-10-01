package incidentrepair

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func fixture(state State) Incident {
	return Incident{ID: "i-1", TenantID: "t-1", State: state, Version: 4, Evidence: []Evidence{{ID: "ev-1", TenantID: "t-1"}}}
}
func auth(actions ...Action) Authorization {
	return Authorization{ActorID: "operator", TenantID: "t-1", AllowedActions: actions, ApproverID: "approver", CanCustomerView: true}
}

func TestTodoADMIN005Registry(t *testing.T) {
	r := Registry()
	if r.ID != "ADMIN-005" || r.Version == "" || !r.RedactionRequired || !r.RequiresSoD {
		t.Fatalf("bad registry: %+v", r)
	}
	want := []Action{ActionInspect, ActionTest, ActionRedrive, ActionReconcile, ActionDiff, ActionSimulate, ActionPromote, ActionRollback, ActionReopen, ActionMerge, ActionSplit}
	if len(r.Actions) != len(want) {
		t.Fatalf("actions=%v want=%v", r.Actions, want)
	}
	for i := range want {
		if r.Actions[i] != want[i] {
			t.Fatalf("actions=%v want=%v", r.Actions, want)
		}
	}
}

// TestTodo_ADMIN_005 is the named primary contract test from the delivery
// registry. It exercises the operator path through the lifecycle mutations.
func TestTodo_ADMIN_005(t *testing.T) {
	for _, tc := range []struct {
		state  State
		action Action
		want   State
	}{
		{StateResolved, ActionReopen, StateReopened},
		{StateDeclared, ActionMerge, StateMerged},
		{StateMonitoring, ActionSplit, StateSplit},
	} {
		r := ActionRequest{Action: tc.action, Incident: fixture(tc.state), Authorization: auth(tc.action), ExpectedVersion: 4, Reason: "verified operator action", IdempotencyKey: "primary-" + string(tc.action)}
		p, err := Plan(r)
		if err != nil {
			t.Fatalf("%s: unexpected planning error: %v", tc.action, err)
		}
		if p.From != tc.state || p.To != tc.want || !p.Mutates || !p.RequiresApproval {
			t.Fatalf("%s: plan=%+v", tc.action, p)
		}
	}
}

// TestTodo_ADMIN_005_Mutation proves that the mutation boundary rejects
// stale, unauthorized, self-approved, and evidence-free repair plans.
func TestTodo_ADMIN_005_Mutation(t *testing.T) {
	base := ActionRequest{Action: ActionPromote, Incident: fixture(StateDeclared), Authorization: auth(ActionPromote), ExpectedVersion: 4, Reason: "repair drift", IdempotencyKey: "mutation-1"}
	cases := []struct {
		name string
		edit func(*ActionRequest)
		want error
	}{
		{"stale plan", func(r *ActionRequest) { r.ExpectedVersion = 3 }, ErrStale},
		{"unauthorized", func(r *ActionRequest) { r.Authorization.AllowedActions = nil }, ErrUnauthorized},
		{"self approval", func(r *ActionRequest) { r.Authorization.ApproverID = r.Authorization.ActorID }, ErrSelfApproval},
		{"missing evidence", func(r *ActionRequest) { r.Incident.Evidence = nil }, ErrEvidenceRequired},
	}
	for _, tc := range cases {
		r := base
		r.Authorization.AllowedActions = append([]Action(nil), base.Authorization.AllowedActions...)
		tc.edit(&r)
		if _, err := Plan(r); !errors.Is(err, tc.want) {
			t.Errorf("%s: error=%v, want %v", tc.name, err, tc.want)
		}
	}
}
func TestTodoADMIN005LifecycleReopenMergeSplit(t *testing.T) {
	for _, tc := range []struct {
		s  State
		a  Action
		to State
	}{{StateResolved, ActionReopen, StateReopened}, {StateDeclared, ActionMerge, StateMerged}, {StateMonitoring, ActionSplit, StateSplit}} {
		p, e := Plan(ActionRequest{Action: tc.a, Incident: fixture(tc.s), Authorization: auth(tc.a), ExpectedVersion: 4, Reason: "governed", IdempotencyKey: "k"})
		if e != nil || p.To != tc.to {
			t.Fatalf("%s: plan=%+v err=%v", tc.a, p, e)
		}
	}
}
func TestTodoADMIN005Safety(t *testing.T) {
	base := ActionRequest{Action: ActionPromote, Incident: fixture(StateDeclared), Authorization: auth(ActionPromote), ExpectedVersion: 4, Reason: "x", IdempotencyKey: "k"}
	cases := []struct {
		name   string
		mutate func(*ActionRequest)
		want   error
	}{{"stale", func(r *ActionRequest) { r.ExpectedVersion = 3 }, ErrStale}, {"unauthorized", func(r *ActionRequest) { r.Authorization.AllowedActions = nil }, ErrUnauthorized}, {"self approval", func(r *ActionRequest) { r.Authorization.ApproverID = "operator" }, ErrSelfApproval}, {"missing evidence", func(r *ActionRequest) { r.Incident.Evidence = nil }, ErrEvidenceRequired}}
	for _, tc := range cases {
		r := base
		tc.mutate(&r)
		if _, e := Plan(r); !errors.Is(e, tc.want) {
			t.Errorf("%s err=%v want %v", tc.name, e, tc.want)
		}
	}
}
func TestTodoADMIN005RedactedTimeline(t *testing.T) {
	in := fixture(StateMonitoring)
	in.Timeline = []TimelineEvent{{ID: "safe", TenantID: "t-1", Compartment: "customer"}, {ID: "secret", TenantID: "t-1", Compartment: "security"}, {ID: "other", TenantID: "t-2", Compartment: "customer"}}
	v := View([]Incident{in}, auth(ActionInspect), time.Now())
	if len(v.Incidents) != 1 || len(v.Incidents[0].Timeline) != 1 || v.Incidents[0].Timeline[0].ID != "safe" {
		t.Fatalf("redaction failed: %+v", v)
	}
}

func TestTodo_ADMIN_005_ViewWithheldSerializesSafeProjection(t *testing.T) {
	in := Incident{
		ID: "incident-1", TenantID: "t-1", Service: "payments-internal", State: StateMonitoring,
		Version: 9, Severity: "critical-secret", AffectedKnown: true,
		Evidence: []Evidence{{ID: "e-secret", Kind: "stacktrace", Summary: "private evidence", TenantID: "t-1", Compartment: "security"}},
		Timeline: []TimelineEvent{{ID: "tl-secret", Type: "trace", Summary: "private timeline", TenantID: "t-1", Compartment: "security"}},
	}
	before, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input before view: %v", err)
	}

	got := View([]Incident{in}, Authorization{TenantID: "t-1", AllowedActions: []Action{ActionInspect}}, time.Unix(100, 0))
	if len(got.Incidents) != 1 {
		t.Fatalf("incidents=%d, want 1", len(got.Incidents))
	}
	view := got.Incidents[0]
	if !view.Withheld || view.Incident.ID != in.ID || view.Incident.TenantID != in.TenantID || view.Incident.State != in.State || view.Incident.Version != in.Version {
		t.Fatalf("withheld marker=%+v", view)
	}
	if view.Incident.Service != "" || view.Incident.Severity != "" || view.Incident.AffectedKnown || len(view.Incident.Evidence) != 0 || len(view.Incident.Timeline) != 0 || len(view.Timeline) != 0 || len(view.Actions) != 0 {
		t.Fatalf("withheld projection retains sensitive data: %+v", view)
	}
	serialized, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal withheld view: %v", err)
	}
	for _, marker := range []string{"payments-internal", "critical-secret", "e-secret", "private evidence", "tl-secret", "private timeline", "inspect"} {
		if bytes.Contains(serialized, []byte(marker)) {
			t.Errorf("withheld serialized projection contains %q: %s", marker, serialized)
		}
	}
	after, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input after view: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("View mutated input: before=%s after=%s", before, after)
	}
}

func TestTodo_ADMIN_005_ViewCustomerFiltersWholeProjection(t *testing.T) {
	in := Incident{
		ID: "incident-1", TenantID: "t-1", Service: "payments", State: StateMonitoring, Version: 9,
		Evidence: []Evidence{
			{ID: "e-customer", Summary: "customer evidence", TenantID: "t-1", Compartment: "customer"},
			{ID: "e-empty", Summary: "shared evidence", TenantID: "t-1"},
			{ID: "e-security", Summary: "security evidence", TenantID: "t-1", Compartment: "security"},
			{ID: "e-other", Summary: "other tenant evidence", TenantID: "t-2", Compartment: "customer"},
		},
		Timeline: []TimelineEvent{
			{ID: "tl-customer", Summary: "customer timeline", TenantID: "t-1", Compartment: "customer"},
			{ID: "tl-empty", Summary: "shared timeline", TenantID: "t-1"},
			{ID: "tl-security", Summary: "security timeline", TenantID: "t-1", Compartment: "security"},
			{ID: "tl-other", Summary: "other tenant timeline", TenantID: "t-2", Compartment: "customer"},
		},
	}
	before, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input before view: %v", err)
	}
	a := Authorization{TenantID: "t-1", CanCustomerView: true, AllowedActions: []Action{ActionInspect, ActionRedrive}}
	view := View([]Incident{in}, a, time.Unix(100, 0)).Incidents[0]
	if view.Withheld || len(view.Actions) != 2 || view.Actions[0] != ActionInspect || view.Actions[1] != ActionRedrive {
		t.Fatalf("authorized customer actions=%v withheld=%v", view.Actions, view.Withheld)
	}
	wantEvidence := []string{"e-customer", "e-empty"}
	if len(view.Incident.Evidence) != len(wantEvidence) {
		t.Fatalf("incident evidence=%v, want %v", view.Incident.Evidence, wantEvidence)
	}
	for i, want := range wantEvidence {
		if view.Incident.Evidence[i].ID != want {
			t.Errorf("incident evidence[%d]=%q, want %q", i, view.Incident.Evidence[i].ID, want)
		}
	}
	wantTimeline := []string{"tl-customer", "tl-empty"}
	if len(view.Incident.Timeline) != len(wantTimeline) || len(view.Timeline) != len(wantTimeline) {
		t.Fatalf("incident timeline=%v projected timeline=%v, want %v", view.Incident.Timeline, view.Timeline, wantTimeline)
	}
	for i, want := range wantTimeline {
		if view.Incident.Timeline[i].ID != want || view.Timeline[i].ID != want {
			t.Errorf("timeline[%d]=%q/%q, want %q", i, view.Incident.Timeline[i].ID, view.Timeline[i].ID, want)
		}
	}
	serialized, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal customer view: %v", err)
	}
	for _, marker := range []string{"e-security", "security evidence", "e-other", "other tenant evidence", "tl-security", "security timeline", "tl-other", "other tenant timeline"} {
		if bytes.Contains(serialized, []byte(marker)) {
			t.Errorf("customer serialized projection contains %q: %s", marker, serialized)
		}
	}
	after, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input after view: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("View mutated input: before=%s after=%s", before, after)
	}
}

func TestTodo_ADMIN_005_ViewCrossTenantProjectionPreservesPrivacy(t *testing.T) {
	in := Incident{ID: "foreign-1", TenantID: "t-2", Service: "private-service", State: StateDeclared, Version: 12, Severity: "private-severity", Evidence: []Evidence{{ID: "foreign-evidence", TenantID: "t-2"}}, Timeline: []TimelineEvent{{ID: "foreign-timeline", TenantID: "t-2"}}}
	view := View([]Incident{in}, Authorization{TenantID: "t-1", CanCustomerView: true, AllowedActions: []Action{ActionInspect}}, time.Unix(100, 0)).Incidents[0]
	if !view.Withheld || view.Incident.ID != in.ID || view.Incident.State != in.State || view.Incident.TenantID != "" || view.Incident.Version != 0 || view.Incident.Service != "" || view.Incident.Severity != "" || len(view.Incident.Evidence) != 0 || len(view.Incident.Timeline) != 0 || len(view.Timeline) != 0 || len(view.Actions) != 0 {
		t.Fatalf("cross-tenant projection changed: %+v", view)
	}
}
