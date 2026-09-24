package application

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	transporthealth "github.com/monstercameron/human-capital-management-suite/internal/transport/health"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

const (
	serveHealthCheckInterval = 5 * time.Second
	serveHealthCheckTimeout  = 2 * time.Second
)

// newServeHealth builds the process health value once and shares it with both
// transports. The serve role admits traffic only when its PostgreSQL pool is
// reachable, its effective configuration fingerprint still matches the live
// config source, and the database reports the expected migration and release
// fingerprints.
func newServeHealth(cfg ServeConfig, pool *pgxadapter.Pool, ping func(context.Context) error, currentConfigFingerprint func() (string, error), interval, timeout time.Duration) *transporthealth.Server {
	if interval <= 0 {
		interval = serveHealthCheckInterval
	}
	if timeout <= 0 {
		timeout = serveHealthCheckTimeout
	}
	if ping == nil && pool != nil {
		ping = pool.Ping
	}
	readiness := func(ctx context.Context) error {
		if pool == nil || ping == nil {
			return fmt.Errorf("serve database pool is unavailable")
		}
		if cfg.ConfigFingerprint == "" || currentConfigFingerprint == nil {
			return fmt.Errorf("serve configuration fingerprint is unavailable")
		}
		observedConfigFingerprint, err := currentConfigFingerprint()
		if err != nil || observedConfigFingerprint == "" || observedConfigFingerprint != cfg.ConfigFingerprint {
			return fmt.Errorf("serve configuration fingerprint does not match")
		}
		if err := ping(ctx); err != nil {
			return err
		}
		want, err := migrations.TargetVersion()
		if err != nil {
			return err
		}
		wantSchemaFingerprint, err := migrations.ArtifactDigest()
		if err != nil {
			return err
		}
		var schema string
		var version int64
		var recordedFingerprint string
		if err := pool.QueryRow(ctx, `SELECT current_schema(),
			COALESCE((SELECT max(version_id) FILTER (WHERE is_applied) FROM goose_db_version), 0)::bigint,
			COALESCE((SELECT artifact_digest FROM schema_release ORDER BY recorded_at DESC LIMIT 1), '')`).Scan(&schema, &version, &recordedFingerprint); err != nil {
			return err
		}
		if schema == "" || version != want || recordedFingerprint == "" || recordedFingerprint != wantSchemaFingerprint {
			return fmt.Errorf("serve schema fingerprint does not match the expected version")
		}
		return nil
	}
	return transporthealth.New(transporthealth.Dependencies{
		Role:          bootstrap.RoleHCMNext,
		Live:          func() bool { return true },
		ReadyCheck:    readiness,
		CheckInterval: interval,
		CheckTimeout:  timeout,
	})
}
