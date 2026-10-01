package agentstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// AppRole is the non-login, least-privilege PostgreSQL role selected by the
// agent pool after authenticating with the deployment-managed agent credential.
const AppRole = "hcmnext_agent_app"

var (
	ErrInvalidConfig    = errors.New("agentstore: invalid configuration")
	ErrSharedDatabase   = errors.New("agent database must be independent from core, chat, and document databases")
	ErrSharedCredential = errors.New("agent database must use a distinct credential from core, chat, and document stores")
)

// Config supplies the agent database DSN and the identities of other data
// planes that must remain isolated from it. Pool bounds are applied to the
// agent DSN before the pool is opened.
type Config struct {
	DSN, CoreDSN, ChatDSN, DocumentDSN string
	MaxConns, MinConns                 int32
}

// Store owns the agent database pool and a separately bounded pool for the
// session locks held across persona steps. It is independent from workflow/core,
// chat, and document pools and may be passed as a dbport.Beginner to agent
// repositories.
type Store struct {
	pool      *pgxadapter.Pool
	fencePool *pgxadapter.Pool
}

// New opens and pings a bounded pool for the agent database. A core DSN is
// required so accidental fallback to the workflow database fails closed.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" || strings.TrimSpace(cfg.CoreDSN) == "" {
		return nil, fmt.Errorf("%w: agent and core DSNs are required", ErrInvalidConfig)
	}
	others := []string{cfg.CoreDSN, cfg.ChatDSN, cfg.DocumentDSN}
	for _, other := range others {
		if strings.TrimSpace(other) == "" {
			continue
		}
		if sameDatabase(cfg.DSN, other) {
			return nil, ErrSharedDatabase
		}
		if sameCredential(cfg.DSN, other) {
			return nil, ErrSharedCredential
		}
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	pool, err := pgxadapter.NewPool(ctx, dsn, map[string]string{"role": AppRole})
	if err != nil {
		return nil, fmt.Errorf("agentstore: open pool: %w", err)
	}
	// Session advisory locks can live for the full duration of a model/tool
	// callback. Keep them off the request/transaction pool so a slow step cannot
	// consume its capacity. The fence pool has its own hard bound of four
	// connections; concurrent steps beyond that wait for a slot or ctx cancel.
	fenceDSN, err := withPoolSize(cfg.DSN, 4, 0)
	if err != nil {
		pool.Close()
		return nil, err
	}
	fencePool, err := pgxadapter.NewPool(ctx, fenceDSN, map[string]string{"role": AppRole})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("agentstore: open persona fence pool: %w", err)
	}
	return &Store{pool: pool, fencePool: fencePool}, nil
}

// Begin opens a transaction from the agent-owned pool.
func (s *Store) Begin(ctx context.Context) (dbport.Tx, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("agentstore: transaction unavailable")
	}
	return s.pool.Begin(ctx)
}

// RunTx runs fn in one agent database transaction and commits only when fn
// succeeds. A failed callback leaves the transaction rolled back.
func (s *Store) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil {
		return errors.New("agentstore: transaction unavailable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agentstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentstore: commit: %w", err)
	}
	return nil
}

// RunTenantTx establishes the tenant RLS setting for the callback's
// transaction. The setting is transaction-local and cannot leak to the next
// pool borrower.
func (s *Store) RunTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is required", ErrInvalidConfig)
	}
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// RunTenantFenceTx keeps blocking source controls separate from run persistence.
func (s *Store) RunTenantFenceTx(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.fencePool == nil || ctx == nil || fn == nil || tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant fence transaction unavailable", ErrInvalidConfig)
	}
	tx, err := s.fencePool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agentstore: begin tenant fence: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentstore: commit tenant fence: %w", err)
	}
	return nil
}

// Close closes the agent pool and its connections.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
	if s != nil && s.fencePool != nil {
		s.fencePool.Close()
	}
}

// PoolStats returns connection accounting without exposing pgx types.
func (s *Store) PoolStats() pgxadapter.Saturation {
	if s == nil || s.pool == nil {
		return pgxadapter.Saturation{}
	}
	return s.pool.Stats()
}

func withPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", fmt.Errorf("%w: MinConns must not exceed MaxConns", ErrInvalidConfig)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("%w: parse agent DSN: %v", ErrInvalidConfig, err)
	}
	if maxConns > 0 {
		config.MaxConns = maxConns
	}
	if minConns > 0 {
		config.MinConns = minConns
	}
	if config.MinConns > config.MaxConns {
		return "", fmt.Errorf("%w: MinConns must not exceed MaxConns", ErrInvalidConfig)
	}
	return withPoolBounds(dsn, maxConns, minConns), nil
}

func withPoolBounds(dsn string, maxConns, minConns int32) string {
	out := strings.TrimSpace(dsn)
	isURL := strings.HasPrefix(out, "postgres://") || strings.HasPrefix(out, "postgresql://")
	if isURL {
		parsed, _ := url.Parse(out) // ParseConfig has already validated the DSN.
		query := parsed.Query()
		if maxConns > 0 {
			query.Set("pool_max_conns", strconv.FormatInt(int64(maxConns), 10))
		}
		if minConns > 0 {
			query.Set("pool_min_conns", strconv.FormatInt(int64(minConns), 10))
		}
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	add := func(key string, value int32) {
		if value <= 0 {
			return
		}
		// pgx uses the last occurrence of a keyword parameter. Appending the
		// configured bound preserves quoted credentials and overrides the DSN.
		out += " " + key + "=" + strconv.FormatInt(int64(value), 10)
	}
	add("pool_max_conns", maxConns)
	add("pool_min_conns", minConns)
	return out
}

func sameDatabase(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return sameEndpoint(ca, cb) && ca.Database == cb.Database
}

func sameCredential(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return false
	}
	return sameEndpoint(ca, cb) && strings.EqualFold(ca.User, cb.User)
}

func sameEndpoint(a, b *pgconn.Config) bool {
	return databaseHostIdentity(a.Host) == databaseHostIdentity(b.Host) && a.Port == b.Port
}

func databaseHostIdentity(host string) string {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return "loopback"
	default:
		return strings.ToLower(strings.TrimSuffix(host, "."))
	}
}

var _ dbport.Beginner = (*Store)(nil)
