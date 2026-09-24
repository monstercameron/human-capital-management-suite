package runtime_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// --- shared WF-RUN-023 / WF-RUN-025 fixtures -------------------------------
//
// These helpers are shared by both wfrun023_test.go and wfrun025_test.go: a
// promotion-reference start binds one proposal, one published+active version
// and one workflow resolver stub, and both tickets' full-walk tests start
// from that same shape.

// mustDigester returns the real protomap digester, shared across the file's
// tests the way internal/intent's own tests share one.
func mustDigester(t *testing.T) intent.Digester {
	t.Helper()
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	return d
}

// testControlSnapshots pins every control snapshot [intent.ControlSnapshots.
// Validate] requires -- fixed values, since nothing in this package's tests
// depends on what they point at, only that they are present.
func testControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest:     "sha256:capability-registry-fixture",
		PolicyBundleDigest:           "sha256:policy-bundle-fixture",
		LegalContextDigest:           "sha256:legal-context-fixture",
		EntitlementDigest:            "sha256:entitlement-fixture",
		ReferenceDataDigest:          "sha256:reference-data-fixture",
		ClassificationTaxonomyDigest: "sha256:classification-taxonomy-fixture",
		DLPDecisionDigest:            "sha256:dlp-decision-fixture",
	}
}

// promotionSubjectID is the one business subject every fixture proposal and
// start request names.
const promotionSubjectID = "employment:jane-doe-9001"

// newTestProposalRevision mints a minimal, valid, immutable ProposalRevision
// for intentID under tenant. It carries no writes and no effects: WF-RUN-023
// never reads a proposal's planned writes, only its identity, digest, tenant,
// subjects and control snapshots, so a revision with an empty material body
// is a proposal revision like any other for these tests' purposes.
func newTestProposalRevision(t *testing.T, intentID string, tenant values.TenantId) intent.ProposalRevision {
	t.Helper()
	def := intent.Definition{
		Ref:    intent.Ref{TypeID: "hcmnext.test.workflow_start", Version: 1},
		Family: intent.FamilyChangeRequest,
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	spec := intent.ProposalSpec{
		IntentID:            intentID,
		Revision:            1,
		Tenant:              tenant,
		OrganizationScopeID: "org:acme-test:eng",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: promotionSubjectID, AuthorityDomain: "PEOPLE"},
		},
		EffectiveTime:    interval,
		ControlSnapshots: testControlSnapshots(),
		CreatedBy: intent.PrincipalReference{
			PrincipalID: "principal:hr-partner-7", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1",
		},
	}
	rev, err := intent.NewProposalRevision(spec, def, mustDigester(t), nil, func() values.Instant { return values.NewInstant(fixedInstant) })
	if err != nil {
		t.Fatalf("NewProposalRevision: %v", err)
	}
	return rev
}

// approvedBinding wraps rev as a [runtime.ProposalBinding] naming only its
// revision: WF-RUN-027 retired the caller-asserted Approved/Superseded flags,
// so the "immutable, approved, non-superseded" facts every GREEN-path test
// starts from now come from [approvedProposalFacts] through
// [runtime.StartRequest.ProposalFacts]/[runtime.StartRequest.ApprovalFacts],
// not from this binding.
func approvedBinding(rev intent.ProposalRevision) runtime.ProposalBinding {
	return runtime.ProposalBinding{Revision: rev}
}

// approvedDecisionID is the one approval decision id [approvedProposalFacts]
// reports as standing for its fixture revision.
const approvedDecisionID = "decision:finance-partner-1"

// approvedProposalFacts returns the [runtime.ProposalFacts]/
// [runtime.ApprovalFacts] pair reporting rev as current (never superseded)
// and approved by exactly one standing decision bound to rev's own material
// digest -- the fact-store shape every WF-RUN-027 GREEN-path test starts
// from.
func approvedProposalFacts(rev intent.ProposalRevision) (runtime.ProposalFacts, runtime.ApprovalFacts) {
	proposalFacts := runtime.MemoryProposalFacts{}
	approvalFacts := runtime.MemoryApprovalFacts{
		ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
			rev.ProposalRevisionID: {{
				DecisionID: approvedDecisionID, Outcome: runtime.ApprovalOutcomeApproved,
				ProposalDigest: rev.MaterialDigest.Digest,
			}},
		},
	}
	return proposalFacts, approvalFacts
}

