package projectcommentstore

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func commentFixture(t *testing.T) (*Store, *projectstore.Store, *pgtest.DB) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	ctx := context.Background()
	if err := projects.CreateProject(ctx, projectstore.ProjectRecord{ID: "project-a", TenantID: "tenant-a", OwnerID: "owner", Name: "Board", Timezone: "UTC"}, "owner", "HUMAN", "project-create"); err != nil {
		t.Fatal(err)
	}
	if err := projects.CreateTask(ctx, projectstore.TaskRecord{ID: "task-a", TenantID: "tenant-a", ProjectID: "project-a", Title: "Task", StatusID: "open"}, "owner", "HUMAN", "task-create"); err != nil {
		t.Fatal(err)
	}
	store, err := New(projects)
	if err != nil {
		t.Fatal(err)
	}
	return store, projects, db
}

func TestTodo_PM_019_Integration(t *testing.T) {
	ctx := context.Background()
	store, _, _ := commentFixture(t)
	at := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)
	c := projectactivity.Comment{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "comment-a", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "initial held text", HTML: "<p>initial held text</p>"}, ActorID: "alice", At: at}}}
	a := projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "alice", Kind: "COMMENT_CREATED", Revision: 1, At: at}
	if err := store.Create(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	corrected := projectactivity.Revision{Number: 2, Text: projectactivity.SafeText{Source: "corrected text", HTML: "<p>corrected text</p>"}, ActorID: "alice", At: at.Add(time.Minute)}
	activity := projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "alice", Kind: "COMMENT_CORRECTED", Revision: 2, At: corrected.At}
	got, err := store.Correct(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 1, corrected, activity)
	if err != nil || got.Current().Number != 2 || got.Current().Text.Source != "corrected text" {
		t.Fatalf("correct: %#v, %v", got, err)
	}
	if _, err := store.Correct(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 1, corrected, activity); !errors.Is(err, projectactivity.ErrConflict) {
		t.Fatalf("stale revision err=%v", err)
	}
	c2 := c
	c2.ID = "comment-b"
	c2.Revisions = []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "second", HTML: "<p>second</p>"}, ActorID: "bob", At: at.Add(2 * time.Minute)}}
	a2 := projectactivity.Activity{TenantID: c2.TenantID, ProjectID: c2.ProjectID, TaskID: c2.TaskID, CommentID: c2.ID, ActorID: "bob", Kind: "COMMENT_CREATED", Revision: 1, At: c2.Revisions[0].At}
	if err := store.Create(ctx, c2, a2); err != nil {
		t.Fatal(err)
	}
	tombstone := projectactivity.Revision{Number: 3, Tombstone: true, ActorID: "alice", At: at.Add(3 * time.Minute)}
	tombstoneActivity := projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "alice", Kind: "COMMENT_TOMBSTONED", Revision: 3, At: tombstone.At}
	if _, err := store.Correct(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 2, tombstone, tombstoneActivity); err != nil {
		t.Fatal(err)
	}

	var events []projectactivity.Activity
	var prior, ceiling uint64
	for {
		comments, page, last, snapshot, more, err := store.Page(ctx, c.TenantID, c.ProjectID, c.TaskID, prior, ceiling, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) != len(page) || len(page) > 1 {
			t.Fatalf("unbounded/misaligned page: comments=%d activity=%d", len(comments), len(page))
		}
		if ceiling == 0 {
			ceiling = snapshot
		}
		events = append(events, page...)
		if !more {
			break
		}
		if last <= prior {
			t.Fatalf("cursor did not advance: %d <= %d", last, prior)
		}
		prior = last
	}
	if len(events) != 4 {
		t.Fatalf("got %d activity rows, want 4", len(events))
	}
	if events[0].Sequence >= events[1].Sequence || events[1].Sequence >= events[2].Sequence || events[2].Sequence >= events[3].Sequence {
		t.Fatalf("activity sequence not increasing: %#v", events)
	}
	// Earlier source text remains queryable after correction and tombstoning.
	comments, _, _, _, _, err := store.Page(ctx, c.TenantID, c.ProjectID, c.TaskID, 0, ceiling, 10)
	if err != nil {
		t.Fatal(err)
	}
	if comments[0].Current().Text.Source != "initial held text" || comments[3].Current().Tombstone != true {
		t.Fatalf("revision history lost: %#v", comments)
	}
}

