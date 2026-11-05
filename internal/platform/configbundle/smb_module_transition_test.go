// TestTodo_SMB_SETUP_011 covers SMB-SETUP-011: suspending and
// re-enabling optional business modules with explicit outstanding-obligation
// handling. Transition plans pin the activation revision, inventory owned
// obligations through registered domain providers, bind approval to an exact
// impact snapshot, refuse dependent-consumer violations (or propose a safe
// order), and re-enable as a new revision without replaying old work.
package configbundle

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// stubObligationProvider is a hermetic ObligationProvider for tests. The
// inventory closure may mutate between calls to simulate obligation drift.
type stubObligationProvider struct {
	domain    string
	inventory func(scope Scope, moduleID string) ([]ObligationRef, error)
}

func (s stubObligationProvider) Domain() string { return s.domain }

func (s stubObligationProvider) Inventory(scope Scope, moduleID string) ([]ObligationRef, error) {
	return s.inventory(scope, moduleID)
}

func smbTransitionManifests() []BusinessModuleManifest {
	return []BusinessModuleManifest{
		{
			ModuleID: "party", Version: "1.0.0", Owner: "party",
			Capabilities: []string{"hcmnext.party.read@1"},
			Pages:        []ModulePage{{Route: "/workspace/app/party", Permission: "hcmnext.party.read"}},
		},
		{
			ModuleID: "job", Version: "1.0.0", Owner: "job",
			Dependencies: []string{"party"},
			Capabilities: []string{"hcmnext.job.read@1"},
			Pages:        []ModulePage{{Route: "/workspace/app/job", Permission: "hcmnext.job.read"}},
		},
		{
			ModuleID: "inventory", Version: "1.0.0", Owner: "stock",
			Capabilities: []string{"hcmnext.stock.read@1"},
			Pages:        []ModulePage{{Route: "/workspace/app/stock", Permission: "hcmnext.stock.read"}},
		},
	}
}

func smbTransitionKnown() map[string]bool {
	return map[string]bool{
		"hcmnext.party.read@1": true,
		"hcmnext.job.read@1":   true,
		"hcmnext.stock.read@1": true,
	}
}

func mustTransitionRegistry(t *testing.T) (*ModuleRegistry, []BusinessModuleManifest) {
	t.Helper()
	manifests := smbTransitionManifests()
	registry, err := CompileBusinessModules(manifests, smbTransitionKnown())
	if err != nil {
		t.Fatalf("CompileBusinessModules err=%v", err)
	}
	return registry, manifests
}

func mustTransitionActivate(t *testing.T, registry *ModuleRegistry, scope Scope, moduleID string, predecessor uint64) ModuleActivationRevision {
	t.Helper()
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID:            moduleID,
		ExpectedPredecessor: predecessor,
		BundleDigest:        smbCoreDigest("bundle:" + moduleID),
		Scope:               scope,
		SchemaReady:         map[string]bool{"party": true, "job": true, "inventory": true},
	})
	if err != nil {
		t.Fatalf("Activate(%s) err=%v", moduleID, err)
	}
	return rev
}

func staticProvider(domain string, obligations []ObligationRef) stubObligationProvider {
	return stubObligationProvider{
		domain: domain,
		inventory: func(Scope, string) ([]ObligationRef, error) {
			return append([]ObligationRef(nil), obligations...), nil
		},
	}
}

