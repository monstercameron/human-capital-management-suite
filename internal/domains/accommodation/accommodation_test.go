package accommodation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	leave "github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_CAP_006 proves the accommodation interactive process: typed
// request, dialogue, options, decision and review records with sealed
// integrity, lifecycle ordering, reception of the return-to-work follow-up,
// and the whole surface behind registered capabilities.
func TestTodo_WF_CAP_006(t *testing.T) {
	t.Run("Lifecycle", func(t *testing.T) {
		req, err := NewRequest("case-1", "worker-1", 100,
			[]string{"SCHEDULE_LIMIT", "SIT_LIMIT"}, "lumbar strain", "evidence:med-1")
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		policy, err := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
		if err != nil {
			t.Fatalf("NewOpenRunAccess: %v", err)
		}
		walled, err := policy.Wall(
			runtime.RunParticipant{Principal: "worker-1", Role: "WORKER"},
			runtime.RunParticipant{Principal: "manager-1", Role: "MANAGER"},
			runtime.RunParticipant{Principal: "hr-1", Role: "HR_PARTNER"},
		)
		if err != nil {
			t.Fatalf("Wall: %v", err)
		}
		cs, err := OpenCase(req, walled)
		if err != nil {
			t.Fatalf("OpenCase: %v", err)
		}
		if err := cs.RecordDialogue(DialogueEntry{ID: "d1", At: 110, AuthorRole: "HR_PARTNER", Author: "hr-1",
			Classification: EntryOperational, Body: "intake meeting held"}); err != nil {
			t.Fatalf("RecordDialogue: %v", err)
		}
		if err := cs.RecordDialogue(DialogueEntry{ID: "d2", At: 120, AuthorRole: "MEDICAL_REVIEWER", Author: "med-1",
			Classification: EntryMedical, Body: "restrictions confirmed"}); err != nil {
			t.Fatalf("RecordDialogue medical: %v", err)
		}
		opt1, err := NewOption("o1", "case-1", "adjusted schedule", true, false, "")
		if err != nil {
			t.Fatalf("NewOption: %v", err)
		}
		if err := cs.ConsiderOption(opt1); err != nil {
			t.Fatalf("ConsiderOption: %v", err)
		}
		opt2, err := NewOption("o2", "case-1", "full remote", false, true, "onsite role with no remote precedent")
		if err != nil {
			t.Fatalf("NewOption rejected: %v", err)
		}
		if err := cs.ConsiderOption(opt2); err != nil {
			t.Fatalf("ConsiderOption rejected: %v", err)
		}
		dec, err := NewDecision("dec-1", "case-1", DecisionPartial,
			[]string{"SCHEDULE_LIMIT"}, 200, 0, "adjusted schedule granted; remote denied", 150)
		if err != nil {
			t.Fatalf("NewDecision: %v", err)
		}
		if err := cs.Decide(dec); err != nil {
			t.Fatalf("Decide: %v", err)
		}
		rev, err := NewReview("r1", "case-1", 400)
		if err != nil {
			t.Fatalf("NewReview: %v", err)
		}
		if err := cs.RecordReview(rev); err != nil {
			t.Fatalf("RecordReview: %v", err)
		}
		if !rev.Overdue(401) || rev.Overdue(399) {
			t.Fatal("Overdue is wrong on either side of the due tick")
		}
		done, err := rev.Complete(ReviewContinue, 390, 800)
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if err := cs.CompleteReview(done); err != nil {
			t.Fatalf("CompleteReview: %v", err)
		}
		if done.Overdue(900) {
			t.Fatal("completed review reports overdue")
		}
		for _, v := range []interface{ Verify() error }{cs.Request, cs.Options[0], cs.Options[1], *cs.Decision, cs.Reviews[0]} {
			if err := v.Verify(); err != nil {
				t.Fatalf("seal broken: %v", err)
			}
		}
		mut := *cs.Decision
		mut.Rationale = "rewritten"
		if mut.Verify() == nil {
			t.Fatal("mutated decision still verifies")
		}
	})

	t.Run("OrderingEnforced", func(t *testing.T) {
		open := func() *Case {
			req, err := NewRequest("case-o", "worker-1", 100, []string{"SIT_LIMIT"}, "", "")
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			policy, _ := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
			walled, _ := policy.Wall(runtime.RunParticipant{Principal: "worker-1", Role: "WORKER"})
			cs, err := OpenCase(req, walled)
			if err != nil {
				t.Fatalf("OpenCase: %v", err)
			}
			return cs
		}
		dec, _ := NewDecision("d", "case-o", DecisionGranted, []string{"SIT_LIMIT"}, 200, 0, "ok", 150)
		if err := open().Decide(dec); err == nil {
			t.Fatal("decision without dialogue accepted")
		}
		cs := open()
		_ = cs.RecordDialogue(DialogueEntry{ID: "d1", At: 110, AuthorRole: "WORKER", Author: "worker-1",
			Classification: EntryOperational, Body: "request discussed"})
		if err := cs.Decide(dec); err == nil {
			t.Fatal("decision without considered options accepted")
		}
		rev, _ := NewReview("r1", "case-o", 400)
		if err := open().RecordReview(rev); err == nil {
			t.Fatal("review before decision accepted")
		}
		if _, err := NewRequest("x", "w", 1, []string{"NOPE"}, "", ""); err == nil {
			t.Fatal("unknown limitation code accepted")
		}
		if _, err := NewRequest("x", "w", 1, []string{"SIT_LIMIT"}, "flu", ""); err == nil {
			t.Fatal("diagnosis without evidence provenance accepted")
		}
		if _, err := NewOption("o", "c", "x", false, false, ""); err == nil {
			t.Fatal("rejected option without reason accepted")
		}
		if _, err := NewDecision("d", "c", "MAYBE", nil, 1, 0, "r", 2); err == nil {
			t.Fatal("unknown decision outcome accepted")
		}
	})

	t.Run("StoreRejectsTamperedRecords", func(t *testing.T) {
		build := func(t *testing.T) (*Store, Case) {
			t.Helper()
			req, _ := NewRequest("case-t", "worker-1", 100, []string{"SIT_LIMIT"}, "", "")
			policy, _ := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
			walled, _ := policy.Wall(runtime.RunParticipant{Principal: "worker-1", Role: "WORKER"})
			cs, err := OpenCase(req, walled)
			if err != nil {
				t.Fatalf("OpenCase: %v", err)
			}
			_ = cs.RecordDialogue(DialogueEntry{ID: "d1", At: 110, AuthorRole: "WORKER", Author: "worker-1",
				Classification: EntryOperational, Body: "hello"})
			opt, _ := NewOption("o1", "case-t", "chair", true, false, "")
			_ = cs.ConsiderOption(opt)
			dec, _ := NewDecision("dec-1", "case-t", DecisionGranted, []string{"SIT_LIMIT"}, 200, 0, "granted", 150)
			_ = cs.Decide(dec)
			rev, _ := NewReview("r1", "case-t", 400)
			_ = cs.RecordReview(rev)
			store := NewStore()
			if err := store.Create(*cs); err != nil {
				t.Fatalf("Create: %v", err)
			}
			return store, *cs
		}
		tampers := []struct {
			name string
			mut  func(*Case)
		}{
			{"Request", func(c *Case) { c.Request.Limitations[0] = "REMOTE_WORK" }},
			{"DialogueBody", func(c *Case) { c.Dialogue[0].Body = "rewritten" }},
			{"DialogueOrder", func(c *Case) {
				_ = c.RecordDialogue(DialogueEntry{ID: "d2", At: 120, AuthorRole: "WORKER", Author: "worker-1",
					Classification: EntryOperational, Body: "second"})
				c.Dialogue[0], c.Dialogue[1] = c.Dialogue[1], c.Dialogue[0]
			}},
			{"Option", func(c *Case) { c.Options[0].Description = "rewritten" }},
			{"DecisionFlip", func(c *Case) { c.Decision.Outcome = DecisionDenied }},
			{"DecisionRationale", func(c *Case) { c.Decision.Rationale = "rewritten" }},
			{"Review", func(c *Case) { c.Reviews[0].DueAt = 999 }},
		}
		for _, tc := range tampers {
			store, cs := build(t)
			tc.mut(&cs)
			if err := store.Update(cs); err == nil {
				t.Errorf("%s tamper accepted by Update", tc.name)
			}
			if err := cs.Verify(); err == nil {
				t.Errorf("%s tamper accepted by Verify", tc.name)
			}
		}
	})

	t.Run("StoreCopiesOnEveryBoundary", func(t *testing.T) {
		req, _ := NewRequest("case-a", "worker-1", 100, []string{"SIT_LIMIT"}, "", "")
		policy, _ := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
		walled, _ := policy.Wall(runtime.RunParticipant{Principal: "worker-1", Role: "WORKER"})
		cs, _ := OpenCase(req, walled)
		_ = cs.RecordDialogue(DialogueEntry{ID: "d1", At: 110, AuthorRole: "WORKER", Author: "worker-1",
			Classification: EntryOperational, Body: "hello"})
		opt, _ := NewOption("o1", "case-a", "chair", true, false, "")
		_ = cs.ConsiderOption(opt)
		dec, _ := NewDecision("dec-1", "case-a", DecisionGranted, []string{"SIT_LIMIT"}, 200, 0, "granted", 150)
		_ = cs.Decide(dec)
		store := NewStore()
		if err := store.Create(*cs); err != nil {
			t.Fatalf("Create: %v", err)
		}
		// Mutating the opened value must not touch the store (Create cloned).
		cs.Decision.Rationale = "poisoned"
		cs.Dialogue[0].Body = "poisoned"
		cs.Options[0].Description = "poisoned"
		got, ok := store.Get("case-a")
		if !ok {
			t.Fatal("Get failed")
		}
		if got.Decision.Rationale != "granted" || got.Dialogue[0].Body != "hello" || got.Options[0].Description != "chair" {
			t.Fatalf("Create aliased caller state: %+v", got.Decision)
		}
		// Mutating a Get result must not touch the store (Get clones).
		got.Decision.Rationale = "poisoned"
		got.Dialogue[0].Body = "poisoned"
		again, ok := store.Get("case-a")
		if !ok {
			t.Fatal("Get failed")
		}
		if again.Decision.Rationale != "granted" || again.Dialogue[0].Body != "hello" {
			t.Fatal("Get aliased stored state")
		}
	})

	t.Run("ReceivesReturnToWorkFollowUp", func(t *testing.T) {
		if FollowUpKindAccommodation != leave.FollowUpAccommodation {
			t.Fatalf("follow-up kind drifted: %q vs %q", FollowUpKindAccommodation, leave.FollowUpAccommodation)
		}
		req, err := RequestFromFollowUp(leave.FollowUpIntent{Kind: leave.FollowUpAccommodation, Reason: "accommodation_review"},
			"worker-7", "case-7", 500, []string{"STAND_LIMIT"})
		if err != nil {
			t.Fatalf("RequestFromFollowUp: %v", err)
		}
		if req.WorkerID != "worker-7" || req.ID != "case-7" {
			t.Fatalf("request = %+v", req)
		}
		_, err = RequestFromFollowUp(leave.FollowUpIntent{Kind: leave.FollowUpReassignment}, "w", "c", 1, nil)
		if err == nil {
			t.Fatal("non-accommodation follow-up received")
		}
	})

	t.Run("CapabilitiesRegistered", func(t *testing.T) {
		defs := Definitions()
		if len(defs) != 6 {
			t.Fatalf("definitions = %d, want 6", len(defs))
		}
		reg := capability.NewRegistry()
		store := NewStore()
		if err := RegisterInto(reg, store); err != nil {
			t.Fatalf("RegisterInto: %v", err)
		}
		for _, d := range defs {
			if _, ok := reg.Lookup(capability.Key{ID: d.ID, Version: d.Version}); !ok {
				t.Fatalf("capability %s not resolvable", d.ID)
			}
		}
		if err := RegisterInto(reg, store); err == nil {
			t.Fatal("double registration accepted")
		}
		// Write handlers run directly: the P1A gateway refuses write
		// effects by platform law, so the suite proves the refusal too.
		req, _ := NewRequest("case-c", "worker-1", 100, []string{"SIT_LIMIT"}, "", "")
		policy, _ := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
		out, err := RequestHandler(store)(context.Background(), RecordRequestPayload{
			Request: req, Classification: policy.Classification(), Purpose: policy.Purpose(),
			Participants: []runtime.RunParticipant{{Principal: "worker-1", Role: "WORKER"}},
		})
		if err != nil {
			t.Fatalf("RequestHandler: %v", err)
		}
		if out.(Case).ID != "case-c" {
			t.Fatalf("handler response = %+v", out)
		}
		gw := capability.NewGateway(reg, sinkFunc{}, capability.WithClock(func() time.Time { return time.Unix(1000, 0) }))
		allow := capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:accommodation.read", "scope:accommodation.write"}, Tenant: "acme"}
		if _, err := gw.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: CapRecordRequest, Version: 1}, Payload: nil, Authorization: allow,
		}); err == nil || !strings.Contains(err.Error(), "write") {
			t.Fatalf("write through P1A gateway: err = %v", err)
		}
	})
}

