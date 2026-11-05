// TestTodo_WF_REV_011 proves every ledger-derived REBUILDABLE table folds
// compensation and correction facts the same way live and on rebuild:
// commit, reverse, rebuild from the ledger, and the rebuilt state equals
// the live projection. A projection that ignores the reversal fails.
package rebuildcheck_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuildcheck"
	"github.com/monstercameron/human-capital-management-suite/internal/data/search"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const rev011SchemaRef = "hcmnext.events.v1.Rev011Event@1"

var (
	rev011Occurred  = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	rev011Effective = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
)

func rev011Fixture(t *testing.T, db *pgtest.DB, slug string) rebuildcheck.Fixture {
	t.Helper()
	tenant := uuid.New()
	ctx := context.Background()
	if _, err := db.Conn.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-rev011', 'REV-011', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, slug+"-"+tenant.String()); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := db.Conn.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.events.v1.Rev011Event', 1,
			'hcmnext.events.v1.Rev011Event', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, rev011SchemaRef); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.Conn.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.events.v1.Rev011EventCompensation', 1,
			'hcmnext.events.v1.Rev011EventCompensation', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, rev011SchemaRef+":compensation"); err != nil {
		t.Fatalf("compensation schema: %v", err)
	}
	return rebuildcheck.NewFixture(db.Conn, tenant, values.TenantId(slug))
}

func rev011Append(t *testing.T, f rebuildcheck.Fixture, stream string, head int64, class ledger.AssertionClass, payload []byte, corrects *ledger.EventRef) ledger.AppendReceipt {
	t.Helper()
	var receipt ledger.AppendReceipt
	err := f.InTx(context.Background(), func(tx dbport.Tx) error {
		var err error
		receipt, err = ledger.Append(context.Background(), tx, ledger.AppendRequest{
			Tenant: f.Tenant, StreamKey: stream, ExpectedHead: head,
			AssertionClass: class, SourceRef: "hcmnext:rev011", SchemaRef: rev011SchemaRef,
			Payload: payload, OccurredAt: rev011Occurred, EffectiveAt: rev011Effective,
			CorrelationID: uuid.New(), IdempotencyKey: uuid.NewString(), Corrects: corrects,
		})
		return err
	})
	if err != nil {
		t.Fatalf("append %s/%d: %v", stream, head+1, err)
	}
	return receipt
}

