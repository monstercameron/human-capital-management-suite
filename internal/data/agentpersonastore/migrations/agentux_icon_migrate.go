// Package iconmigration supplies the isolated agent identity migration without
// importing the persona store, so the agent database migration runner can use it.
package iconmigration

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed *_agentux_icon.sql
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