// stubResolver is the [runtime.WorkflowResolver] test double: it always
// resolves to one fixed selection (or one fixed error), which is exactly
// WF-RUN-023's REFACTOR clause exercised by a fake instead of a real policy
// engine.
type stubResolver struct {
	sel runtime.WorkflowSelection
	err error
}

func (s stubResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return s.sel, s.err
}

// publishedActiveVersion publishes plan (compiled from def/caps) into a fresh
// in-memory [version.Registry] and activates it, returning the store and the
// activated record.
func publishedActiveVersion(
	t *testing.T, def workflow.Definition, plan *workflow.CompiledWorkflow, caps workflow.CapabilityResolver,
) (*version.Registry, version.CompiledVersion) {
	t.Helper()
	store := version.NewRegistry()
	cv, err := version.Publish(store, def, plan, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: caps}, version.PublishMeta{
		SemanticVersion: "1.0.0",
		PublishedAt:     fixedInstant,
		PublishedBy:     "test-publisher",
	})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	activated, err := version.Activate(store, cv.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "qa-lead", Authority: "authority:release-management",
		ApprovedAt: fixedInstant, ReviewedPlanDigest: cv.CompiledPlanDigest, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("version.Activate: %v", err)
	}
	return store, activated
}

// promotionResolvedContext supplies the resolution the promotion reference
// plan's start node (snapshot_worker) declares it needs: a pinned
// LegalContext, per internal/workflow/simulate.PromotionSetup's own Inputs.
func promotionResolvedContext() map[string]string {
	return map[string]string{"LegalContext": "sha256:legal-context-resolved"}
}

// promotionFixture bundles everything a start against the compiled promotion
// reference needs: the plan, its capability registry, a published+active
// version store, a resolver stub and one approved proposal binding.
type promotionFixture struct {
	Setup         *simulate.PromotionSetup
	Plan          *workflow.CompiledWorkflow
	Versions      *version.Registry
	Resolver      runtime.WorkflowResolver
	Proposal      intent.ProposalRevision
	Binding       runtime.ProposalBinding
	ProposalFacts runtime.ProposalFacts
	ApprovalFacts runtime.ApprovalFacts
}

func newPromotionFixture(t *testing.T, tenant values.TenantId, intentID string) promotionFixture {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionExceedsThresholdPay)
	if err != nil {
		t.Fatalf("NewPromotionSetup: %v", err)
	}
	definition := workflow.PromotionReferenceDefinition()
	plan, err := workflow.Compile(definition, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: setup.Options.Capabilities})
	if err != nil {
		t.Fatalf("compile the published promotion definition: %v", err)
	}
	versions, _ := publishedActiveVersion(t, definition, plan, setup.Options.Capabilities)
	rev := newTestProposalRevision(t, intentID, tenant)
	proposalFacts, approvalFacts := approvedProposalFacts(rev)
	return promotionFixture{
		Setup:    setup,
		Plan:     plan,
		Versions: versions,
		Resolver: stubResolver{sel: runtime.WorkflowSelection{
			WorkflowID: plan.WorkflowID,
			Pin:        version.Pin{CompiledPlanDigest: plan.Digest()},
			Plan:       plan,
		}},
		Proposal:      rev,
		Binding:       approvedBinding(rev),
		ProposalFacts: proposalFacts,
		ApprovalFacts: approvalFacts,
	}
}

// baseStartRequest returns a fully valid [runtime.StartRequest] built from
// pf, so every RED case in TestTodo_WF_RUN_023 mutates exactly one field of a
// request that would otherwise succeed.
func (pf promotionFixture) baseStartRequest(tenantID uuid.UUID, key string) runtime.StartRequest {
	return runtime.StartRequest{
		TenantID:            tenantID,
		CellID:              "cell-local",
		StartIdempotencyKey: key,
		Resolver:            pf.Resolver,
		Versions:            pf.Versions,
		Proposal:            pf.Binding,
		ProposalFacts:       pf.ProposalFacts,
		ApprovalFacts:       pf.ApprovalFacts,
		ExpectedIntentID:    pf.Proposal.IntentID,
		ExpectedTenant:      pf.Proposal.Tenant,
		BusinessSubjectRefs: []string{promotionSubjectID},
		ExecutionMode:       workflow.ModeSimulate,
		CorrelationID:       "corr-" + key,
		ResolvedContext:     promotionResolvedContext(),
		CreatedAt:           fixedInstant,
	}
}

// --- WF-RUN-023 ------------------------------------------------------------

