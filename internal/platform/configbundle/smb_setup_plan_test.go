package configbundle

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
)

func smbSetupModules(t *testing.T) *ModuleRegistry {
	t.Helper()
	manifests := []BusinessModuleManifest{
		{ModuleID: "party", Version: "1.0.0", Owner: "crm", Capabilities: []string{"crm.party.v1"}, Pages: []ModulePage{{Route: "/party", Permission: "crm.party.read"}}},
		{ModuleID: "inventory", Version: "1.0.0", Owner: "stock", Dependencies: []string{"party"}, Capabilities: []string{"stock.inventory.v1"}, Pages: []ModulePage{{Route: "/inventory", Permission: "stock.inventory.read"}}},
	}
	registry, err := CompileBusinessModules(manifests, map[string]bool{"crm.party.v1": true, "stock.inventory.v1": true})
	if err != nil {
		t.Fatalf("compile modules: %v", err)
	}
	return registry
}

// smbSetupApproval builds a real signed starter baseline and approves it
// through the existing promotion control plane. The returned pin is what a
// setup plan cites; signerKeyID is what a distinct-approval check must
// differ from.
func smbSetupApproval(t *testing.T, approvals *promotion.Registry, id, signer, approver string, at time.Time) (promotion.Approval, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := config.Bundle{BundleID: id, ManifestVersion: 1, CompatibilityRange: "^1", Signer: signer, Provenance: "smb-setup-test",
		Dependencies: []config.Dependency{{Kind: config.DependencySchema, Name: "worker", Version: "1.0.0", Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}
	sb, err := config.SignBundle(bundle, signer, priv)
	if err != nil {
		t.Fatalf("sign baseline: %v", err)
	}
	pkg := promotion.Package{ID: id, Environment: "production", Bundle: sb, PublicKey: pub}
	if err := approvals.Put(pkg); err != nil {
		t.Fatalf("put baseline: %v", err)
	}
	if _, err := approvals.Simulate(id); err != nil {
		t.Fatalf("simulate baseline: %v", err)
	}
	approval, err := approvals.Approve(id, approver, at)
	if err != nil {
		t.Fatalf("approve baseline: %v", err)
	}
	return approval, sb.SignerKeyID
}

func smbSetupSteps() []SetupStep {
	return []SetupStep{
		{ID: "company-profile", Title: "Confirm company profile", Required: true, Capability: "setup.profile"},
		{ID: "role-review", Title: "Assign payroll roles", Required: true, DependsOn: []string{"company-profile"}, Capability: "setup.roles"},
		{ID: "import-contacts", Title: "Import contacts (optional)", Required: false, ModuleID: "party", Capability: "setup.import"},
	}
}

// countingCapability is the bounded concrete owner-step adapter: it records
// verified receipts and counts invocations so fault tests prove a crashed
// call never executes twice.
type countingCapability struct {
	mu       sync.Mutex
	calls    map[string]int
	unavail  map[string]bool
	receipts map[string]StepReceipt
}

func (c *countingCapability) Execute(step SetupStep, in StepInput) (StepReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = make(map[string]int)
		c.receipts = make(map[string]StepReceipt)
	}
	c.calls[step.ID]++
	if c.unavail[step.ID] {
		return StepReceipt{}, ErrStepCapabilityUnavailable
	}
	receipt := StepReceipt{ReceiptRef: "receipt:" + in.PlanID + ":" + step.ID, Title: step.Title, EffectID: "effect:" + step.ID, At: in.At}
	c.receipts[step.ID] = receipt
	return receipt, nil
}

func (c *countingCapability) invoked(stepID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[stepID]
}

// errSetupSimulatedCrash stands in for a server kill between the owner
// receipt and the setup response: recorded state survives, the response is
// lost, and replay must converge without a second effect.
var errSetupSimulatedCrash = errors.New("configbundle: simulated setup crash")

func smbSetupCreate(t *testing.T, planner *SetupPlanner, approvals *promotion.Registry, tenant, planID, admin string, at time.Time) SetupPlanRevision {
	t.Helper()
	approval, _ := smbSetupApproval(t, approvals, "starter-"+planID, "release", "release-authority", at)
	plan, err := planner.Create(SetupPlanRequest{
		PlanID: planID, Scope: Scope{TenantID: tenant}, Admin: admin,
		Baseline: BaselinePin{PackageID: approval.PackageID, Digest: approval.Digest, Scope: Scope{TenantID: tenant}, Approver: approval.Approver, ExpiresAt: at.Add(24 * time.Hour)},
		Modules:  []string{"party", "inventory"}, Steps: smbSetupSteps(),
		RequiredChecks: []string{"role:payroll-admin", "policy:retention"},
		At:             at,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

// TestTodo_SMB_SETUP_001 proves the setup-plan lifecycle: bounded creation,
// receipt-backed advance through a required/optional DAG, skips with reason
// for optional steps only, verified-outcome READY, and typed refusals for
// every shortcut (self-approval, tampered pins, non-admin writes, abandon
// bypass).
func TestTodo_SMB_SETUP_001(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-1", "ada.admin", at)
	if plan.State != SetupStateDraft || plan.Revision != 1 {
		t.Fatalf("fresh plan = %+v, want DRAFT at revision 1", plan)
	}

	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: plan.Revision, StepID: "role-review", IdempotencyKey: "k1", At: at, Caps: caps}); !errors.Is(err, ErrSetupBlocked) {
		t.Fatalf("advance before dependency = %v, want ErrSetupBlocked", err)
	}
	advanced, receipt, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "k1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("advance profile: %v", err)
	}
	if receipt.ReceiptRef == "" || advanced.StepState["company-profile"].Status != SetupStepVerified {
		t.Fatalf("advanced plan = %+v, want a verified profile step with receipt", advanced)
	}
	if advanced.State != SetupStateInProgress {
		t.Fatalf("plan state = %q, want IN_PROGRESS", advanced.State)
	}
	if _, err := planner.SkipStep(SetupSkipRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: advanced.Revision, StepID: "role-review", Reason: "busy"}); !errors.Is(err, ErrSetupSkipRefused) {
		t.Fatalf("skip required step err = %v, want ErrSetupSkipRefused", err)
	}
	if _, err := planner.SkipStep(SetupSkipRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: advanced.Revision, StepID: "import-contacts", Reason: ""}); !errors.Is(err, ErrSetupInvalid) {
		t.Fatalf("reasonless skip err = %v, want ErrSetupInvalid", err)
	}
	rolesDone, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: advanced.Revision, StepID: "role-review", IdempotencyKey: "k2", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("advance roles: %v", err)
	}
	waived, err := planner.SkipStep(SetupSkipRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: rolesDone.Revision, StepID: "import-contacts", Reason: "no legacy contacts"})
	if err != nil {
		t.Fatalf("skip optional: %v", err)
	}
	checked, err := planner.RecordCheck(SetupCheckRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: waived.Revision, CheckID: "role:payroll-admin", Passed: true, EvidenceRef: "evidence:roles", At: at})
	if err != nil {
		t.Fatalf("record check: %v", err)
	}
	if checked.State == SetupStateReady {
		t.Fatal("plan is READY with a required policy check still failing")
	}
	policyDone, err := planner.RecordCheck(SetupCheckRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: checked.Revision, CheckID: "policy:retention", Passed: true, EvidenceRef: "evidence:policy", At: at})
	if err != nil {
		t.Fatalf("record policy check: %v", err)
	}
	ready, err := planner.ActivateBaseline(Scope{TenantID: "tenant-a"}, "plan-1", "ada.admin", policyDone.Revision, at)
	if err != nil {
		t.Fatalf("activate baseline: %v", err)
	}
	if ready.State != SetupStateReady {
		t.Fatalf("plan state = %q, want READY after verified outcomes plus baseline", ready.State)
	}

	// Negative: self-approved custom bundle is refused by the reused
	// promotion guard, and the planner never sees it as an approval.
	custom, err := func() (promotion.Approval, error) {
		pub, priv, _ := ed25519.GenerateKey(nil)
		sb, err := config.SignBundle(config.Bundle{BundleID: "custom-1", ManifestVersion: 1, CompatibilityRange: "^1", Signer: "ada.admin", Provenance: "t",
			Dependencies: []config.Dependency{{Kind: config.DependencySchema, Name: "worker", Version: "1.0.0", Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}, "ada.admin", priv)
		if err != nil {
			return promotion.Approval{}, err
		}
		pkg := promotion.Package{ID: "custom-1", Environment: "production", Bundle: sb, PublicKey: pub}
		if err := approvals.Put(pkg); err != nil {
			return promotion.Approval{}, err
		}
		if _, err := approvals.Simulate("custom-1"); err != nil {
			return promotion.Approval{}, err
		}
		return approvals.Approve("custom-1", sb.SignerKeyID, at)
	}()
	if err == nil {
		t.Fatalf("self-approved custom bundle = %+v, want promotion refusal", custom)
	}
	if _, err := planner.Create(SetupPlanRequest{PlanID: "plan-evil", Scope: Scope{TenantID: "tenant-a"}, Admin: "ada.admin",
		Baseline: BaselinePin{PackageID: "custom-1", Digest: "forged", Scope: Scope{TenantID: "tenant-a"}, Approver: "ada.admin", ExpiresAt: at.Add(time.Hour)},
		Modules:  []string{"party"}, Steps: smbSetupSteps(), At: at}); !errors.Is(err, ErrSetupBaselineRefused) {
		t.Fatalf("unapproved baseline err = %v, want ErrSetupBaselineRefused", err)
	}

	abandoned, err := planner.Abandon(SetupAbandonRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: ready.Revision, Reason: "duplicate company", At: at})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if abandoned.State != SetupStateAbandoned {
		t.Fatalf("abandoned state = %q", abandoned.State)
	}
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-1", ExpectedRevision: abandoned.Revision, StepID: "import-contacts", IdempotencyKey: "k9", At: at, Caps: caps}); !errors.Is(err, ErrSetupAbandoned) {
		t.Fatalf("advance after abandon = %v, want ErrSetupAbandoned", err)
	}
	kept, ok := planner.Load(Scope{TenantID: "tenant-a"}, "plan-1", "ada.admin")
	if !ok || kept.StepState["company-profile"].ReceiptRef == "" {
		t.Fatal("abandon erased committed step receipts")
	}
}