// Acceptance 1: suspending inventory with a reservation and an ambiguous
// print blocks new issue/print starts, retains the reservation, keeps
// history readable, and leaves print repair available.
func TestTodo_SMB_SETUP_011(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	active := mustTransitionActivate(t, registry, scope, "inventory", 0)

	reservation := ObligationRef{
		Domain: "stock", Kind: "reservation", Ref: "RSV-1001",
		Status: "outstanding", ResponsibleRole: "warehouse-clerk",
		Allowed: []ObligationDisposition{ObligationRetain},
	}
	ambiguousPrint := ObligationRef{
		Domain: "print", Kind: "label-print", Ref: "PRN-77",
		Status: "uncertain", ResponsibleRole: "warehouse-clerk",
		Class: EffectClassQueued,
		Allowed: []ObligationDisposition{
			ObligationQuarantine, ObligationRepair, ObligationCancel,
		},
	}
	if err := planner.RegisterObligationProvider(staticProvider("stock", []ObligationRef{reservation})); err != nil {
		t.Fatalf("register stock provider err=%v", err)
	}
	if err := planner.RegisterObligationProvider(staticProvider("print", []ObligationRef{ambiguousPrint})); err != nil {
		t.Fatalf("register print provider err=%v", err)
	}

	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "stocktake freeze", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	if plan.State != TransitionPlanned {
		t.Fatalf("preview state=%s, want planned", plan.State)
	}
	if plan.PinnedRevision != active.Revision {
		t.Fatalf("pinned revision=%d, want active %d", plan.PinnedRevision, active.Revision)
	}
	if len(plan.Obligations) != 2 {
		t.Fatalf("obligations=%d, want 2 (reservation + print)", len(plan.Obligations))
	}
	if !strings.HasPrefix(plan.ImpactDigest, "sha256:") {
		t.Fatalf("impact digest %q is not pinned", plan.ImpactDigest)
	}

	approved, err := planner.Approve(TransitionApproveRequest{
		Scope: scope, PlanID: plan.PlanID,
		ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director",
	})
	if err != nil {
		t.Fatalf("Approve err=%v", err)
	}
	if approved.State != TransitionApproved {
		t.Fatalf("approved state=%s, want approved", approved.State)
	}
	applied, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID})
	if err != nil {
		t.Fatalf("Apply err=%v", err)
	}
	if applied.State != TransitionApplied {
		t.Fatalf("applied state=%s, want applied", applied.State)
	}

	// New issue/print starts stop at the transition fence.
	if err := registry.AuthorizeWrite(scope, "inventory", active.Revision); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("stale pinned write err=%v, want revision conflict at fence", err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", applied.AppliedRevision); !errors.Is(err, ErrModuleWritesBlocked) {
		t.Fatalf("post-suspend write err=%v, want writes blocked", err)
	}
	// History stays readable and the reservation is retained, not released.
	if err := registry.AuthorizeRead(scope, "inventory"); err != nil {
		t.Fatalf("post-suspend history read err=%v", err)
	}
	loaded, ok := planner.Load(scope, plan.PlanID)
	if !ok {
		t.Fatal("applied plan is not loadable")
	}
	if loaded.Obligations[1].Receipt != "" || loaded.Obligations[1].Action != "" {
		t.Fatalf("reservation auto-resolved: %+v", loaded.Obligations[1])
	}
	// Ambiguous print repair stays available through observed-effect recovery.
	dedup, err := registry.CompleteObservedEffect(scope, "inventory", active.Revision, "PRN-77")
	if err != nil || dedup {
		t.Fatalf("print repair dedup=%v err=%v, want first completion accepted", dedup, err)
	}
	repaired, err := planner.RecordObligationResult(scope, plan.PlanID, "print", "label-print", "PRN-77", string(ObligationRepair), "reprint-ok")
	if err != nil {
		t.Fatalf("RecordObligationResult err=%v", err)
	}
	if repaired.Obligations[0].Receipt != "reprint-ok" {
		t.Fatalf("repair receipt not recorded: %+v", repaired.Obligations[0])
	}
	view := TransitionReview(repaired)
	if !strings.Contains(view.CompletionSummary, "New activity stopped") {
		t.Fatalf("completion summary %q hides the fence outcome", view.CompletionSummary)
	}
	if strings.Contains(view.CompletionSummary, "All work cancelled") {
		t.Fatalf("completion summary %q claims false cancellation", view.CompletionSummary)
	}
}

