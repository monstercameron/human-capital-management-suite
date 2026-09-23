package chatrecordstore_test

import (
	"context"
	"io/fs"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatrecordstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newStore(t *testing.T) *chatrecordstore.Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	cs, err := chatstore.New(context.Background(), chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cs.Close)
	return chatrecordstore.New(cs)
}

func TestTodo_CHAT_049_Recovery(t *testing.T) {
	s := newStore(t)
	dst := newStore(t)
	ctx := context.Background()
	at := time.Unix(10, 0).UTC()
	r := chatrecords.Record{TenantID: "tenant-a", ConversationID: "c", RecordID: "p", Kind: chatrecords.KindPost, Revision: 1, CreatedAt: at}
	e := chatrecords.AuditEvent{TenantID: "tenant-a", EventID: "e1", Sequence: 1, ActorID: "a", Action: "post", TargetType: "chat", TargetID: "p", Reason: "test", PolicyEvidence: "p:v1", At: at, Digest: "d"}
	if err := s.Append(ctx, r, e, chatrecords.OutboxEvent{ID: "o1", EventID: "e1", Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.RawTables) != 31 || snap.Digest == "" {
		t.Fatalf("incomplete snapshot: %+v", snap)
	}
	if err := dst.Restore(ctx, snap); err != nil {
		t.Fatal(err)
	}
	rows, err := dst.List(ctx, "tenant-a")
	if err != nil || len(rows) != 1 {
		t.Fatalf("restored rows=%v err=%v", rows, err)
	}
}

func TestTodo_CHAT_049_Fault(t *testing.T) {
	s := newStore(t)
	snap := chatrecords.Snapshot{TenantID: "tenant-a", Digest: "sha256:bad"}
	if err := s.Restore(context.Background(), snap); err == nil {
		t.Fatal("corrupt snapshot restored")
	}
}

func TestTodo_CHAT_047_Integration_ReconcileMissingProjection(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,'conversation','post.created','{}'::jsonb)`, "tenant-a")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Reconcile(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Ready || r.Outbox != 1 || r.MissingOutbox != 1 {
		t.Fatalf("missing projection reported ready: %+v", r)
	}
}

func TestTodo_CHAT_047_Integration_ConcurrentAuditIdentity(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	at := time.Unix(20, 0).UTC()
	var wg sync.WaitGroup
	results := make(chan chatrecords.AuditEvent, 2)
	errs := make(chan error, 2)
	for _, id := range []string{"conversation:a", "conversation:b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r := chatrecords.Record{TenantID: "tenant-a", ConversationID: id, RecordID: id, Kind: chatrecords.KindConversation, Revision: 1, CreatedAt: at}
			e := chatrecords.AuditEvent{TenantID: r.TenantID, ActorID: "alice", Action: "chat.conversation.create", TargetType: "chat", TargetID: id, Reason: "create", PolicyEvidence: "chat:" + id + ":1", At: at}
			stored, err := s.AppendEvent(ctx, r, e, chatrecords.OutboxEvent{})
			if err != nil {
				errs <- err
				return
			}
			results <- stored
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(results)
	events, err := s.Events(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence == events[1].Sequence {
		t.Fatalf("stored events=%+v", events)
	}
	for returned := range results {
		matched := false
		for _, stored := range events {
			if stored.EventID == returned.EventID && stored.Sequence == returned.Sequence && stored.Digest == returned.Digest {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("returned event not persisted: %+v vs %+v", returned, events)
		}
	}
}

func TestTodo_CHAT_048_Integration_GovernanceRecords(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-a"
	at := time.Unix(30, 0).UTC()
	h := chatrecords.Hold{TenantID: tenant, HoldID: "hold-1", MatterRef: "case-1", Reason: "preserve", PlacedBy: "alice", PlacedAt: at}
	if err := s.PutHold(ctx, h); err != nil {
		t.Fatal(err)
	}
	holds, err := s.Holds(ctx, tenant)
	if err != nil || len(holds) != 1 || holds[0].MatterRef != h.MatterRef {
		t.Fatalf("holds=%+v err=%v", holds, err)
	}
	ex := chatrecords.Export{TenantID: tenant, ExportID: "export-1", RecordIDs: []string{"post-1"}, Digest: "digest", CreatedAt: at}
	if err := s.PutExport(ctx, ex); err != nil {
		t.Fatal(err)
	}
	report := chatrecords.Report{TenantID: tenant, ReportID: "report-1", ConversationID: "c1", TargetID: "post-1", ReporterID: "alice", Reason: "spam", CreatedAt: at, State: "OPEN"}
	if err := s.PutReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	reports, err := s.Reports(ctx, tenant)
	if err != nil || len(reports) != 1 || reports[0].TargetID != report.TargetID {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	if err := s.PutCaseAction(ctx, tenant, chatrecords.CaseAction{CaseID: "case-1", Action: "review", ActorID: "alice", Reason: "spam", EvidenceRef: "evidence-1", At: at}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, tenant)
	if err != nil || snap.Digest == "" || len(snap.RawTables["chat_record_hold"]) == 0 || len(snap.RawTables["chat_moderation_action"]) == 0 {
		t.Fatalf("snapshot=%+v err=%v", snap, err)
	}
	r, err := s.Reconcile(ctx, tenant)
	if err != nil || !r.Ready || r.MissingOutbox != 0 {
		t.Fatalf("reconcile=%+v err=%v", r, err)
	}
}

// TestTodo_CHAT_049_RestoreResetsSequence reproduces the CHAT-049 review
// finding: Restore inserted snapshot rows with their explicit bigserial ids
// but never advanced the underlying sequence, so the first ordinary insert
// on the destination store after a restore (which lets Postgres assign the
// id) collided with a restored row already holding that id. AppendEvent's
// plain `INSERT INTO chat_outbox(...)` (no explicit id) is exactly that
// first insert.
func TestTodo_CHAT_049_RestoreResetsSequence(t *testing.T) {
	s := newStore(t)
	dst := newStore(t)
	ctx := context.Background()
	at := time.Unix(40, 0).UTC()

	// Produce several chat_outbox rows (and chat_post_revision/
	// chat_moderation_action are exercised by other tests) on the source so
	// the destination restores ids that a fresh sequence would also hand out.
	for i, id := range []string{"p1", "p2", "p3"} {
		r := chatrecords.Record{TenantID: "tenant-a", ConversationID: "c", RecordID: id, Kind: chatrecords.KindPost, Revision: uint64(i + 1), CreatedAt: at}
		e := chatrecords.AuditEvent{TenantID: "tenant-a", ActorID: "a", Action: "post", TargetType: "chat", TargetID: id, Reason: "test", PolicyEvidence: "p:v1", At: at, Digest: "d"}
		if _, err := s.AppendEvent(ctx, r, e, chatrecords.OutboxEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := s.Snapshot(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.Restore(ctx, snap); err != nil {
		t.Fatal(err)
	}

	// The first ordinary write on dst after restore lets Postgres assign the
	// chat_outbox id from the sequence. Before the fix this collided with a
	// restored row's id (a bare INSERT with no ON CONFLICT clause) instead of
	// continuing after the highest restored id.
	r := chatrecords.Record{TenantID: "tenant-a", ConversationID: "c", RecordID: "p4", Kind: chatrecords.KindPost, Revision: 1, CreatedAt: at}
	e := chatrecords.AuditEvent{TenantID: "tenant-a", ActorID: "a", Action: "post", TargetType: "chat", TargetID: "p4", Reason: "test", PolicyEvidence: "p:v1", At: at, Digest: "d2"}
	if _, err := dst.AppendEvent(ctx, r, e, chatrecords.OutboxEvent{}); err != nil {
		t.Fatalf("post-restore insert collided with a restored id: %v", err)
	}
	var maxID, count int64
	if err := dst.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT max(id),count(*) FROM chat_outbox WHERE tenant_id='tenant-a'`).Scan(&maxID, &count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 4 { // 3 restored + 1 new
		t.Fatalf("chat_outbox row count after restore+insert = %d, want 4", count)
	}
	if maxID < count {
		t.Fatalf("chat_outbox ids not distinct after restore: max id=%d count=%d", maxID, count)
	}
}