// TestTodo_SMB_SETUP_001_Integration proves handoff, concurrent-edit
// conflicts and choice recompute: a second admin resumes, a module removal
// drops that module's pending steps while verified receipts survive, and a
// stale writer conflicts without enabling anything new.
func TestTodo_SMB_SETUP_001_Integration(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-2", "ada.admin", at)
	advanced, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-2", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "i1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	handed, err := planner.AddAdmin(SetupAdminRequest{Actor: "ada.admin", PlanID: "plan-2", ExpectedRevision: advanced.Revision, Admin: "bea.admin", At: at})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	resumed, _, err := planner.Advance(SetupAdvanceRequest{Actor: "bea.admin", PlanID: "plan-2", ExpectedRevision: handed.Revision, StepID: "role-review", IdempotencyKey: "i2", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("resumed advance by second admin: %v", err)
	}
	revised, err := planner.Revise(SetupReviseRequest{Actor: "ada.admin", PlanID: "plan-2", ExpectedRevision: resumed.Revision, Modules: []string{"inventory"}, At: at})
	if err != nil {
		t.Fatalf("revise modules: %v", err)
	}
	if _, pending := revised.StepState["import-contacts"]; pending {
		t.Fatal("removed module's pending step survived revision")
	}
	if revised.StepState["company-profile"].Status != SetupStepVerified || revised.StepState["company-profile"].ReceiptRef == "" {
		t.Fatal("module removal dropped a verified receipt")
	}
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "bea.admin", PlanID: "plan-2", ExpectedRevision: resumed.Revision, StepID: "import-contacts", IdempotencyKey: "i3", At: at, Caps: caps}); !errors.Is(err, ErrSetupRevisionConflict) {
		t.Fatalf("stale advance err = %v, want ErrSetupRevisionConflict", err)
	}
	view, ok := planner.View(Scope{TenantID: "tenant-a"}, "plan-2", "bea.admin")
	if !ok || view.RequiredRemaining != 0 {
		t.Fatalf("resumed view = %+v ok=%v, want 0 required remaining", view, ok)
	}
}

