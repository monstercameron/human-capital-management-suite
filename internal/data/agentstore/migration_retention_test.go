package agentstore

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestTodo_AGENT_008_Recovery_RollbackPreservesPersonaEvidence(t *testing.T) {
	for _, test := range []struct {
		migration string
		tables    []string
	}{
		{"00007_persona_invocation.sql", []string{"persona_invocations"}},
		{"00008_persona_chat_identity.sql", []string{"persona_chat_identities"}},
		{"00009_persona_limits.sql", []string{"persona_limit_reservations", "persona_limit_buckets"}},
		{"00010_persona_final_output.sql", []string{"persona_final_outputs"}},
		{"00011_persona_publication_evidence.sql", []string{"persona_lifecycle_events"}},
		{"00012_persona_review_evidence.sql", []string{"persona_review_decision", "persona_review_grant"}},
		{"00015_persona_run_policy.sql", []string{"persona_run_policy"}},
		{"00016_persona_agent_principal.sql", []string{"persona_agent_principal_binding"}},
		{"00017_persona_model_route_policy.sql", []string{"persona_model_route_policy"}},
	} {
		t.Run(test.migration, func(t *testing.T) {
			db := pgtest.NewEmpty(t)
			body, err := fs.ReadFile(Migrations, "migrations/"+test.migration)
			if err != nil {
				t.Fatal(err)
			}
			_, down, found := strings.Cut(string(body), "-- +goose Down")
			if !found {
				t.Fatal("migration has no rollback")
			}
			// Use minimal retained relations so every exact Down script is driven
			// through Goose and PostgreSQL without unrelated later migrations
			// stopping rollback before its own evidence-protection boundary.
			up := "-- +goose Up\n"
			for _, table := range test.tables {
				up += "CREATE TABLE " + table + "(to_state text);\nINSERT INTO " + table + " VALUES ('PUBLISHED');\n"
			}
			provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, fstest.MapFS{"00001_retention.sql": &fstest.MapFile{Data: []byte(up + "-- +goose Down" + down)}}, goose.WithDisableGlobalRegistry(true))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Up(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Down(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot remove retained") {
				t.Fatalf("rollback failed to protect retained evidence: %v", err)
			}
			for _, table := range test.tables {
				var count int
				if err := db.SQL.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
					t.Fatalf("rollback lost retained %s: count=%d err=%v", table, count, err)
				}
			}
		})
	}
}