func TestTodo_PM_019_UnifiedTimelineIncludesTaskMutations(t *testing.T) {
	ctx := context.Background()
	store, projects, _ := commentFixture(t)
	entries, _, _, more, err := store.TimelinePage(ctx, "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil || more || len(entries) != 1 || entries[0].Kind != "task.created" || entries[0].Sequence != 1 {
		t.Fatalf("initial task event missing from timeline: %#v more=%v err=%v", entries, more, err)
	}
	at := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)
	c := projectactivity.Comment{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "timeline-comment", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "private comment body", HTML: "<p>private comment body</p>"}, ActorID: "alice", At: at}}}
	a := projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "alice", Kind: "COMMENT_CREATED", Revision: 1, At: at}
	if err := store.Create(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	entries, _, _, more, err = store.TimelinePage(ctx, "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil || more || len(entries) != 2 {
		t.Fatalf("unified page: %#v more=%v err=%v", entries, more, err)
	}
	if entries[0].Kind != "task.created" || entries[0].Sequence != 1 || entries[0].CommentID != "" || entries[1].Kind != "COMMENT_CREATED" || entries[1].Sequence != 2 || entries[1].CommentID != c.ID {
		t.Fatalf("unexpected merged order or projection: %#v", entries)
	}
	// A project event can share the task aggregate ID in separate namespaces;
	// only task.* events belong in the task timeline.
	if err := projects.CreateProject(ctx, projectstore.ProjectRecord{ID: "same-id", TenantID: "tenant-a", OwnerID: "owner", Name: "Same ID", Timezone: "UTC"}, "owner", "HUMAN", "same-project"); err != nil {
		t.Fatal(err)
	}
	if err := projects.CreateTask(ctx, projectstore.TaskRecord{ID: "same-id", TenantID: "tenant-a", ProjectID: "same-id", Title: "Same ID", StatusID: "open"}, "owner", "HUMAN", "same-task"); err != nil {
		t.Fatal(err)
	}
	entries, _, _, more, err = store.TimelinePage(ctx, "tenant-a", "same-id", "same-id", 0, 0, 10)
	if err != nil || more || len(entries) != 1 || entries[0].Kind != "task.created" {
		t.Fatalf("project event leaked into task timeline: %#v more=%v err=%v", entries, more, err)
	}
}