// Acceptance 2: suspending party while job requires it refuses with the
// named consumer and a go-to reference; the proposed safe order suspends
// the dependent first with no broken foreign refs.
func TestTodo_SMB_SETUP_011_DependencyRefusalOrSafeOrder(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "party", 0)
	mustTransitionActivate(t, registry, scope, "job", 0)

	blocked, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "party", Kind: TransitionSuspend,
		Reason: "party cleanup", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	if blocked.State != TransitionBlocked {
		t.Fatalf("state=%s, want blocked while job consumes party", blocked.State)
	}
	if len(blocked.Blockers) != 1 || blocked.Blockers[0].ModuleID != "job" || blocked.Blockers[0].GoToModule != "job" {
		t.Fatalf("blockers=%+v, want the named job consumer with go-to", blocked.Blockers)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: blocked.PlanID, ImpactDigest: blocked.ImpactDigest, ApprovedBy: "ops.director"}); !errors.Is(err, ErrTransitionBlocked) {
		t.Fatalf("approve blocked plan err=%v, want transition blocked", err)
	}

	order, err := planner.PreviewOrder(TransitionOrderRequest{Scope: scope, ModuleIDs: []string{"party", "job"}, Kind: TransitionSuspend})
	if err != nil {
		t.Fatalf("PreviewOrder err=%v", err)
	}
	if len(order) != 2 || order[0] != "job" || order[1] != "party" {
		t.Fatalf("suspend order=%v, want dependents first", order)
	}
	for _, moduleID := range order {
		plan, err := planner.Preview(TransitionPreviewRequest{
			Scope: scope, ModuleID: moduleID, Kind: TransitionSuspend,
			Reason: "ordered suspend", RequestedBy: "ops.lead",
		})
		if err != nil || plan.State != TransitionPlanned {
			t.Fatalf("Preview(%s) state=%s err=%v, want planned", moduleID, plan.State, err)
		}
		if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
			t.Fatalf("Approve(%s) err=%v", moduleID, err)
		}
		if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID}); err != nil {
			t.Fatalf("Apply(%s) err=%v", moduleID, err)
		}
	}
	// No broken foreign refs: both histories stay readable after the
	// ordered suspension.
	if len(registry.History(scope, "party")) != 2 || len(registry.History(scope, "job")) != 2 {
		t.Fatal("ordered suspend trimmed module history")
	}
}

// Acceptance 3: re-enable after suspension is a new revision, retains
// idempotency tombstones, and never replays prior payments/prints.
func TestTodo_SMB_SETUP_011_ReenableIsNewRevisionWithoutReplay(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	active := mustTransitionActivate(t, registry, scope, "inventory", 0)
	if err := planner.RegisterObligationProvider(staticProvider("pay", []ObligationRef{{
		Domain: "pay", Kind: "payment", Ref: "PAY-9",
		Status: "entered", ResponsibleRole: "billing-clerk",
		Class: EffectClassEntered, Allowed: []ObligationDisposition{ObligationRetain},
	}})); err != nil {
		t.Fatalf("register pay provider err=%v", err)
	}
	dedup, err := registry.CompleteObservedEffect(scope, "inventory", active.Revision, "PAY-9")
	if err != nil || dedup {
		t.Fatalf("pre-suspend completion dedup=%v err=%v", dedup, err)
	}

	suspend, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "pause billing", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("suspend Preview err=%v", err)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: suspend.PlanID, ImpactDigest: suspend.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
		t.Fatalf("suspend Approve err=%v", err)
	}
	suspended, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: suspend.PlanID})
	if err != nil {
		t.Fatalf("suspend Apply err=%v", err)
	}

	ready := map[string]bool{"party": true, "job": true, "inventory": true}
	reenable, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionReenable,
		Reason: "resume billing", RequestedBy: "ops.lead", SchemaReady: ready,
	})
	if err != nil {
		t.Fatalf("re-enable Preview err=%v", err)
	}
	if reenable.State != TransitionPlanned {
		t.Fatalf("re-enable state=%s, want planned", reenable.State)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: reenable.PlanID, ImpactDigest: reenable.ImpactDigest, ApprovedBy: "ops.director", SchemaReady: ready}); err != nil {
		t.Fatalf("re-enable Approve err=%v", err)
	}
	resumed, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: reenable.PlanID, BundleDigest: smbCoreDigest("bundle:inventory:v2")})
	if err != nil {
		t.Fatalf("re-enable Apply err=%v", err)
	}
	if resumed.AppliedRevision != suspended.AppliedRevision+1 {
		t.Fatalf("re-enable revision=%d, want a new revision after %d", resumed.AppliedRevision, suspended.AppliedRevision)
	}
	// Tombstones survive: the prior payment still deduplicates and the old
	// revision can no longer authorize writes.
	if dedup, err := registry.CompleteObservedEffect(scope, "inventory", active.Revision, "PAY-9"); err != nil || !dedup {
		t.Fatalf("post re-enable completion dedup=%v err=%v, want tombstone retained", dedup, err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", active.Revision); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("old revision write err=%v, want revision conflict (no replay)", err)
	}
	loaded, _ := planner.Load(scope, reenable.PlanID)
	if loaded.Obligations[0].Receipt != "" {
		t.Fatalf("re-enable auto-resolved obligation: %+v", loaded.Obligations[0])
	}
	if err := registry.AuthorizeWrite(scope, "inventory", resumed.AppliedRevision); err != nil {
		t.Fatalf("new revision write err=%v, want authorized", err)
	}
}

