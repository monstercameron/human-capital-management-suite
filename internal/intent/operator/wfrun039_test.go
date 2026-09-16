package operator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// movingClock is a gateway clock a test advances.
type movingClock struct{ at time.Time }

func (c *movingClock) now() time.Time          { return c.at }
func (c *movingClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// breakGlassFor opens a grant for user over kind, approved by approver.
func breakGlassFor(t *testing.T, id, user, approver string, kind Kind, now time.Time) *breakglass.Grant {
	t.Helper()
	g, err := breakglass.Open(id, breakglass.Request{
		User: user, IncidentRef: "INC-39", Justification: "payroll run wedged mid-cycle",
		Capabilities: []string{string(kind)}, TTL: 30 * time.Minute,
	}, breakglass.Approval{Approver: approver, At: now}, now)
	if err != nil {
		t.Fatalf("break-glass grant: %v", err)
	}
	return g
}

// emergencyRequest is a break-glass submission of kind over scope ids.
func emergencyRequest(t *testing.T, kind Kind, key string, grant *breakglass.Grant, ids ...string) Request {
	t.Helper()
	req := baseRequest(t, kind, key)
	req.JIT, req.SecondApprover, req.Simulation = nil, "", nil
	req.Operator = grant.User
	req.Scope = Scope{Resource: "workflow_instance", IDs: ids}
	req.Emergency = &Emergency{Grant: grant, BypassReason: "primary region down; no approver reachable"}
	return req
}

// grantAt mints a JIT grant that is current at now. grantFor pins testNow,
// which a test that advances past the review window has long outlived.
func grantAt(t *testing.T, principal string, role jit.Role, now time.Time, kinds ...Kind) *jit.Grant {
	t.Helper()
	caps := make([]string, 0, len(kinds))
	for _, k := range kinds {
		caps = append(caps, string(k))
	}
	g, err := jit.New("jit-"+principal+"-"+string(role)+"-"+now.Format(time.RFC3339Nano), jit.Request{
		Principal: principal, Tenant: testTenant, Role: role, TicketRef: "INC-39",
		Justification: "repair a wedged instance", Capabilities: caps, Purpose: "incident repair", TTL: time.Hour,
	}, jit.Approval{Approver: "granter:pat", At: now}, now)
	if err != nil {
		t.Fatalf("jit grant: %v", err)
	}
	return g
}

// jitRequest is an ordinary governed submission of kind by operatorID at now.
func jitRequest(t *testing.T, kind Kind, key, operatorID, approver string, now time.Time, ids ...string) Request {
	t.Helper()
	p, ok := PolicyFor(kind)
	if !ok {
		t.Fatalf("no policy for %s", kind)
	}
	scope := Scope{Resource: "workflow_instance", IDs: ids}
	req := baseRequest(t, kind, key)
	req.Operator, req.Scope = operatorID, scope
	req.JIT = grantAt(t, operatorID, p.Roles[0], now, kind)
	req.SecondApprover = approver
	req.Simulation = &Simulation{Digest: "sha256:sim", Scope: scope, At: now.Add(-time.Minute)}
	if !p.DualControl {
		req.SecondApprover = ""
	}
	if !p.SimulationRequired {
		req.Simulation = nil
	}
	return req
}

// TestTodo_WF_RUN_039 proves the bypass-obligation model end to end on the
// operator gateway: repair, override and migrate are three distinct operator
// kinds in three distinct authority families; a break-glass action records an
// obligation naming what it bypassed, who approved it and when its review is
// due; the family keeps working while the review is merely pending; once the
// review is overdue the family is suspended at the gateway and no effect runs;
// a sibling family is untouched; and a distinct reviewer's discharge lifts the
// suspension.
func TestTodo_WF_RUN_039(t *testing.T) {
	ctx := context.Background()
	clock := &movingClock{at: testNow}
	exec := &counting{}
	execs := map[Kind]Executor{}
	for _, k := range Kinds() {
		execs[k] = exec
	}
	journal := NewMemoryJournal()
	gw, err := NewGateway(journal, execs, clock.now)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}

	// The three authority families are distinct kinds with distinct policies.
	for kind, want := range map[Kind]struct {
		family Family
		role   jit.Role
		sim    bool
	}{
		KindWorkflowRepair:           {FamilyRepair, jit.RoleIntegrityRepair, true},
		KindWorkflowMigrate:          {FamilyMigrate, jit.RoleIntegrityRepair, true},
		KindWorkflowOverrideDecision: {FamilyOverride, jit.RoleIncidentResponder, false},
	} {
		p, ok := PolicyFor(kind)
		switch {
		case !ok || !p.Material || !p.DualControl:
			t.Fatalf("%s policy = %+v, %v; want a material dual-control policy", kind, p, ok)
		case kind.Family() != want.family:
			t.Errorf("%s family = %s, want %s", kind, kind.Family(), want.family)
		case len(p.Roles) != 1 || p.Roles[0] != want.role:
			t.Errorf("%s roles = %v, want exactly [%s]", kind, p.Roles, want.role)
		case p.SimulationRequired != want.sim:
			t.Errorf("%s simulation required = %v, want %v", kind, p.SimulationRequired, want.sim)
		}
	}
	// Repair authority does not authorize an override, and the other way
	// round: the kinds are separate capabilities on a grant, not one pool.
	crossed := jitRequest(t, KindWorkflowOverrideDecision, "key-crossed", "operator:ana", "operator:ben", clock.at, "instance-9")
	crossed.JIT = grantFor(t, "operator:ana", jit.RoleIntegrityRepair, KindWorkflowOverrideDecision)
	if _, err := gw.Submit(ctx, crossed); CodeOf(err) != CodeAuthorityMismatch {
		t.Fatalf("override under an integrity-repair grant = %v, want %s", err, CodeAuthorityMismatch)
	}

	// A break-glass repair records its obligation with a due review.
	grant := breakGlassFor(t, "bg-39", "operator:ana", "approver:lead", KindWorkflowRepair, clock.at)
	emergency := emergencyRequest(t, KindWorkflowRepair, "key-bypass", grant, "instance-1")
	receipt, err := gw.Submit(ctx, emergency)
	if err != nil {
		t.Fatalf("emergency repair: %v", err)
	}
	wantBypassed := []string{BypassJITAuthority, BypassDualControl, BypassSimulation}
	if !receipt.ReviewRequired || strings.Join(receipt.Bypassed, ",") != strings.Join(wantBypassed, ",") {
		t.Fatalf("receipt bypass evidence = %v (review %v), want %v", receipt.Bypassed, receipt.ReviewRequired, wantBypassed)
	}
	outstanding, err := gw.OutstandingObligations(ctx, testTenant)
	if err != nil || len(outstanding) != 1 {
		t.Fatalf("outstanding = %+v, %v; want exactly one obligation", outstanding, err)
	}
	o := outstanding[0]
	switch {
	case o.ID != ObligationID(testTenant, "key-bypass") || o.Family != FamilyRepair || o.Kind != KindWorkflowRepair:
		t.Errorf("obligation identity = %+v", o)
	case o.Operator != "operator:ana" || o.Approver != "approver:lead":
		t.Errorf("obligation accountability = operator %q approver %q", o.Operator, o.Approver)
	case !o.DueAt.Equal(clock.at.Add(ReviewWindow)) || o.RequestDigest != receipt.RequestDigest:
		t.Errorf("obligation due %s request %q; want %s / %s", o.DueAt, o.RequestDigest, clock.at.Add(ReviewWindow), receipt.RequestDigest)
	case o.Status(clock.at) != ObligationOpen || o.Verify() != nil:
		t.Errorf("obligation status = %s, verify = %v", o.Status(clock.at), o.Verify())
	}

	// Still within the review window: repair authority keeps working.
	clock.advance(ReviewWindow - time.Minute)
	ok := jitRequest(t, KindWorkflowRepair, "key-ok", "operator:cara", "operator:dee", clock.at, "instance-2")
	if r, err := gw.Submit(ctx, ok); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("repair inside the review window = %+v, %v; want APPLIED", r, err)
	}

	// Past the due review: the whole repair family is suspended and nothing
	// runs, while a sibling family is untouched.
	clock.advance(2 * time.Minute)
	before := exec.applied.Load()
	late := jitRequest(t, KindWorkflowRepair, "key-late", "operator:cara", "operator:dee", clock.at, "instance-3")
	_, err = gw.Submit(ctx, late)
	if CodeOf(err) != CodeObligationOverdue {
		t.Fatalf("repair under an overdue obligation = %v, want %s", err, CodeObligationOverdue)
	}
	if !strings.Contains(err.Error(), o.ID) {
		t.Errorf("refusal %q does not name the obligation %s that suspends the family", err, o.ID)
	}
	if exec.applied.Load() != before {
		t.Fatalf("a suspended family performed %d effects", exec.applied.Load()-before)
	}
	sibling := jitRequest(t, KindWorkflowOverrideDecision, "key-sibling", "operator:cara", "operator:dee", clock.at, "instance-4")
	if r, err := gw.Submit(ctx, sibling); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("override while repair is suspended = %+v, %v; want APPLIED", r, err)
	}

	// A distinct reviewer discharges the obligation and the family resumes.
	review := ObligationReview{Reviewer: "reviewer:sam", Outcome: ObligationJustified, Note: "outage confirmed; repair was correct", At: clock.at}
	discharged, err := gw.ReviewObligation(ctx, testTenant, o.ID, review)
	if err != nil || discharged.Status(clock.at) != ObligationDischarged || discharged.Verify() != nil {
		t.Fatalf("review = %+v, %v; want a sealed discharged obligation", discharged, err)
	}
	if left, err := gw.OutstandingObligations(ctx, testTenant); err != nil || len(left) != 0 {
		t.Fatalf("outstanding after review = %+v, %v; want none", left, err)
	}
	resumed := jitRequest(t, KindWorkflowRepair, "key-resumed", "operator:cara", "operator:dee", clock.at, "instance-5")
	if r, err := gw.Submit(ctx, resumed); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("repair after the review = %+v, %v; want APPLIED", r, err)
	}
	if _, err := gw.ReviewObligation(ctx, testTenant, o.ID, review); CodeOf(err) != CodeObligationUnknown {
		t.Fatalf("second review = %v, want %s", err, CodeObligationUnknown)
	}
}

