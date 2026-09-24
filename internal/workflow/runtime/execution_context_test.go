package runtime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestDeriveExecutionContextPinsEveryField(t *testing.T) {
	pf := newPromotionFixture(t, values.TenantId("ctx-derive-tenant"), "intent:ctx-derive")
	tenantID := uuid.New()
	req := pf.baseStartRequest(tenantID, "ctx-derive")
	sel := runtime.WorkflowSelection{WorkflowID: pf.Plan.WorkflowID, Plan: pf.Plan}

	c := runtime.DeriveExecutionContext(req, sel)
	if err := c.Validate(); err != nil {
		t.Fatalf("derived context is invalid: %v", err)
	}
	if c.Locale != runtime.DefaultLocale || c.BillingRef != "billing:tenant:"+tenantID.String() {
		t.Fatalf("defaults = locale %q billing %q", c.Locale, c.BillingRef)
	}
	if c.Principal != "principal:hr-partner-7" || c.Tenant != tenantID.String() || c.Organization != "org:acme-test:eng" {
		t.Fatalf("identity fields = %+v", c)
	}
	if c.CompiledPlanDigest != pf.Plan.Digest() || c.WorkflowVersion != pf.Plan.Version || c.RiskClass != pf.Plan.RiskClass ||
		c.RuntimeVersion != runtime.RuntimeVersion || c.ExecutionMode != workflow.ModeSimulate {
		t.Fatalf("plan and runtime fields = %+v", c)
	}
	if again := runtime.DeriveExecutionContext(req, sel); again.Digest() != c.Digest() {
		t.Fatal("deriving the same start twice produced different digests")
	}

	req.Locale, req.BillingRef = "fr-CA", "billing:cost-center:42"
	other := runtime.DeriveExecutionContext(req, sel)
	if other.Locale != "fr-CA" || other.BillingRef != "billing:cost-center:42" || other.Digest() == c.Digest() {
		t.Fatalf("explicit locale and billing were not pinned into a distinct digest: %+v", other)
	}
	if !strings.HasPrefix(c.Digest(), "sha256:") {
		t.Fatalf("digest %q is not a sha256 reference", c.Digest())
	}
	if noPlan := runtime.DeriveExecutionContext(req, runtime.WorkflowSelection{WorkflowID: "wf"}); noPlan.CompiledPlanDigest != "" {
		t.Fatalf("a selection without a plan pinned digest %q", noPlan.CompiledPlanDigest)
	}
}

