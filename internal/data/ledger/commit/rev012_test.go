package commit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	ledgercommit "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

var revOccurredAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

func revDigestHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func revDraftLifecycle() *intentsv1.LifecycleDimensions {
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_DRAFT,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE,
	}
}

func revSubmittedLifecycle() *intentsv1.LifecycleDimensions {
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_SUBMITTED,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_PENDING,
	}
}

func revIntentPayload(t *testing.T, intentID uuid.UUID, idemKey string, version uint64, lifecycle *intentsv1.LifecycleDimensions) []byte {
	t.Helper()
	msg := &intentsv1.IntentInstance{
		IntentId:   intentID.String(),
		Definition: &intentsv1.DefinitionReference{IntentTypeId: "intent.worker.promote", Version: 1},
		CanonicalRequestDigest: &intentsv1.CanonicalDigestReference{
			Digest:      revDigestHex("request:" + idemKey),
			AlgorithmId: "sha256",
		},
		IdempotencyKey:   idemKey,
		Lifecycle:        lifecycle,
		InstanceVersion:  version,
		CreatedAt:        timestamppb.New(revOccurredAt),
		RecordedAt:       timestamppb.New(revOccurredAt),
		LastTransitionAt: timestamppb.New(revOccurredAt),
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal IntentInstance: %v", err)
	}
	return b
}

func revProposalPayload(t *testing.T, intentID uuid.UUID, revision uint64, producedBy string) []byte {
	t.Helper()
	proposalBytes := []byte(fmt.Sprintf("proposal-bytes-%s-%d", intentID, revision))
	msg := &intentsv1.ProposalRevision{
		ProposalRevisionId: uuid.NewString(),
		IntentId:           intentID.String(),
		Revision:           revision,
		MaterialProposalDigest: &intentsv1.CanonicalDigestReference{
			Digest:      revDigestHex(fmt.Sprintf("material:%s:%d", intentID, revision)),
			AlgorithmId: "sha256",
		},
		Proposal: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "hcmnext.intents.v1.BusinessIntent", Version: 1},
			ProtobufWireBytes: proposalBytes,
			CanonicalDigest: &intentsv1.CanonicalDigestReference{
				Digest:      revDigestHex(string(proposalBytes)),
				AlgorithmId: "sha256",
			},
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: producedBy, Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
		CreatedAt: timestamppb.New(revOccurredAt),
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ProposalRevision: %v", err)
	}
	return b
}

type revSetup struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	intentID uuid.UUID
	stream   string
}