// TestTodo_WF_RUN_039_Security proves the authority separation the todo asks
// for: the approver of record over a scope may not come back as its repair
// operator, a bypass the gateway cannot make accountable does not happen at
// all, and neither the operator nor the approver can review their own bypass.
func TestTodo_WF_RUN_039_Security(t *testing.T) {
	ctx := context.Background()
	clock := &movingClock{at: testNow}
	exec := &counting{}
	execs := map[Kind]Executor{}
	for _, k := range Kinds() {
		execs[k] = exec
	}
	journal := NewMemoryJournal()
	gw, err := NewGateway(journal, execs, clock.now)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}

	// approver:lead approves a break-glass repair of instance-1.
	grant := breakGlassFor(t, "bg-sec", "operator:ana", "approver:lead", KindWorkflowMigrate, clock.at)
	if _, err := gw.Submit(ctx, emergencyRequest(t, KindWorkflowMigrate, "key-sec", grant, "instance-1")); err != nil {
		t.Fatalf("emergency migrate: %v", err)
	}

	// The approver of record may not be the repair operator over that scope.
	before := exec.applied.Load()
	sameScope := jitRequest(t, KindWorkflowRepair, "key-approver-repairs", "approver:lead", "operator:ben", clock.at, "instance-1")
	_, err = gw.Submit(ctx, sameScope)
	if CodeOf(err) != CodeRepairSeparation {
		t.Fatalf("approver acting as repair operator = %v, want %s", err, CodeRepairSeparation)
	}
	if exec.applied.Load() != before {
		t.Fatalf("a refused repair performed %d effects", exec.applied.Load()-before)
	}
	// Another scope is another matter; the rule is scoped, not a blanket ban.
	elsewhere := jitRequest(t, KindWorkflowRepair, "key-elsewhere", "approver:lead", "operator:ben", clock.at, "instance-77")
	if r, err := gw.Submit(ctx, elsewhere); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("repair over an unrelated scope = %+v, %v; want APPLIED", r, err)
	}
	// A family that does not carry repair authority is not separated this way.
	override := jitRequest(t, KindWorkflowOverrideDecision, "key-override-same-scope", "approver:lead", "operator:ben", clock.at, "instance-1")
	if r, err := gw.Submit(ctx, override); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("override over the approved scope = %+v, %v; want APPLIED", r, err)
	}

	// Neither the operator nor the approver may review the bypass.
	id := ObligationID(testTenant, "key-sec")
	for who, reviewer := range map[string]string{"the operator": "operator:ana", "the approver": "approver:lead"} {
		_, err := gw.ReviewObligation(ctx, testTenant, id, ObligationReview{
			Reviewer: reviewer, Outcome: ObligationJustified, Note: "self-signed", At: clock.at})
		if CodeOf(err) != CodeObligationReviewer {
			t.Errorf("review by %s = %v, want %s", who, err, CodeObligationReviewer)
		}
	}

	// A gateway whose journal is not an obligation store refuses the bypass
	// outright rather than taking it unaccountably.
	blind, err := NewGateway(journalOnly{NewMemoryJournal()}, execs, clock.now)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	before = exec.applied.Load()
	blindGrant := breakGlassFor(t, "bg-blind", "operator:ana", "approver:lead", KindWorkflowRepair, clock.at)
	_, err = blind.Submit(ctx, emergencyRequest(t, KindWorkflowRepair, "key-blind", blindGrant, "instance-8"))
	if CodeOf(err) != CodeObligationRequired {
		t.Fatalf("bypass without an obligation store = %v, want %s", err, CodeObligationRequired)
	}
	if exec.applied.Load() != before {
		t.Fatalf("an unaccountable bypass performed %d effects", exec.applied.Load()-before)
	}
	if _, err := blind.ReviewObligation(ctx, testTenant, id, ObligationReview{}); CodeOf(err) != CodeObligationRequired {
		t.Fatalf("review without an obligation store = %v, want %s", err, CodeObligationRequired)
	}

	// A store that cannot answer fails the submission closed: the gateway
	// cannot tell whether the family is suspended, so it does not act.
	failing, err := NewGateway(NewMemoryJournal(), execs, clock.now, WithObligations(brokenObligations{}))
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	before = exec.applied.Load()
	if _, err := failing.Submit(ctx, jitRequest(t, KindWorkflowRepair, "key-broken", "operator:cara", "operator:dee", clock.at, "instance-6")); CodeOf(err) != CodeObligationFailed {
		t.Fatalf("submit over an unreadable obligation store = %v, want %s", err, CodeObligationFailed)
	}
	if exec.applied.Load() != before {
		t.Fatalf("a submission over an unreadable store performed %d effects", exec.applied.Load()-before)
	}
}