func TestExecutionContextValidateRefusesMissingFields(t *testing.T) {
	valid := runtime.ExecutionContext{
		Principal: "p", Tenant: "t", Locale: "und", BillingRef: "b", ExecutionMode: workflow.ModeExecute,
		WorkflowID: "wf", CompiledPlanDigest: "sha256:x", RuntimeVersion: runtime.RuntimeVersion,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid context refused: %v", err)
	}
	for name, mutate := range map[string]func(*runtime.ExecutionContext){
		"principal": func(c *runtime.ExecutionContext) { c.Principal = " " },
		"tenant":    func(c *runtime.ExecutionContext) { c.Tenant = "" },
		"locale":    func(c *runtime.ExecutionContext) { c.Locale = "" },
		"billing":   func(c *runtime.ExecutionContext) { c.BillingRef = "" },
		"mode":      func(c *runtime.ExecutionContext) { c.ExecutionMode = "WHENEVER" },
		"workflow":  func(c *runtime.ExecutionContext) { c.WorkflowID = "" },
		"digest":    func(c *runtime.ExecutionContext) { c.CompiledPlanDigest = "" },
		"runtime":   func(c *runtime.ExecutionContext) { c.RuntimeVersion = "" },
	} {
		c := valid
		mutate(&c)
		if code := runtimeCode(c.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("missing %s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
}

func TestNodeAllowsModeFailsClosed(t *testing.T) {
	node := workflow.CompiledNode{AllowedModes: []workflow.ExecutionMode{workflow.ModeExecute, workflow.ModeRepair}}
	if !runtime.NodeAllowsMode(node, workflow.ModeExecute) {
		t.Fatal("a declared mode was refused")
	}
	if runtime.NodeAllowsMode(node, workflow.ModeSimulate) || runtime.NodeAllowsMode(node, "") {
		t.Fatal("an undeclared or empty mode was admitted")
	}
	if runtime.NodeAllowsMode(workflow.CompiledNode{}, workflow.ModeExecute) {
		t.Fatal("a node with no compiled modes admitted a mode")
	}
}

// TestTodo_Unit6_ExecutionContextDigestCoversEveryField proves the pinned
// fingerprint binds every execution dimension it names: changing any one
// field of the context changes its digest, and changing any one source
// dimension of the derivation changes the derived digest. A field or source
// the digest ignored would let that dimension drift mid-run undetected, so
// each is proven live here rather than trusted to the marshal.
func TestTodo_Unit6_ExecutionContextDigestCoversEveryField(t *testing.T) {
	base := runtime.ExecutionContext{
		Principal: "principal:a", PrincipalKind: "HUMAN", Tenant: "tenant:a", Organization: "org:a",
		Locale: "en-US", LegalEntity: "le:a", LegalContextDigest: "sha256:legal",
		Purpose: "purpose:a", Residency: "residency:a", EntitlementDigest: "sha256:ent",
		RiskClass: "risk:a", BillingRef: "billing:a", ExecutionMode: workflow.ModeExecute,
		WorkflowID: "wf.a", WorkflowVersion: 1, CompiledPlanDigest: "sha256:plan",
		RuntimeVersion: runtime.RuntimeVersion,
	}
	pinned := base.Digest()
	for name, mutate := range map[string]func(*runtime.ExecutionContext){
		"principal":      func(c *runtime.ExecutionContext) { c.Principal = "principal:b" },
		"principal kind": func(c *runtime.ExecutionContext) { c.PrincipalKind = "SERVICE" },
		"tenant":         func(c *runtime.ExecutionContext) { c.Tenant = "tenant:b" },
		"organization":   func(c *runtime.ExecutionContext) { c.Organization = "org:b" },
		"locale":         func(c *runtime.ExecutionContext) { c.Locale = "de-DE" },
		"legal entity":   func(c *runtime.ExecutionContext) { c.LegalEntity = "le:b" },
		"legal digest":   func(c *runtime.ExecutionContext) { c.LegalContextDigest = "sha256:legal-b" },
		"purpose":        func(c *runtime.ExecutionContext) { c.Purpose = "purpose:b" },
		"residency":      func(c *runtime.ExecutionContext) { c.Residency = "residency:b" },
		"entitlement":    func(c *runtime.ExecutionContext) { c.EntitlementDigest = "sha256:ent-b" },
		"risk class":     func(c *runtime.ExecutionContext) { c.RiskClass = "risk:b" },
		"billing":        func(c *runtime.ExecutionContext) { c.BillingRef = "billing:b" },
		"mode":           func(c *runtime.ExecutionContext) { c.ExecutionMode = workflow.ModeSimulate },
		"workflow":       func(c *runtime.ExecutionContext) { c.WorkflowID = "wf.b" },
		"workflow ver":   func(c *runtime.ExecutionContext) { c.WorkflowVersion = 2 },
		"plan digest":    func(c *runtime.ExecutionContext) { c.CompiledPlanDigest = "sha256:plan-b" },
		"runtime ver":    func(c *runtime.ExecutionContext) { c.RuntimeVersion = "hcmnext.workflow.runtime/v0" },
	} {
		changed := base
		mutate(&changed)
		if changed.Digest() == pinned {
			t.Errorf("changing %s left the execution context digest unchanged", name)
		}
	}

	// Every derivation source dimension flips the derived digest. The
	// proposal revision's own identity and material digest are deliberately
	// absent: proposal binding is enforced per advancement by the drift
	// checks against the stored work item, timer and signal rows, not by
	// this digest.
	pf := newPromotionFixture(t, values.TenantId("ctx-unit6-tenant"), "intent:ctx-unit6")
	tenantID := uuid.New()
	derive := func(req runtime.StartRequest, sel runtime.WorkflowSelection) string {
		return runtime.DeriveExecutionContext(req, sel).Digest()
	}
	sel := runtime.WorkflowSelection{WorkflowID: pf.Plan.WorkflowID, Plan: pf.Plan}
	derived := derive(pf.baseStartRequest(tenantID, "ctx-unit6"), sel)
	flip := func(name string, mutate func(*runtime.StartRequest, *runtime.WorkflowSelection)) {
		t.Helper()
		req := pf.baseStartRequest(tenantID, "ctx-unit6")
		s := sel
		mutate(&req, &s)
		if derive(req, s) == derived {
			t.Errorf("source dimension %s is not bound into the derived execution context", name)
		}
	}
	flip("tenant", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) { req.TenantID = uuid.New() })
	flip("locale", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) { req.Locale = "de-DE" })
	flip("billing", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) { req.BillingRef = "billing:other" })
	flip("mode", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.ExecutionMode = workflow.ModeExecute
	})
	flip("workflow id", func(_ *runtime.StartRequest, s *runtime.WorkflowSelection) { s.WorkflowID = "wf.other" })
	flip("plan risk", func(req *runtime.StartRequest, s *runtime.WorkflowSelection) {
		plan := *s.Plan
		plan.RiskClass = "risk:changed"
		s.Plan = &plan
	})
	flip("principal", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.CreatedBy.PrincipalID = "principal:other"
	})
	flip("principal kind", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.CreatedBy.Kind = intent.InitiatorService
	})
	flip("organization", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.OrganizationScopeID = "org:other"
	})
	flip("legal entity", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.LegalEntityID = "le:other"
	})
	flip("legal digest", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		snaps := req.Proposal.Revision.ControlSnapshots
		snaps.LegalContextDigest = "sha256:other-legal"
		req.Proposal.Revision.ControlSnapshots = snaps
	})
	flip("entitlement", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		snaps := req.Proposal.Revision.ControlSnapshots
		snaps.EntitlementDigest = "sha256:other-ent"
		req.Proposal.Revision.ControlSnapshots = snaps
	})
	flip("purpose", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.Purpose.Purpose = "purpose:other"
	})
	flip("residency", func(req *runtime.StartRequest, _ *runtime.WorkflowSelection) {
		req.Proposal.Revision.Purpose.ResidencyRef = "residency:other"
	})
}

