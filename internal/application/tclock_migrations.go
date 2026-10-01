package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// TimeMigrator applies only the time store's independently owned schema.
type TimeMigrator func(context.Context, string, string, string, bootstrap.Logger) error

// WithTimeMigrator supplies the command-owned time migration adapter.
func WithTimeMigrator(migrate TimeMigrator) Option {
	return func(o *Options) { o.MigrateTime = migrate }
}

func applyClockMigrations(ctx context.Context, cfg ServeConfig, options Options, logger bootstrap.Logger) error {
	input := cfg.ClockRuntime
	if strings.TrimSpace(input.TimeDatabaseURL) == "" || !cfg.Migrate {
		return nil
	}
	input.CoreDatabaseURL = cfg.DatabaseURL
	parsed, err := ParseClockRuntimeConfig(input)
	if err != nil {
		return err
	}
	if options.MigrateTime == nil {
		return fmt.Errorf("application: -%s requires a time schema migrator", FieldTimeDatabaseURL)
	}
	if err := options.MigrateTime(ctx, parsed.TimeDatabaseURL, cfg.DatabaseURL, parsed.TimeSchema, logger); err != nil {
		return fmt.Errorf("application: apply clock migrations: %w", err)
	}
	return nil
}