// Acceptance 4: an obligation change during review makes the approval
// stale; it is rejected before the activation pointer moves.
func TestTodo_SMB_SETUP_011_StaleApprovalRejected(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "inventory", 0)

	extra := false
	stock := stubObligationProvider{
		domain: "stock",
		inventory: func(Scope, string) ([]ObligationRef, error) {
			out := []ObligationRef{{
				Domain: "stock", Kind: "reservation", Ref: "RSV-1",
				Status: "outstanding", ResponsibleRole: "clerk",
				Allowed: []ObligationDisposition{ObligationRetain},
			}}
			if extra {
				out = append(out, ObligationRef{
					Domain: "stock", Kind: "reservation", Ref: "RSV-2",
					Status: "outstanding", ResponsibleRole: "clerk",
					Allowed: []ObligationDisposition{ObligationRetain},
				})
			}
			return out, nil
		},
	}
	if err := planner.RegisterObligationProvider(stock); err != nil {
		t.Fatalf("register provider err=%v", err)
	}
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "review race", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	extra = true // obligation arrives while the approval is in flight.
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"}); !errors.Is(err, ErrTransitionStale) {
		t.Fatalf("Approve err=%v, want stale transition", err)
	}
	if view := registry.ResolveView(scope, "inventory", nil); view.EffectiveStatus != string(ModuleStateEnabled) || view.ActiveRevision != 1 {
		t.Fatalf("activation moved during stale approval: %+v", view)
	}
	// A fresh preview binds the new impact and completes normally.
	fresh, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "review race retry", RequestedBy: "ops.lead",
	})
	if err != nil || len(fresh.Obligations) != 2 {
		t.Fatalf("fresh Preview obligations=%d err=%v, want 2", len(fresh.Obligations), err)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: fresh.PlanID, ImpactDigest: fresh.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
		t.Fatalf("fresh Approve err=%v", err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: fresh.PlanID}); err != nil {
		t.Fatalf("fresh Apply err=%v", err)
	}
}

// Entered physical/payment effects must never be promised cancellation:
// a provider offering cancel on an entered effect blocks the plan.
func TestTodo_SMB_SETUP_011_EnteredEffectsCannotCancel(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "inventory", 0)
	if err := planner.RegisterObligationProvider(staticProvider("pay", []ObligationRef{{
		Domain: "pay", Kind: "payment", Ref: "PAY-1",
		Status: "entered", ResponsibleRole: "billing-clerk",
		Class:   EffectClassEntered,
		Allowed: []ObligationDisposition{ObligationCancel},
	}})); err != nil {
		t.Fatalf("register provider err=%v", err)
	}
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "entered cancel probe", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	if plan.State != TransitionBlocked {
		t.Fatalf("state=%s, want blocked for entered-effect cancel", plan.State)
	}
	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0].Reason, "PAY-1") {
		t.Fatalf("blockers=%+v, want the named entered effect", plan.Blockers)
	}
}