func newRevSetup(t *testing.T) revSetup {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	intentID := uuid.New()
	stream := "intent:" + intentID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'REV 012 01', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, tenant.String())
	for _, sr := range []struct {
		ref  string
		id   string
		ver  int
		name string
	}{
		{commitSchema, commitSchema, 1, commitSchema},
		{critical.SchemaRefIntentInstance, "hcmnext.intents.v1.IntentInstance", 1, "hcmnext.intents.v1.IntentInstance"},
		{critical.SchemaRefProposalRevision, "hcmnext.intents.v1.ProposalRevision", 1, "hcmnext.intents.v1.ProposalRevision"},
	} {
		db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $3, $4, $5, 'PROTOBUF', 'LEDGER_EVENT')`, tenant, sr.ref, sr.id, sr.ver, sr.name)
	}
	inTx(t, db, func(tx dbport.Tx) error {
		if err := ledger.EnsureStream(context.Background(), tx, tenant, stream, "TRANSACTION", intentID.String()); err != nil {
			return err
		}
		return projection.EnsureProjection(context.Background(), tx, tenant, "workflow.promotion_outcome", stream)
	})
	return revSetup{db: db, tenant: tenant, intentID: intentID, stream: stream}
}

func (s revSetup) commitReq(schemaRef string, payload []byte, expectedHead int64, idem string) ledgercommit.Request {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	return ledgercommit.Request{
		Append: ledger.AppendRequest{
			Tenant: s.tenant, StreamKey: s.stream, ExpectedHead: expectedHead,
			AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:test",
			SchemaRef: schemaRef, Payload: payload,
			OccurredAt: now, EffectiveAt: now, CorrelationID: uuid.New(),
			IdempotencyKey: idem,
		},
		Projection: "workflow.promotion_outcome",
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "promotion:" + s.stream + ":" + idem,
			OrderingKey:    s.stream + ":" + idem, SchemaRef: commitSchema, Payload: []byte("promotion-outcome"),
		},
		Provenance: provenance.PublishRequest{
			Tenant: s.tenant, IntentRef: "intent:" + s.stream + ":" + idem,
			SourceAuthority: "hcmnext:test", PrincipalRef: "tester",
			EvidenceIDs: []string{"evidence:" + s.stream + ":" + idem},
		},
		RecordedAt: now,
	}
}

func revCommit(t *testing.T, s revSetup, req ledgercommit.Request) ledgercommit.Receipt {
	t.Helper()
	tx, err := s.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ledgercommit.Commit(context.Background(), tx, ledger.New(), req)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("commit: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return receipt
}

// TestTodo_REV_012_01 proves the ledger-commit path invokes the
// critical-projection materializer in the same transaction: committing an
// IntentInstance event and a ProposalRevision event through ledgercommit.Commit
// produces the intent_instance and proposal_revision rows with the critical
// checkpoint advanced, and critical.Verify reconstructs them from the ledger.
func TestTodo_REV_012_01(t *testing.T) {
	s := newRevSetup(t)
	idemKey := "idem-" + s.intentID.String()

	creation := revIntentPayload(t, s.intentID, idemKey, 1, revDraftLifecycle())
	receipt := revCommit(t, s, s.commitReq(critical.SchemaRefIntentInstance, creation, 0, "rev012-creation"))
	if !receipt.Critical.Applied || receipt.Critical.Target != critical.TargetIntentInstance {
		t.Fatalf("creation critical = %+v, want Applied=true Target=INTENT_INSTANCE", receipt.Critical)
	}
	if !receipt.Projection.Applied {
		t.Fatalf("creation generic projection Applied=false, want true")
	}
	var requestState string
	var instanceVersion int64
	if err := s.db.Conn.QueryRow(context.Background(),
		`SELECT request_state, instance_version FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		s.tenant, s.intentID).Scan(&requestState, &instanceVersion); err != nil {
		t.Fatalf("read intent_instance: %v (critical projection did not write the row in the commit transaction)", err)
	}
	if requestState != "DRAFT" || instanceVersion != 1 {
		t.Fatalf("intent_instance = (%s, %d), want (DRAFT, 1)", requestState, instanceVersion)
	}
	cp, err := projection.Read(context.Background(), s.db.Conn, s.tenant, critical.ProjectionName, s.stream)
	if err != nil {
		t.Fatalf("read critical checkpoint: %v", err)
	}
	if cp.LastAppliedSequence != receipt.Ledger.Sequence {
		t.Fatalf("critical checkpoint at %d, want ledger sequence %d", cp.LastAppliedSequence, receipt.Ledger.Sequence)
	}

	revision := revProposalPayload(t, s.intentID, 1, "principal:manager-1")
	receipt2 := revCommit(t, s, s.commitReq(critical.SchemaRefProposalRevision, revision, 1, "rev012-rev1"))
	if !receipt2.Critical.Applied || receipt2.Critical.Target != critical.TargetProposalRevision {
		t.Fatalf("revision critical = %+v, want Applied=true Target=PROPOSAL_REVISION", receipt2.Critical)
	}
	var producedBy string
	if err := s.db.Conn.QueryRow(context.Background(),
		`SELECT produced_by FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2 AND revision = 1`,
		s.tenant, s.intentID).Scan(&producedBy); err != nil {
		t.Fatalf("read proposal_revision: %v (critical projection did not write the row in the commit transaction)", err)
	}
	if producedBy != "principal:manager-1" {
		t.Fatalf("produced_by = %q, want %q", producedBy, "principal:manager-1")
	}

	vr, err := critical.Verify(context.Background(), s.db.Conn, ledger.NewReader(), critical.ProtoMapper{}, s.tenant, s.stream)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK() {
		t.Fatalf("verify found diffs: %+v", vr.Diffs)
	}
}

