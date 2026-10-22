// Package iconmigration supplies the persona store's own migrations (agent icons,
// and who removed a placement) without importing the persona store, so the agent
// database migration runner can use it. They run after the agent database's own
// numbered migrations, in the 10000 range.
package iconmigration

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var migrations embed.FS

func Migrate(ctx context.Context, db *sql.DB) error {
	if ctx == nil || db == nil {
		return fmt.Errorf("agent icons: migration context and database required")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithTableName("goose_agent_icon_version"), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
