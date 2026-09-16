package operator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

var (
	testNow    = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	testTenant = values.TenantId("tenant-ops")
)

func testClock() time.Time { return testNow }

// counting is an executor that refuses without gateway authorization and
// counts how many effects it performed.
type counting struct {
	applied atomic.Int64
	fail    error
}

func (c *counting) Apply(_ context.Context, auth Authorization, req Request) (string, error) {
	if err := auth.Require(req.Kind, req.Tenant); err != nil {
		return "", err
	}
	if c.fail != nil {
		return "", c.fail
	}
	n := c.applied.Add(1)
	return fmt.Sprintf("effect:%s:%d", auth.IntentInstanceID(), n), nil
}

func grantFor(t *testing.T, principal string, role jit.Role, kinds ...Kind) *jit.Grant {
	t.Helper()
	caps := make([]string, 0, len(kinds))
	for _, k := range kinds {
		caps = append(caps, string(k))
	}
	g, err := jit.New("jit-"+principal+"-"+string(role), jit.Request{
		Principal: principal, Tenant: testTenant, Role: role, TicketRef: "INC-22",
		Justification: "repair a stuck promotion", Capabilities: caps, Purpose: "incident repair", TTL: time.Hour,
	}, jit.Approval{Approver: "approver:lead", At: testNow}, testNow)
	if err != nil {
		t.Fatalf("jit grant: %v", err)
	}
	return g
}

func simulationOf(scope Scope) *Simulation {
	return &Simulation{Digest: "sha256:sim", Scope: scope, At: testNow.Add(-time.Minute)}
}

func baseRequest(t *testing.T, kind Kind, key string) Request {
	t.Helper()
	scope := Scope{Resource: "workflow_instance", IDs: []string{"instance-2", "instance-1"}}
	return Request{
		Kind: kind, Tenant: testTenant, Operator: "operator:ana", Scope: scope, ExpectedVersion: "7",
		PayloadDigest: "sha256:payload", IdempotencyKey: key, Reason: "node stuck after connector outage", TicketRef: "INC-22",
		JIT:            grantFor(t, "operator:ana", jit.RoleIntegrityRepair, kind),
		SecondApprover: "operator:ben",
		Simulation:     simulationOf(Scope{Resource: "workflow_instance", IDs: []string{"instance-1", "instance-2"}}),
	}
}

func newGateway(t *testing.T, j Journal, execs map[Kind]Executor) *Gateway {
	t.Helper()
	g, err := NewGateway(j, execs, testClock)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	return g
}

