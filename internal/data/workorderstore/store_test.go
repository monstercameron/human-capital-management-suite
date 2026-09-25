package workorderstore

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, db.Schema
}

func initialSnapshot(t *testing.T, tenant, id, key, actor string) workorder.Snapshot {
	t.Helper()
	order, err := workorder.NewWorkOrder(workorder.CreateInput{ID: id, TenantID: tenant, ProjectID: "project", TemplateID: "service", TemplateVersion: "1.0.0", TemplateDigest: "sha256:template", ActorID: actor, IdempotencyKey: key, CommandDigest: "digest-" + key, Now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return order.Snapshot()
}

func TestConfigurationAndInputBounds(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("empty DSN accepted")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@localhost/db", Schema: "invalid-name"}); err == nil {
		t.Fatal("invalid schema accepted")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@localhost/db", CoreDSN: "postgres://u:p@localhost/db", Schema: SchemaName}); !errors.Is(err, ErrCoreCredential) {
		t.Fatalf("shared core credential error=%v", err)
	}
	if _, err := withPoolSize("postgres://u:p@localhost/db", 2, 3); err == nil {
		t.Fatal("invalid pool bounds accepted")
	}
	if got, err := withPoolSize("postgres://u:p@localhost/db?pool_max_conns=4", 8, 2); err != nil || !strings.Contains(got, "pool_min_conns=2") {
		t.Fatalf("pool size DSN %q err=%v", got, err)
	}
	if !validSchema("hcmnext_workorder") || validSchema("bad-name") {
		t.Fatal("schema identifier validation failed")
	}
	s, _ := fixture(t)
	if _, _, err := s.List(context.Background(), "t", "p", "", "", 201); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized list limit error=%v", err)
	}
	if err := s.PublishTemplate(context.Background(), "", "x", "1.0.0", "digest", "author", json.RawMessage(`{}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid template error=%v", err)
	}
	if _, err := s.Execute(context.Background(), "", "", "", "", 0, "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid execute error=%v", err)
	}
}

func TestConcurrentCreateReplayIsIdempotent(t *testing.T) {
	s, _ := fixture(t)
	snapshot := initialSnapshot(t, "tenant", "concurrent", "create-concurrent", "initiator")
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- s.Create(context.Background(), "tenant", snapshot, "initiator", "create-concurrent")
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent create replay failed: %v", err)
		}
	}
}

func TestRecordIdempotencyIsScopedToActor(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	snapshot := initialSnapshot(t, "tenant", "record-actors", "create-record-actors", "creator")
	if err := s.Create(ctx, "tenant", snapshot, "creator", "create-record-actors"); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		for i, actor := range []string{"actor-a", "actor-b"} {
			_, err := tx.Exec(ctx, `INSERT INTO work_order_record(tenant_id,id,work_order_id,sequence,revision,kind,actor_id,idempotency_key,payload) VALUES('tenant',$1,'record-actors',$2,1,'NOTE',$3,'same-key','{}'::jsonb)`, uuid.NewString(), int64(10+i), actor)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("different actors could not reuse a client key: %v", err)
	}
}

func TestTenantRevisionIdempotencyAndImmutableHistory(t *testing.T) {
	s, schema := fixture(t)
	ctx := context.Background()
	for _, tenant := range []string{"a", "b"} {
		snapshot := initialSnapshot(t, tenant, "same-id", "create-"+tenant, "initiator")
		if err := s.Create(ctx, tenant, snapshot, "initiator", "create-"+tenant); err != nil {
			t.Fatal(err)
		}
		if err := s.Create(ctx, tenant, snapshot, "initiator", "create-"+tenant); err != nil {
			t.Fatalf("create replay: %v", err)
		}
		snapshot.Journal[0].CommandDigest = "changed"
		if err := s.Create(ctx, tenant, snapshot, "initiator", "create-"+tenant); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("changed create replay error=%v", err)
		}
	}
	command := json.RawMessage(`{"amount":"25.00","currency":"USD"}`)
	digest := workorder.DigestCommandJSON(command)
	mutate := func(current workorder.Snapshot) (workorder.Snapshot, error) {
		current.Revision++
		now := current.UpdatedAt.Add(time.Minute)
		current.UpdatedAt = now
		current.Journal = append(current.Journal, workorder.Event{Revision: current.Revision, Type: "REQUEST_SUBMITTED", ActorID: "actor", IdempotencyKey: "request-key", CommandDigest: digest, Detail: "BUDGET", At: now})
		return current, nil
	}
	first, err := s.Execute(ctx, "a", "same-id", "actor", "request-key", 1, digest, mutate)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 2 || len(first.Journal) != 2 {
		t.Fatalf("revision=%d journal=%d", first.Revision, len(first.Journal))
	}
	if first.Journal[1].CommandDigest != workorder.DigestCommandJSON(command) {
		t.Fatalf("event digest=%q", first.Journal[1].CommandDigest)
	}
	replayed, err := s.Execute(ctx, "a", "same-id", "actor", "request-key", 1, digest, func(workorder.Snapshot) (workorder.Snapshot, error) {
		t.Fatal("mutation ran on replay")
		return workorder.Snapshot{}, nil
	})
	if err != nil || replayed.Revision != 2 {
		t.Fatalf("replay %#v, %v", replayed, err)
	}
	if _, err = s.Execute(ctx, "a", "same-id", "actor", "request-key", 1, workorder.DigestCommandJSON([]byte(`{"amount":"26.00","currency":"USD"}`)), mutate); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed retry error=%v", err)
	}
	if _, err = s.Execute(ctx, "a", "same-id", "actor", "stale-key", 1, "digest-stale", mutate); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale execute error=%v", err)
	}
	if _, err = s.Get(ctx, "a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing order error=%v", err)
	}
	if got, err := s.Get(ctx, "b", "same-id"); err != nil || got.Revision != 1 {
		t.Fatalf("tenant b read %#v, %v", got, err)
	}
	history, err := s.History(ctx, "a", "same-id", 0, 10)
	if err != nil || len(history) != 2 || history[1].Revision != 2 {
		t.Fatalf("history %#v, %v", history, err)
	}
	if err := s.RunTenantTx(ctx, "a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE work_order_record SET payload='{}' WHERE tenant_id='a' AND work_order_id='same-id'")
		return err
	}); err == nil {
		t.Fatal("append-only history mutation succeeded")
	}
	var policyCount int
	if err := s.RunTenantTx(ctx, "a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname=$1 AND tablename='work_order' AND policyname='tenant_isolation'`, schema).Scan(&policyCount)
	}); err != nil || policyCount != 1 {
		t.Fatalf("work_order RLS policy count=%d err=%v", policyCount, err)
	}
}