// TestTodo_SMB_SETUP_001_Security proves tenant and role isolation: a
// non-admin and another tenant read no progress or receipt titles, forged
// refs do not resolve, and an expired baseline approval cannot activate.
func TestTodo_SMB_SETUP_001_Security(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-3", "ada.admin", at)
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-3", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "s1", At: at, Caps: caps}); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, ok := planner.Load(Scope{TenantID: "tenant-a"}, "plan-3", "mallory"); ok {
		t.Fatal("non-admin loaded plan progress")
	}
	if _, ok := planner.Load(Scope{TenantID: "tenant-b"}, "plan-3", "ada.admin"); ok {
		t.Fatal("other tenant loaded plan progress")
	}
	if view, ok := planner.View(Scope{TenantID: "tenant-a"}, "plan-3", "mallory"); ok || view.RequiredTotal != 0 {
		t.Fatalf("non-admin view = %+v ok=%v, want nothing", view, ok)
	}
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "mallory", PlanID: "plan-3", ExpectedRevision: plan.Revision + 1, StepID: "role-review", IdempotencyKey: "s2", At: at, Caps: caps}); !errors.Is(err, ErrSetupNotFound) {
		t.Fatalf("non-admin advance err = %v, want non-revealing ErrSetupNotFound", err)
	}
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-nope", ExpectedRevision: 1, StepID: "role-review", IdempotencyKey: "s3", At: at, Caps: caps}); !errors.Is(err, ErrSetupNotFound) {
		t.Fatalf("forged plan advance err = %v, want ErrSetupNotFound", err)
	}

	oldApproval, _ := smbSetupApproval(t, approvals, "starter-plan-old", "release", "release-authority", at)
	aging, err := planner.Create(SetupPlanRequest{PlanID: "plan-old", Scope: Scope{TenantID: "tenant-a"}, Admin: "ada.admin",
		Baseline: BaselinePin{PackageID: "starter-plan-old", Digest: oldApproval.Digest, Scope: Scope{TenantID: "tenant-a"}, Approver: oldApproval.Approver, ExpiresAt: at.Add(time.Hour)},
		Modules:  []string{"party"}, Steps: smbSetupSteps(), At: at})
	if err != nil {
		t.Fatalf("create aging plan: %v", err)
	}
	if _, err := planner.ActivateBaseline(Scope{TenantID: "tenant-a"}, "plan-old", "ada.admin", aging.Revision, at.Add(2*time.Hour)); !errors.Is(err, ErrSetupBaselineRefused) {
		t.Fatalf("expired baseline activation err = %v, want ErrSetupBaselineRefused", err)
	}
}