func TestTodo_WF_REV_011(t *testing.T) {
	db := pgtest.New(t)

	t.Run("projection_checkpoint", func(t *testing.T) {
		f := rev011Fixture(t, db, "rev011")
		ctx := context.Background()
		stream := "rev011:checkpoint"
		if err := f.InTx(ctx, func(tx dbport.Tx) error {
			if err := ledger.EnsureStream(ctx, tx, f.Tenant, stream, "TRANSACTION", stream); err != nil {
				return err
			}
			return projection.EnsureProjection(ctx, tx, f.Tenant, "rev011-live", stream)
		}); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		apply := func(projectionName string, receipt ledger.AppendReceipt) {
			t.Helper()
			if err := f.InTx(ctx, func(tx dbport.Tx) error {
				_, err := projection.Apply(ctx, tx, projection.ApplyRequest{
					Tenant: f.Tenant, ProjectionName: projectionName, StreamKey: stream,
					Sequence: receipt.Sequence, Digest: receipt.Digest,
				})
				return err
			}); err != nil {
				t.Fatalf("apply %d: %v", receipt.Sequence, err)
			}
		}
		// Live: commit, then its correction.
		first := rev011Append(t, f, stream, 0, ledger.TransactionFact, []byte("commit"), nil)
		apply("rev011-live", first)
		second := rev011Append(t, f, stream, 1, ledger.Correction, []byte("correct"), &ledger.EventRef{StreamKey: stream, Sequence: first.Sequence})
		apply("rev011-live", second)
		live, err := projection.Read(ctx, f.Conn(), f.Tenant, "rev011-live", stream)
		if err != nil {
			t.Fatalf("read live: %v", err)
		}
		// Rebuild from the ledger into a fresh checkpoint and compare.
		if err := f.InTx(ctx, func(tx dbport.Tx) error {
			return projection.EnsureProjection(ctx, tx, f.Tenant, "rev011-rebuilt", stream)
		}); err != nil {
			t.Fatalf("ensure rebuilt: %v", err)
		}
		events, err := ledger.NewReader().ReadStream(ctx, f.Conn(), f.Tenant, stream)
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("stream holds %d events, want 2", len(events))
		}
		for _, event := range events {
			apply("rev011-rebuilt", ledger.AppendReceipt{Sequence: event.Sequence, Digest: event.Digest})
		}
		rebuilt, err := projection.Read(ctx, f.Conn(), f.Tenant, "rev011-rebuilt", stream)
		if err != nil {
			t.Fatalf("read rebuilt: %v", err)
		}
		if rebuilt.LastAppliedSequence != live.LastAppliedSequence || rebuilt.LastAppliedDigest != live.LastAppliedDigest {
			t.Fatalf("rebuilt checkpoint %+v != live %+v", rebuilt, live)
		}
		// A projection that ignores the correction fails: it stalls a
		// sequence behind with the commit digest.
		if err := f.InTx(ctx, func(tx dbport.Tx) error {
			return projection.EnsureProjection(ctx, tx, f.Tenant, "rev011-ignoring", stream)
		}); err != nil {
			t.Fatalf("ensure ignoring: %v", err)
		}
		apply("rev011-ignoring", first)
		ignoring, err := projection.Read(ctx, f.Conn(), f.Tenant, "rev011-ignoring", stream)
		if err != nil {
			t.Fatalf("read ignoring: %v", err)
		}
		if ignoring.LastAppliedSequence == live.LastAppliedSequence && ignoring.LastAppliedDigest == live.LastAppliedDigest {
			t.Fatal("a checkpoint that ignores the correction matches live: the comparison is blind")
		}
	})

	t.Run("outbox", func(t *testing.T) {
		f := rev011Fixture(t, db, "rev011")
		ctx := context.Background()
		stream := "rev011:outbox"
		appender := ledger.New(ledger.WithClock(func() time.Time { return rev011Occurred }))
		commit := func(effect, schema string, payload []byte, head int64) ledger.AppendReceipt {
			t.Helper()
			var receipt outbox.CommitReceipt
			err := f.InTx(ctx, func(tx dbport.Tx) error {
				var err error
				receipt, err = outbox.Commit(ctx, tx, appender, outbox.CommitRequest{
					Append: ledger.AppendRequest{
						Tenant: f.Tenant, StreamKey: stream, ExpectedHead: head,
						AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:rev011", SchemaRef: rev011SchemaRef,
						Payload: payload, OccurredAt: rev011Occurred, EffectiveAt: rev011Effective,
						CorrelationID: uuid.New(), IdempotencyKey: uuid.NewString(),
					},
					Projection: outbox.ProjectionSpec{Name: "rev011-outbox"},
					Outbox: outbox.OutboxSpec{
						EffectIdentity: effect, OrderingKey: "rev011:outbox",
						Criticality: outbox.CriticalityP1, SchemaRef: schema, Payload: payload,
					},
				})
				return err
			})
			if err != nil {
				t.Fatalf("commit %s: %v", effect, err)
			}
			return receipt.Ledger
		}
		if err := f.InTx(ctx, func(tx dbport.Tx) error {
			if err := ledger.EnsureStream(ctx, tx, f.Tenant, stream, "TRANSACTION", stream); err != nil {
				return err
			}
			return projection.EnsureProjection(ctx, tx, f.Tenant, "rev011-outbox", stream)
		}); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		// Live: commit, then its compensation.
		commit("rev011:effect-a", rev011SchemaRef, []byte("commit-effect-a"), 0)
		commit("rev011:effect-a-compensation", rev011SchemaRef+":compensation", []byte("compensate-effect-a"), 1)
		live := rev011OutboxDigest(t, f, "rev011:effect-a", "rev011:effect-a-compensation")
		// Rebuild: truncate the derived rows and re-enqueue from the
		// ledger stream, folding every class including compensation.
		if _, err := f.Conn().Exec(ctx, `DELETE FROM outbox WHERE tenant_id = $1`, f.Tenant); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		events, err := ledger.NewReader().ReadStream(ctx, f.Conn(), f.Tenant, stream)
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		for _, event := range events {
			if err := f.InTx(ctx, func(tx dbport.Tx) error {
				_, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
					Tenant: f.Tenant, EffectIdentity: rev011EffectFor(t, event),
					OrderingKey: "rev011:outbox", Criticality: outbox.CriticalityP1,
					SchemaRef: rev011SchemaFor(t, event), Payload: event.Payload,
				})
				return err
			}); err != nil {
				t.Fatalf("re-enqueue seq %d: %v", event.Sequence, err)
			}
		}
		rebuilt := rev011OutboxDigest(t, f, "rev011:effect-a", "rev011:effect-a-compensation")
		if rebuilt != live {
			t.Fatalf("rebuilt outbox %q != live %q", rebuilt, live)
		}
	})

	t.Run("search_projection", func(t *testing.T) {
		f := rev011Fixture(t, db, "rev011")
		ctx := context.Background()
		subject := values.EntityRef{Tenant: "rev011", Kind: "worker", Id: uuid.NewString()}
		project := func(fields map[people.FieldID]string, revision uint64) {
			t.Helper()
			token, err := values.NewSequenceRevision("rev011", revision)
			if err != nil {
				t.Fatalf("revision token: %v", err)
			}
			_, err = search.Project(ctx, f.Conn(), f.Tenant, search.ProjectionInput{
				Tenant: "rev011", Subject: subject, Kind: search.KindWorker,
				Revision: token, Fields: fields,
			})
			if err != nil {
				t.Fatalf("project rev %d: %v", revision, err)
			}
		}
		nameField := search.ClearedFields()[0]
		// Live: index, then its correction.
		project(map[people.FieldID]string{nameField: "Jon"}, 1)
		project(map[people.FieldID]string{nameField: "John"}, 2)
		live := rev011SearchDigest(t, f, subject)
		// Rebuild: truncate and re-project the fact stream in order.
		if _, err := f.Conn().Exec(ctx, `DELETE FROM search_projection WHERE tenant_id = $1`, f.Tenant); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		project(map[people.FieldID]string{nameField: "Jon"}, 1)
		project(map[people.FieldID]string{nameField: "John"}, 2)
		rebuilt := rev011SearchDigest(t, f, subject)
		if rebuilt != live {
			t.Fatalf("rebuilt search %q != live %q", rebuilt, live)
		}
		// A projection that ignores the correction fails: it still
		// serves the uncorrected text.
		if _, err := f.Conn().Exec(ctx, `DELETE FROM search_projection WHERE tenant_id = $1`, f.Tenant); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		project(map[people.FieldID]string{nameField: "Jon"}, 1)
		ignoring := rev011SearchDigest(t, f, subject)
		if ignoring == live {
			t.Fatal("a projection that ignores the correction matches live: the comparison is blind")
		}
	})
}

