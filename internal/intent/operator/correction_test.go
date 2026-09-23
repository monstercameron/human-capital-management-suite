package operator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/correction"
)

var (
	rev00703Now       = time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	rev00703Occurred  = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	rev00703Effective = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

func rev00703Request() intent.OperatorActionRequest {
	return intent.OperatorActionRequest{
		IntentInstanceID: "intent:rev00703",
		Operation:        intent.OperationLedgerCorrection,
		Tenant:           "tenant-rev00703",
		Target:           "worker:promotion",
		IdempotencyKey:   "correction:rev00703",
		Simulated:        true,
		SimulationRef:    "sim:rev00703",
		JITGrant:         "jit:rev00703",
		JITExpires:       values.NewInstant(rev00703Now.Add(time.Hour)),
	}
}

func rev00703Correction(tenant uuid.UUID) correction.Request {
	return correction.Request{
		Tenant: tenant, StreamKey: "worker:promotion", ExpectedHead: 1,
		Target:    ledger.EventRef{StreamKey: "worker:promotion", Sequence: 1},
		Authority: "hcmnext:people", SourceRef: "hcmnext:test:promotion-correction",
		SchemaRef: "hcmnext.people.v1.Promotion@1", Payload: []byte("promotion:principal"),
		OccurredAt: rev00703Occurred, EffectiveAt: rev00703Effective,
		CorrelationID: uuid.New(), IdempotencyKey: "correction:rev00703",
		Reason: "promotion fact was recorded with the wrong level", CorrectedBy: "steward:people",
		Obligations: []correction.ReconciliationObligation{{
			EffectIdentity: "reconcile:promotion:rev00703", OrderingKey: "promotion:worker",
			SchemaRef: "hcmnext.reconciliation.v1", Payload: []byte("recalculate promotion consumers"),
		}},
	}
}

// TestTodo_REV_007_03 is the PRIMARY matrix entry: the ledger-correction
// kind is a valid governed mutation, and the resolver refuses unauthorized
// or unbound executions before touching storage (nil transaction: any
// refusal below proves no ledger read happened).
func TestTodo_REV_007_03(t *testing.T) {
	if !intent.OperationLedgerCorrection.Valid() {
		t.Fatal("LEDGER_CORRECTION is not a valid operation kind")
	}
	if intent.OperationLedgerCorrection.DualControl() {
		t.Fatal("LEDGER_CORRECTION must stay single-operator: the ledger preserves the original event")
	}
	at := values.NewInstant(rev00703Now)

	tenant := uuid.New()
	if _, err := operator.ExecuteLedgerCorrection(context.Background(), nil, rev00703Request(), at, rev00703Correction(tenant), nil); !errors.Is(err, correction.ErrInvalidRequest) {
		t.Fatalf("authorized shape with nil tx err = %v, want the transaction requirement (binding passed)", err)
	}

	undescribed := rev00703Request()
	undescribed.Simulated = false
	if _, err := operator.ExecuteLedgerCorrection(context.Background(), nil, undescribed, at, rev00703Correction(tenant), nil); !errors.Is(err, intent.ErrOperatorAction) {
		t.Fatalf("unsimulated routine err = %v, want ErrOperatorAction", err)
	}

	escaped := rev00703Correction(tenant)
	escaped.StreamKey = "worker:other"
	escaped.Target = ledger.EventRef{StreamKey: "worker:other", Sequence: 1}
	if _, err := operator.ExecuteLedgerCorrection(context.Background(), nil, rev00703Request(), at, escaped, nil); !errors.Is(err, intent.ErrOperatorAction) {
		t.Fatalf("off-target correction err = %v, want ErrOperatorAction", err)
	}
}

// TestTodo_REV_007_03_Security proves the binding is exact: a receipt for
// another operation, a mismatched idempotency key, a missing grant and an
// empty operation all refuse with the operator sentinel before any ledger
// access (nil transaction throughout).
func TestTodo_REV_007_03_Security(t *testing.T) {
	at := values.NewInstant(rev00703Now)
	tenant := uuid.New()

	for name, mutate := range map[string]func(*intent.OperatorActionRequest, *correction.Request){
		"wrong operation kind": func(req *intent.OperatorActionRequest, _ *correction.Request) {
			req.Operation = intent.OperationBreakGlass
			req.Approvers = []string{"operator-1", "operator-2"}
			req.Simulated = false
			req.Emergency = &intent.EmergencyBypass{Reason: "declared emergency", ReviewBy: values.NewInstant(rev00703Now.Add(24 * time.Hour))}
		},
		"idempotency mismatch": func(req *intent.OperatorActionRequest, creq *correction.Request) {
			creq.IdempotencyKey = "correction:forged"
		},
		"missing JIT grant": func(req *intent.OperatorActionRequest, _ *correction.Request) {
			req.JITGrant = ""
		},
		"lapsed grant": func(req *intent.OperatorActionRequest, _ *correction.Request) {
			req.JITExpires = values.NewInstant(rev00703Now.Add(-time.Hour))
		},
		"no intent instance": func(req *intent.OperatorActionRequest, _ *correction.Request) {
			req.IntentInstanceID = ""
		},
	} {
		req, creq := rev00703Request(), rev00703Correction(tenant)
		mutate(&req, &creq)
		if _, err := operator.ExecuteLedgerCorrection(context.Background(), nil, req, at, creq, nil); !errors.Is(err, intent.ErrOperatorAction) {
			t.Fatalf("%s: err = %v, want ErrOperatorAction", name, err)
		}
	}
}

// TestTodo_REV_007_03_Integration drives one correction from a governed
// intent instance through to the appended ledger row and its reconciliation
// obligation, then replays the same idempotency key and proves no duplicate
// effect lands.
func TestTodo_REV_007_03_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', 'Ledger correction', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "correction-"+tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String())
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1, 'hcmnext.people.v1.Promotion@1', 'hcmnext.people.v1.Promotion', 1, 'hcmnext.people.v1.Promotion', 'PROTOBUF', 'LEDGER_EVENT')`, tenant)
	db.Exec(t, `INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from) VALUES ($1, 'hcmnext:people', 'INTERNAL', 'people', timestamptz '2026-01-01T00:00:00Z')`, tenant)
	db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref) VALUES ($1, 'worker:promotion', 'WORKER', 'worker:promotion')`, tenant)
	db.Exec(t, `INSERT INTO stream_head (tenant_id, stream_key, head_sequence) VALUES ($1, 'worker:promotion', 0)`, tenant)
	seedTx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.New().Append(context.Background(), seedTx, ledger.AppendRequest{
		Tenant: tenant, StreamKey: "worker:promotion", ExpectedHead: 0,
		AssertionClass: ledger.DomainFact, Authority: "hcmnext:people", SourceRef: "hcmnext:test:promotion",
		SchemaRef: "hcmnext.people.v1.Promotion@1", Payload: []byte("promotion:manager"),
		OccurredAt: rev00703Occurred, EffectiveAt: rev00703Effective,
		CorrelationID: uuid.New(), IdempotencyKey: "promotion-original",
	}); err != nil {
		_ = seedTx.Rollback(context.Background())
		t.Fatalf("seed original: %v", err)
	}
	if err := seedTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	execute := func(t *testing.T) correction.Result {
		t.Helper()
		tx, err := db.Conn.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		result, err := operator.ExecuteLedgerCorrection(context.Background(), tx, rev00703Request(), values.NewInstant(rev00703Now), rev00703Correction(tenant), func() time.Time { return rev00703Now })
		if err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("execute correction: %v", err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := execute(t)
	if first.Correction.Sequence != 2 || first.Correction.Replayed || len(first.Obligations) != 1 {
		t.Fatalf("first correction = %+v, want successor sequence 2 and one obligation", first.Correction)
	}
	second := execute(t)
	if !second.Correction.Replayed || second.CorrectionID != first.CorrectionID {
		t.Fatalf("replay = %+v, want the same correction id replayed", second.Correction)
	}
	var total int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = 'worker:promotion'`, tenant).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("promotion ledger rows = %d, want original plus one correction", total)
	}
}
