package iconmigration_test

import (
	"context"
	"testing"

	iconmigration "github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore/migrations"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestAgentUXIcon_Generated(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := iconmigration.Migrate(ctx, db.SQL); err != nil {
			t.Fatal("migration", err)
		}
	}
	var count int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename IN ('persona_icons','persona_icon_events') AND rowsecurity`).Scan(&count); err != nil || count != 2 {
		t.Fatal("tenant isolated icon tables", count, err)
	}
	if err := iconmigration.Migrate(ctx, nil); err == nil {
		t.Fatal("nil database accepted")
	}
	if err := iconmigration.Migrate(nil, db.SQL); err == nil {
		t.Fatal("nil context accepted")
	}
}