// TestTodo_REV_012_01_Golden pins the exact row bytes for one fixed scenario
// so a swapped column or mis-cased enum fails here.
func TestTodo_REV_012_01_Golden(t *testing.T) {
	s := newRevSetup(t)
	intentID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s.intentID = intentID
	s.stream = "intent:" + intentID.String()
	inTx(t, s.db, func(tx dbport.Tx) error {
		if err := ledger.EnsureStream(context.Background(), tx, s.tenant, s.stream, "TRANSACTION", intentID.String()); err != nil {
			return err
		}
		return projection.EnsureProjection(context.Background(), tx, s.tenant, "workflow.promotion_outcome", s.stream)
	})
	const idemKey = "idem-golden-rev012"

	creation := revIntentPayload(t, intentID, idemKey, 1, revDraftLifecycle())
	revCommit(t, s, s.commitReq(critical.SchemaRefIntentInstance, creation, 0, "rev012-golden-creation"))
	submitted := revIntentPayload(t, intentID, idemKey, 2, revSubmittedLifecycle())
	revCommit(t, s, s.commitReq(critical.SchemaRefIntentInstance, submitted, 1, "rev012-golden-submitted"))
	revision := revProposalPayload(t, intentID, 1, "principal:golden")
	revCommit(t, s, s.commitReq(critical.SchemaRefProposalRevision, revision, 2, "rev012-golden-rev1"))

	var (
		definitionRef, requestState, executionState, businessState, consistencyState, obligationState string
		instanceVersion                                                                               int64
	)
	if err := s.db.Conn.QueryRow(context.Background(), `
		SELECT definition_ref, request_state, execution_state, business_state, consistency_state, obligation_state, instance_version
		FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		s.tenant, intentID).Scan(&definitionRef, &requestState, &executionState, &businessState, &consistencyState, &obligationState, &instanceVersion); err != nil {
		t.Fatalf("read intent_instance: %v", err)
	}
	want := []string{"intent.worker.promote", "SUBMITTED", "SCHEDULED", "IN_PROGRESS", "PENDING_OBSERVATION", "PENDING"}
	got := []string{definitionRef, requestState, executionState, businessState, consistencyState, obligationState}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("intent_instance column %d = %q, want %q (full row %v)", i, got[i], want[i], got)
		}
	}
	if instanceVersion != 2 {
		t.Fatalf("instance_version = %d, want 2", instanceVersion)
	}
	var schemaRef, producedBy string
	if err := s.db.Conn.QueryRow(context.Background(),
		`SELECT schema_ref, produced_by FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2 AND revision = 1`,
		s.tenant, intentID).Scan(&schemaRef, &producedBy); err != nil {
		t.Fatalf("read proposal_revision: %v", err)
	}
	if schemaRef != "hcmnext.intents.v1.BusinessIntent@1" || producedBy != "principal:golden" {
		t.Fatalf("proposal_revision = (%q, %q), want (%q, %q)", schemaRef, producedBy, "hcmnext.intents.v1.BusinessIntent@1", "principal:golden")
	}
	vr, err := critical.Verify(context.Background(), s.db.Conn, ledger.NewReader(), critical.ProtoMapper{}, s.tenant, s.stream)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK() {
		t.Fatalf("verify found diffs against the golden scenario: %+v", vr.Diffs)
	}
}

// TestTodo_REV_012_01_Integration proves the rows are ledger-replayable and
// that RevisionStore.Materialize shares the one CAS-guarded writer: the same
// revision materializes as a no-op, a different digest for the same revision
// is a conflict, and non-critical commits never touch the critical tables.
func TestTodo_REV_012_01_Integration(t *testing.T) {
	s := newRevSetup(t)
	idemKey := "idem-" + s.intentID.String()

	creation := revIntentPayload(t, s.intentID, idemKey, 1, revDraftLifecycle())
	revCommit(t, s, s.commitReq(critical.SchemaRefIntentInstance, creation, 0, "rev012-int-creation"))
	revisionPayload := revProposalPayload(t, s.intentID, 1, "principal:manager-1")
	revCommit(t, s, s.commitReq(critical.SchemaRefProposalRevision, revisionPayload, 1, "rev012-int-rev1"))

	vr, err := critical.Verify(context.Background(), s.db.Conn, ledger.NewReader(), critical.ProtoMapper{}, s.tenant, s.stream)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vr.OK() {
		t.Fatalf("verify found diffs: %+v", vr.Diffs)
	}

	stored, err := (intentcontrol.RevisionStore{}).Load(context.Background(), s.db.Conn, s.tenant, s.intentID, 1)
	if err != nil {
		t.Fatalf("load stored revision: %v", err)
	}
	var created bool
	inTx(t, s.db, func(tx dbport.Tx) error {
		var err error
		created, err = (intentcontrol.RevisionStore{}).Materialize(context.Background(), tx, intentcontrol.Revision{
			TenantID:       stored.TenantID,
			IntentID:       stored.IntentID,
			Revision:       1,
			ProposalDigest: stored.ProposalDigest,
			MaterialDigest: stored.MaterialDigest,
			SchemaRef:      stored.SchemaRef,
			Payload:        stored.Payload,
			ProducedBy:     stored.ProducedBy,
			ProducedAt:     stored.ProducedAt,
		})
		return err
	})
	if created {
		t.Fatalf("Materialize of the already-projected revision reported created=true, want idempotent no-op (one writer)")
	}

	inTx(t, s.db, func(tx dbport.Tx) error {
		_, err := (intentcontrol.RevisionStore{}).Materialize(context.Background(), tx, intentcontrol.Revision{
			TenantID:       stored.TenantID,
			IntentID:       stored.IntentID,
			Revision:       1,
			ProposalDigest: revDigestHex("proposal:conflicting"),
			MaterialDigest: stored.MaterialDigest,
			SchemaRef:      stored.SchemaRef,
			Payload:        stored.Payload,
			ProducedBy:     stored.ProducedBy,
			ProducedAt:     stored.ProducedAt,
		})
		if err == nil {
			t.Fatalf("Materialize with a conflicting proposal digest succeeded, want a conflict error (CAS-guarded writer)")
		}
		return nil
	})

	plain := revCommit(t, s, s.commitReq(commitSchema, []byte("promotion-outcome"), 2, "rev012-int-plain"))
	if plain.Critical.Applied {
		t.Fatalf("non-critical commit reported Critical.Applied=true, want the critical projection untouched")
	}
	var n int
	if err := s.db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM proposal_revision WHERE tenant_id = $1`, s.tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("proposal_revision rows = %d after a non-critical commit, want 1 (untouched)", n)
	}
}