// TestTodo_SMB_SETUP_001_Fault proves receipt-before-response: when the
// response is lost after the owner receipt is recorded, replaying the same
// idempotency key returns the recorded receipt and the owner capability
// still ran exactly once.
func TestTodo_SMB_SETUP_001_Fault(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-4", "ada.admin", at)
	planner.AfterReceipt = func() error { return errSetupSimulatedCrash }
	_, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-4", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "f1", At: at, Caps: caps})
	if !errors.Is(err, errSetupSimulatedCrash) {
		t.Fatalf("crashing advance err = %v, want the simulated crash", err)
	}
	planner.AfterReceipt = nil
	resumed, receipt, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-4", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "f1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("replay after crash: %v", err)
	}
	if receipt.ReceiptRef == "" || resumed.StepState["company-profile"].Status != SetupStepVerified {
		t.Fatal("replay did not recognize the recorded receipt")
	}
	if got := caps.invoked("company-profile"); got != 1 {
		t.Fatalf("owner capability ran %d times, want exactly once", got)
	}
	if _, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-4", ExpectedRevision: resumed.Revision, StepID: "role-review", IdempotencyKey: "f1", At: at, Caps: caps}); !errors.Is(err, ErrSetupIdempotencyConflict) {
		t.Fatalf("key reuse with another step err = %v, want ErrSetupIdempotencyConflict", err)
	}
}

