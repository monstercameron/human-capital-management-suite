package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	agentPoolMaxConns int32 = 8
	agentPoolMinConns int32 = 1
)

// composedAgentDatabase owns the independent agent database pool and the
// tenant-scoped persona store factory. Its close function is safe to call
// during both startup rollback and normal server shutdown.
type composedAgentDatabase struct {
	store    *agentstore.Store
	personas *agentpersonastore.Store
	review   *agentstore.PersonaReviewAuthorityStore
	close    func()
}

// composeAgentDatabase migrates and opens the isolated agent database when an
// agent DSN is configured. Migration uses a short-lived database/sql handle;
// serving uses the bounded agentstore pool and never shares core connections.
func composeAgentDatabase(ctx context.Context, cfg ServeConfig) (composedAgentDatabase, error) {
	if strings.TrimSpace(cfg.AgentDatabaseURL) == "" {
		return composedAgentDatabase{}, nil
	}
	if err := cfg.validateAgentDatabaseURL(); err != nil {
		return composedAgentDatabase{}, err
	}
	if err := ensureLocalDevAgentDatabase(ctx, cfg); err != nil {
		return composedAgentDatabase{}, err
	}
	if cfg.Migrate {
		if err := migrateAgentDatabase(ctx, cfg.AgentDatabaseURL); err != nil {
			return composedAgentDatabase{}, err
		}
	}
	store, err := agentstore.New(ctx, agentstore.Config{
		DSN: cfg.AgentDatabaseURL, CoreDSN: cfg.DatabaseURL,
		ChatDSN: cfg.ChatDatabaseURL, DocumentDSN: cfg.DocumentDatabaseURL,
		MaxConns: agentPoolMaxConns, MinConns: agentPoolMinConns,
	})
	if err != nil {
		return composedAgentDatabase{}, fmt.Errorf("compose agent database: %w", err)
	}
	mapTenant := func(tenant values.TenantId) uuid.UUID {
		return pgstore.TenantID(tenant.String())
	}
	personas, err := composeAgentPersonaStore(store, mapTenant, cfg.PersonaEvaluationPublicKeys, time.Now)
	if err != nil {
		store.Close()
		return composedAgentDatabase{}, fmt.Errorf("compose agent persona store: %w", err)
	}
	if err := reconcileAgentInstallationsOnStart(ctx, cfg, store, personas); err != nil {
		store.Close()
		return composedAgentDatabase{}, err
	}
	var review *agentstore.PersonaReviewAuthorityStore
	if strings.TrimSpace(cfg.PersonaReviewAuthorityDatabaseURL) != "" {
		review, err = agentstore.NewPersonaReviewAuthorityStore(ctx, agentstore.PersonaReviewAuthorityConfig{
			DSN: cfg.PersonaReviewAuthorityDatabaseURL, AgentDSN: cfg.AgentDatabaseURL,
			CoreDSN: cfg.DatabaseURL, ChatDSN: cfg.ChatDatabaseURL,
			DocumentDSN: cfg.DocumentDatabaseURL, MaxConns: 2, MinConns: 1,
		})
		if err != nil {
			store.Close()
			return composedAgentDatabase{}, fmt.Errorf("compose persona review authority: %w", err)
		}
	}
	return composedAgentDatabase{store: store, personas: personas, review: review, close: func() {
		if review != nil {
			review.Close()
		}
		store.Close()
	}}, nil
}

// composeAgentPersonaStore binds durable publication verification to the same
// persona store used by the catalog and lifecycle commands. It loads public
// verifier keys only; it never creates evaluation results or signing authority.
func composeAgentPersonaStore(db agentpersonastore.DB, mapTenant func(values.TenantId) uuid.UUID, keyring string, now func() time.Time) (*agentpersonastore.Store, error) {
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapTenant)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keyring) == "" {
		return agentpersonastore.NewWithReviewAuthority(db, mapTenant, reviews)
	}
	keys, err := ParsePersonaEvaluationVerificationKeys(keyring)
	if err != nil {
		return nil, err
	}
	evaluations, err := agentpersonastore.NewEvaluationSealAuthority(keys, mapTenant, now)
	if err != nil {
		return nil, err
	}
	return agentpersonastore.NewWithPublicationAuthorities(db, mapTenant, reviews, evaluations)
}

// ensureLocalDevAgentDatabase creates the local development database on the
// first run. PostgreSQL creates databases outside a database transaction, so
// this connects briefly to the maintenance database and only runs for the
// exact built-in loopback profile. Production and custom local DSNs must be
// provisioned by the deployment owner.
func ensureLocalDevAgentDatabase(ctx context.Context, cfg ServeConfig) error {
	if !localDevAgentDatabaseRequired(cfg) {
		return nil
	}
	config, err := pgx.ParseConfig(cfg.AgentDatabaseURL)
	if err != nil {
		return fmt.Errorf("compose agent database: parse local-dev DSN: %w", err)
	}
	databaseName := config.Database
	if strings.TrimSpace(databaseName) == "" {
		return fmt.Errorf("compose agent database: local-dev DSN has no database name")
	}
	config.Database = "postgres"
	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("compose agent database: local-dev maintenance connection: %w", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)", databaseName).Scan(&exists); err != nil {
		return fmt.Errorf("compose agent database: check local-dev database: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+(pgx.Identifier{databaseName}).Sanitize()); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
			return fmt.Errorf("compose agent database: create local-dev database: %w", err)
		}
	}
	return nil
}

func localDevAgentDatabaseRequired(cfg ServeConfig) bool {
	return cfg.Profile == ServeProfileLocalDev && cfg.AgentDatabaseURL == LocalDevAgentDatabaseURL
}

func migrateAgentDatabase(ctx context.Context, dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("compose agent database: parse migration DSN: %w", err)
	}
	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("compose agent database: migration connection: %w", err)
	}
	if err := agentstore.Migrate(ctx, db); err != nil {
		return fmt.Errorf("compose agent database: migrate: %w", err)
	}
	return nil
}

// validateAgentDatabaseURL keeps this composition's isolation check close to
// the config contract while agentstore repeats it defensively at its boundary.
func (c ServeConfig) validateAgentDatabaseURL() error {
	if strings.TrimSpace(c.AgentDatabaseURL) == "" {
		return nil
	}
	for field, other := range map[string]string{
		FieldDatabaseURL: c.DatabaseURL, FieldChatDatabaseURL: c.ChatDatabaseURL,
		FieldDocumentDatabaseURL: c.DocumentDatabaseURL,
	} {
		if strings.TrimSpace(other) != "" && sameLogicalDatabaseURL(c.AgentDatabaseURL, other) {
			return fmt.Errorf("-%s must use a database independent from -%s", FieldAgentDatabaseURL, field)
		}
	}
	return nil
}
