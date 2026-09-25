package projectoutboxstore

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectcommentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PM_003_OutboxRequestBounds(t *testing.T) {
	for _, tc := range []struct {
		tenant, project string
		after           int64
		limit           int
	}{
		{"", "p", 0, 1}, {"t", "", 0, 1}, {"t", "p", -1, 1}, {"t", "p", 0, 0}, {"t", "p", 0, MaxBatchSize + 1},
	} {
		if _, err := Pull(context.Background(), nil, tc.tenant, tc.project, tc.after, tc.limit); err != ErrInvalidRequest {
			t.Fatalf("Pull(%+v) error=%v", tc, err)
		}
	}
	if _, err := Cursor(context.Background(), nil, "t", "p", "c"); err != ErrInvalidRequest {
		t.Fatalf("nil cursor transaction error=%v", err)
	}
}

func TestTodo_PM_028_ProducerOrderingReplayAndRollback(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	ctx := context.Background()
	const tenant, project, owner = "tenant-outbox", "project-outbox", "owner"
	if err := projects.CreateProject(ctx, projectstore.ProjectRecord{TenantID: tenant, ID: project, OwnerID: owner, Name: "Board", Timezone: "UTC"}, owner, "HUMAN", "create-project"); err != nil {
		t.Fatal(err)
	}
	configs := projectconfigstore.New(projects)
	if err := configs.InitializeProject(ctx, tenant, project, owner); err != nil {
		t.Fatal(err)
	}
	draftConfig := projectconfigstore.StarterConfig()
	draftConfig.TaskTypes[0].Name = "Work item"
	draft, err := configs.SaveDraft(ctx, tenant, project, owner, 1, draftConfig, "save-draft")
	if err != nil {
		t.Fatal(err)
	}
	_, err = configs.Publish(ctx, tenant, project, owner, draft.Revision, 1, draft.Digest, "publish-config", projectconfigstore.ReviewEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	members, err := projectmemberstore.New(projects)
	if err != nil {
		t.Fatal(err)
	}
	if err := members.Invite(ctx, tenant, project, owner, "contributor", projectaccess.RoleContributor, 0, 1, "invite-contributor"); err != nil {
		t.Fatal(err)
	}
	views := projectviewstore.New(projects)
	view := projectboard.BoardView{ID: "team", Name: "Team", Version: 1, Audience: projectboard.AudienceProject, Columns: []projectboard.Column{{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}}}, Grouping: projectboard.Grouping{Kind: projectboard.GroupNone}, CardFields: []string{projectboard.CardTitle}}
	if _, err := views.SaveView(ctx, tenant, project, "", owner, view, 0, "save-view"); err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id) VALUES($1,'task','project-outbox','Task','todo')`, tenant)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	comments, err := projectcommentstore.New(projects)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC)
	comment := projectactivity.Comment{TenantID: tenant, ProjectID: project, TaskID: "task", ID: "comment", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "Started", HTML: "<p>Started</p>"}, ActorID: owner, At: at}}}
	activity := projectactivity.Activity{TenantID: tenant, ProjectID: project, TaskID: "task", CommentID: "comment", ActorID: owner, Kind: "COMMENT_CREATED", Revision: 1, At: at}
	if _, err := comments.CreateIdempotent(ctx, comment, activity, "comment-create"); err != nil {
		t.Fatal(err)
	}
	corrected := projectactivity.Revision{Number: 2, Text: projectactivity.SafeText{Source: "Updated", HTML: "<p>Updated</p>"}, ActorID: owner, At: at.Add(time.Minute)}
	correctedActivity := projectactivity.Activity{TenantID: tenant, ProjectID: project, TaskID: "task", CommentID: "comment", ActorID: owner, Kind: "COMMENT_CORRECTED", Revision: 2, At: corrected.At}
	if _, err := comments.CorrectIdempotent(ctx, tenant, project, "task", "comment", 1, corrected, correctedActivity, "comment-correct"); err != nil {
		t.Fatal(err)
	}
	tombstone := projectactivity.Revision{Number: 3, Tombstone: true, ActorID: owner, At: at.Add(2 * time.Minute)}
	tombstoneActivity := projectactivity.Activity{TenantID: tenant, ProjectID: project, TaskID: "task", CommentID: "comment", ActorID: owner, Kind: "COMMENT_TOMBSTONED", Revision: 3, At: tombstone.At}
	if _, err := comments.DeleteIdempotent(ctx, tenant, project, "task", "comment", 2, tombstone, tombstoneActivity, "comment-delete"); err != nil {
		t.Fatal(err)
	}

	// Exact idempotency replays must not allocate additional project sequences.
	if _, err := configs.SaveDraft(ctx, tenant, project, owner, 1, draftConfig, "save-draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := configs.Publish(ctx, tenant, project, owner, draft.Revision, 1, draft.Digest, "publish-config", projectconfigstore.ReviewEvidence{}); err != nil {
		t.Fatal(err)
	}
	if err := members.Invite(ctx, tenant, project, owner, "contributor", projectaccess.RoleContributor, 0, 1, "invite-contributor"); err != nil {
		t.Fatal(err)
	}
	if _, err := views.SaveView(ctx, tenant, project, "", owner, view, 0, "save-view"); err != nil {
		t.Fatal(err)
	}
	if _, err := comments.CreateIdempotent(ctx, comment, activity, "comment-create"); err != nil {
		t.Fatal(err)
	}
	if _, err := comments.CorrectIdempotent(ctx, tenant, project, "task", "comment", 1, corrected, correctedActivity, "comment-correct"); err != nil {
		t.Fatal(err)
	}
	if _, err := comments.DeleteIdempotent(ctx, tenant, project, "task", "comment", 2, tombstone, tombstoneActivity, "comment-delete"); err != nil {
		t.Fatal(err)
	}

	var events []Event
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, tenant, project, 0, 100)
		if err != nil {
			return err
		}
		events = batch
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"project.created", "workflow.draft_saved", "workflow.published", "membership.invited", "board_view.saved", "task.comment.created", "task.comment.corrected", "task.comment.tombstoned"}
	if len(events) != len(want) {
		t.Fatalf("event count=%d want %d: %#v", len(events), len(want), events)
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) || event.Type != want[i] {
			t.Fatalf("event[%d]=sequence %d type %q want sequence %d type %q", i, event.Sequence, event.Type, i+1, want[i])
		}
	}

	// A producer failure after state and activity writes rolls back those writes
	// and the project-wide sequence with the outbox insert.
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE FUNCTION fail_board_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='board_view.saved' THEN RAISE EXCEPTION 'injected outbox failure'; END IF; RETURN NEW; END $$`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `CREATE TRIGGER fail_board_event BEFORE INSERT ON project_outbox FOR EACH ROW EXECUTE FUNCTION fail_board_event()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rollbackView := view
	rollbackView.ID = "rollback"
	if _, err := views.SaveView(ctx, tenant, project, "", owner, rollbackView, 0, "save-rollback-view"); err == nil {
		t.Fatal("injected outbox failure unexpectedly committed")
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `DROP TRIGGER fail_board_event ON project_outbox`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DROP FUNCTION fail_board_event()`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id=$1 AND project_id=$2`, tenant, project).Scan(&count); err != nil {
			return err
		}
		if count != len(want) {
			t.Fatalf("event count after rollback=%d want %d", count, len(want))
		}
		var revision int
		if err := tx.QueryRow(ctx, `SELECT event_sequence FROM project WHERE tenant_id=$1 AND id=$2`, tenant, project).Scan(&revision); err != nil {
			return err
		}
		if revision != len(want) {
			t.Fatalf("project sequence after rollback=%d want %d", revision, len(want))
		}
		var viewsCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_board_view WHERE tenant_id=$1 AND project_id=$2 AND id='rollback'`, tenant, project).Scan(&viewsCount); err != nil {
			return err
		}
		if viewsCount != 0 {
			t.Fatalf("view row survived failed outbox append: %d", viewsCount)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PM_003_OutboxPullAckIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,$2,'owner','Project','UTC')`, tenant, "p")
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO project_outbox(tenant_id,project_id,event_id,project_sequence,event_type,schema_version,source_revision,classification,payload) VALUES($1,'p',$2,1,'task.created',1,1,'INTERNAL',jsonb_build_object('eventId',$2::text,'tenantId',$1::text,'projectId','p','aggregateId','task','eventType','task.created','sourceRevision',1,'projectSequence',1,'schemaVersion',1,'classification','INTERNAL','value',jsonb_build_object()))`, tenant, tenant+"-event")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var event Event
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, "tenant-a", "p", 0, 1)
		if err != nil {
			return err
		}
		if len(batch) != 1 || batch[0].ID != "tenant-a-event" || batch[0].Sequence != 1 || batch[0].SchemaVersion != 1 {
			t.Fatalf("unexpected batch: %#v", batch)
		}
		event = batch[0]
		if err := Ack(ctx, tx, "tenant-a", "p", "consumer-1", event); err != nil {
			return err
		}
		cursor, err := Cursor(ctx, tx, "tenant-a", "p", "consumer-1")
		if err != nil {
			return err
		}
		if cursor != 1 {
			t.Fatalf("cursor=%d", cursor)
		}
		if err := Ack(ctx, tx, "tenant-a", "p", "consumer-1", event); err != nil {
			return err
		}
		if _, err := Pull(ctx, tx, "tenant-a", "p", 1, 1); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		batch, err := Pull(ctx, tx, "tenant-b", "p", 0, 1)
		if err != nil {
			return err
		}
		if len(batch) != 1 || batch[0].ID != "tenant-b-event" {
			t.Fatalf("tenant leak or missing event: %#v", batch)
		}
		if err := Ack(ctx, tx, "tenant-b", "p", "consumer-1", event); err == nil {
			t.Fatal("cross-tenant event acknowledgement accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
