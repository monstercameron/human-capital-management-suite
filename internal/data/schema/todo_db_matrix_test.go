package schema_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schema"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestTodo_DB_005_Fault(t *testing.T) {
	db := pgtest.New(t)
	id := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.Conn.Exec(ctx, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,'fault-write','cell-local','x','ACTIVE',now())`, id)
	if err == nil {
		t.Fatal("write with canceled request context succeeded")
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM tenant WHERE tenant_id=$1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled write left %d rows", count)
	}
}

func FuzzTodo_DB_005(f *testing.F) {
	f.Add(int32(1))
	f.Add(int32(0))
	f.Add(int32(-1))
	f.Fuzz(func(t *testing.T, delta int32) {
		db := pgtest.New(t)
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := from.Add(time.Duration(delta) * time.Second)
		var end any = to
		if delta == 0 {
			end = from
		}
		err := db.ExecErr(`INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from, effective_to) VALUES ($1,$2,'cell-local','x','ACTIVE',$3,$4)`, uuid.New(), "fuzz-"+uuid.NewString(), from, end)
		if delta > 0 && err != nil {
			t.Fatalf("positive interval was rejected: %v", err)
		}
		if delta <= 0 && err == nil {
			t.Fatalf("non-positive interval delta %d was accepted", delta)
		}
	})
}

func TestTodo_DB_005_Integration(t *testing.T) {
	db := pgtest.New(t)
	a, b := insertNamedTenant(t, db, "primitive-a"), insertNamedTenant(t, db, "primitive-b")
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, a.String()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM tenant WHERE tenant_id=$1`, b).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("tenant A saw tenant B row (count %d)", visible)
	}
}

func TestTodo_DB_005_Mutation(t *testing.T) {
	db := pgtest.New(t)
	id := uuid.New()
	if err := db.ExecErr(`INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from, cell_epoch) VALUES ($1,'mut-invalid','cell-local','x','ACTIVE',now(),0)`, id); err == nil {
		t.Fatal("zero epoch mutation was accepted")
	}
	if err := db.ExecErr(`INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from, effective_to) VALUES ($1,'mut-empty','cell-local','x','ACTIVE',now(),now())`, uuid.New()); err == nil {
		t.Fatal("empty effective interval mutation was accepted")
	}
}

func TestTodo_DB_005_Race(t *testing.T) {
	db := pgtest.New(t)
	const n = 8
	conns := make([]dbport.Conn, n)
	for i := range conns {
		conns[i] = db.NewConn(t)
	}
	var wg sync.WaitGroup
	var won atomic.Int32
	for i := 0; i < n; i++ {
		conn := conns[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := conn.Exec(context.Background(), `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,'race-shared-key','cell-local','x','ACTIVE',now())`, uuid.New()); err == nil {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := won.Load(); got != 1 {
		t.Fatalf("unique tenant key accepted %d concurrent writes, want exactly one", got)
	}
}

func TestTodo_DB_006_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	j := schema.NewJournal(db.SQL, "matrix-tool", "matrix-owner", schema.WithTrustedTimeSource("TEST_CLOCK"))
	release := testRelease(t)
	if err := j.EnsureRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	rid, err := j.ReleaseID(ctx, release.Version)
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrations.Files()
	if err != nil || len(files) == 0 {
		t.Fatalf("migration files: %v (%d)", err, len(files))
	}
	e, err := j.Begin(ctx, rid, files[0], schema.DirectionUp)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Fail(ctx, e, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if applied, err := j.Applied(ctx, rid, files[0]); err != nil || applied {
		t.Fatalf("failed migration applied=%v err=%v", applied, err)
	}
	retry, err := j.Begin(ctx, rid, files[0], schema.DirectionUp)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Succeed(ctx, retry); err != nil {
		t.Fatal(err)
	}
	if applied, err := j.Applied(ctx, rid, files[0]); err != nil || !applied {
		t.Fatalf("successful retry applied=%v err=%v", applied, err)
	}
}

func TestTodo_DB_006_Race(t *testing.T) {
	db := pgtest.New(t)
	release := testRelease(t)
	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- schema.NewJournal(db.SQL, "matrix-tool", "matrix-owner").EnsureRelease(context.Background(), release)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent idempotent registration: %v", err)
		}
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM schema_release WHERE release_version=$1`, release.Version).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("release rows=%d, want 1", count)
	}
}

func TestTodo_DB_007_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	for v := int64(1); v <= 3; v++ {
		var prev any
		if v > 1 {
			prev = v - 1
		}
		if err := db.ExecErr(`INSERT INTO definition_version (tenant_id,definition_kind,definition_key,version,definition_digest,source_ref,body,supersedes_version,published_by,published_at) VALUES ($1,'RULE','integration-lineage',$2,$3,'git://matrix','\x01',$4,'tester',now())`, tenant, v, fixtureDigestA, prev); err != nil {
			t.Fatalf("insert version %d: %v", v, err)
		}
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM definition_version WHERE tenant_id=$1 AND definition_kind='RULE' AND definition_key='integration-lineage'`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("lineage rows=%d, want 3", count)
	}
}

func TestTodo_DB_007_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	db.Exec(t, `INSERT INTO definition_version (tenant_id,definition_kind,definition_key,version,definition_digest,source_ref,body,published_by,published_at) VALUES ($1,'RULE','mutation',1,$2,'git://source','\x01','publisher',now())`, tenant, fixtureDigestA)
	if err := db.ExecErr(`UPDATE definition_version SET source_ref='forged' WHERE tenant_id=$1 AND definition_key='mutation'`, tenant); err == nil {
		t.Fatal("published source mutation succeeded")
	}
	var source string
	if err := db.QueryRow(context.Background(), `SELECT source_ref FROM definition_version WHERE tenant_id=$1 AND definition_key='mutation'`, tenant).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "git://source" {
		t.Fatalf("source after refused mutation=%q", source)
	}
}

func TestTodo_DB_007_Race(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	const n = 8
	conns := make([]dbport.Conn, n)
	for i := range conns {
		conns[i] = db.NewConn(t)
	}
	var wg sync.WaitGroup
	var won atomic.Int32
	for i := 0; i < n; i++ {
		conn := conns[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := conn.Exec(context.Background(), `INSERT INTO definition_version (tenant_id,definition_kind,definition_key,version,definition_digest,source_ref,body,published_by,published_at) VALUES ($1,'RULE','race-lineage',1,$2,'git://source','\x01','publisher',now())`, tenant, fixtureDigestA); err == nil {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := won.Load(); got != 1 {
		t.Fatalf("concurrent publication accepted %d duplicate versions", got)
	}
}
