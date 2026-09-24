// Package documenthubstore owns Knowledge document persistence (HUB-001). It
// has no import path into chat or core/workflow storage and accepts a
// document DSN at its composition root. Migrations, outbox and backup follow
// once the document schema lands; this file owns only the isolation fence
// and pool budget, mirroring internal/data/chatstore.
package documenthubstore

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// ErrIsolatedDatabase is returned when the document DSN names the same
// logical database as chat or core/workflow storage.
var ErrIsolatedDatabase = errors.New("document database must be isolated from chat and core databases")

type Config struct {
	DSN, ChatDSN, CoreDSN string
	MaxConns, MinConns    int32
}

type Store struct {
	pool  *pgxadapter.Pool
	texts *versionTextCache
	// indexModel is the embedding model searchable writes enqueue index
	// jobs for; empty enqueues nothing.
	indexModel string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("document DSN is required")
	}
	if cfg.ChatDSN != "" && sameDatabase(cfg.DSN, cfg.ChatDSN) {
		return nil, ErrIsolatedDatabase
	}
	if cfg.CoreDSN != "" && sameDatabase(cfg.DSN, cfg.CoreDSN) {
		return nil, ErrIsolatedDatabase
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	p, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{pool: p, texts: &versionTextCache{}}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Begin exposes the document-owned transaction port to adjacent Knowledge
// adapters; callers still use dbport and cannot reach pgx types.
func (s *Store) Begin(ctx context.Context) (dbport.Tx, error) { return s.pool.Begin(ctx) }

// RunTx exposes the document pool's narrow transaction port to other
// document-owned repositories. It never exposes the chat or core database
// pools.
func (s *Store) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil {
		return errors.New("document store: transaction unavailable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunTenantTx applies the document database RLS tenant setting before
// invoking a transaction callback.
func (s *Store) RunTenantTx(ctx context.Context, tenantID string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if err := tenant(ctx, tx, tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func tenant(ctx context.Context, tx dbport.Tx, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("tenant is required")
	}
	_, err := tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", id)
	return err
}

// withPoolSize applies the configured document pool bounds to the document
// DSN. A bound already spelled in the DSN wins, because that spelling is the
// operator's own and this function must not silently contradict it.
func withPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", errors.New("document pool MinConns must not exceed MaxConns")
	}
	out := strings.TrimSpace(dsn)
	url := strings.HasPrefix(out, "postgres://") || strings.HasPrefix(out, "postgresql://")
	add := func(key string, value int32) {
		if value <= 0 || strings.Contains(out, key+"=") {
			return
		}
		if !url {
			out += " " + key + "=" + strconv.FormatInt(int64(value), 10)
			return
		}
		separator := "?"
		if strings.Contains(out, "?") {
			separator = "&"
		}
		out += separator + key + "=" + strconv.FormatInt(int64(value), 10)
	}
	add("pool_max_conns", maxConns)
	add("pool_min_conns", minConns)
	return out, nil
}

// sameDatabase compares logical database identity across both DSN spellings.
// pgconn.ParseConfig understands the URL form and the libpq keyword form, so
// a composition that hands documents a keyword DSN and chat or core a URL
// for the same database is still rejected. Credentials are expected to
// differ as an additional defense; a shared database is forbidden even when
// callers supplied separate users or reordered query parameters.
func sameDatabase(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.Port == cb.Port && strings.EqualFold(ca.Host, cb.Host) && ca.Database == cb.Database
}
