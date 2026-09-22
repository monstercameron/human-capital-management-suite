package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_EXT_001 proves the WF-EXT-001 RED is closed: the driver claims
// compensate_budget_hold for the advance transaction, and the in-transaction
// path delivers the step transaction past the port's advance-transaction
// gate to dispatch. The served compensation can no longer fail every time it
// is reached the way the RED describes.
func TestTodo_WF_EXT_001(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	executeNode, ok := plan.Node(promotionexec.NodeExecutePromotion)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeExecutePromotion)
	}
	compensateNode, ok := plan.Node(promotionexec.NodeCompensateHold)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeCompensateHold)
	}
	snapshotNode, ok := plan.Node(promotionexec.NodeSnapshotWorker)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeSnapshotWorker)
	}

	runner := promotionStepRunner{plan: PLAN_EXECUTE}
	prototype := promotionStepRunner{plan: PLAN_PROTOTYPE}
	if !runner.RunsInTransaction(executeNode) {
		t.Error("execute plan does not claim execute_promotion for the advance transaction")
	}
	if !runner.RunsInTransaction(compensateNode) {
		t.Error("execute plan does not claim compensate_budget_hold for the advance transaction: the compensation still runs outside the transaction it requires")
	}
	if runner.RunsInTransaction(snapshotNode) {
		t.Error("execute plan claims a read-only snapshot node for the advance transaction")
	}
	if prototype.RunsInTransaction(executeNode) || prototype.RunsInTransaction(compensateNode) {
		t.Error("prototype plan claims a governed write for the advance transaction")
	}

	// The same compensation refused outside the transaction must reach
	// dispatch once the driver runs it inside: with unbound services the
	// runner is portless, so dispatch must fail closed with
	// PORT_NOT_CONFIGURED rather than the advance-transaction refusal.
	req := compensateHoldRequest(uuid.NewString(), strings.Repeat("c", 64))
	req.Node = compensateNode
	req.Plan = plan
	if _, err := (&promotionStepPorts{}).ReleaseHold(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "advance transaction") {
		t.Fatalf("ReleaseHold without advance tx = %v, want the advance-transaction refusal", err)
	}
	txRunner := promotionStepRunner{plan: PLAN_EXECUTE, ports: &promotionStepPorts{}}
	outcome, _, err := txRunner.RunInTx(context.Background(), &scriptTx{}, req)
	if err != nil {
		t.Fatalf("RunInTx(compensate_budget_hold) = %v, want dispatch past the transaction gate", err)
	}
	if !outcome.Failed || outcome.ErrorClass != promotionsteps.FailureNotWired {
		t.Fatalf("outcome = %+v, want failed PORT_NOT_CONFIGURED: the node reached dispatch inside the transaction", outcome)
	}
	if outcome.NodeID != promotionexec.NodeCompensateHold {
		t.Fatalf("outcome NodeID = %q, want %q", outcome.NodeID, promotionexec.NodeCompensateHold)
	}
}

var (
	ext001ProducedAt     = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	ext001EffectiveStart = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
)

func ext001SeedTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'wf-ext-001 test', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "wfext001-"+tenantID.String()[:8])
	return tenantID
}

