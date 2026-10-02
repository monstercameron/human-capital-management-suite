package agentstore

import (
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"testing"
)

func TestIntegrate1BaseMigrationsIncludeIcons(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := Migrate(t.Context(), db.SQL); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db.SQL); err != nil {
		t.Fatal("idempotent startup", err)
	}
	for _, name := range []string{"persona_icons", "persona_icon_events"} {
		var enabled bool
		if err := db.SQL.QueryRowContext(t.Context(), `SELECT relrowsecurity FROM pg_class WHERE oid=$1::regclass`, name).Scan(&enabled); err != nil || !enabled {
			t.Fatal("icon table or RLS missing", name, enabled, err)
		}
	}
	var trigger bool
	if err := db.SQL.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid='persona_icon_events'::regclass AND p.proname='forbid_mutation')`).Scan(&trigger); err != nil || !trigger {
		t.Fatal("icon audit mutation trigger missing", trigger, err)
	}
}