// journalOnly hides a MemoryJournal's ObligationStore methods, which is what a
// composition that wires a receipt journal but no obligation store looks like.
type journalOnly struct{ inner *MemoryJournal }

func (j journalOnly) Lookup(ctx context.Context, tenant values.TenantId, key string) (Receipt, bool, error) {
	return j.inner.Lookup(ctx, tenant, key)
}
func (j journalOnly) Begin(ctx context.Context, pending Receipt) (Receipt, bool, error) {
	return j.inner.Begin(ctx, pending)
}
func (j journalOnly) Complete(ctx context.Context, final Receipt) error {
	return j.inner.Complete(ctx, final)
}
func (j journalOnly) Abort(ctx context.Context, pending Receipt) error {
	return j.inner.Abort(ctx, pending)
}

// brokenObligations is an obligation store that cannot answer.
type brokenObligations struct{}

var errStoreDown = errors.New("obligation store is unreachable")

func (brokenObligations) RecordObligation(context.Context, Obligation) error { return errStoreDown }
func (brokenObligations) OutstandingObligations(context.Context, values.TenantId) ([]Obligation, error) {
	return nil, errStoreDown
}
func (brokenObligations) DischargeObligation(context.Context, values.TenantId, string, ObligationReview) (Obligation, error) {
	return Obligation{}, errStoreDown
}