// TestTodo_WF_CAP_006_Security proves medical detail stays behind the run
// wall and field policy: a manager sees the accommodation, never the
// diagnosis; outsiders and wrong purposes learn nothing at all.
func TestTodo_WF_CAP_006_Security(t *testing.T) {
	fixture := func(t *testing.T) Case {
		t.Helper()
		req, err := NewRequest("case-s", "worker-1", 100,
			[]string{"SCHEDULE_LIMIT"}, "migraine disorder", "evidence:med-9")
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		policy, _ := runtime.NewOpenRunAccess("MEDICAL_SENSITIVE", authz.PurposeAccommodationCase)
		walled, err := policy.Wall(
			runtime.RunParticipant{Principal: "worker-1", Role: "WORKER"},
			runtime.RunParticipant{Principal: "manager-1", Role: "MANAGER"},
			runtime.RunParticipant{Principal: "hr-1", Role: "HR_PARTNER"},
		)
		if err != nil {
			t.Fatalf("Wall: %v", err)
		}
		cs, err := OpenCase(req, walled)
		if err != nil {
			t.Fatalf("OpenCase: %v", err)
		}
		_ = cs.RecordDialogue(DialogueEntry{ID: "d1", At: 110, AuthorRole: "HR_PARTNER", Author: "hr-1",
			Classification: EntryOperational, Body: "schedule options discussed"})
		_ = cs.RecordDialogue(DialogueEntry{ID: "d2", At: 120, AuthorRole: "MEDICAL_REVIEWER", Author: "med-1",
			Classification: EntryMedical, Body: "neurologist letter reviewed"})
		opt, _ := NewOption("o1", "case-s", "shift swap", true, false, "")
		_ = cs.ConsiderOption(opt)
		dec, _ := NewDecision("dec-1", "case-s", DecisionGranted, []string{"SCHEDULE_LIMIT"}, 200, 0, "shift swap granted", 150)
		_ = cs.Decide(dec)
		return *cs
	}
	manager := testPrincipal(t, "manager-1", string(authz.RoleManager))
	hrPartner := testPrincipal(t, "hr-1", string(authz.RoleHRPartner))

	t.Run("ManagerSeesAccommodationNotDiagnosis", func(t *testing.T) {
		view, err := ProjectCase(fixture(t), manager, authz.PurposeAccommodationCase)
		if err != nil {
			t.Fatalf("ProjectCase manager: %v", err)
		}
		if view.Decision == nil || view.Decision.Outcome != DecisionGranted {
			t.Fatalf("manager lost the accommodation: %+v", view.Decision)
		}
		if len(view.Options) != 1 || len(view.Limitations) != 1 {
			t.Fatalf("manager lost operational records: %+v", view)
		}
		if view.Diagnosis != "" || view.MedicalEvidenceRef != "" || !view.MedicalRedacted {
			t.Fatalf("manager sees medical detail: %+v", view)
		}
		for _, e := range view.Dialogue {
			if e.Classification == EntryMedical {
				t.Fatalf("manager sees a medical dialogue entry: %+v", e)
			}
		}
		if len(view.Dialogue) != 1 {
			t.Fatalf("manager lost operational dialogue: %+v", view.Dialogue)
		}
	})

	t.Run("HRPartnerSeesMedicalDetail", func(t *testing.T) {
		view, err := ProjectCase(fixture(t), hrPartner, authz.PurposeAccommodationCase)
		if err != nil {
			t.Fatalf("ProjectCase hr: %v", err)
		}
		if view.Diagnosis != "migraine disorder" || view.MedicalEvidenceRef != "evidence:med-9" {
			t.Fatalf("hr lost medical detail: %+v", view)
		}
		if view.MedicalRedacted || len(view.Dialogue) != 2 {
			t.Fatalf("hr lost dialogue: %+v", view)
		}
	})

	t.Run("OutsiderLearnsNothing", func(t *testing.T) {
		stranger := testPrincipal(t, "stranger-1", string(authz.RoleManager))
		_, err := ProjectCase(fixture(t), stranger, authz.PurposeAccommodationCase)
		if !errors.Is(err, runtime.ErrRunNotDisclosed) {
			t.Fatalf("outsider err = %v", err)
		}
		if strings.Contains(err.Error(), "migraine") || strings.Contains(err.Error(), "case-s") {
			t.Fatalf("denial discloses case content: %v", err)
		}
	})

	t.Run("WrongPurposeDenied", func(t *testing.T) {
		_, err := ProjectCase(fixture(t), manager, authz.PurposePerformanceReview)
		if !errors.Is(err, runtime.ErrRunNotDisclosed) {
			t.Fatalf("wrong purpose err = %v", err)
		}
	})

	t.Run("RevokedParticipantDenied", func(t *testing.T) {
		cs := fixture(t)
		narrowed, err := cs.Access.Narrow("manager-1")
		if err != nil {
			t.Fatalf("Narrow: %v", err)
		}
		cs.Access = narrowed
		if _, err := ProjectCase(cs, manager, authz.PurposeAccommodationCase); !errors.Is(err, runtime.ErrRunNotDisclosed) {
			t.Fatalf("revoked manager err = %v", err)
		}
	})
}

// testPrincipal builds a server-derived human principal like the authz
// suite's own fixture.
func testPrincipal(t *testing.T, subject, role string) *trust.Principal {
	t.Helper()
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("acme-corp"), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		Roles: []string{role}, Purposes: []string{authz.PurposeAccommodationCase, authz.PurposePerformanceReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-" + subject, IssuedAt: base.Add(-time.Hour), ExpiresAt: base.Add(time.Hour),
		CredentialDigest: "digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// sinkFunc is an in-memory capability evidence sink.
type sinkFunc struct{}

func (sinkFunc) RecordInvocation(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return "ev:test", nil
}

func (sinkFunc) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return "ev:test", nil
}
