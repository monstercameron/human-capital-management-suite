package chatstore

import (
	"context"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// servingGrantsAfter51 is what the store runs against each table added after
// migration 51, and so what the migration that adds the table must give the
// serving roles: no less, or the feature is refused by the database, and no
// more, or a role can remove a record the store never removes.
var servingGrantsAfter51 = map[string]string{
	"agentux_ambient_workflow_mute": "SELECT,INSERT,UPDATE",
	"chat_ack_record":               "SELECT,INSERT",
	"chat_ack_requirement":          "SELECT,INSERT",
	"chat_approval_clarification":   "SELECT,INSERT",
	"chat_away":                     "SELECT,INSERT,UPDATE",
	"chat_channel_memory":           "SELECT,INSERT,UPDATE",
	"chat_conversation_case":        "SELECT,INSERT,UPDATE",
	"chat_decision":                 "SELECT,INSERT,UPDATE",
	"chat_digest_pref":              "SELECT,INSERT,UPDATE",
	"chat_followup_state":           "SELECT,INSERT,UPDATE",
	"chat_form_submission":          "SELECT,INSERT",
	"chat_information_barrier":      "SELECT,INSERT,UPDATE",
	"chat_input_request":            "SELECT,INSERT",
	"chat_notification_read":        "SELECT,INSERT",
	"chat_post_reference":           "SELECT,INSERT,DELETE",
	"chat_quiet_schedule":           "SELECT,INSERT,UPDATE",
	"chat_saved_search":             "SELECT,INSERT,UPDATE,DELETE",
	"chat_scheduled_post":           "SELECT,INSERT,UPDATE,DELETE",
	"chat_work_handoff":             "SELECT,INSERT,UPDATE",
	"chat_work_link":                "SELECT,INSERT",
}

// TestServingRoleIsGrantedEveryTableAddedAfter51 builds the two kinds of cell
// the grant migrations have had to serve (migration 00041 has the history) and
// checks the serving role's privileges on every table a later migration adds.
//
// "defaults": the role's privileges come from the schema's default privileges
// (SELECT, INSERT, UPDATE on every new table), so a migration must add DELETE
// where the store or a trigger deletes.
//
// "granted before": the role was given SELECT, INSERT and UPDATE once, on the
// tables that existed at migration 51, and the schema has no defaults. A table
// added later is unreadable unless its own migration grants it, which is how a
// whole feature answers 503 in one cell and works in another.
func TestServingRoleIsGrantedEveryTableAddedAfter51(t *testing.T) {
	ctx := context.Background()
	verbs := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	for _, cell := range []string{"defaults", "granted before"} {
		t.Run(cell, func(t *testing.T) {
			db := pgtest.NewEmpty(t)
			role := "servinggrants_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
			db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
			t.Cleanup(func() {
				_ = db.ExecErr(`DROP OWNED BY ` + role)
				_ = db.ExecErr(`DROP ROLE ` + role)
			})
			db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
			if cell == "defaults" {
				db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
			}
			migrations, err := fs.Sub(Migrations, "migrations")
			if err != nil {
				t.Fatal(err)
			}
			provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 51); err != nil {
				t.Fatal(err)
			}
			tables := func() map[string]bool {
				t.Helper()
				out := map[string]bool{}
				rows, err := db.SQL.QueryContext(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p')`, db.Schema)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for rows.Next() {
					var table string
					if err := rows.Scan(&table); err != nil {
						t.Fatal(err)
					}
					out[table] = true
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := tables()
			if cell == "granted before" {
				db.Exec(t, `GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA `+db.Schema+` TO `+role)
			}
			if _, err := provider.Up(ctx); err != nil {
				t.Fatal(err)
			}
			var added []string
			for table := range tables() {
				if !before[table] {
					added = append(added, table)
				}
			}
			sort.Strings(added)
			seen := map[string]bool{}
			for _, table := range added {
				seen[table] = true
				want, listed := servingGrantsAfter51[table]
				if !listed {
					t.Errorf("%s was added after migration 51 and is not in servingGrantsAfter51; say what the store runs against it and grant that in the migration that adds it", table)
					continue
				}
				for _, verb := range verbs {
					var held bool
					if err := db.SQL.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,$3)`, role, db.Schema+"."+table, verb).Scan(&held); err != nil {
						t.Fatal(err)
					}
					needed := strings.Contains(","+want+",", ","+verb+",")
					switch {
					case needed && !held:
						t.Errorf("the serving role may not %s %s, which the store or a trigger does; grant it in the migration that adds the table", verb, table)
					case !needed && held && (verb == "DELETE" || cell == "granted before"):
						// In the "defaults" cell the schema itself gives SELECT,
						// INSERT and UPDATE on every new table, so only DELETE
						// can be the migration's doing there.
						t.Errorf("the serving role may %s %s, which the store never does", verb, table)
					}
				}
			}
			for table := range servingGrantsAfter51 {
				if !seen[table] {
					t.Errorf("servingGrantsAfter51 names %s, which no migration after 51 adds", table)
				}
			}
		})
	}
}