func ext001InTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// ext001SeedHold cuts a real budget hold for the intent's own recorded
// proposal revision: pool, intent instance, materialized revision and the
// held reservation, following the promotionbudget store's own fixture shape.
func ext001SeedHold(t *testing.T, db *pgtest.DB, tenantID, intentID, proposal uuid.UUID, materialDigest string) {
	t.Helper()
	ctx := context.Background()
	db.Exec(t, `INSERT INTO intent_instance
		(tenant_id, intent_id, definition_ref, definition_version, request_digest, idempotency_key,
		 request_state, execution_state, business_state, consistency_state, obligation_state,
		 created_at, last_transition_at)
		VALUES ($1,$2,'promotion.default/v1',1,$3,$4,'APPROVED','SCHEDULED','IN_PROGRESS','PENDING_OBSERVATION','PENDING',$5,$5)`,
		tenantID, intentID, strings.Repeat("b", 64), "wfext001-"+intentID.String(), ext001ProducedAt)
	start, err := values.ParseLocalDate("2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenLocalDateInterval(start, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	dto := intent.ProposalRevision{
		ProposalRevisionID: proposal.String(), IntentID: intentID.String(), Revision: 1,
		Tenant: values.TenantId(tenantID.String()), OrganizationScopeID: "org:people-ops",
		EffectiveTime: effective, MaterialDigest: digest.Reference{ProfileID: "hcmnext.proposal", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: materialDigest},
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "ial:test"},
		Subjects:  []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: uuid.NewString(), AuthorityDomain: "PEOPLE"}},
		CreatedAt: values.NewInstant(ext001ProducedAt),
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest: "sha256:capability", PolicyBundleDigest: "sha256:policy",
			LegalContextDigest: "sha256:legal", EntitlementDigest: "sha256:entitlement",
			ReferenceDataDigest: "sha256:reference", ClassificationTaxonomyDigest: "sha256:classification",
			ClassificationLabelSetDigest: "sha256:labels", ClassificationPropagationWatermark: "sha256:watermark",
			DLPDecisionDigest: "sha256:dlp",
		},
	}
	payload, err := intentcontrol.EncodeFullProposal(dto)
	if err != nil {
		t.Fatal(err)
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := demoworkforce.SeedAggregateCatalog(ctx, tx, tenantID, demoworkforce.AggregateCatalog{
			LegalEntityName: "WF-EXT-001 Entity", BudgetOrgUnits: []string{"people-ops"},
			BudgetCurrency: "USD", BudgetAmount: "1000000.00", RecordedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Jobs:      []demoworkforce.CatalogJob{{Code: "OPS-HRBP3", Grade: "P3"}},
			Vacancies: []demoworkforce.CatalogVacancy{{OrgUnit: "people-ops", JobCode: "OPS-HRBP3", Grade: "P3"}},
		}); err != nil {
			return err
		}
		if _, err := (intentcontrol.RevisionStore{}).Materialize(ctx, tx, intentcontrol.Revision{
			TenantID: tenantID, IntentID: intentID, Revision: 1, ProposalDigest: materialDigest, MaterialDigest: materialDigest,
			SchemaRef: "hcmnext.proposal.full/v1", Payload: payload, ProducedBy: "test", ProducedAt: ext001ProducedAt,
		}); err != nil {
			return err
		}
		held, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{
			ProposalRevisionID: proposal, OrgUnit: "people-ops", Amount: "5000.00",
			ProducedAt: ext001ProducedAt, EffectiveStart: ext001EffectiveStart,
		})
		if err != nil {
			return err
		}
		if !held {
			t.Fatal("ReserveProposalBudget cut no hold for the seeded pool")
		}
		return nil
	})
}

func ext001RecordDelegation(t *testing.T, db *pgtest.DB, tenantID, instanceID uuid.UUID) {
	t.Helper()
	db.Exec(t, `INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.promotion', 1,
			'0000000000000000000000000000000000000000000000000000000000000000', 'EXECUTE', 'RUNNING', 'sha256:input',
			ARRAY['compensate_budget_hold'], $3, $4)`,
		tenantID, instanceID, "corr-"+instanceID.String(), ext001ProducedAt)
	db.Exec(t, `INSERT INTO workflow_execution_delegation
		(tenant_id, instance_id, subject, subject_kind, tenant_key, organization_scope_id, roles, purposes,
		 authentication_method, assurance, session_ref, evidence_ref, recorded_at)
		VALUES ($1,$2,'hc-050-rafael-torres','human','harborcare','org:harborcare:people-ops',
			'{promotion_operator}','{compensation_review}','bearer_token','substantial','session-1','ev:authn:abc',$3)
		ON CONFLICT (tenant_id, instance_id) DO NOTHING`,
		tenantID, instanceID, ext001ProducedAt)
}

func ext001CompensateRequest(t *testing.T, tenantID, instanceID uuid.UUID, intentID, proposal, materialDigest string) execute.StepRequest {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	node, ok := plan.Node(promotionexec.NodeCompensateHold)
	if !ok {
		t.Fatalf("compiled plan carries no node %q", promotionexec.NodeCompensateHold)
	}
	return execute.StepRequest{
		TenantID: tenantID, InstanceID: instanceID, Attempt: 1,
		Node: node, Plan: plan,
		Proposal: runtime.ProposalBinding{Revision: intent.ProposalRevision{
			IntentID: intentID, ProposalRevisionID: proposal, Revision: 1,
			MaterialDigest: digest.Reference{Digest: materialDigest},
		}},
		RecordedAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	}
}