// TestTodo_SMB_SETUP_001_Recovery proves restart convergence: an exported
// snapshot restores verified receipts, revision chains and idempotency keys
// on a fresh planner, concurrent stale writers conflict, and a tampered
// snapshot fails closed.
func TestTodo_SMB_SETUP_001_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-5", "ada.admin", at)
	advanced, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-5", ExpectedRevision: plan.Revision, StepID: "company-profile", IdempotencyKey: "r1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	snapshot, err := planner.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restored := NewSetupPlanner(smbSetupModules(t), approvals)
	if err := restored.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	loaded, ok := restored.Load(Scope{TenantID: "tenant-a"}, "plan-5", "ada.admin")
	if !ok || loaded.StepState["company-profile"].Status != SetupStepVerified || loaded.Revision != advanced.Revision {
		t.Fatalf("restored plan = %+v ok=%v, want verified profile at revision %d", loaded, ok, advanced.Revision)
	}
	replayed, _, err := restored.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-5", ExpectedRevision: advanced.Revision, StepID: "company-profile", IdempotencyKey: "r1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("replay on restored planner: %v", err)
	}
	if got := caps.invoked("company-profile"); got != 1 {
		t.Fatalf("owner capability ran %d times across restart, want once", got)
	}
	if _, err := restored.RecordCheck(SetupCheckRequest{Actor: "ada.admin", PlanID: "plan-5", ExpectedRevision: plan.Revision, CheckID: "role:payroll-admin", Passed: true, EvidenceRef: "e", At: at}); !errors.Is(err, ErrSetupRevisionConflict) {
		t.Fatalf("stale check err = %v, want ErrSetupRevisionConflict", err)
	}
	_ = replayed
	if err := restored.Import([]byte(`{"plans": {"x": {"revision": 3, "predecessor": 9}}}`)); err == nil {
		t.Fatal("tampered snapshot imported, want refusal")
	}
}

// TestTodo_SMB_SETUP_001_Browser proves the served setup-view contract the
// admin setup page renders: exact required-remaining counts, optional work
// labeled separately, an actionable distinct-approval explanation while the
// baseline is blocked, and truthful denied states.
func TestTodo_SMB_SETUP_001_Browser(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	caps := &countingCapability{}

	plan := smbSetupCreate(t, planner, approvals, "tenant-a", "plan-6", "ada.admin", at)
	view, ok := planner.View(Scope{TenantID: "tenant-a"}, "plan-6", "ada.admin")
	if !ok {
		t.Fatal("admin sees no setup view")
	}
	if view.RequiredRemaining != 2 || view.RequiredTotal != 2 {
		t.Fatalf("fresh view = %+v, want 2 of 2 required remaining", view)
	}
	if !strings.Contains(view.Summary, "2 required steps remaining") {
		t.Fatalf("fresh summary = %q, want the required-remaining count", view.Summary)
	}
	if len(view.OptionalPending) != 1 || !strings.Contains(view.Summary, "optional") {
		t.Fatalf("fresh view = %+v, want optional work labeled separately", view)
	}
	if !strings.Contains(view.BaselineExplain, "distinct approval required") {
		t.Fatalf("baseline explanation = %q, want the distinct-approval notice", view.BaselineExplain)
	}
	if len(view.AvailableActions) == 0 || view.AvailableActions[0] != SetupActionAdvance {
		t.Fatalf("fresh actions = %v, want an advance action", view.AvailableActions)
	}
	_ = plan
	advanced, _, err := planner.Advance(SetupAdvanceRequest{Actor: "ada.admin", PlanID: "plan-6", ExpectedRevision: 1, StepID: "company-profile", IdempotencyKey: "b1", At: at, Caps: caps})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	_ = advanced
	view, _ = planner.View(Scope{TenantID: "tenant-a"}, "plan-6", "ada.admin")
	if !strings.Contains(view.Summary, "1 required steps remaining") {
		t.Fatalf("progressed summary = %q, want 1 required remaining", view.Summary)
	}
	denied, ok := planner.View(Scope{TenantID: "tenant-a"}, "plan-6", "mallory")
	if ok || denied.Summary != "" {
		t.Fatalf("denied view = %+v ok=%v, want empty and unok", denied, ok)
	}
}
