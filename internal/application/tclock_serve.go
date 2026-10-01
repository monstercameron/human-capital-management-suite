package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func composeConfiguredClock(ctx context.Context, cfg ServeConfig, pool *pgxadapter.Pool, options Options) (*ClockRuntime, error) {
	input := cfg.ClockRuntime
	if !input.enabled() {
		return nil, nil
	}
	input.CoreDatabaseURL = cfg.DatabaseURL
	parsed, err := ParseClockRuntimeConfig(input)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return ComposeClock(ctx, &parsed, pool, now, options.ClockDependencies)
}