func TestTemplateVersionsAreImmutable(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if err := s.PublishTemplate(ctx, "tenant", "service", "1.0.0", "sha256:one", "publisher", json.RawMessage(`{"phases":["DRAFT"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE work_order_template_version SET digest='changed' WHERE tenant_id='tenant'`)
		return err
	}); err == nil {
		t.Fatal("published template mutation succeeded")
	}
}

func TestTodo_WorkOrderStore_ArtifactsAndList(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	create := func(id, key string) {
		t.Helper()
		snapshot := initialSnapshot(t, "tenant", id, key, "initiator")
		if err := s.Create(ctx, "tenant", snapshot, "initiator", key); err != nil {
			t.Fatal(err)
		}
	}
	create("wo-a", "create-a")
	create("wo-b", "create-b")
	page, next, err := s.List(ctx, "tenant", "project", "", "", 1)
	if err != nil || len(page) != 1 || page[0].ID != "wo-a" || next != "wo-a" {
		t.Fatalf("first page %#v cursor=%q err=%v", page, next, err)
	}
	page, next, err = s.List(ctx, "tenant", "project", "DRAFT", next, 1)
	if err != nil || len(page) != 1 || page[0].ID != "wo-b" || next != "" {
		t.Fatalf("second page %#v cursor=%q err=%v", page, next, err)
	}

	payload := []byte(`{"sourceCutoff":"rev-1","lines":[]}`)
	artifact, err := s.RecordArtifact(ctx, "tenant", "wo-a", "project", "finance", "report-1", 1, "REPORT", "digest-report", payload)
	if err != nil || artifact.ID == "" || artifact.SourceRevision != 1 {
		t.Fatalf("artifact %#v err=%v", artifact, err)
	}
	fetched, err := s.GetArtifact(ctx, "tenant", "wo-a", "finance", "report-1", "REPORT")
	var wantPayload, gotPayload map[string]any
	_ = json.Unmarshal(payload, &wantPayload)
	_ = json.Unmarshal(fetched.Payload, &gotPayload)
	if err != nil || fetched.ID != artifact.ID || !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf("GetArtifact %#v err=%v", fetched, err)
	}
	if _, err := s.GetArtifact(ctx, "tenant", "wo-a", "finance", "missing", "REPORT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing artifact err=%v", err)
	}
	if _, err := s.Execute(ctx, "tenant", "wo-a", "supervisor", "advance", 1, "digest-advance", func(current workorder.Snapshot) (workorder.Snapshot, error) {
		current.Revision++
		now := current.UpdatedAt.Add(time.Minute)
		current.UpdatedAt = now
		current.Journal = append(current.Journal, workorder.Event{Revision: current.Revision, Type: "PHASE_TRANSITION", ActorID: "supervisor", IdempotencyKey: "advance", CommandDigest: "digest-advance", FromPhase: workorder.PhaseDraft, ToPhase: workorder.PhaseAuthorization, At: now})
		current.Phase = workorder.PhaseAuthorization
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	if replay, err := s.RecordArtifact(ctx, "tenant", "wo-a", "project", "finance", "report-1", 1, "REPORT", "digest-report", payload); err != nil || replay.ID != artifact.ID {
		t.Fatalf("artifact replay %#v err=%v", replay, err)
	}
	if _, err := s.RecordArtifact(ctx, "tenant", "wo-a", "project", "finance", "report-1", 1, "REPORT", "different", payload); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("artifact changed replay error=%v", err)
	}
	if _, err := s.RecordArtifact(ctx, "tenant", "wo-a", "project", "finance", "report-2", 1, "BILLING", "digest-billing", payload); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("artifact stale source error=%v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM work_order_artifact WHERE tenant_id='tenant' AND work_order_id='wo-a'`)
		return err
	}); err == nil {
		t.Fatal("artifact mutation succeeded")
	}
}