func ext001ReleaseInTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ports *promotionStepPorts, req execute.StepRequest, commit bool) promotionsteps.HoldReleaseResult {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	result, err := ports.ReleaseHold(withStepTx(ctx, tx), req)
	if err != nil {
		t.Fatalf("ReleaseHold in the advance transaction: %v", err)
	}
	if commit {
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	return result
}

// TestTodo_WF_EXT_001_Integration releases a real budget hold through the
// served compensation port inside the advance transaction and proves the
// release happens exactly once: the first call unwinds the hold with
// COMPENSATED status, the redelivered call reports COMPENSATED without
// writing a second release.
func TestTodo_WF_EXT_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := ext001SeedTenant(t, db)
	intentID, proposal, instanceID := uuid.New(), uuid.New(), uuid.New()
	materialDigest := strings.Repeat("c", 64)
	ext001SeedHold(t, db, tenantID, intentID, proposal, materialDigest)
	ext001RecordDelegation(t, db, tenantID, instanceID)

	ports := &promotionStepPorts{}
	ports.services = &fakeStepServices{}
	req := ext001CompensateRequest(t, tenantID, instanceID, intentID.String(), proposal.String(), materialDigest)

	first := ext001ReleaseInTx(t, db, tenantID, ports, req, true)
	if first.Status != "COMPENSATED" {
		t.Fatalf("first release status = %q, want COMPENSATED", first.Status)
	}
	if strings.TrimSpace(first.Artifact.OutputDigest) == "" {
		t.Fatal("first release carries no output digest")
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the compensation = %q, want RELEASED", reservation.Status)
		}
		return nil
	})

	second := ext001ReleaseInTx(t, db, tenantID, ports, req, true)
	if second.Status != "COMPENSATED" {
		t.Fatalf("redelivered release status = %q, want COMPENSATED", second.Status)
	}
	if second.Artifact.OutputDigest == first.Artifact.OutputDigest {
		t.Fatal("redelivered release reused the first output digest: the resume must observe that no hold is open")
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the redelivery = %q, want RELEASED", reservation.Status)
		}
		return nil
	})
}

// TestTodo_WF_EXT_001_Fault proves the compensation is atomic with the
// advance transaction: a release rolled back by a mid-compensation crash
// leaves the hold open (nothing is half-unwound), and the resume after the
// crash releases it exactly once.
func TestTodo_WF_EXT_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := ext001SeedTenant(t, db)
	intentID, proposal, instanceID := uuid.New(), uuid.New(), uuid.New()
	materialDigest := strings.Repeat("d", 64)
	ext001SeedHold(t, db, tenantID, intentID, proposal, materialDigest)
	ext001RecordDelegation(t, db, tenantID, instanceID)

	ports := &promotionStepPorts{}
	ports.services = &fakeStepServices{}
	req := ext001CompensateRequest(t, tenantID, instanceID, intentID.String(), proposal.String(), materialDigest)

	// The crash: the release runs but its transaction never commits.
	lost := ext001ReleaseInTx(t, db, tenantID, ports, req, false)
	if lost.Status != "COMPENSATED" {
		t.Fatalf("crashed release status = %q, want COMPENSATED", lost.Status)
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationHeld {
			t.Fatalf("reservation after the crash = %q, want HELD: the rolled-back release must unwind nothing", reservation.Status)
		}
		return nil
	})

	// The resume: the redriven compensation releases the still-open hold.
	// It runs as a new attempt with its own recorded instant.
	req.RecordedAt = req.RecordedAt.Add(time.Second)
	resumed := ext001ReleaseInTx(t, db, tenantID, ports, req, true)
	if resumed.Status != "COMPENSATED" {
		t.Fatalf("resumed release status = %q, want COMPENSATED", resumed.Status)
	}
	if strings.TrimSpace(resumed.Artifact.OutputDigest) == "" {
		t.Fatal("resumed release carries no output digest")
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the resume = %q, want RELEASED", reservation.Status)
		}
		return nil
	})
	if resumed.Artifact.OutputDigest == lost.Artifact.OutputDigest {
		t.Fatal("resumed release reused the crashed attempt's digest: each attempt must record its own outcome")
	}
}