// Re-enable waits on readiness: missing schema keeps the plan waiting,
// approval binds readiness, and apply executes once ready.
func TestTodo_SMB_SETUP_011_ReenableWaitsOnReadiness(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "party", 0)
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "party", Kind: TransitionSuspend,
		Reason: "pause", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("suspend Preview err=%v", err)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
		t.Fatalf("suspend Approve err=%v", err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID}); err != nil {
		t.Fatalf("suspend Apply err=%v", err)
	}

	waiting, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "party", Kind: TransitionReenable,
		Reason: "resume", RequestedBy: "ops.lead", SchemaReady: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("re-enable Preview err=%v", err)
	}
	if waiting.State != TransitionWaiting {
		t.Fatalf("state=%s, want waiting on schema readiness", waiting.State)
	}
	if len(waiting.Compatibility) == 0 {
		t.Fatal("waiting plan carries no compatibility findings")
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: waiting.PlanID, ImpactDigest: waiting.ImpactDigest, ApprovedBy: "ops.director"}); !errors.Is(err, ErrTransitionConflict) {
		t.Fatalf("early Approve err=%v, want readiness conflict", err)
	}
	ready := map[string]bool{"party": true, "job": true, "inventory": true}
	approved, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: waiting.PlanID, ImpactDigest: waiting.ImpactDigest, ApprovedBy: "ops.director", SchemaReady: ready})
	if err != nil || approved.State != TransitionApproved {
		t.Fatalf("Approve state=%s err=%v, want approved", approved.State, err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: waiting.PlanID, BundleDigest: smbCoreDigest("bundle:party:v2")}); err != nil {
		t.Fatalf("Apply err=%v", err)
	}
}

// Applying twice is a conflict, not a second suspension revision.
func TestTodo_SMB_SETUP_011_DoubleApplyConflicts(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "inventory", 0)
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "double apply", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
		t.Fatalf("Approve err=%v", err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID}); err != nil {
		t.Fatalf("first Apply err=%v", err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID}); !errors.Is(err, ErrTransitionConflict) {
		t.Fatalf("second Apply err=%v, want state conflict", err)
	}
	if len(registry.History(scope, "inventory")) != 2 {
		t.Fatal("double apply recorded a second revision")
	}
}

