package main

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestTodo_TCLOCK002_TimeMigratorRejectsInvalidStoreBeforeMigration(t *testing.T) {
	var migrate application.TimeMigrator = migrateTimeUp
	for _, tc := range []struct{ name, timeURL, coreURL, schema string }{
		{"missing connection", "", "", "hcmnext_time"},
		{"invalid schema", "postgres://time:secret@localhost/db", "postgres://core:secret@localhost/db", "time;DROP SCHEMA public"},
		{"shared credential", "postgres://same:secret@localhost/db", "postgres://same:secret@localhost/db", "hcmnext_time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := migrate(context.Background(), tc.timeURL, tc.coreURL, tc.schema, nil); err == nil {
				t.Fatal("invalid store reached migration")
			}
		})
	}
}
