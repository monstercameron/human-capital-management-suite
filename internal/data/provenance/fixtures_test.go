package provenance_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const eventSchemaRef = "hcmnext.intents.v1.BusinessIntent@1"

var occurredAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

// fixture is a migrated schema, plus this package's own provenance_record
// side table (schema.go's SchemaDDL), holding one tenant, one registered
// event payload schema, this package's own outbox payload schema, and one
// empty ledger stream.
type fixture struct {
	db        *pgtest.DB
	tenant    uuid.UUID
	reader    *ledger.Reader
	streamKey string
	intentRef string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	db.Exec(t, provenance.SchemaDDL)
	tenant := uuid.New()
	streamKey := "intent:" + uuid.NewString()
	intentRef := uuid.NewString()

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())

	db.Exec(t, `
		INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1, 'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, eventSchemaRef)
	db.Exec(t, `
		INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.provenance.v1.Record', 1, 'hcmnext.provenance.v1.Record', 'PROTOBUF', 'EVIDENCE_MANIFEST')`,
		tenant, provenance.OutboxSchemaRef)

	f := fixture{db: db, tenant: tenant, reader: ledger.NewReader(), streamKey: streamKey, intentRef: intentRef}
	f.inTx(t, db.Conn, func(tx dbport.Tx) error {
		return ledger.EnsureStream(context.Background(), tx, tenant, streamKey, "TRANSACTION", intentRef)
	})
	return f
}

func (f fixture) inTx(t *testing.T, conn *pgxadapter.Conn, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(conn *pgxadapter.Conn, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// appendEvent appends one ledger event on f.streamKey and returns its receipt.
func (f fixture) appendEvent(t *testing.T, expectedHead int64) ledger.AppendReceipt {
	t.Helper()
	var receipt ledger.AppendReceipt
	f.inTx(t, f.db.Conn, func(tx dbport.Tx) error {
		var err error
		receipt, err = ledger.Append(context.Background(), tx, ledger.AppendRequest{
			Tenant:         f.tenant,
			StreamKey:      f.streamKey,
			ExpectedHead:   expectedHead,
			AssertionClass: ledger.TransactionFact,
			SourceRef:      "hcmnext:intent",
			SchemaRef:      eventSchemaRef,
			Payload:        fmt.Appendf(nil, "event-%d", expectedHead+1),
			OccurredAt:     occurredAt,
			EffectiveAt:    occurredAt,
			CorrelationID:  uuid.New(),
			IdempotencyKey: uuid.NewString(),
		})
		return err
	})
	return receipt
}

// publishRequest returns a well-formed PublishRequest for a ledger event.
func (f fixture) publishRequest(receipt ledger.AppendReceipt) provenance.PublishRequest {
	return provenance.PublishRequest{
		Tenant:          f.tenant,
		IntentRef:       f.intentRef,
		SourceKind:      provenance.SourceLedgerEvent,
		SourceRef:       provenance.LedgerEventSourceRef(f.streamKey, receipt.Sequence),
		StreamKey:       f.streamKey,
		Sequence:        receipt.Sequence,
		EventID:         receipt.EventID,
		SourceAuthority: "hcmnext:intent",
		PrincipalRef:    "principal:worker-1",
		EvidenceIDs:     []string{"evidence:" + receipt.EventID.String()},
		Digests: []provenance.Digest{
			{Kind: "LEDGER_EVENT", Algorithm: receipt.DigestAlgorithm, Digest: receipt.Digest},
		},
		PublishedAt: occurredAt,
	}
}

func (f fixture) publish(t *testing.T, req provenance.PublishRequest) provenance.Record {
	t.Helper()
	var rec provenance.Record
	f.inTx(t, f.db.Conn, func(tx dbport.Tx) error {
		var err error
		rec, err = provenance.Publish(context.Background(), tx, req)
		return err
	})
	return rec
}

func (f fixture) publishErr(t *testing.T, conn *pgxadapter.Conn, req provenance.PublishRequest) (provenance.Record, error) {
	t.Helper()
	var rec provenance.Record
	err := f.inTxErr(conn, func(tx dbport.Tx) error {
		var pubErr error
		rec, pubErr = provenance.Publish(context.Background(), tx, req)
		return pubErr
	})
	return rec, err
}