// TestTodo_REV_012_01_Mutation kills the semantic mutants around the wiring:
// a conflicting revision digest fails the commit, a stale instance version
// fails the commit without overwriting the row, and an exact replay is a
// no-op on both checkpoints.
func TestTodo_REV_012_01_Mutation(t *testing.T) {
	s := newRevSetup(t)
	idemKey := "idem-" + s.intentID.String()

	creation := revIntentPayload(t, s.intentID, idemKey, 1, revDraftLifecycle())
	first := revCommit(t, s, s.commitReq(critical.SchemaRefIntentInstance, creation, 0, "rev012-mut-creation"))
	revisionPayload := revProposalPayload(t, s.intentID, 1, "principal:manager-1")
	revCommit(t, s, s.commitReq(critical.SchemaRefProposalRevision, revisionPayload, 1, "rev012-mut-rev1"))

	conflicting := revProposalPayload(t, s.intentID, 1, "principal:manager-1")
	// Mutate the inner digest by flipping the produced_by-independent bytes:
	// remarshal with a different proposal body so the digest differs for the
	// same (tenant, intent, revision).
	_ = conflicting
	otherBytes := []byte(fmt.Sprintf("proposal-bytes-%s-%d-other", s.intentID, 1))
	otherMsg := &intentsv1.ProposalRevision{
		ProposalRevisionId: uuid.NewString(),
		IntentId:           s.intentID.String(),
		Revision:           1,
		MaterialProposalDigest: &intentsv1.CanonicalDigestReference{
			Digest:      revDigestHex(fmt.Sprintf("material:%s:%d", s.intentID, 1)),
			AlgorithmId: "sha256",
		},
		Proposal: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "hcmnext.intents.v1.BusinessIntent", Version: 1},
			ProtobufWireBytes: otherBytes,
			CanonicalDigest: &intentsv1.CanonicalDigestReference{
				Digest:      revDigestHex(string(otherBytes)),
				AlgorithmId: "sha256",
			},
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: "principal:manager-1", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
		CreatedAt: timestamppb.New(revOccurredAt),
	}
	otherPayload, err := proto.Marshal(otherMsg)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledgercommit.Commit(context.Background(), tx, ledger.New(), s.commitReq(critical.SchemaRefProposalRevision, otherPayload, 2, "rev012-mut-conflict"))
	_ = tx.Rollback(context.Background())
	if err == nil {
		t.Fatalf("conflicting proposal revision commit succeeded, want CRITICAL_PROJECTION_PROPOSAL_REVISION_CONFLICT")
	}
	var conflict critical.ErrProposalRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("conflicting commit error = %v, want ErrProposalRevisionConflict", err)
	}

	stale := revIntentPayload(t, s.intentID, idemKey, 1, revSubmittedLifecycle())
	tx2, err := s.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledgercommit.Commit(context.Background(), tx2, ledger.New(), s.commitReq(critical.SchemaRefIntentInstance, stale, 2, "rev012-mut-stale"))
	_ = tx2.Rollback(context.Background())
	if err == nil {
		t.Fatalf("stale instance_version commit succeeded, want CRITICAL_PROJECTION_STALE_INSTANCE_VERSION")
	}
	var staleErr critical.ErrStaleInstanceVersion
	if !errors.As(err, &staleErr) {
		t.Fatalf("stale commit error = %v, want ErrStaleInstanceVersion", err)
	}
	var instanceVersion int64
	if err := s.db.Conn.QueryRow(context.Background(),
		`SELECT instance_version FROM intent_instance WHERE tenant_id = $1 AND intent_id = $2`,
		s.tenant, s.intentID).Scan(&instanceVersion); err != nil {
		t.Fatal(err)
	}
	if instanceVersion != 1 {
		t.Fatalf("instance_version = %d after a refused stale commit, want 1 (unchanged)", instanceVersion)
	}

	replayTx, err := s.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := ledgercommit.Commit(context.Background(), replayTx, ledger.New(), s.commitReq(critical.SchemaRefIntentInstance, creation, 0, "rev012-mut-creation"))
	if err != nil {
		_ = replayTx.Rollback(context.Background())
		t.Fatalf("replay commit: %v", err)
	}
	_ = replayTx.Rollback(context.Background())
	if replayed.Ledger.EventID != first.Ledger.EventID {
		t.Fatalf("replay event %s != first event %s, want idempotent ledger replay", replayed.Ledger.EventID, first.Ledger.EventID)
	}
	if replayed.Critical.Applied {
		t.Fatalf("replay reported Critical.Applied=true, want idempotent no-op")
	}
}