// TestOperatorMutationRequiresIntentAndJITAuthority proves the operator
// gateway: every material operator action resolves a typed operational intent
// under a JIT grant with its policy's dual control and simulation, runs its
// effect exactly once behind a gateway-minted authorization, and leaves a
// verifiable receipt; the same action without that authority -- including
// the executor called directly as a side door -- changes nothing.
func TestOperatorMutationRequiresIntentAndJITAuthority(t *testing.T) {
	ctx := context.Background()
	exec := &counting{}
	execs := map[Kind]Executor{}
	for _, k := range Kinds() {
		execs[k] = exec
	}
	gw := newGateway(t, NewMemoryJournal(), execs)

	for _, k := range Kinds() {
		if k == KindDiagnosticRead {
			continue
		}
		p, _ := PolicyFor(k)
		if !p.Material || len(p.Roles) == 0 || !strings.HasPrefix(p.IntentType, "hcmnext.operations.") {
			t.Errorf("%s policy = %+v, want a material registered intent with JIT roles", k, p)
		}
		req := baseRequest(t, k, "key-"+string(k))
		req.JIT = grantFor(t, "operator:ana", p.Roles[0], k)
		before := exec.applied.Load()
		r, err := gw.Submit(ctx, req)
		if err != nil {
			t.Fatalf("%s: governed submit refused: %v", k, err)
		}
		if exec.applied.Load() != before+1 || r.Outcome != OutcomeApplied || r.EffectRef == "" {
			t.Fatalf("%s: receipt %+v, effects %d->%d", k, r, before, exec.applied.Load())
		}
		if r.IntentInstanceID != IntentInstanceID(testTenant, req.IdempotencyKey) || r.IntentType != p.IntentType ||
			r.AuthorityKind != AuthorityJIT || r.AuthorityRef != req.JIT.ID || r.Verify() != nil {
			t.Errorf("%s: receipt identity/authority = %+v", k, r)
		}
		if p.DualControl != (r.SecondApprover != "") || p.SimulationRequired != (r.SimulationDigest != "") {
			t.Errorf("%s: receipt dual/sim evidence %q/%q does not match policy %+v", k, r.SecondApprover, r.SimulationDigest, p)
		}
		used := false
		for _, e := range req.JIT.Evidence() {
			used = used || (e.Kind == jit.EvidenceUsed && strings.Contains(e.Detail, string(k)))
		}
		if !used {
			t.Errorf("%s: the JIT grant carries no USED evidence for the action", k)
		}
	}

	// No authority: nothing runs.
	before := exec.applied.Load()
	noJIT := baseRequest(t, KindDatabaseRepair, "key-no-jit")
	noJIT.JIT = nil
	if _, err := gw.Submit(ctx, noJIT); CodeOf(err) != CodeAuthorityRequired {
		t.Fatalf("submit without JIT = %v, want %s", err, CodeAuthorityRequired)
	}
	// Side door: the executor invoked directly, without a gateway authorization.
	if _, err := exec.Apply(ctx, Authorization{}, noJIT); CodeOf(err) != CodeUnauthorizedEffect {
		t.Fatalf("direct executor call = %v, want %s", err, CodeUnauthorizedEffect)
	}
	if exec.applied.Load() != before {
		t.Fatalf("an unauthorized path performed %d effects", exec.applied.Load()-before)
	}

	// A request can only name a typed scope and version, never a target state.
	for _, forbidden := range []string{"TargetState", "Status", "State", "SQL", "Statement"} {
		if _, ok := reflect.TypeOf(Request{}).FieldByName(forbidden); ok {
			t.Errorf("Request carries a %s field: operators could submit an arbitrary target state", forbidden)
		}
	}

	// Emergency: bypasses dual control and simulation, records the bypass
	// reason, and demands review.
	bg, err := breakglass.Open("bg-1", breakglass.Request{User: "operator:ana", IncidentRef: "INC-22", Justification: "payroll outage",
		Capabilities: []string{string(KindFailover)}, TTL: 30 * time.Minute}, breakglass.Approval{Approver: "approver:lead", At: testNow}, testNow)
	if err != nil {
		t.Fatalf("break-glass: %v", err)
	}
	emergency := baseRequest(t, KindFailover, "key-emergency")
	emergency.JIT, emergency.SecondApprover, emergency.Simulation = nil, "", nil
	emergency.Emergency = &Emergency{Grant: bg, BypassReason: "primary region down; second approver unreachable"}
	r, err := gw.Submit(ctx, emergency)
	if err != nil {
		t.Fatalf("emergency submit: %v", err)
	}
	if r.AuthorityKind != AuthorityBreakGlass || !r.ReviewRequired || r.BypassReason == "" || !bg.ReviewRequired() {
		t.Errorf("emergency receipt = %+v, grant review required %v", r, bg.ReviewRequired())
	}
}

// TestTodo_INTENT_022_Race proves concurrent submissions of one action under
// one idempotency key perform exactly one effect and all agree on it.
func TestTodo_INTENT_022_Race(t *testing.T) {
	exec := &counting{}
	gw := newGateway(t, NewMemoryJournal(), map[Kind]Executor{KindConnectorRedrive: exec})
	req := baseRequest(t, KindConnectorRedrive, "key-race")
	req.JIT = grantFor(t, "operator:ana", jit.RoleIncidentResponder, KindConnectorRedrive)
	var wg sync.WaitGroup
	results := make([]Receipt, 16)
	errs := make([]error, 16)
	for i := range results {
		wg.Go(func() { results[i], errs[i] = gw.Submit(context.Background(), req) })
	}
	wg.Wait()
	if exec.applied.Load() != 1 {
		t.Fatalf("concurrent submissions performed %d effects, want 1", exec.applied.Load())
	}
	applied := 0
	for i, r := range results {
		switch {
		case errs[i] == nil && r.Outcome == OutcomeApplied:
			applied++
		case errs[i] == nil && r.Outcome == OutcomeDuplicate:
		case CodeOf(errs[i]) == CodeRepairRequired && r.Outcome == OutcomePending:
			// Lost the race while the winner was still applying: refused, not re-run.
		default:
			t.Errorf("submission %d = %+v, %v", i, r, errs[i])
		}
	}
	if applied != 1 {
		t.Errorf("APPLIED receipts = %d, want 1", applied)
	}
}

