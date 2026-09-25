// Package projectstore owns project data persistence and its database boundary.
// It shares the core PostgreSQL instance while using a separate schema, role,
// and bounded pool supplied by the composition root.
package projectstore

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

var (
	ErrCoreCredential = errors.New("project database must use a distinct core credential")
	ErrSchemaRequired = errors.New("project schema is required")
)

type Config struct {
	DSN, CoreDSN       string
	Schema             string
	MaxConns, MinConns int32
}

type Store struct{ pool *pgxadapter.Pool }

func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("project DSN is required")
	}
	if strings.TrimSpace(cfg.Schema) == "" {
		return nil, ErrSchemaRequired
	}
	if !validSchema(cfg.Schema) {
		return nil, errors.New("project schema must be a PostgreSQL identifier")
	}
	if cfg.CoreDSN != "" && sameDatabase(cfg.DSN, cfg.CoreDSN) && sameCredential(cfg.DSN, cfg.CoreDSN) {
		return nil, ErrCoreCredential
	}
	dsn, err := withProjectPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	dsn, err = withSchema(dsn, cfg.Schema)
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
	return &Store{pool: p}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil {
		return errors.New("project store: transaction unavailable")
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

func (s *Store) RunTenantTx(ctx context.Context, tenantID string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if strings.TrimSpace(tenantID) == "" {
			return errors.New("tenant is required")
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (s *Store) PoolStats() pgxadapter.Saturation { return s.pool.Stats() }

func withProjectPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", errors.New("project pool MinConns must not exceed MaxConns")
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

func withSchema(dsn, schema string) (string, error) {
	if !validSchema(schema) {
		return "", errors.New("project schema must be a PostgreSQL identifier")
	}
	trimmed := strings.TrimSpace(dsn)
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "", err
		}
		query := u.Query()
		query.Set("search_path", schema)
		u.RawQuery = query.Encode()
		return u.String(), nil
	}
	if _, err := pgconn.ParseConfig(trimmed); err != nil {
		return "", err
	}
	return trimmed + " search_path=" + schema, nil
}

func validSchema(schema string) bool {
	if len(schema) == 0 || len(schema) > 63 {
		return false
	}
	for i, r := range schema {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func sameDatabase(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.Port == cb.Port && strings.EqualFold(ca.Host, cb.Host) && ca.Database == cb.Database
}

func sameCredential(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.User == cb.User && ca.Password == cb.Password
}
