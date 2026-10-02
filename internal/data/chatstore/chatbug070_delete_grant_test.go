package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_070 reproduces the review cell's roles: the serving role
// holds only the schema's default table privileges (SELECT, INSERT, UPDATE).
// Tombstoning a post fires chatrender_remove_views, which deletes the post's
// derived rows as the caller, so every delete of a message was refused by the
// database. Migration 39 must let the role remove those rows.
func TestTodo_CHATBUG_070(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chatbug070_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 38); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('c','t','PUBLIC_CHANNEL','General','writer')`)
	db.Exec(t, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('post','t','c','writer','t',1,'hello')`)

	tombstone := func() error {
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, `SET ROLE `+role); err != nil {
			t.Fatal(err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id','t',true)`); err != nil {
			t.Fatal(err)
		}
		changed, err := tx.Exec(ctx, `UPDATE chat_post SET body='',tombstoned=true,revision=revision+1,updated_at=now() WHERE tenant_id='t' AND conversation_id='c' AND id='post' AND tombstoned=false`)
		if err != nil {
			return err
		}
		if changed != 1 {
			t.Fatalf("tombstoned %d posts, want 1", changed)
		}
		return tx.Commit(ctx)
	}
	var denied *pgconn.PgError
	if err := tombstone(); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("before migration 39 the serving role's delete = %v; want the permission refusal the cell answered with", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tombstone(); err != nil {
		t.Fatalf("after migration 39 the serving role cannot delete a message: %v", err)
	}
}