// TestTodo_WF_RUN_039_Mutation proves the obligation's own integrity rules:
// a tampered obligation is refused rather than trusted, the due-review
// boundary is exact, and a bypass changes the receipt it is sealed into.
func TestTodo_WF_RUN_039_Mutation(t *testing.T) {
	ctx := context.Background()
	base := Obligation{
		ID: ObligationID(testTenant, "key-mut"), Tenant: testTenant, Kind: KindWorkflowRepair,
		Family: FamilyRepair, Scope: Scope{Resource: "workflow_instance", IDs: []string{"instance-1"}},
		Operator: "operator:ana", Approver: "approver:lead", AuthorityKind: AuthorityBreakGlass,
		AuthorityRef: "bg-1", IdempotencyKey: "key-mut", RequestDigest: "sha256:request",
		TicketRef: "INC-39", Bypassed: []string{BypassJITAuthority}, BypassReason: "region down",
		RecordedAt: testNow, DueAt: testNow.Add(ReviewWindow),
	}
	sealed := base.Sealed()
	if sealed.Verify() != nil {
		t.Fatalf("a freshly sealed obligation does not verify: %v", sealed.Verify())
	}

	// Every field is under the seal: changing any of them breaks it.
	for name, mutate := range map[string]func(*Obligation){
		"approver":  func(o *Obligation) { o.Approver = "operator:ana" },
		"due":       func(o *Obligation) { o.DueAt = o.DueAt.Add(365 * 24 * time.Hour) },
		"scope":     func(o *Obligation) { o.Scope.IDs = []string{"instance-2"} },
		"bypassed":  func(o *Obligation) { o.Bypassed = nil },
		"unsealed":  func(o *Obligation) { o.Digest = "" },
		"request":   func(o *Obligation) { o.RequestDigest = "sha256:other" },
		"operator":  func(o *Obligation) { o.Operator = "operator:mallory" },
		"recorded":  func(o *Obligation) { o.RecordedAt = o.RecordedAt.Add(-time.Hour) },
		"bypassref": func(o *Obligation) { o.BypassReason = "" },
	} {
		tampered := sealed
		mutate(&tampered)
		if err := tampered.Verify(); err == nil {
			t.Errorf("a %s-tampered obligation still verifies", name)
		}
		if err := NewMemoryJournal().RecordObligation(ctx, tampered); err == nil {
			t.Errorf("the store recorded a %s-tampered obligation", name)
		}
	}

	// The due-review boundary is exact: open right up to it, overdue at it.
	if got := sealed.Status(sealed.DueAt.Add(-time.Nanosecond)); got != ObligationOpen {
		t.Errorf("status one nanosecond before due = %s, want %s", got, ObligationOpen)
	}
	if got := sealed.Status(sealed.DueAt); got != ObligationOverdue {
		t.Errorf("status at the due instant = %s, want %s", got, ObligationOverdue)
	}
	if suspended := SuspendedFamilies([]Obligation{sealed}, sealed.DueAt); len(suspended) != 1 || suspended[FamilyRepair].ID != sealed.ID {
		t.Errorf("SuspendedFamilies at the due instant = %+v", suspended)
	}
	if suspended := SuspendedFamilies([]Obligation{sealed}, sealed.RecordedAt); len(suspended) != 0 {
		t.Errorf("SuspendedFamilies inside the window = %+v, want none", suspended)
	}
	discharged, err := sealed.Discharged(ObligationReview{Reviewer: "reviewer:sam", Outcome: ObligationViolation, Note: "not justified", At: sealed.DueAt})
	if err != nil {
		t.Fatalf("Discharged: %v", err)
	}
	if len(SuspendedFamilies([]Obligation{discharged}, sealed.DueAt.Add(time.Hour))) != 0 {
		t.Errorf("a discharged obligation still suspends its family")
	}

	// The bypass list is under the receipt seal too.
	r := Receipt{Kind: KindWorkflowRepair, Tenant: testTenant, IdempotencyKey: "key-mut", Bypassed: []string{BypassDualControl}}.sealed()
	if r.Verify() != nil {
		t.Fatalf("sealed receipt does not verify: %v", r.Verify())
	}
	r.Bypassed = nil
	if r.Verify() == nil {
		t.Error("a receipt whose bypass list was removed still verifies")
	}
}
