package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-027: the durable ProposalFacts/ApprovalFacts adapter, proved against
// embedded PostgreSQL rather than against a double. The in-memory adapters in
// internal/workflow/runtime still prove Start's own refusal vocabulary; these
// prove that the rows migration 00024 actually stores are the rows Start would
// read.
//
// This file reuses [wfInspectorTenant], [wfInspectorConn] and [wfInspectorTx]
// from workflow_inspector_test.go, and the single TestMain that file declares.

// pfBeginner is a dbport.Beginner that never opens anything. It stands in for
// "this cell was composed with an execution database" in the pure composition
// test below, which never reaches a statement.
type pfBeginner struct{}

func (pfBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("pfBeginner opens nothing")
}

// pfClock is the instant every fixture here stamps.
var pfClock = time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

// pfDigest mints a well-formed content_digest from a label.
func pfDigest(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

// pfIntent inserts one intent_instance row, the parent every intent-control
// table keys on, and returns its id.
func pfIntent(t *testing.T, db *pgtest.DB, tenant uuid.UUID, idempotencyKey string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			created_at, last_transition_at)
		VALUES ($1, $2, 'promotion.request', 1, $3, $4,
			'DRAFT', 'NOT_PLANNED', 'NOT_STARTED', 'NOT_APPLICABLE', 'NOT_APPLICABLE',
			$5, $5)`,
		tenant, id, pfDigest("request:"+idempotencyKey), idempotencyKey, pfClock)
	return id
}

func pfTxErr(ctx context.Context, db dbport.Beginner, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// pfRevision is the in-memory revision the adapter is asked about.
func pfRevision(intentID uuid.UUID, materialDigest string) intent.ProposalRevision {
	effective, err := values.NewOpenInstantInterval(values.NewInstant(pfClock))
	if err != nil {
		panic(err)
	}
	return intent.ProposalRevision{
		ProposalRevisionID:  "revision:" + intentID.String(),
		IntentID:            intentID.String(),
		Revision:            simulationRevision,
		Tenant:              values.TenantId(intentID.String()),
		OrganizationScopeID: "org:test",
		Subjects:            []intent.SubjectReference{{Kind: "WORKER", SubjectID: "worker:test", AuthorityDomain: "PEOPLE"}},
		EffectiveTime:       effective,
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest: "cap", PolicyBundleDigest: "policy", LegalContextDigest: "legal",
			EntitlementDigest: "entitlement", ReferenceDataDigest: "reference",
			ClassificationTaxonomyDigest: "taxonomy", DLPDecisionDigest: "dlp",
		},
		CreatedBy:      intent.PrincipalReference{PrincipalID: "principal:test", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "aal2"},
		CreatedAt:      pfClockInstant(),
		MaterialDigest: digest.Reference{Digest: materialDigest, AlgorithmID: "sha256"},
	}
}

func pfClockInstant() values.Instant { return values.NewInstant(pfClock) }

type pfProposalDigester struct{}

func (pfProposalDigester) RequestDigest(intent.Instance) (digest.Reference, error) {
	return digest.Reference{}, errors.New("request digest is not used")
}
func (pfProposalDigester) ProposalDigest(p intent.ProposalRevision) (digest.Reference, error) {
	return p.MaterialDigest, nil
}

// pfAuthorization is the AUTHZ admission the journey records before a start.
func pfAuthorization(tenant, intentID uuid.UUID, materialDigest string) executionDecision {
	p := pfRevision(intentID, materialDigest)
	p.Tenant = values.TenantId(tenant.String())
	return executionDecision{
		TenantID:         tenant,
		IntentID:         intentID,
		Revision:         simulationRevision,
		MaterialDigest:   materialDigest,
		ControlDigest:    pfDigest("control"),
		RequirementID:    executionAuthorityRequirementID,
		Kind:             intentcontrol.DecisionAuthZ,
		Outcome:          intentcontrol.OutcomeApproved,
		DecidedBy:        "principal:operator",
		AuthorityRef:     "sha256:test-authority",
		Reason:           "admitted",
		DecidedAt:        pfClock,
		Proposal:         &p,
		ProposalVerifier: pfProposalDigester{},
		TenantUUID:       func(values.TenantId) uuid.UUID { return tenant },
	}
}

func TestDurableProposalFactsReadsARecordedDecision(t *testing.T) {
	db := pgtest.New(t)
	conn := wfInspectorConn(t, db)
	tenant := wfInspectorTenant(t, db, "pf-decision")
	intentID := pfIntent(t, db, tenant, "pf-decision-1")
	material := pfDigest("material:pf-decision")
	admission := pfAuthorization(tenant, intentID, material)

	// Before anything is recorded, the adapter reports no decisions at all --
	// which is what makes runtime.Start refuse an unapproved proposal rather
	// than admit one on a caller's say-so.
	var before []runtime.ApprovalDecisionFact
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		before, err = DurableProposalFacts{}.Decisions(t.Context(), tx, tenant, pfRevision(intentID, material))
		return err
	})
	if len(before) != 0 {
		t.Fatalf("Decisions before any record = %v, want none", before)
	}

	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		return admission.record(t.Context(), tx)
	})

	var after []runtime.ApprovalDecisionFact
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		after, err = DurableProposalFacts{}.Decisions(t.Context(), tx, tenant, pfRevision(intentID, material))
		return err
	})
	if len(after) != 1 {
		t.Fatalf("Decisions after recording = %v, want exactly one", after)
	}
	if after[0].Outcome != runtime.ApprovalOutcomeApproved {
		t.Errorf("Outcome = %q, want APPROVED", after[0].Outcome)
	}
	if after[0].ProposalDigest != material {
		t.Errorf("ProposalDigest = %q, want %q", after[0].ProposalDigest, material)
	}
	if after[0].DecisionID != admission.decisionID().String() {
		t.Errorf("DecisionID = %q, want %q", after[0].DecisionID, admission.decisionID())
	}
	if after[0].Invalidated {
		t.Error("intent_decision has no invalidation column; the fact must never claim one")
	}
}

func TestDurableProposalFactsReportsADecisionBoundToAnotherDigest(t *testing.T) {
	db := pgtest.New(t)
	conn := wfInspectorConn(t, db)
	tenant := wfInspectorTenant(t, db, "pf-binding")
	intentID := pfIntent(t, db, tenant, "pf-binding-1")
	recorded := pfDigest("material:recorded")
	started := pfDigest("material:started")

	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		return pfAuthorization(tenant, intentID, recorded).record(t.Context(), tx)
	})

	var facts []runtime.ApprovalDecisionFact
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		facts, err = DurableProposalFacts{}.Decisions(t.Context(), tx, tenant, pfRevision(intentID, started))
		return err
	})
	if len(facts) != 1 || facts[0].ProposalDigest != recorded {
		t.Fatalf("Decisions = %v, want one bound to %q", facts, recorded)
	}
	// The adapter reports the binding rather than filtering it out: refusing
	// it is runtime.Start's job (CodeApprovalBindingMismatch), and a filter
	// here would silently turn a mismatch into "unapproved".
	if facts[0].ProposalDigest == started {
		t.Error("the adapter must not rewrite a decision's binding to the revision being started")
	}
}

func TestDurableProposalFactsReadsASupersedesEdge(t *testing.T) {
	db := pgtest.New(t)
	conn := wfInspectorConn(t, db)
	tenant := wfInspectorTenant(t, db, "pf-supersession")
	superseded := pfIntent(t, db, tenant, "pf-supersession-old")
	superseder := pfIntent(t, db, tenant, "pf-supersession-new")
	material := pfDigest("material:pf-supersession")

	var fact runtime.ProposalSupersessionFact
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		fact, err = DurableProposalFacts{}.Supersession(t.Context(), tx, tenant, pfRevision(superseded, material))
		return err
	})
	if fact.Superseded {
		t.Fatalf("Supersession with no edge = %+v, want not superseded", fact)
	}

	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		return (intentcontrol.RelationshipStore{}).Link(t.Context(), tx, intentcontrol.Relationship{
			TenantID:            tenant,
			RelationshipID:      uuid.New(),
			Type:                intentcontrol.RelationSupersedes,
			Parent:              superseder,
			Child:               superseded,
			Ordinal:             1,
			MaterialInputDigest: material,
			EstablishedAt:       pfClock,
		})
	})

	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		fact, err = DurableProposalFacts{}.Supersession(t.Context(), tx, tenant, pfRevision(superseded, material))
		return err
	})
	if !fact.Superseded {
		t.Fatal("a revision whose intent carries an inbound SUPERSEDES edge must be reported superseded")
	}
	if !strings.Contains(fact.SupersededByRevisionID, superseder.String()) {
		t.Errorf("SupersededByRevisionID = %q, want it to name %s", fact.SupersededByRevisionID, superseder)
	}
	if fact.CurrentRevision != nil {
		t.Error("this adapter carries no superseding proposal content and must not claim to")
	}
}

func TestExecutionDecisionRecordIsIdempotentAndRefusesADigestChange(t *testing.T) {
	db := pgtest.New(t)
	conn := wfInspectorConn(t, db)
	tenant := wfInspectorTenant(t, db, "pf-idempotent")
	intentID := pfIntent(t, db, tenant, "pf-idempotent-1")
	material := pfDigest("material:pf-idempotent")
	admission := pfAuthorization(tenant, intentID, material)

	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := admission.record(t.Context(), tx); err != nil {
			return err
		}
		return admission.record(t.Context(), tx)
	})

	var facts []runtime.ApprovalDecisionFact
	wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		facts, err = DurableProposalFacts{}.Decisions(t.Context(), tx, tenant, pfRevision(intentID, material))
		return err
	})
	if len(facts) != 1 {
		t.Fatalf("recording the same decision twice produced %d rows, want 1", len(facts))
	}
	provenanceDrift := pfAuthorization(tenant, intentID, material)
	provenanceDrift.DecidedBy = "principal:other"
	provenanceDrift.Proposal.CreatedAt = values.NewInstant(pfClock.Add(time.Second))
	provenanceDrift.Proposal.ControlSnapshots.PolicyBundleDigest = "policy:changed"
	err := pfTxErr(t.Context(), conn, tenant, func(tx dbport.Tx) error {
		return provenanceDrift.record(t.Context(), tx)
	})
	if err != nil {
		t.Fatalf("non-material resimulation should retain the immutable stored snapshot: %v", err)
	}

	// A second proposal wearing the same revision number cannot borrow the
	// first one's stored row: the revision is append-only, so the recorder
	// refuses rather than binding a decision to content nobody decided on.
	drifted := pfAuthorization(tenant, intentID, pfDigest("material:drifted"))
	drifted.DecidedBy = "principal:someone-else"
	ctx := t.Context()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	var conflict critical.ErrProposalRevisionConflict
	if err := drifted.record(ctx, tx); !errors.As(err, &conflict) {
		t.Fatalf("record(drifted) = %v, want an immutable stored-proposal mismatch", err)
	}
	if conflict.IntentID != intentID || conflict.Revision != 1 {
		t.Fatalf("conflict = %+v, want the conflicting intent and revision", conflict)
	}
}

func TestExecutionFactsForRequiresTheExecutionDatabase(t *testing.T) {
	if facts := executionFactsFor(CellConfig{}); facts != nil {
		t.Errorf("executionFactsFor(no database) = %v, want nil", facts)
	}
	if facts := executionFactsFor(CellConfig{ExecutionDB: pfBeginner{}}); facts != nil {
		t.Errorf("executionFactsFor(no tenant mapper) = %v, want nil", facts)
	}
	cfg := CellConfig{
		ExecutionDB: pfBeginner{},
		TenantUUID:  func(values.TenantId) uuid.UUID { return uuid.Nil },
	}
	if facts := executionFactsFor(cfg); facts == nil {
		t.Fatal("executionFactsFor(database + tenant mapper) must supply the durable facts")
	}
	if proposalFactsOf(nil) != nil || approvalFactsOf(nil) != nil {
		t.Error("a nil ExecutionFacts must project onto two nil ports, never onto typed nils")
	}
	facts := executionFactsFor(cfg)
	if proposalFactsOf(facts) == nil || approvalFactsOf(facts) == nil {
		t.Error("a non-nil ExecutionFacts must project onto two non-nil ports")
	}
}

func TestExecutionIntentUUIDRefusesANonUUIDIntentID(t *testing.T) {
	if _, err := executionIntentUUID("intent:not-a-uuid"); err == nil {
		t.Fatal("a non-uuid intent id must be refused, not silently read as no decisions")
	}
	id := uuid.New()
	got, err := executionIntentUUID(id.String())
	if err != nil || got != id {
		t.Fatalf("executionIntentUUID(%s) = %v/%v, want the same id", id, got, err)
	}
}

func TestControlSnapshotDigestIsDeterministicAndFieldSensitive(t *testing.T) {
	snapshots := intent.ControlSnapshots{
		CapabilityRegistryDigest: "cap-1",
		PolicyBundleDigest:       "policy-1",
		LegalContextDigest:       "legal-1",
	}
	first := controlSnapshotDigest(snapshots)
	if first != controlSnapshotDigest(snapshots) {
		t.Fatal("the same control snapshots must digest to the same value")
	}
	if len(first) != 64 {
		t.Fatalf("control digest %q is %d characters, want 64 hex", first, len(first))
	}
	snapshots.PolicyBundleDigest = "policy-2"
	if controlSnapshotDigest(snapshots) == first {
		t.Fatal("a changed control snapshot must change the digest")
	}
	if empty := controlSnapshotDigest(intent.ControlSnapshots{}); len(empty) != 64 {
		t.Fatalf("an unpinned control context must still digest to 64 hex, got %q", empty)
	}
}

func TestExecutionDecisionIDIsDerivedFromItsOwnTuple(t *testing.T) {
	base := pfAuthorization(uuid.New(), uuid.New(), pfDigest("material:id"))
	firstDecisionID, secondDecisionID := base.decisionID(), base.decisionID()
	if firstDecisionID != secondDecisionID {
		t.Fatal("the same decision must derive the same id")
	}
	for name, mutate := range map[string]func(*executionDecision){
		"requirement": func(d *executionDecision) { d.RequirementID = "other" },
		"principal":   func(d *executionDecision) { d.DecidedBy = "principal:other" },
		"outcome":     func(d *executionDecision) { d.Outcome = intentcontrol.OutcomeRejected },
		"digest":      func(d *executionDecision) { d.MaterialDigest = pfDigest("material:other") },
		"revision":    func(d *executionDecision) { d.Revision = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			other := base
			mutate(&other)
			if other.decisionID() == base.decisionID() {
				t.Fatalf("changing the %s must derive a different decision id", name)
			}
		})
	}
	// The reason and the instant are provenance, not identity: re-recording
	// the same decision a moment later must still collide rather than mint a
	// second vote.
	later := base
	later.DecidedAt = base.DecidedAt.Add(time.Hour)
	later.Reason = "re-recorded"
	if later.decisionID() != base.decisionID() {
		t.Error("the recording instant and reason must not change a decision's identity")
	}
}

func TestExecutionAuthorityDecisionRefFallsBackToTheRule(t *testing.T) {
	var missing *ExecutionAuthority
	if got := missing.decisionRef(); got != ruleExecutionAuthorityGate {
		t.Errorf("(nil).decisionRef() = %q, want %q", got, ruleExecutionAuthorityGate)
	}
	if got := (&ExecutionAuthority{AuthorityDigest: "  "}).decisionRef(); got != ruleExecutionAuthorityGate {
		t.Errorf("blank digest decisionRef() = %q, want the rule reference", got)
	}
	if got := (&ExecutionAuthority{AuthorityDigest: " sha256:x "}).decisionRef(); got != "sha256:x" {
		t.Errorf("decisionRef() = %q, want the trimmed digest", got)
	}
}

func TestNonEmptyReasonFallsBackOnBlankInput(t *testing.T) {
	if got := nonEmptyReason("  ", "fallback"); got != "fallback" {
		t.Errorf("nonEmptyReason(blank) = %q, want the fallback", got)
	}
	if got := nonEmptyReason(" stated ", "fallback"); got != "stated" {
		t.Errorf("nonEmptyReason(stated) = %q, want the trimmed reason", got)
	}
}