func TestTodo_PM_019_Migration08RestrictedOwnerAndImmutableActivity(t *testing.T) {
	ctx := context.Background()
	store, _, db := commentFixture(t)
	at := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)
	c := projectactivity.Comment{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "legacy-comment", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "legacy", HTML: "<p>legacy</p>"}, ActorID: "alice", At: at}}}
	a := projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "alice", Kind: "COMMENT_CREATED", Revision: 1, At: at}
	if err := store.Create(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	adminProvider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminProvider.DownTo(ctx, 7); err != nil {
		t.Fatal(err)
	}

	role := "timeline_migrator_" + time.Now().Format("150405000")
	quotedRole, quotedSchema := `"`+role+`"`, `"`+db.Schema+`"`
	if _, err := db.SQL.ExecContext(ctx, `CREATE ROLE `+quotedRole+` LOGIN PASSWORD 'timeline-test' NOSUPERUSER NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(context.Background(), `REASSIGN OWNED BY `+quotedRole+` TO postgres`)
		_, _ = db.SQL.ExecContext(context.Background(), `DROP OWNED BY `+quotedRole)
		_, _ = db.SQL.ExecContext(context.Background(), `DROP ROLE `+quotedRole)
	})
	for _, stmt := range []string{
		`ALTER SCHEMA ` + quotedSchema + ` OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_task OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_task_link OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_activity OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_outbox OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_outbox_cursor OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.project_task_comment_activity OWNER TO ` + quotedRole,
		`ALTER TABLE ` + quotedSchema + `.goose_db_version OWNER TO ` + quotedRole,
	} {
		if _, err := db.SQL.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("restricted migration setup %q: %v", stmt, err)
		}
	}
	parsed, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(role, "timeline-test")
	query := parsed.Query()
	query.Set("search_path", db.Schema)
	parsed.RawQuery = query.Encode()
	roleConfig, err := pgx.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	roleDB := stdlib.OpenDB(*roleConfig)
	roleDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = roleDB.Close() })
	roleProvider, err := goose.NewProvider(goose.DialectPostgres, roleDB, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roleProvider.Up(ctx); err != nil {
		t.Fatalf("restricted role migration with FORCE RLS and immutable activity: %v", err)
	}

	entries, last, ceiling, more, err := store.TimelinePage(ctx, "tenant-a", "project-a", "task-a", 0, 0, 1)
	if err != nil || !more || len(entries) != 1 || entries[0].Kind != "COMMENT_CREATED" || entries[0].Sequence != 1 {
		t.Fatalf("first legacy page was not deterministically ranked: %#v more=%v err=%v", entries, more, err)
	}
	entries, last, _, more, err = store.TimelinePage(ctx, "tenant-a", "project-a", "task-a", last, ceiling, 1)
	if err != nil || more || len(entries) != 1 || entries[0].Kind != "task.created" || entries[0].Sequence != 2 {
		t.Fatalf("second legacy page was not deterministic: %#v more=%v err=%v", entries, more, err)
	}
	next := projectactivity.Comment{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "post-migration-comment", CreatedAt: at.Add(time.Hour), Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "new", HTML: "<p>new</p>"}, ActorID: "alice", At: at.Add(time.Hour)}}}
	if err := store.Create(ctx, next, projectactivity.Activity{TenantID: next.TenantID, ProjectID: next.ProjectID, TaskID: next.TaskID, CommentID: next.ID, ActorID: "alice", Kind: "COMMENT_CREATED", Revision: 1, At: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	entries, _, _, more, err = store.TimelinePage(ctx, "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil || more || len(entries) != 3 || entries[2].Sequence != 3 || entries[2].CommentID != next.ID {
		t.Fatalf("new write did not continue after legacy sequence: %#v more=%v err=%v", entries, more, err)
	}
	// A tenant-visible mutation still cannot rewrite append-only audit rows.
	if _, err := roleDB.ExecContext(ctx, `SELECT set_config('hcmnext.tenant_id','tenant-a',false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := roleDB.ExecContext(ctx, `UPDATE project_activity SET task_sequence=1 WHERE tenant_id='tenant-a' AND project_id='project-a' AND aggregate_id='task-a'`); err == nil {
		t.Fatal("append-only project activity unexpectedly allowed a backfill update")
	}
}

func TestTodo_PM_019_Security(t *testing.T) {
	ctx := context.Background()
	store, projects, db := commentFixture(t)
	c := commandComment("security-comment", "tenant private", time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC))
	if _, err := IdempotentStore(store).CreateIdempotent(ctx, c, commandActivity(c, "COMMENT_CREATED", 1, c.CreatedAt), "tenant-private-key"); err != nil {
		t.Fatal(err)
	}
	rows, _, _, _, _, err := store.Page(ctx, "tenant-b", "project-a", "task-a", 0, 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("tenant B read tenant A comment data: %#v", rows)
	}
	if err := store.Create(ctx, projectactivity.Comment{TenantID: "tenant-b", ProjectID: "project-a", TaskID: "task-a", ID: "forged", CreatedAt: time.Now().UTC(), Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "x", HTML: "<p>x</p>"}, ActorID: "x", At: time.Now().UTC()}}}, projectactivity.Activity{TenantID: "tenant-b", ProjectID: "project-a", TaskID: "task-a", CommentID: "forged", ActorID: "x", Kind: "COMMENT_CREATED", Revision: 1, At: time.Now().UTC()}); !errors.Is(err, projectactivity.ErrNotFound) {
		t.Fatalf("cross-tenant task reference err=%v", err)
	}
	role := "project_comment_rls_test"
	qrole, qschema := `"`+role+`"`, `"`+db.Schema+`"`
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+qrole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT SELECT ON ALL TABLES IN SCHEMA `+qschema+` TO `+qrole)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projects.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+qrole)
			return err
		}); err != nil {
			t.Errorf("drop test role: %v", err)
		}
	})
	var visible, receipts int
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+role); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','tenant-b',true)`); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_task_comment WHERE tenant_id='tenant-a'`).Scan(&visible); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_task_comment_idempotency WHERE tenant_id='tenant-a'`).Scan(&receipts)
	}); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("tenant RLS exposed %d rows", visible)
	}
	if receipts != 0 {
		t.Fatalf("tenant RLS exposed %d idempotency receipts", receipts)
	}
}

