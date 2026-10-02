package agentpersonastore

import (
	"context"
	"database/sql"
	"fmt"

	iconmigration "github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore/migrations"
)

// MigrateIcons applies the identity-owned icon migration after agentstore.Migrate.
func MigrateIcons(ctx context.Context, db *sql.DB) error {
	if ctx == nil || db == nil {
		return fmt.Errorf("%w: icon migration database and context required", ErrInvalid)
	}
	return iconmigration.Migrate(ctx, db)
}