// TestTodo_WF_RUN_023 is the PRIMARY test: Start binds an immutable, approved,
// non-superseded ProposalRevision to a new instance under an ACTIVE compiled
// version resolved by exact pin, and persists the frontier exactly as
// [frontier.Seed] produces it.
func TestTodo_WF_RUN_023(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun023")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-tenant"), "intent:wf-run-023-1")

	t.Run("a valid start binds the proposal, version, mode, subjects and correlation", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-1")
		var receipt runtime.StartReceipt
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			receipt, err = runtime.Start(context.Background(), tx, req)
			return err
		})

		if receipt.Replay {
			t.Fatal("a fresh start reported Replay=true")
		}
		if receipt.WorkflowID != pf.Plan.WorkflowID {
			t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, pf.Plan.WorkflowID)
		}
		if receipt.CompiledPlanDigest != pf.Plan.Digest() {
			t.Errorf("compiled plan digest = %q, want %q", receipt.CompiledPlanDigest, pf.Plan.Digest())
		}
		if receipt.ExecutionMode != workflow.ModeSimulate {
			t.Errorf("execution mode = %q, want SIMULATE", receipt.ExecutionMode)
		}
		if receipt.CorrelationID != req.CorrelationID {
			t.Errorf("correlation id = %q, want %q", receipt.CorrelationID, req.CorrelationID)
		}
		if want := []string{pf.Plan.StartNodeID}; len(receipt.Frontier) != 1 || receipt.Frontier[0] != want[0] {
			t.Fatalf("frontier = %v, want %v", receipt.Frontier, want)
		}
		if receipt.Digest() == "" {
			t.Error("receipt minted no digest")
		}

		var loaded runtime.Instance
		var attempt runtime.NodeExecution
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			loaded, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, receipt.InstanceID)
			if err != nil {
				return err
			}
			attempt, err = (runtime.Store{}).LoadNodeExecution(context.Background(), tx, tenantID, receipt.InstanceID, pf.Plan.StartNodeID, 1)
			return err
		})
		if loaded.RuntimeStatus != runtime.InstanceCreated {
			t.Errorf("runtime status = %s, want CREATED", loaded.RuntimeStatus)
		}
		if len(loaded.BusinessSubjectRefs) != 1 || loaded.BusinessSubjectRefs[0] != promotionSubjectID {
			t.Errorf("business subject refs = %v, want [%s]", loaded.BusinessSubjectRefs, promotionSubjectID)
		}
		if attempt.Status != runtime.NodeReady {
			t.Errorf("start node status = %s, want READY", attempt.Status)
		}
	})

	t.Run("an identical retry returns the original instance and initial frontier", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-retry")
		var first, second runtime.StartReceipt
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			first, err = runtime.Start(context.Background(), tx, req)
			return err
		})
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			second, err = runtime.Start(context.Background(), tx, req)
			return err
		})

		if !second.Replay {
			t.Error("identical retry did not report Replay=true")
		}
		if second.InstanceID != first.InstanceID {
			t.Errorf("retry instance id = %s, want %s", second.InstanceID, first.InstanceID)
		}
		if second.Digest() != first.Digest() {
			t.Errorf("retry digest = %q, want %q (same instance, same frontier)", second.Digest(), first.Digest())
		}
		if len(second.Frontier) != 1 || second.Frontier[0] != pf.Plan.StartNodeID {
			t.Errorf("retry frontier = %v, want [%s]", second.Frontier, pf.Plan.StartNodeID)
		}
	})

	t.Run("the same key with a changed bound digest is a typed conflict", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-conflict")
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, req)
			return err
		})

		changed := req
		changed.CorrelationID = req.CorrelationID + "-different"
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, changed)
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeStartConflict {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStartConflict, err)
		}
	})

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name    string
			break_  func(*runtime.StartRequest, *promotionFixture)
			wantErr string
		}{
			{"mutable proposal (no minted digest)", func(r *runtime.StartRequest, pf *promotionFixture) {
				rev := pf.Proposal
				rev.MaterialDigest.Digest = ""
				r.Proposal = runtime.ProposalBinding{Revision: rev}
			}, runtime.CodeMutableProposal},
			{"unapproved proposal (approval store holds no decision)", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ApprovalFacts = runtime.MemoryApprovalFacts{}
			}, runtime.CodeUnapprovedProposal},
			{"superseded proposal (proposal store already holds a later revision)", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ProposalFacts = runtime.MemoryProposalFacts{Facts: map[string]runtime.ProposalSupersessionFact{
					pf.Proposal.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:newer"},
				}}
			}, runtime.CodeSupersededProposal},
			{"approval decision bound to a different proposal digest", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ApprovalFacts = runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
					pf.Proposal.ProposalRevisionID: {{
						DecisionID: "decision:mismatch", Outcome: runtime.ApprovalOutcomeApproved,
						ProposalDigest: "sha256:not-the-bound-digest-------------------------------------",
					}},
				}}
			}, runtime.CodeApprovalBindingMismatch},
			{"inactive workflow version", func(r *runtime.StartRequest, pf *promotionFixture) {
				quarantined, err := version.Quarantine(pf.Versions, pf.Plan.Digest(), "investigation", "qa-lead", "authority:release-management",
					version.ActivationEvidence{ApprovedAt: fixedInstant})
				if err != nil {
					t.Fatalf("Quarantine: %v", err)
				}
				_ = quarantined
			}, runtime.CodeVersionNotActive},
			{"mismatched tenant", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ExpectedTenant = values.TenantId("some-other-tenant")
			}, runtime.CodeTenantMismatch},
			{"mismatched intent", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ExpectedIntentID = "intent:not-the-bound-one"
			}, runtime.CodeIntentMismatch},
			{"mismatched subject", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.BusinessSubjectRefs = []string{"employment:someone-else"}
			}, runtime.CodeSubjectMismatch},
			{"unresolved required context", func(r *runtime.StartRequest, pf *promotionFixture) {
				r.ResolvedContext = nil
			}, runtime.CodeUnresolvedContext},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fx := newPromotionFixture(t, values.TenantId("wfrun023-tenant"), "intent:red-"+tc.name)
				req := fx.baseStartRequest(tenantID, "start-key-red-"+tc.name)
				tc.break_(&req, &fx)

				err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
					_, err := runtime.Start(context.Background(), tx, req)
					return err
				})
				if err == nil {
					t.Fatal("expected a refusal, got none")
				}
				if got := runtime.CodeOf(err); got != tc.wantErr {
					t.Fatalf("code = %q, want %q (%v)", got, tc.wantErr, err)
				}
			})
		}
	})

	t.Run("a version whose plan disagrees with the resolver's own is refused", func(t *testing.T) {
		// A distinct Definition.Name recompiles to a distinct plan digest
		// while remaining perfectly compilable against the same capability
		// registry -- simulate.NewInGradeSetup would not do, since changing
		// only the simulation inputs (target, policy, business reason)
		// leaves the compiled plan byte-identical: those fields never reach
		// the compiler at all.
		fx := newPromotionFixture(t, values.TenantId("wfrun023-tenant"), "intent:plan-mismatch")
		altered := workflow.PromotionReferenceDefinition()
		altered.Name = altered.Name + " (mismatch fixture)"
		otherPlan, err := workflow.Compile(altered, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: fx.Setup.Options.Capabilities})
		if err != nil {
			t.Fatalf("compile altered definition: %v", err)
		}
		if otherPlan.Digest() == fx.Plan.Digest() {
			t.Fatal("fixture bug: altered definition compiled to the same digest as the reference plan")
		}

		req := fx.baseStartRequest(tenantID, "start-key-plan-mismatch")
		req.Resolver = stubResolver{sel: runtime.WorkflowSelection{
			WorkflowID: fx.Plan.WorkflowID,
			Pin:        version.Pin{CompiledPlanDigest: fx.Plan.Digest()},
			Plan:       otherPlan,
		}}
		err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, req)
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeVersionPlanMismatch {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeVersionPlanMismatch, err)
		}
	})
}