func TestTodo_PM_019_Idempotency(t *testing.T) {
	ctx := context.Background()
	store, _, _ := commentFixture(t)
	commands, ok := any(store).(IdempotentStore)
	if !ok {
		t.Fatal("persistent store does not implement idempotent commands")
	}
	at := time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)
	c := commandComment("comment-retry", "first text", at)
	a := commandActivity(c, "COMMENT_CREATED", 1, at)
	created, err := commands.CreateIdempotent(ctx, c, a, "create-key")
	if err != nil {
		t.Fatal(err)
	}
	retry := commandComment(c.ID, "first text", at.Add(time.Hour))
	replayed, err := commands.CreateIdempotent(ctx, retry, commandActivity(retry, "COMMENT_CREATED", 1, retry.CreatedAt), "create-key")
	if err != nil {
		t.Fatalf("same-key retry: %v", err)
	}
	if !replayed.CreatedAt.Equal(created.CreatedAt) || replayed.Current().At != created.Current().At {
		t.Fatalf("retry did not return original create result: %#v != %#v", replayed, created)
	}
	changed := commandComment(c.ID, "different text", at.Add(2*time.Hour))
	if _, err := commands.CreateIdempotent(ctx, changed, commandActivity(changed, "COMMENT_CREATED", 1, changed.CreatedAt), "create-key"); !errors.Is(err, projectactivity.ErrConflict) {
		t.Fatalf("conflicting create key err=%v", err)
	}

	revisionAt := at.Add(3 * time.Minute)
	r := projectactivity.Revision{Number: 2, Text: projectactivity.SafeText{Source: "corrected", HTML: "<p>corrected</p>"}, ActorID: "owner", At: revisionAt}
	corrected, err := commands.CorrectIdempotent(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 1, r, commandActivity(c, "COMMENT_CORRECTED", 2, revisionAt), "correct-key")
	if err != nil {
		t.Fatal(err)
	}
	r.At = revisionAt.Add(time.Hour)
	correctRetry, err := commands.CorrectIdempotent(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 1, r, commandActivity(c, "COMMENT_CORRECTED", 2, r.At), "correct-key")
	if err != nil {
		t.Fatalf("same correction retry: %v", err)
	}
	if !correctRetry.Current().At.Equal(corrected.Current().At) || correctRetry.Current().Text.Source != "corrected" {
		t.Fatalf("correction retry result = %#v, want original %#v", correctRetry, corrected)
	}
	if _, err := commands.CorrectIdempotent(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 1, r, commandActivity(c, "COMMENT_CORRECTED", 2, r.At), "another-key"); !errors.Is(err, projectactivity.ErrConflict) {
		t.Fatalf("stale correction err=%v", err)
	}

	tombstoneAt := at.Add(4 * time.Minute)
	tombstone := projectactivity.Revision{Number: 3, Tombstone: true, ActorID: "owner", At: tombstoneAt}
	deleted, err := commands.DeleteIdempotent(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 2, tombstone, commandActivity(c, "COMMENT_TOMBSTONED", 3, tombstoneAt), "delete-key")
	if err != nil || !deleted.Current().Tombstone {
		t.Fatalf("delete result=%#v err=%v", deleted, err)
	}
	tombstone.At = tombstoneAt.Add(time.Hour)
	deleteRetry, err := commands.DeleteIdempotent(ctx, c.TenantID, c.ProjectID, c.TaskID, c.ID, 2, tombstone, commandActivity(c, "COMMENT_TOMBSTONED", 3, tombstone.At), "delete-key")
	if err != nil || !deleteRetry.Current().Tombstone || !deleteRetry.Current().At.Equal(deleted.Current().At) {
		t.Fatalf("delete replay=%#v err=%v", deleteRetry, err)
	}
	if _, _, _, _, _, err := store.Page(ctx, c.TenantID, c.ProjectID, c.TaskID, 0, 0, 10); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PM_019_Idempotency_ConcurrentRetry(t *testing.T) {
	ctx := context.Background()
	store, _, _ := commentFixture(t)
	commands := IdempotentStore(store)
	base := time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC)
	const attempts = 8
	results := make([]projectactivity.Comment, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			at := base.Add(time.Duration(i) * time.Second)
			c := commandComment("comment-concurrent", "one command", at)
			results[i], errs[i] = commands.CreateIdempotent(ctx, c, commandActivity(c, "COMMENT_CREATED", 1, at), "concurrent-key")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	for i := 1; i < attempts; i++ {
		if !results[i].CreatedAt.Equal(results[0].CreatedAt) {
			t.Fatalf("attempt %d returned a different creation time", i)
		}
	}
	_, activities, _, _, _, err := store.Page(ctx, "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 {
		t.Fatalf("concurrent retry wrote %d activities, want 1", len(activities))
	}
}