// TestTodo_WF_RUN_040 proves the execution context is pinned durably at
// start, replayed unchanged, proven on every advancement and detected when
// its stored row is tampered with.
func TestTodo_WF_RUN_040(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun040")
	pf := newPromotionFixture(t, values.TenantId("wfrun040-tenant"), "intent:wf-run-040")
	req := pf.baseStartRequest(tenantID, "wfrun040")
	req.Locale = "en-US"

	started := startPromotionInstance(t, conn, tenantID, req)
	pinned := started.ExecutionContext
	if pinned.Locale != "en-US" || pinned.Tenant != tenantID.String() || pinned.CompiledPlanDigest != pf.Plan.Digest() {
		t.Fatalf("start receipt context = %+v", pinned)
	}
	var loaded runtime.ExecutionContext
	var found bool
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		loaded, found, err = runtime.LoadExecutionContext(ctx, tx, tenantID, started.InstanceID)
		return err
	})
	if !found || loaded.Digest() != pinned.Digest() {
		t.Fatalf("loaded context = %+v (found %v), want the pinned one", loaded, found)
	}

	replayed := startPromotionInstance(t, conn, tenantID, req)
	if !replayed.Replay || replayed.ExecutionContext.Digest() != pinned.Digest() {
		t.Fatalf("replayed start context = %+v (replay %v), want the originally pinned one", replayed.ExecutionContext, replayed.Replay)
	}

	step := promotionExceedsThresholdWalk[0]
	advance := func(digest string) (runtime.AdvanceReceipt, error) {
		return advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: started.InstanceID, ExpectedInstanceVersion: started.InstanceVersion, Attempt: 1,
			Plan:       pf.Plan,
			Outcome:    frontier.NodeOutcome{NodeID: step.NodeID, Outcome: step.Outcome, OutputDigest: step.Digest},
			RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(), ExecutionContextDigest: digest,
		})
	}
	req.Locale = "de-DE"
	drifted := runtime.DeriveExecutionContext(req, runtime.WorkflowSelection{WorkflowID: pf.Plan.WorkflowID, Plan: pf.Plan})
	if _, err := advance(drifted.Digest()); runtimeCode(err) != runtime.CodeContextDrift {
		t.Fatalf("advance under a different context = %v, want %s", err, runtime.CodeContextDrift)
	}
	if receipt, err := advance(pinned.Digest()); err != nil || receipt.NewInstanceVersion <= started.InstanceVersion {
		t.Fatalf("advance under the pinned context = %+v, %v", receipt, err)
	}

	// The table is append-only (forbid_mutation, 00298): even a privileged
	// session is refused, and a tamper has to disable the trigger first.
	if err := db.ExecErr(`UPDATE workflow_execution_context SET recorded_at = now() WHERE tenant_id = $1`, tenantID); err == nil {
		t.Fatal("workflow_execution_context accepted an UPDATE")
	}
	db.Exec(t, `ALTER TABLE workflow_execution_context DISABLE TRIGGER workflow_execution_context_forbid_mutation`)
	db.Exec(t, `UPDATE workflow_execution_context SET context = jsonb_set(context, '{locale}', '"de-DE"') WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, started.InstanceID)
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, _, err := runtime.LoadExecutionContext(ctx, tx, tenantID, started.InstanceID)
		return err
	})
	if runtimeCode(err) != runtime.CodeContextDrift {
		t.Fatalf("load of a tampered context = %v, want %s", err, runtime.CodeContextDrift)
	}
	db.Exec(t, `DELETE FROM workflow_execution_context WHERE tenant_id = $1 AND instance_id = $2`, tenantID, started.InstanceID)
	err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, _, err := runtime.LoadExecutionContext(ctx, tx, tenantID, started.InstanceID)
		return err
	})
	if runtimeCode(err) != runtime.CodeContextDrift {
		t.Fatalf("load with the context row missing = %v, want %s", err, runtime.CodeContextDrift)
	}
}

// TestTodo_WF_RUN_040_Mutation proves every authority-relevant field changes
// the immutable context fingerprint when a caller attempts to alter it.
func TestTodo_WF_RUN_040_Mutation(t *testing.T) {
	base := runtime.ExecutionContext{
		Principal: "principal:worker", PrincipalKind: "HUMAN", Tenant: "tenant:one", Organization: "org:one",
		Locale: "en-US", LegalEntity: "entity:one", LegalContextDigest: "sha256:legal-one",
		Purpose: "promotion", Residency: "region:one", EntitlementDigest: "sha256:entitlement-one",
		RiskClass: "high", BillingRef: "billing:one", ExecutionMode: workflow.ModeExecute,
		WorkflowID: "promotion", WorkflowVersion: 7, CompiledPlanDigest: "sha256:plan-one", RuntimeVersion: runtime.RuntimeVersion,
	}
	pinned := base.Digest()
	mutations := map[string]func(*runtime.ExecutionContext){
		"principal":        func(c *runtime.ExecutionContext) { c.Principal = "principal:other" },
		"tenant":           func(c *runtime.ExecutionContext) { c.Tenant = "tenant:other" },
		"organization":     func(c *runtime.ExecutionContext) { c.Organization = "org:other" },
		"locale":           func(c *runtime.ExecutionContext) { c.Locale = "de-DE" },
		"legal entity":     func(c *runtime.ExecutionContext) { c.LegalEntity = "entity:other" },
		"legal context":    func(c *runtime.ExecutionContext) { c.LegalContextDigest = "sha256:legal-other" },
		"purpose":          func(c *runtime.ExecutionContext) { c.Purpose = "termination" },
		"residency":        func(c *runtime.ExecutionContext) { c.Residency = "region:other" },
		"entitlement":      func(c *runtime.ExecutionContext) { c.EntitlementDigest = "sha256:entitlement-other" },
		"risk":             func(c *runtime.ExecutionContext) { c.RiskClass = "low" },
		"billing":          func(c *runtime.ExecutionContext) { c.BillingRef = "billing:other" },
		"mode":             func(c *runtime.ExecutionContext) { c.ExecutionMode = workflow.ModeSimulate },
		"workflow":         func(c *runtime.ExecutionContext) { c.WorkflowID = "other" },
		"workflow version": func(c *runtime.ExecutionContext) { c.WorkflowVersion++ },
		"plan":             func(c *runtime.ExecutionContext) { c.CompiledPlanDigest = "sha256:plan-other" },
		"runtime":          func(c *runtime.ExecutionContext) { c.RuntimeVersion = "other-runtime" },
	}
	for name, mutate := range mutations {
		changed := base
		mutate(&changed)
		if changed.Digest() == pinned {
			t.Errorf("changing %s did not change the pinned context digest", name)
		}
		if base.Digest() != pinned {
			t.Errorf("changing %s mutated the original context", name)
		}
	}
}