func TestTodo_SMB_SETUP_011_PreviewValidation(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	if _, err := planner.Preview(TransitionPreviewRequest{Scope: scope, ModuleID: "nope", Kind: TransitionSuspend, Reason: "x"}); !errors.Is(err, ErrModuleUnknown) {
		t.Fatalf("unknown module err=%v, want module unknown", err)
	}
	if _, err := planner.Preview(TransitionPreviewRequest{Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend, Reason: "x"}); !errors.Is(err, ErrModuleNotActive) {
		t.Fatalf("never-activated err=%v, want not active", err)
	}
	mustTransitionActivate(t, registry, scope, "inventory", 0)
	if _, err := planner.Preview(TransitionPreviewRequest{Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty reason err=%v, want invalid request", err)
	}
	if _, err := planner.Preview(TransitionPreviewRequest{Scope: scope, ModuleID: "inventory", Kind: "retire"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad kind err=%v, want invalid request", err)
	}
	if err := planner.RegisterObligationProvider(staticProvider("stock", nil)); err != nil {
		t.Fatalf("register err=%v", err)
	}
	if err := planner.RegisterObligationProvider(staticProvider("stock", nil)); err == nil {
		t.Fatal("duplicate provider domain accepted")
	}
	if err := planner.RegisterObligationProvider(stubObligationProvider{}); err == nil {
		t.Fatal("empty provider domain accepted")
	}
}

// INTEGRATION: authorized write/read round trips survive a restart
// carried by Export/Import snapshots; the resumed planner keeps plan
// state, obligations and tombstones without duplicating logical work.
func TestTodo_SMB_SETUP_011_Integration(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	active := mustTransitionActivate(t, registry, scope, "inventory", 0)
	dedup, err := registry.CompleteObservedEffect(scope, "inventory", active.Revision, "PRN-1")
	if err != nil || dedup {
		t.Fatalf("completion dedup=%v err=%v", dedup, err)
	}
	if err := planner.RegisterObligationProvider(staticProvider("stock", []ObligationRef{{
		Domain: "stock", Kind: "reservation", Ref: "RSV-7",
		Status: "outstanding", ResponsibleRole: "clerk",
		Allowed: []ObligationDisposition{ObligationRetain},
	}})); err != nil {
		t.Fatalf("register err=%v", err)
	}
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "restart drill", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	approved, err := planner.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"})
	if err != nil {
		t.Fatalf("Approve err=%v", err)
	}

	// Restart: fresh registry + planner restore from snapshots.
	registryBytes, err := registry.Export()
	if err != nil {
		t.Fatalf("registry Export err=%v", err)
	}
	planBytes, err := planner.Export()
	if err != nil {
		t.Fatalf("planner Export err=%v", err)
	}
	registry2, err := CompileBusinessModules(manifests, smbTransitionKnown())
	if err != nil {
		t.Fatalf("recompile err=%v", err)
	}
	if err := registry2.Import(registryBytes); err != nil {
		t.Fatalf("registry Import err=%v", err)
	}
	planner2 := NewTransitionPlanner(registry2, manifests)
	if err := planner2.RegisterObligationProvider(staticProvider("stock", []ObligationRef{{
		Domain: "stock", Kind: "reservation", Ref: "RSV-7",
		Status: "outstanding", ResponsibleRole: "clerk",
		Allowed: []ObligationDisposition{ObligationRetain},
	}})); err != nil {
		t.Fatalf("re-register err=%v", err)
	}
	if err := planner2.Import(planBytes); err != nil {
		t.Fatalf("planner Import err=%v", err)
	}
	restored, ok := planner2.Load(scope, plan.PlanID)
	if !ok || restored.State != TransitionApproved || restored.ApprovalDigest != approved.ApprovalDigest {
		t.Fatalf("restored plan=%+v ok=%v, want the approved plan", restored, ok)
	}
	applied, err := planner2.Apply(TransitionApplyRequest{Scope: scope, PlanID: plan.PlanID})
	if err != nil || applied.State != TransitionApplied {
		t.Fatalf("post-restart Apply state=%s err=%v", applied.State, err)
	}
	if dedup, err := registry2.CompleteObservedEffect(scope, "inventory", active.Revision, "PRN-1"); err != nil || !dedup {
		t.Fatalf("post-restart completion dedup=%v err=%v, want tombstone", dedup, err)
	}
	if err := registry2.AuthorizeRead(scope, "inventory"); err != nil {
		t.Fatalf("post-restart history read err=%v", err)
	}
}

// SECURITY: two scopes stay isolated, forged refs and cross-scope
// requests share one generic refusal, and provider failures fail closed.
func TestTodo_SMB_SETUP_011_Security(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	tenantA := Scope{TenantID: "tenant-a"}
	tenantB := Scope{TenantID: "tenant-b"}
	mustTransitionActivate(t, registry, tenantA, "inventory", 0)
	mustTransitionActivate(t, registry, tenantB, "inventory", 0)
	if err := planner.RegisterObligationProvider(staticProvider("stock", []ObligationRef{{
		Domain: "stock", Kind: "reservation", Ref: "RSV-A",
		Status: "outstanding", ResponsibleRole: "clerk",
		Allowed: []ObligationDisposition{ObligationRetain},
	}})); err != nil {
		t.Fatalf("register err=%v", err)
	}
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: tenantA, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "scope drill", RequestedBy: "a.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	// Cross-scope load/approve/apply share the generic not-found refusal.
	if _, ok := planner.Load(tenantB, plan.PlanID); ok {
		t.Fatal("tenant-b loaded tenant-a plan")
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: tenantB, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "b.lead"}); !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("cross-scope Approve err=%v, want generic not-found", err)
	}
	if _, err := planner.Apply(TransitionApplyRequest{Scope: tenantB, PlanID: plan.PlanID}); !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("cross-scope Apply err=%v, want generic not-found", err)
	}
	if _, err := planner.Approve(TransitionApproveRequest{Scope: tenantA, PlanID: "tr-999999", ImpactDigest: plan.ImpactDigest, ApprovedBy: "a.lead"}); !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("forged plan Approve err=%v, want generic not-found", err)
	}
	if _, err := planner.RecordObligationResult(tenantB, plan.PlanID, "stock", "reservation", "RSV-A", string(ObligationRetain), "r"); !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("cross-scope receipt err=%v, want generic not-found", err)
	}
	if _, err := planner.RecordObligationResult(tenantA, plan.PlanID, "stock", "reservation", "RSV-FORGED", string(ObligationRetain), "r"); !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("forged obligation err=%v, want generic not-found", err)
	}
	// Disallowed actions are refused even for the owning scope.
	if _, err := planner.RecordObligationResult(tenantA, plan.PlanID, "stock", "reservation", "RSV-A", string(ObligationCancel), "r"); !errors.Is(err, ErrTransitionInvalid) {
		t.Fatalf("disallowed action err=%v, want invalid", err)
	}
	// Tenant B is unaffected: no plan, still enabled.
	if view := registry.ResolveView(tenantB, "inventory", nil); view.EffectiveStatus != string(ModuleStateEnabled) {
		t.Fatalf("tenant-b status=%s, want enabled", view.EffectiveStatus)
	}
}