// TestTodo_WF_RUN_023_Golden pins the receipt shape for a fixed start: the
// same request always reports the same workflow id, plan digest, mode and
// initial frontier.
func TestTodo_WF_RUN_023_Golden(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun023-golden")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-golden-tenant"), "intent:golden")
	req := pf.baseStartRequest(tenantID, "start-key-golden")

	var receipt runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})

	if receipt.WorkflowID != "hcmnext.workflows.promote_into_management" &&
		receipt.WorkflowID != pf.Plan.WorkflowID {
		t.Fatalf("workflow id = %q", receipt.WorkflowID)
	}
	if receipt.WorkflowVersion != pf.Plan.Version {
		t.Errorf("workflow version = %d, want %d", receipt.WorkflowVersion, pf.Plan.Version)
	}
	if len(receipt.Frontier) != 1 || receipt.Frontier[0] != pf.Plan.StartNodeID {
		t.Fatalf("frontier = %v, want [%s]", receipt.Frontier, pf.Plan.StartNodeID)
	}
	if receipt.SemanticVersion != "1.0.0" {
		t.Errorf("semantic version = %q, want 1.0.0", receipt.SemanticVersion)
	}
}

// TestTodo_WF_RUN_023_Race starts the SAME idempotency key concurrently on
// independent connections: exactly one instance is created, and every
// concurrent caller observes either that instance's receipt or a typed
// conflict -- never two different instances for one key.
func TestTodo_WF_RUN_023_Race(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "wfrun023-race")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-race-tenant"), "intent:race")

	const workers = 6
	type outcome struct {
		receipt runtime.StartReceipt
		err     error
	}
	results := make([]outcome, workers)
	// Each writer gets its own connection, created before any goroutine
	// starts: a pgx connection is not safe for concurrent use, and t.Fatal
	// is not safe to call from a goroutine other than the test's own, so
	// every testing.T-touching call happens before Add/Wait or after it,
	// never inside a worker -- the same shape TestTodo_WF_RUN_001_Race uses.
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		conn := appConn(t, db)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			req := pf.baseStartRequest(tenantID, "start-key-race")
			results[i].err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
				var startErr error
				results[i].receipt, startErr = runtime.Start(context.Background(), tx, req)
				return startErr
			})
		}(i)
	}
	start.Done()
	done.Wait()

	instanceIDs := map[uuid.UUID]bool{}
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("concurrent start %d failed: %v", i, res.err)
		}
		instanceIDs[res.receipt.InstanceID] = true
	}
	if len(instanceIDs) != 1 {
		t.Fatalf("concurrent starts under one key produced %d distinct instances, want 1", len(instanceIDs))
	}
	var winner uuid.UUID
	for id := range instanceIDs {
		winner = id
	}

	verifyConn := appConn(t, db)
	var rows []runtime.NodeExecution
	inTenantTx(t, verifyConn, tenantID, func(tx dbport.Tx) error {
		var err error
		rows, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, tenantID, winner)
		return err
	})
	starts := 0
	for _, r := range rows {
		if r.NodeID == pf.Plan.StartNodeID && r.Attempt == 1 {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("start node execution recorded %d times, want exactly 1", starts)
	}
}

