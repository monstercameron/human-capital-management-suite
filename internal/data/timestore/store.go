// Package timestore owns durable time-keeping persistence in its own
// schema: punch observations, sessions, devices, profiles, timecards,
// shifts and the clock event outbox. Each concern lives in its own file
// and its own numbered migration range.
package timestore

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
	ErrNotFound            = errors.New("time record not found")
	ErrRevisionConflict    = errors.New("time revision conflict")
	ErrIdempotencyConflict = errors.New("time idempotency key conflict")
	ErrInvalid             = errors.New("invalid time record")
	ErrCoreCredential      = errors.New("time database must use a distinct core credential")
)

type Config struct {
	DSN, CoreDSN, Schema string
	MaxConns, MinConns   int32
}
type Store struct{ pool *pgxadapter.Pool }

const SchemaName = "hcmnext_time"

func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("time DSN is required")
	}
	if !validSchema(cfg.Schema) {
		return nil, errors.New("time schema must be a PostgreSQL identifier")
	}
	if cfg.CoreDSN != "" && sameDatabase(cfg.DSN, cfg.CoreDSN) && sameCredential(cfg.DSN, cfg.CoreDSN) {
		return nil, ErrCoreCredential
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	dsn = strings.TrimSpace(dsn)
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("search_path", cfg.Schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		if _, err := pgconn.ParseConfig(dsn); err != nil {
			return nil, err
		}
		dsn += " search_path=" + cfg.Schema
	}
	p, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{pool: p}, nil
}
func validSchema(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func withPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", errors.New("time pool MinConns must not exceed MaxConns")
	}
	out := strings.TrimSpace(dsn)
	isURL := strings.HasPrefix(out, "postgres://") || strings.HasPrefix(out, "postgresql://")
	add := func(key string, value int32) {
		if value <= 0 || strings.Contains(out, key+"=") {
			return
		}
		v := strconv.FormatInt(int64(value), 10)
		if !isURL {
			out += " " + key + "=" + v
			return
		}
		sep := "?"
		if strings.Contains(out, "?") {
			sep = "&"
		}
		out += sep + key + "=" + v
	}
	add("pool_max_conns", maxConns)
	add("pool_min_conns", minConns)
	return out, nil
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
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) RunTenantTx(ctx context.Context, tenant string, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil || strings.TrimSpace(tenant) == "" {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", tenant); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