func TestTodo_PM_019_Idempotency_Rollback(t *testing.T) {
	ctx := context.Background()
	store, projects, _ := commentFixture(t)
	commands := IdempotentStore(store)
	at := time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC)
	c := commandComment("comment-rollback", "rollback text", at)
	a := commandActivity(c, "COMMENT_CREATED", 1, at)
	if err := projects.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `CREATE FUNCTION fail_comment_activity_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected comment activity failure'; END $$`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `CREATE TRIGGER fail_comment_activity_test BEFORE INSERT ON project_task_comment_activity FOR EACH ROW EXECUTE FUNCTION fail_comment_activity_test()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := commands.CreateIdempotent(ctx, c, a, "rollback-key"); err == nil {
		t.Fatal("injected activity failure was ignored")
	}
	if err := projects.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `DROP TRIGGER fail_comment_activity_test ON project_task_comment_activity`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DROP FUNCTION fail_comment_activity_test()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := commands.CreateIdempotent(ctx, c, a, "rollback-key"); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	comments, activities, _, _, _, err := store.Page(ctx, c.TenantID, c.ProjectID, c.TaskID, 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || len(activities) != 1 {
		t.Fatalf("rollback left partial or duplicate data: %d comments, %d events", len(comments), len(activities))
	}
}

func commandComment(id, text string, at time.Time) projectactivity.Comment {
	return projectactivity.Comment{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: id, CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: text, HTML: "<p>" + text + "</p>"}, ActorID: "owner", At: at}}}
}
func commandActivity(c projectactivity.Comment, kind string, revision uint64, at time.Time) projectactivity.Activity {
	return projectactivity.Activity{TenantID: c.TenantID, ProjectID: c.ProjectID, TaskID: c.TaskID, CommentID: c.ID, ActorID: "owner", Kind: kind, Revision: revision, At: at}
}

func TestConstructorRejectsNilProjectStore(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, projectactivity.ErrPortMissing) {
		t.Fatalf("New(nil) err=%v", err)
	}
}

func TestStorageRejectsActiveOrMalformedRendering(t *testing.T) {
	for _, markup := range []string{
		`<script>alert(1)</script>`,
		`<p onclick="alert(1)">hello</p>`,
		`<a href="javascript:alert(1)">hello</a>`,
		`<svg><a href="https://example.com">hello</a></svg>`,
	} {
		if validSafeText(projectactivity.SafeText{Source: "hello", HTML: markup}) {
			t.Errorf("accepted active or unapproved markup %q", markup)
		}
	}
	if !validSafeText(projectactivity.SafeText{Source: "hello", HTML: `<p><strong>hello</strong> <a href="https://example.com">link</a></p>`}) {
		t.Fatal("rejected inert allowlisted formatting")
	}
}