// FAULT: a failing obligation provider fails the preview closed: no
// undercounted plan is stored and the registry is untouched.
func TestTodo_SMB_SETUP_011_Fault(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	active := mustTransitionActivate(t, registry, scope, "inventory", 0)
	boom := errors.New("stock inventory unavailable")
	if err := planner.RegisterObligationProvider(stubObligationProvider{
		domain: "stock",
		inventory: func(Scope, string) ([]ObligationRef, error) {
			return nil, boom
		},
	}); err != nil {
		t.Fatalf("register err=%v", err)
	}
	if _, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "fault drill", RequestedBy: "ops.lead",
	}); !errors.Is(err, boom) {
		t.Fatalf("Preview err=%v, want the provider failure closed", err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", active.Revision); err != nil {
		t.Fatalf("registry touched by failed preview: %v", err)
	}
	if len(registry.History(scope, "inventory")) != 1 {
		t.Fatal("failed preview recorded activation history")
	}
}

// RECOVERY: tampered snapshots are refused whole; a valid snapshot
// resumes without phantom plans or scope-swapped authority.
func TestTodo_SMB_SETUP_011_Recovery(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "inventory", 0)
	plan, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "inventory", Kind: TransitionSuspend,
		Reason: "recovery drill", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	raw, err := planner.Export()
	if err != nil {
		t.Fatalf("Export err=%v", err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode snapshot err=%v", err)
	}
	_ = snapshot

	fresh := func() *TransitionPlanner { return NewTransitionPlanner(registry, manifests) }
	// Scope-swapped map key: tenant-a plan filed under tenant-b.
	var decoded struct {
		Plans map[string]ModuleTransitionPlan `json:"plans"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode plans err=%v", err)
	}
	swapped := map[string]ModuleTransitionPlan{}
	for id, stored := range decoded.Plans {
		_ = id
		swapped["tenant-b\x00\x00"+stored.PlanID] = stored
	}
	swappedRaw, _ := json.Marshal(map[string]any{"plans": swapped})
	if err := fresh().Import(swappedRaw); err == nil {
		t.Fatal("scope-swapped snapshot accepted")
	}
	// Unknown state + malformed digest are refused.
	tampered := map[string]ModuleTransitionPlan{}
	for key, stored := range decoded.Plans {
		stored.State = "evaporated"
		stored.ImpactDigest = "nope"
		tampered[key] = stored
	}
	tamperedRaw, _ := json.Marshal(map[string]any{"plans": tampered})
	if err := fresh().Import(tamperedRaw); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
	// The valid snapshot imports and the plan resumes.
	if err := fresh().Import(raw); err != nil {
		t.Fatalf("valid Import err=%v", err)
	}
	resumed := fresh()
	if err := resumed.Import(raw); err != nil {
		t.Fatalf("valid Import err=%v", err)
	}
	loaded, ok := resumed.Load(scope, plan.PlanID)
	if !ok || loaded.State != TransitionPlanned || len(loaded.Obligations) != 0 {
		t.Fatalf("resumed plan=%+v ok=%v", loaded, ok)
	}
	if _, err := resumed.Approve(TransitionApproveRequest{Scope: scope, PlanID: plan.PlanID, ImpactDigest: plan.ImpactDigest, ApprovedBy: "ops.director"}); err != nil {
		t.Fatalf("post-import Approve err=%v", err)
	}
}

// BROWSER: the review contract groups obligations into New activity,
// Work in progress and Historical access, names blocked consumers with
// go-to refs, and reports re-enable findings for the served page.
func TestTodo_SMB_SETUP_011_Browser(t *testing.T) {
	registry, manifests := mustTransitionRegistry(t)
	planner := NewTransitionPlanner(registry, manifests)
	scope := Scope{TenantID: "tenant-a"}
	mustTransitionActivate(t, registry, scope, "party", 0)
	mustTransitionActivate(t, registry, scope, "job", 0)
	if err := planner.RegisterObligationProvider(staticProvider("jobq", []ObligationRef{{
		Domain: "jobq", Kind: "queued-dispatch", Ref: "DSP-3",
		Status: "queued", ResponsibleRole: "dispatcher",
		Class:   EffectClassQueued,
		Allowed: []ObligationDisposition{ObligationDrain, ObligationCancel},
	}})); err != nil {
		t.Fatalf("register err=%v", err)
	}
	blocked, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "party", Kind: TransitionSuspend,
		Reason: "review drill", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("Preview err=%v", err)
	}
	view := TransitionReview(blocked)
	if len(view.NewActivity) != 2 { // fence row + queued dispatch.
		t.Fatalf("new activity rows=%d, want fence + queued", len(view.NewActivity))
	}
	if view.NewActivity[1].Reference != "DSP-3" || len(view.NewActivity[1].Actions) != 2 {
		t.Fatalf("queued row=%+v, want ref + allowed actions", view.NewActivity[1])
	}
	if len(view.BlockedConsumers) != 1 || view.BlockedConsumers[0].GoToModule != "job" {
		t.Fatalf("blocked consumers=%+v, want job go-to", view.BlockedConsumers)
	}
	if !view.HistoricalAccess.HistoryReadable || !view.HistoricalAccess.ExportAvailable {
		t.Fatalf("historical access=%+v, want readable + export", view.HistoricalAccess)
	}

	suspend, err := planner.Preview(TransitionPreviewRequest{
		Scope: scope, ModuleID: "job", Kind: TransitionSuspend,
		Reason: "review drill", RequestedBy: "ops.lead",
	})
	if err != nil {
		t.Fatalf("job Preview err=%v", err)
	}
	jobView := TransitionReview(suspend)
	if len(jobView.WorkInProgress) != 0 {
		t.Fatalf("work in progress=%+v, want empty for queued-only", jobView.WorkInProgress)
	}
	if suspend.RetainedRevisions != 1 {
		t.Fatalf("retained revisions=%d, want 1", suspend.RetainedRevisions)
	}
}