// TestTodo_CHAT_049_SnapshotRowCapExceeded confirms Snapshot fails loudly
// instead of silently truncating a table once it has more rows than the
// bounded-page cap the review required.
func TestTodo_CHAT_049_SnapshotRowCapExceeded(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_record_hold(tenant_id,hold_id,matter_ref,reason,placed_by,placed_at) SELECT 'tenant-a','hold-'||g,'m','r','p',now() FROM generate_series(1,5001) g`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(ctx, "tenant-a"); err == nil {
		t.Fatal("snapshot silently truncated an over-cap table instead of failing")
	}
}

func TestTodo_CHAT_048_PutHoldIdempotent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	h := chatrecords.Hold{TenantID: "tenant-a", HoldID: "hold-dup", MatterRef: "case-1", Reason: "preserve", PlacedBy: "alice", PlacedAt: time.Unix(50, 0).UTC()}
	if err := s.PutHold(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := s.PutHold(ctx, h); err != nil {
		t.Fatalf("retried hold placement should be idempotent: %v", err)
	}
	holds, err := s.Holds(ctx, "tenant-a")
	if err != nil || len(holds) != 1 {
		t.Fatalf("holds=%+v err=%v", holds, err)
	}
}

// TestTodo_CHAT_048_KeysetPagination exercises the *Page methods added for
// the CHAT-048 review finding: List/Events/Holds/Reports had no LIMIT or
// cursor at all.
func TestTodo_CHAT_048_KeysetPagination(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-a"
	at := time.Unix(60, 0).UTC()
	for _, id := range []string{"h1", "h2", "h3"} {
		if err := s.PutHold(ctx, chatrecords.Hold{TenantID: tenant, HoldID: id, MatterRef: "case", Reason: "preserve", PlacedBy: "alice", PlacedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := s.HoldsPage(ctx, tenant, "", 2)
	if err != nil || len(page1) != 2 || page1[0].HoldID != "h1" || page1[1].HoldID != "h2" {
		t.Fatalf("holds page1=%+v err=%v", page1, err)
	}
	page2, err := s.HoldsPage(ctx, tenant, page1[len(page1)-1].HoldID, 2)
	if err != nil || len(page2) != 1 || page2[0].HoldID != "h3" {
		t.Fatalf("holds page2=%+v err=%v", page2, err)
	}
	if all, err := s.HoldsPage(ctx, tenant, "", 0); err != nil || len(all) != 3 {
		t.Fatalf("holds zero-limit default=%+v err=%v", all, err)
	}
	if all, err := s.HoldsPage(ctx, tenant, "", -5); err != nil || len(all) != 3 {
		t.Fatalf("holds negative-limit default=%+v err=%v", all, err)
	}

	for _, id := range []string{"r1", "r2"} {
		if err := s.PutReport(ctx, chatrecords.Report{TenantID: tenant, ReportID: id, ConversationID: "c", TargetID: "post", ReporterID: "alice", Reason: "spam", CreatedAt: at, State: "OPEN"}); err != nil {
			t.Fatal(err)
		}
	}
	reportsPage, err := s.ReportsPage(ctx, tenant, "", 1)
	if err != nil || len(reportsPage) != 1 || reportsPage[0].ReportID != "r1" {
		t.Fatalf("reports page=%+v err=%v", reportsPage, err)
	}

	for i, id := range []string{"list-1", "list-2"} {
		r := chatrecords.Record{TenantID: tenant, ConversationID: "c", RecordID: id, Kind: chatrecords.KindPost, Revision: uint64(i + 1), CreatedAt: at}
		e := chatrecords.AuditEvent{TenantID: tenant, ActorID: "a", Action: "post", TargetType: "chat", TargetID: id, Reason: "test", PolicyEvidence: "p:v1", At: at, Digest: "d"}
		if _, err := s.AppendEvent(ctx, r, e, chatrecords.OutboxEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	listPage, err := s.ListPage(ctx, tenant, "", 1)
	if err != nil || len(listPage) != 1 || listPage[0].RecordID != "list-1" {
		t.Fatalf("list page=%+v err=%v", listPage, err)
	}
	eventsPage, err := s.EventsPage(ctx, tenant, 0, 1)
	if err != nil || len(eventsPage) != 1 || eventsPage[0].Sequence != 1 {
		t.Fatalf("events page=%+v err=%v", eventsPage, err)
	}
	eventsPage2, err := s.EventsPage(ctx, tenant, eventsPage[0].Sequence, 10)
	if err != nil || len(eventsPage2) != 1 || eventsPage2[0].Sequence != 2 {
		t.Fatalf("events page2=%+v err=%v", eventsPage2, err)
	}
}