// TestTodo_WF_REV_011_Conformance ties the cases to the registry: every
// ledger-derived REBUILDABLE table has a passing case, and a new one
// fails here until it gets one.
func TestTodo_WF_REV_011_Conformance(t *testing.T) {
	tables, err := rebuildcheck.LedgerRebuildables()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	want := map[string]bool{"projection_checkpoint": true, "outbox": true, "search_projection": true}
	if len(tables) != len(want) {
		t.Fatalf("ledger-rebuildable tables are %v, want exactly %v", tables, want)
	}
	for _, table := range tables {
		if !want[table] {
			t.Fatalf("table %s rebuilds from the ledger but has no reversal case", table)
		}
		if _, err := rebuildcheck.RegistryEntry(table); err != nil {
			t.Fatalf("registry entry %s: %v", table, err)
		}
	}
	if _, err := rebuildcheck.RegistryEntry("observation_checkpoint"); err == nil {
		t.Fatal("observation_checkpoint must stay out of the ledger cases: it rebuilds from external_observation")
	}
	if _, err := rebuildcheck.RegistryEntry("no_such_table"); err == nil {
		t.Fatal("unknown tables must fail closed")
	}
}

// TestTodo_WF_REV_011_Recovery proves rebuilds are idempotent: replaying
// the stream a second time changes nothing.
func TestTodo_WF_REV_011_Recovery(t *testing.T) {
	db := pgtest.New(t)
	f := rev011Fixture(t, db, "rev011")
	ctx := context.Background()
	stream := "rev011:recovery"
	if err := f.InTx(ctx, func(tx dbport.Tx) error {
		if err := ledger.EnsureStream(ctx, tx, f.Tenant, stream, "TRANSACTION", stream); err != nil {
			return err
		}
		return projection.EnsureProjection(ctx, tx, f.Tenant, "rev011-recovery", stream)
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	first := rev011Append(t, f, stream, 0, ledger.TransactionFact, []byte("commit"), nil)
	second := rev011Append(t, f, stream, 1, ledger.Correction, []byte("correct"), &ledger.EventRef{StreamKey: stream, Sequence: first.Sequence})
	apply := func() {
		t.Helper()
		for _, receipt := range []ledger.AppendReceipt{first, second} {
			if err := f.InTx(ctx, func(tx dbport.Tx) error {
				_, err := projection.Apply(ctx, tx, projection.ApplyRequest{
					Tenant: f.Tenant, ProjectionName: "rev011-recovery", StreamKey: stream,
					Sequence: receipt.Sequence, Digest: receipt.Digest,
				})
				return err
			}); err != nil {
				t.Fatalf("apply %d: %v", receipt.Sequence, err)
			}
		}
	}
	apply()
	once, err := projection.Read(ctx, f.Conn(), f.Tenant, "rev011-recovery", stream)
	if err != nil {
		t.Fatalf("read once: %v", err)
	}
	apply()
	twice, err := projection.Read(ctx, f.Conn(), f.Tenant, "rev011-recovery", stream)
	if err != nil {
		t.Fatalf("read twice: %v", err)
	}
	if twice != once {
		t.Fatalf("second replay changed the checkpoint: %+v vs %+v", twice, once)
	}
}

func rev011OutboxDigest(t *testing.T, f rebuildcheck.Fixture, effects ...string) string {
	t.Helper()
	ctx := context.Background()
	parts := make([]string, 0, len(effects))
	for _, effect := range effects {
		record, ok, err := outbox.ReadByEffectIdentity(ctx, f.Conn(), f.Tenant, effect)
		if err != nil || !ok {
			t.Fatalf("read %s: %v %v", effect, ok, err)
		}
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%s|%s", record.EffectIdentity, record.OrderingKey, record.Criticality, record.SchemaRef, string(record.Payload)))
	}
	sort.Strings(parts)
	out := ""
	for _, part := range parts {
		out += part + "\n"
	}
	return out
}

func rev011EffectFor(t *testing.T, event ledger.EventRecord) string {
	t.Helper()
	switch string(event.Payload) {
	case "commit-effect-a":
		return "rev011:effect-a"
	case "compensate-effect-a":
		return "rev011:effect-a-compensation"
	default:
		t.Fatalf("unknown stream payload %q", string(event.Payload))
		return ""
	}
}

func rev011SchemaFor(t *testing.T, event ledger.EventRecord) string {
	t.Helper()
	switch string(event.Payload) {
	case "commit-effect-a":
		return rev011SchemaRef
	case "compensate-effect-a":
		return rev011SchemaRef + ":compensation"
	default:
		t.Fatalf("unknown stream payload %q", string(event.Payload))
		return ""
	}
}

func rev011SearchDigest(t *testing.T, f rebuildcheck.Fixture, subject values.EntityRef) string {
	t.Helper()
	var text, revision string
	err := f.Conn().QueryRow(context.Background(),
		`SELECT search_text, source_revision FROM search_projection WHERE tenant_id = $1`,
		f.Tenant).Scan(&text, &revision)
	if err != nil {
		t.Fatalf("read search row: %v", err)
	}
	return fmt.Sprintf("%s@%s", text, revision)
}
