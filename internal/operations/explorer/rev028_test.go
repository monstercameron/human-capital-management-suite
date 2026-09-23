package explorer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_REV_028_01 proves the ledger correction and supersession lineage
// capability (LEDGER-005) is reachable through the governed operator path:
// explorer.RecordCorrection invokes lineage.Append to record a business
// correction, and explorer.EffectiveCurrent resolves current-effective truth
// back through lineage.EffectiveCurrent.
func TestTodo_REV_028_01(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	origin := f.appendLinked(t, eventSpec{Stream: streamA, Class: ledger.DomainFact, Payload: "120000"})
	if origin.Sequence != 1 {
		t.Fatalf("origin sequence = %d, want 1", origin.Sequence)
	}

	correctionReq := explorer.CorrectionRequest{
		StreamKey:      streamA,
		ExpectedHead:   origin.Sequence,
		Corrects:       explorer.EventRef{StreamKey: streamA, Sequence: origin.Sequence},
		Authority:      authorityRef,
		SourceRef:      "test:rev028",
		SchemaRef:      schemaComp,
		Payload:        []byte("125000"),
		OccurredAt:     occurredAt,
		EffectiveAt:    effectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}

	var recorded explorer.RecordCorrectionView
	if err := f.inTxErr(func(tx dbport.Tx) error {
		var err error
		recorded, err = explorer.RecordCorrection(ctx, tx, f.appender, f.tenant, correctionReq, nil)
		return err
	}); err != nil {
		t.Fatalf("record correction: %v", err)
	}
	if recorded.Sequence != origin.Sequence+1 {
		t.Fatalf("correction sequence = %d, want %d", recorded.Sequence, origin.Sequence+1)
	}
	if recorded.Ref != (explorer.EventRef{StreamKey: streamA, Sequence: recorded.Sequence}) {
		t.Fatalf("correction ref = %+v, want stream %s sequence %d", recorded.Ref, streamA, recorded.Sequence)
	}
	if recorded.EventID == uuid.Nil {
		t.Fatal("correction event id is nil")
	}
	if recorded.Digest == "" {
		t.Fatal("correction digest is empty")
	}
	if got := countLedgerEvents(t, f, f.tenant); got != 2 {
		t.Fatalf("ledger_event rows = %d, want 2 (origin plus correction)", got)
	}

	current, err := explorer.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		explorer.EventRef{StreamKey: streamA, Sequence: origin.Sequence}, nil)
	if err != nil {
		t.Fatalf("effective current: %v", err)
	}
	if current.Withheld {
		t.Fatal("unrestricted effective-current read reports Withheld")
	}
	if current.Current.Ref.Sequence != recorded.Sequence {
		t.Fatalf("effective current sequence = %d, want correction at %d",
			current.Current.Ref.Sequence, recorded.Sequence)
	}
	if current.Current.AssertionClass != ledger.Correction {
		t.Fatalf("effective current class = %s, want %s", current.Current.AssertionClass, ledger.Correction)
	}
	if len(current.Path) != 1 || current.Path[0].Ref.Sequence != recorded.Sequence {
		t.Fatalf("effective current path = %+v, want one hop to the correction", current.Path)
	}
	if current.Digest == "" {
		t.Fatal("effective current digest is empty")
	}

	direct, directPath, err := lineage.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		ledger.EventRef{StreamKey: streamA, Sequence: origin.Sequence})
	if err != nil {
		t.Fatalf("direct effective current: %v", err)
	}
	if !reflect.DeepEqual(direct, current.Current) {
		t.Fatalf("adapter current %+v != library current %+v", current.Current, direct)
	}
	if !reflect.DeepEqual(directPath, current.Path) {
		t.Fatalf("adapter path %+v != library path %+v", current.Path, directPath)
	}

	again, err := explorer.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		explorer.EventRef{StreamKey: streamA, Sequence: origin.Sequence}, nil)
	if err != nil {
		t.Fatalf("effective current again: %v", err)
	}
	if again.Digest != current.Digest {
		t.Fatal("repeated effective-current digest changed without any write")
	}

	self, err := explorer.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		explorer.EventRef{StreamKey: streamA, Sequence: recorded.Sequence}, nil)
	if err != nil {
		t.Fatalf("effective current of the correction itself: %v", err)
	}
	if self.Current.Ref.Sequence != recorded.Sequence || len(self.Path) != 0 {
		t.Fatalf("correction is not its own effective current: %+v path %+v", self.Current, self.Path)
	}

	// The governed write keeps the well-formed pattern: the correction's
	// hash-chain link is recorded in the same transaction, so the stream
	// still verifies after a correction lands.
	chain, err := explorer.VerifyChain(ctx, f.db.Conn, f.digester, f.tenant, streamA)
	if err != nil {
		t.Fatalf("verify chain after correction: %v", err)
	}
	if !chain.Verified || chain.Head.Sequence != recorded.Sequence {
		t.Fatalf("chain after correction = %+v, want verified at head %d", chain, recorded.Sequence)
	}

	// A decision whose subject is not disclosable refuses the write: an
	// operator who may not see the subject may not author corrections
	// about it either.
	denied := &authz.Decision{SubjectDisclosable: false}
	deniedReq := correctionReq
	deniedReq.ExpectedHead = recorded.Sequence
	deniedReq.Corrects = explorer.EventRef{StreamKey: streamA, Sequence: recorded.Sequence}
	deniedReq.IdempotencyKey = uuid.NewString()
	denyErr := f.inTxErr(func(tx dbport.Tx) error {
		_, err := explorer.RecordCorrection(ctx, tx, f.appender, f.tenant, deniedReq, denied)
		return err
	})
	var forbidden explorer.ErrCorrectionForbidden
	if !errors.As(denyErr, &forbidden) {
		t.Fatalf("denied correction error = %v, want ErrCorrectionForbidden", denyErr)
	}
	if got := countLedgerEvents(t, f, f.tenant); got != 2 {
		t.Fatalf("ledger_event rows after denied write = %d, want 2 (zero-effect)", got)
	}

	// The same denial withholds the read rather than returning an empty
	// walk a caller could mistake for "no lineage".
	withheld, err := explorer.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		explorer.EventRef{StreamKey: streamA, Sequence: origin.Sequence}, denied)
	if err != nil {
		t.Fatalf("withheld effective current: %v", err)
	}
	if !withheld.Withheld || withheld.Path != nil {
		t.Fatalf("denied read = %+v, want Withheld with nil path", withheld)
	}

	// A correction naming a target that was never recorded is refused
	// before anything is written.
	danglingReq := correctionReq
	danglingReq.ExpectedHead = recorded.Sequence
	danglingReq.Corrects = explorer.EventRef{StreamKey: streamA, Sequence: 999}
	danglingReq.IdempotencyKey = uuid.NewString()
	danglingErr := f.inTxErr(func(tx dbport.Tx) error {
		_, err := explorer.RecordCorrection(ctx, tx, f.appender, f.tenant, danglingReq, nil)
		return err
	})
	var notFound lineage.ErrCorrectionTargetNotFound
	if !errors.As(danglingErr, &notFound) {
		t.Fatalf("dangling correction error = %v, want ErrCorrectionTargetNotFound", danglingErr)
	}
	if got := countLedgerEvents(t, f, f.tenant); got != 2 {
		t.Fatalf("ledger_event rows after dangling write = %d, want 2 (zero-effect)", got)
	}

	// A target recorded for a different tenant is indistinguishable from a
	// target that was never recorded at all.
	other := uuid.New()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'other', 'cell-local', 'Other', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		other)
	f.inTx(t, func(tx dbport.Tx) error {
		return ledger.EnsureStream(ctx, tx, other, streamA, "WORKER", streamA)
	})
	crossReq := correctionReq
	crossReq.ExpectedHead = 0
	crossReq.IdempotencyKey = uuid.NewString()
	crossErr := f.inTxErr(func(tx dbport.Tx) error {
		_, err := explorer.RecordCorrection(ctx, tx, f.appender, other, crossReq, nil)
		return err
	})
	if !errors.As(crossErr, &notFound) {
		t.Fatalf("cross-tenant correction error = %v, want ErrCorrectionTargetNotFound", crossErr)
	}
	if got := countLedgerEvents(t, f, other); got != 0 {
		t.Fatalf("other tenant ledger_event rows = %d, want 0 (zero-effect)", got)
	}

	// The correction payload is deterministic evidence: re-resolving from
	// the origin after every refusal above still lands on the correction.
	final, err := explorer.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		explorer.EventRef{StreamKey: streamA, Sequence: origin.Sequence}, nil)
	if err != nil {
		t.Fatalf("final effective current: %v", err)
	}
	if final.Digest != current.Digest {
		t.Fatal("effective-current digest changed across refused writes")
	}
}

func countLedgerEvents(t *testing.T, f *fixture, tenant uuid.UUID) int64 {
	t.Helper()
	var n int64
	row := f.db.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count ledger_event: %v", err)
	}
	return n
}