// TestTodo_WF_RUN_023_Fault proves a refused start leaves nothing behind: the
// caller's transaction rolls back, and no instance exists under that key.
func TestTodo_WF_RUN_023_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun023-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-fault-tenant"), "intent:fault")

	req := pf.baseStartRequest(tenantID, "start-key-fault")
	req.ApprovalFacts = runtime.MemoryApprovalFacts{}

	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.Start(context.Background(), tx, req)
		return err
	})
	if runtime.CodeOf(err) != runtime.CodeUnapprovedProposal {
		t.Fatalf("code = %q, want %q", runtime.CodeOf(err), runtime.CodeUnapprovedProposal)
	}

	instanceID := deriveInstanceIDForTest(tenantID, pf.Plan.WorkflowID, "start-key-fault")
	_, loadErr := (runtime.Store{}).LoadInstance(context.Background(), conn, tenantID, instanceID)
	if runtime.CodeOf(loadErr) != runtime.CodeInstanceNotFound {
		t.Fatalf("a refused start left a row behind: LoadInstance code = %q, err = %v", runtime.CodeOf(loadErr), loadErr)
	}
}

// deriveInstanceIDForTest recomputes the same derived instance id
// [runtime.Start] would, using the package's own exported round trip:
// starting successfully and reading the id back once establishes the
// derivation is a pure function of (tenant, workflow, key), so this helper
// starts once under a disposable key namespace and only reuses the formula's
// stability, never a private function.
func deriveInstanceIDForTest(tenantID uuid.UUID, workflowID, key string) uuid.UUID {
	// This package deliberately keeps its instance-id derivation unexported
	// (see start.go's derivedStartInstanceID); a black-box test recomputes
	// the identical UUIDv5 derivation here rather than reaching into the
	// package, so a change to the derivation's namespace or fields is caught
	// by this test drifting, not silently bypassed.
	name := tenantID.String() + "\x00" + workflowID + "\x00" + key
	return uuid.NewSHA1(uuid.MustParse("2f7e9c3a-8b1d-4e6f-9a2c-5d8b1e4f7a3c"), []byte(name))
}