// TestTodo_INTENT_022_Integration runs the gateway over real JIT and
// break-glass grants through their whole lifecycle: grant, use, replay,
// revocation, emergency use and mandatory post-use review.
func TestTodo_INTENT_022_Integration(t *testing.T) {
	ctx := context.Background()
	journal := NewMemoryJournal()
	exec := &counting{}
	gw := newGateway(t, journal, map[Kind]Executor{KindTenantSuspension: exec, KindDiagnosticRead: exec})

	suspend := baseRequest(t, KindTenantSuspension, "key-suspend")
	suspend.JIT = grantFor(t, "operator:ana", jit.RoleAccessRevocation, KindTenantSuspension, KindDiagnosticRead)
	first, err := gw.Submit(ctx, suspend)
	if err != nil || first.Outcome != OutcomeApplied {
		t.Fatalf("suspend = %+v, %v", first, err)
	}
	again, err := gw.Submit(ctx, suspend)
	if err != nil || again.Outcome != OutcomeDuplicate || again.EffectRef != first.EffectRef || exec.applied.Load() != 1 {
		t.Fatalf("replay = %+v, %v (effects %d); want the original receipt and no second effect", again, err, exec.applied.Load())
	}

	// The diagnostic read needs a read role but no dual control or simulation.
	read := baseRequest(t, KindDiagnosticRead, "key-read")
	read.JIT = grantFor(t, "operator:ana", jit.RoleSupportReadOnly, KindDiagnosticRead)
	read.SecondApprover, read.Simulation = "", nil
	if r, err := gw.Submit(ctx, read); err != nil || r.SecondApprover != "" {
		t.Fatalf("diagnostic read = %+v, %v", r, err)
	}

	// Revocation ends authority immediately.
	revoked := baseRequest(t, KindTenantSuspension, "key-after-revoke")
	revoked.JIT = grantFor(t, "operator:ana", jit.RoleAccessRevocation, KindTenantSuspension)
	if err := revoked.JIT.Revoke("approver:lead", "incident closed", testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Submit(ctx, revoked); CodeOf(err) != CodeAuthorityInactive || !errors.Is(err, jit.ErrGrantRevoked) {
		t.Fatalf("revoked grant = %v, want %s wrapping ErrGrantRevoked", err, CodeAuthorityInactive)
	}
	if len(journal.Receipts()) != 2 {
		t.Errorf("journal holds %d receipts, want 2 (denials record nothing)", len(journal.Receipts()))
	}

	bg, err := breakglass.Open("bg-2", breakglass.Request{User: "operator:ana", IncidentRef: "INC-23", Justification: "tenant compromise",
		Capabilities: []string{string(KindTenantSuspension)}, TTL: 20 * time.Minute}, breakglass.Approval{Approver: "approver:lead", At: testNow}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	emergency := baseRequest(t, KindTenantSuspension, "key-bg")
	emergency.JIT, emergency.SecondApprover = nil, ""
	emergency.Emergency = &Emergency{Grant: bg, BypassReason: "active credential abuse"}
	r, err := gw.Submit(ctx, emergency)
	if err != nil || !r.ReviewRequired {
		t.Fatalf("emergency = %+v, %v", r, err)
	}
	if err := bg.PostUseReview("reviewer:sec", breakglass.ReviewJustified, "abuse confirmed", testNow.Add(time.Hour)); err != nil || bg.ReviewRequired() {
		t.Fatalf("post-use review = %v, still required %v", err, bg.ReviewRequired())
	}
}

// TestTodo_INTENT_022_Fault covers failures around the effect: a failed
// effect is recorded REPAIR_REQUIRED and never retried blindly, journal
// errors refuse before any effect, and malformed wiring is refused.
func TestTodo_INTENT_022_Fault(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("connector timeout")
	exec := &counting{fail: boom}
	journal := NewMemoryJournal()
	gw := newGateway(t, journal, map[Kind]Executor{KindProjectionRebuild: exec})
	req := baseRequest(t, KindProjectionRebuild, "key-fault")
	req.SecondApprover = ""
	r, err := gw.Submit(ctx, req)
	if CodeOf(err) != CodeRepairRequired || !errors.Is(err, boom) || r.Outcome != OutcomeRepairRequired || r.FailureCode != "EFFECT_FAILED" {
		t.Fatalf("failed effect = %+v, %v", r, err)
	}
	exec.fail = nil
	if _, err := gw.Submit(ctx, req); CodeOf(err) != CodeRepairRequired || exec.applied.Load() != 0 {
		t.Fatalf("retry after failure = %v (effects %d), want REPAIR_REQUIRED and no re-run", err, exec.applied.Load())
	}

	lookupFail := newGateway(t, faultyJournal{lookup: boom}, map[Kind]Executor{KindProjectionRebuild: exec})
	if _, err := lookupFail.Submit(ctx, baseRequest(t, KindProjectionRebuild, "k2")); CodeOf(err) != CodeJournal {
		t.Errorf("lookup failure = %v", err)
	}
	beginFail := newGateway(t, faultyJournal{begin: boom}, map[Kind]Executor{KindProjectionRebuild: exec})
	if _, err := beginFail.Submit(ctx, baseRequest(t, KindProjectionRebuild, "k3")); CodeOf(err) != CodeJournal || exec.applied.Load() != 0 {
		t.Errorf("begin failure = %v (effects %d)", err, exec.applied.Load())
	}
	completeFail := newGateway(t, faultyJournal{complete: boom}, map[Kind]Executor{KindProjectionRebuild: exec})
	if _, err := completeFail.Submit(ctx, baseRequest(t, KindProjectionRebuild, "k4")); CodeOf(err) != CodeJournal {
		t.Errorf("complete failure = %v", err)
	}
	failedCoded := &counting{fail: &Error{Code: "CONNECTOR_DOWN"}}
	gwCoded := newGateway(t, NewMemoryJournal(), map[Kind]Executor{KindProjectionRebuild: failedCoded})
	if r, _ := gwCoded.Submit(ctx, baseRequest(t, KindProjectionRebuild, "k5")); r.FailureCode != "CONNECTOR_DOWN" {
		t.Errorf("coded failure recorded %q", r.FailureCode)
	}
	if _, err := newGateway(t, NewMemoryJournal(), nil).Submit(ctx, baseRequest(t, KindProjectionRebuild, "k6")); CodeOf(err) != CodeNoExecutor {
		t.Errorf("unregistered executor = %v", err)
	}
	if _, err := NewGateway(nil, nil, nil); CodeOf(err) != CodeInvalidRequest {
		t.Errorf("nil journal = %v", err)
	}
	if _, err := NewGateway(NewMemoryJournal(), map[Kind]Executor{"SQL_CONSOLE": exec}, nil); CodeOf(err) != CodeUnknownKind {
		t.Errorf("unknown executor kind = %v", err)
	}
	// A failure the executor proves had no effect leaves the key retryable.
	noEffect := &counting{fail: fmt.Errorf("plan lookup: %w", ErrNoEffect)}
	nj := NewMemoryJournal()
	gwNoEffect := newGateway(t, nj, map[Kind]Executor{KindProjectionRebuild: noEffect})
	retryable := baseRequest(t, KindProjectionRebuild, "k7")
	if _, err := gwNoEffect.Submit(ctx, retryable); !errors.Is(err, ErrNoEffect) || len(nj.Receipts()) != 0 {
		t.Fatalf("no-effect failure = %v, receipts %d; want the error and no receipt", err, len(nj.Receipts()))
	}
	noEffect.fail = nil
	if r, err := gwNoEffect.Submit(ctx, retryable); err != nil || r.Outcome != OutcomeApplied {
		t.Fatalf("retry after a no-effect failure = %+v, %v", r, err)
	}
	abortFail := newGateway(t, faultyJournal{complete: boom}, map[Kind]Executor{KindProjectionRebuild: &counting{fail: ErrNoEffect}})
	if _, err := abortFail.Submit(ctx, baseRequest(t, KindProjectionRebuild, "k8")); CodeOf(err) != CodeJournal {
		t.Errorf("abort failure = %v", err)
	}
	if err := NewMemoryJournal().Abort(ctx, Receipt{Tenant: testTenant, IdempotencyKey: "none"}); CodeOf(err) != CodeJournal {
		t.Errorf("aborting an unknown receipt = %v", err)
	}
	if err := NewMemoryJournal().Complete(ctx, Receipt{Tenant: testTenant, IdempotencyKey: "none"}); CodeOf(err) != CodeJournal {
		t.Errorf("completing an unknown receipt = %v", err)
	}
	e := &Error{Code: CodeJournal, Kind: KindFailover, Detail: "d", Err: boom}
	if !strings.Contains(e.Error(), "connector timeout") || e.ErrorCode() != CodeJournal || (*Error)(nil).ErrorCode() != "" {
		t.Errorf("error rendering = %q", e.Error())
	}
}

type faultyJournal struct{ lookup, begin, complete error }

func (f faultyJournal) Lookup(context.Context, values.TenantId, string) (Receipt, bool, error) {
	return Receipt{}, false, f.lookup
}
func (f faultyJournal) Begin(context.Context, Receipt) (Receipt, bool, error) {
	return Receipt{}, false, f.begin
}
func (f faultyJournal) Complete(context.Context, Receipt) error { return f.complete }
func (f faultyJournal) Abort(context.Context, Receipt) error    { return f.complete }

// TestTodo_INTENT_022_Security refuses every attempt to borrow, stretch or
// self-approve authority, and every malformed or conflicting request.
func TestTodo_INTENT_022_Security(t *testing.T) {
	ctx := context.Background()
	exec := &counting{}
	execs := map[Kind]Executor{}
	for _, k := range Kinds() {
		execs[k] = exec
	}
	gw := newGateway(t, NewMemoryJournal(), execs)
	otherTenant, err := jit.New("jit-other", jit.Request{Principal: "operator:ana", Tenant: "tenant-other", Role: jit.RoleIntegrityRepair,
		TicketRef: "INC", Justification: "j", Capabilities: []string{string(KindDatabaseRepair)}, Purpose: "p", TTL: time.Hour},
		jit.Approval{Approver: "approver:lead", At: testNow}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := jit.New("jit-expired", jit.Request{Principal: "operator:ana", Tenant: testTenant, Role: jit.RoleIntegrityRepair,
		TicketRef: "INC", Justification: "j", Capabilities: []string{string(KindDatabaseRepair)}, Purpose: "p", TTL: time.Minute},
		jit.Approval{Approver: "approver:lead", At: testNow.Add(-time.Hour)}, testNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	foreignBG, _ := breakglass.Open("bg-foreign", breakglass.Request{User: "operator:zed", IncidentRef: "I", Justification: "j",
		Capabilities: []string{string(KindDatabaseRepair)}, TTL: time.Minute}, breakglass.Approval{Approver: "approver:lead", At: testNow}, testNow)
	narrowBG, _ := breakglass.Open("bg-narrow", breakglass.Request{User: "operator:ana", IncidentRef: "I", Justification: "j",
		Capabilities: []string{string(KindQuarantine)}, TTL: time.Minute}, breakglass.Approval{Approver: "approver:lead", At: testNow}, testNow)

	cases := map[string]struct {
		mutate func(*Request)
		code   string
	}{
		"unknown kind":           {func(r *Request) { r.Kind = "SQL_CONSOLE" }, CodeUnknownKind},
		"no tenant":              {func(r *Request) { r.Tenant = "" }, CodeInvalidRequest},
		"no ticket":              {func(r *Request) { r.TicketRef = " " }, CodeInvalidRequest},
		"tenant-wide omission":   {func(r *Request) { r.Scope.IDs = nil }, CodeInvalidRequest},
		"borrowed grant":         {func(r *Request) { r.Operator = "operator:mallory" }, CodeAuthorityMismatch},
		"other tenant grant":     {func(r *Request) { r.JIT = otherTenant }, CodeAuthorityMismatch},
		"wrong role":             {func(r *Request) { r.JIT = grantFor(t, "operator:ana", jit.RoleSupportReadOnly, KindDatabaseRepair) }, CodeAuthorityMismatch},
		"capability not granted": {func(r *Request) { r.JIT = grantFor(t, "operator:ana", jit.RoleIntegrityRepair, KindProjectionRebuild) }, CodeAuthorityMismatch},
		"expired grant":          {func(r *Request) { r.JIT = expired }, CodeAuthorityInactive},
		"self dual control":      {func(r *Request) { r.SecondApprover = "OPERATOR:ANA" }, CodeDualControlRequired},
		"no second approver":     {func(r *Request) { r.SecondApprover = "" }, CodeDualControlRequired},
		"no simulation":          {func(r *Request) { r.Simulation = nil }, CodeSimulationRequired},
		"simulation other scope": {func(r *Request) {
			r.Simulation = simulationOf(Scope{Resource: "workflow_instance", IDs: []string{"instance-9"}})
		}, CodeSimulationRequired},
		"stale simulation": {func(r *Request) {
			r.Simulation = &Simulation{Digest: "d", Scope: r.Scope, At: testNow.Add(-48 * time.Hour)}
		}, CodeSimulationRequired},
		"emergency without reason": {func(r *Request) { r.Emergency = &Emergency{Grant: narrowBG} }, CodeBypassReason},
		"emergency without grant":  {func(r *Request) { r.Emergency = &Emergency{BypassReason: "x"} }, CodeAuthorityRequired},
		"emergency foreign grant":  {func(r *Request) { r.Emergency = &Emergency{Grant: foreignBG, BypassReason: "x"} }, CodeAuthorityMismatch},
		"emergency narrow grant":   {func(r *Request) { r.Emergency = &Emergency{Grant: narrowBG, BypassReason: "x"} }, CodeAuthorityMismatch},
		"emergency diagnostic": {func(r *Request) {
			r.Kind, r.Emergency = KindDiagnosticRead, &Emergency{Grant: narrowBG, BypassReason: "x"}
		}, CodeAuthorityMismatch},
	}
	for name, c := range cases {
		req := baseRequest(t, KindDatabaseRepair, "key-sec-"+name)
		c.mutate(&req)
		if _, err := gw.Submit(ctx, req); CodeOf(err) != c.code {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
	if exec.applied.Load() != 0 {
		t.Fatalf("refused requests performed %d effects", exec.applied.Load())
	}

	// A key already bound to one action cannot be reused for another.
	req := baseRequest(t, KindDatabaseRepair, "key-bound")
	if _, err := gw.Submit(ctx, req); err != nil {
		t.Fatal(err)
	}
	other := baseRequest(t, KindDatabaseRepair, "key-bound")
	other.Scope.IDs = []string{"instance-1", "instance-2", "instance-3"}
	other.Simulation = simulationOf(other.Scope)
	if _, err := gw.Submit(ctx, other); CodeOf(err) != CodeIdempotencyConflict {
		t.Errorf("key reuse = %v, want %s", err, CodeIdempotencyConflict)
	}
	// An executor holding an authorization for one kind or tenant cannot use it for another.
	auth := Authorization{kind: KindDatabaseRepair, tenant: testTenant, instance: "intent:operator:x"}
	if auth.Require(KindKeyRotation, testTenant) == nil || auth.Require(KindDatabaseRepair, "tenant-other") == nil || auth.Require(KindDatabaseRepair, testTenant) != nil {
		t.Error("Authorization.Require does not bind kind and tenant")
	}
	if auth.Kind() != KindDatabaseRepair || auth.Tenant() != testTenant || auth.IntentInstanceID() == "" || auth.Scope().Resource != "" {
		t.Error("Authorization accessors")
	}
	tampered, _ := gw.Submit(ctx, req)
	tampered.Operator = "operator:mallory"
	if tampered.Verify() == nil {
		t.Error("a tampered receipt verified")
	}
}

// TestTodo_INTENT_022_Recovery proves recovery never depends on process
// memory: a gateway recomposed over the same journal replays completed
// actions without re-running them and refuses to blindly re-run an action
// whose previous attempt crashed before recording its outcome.
func TestTodo_INTENT_022_Recovery(t *testing.T) {
	ctx := context.Background()
	journal := NewMemoryJournal()
	exec := &counting{}
	first := newGateway(t, journal, map[Kind]Executor{KindKeyRotation: exec})
	rotate := baseRequest(t, KindKeyRotation, "key-rotate")
	rotate.JIT = grantFor(t, "operator:ana", jit.RoleIncidentResponder, KindKeyRotation)
	applied, err := first.Submit(ctx, rotate)
	if err != nil {
		t.Fatal(err)
	}

	// A crash after authorization and before the outcome was recorded.
	crashed := baseRequest(t, KindKeyRotation, "key-crashed")
	crashed.JIT = grantFor(t, "operator:ana", jit.RoleIncidentResponder, KindKeyRotation)
	pending, err := first.authorize(crashed, mustPolicy(t, KindKeyRotation), testNow)
	if err != nil {
		t.Fatal(err)
	}
	pending.RequestDigest, pending.Outcome, pending.RecordedAt = crashed.digest(), OutcomePending, testNow
	if _, _, err := journal.Begin(ctx, pending.sealed()); err != nil {
		t.Fatal(err)
	}

	restarted := newGateway(t, journal, map[Kind]Executor{KindKeyRotation: exec})
	replayed, err := restarted.Submit(ctx, rotate)
	if err != nil || replayed.Outcome != OutcomeDuplicate || replayed.EffectRef != applied.EffectRef {
		t.Fatalf("replay after restart = %+v, %v", replayed, err)
	}
	if _, err := restarted.Submit(ctx, crashed); CodeOf(err) != CodeRepairRequired {
		t.Fatalf("crashed action after restart = %v, want %s", err, CodeRepairRequired)
	}
	if exec.applied.Load() != 1 {
		t.Fatalf("recovery performed %d effects, want only the original 1", exec.applied.Load())
	}
}

func mustPolicy(t *testing.T, k Kind) Policy {
	t.Helper()
	p, ok := PolicyFor(k)
	if !ok {
		t.Fatalf("no policy for %s", k)
	}
	return p
}

// TestTodo_INTENT_022_Mutation proves each guard is load-bearing: a gateway
// whose policy is weakened in one dimension admits exactly the request the
// real policy refuses, so removing that guard would be caught.
func TestTodo_INTENT_022_Mutation(t *testing.T) {
	ctx := context.Background()
	for name, m := range map[string]struct {
		weaken func(Policy) Policy
		break_ func(*Request)
	}{
		"dual control": {func(p Policy) Policy { p.DualControl = false; return p }, func(r *Request) { r.SecondApprover = "" }},
		"simulation":   {func(p Policy) Policy { p.SimulationRequired = false; return p }, func(r *Request) { r.Simulation = nil }},
		"jit role": {func(p Policy) Policy { p.Roles = append(p.Roles, jit.RoleSupportReadOnly); return p }, func(r *Request) {
			r.JIT = grantFor(t, "operator:ana", jit.RoleSupportReadOnly, KindDatabaseRepair)
		}},
	} {
		exec := &counting{}
		strong := newGateway(t, NewMemoryJournal(), map[Kind]Executor{KindDatabaseRepair: exec})
		weak := newGateway(t, NewMemoryJournal(), map[Kind]Executor{KindDatabaseRepair: exec})
		weak.policy = func(k Kind) (Policy, bool) { p, ok := PolicyFor(k); return m.weaken(p), ok }
		req := baseRequest(t, KindDatabaseRepair, "key-mut-"+name)
		m.break_(&req)
		if _, err := strong.Submit(ctx, req); err == nil {
			t.Errorf("%s: the real policy admitted a request missing that guard", name)
		}
		req.JIT = func() *jit.Grant {
			if req.JIT.Role == jit.RoleSupportReadOnly {
				return grantFor(t, "operator:ana", jit.RoleSupportReadOnly, KindDatabaseRepair)
			}
			return grantFor(t, "operator:ana", jit.RoleIntegrityRepair, KindDatabaseRepair)
		}()
		if _, err := weak.Submit(ctx, req); err != nil {
			t.Errorf("%s: the weakened policy still refused (%v), so the test would not catch the guard's removal", name, err)
		}
	}
	if _, ok := PolicyFor("SQL_CONSOLE"); ok {
		t.Error("an unknown kind has a policy")
	}
}

// TestTodo_WF_RUN_015_InterventionPolicies pins the authority each typed
// workflow intervention demands at the gateway: repair kinds need an
// integrity-repair grant, override an incident responder's, and every one of
// them dual control and a simulation; an override grant never authorizes a
// repair and a repair grant never authorizes an override.
func TestTodo_WF_RUN_015_InterventionPolicies(t *testing.T) {
	ctx := context.Background()
	repair := []Kind{KindWorkflowSkip, KindWorkflowSatisfy, KindWorkflowRewind, KindWorkflowCompensate, KindWorkflowSupersede, KindWorkflowReconcile}
	for _, k := range append(repair, KindWorkflowOverride) {
		p, ok := PolicyFor(k)
		if !ok || !p.Material || !p.DualControl || !p.SimulationRequired || len(p.Roles) != 1 {
			t.Fatalf("%s policy = %+v, %v; want a material dual-control simulated kind with one role", k, p, ok)
		}
		wantRole := jit.RoleIntegrityRepair
		if k == KindWorkflowOverride {
			wantRole = jit.RoleIncidentResponder
		}
		if p.Roles[0] != wantRole {
			t.Fatalf("%s role = %s, want %s", k, p.Roles[0], wantRole)
		}
	}
	exec := &counting{}
	gw := newGateway(t, NewMemoryJournal(), map[Kind]Executor{KindWorkflowOverride: exec, KindWorkflowSkip: exec})

	override := baseRequest(t, KindWorkflowOverride, "override-with-repair-grant")
	if _, err := gw.Submit(ctx, override); CodeOf(err) != CodeAuthorityMismatch {
		t.Fatalf("override under an integrity-repair grant = %v, want %s", err, CodeAuthorityMismatch)
	}
	skip := baseRequest(t, KindWorkflowSkip, "skip-with-override-grant")
	skip.JIT = grantFor(t, "operator:ana", jit.RoleIncidentResponder, KindWorkflowSkip)
	if _, err := gw.Submit(ctx, skip); CodeOf(err) != CodeAuthorityMismatch {
		t.Fatalf("skip under an incident-responder grant = %v, want %s", err, CodeAuthorityMismatch)
	}
	skip = baseRequest(t, KindWorkflowSkip, "skip-without-second")
	skip.SecondApprover = ""
	if _, err := gw.Submit(ctx, skip); CodeOf(err) != CodeDualControlRequired {
		t.Fatalf("skip without dual control = %v", err)
	}
	override = baseRequest(t, KindWorkflowOverride, "override-governed")
	override.JIT = grantFor(t, "operator:ana", jit.RoleIncidentResponder, KindWorkflowOverride)
	if r, err := gw.Submit(ctx, override); err != nil || r.Outcome != OutcomeApplied || exec.applied.Load() != 1 {
		t.Fatalf("governed override = %+v, %v (applied %d)", r, err, exec.applied.Load())
	}
}