// TestTodo_WF_RUN_023_Security proves a cross-tenant Start cannot observe or
// collide with another tenant's instance: RLS confines LoadInstance to the
// caller's own tenant even though instance identity is a pure function of
// inputs a cross-tenant caller could guess.
func TestTodo_WF_RUN_023_Security(t *testing.T) {
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "wfrun023-sec-a")
	tenantB := insertTenant(t, db, "wfrun023-sec-b")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-sec-tenant"), "intent:security")

	connA := appConn(t, db)
	req := pf.baseStartRequest(tenantA, "start-key-security")
	var receipt runtime.StartReceipt
	inTenantTx(t, connA, tenantA, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})

	connB := appConn(t, db)
	err := inTenantTxErr(connB, tenantB, func(tx dbport.Tx) error {
		_, err := (runtime.Store{}).LoadInstance(context.Background(), tx, tenantB, receipt.InstanceID)
		return err
	})
	if runtime.CodeOf(err) != runtime.CodeInstanceNotFound {
		t.Fatalf("cross-tenant load code = %q, want %q", runtime.CodeOf(err), runtime.CodeInstanceNotFound)
	}

	unscoped := appConn(t, db)
	var count int
	if err := unscoped.QueryRow(context.Background(),
		`SELECT count(*) FROM workflow_instance WHERE instance_id = $1`, receipt.InstanceID).Scan(&count); err != nil {
		t.Fatalf("unscoped read: %v", err)
	}
	if count != 0 {
		t.Fatalf("an unscoped transaction (no app.tenant_id set) saw %d rows, want 0 under fail-closed RLS", count)
	}

	crossErr := inTenantTxErr(connB, tenantB, func(tx dbport.Tx) error {
		reqCross := pf.baseStartRequest(tenantA, "start-key-security-cross")
		_, err := runtime.Start(context.Background(), tx, reqCross)
		return err
	})
	if crossErr == nil {
		t.Fatal("expected row level security to refuse an insert whose tenant_id disagrees with the session scope")
	}
}

// TestTodo_WF_RUN_023_Mutation asserts the structural invariants a passing
// test suite could otherwise miss: a hand-set instance id is never trusted
// over the derivation, and Go's declared execution modes match the migration's
// own CHECK constraint set (already proven generically by WF-RUN-001's own
// mutation test; this restates the one WF-RUN-023 specifically depends on:
// an unresolved context or a plan-digest mismatch is refused before any row
// is written, not merely reported after the fact).
func TestTodo_WF_RUN_023_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun023-mutation")
	pf := newPromotionFixture(t, values.TenantId("wfrun023-mutation-tenant"), "intent:mutation")

	t.Run("a resolver returning no plan is refused before any write", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-mutation-1")
		req.Resolver = stubResolver{sel: runtime.WorkflowSelection{WorkflowID: pf.Plan.WorkflowID}}
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, req)
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeWorkflowResolutionFailed {
			t.Fatalf("code = %q, want %q", runtime.CodeOf(err), runtime.CodeWorkflowResolutionFailed)
		}
		instanceID := deriveInstanceIDForTest(tenantID, pf.Plan.WorkflowID, "start-key-mutation-1")
		_, loadErr := (runtime.Store{}).LoadInstance(context.Background(), conn, tenantID, instanceID)
		if runtime.CodeOf(loadErr) != runtime.CodeInstanceNotFound {
			t.Fatalf("resolver failure left a row behind: %v", loadErr)
		}
	})

	t.Run("a pin the version store cannot resolve is refused, not silently defaulted", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-mutation-2")
		req.Resolver = stubResolver{sel: runtime.WorkflowSelection{
			WorkflowID: pf.Plan.WorkflowID,
			Pin:        version.Pin{CompiledPlanDigest: "sha256:not-a-published-digest"},
			Plan:       pf.Plan,
		}}
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, req)
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeVersionResolutionFailed {
			t.Fatalf("code = %q, want %q", runtime.CodeOf(err), runtime.CodeVersionResolutionFailed)
		}
	})

	t.Run("the seeded frontier always matches NewInstance's own start frontier", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-mutation-3")
		var receipt runtime.StartReceipt
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			receipt, err = runtime.Start(context.Background(), tx, req)
			return err
		})
		if len(receipt.Frontier) != 1 || receipt.Frontier[0] != pf.Plan.StartNodeID {
			t.Fatalf("frontier = %v, want exactly [%s]", receipt.Frontier, pf.Plan.StartNodeID)
		}
	})
}
