// Package pgxadapter is the single place where pgx/v5 becomes an
// [dbport] implementation (owner: data plane; LIB-004).
//
// Every other package speaks the driver-free port: it takes a [dbport.Tx] or a
// [dbport.Querier] and never names pgx in a signature. This package holds the
// driver handles - a connection, a pool, a transaction - and translates the
// three differences between pgx's shape and the port's:
//
//   - Exec returns pgx's command tag; the port returns the affected-row count,
//     which is the only field this repository reads.
//   - A single-row miss is pgx.ErrNoRows; the port's own [dbport.ErrNoRows] is
//     substituted at Scan so no caller imports pgx to recognise it.
//   - pgx's Rows already matches the port's cursor shape, so it is passed
//     through unchanged rather than re-wrapped per row.
//
// Constructors take a connection URL rather than an already-open pgx handle.
// That is deliberate: a constructor accepting *pgx.Conn would put the driver
// type back on this package's public surface and re-open the boundary this
// package exists to close.
package pgxadapter

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// row adapts a deferred pgx single-row result, substituting the port's own
// no-rows sentinel.
type row struct{ inner pgx.Row }

func (r row) Scan(dest ...any) error {
	err := r.inner.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbport.ErrNoRows
	}
	return err
}

// Tx is an open transaction on a pgx connection or pool. It satisfies
// [dbport.Tx]; the pgx transaction it holds is never handed out.
type Tx struct{ tx pgx.Tx }

// Exec implements [dbport.Execer].
func (t Tx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (t Tx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (t Tx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: t.tx.QueryRow(ctx, sql, args...)}
}

// Commit implements [dbport.Tx].
func (t Tx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

// Rollback implements [dbport.Tx]. Rolling back a transaction that already
// committed reports pgx.ErrTxClosed, which callers guarding with a deferred
// rollback ignore.
func (t Tx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// Conn is a single pgx connection exposed through the port. It is not safe for
// concurrent use; open one per goroutine, or use a [Pool].
type Conn struct{ conn *pgx.Conn }

// Connect opens one connection to url. runtimeParams are PostgreSQL run-time
// parameters set on the connection itself (search_path, for instance), which is
// how a caller pins a session to a schema without issuing a statement.
func Connect(ctx context.Context, url string, runtimeParams map[string]string) (*Conn, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for k, v := range runtimeParams {
		cfg.RuntimeParams[k] = v
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: conn}, nil
}

// Exec implements [dbport.Execer].
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := c.conn.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (c *Conn) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return c.conn.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: c.conn.QueryRow(ctx, sql, args...)}
}

// Begin implements [dbport.Beginner].
func (c *Conn) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := c.conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// BeginSerializable opens a transaction with PostgreSQL's serializable
// isolation level. It is an optional capability used by retry coordinators;
// Begin retains the driver's default for existing callers.
func (c *Conn) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	tx, err := c.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// BeginReadOnly opens an explicit read-only transaction for resolving an
// ambiguous local outcome. The server enforces that this transaction cannot
// perform writes.
func (c *Conn) BeginReadOnly(ctx context.Context) (dbport.Tx, error) {
	tx, err := c.conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// Ping verifies the connection is still usable.
func (c *Conn) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

// Close closes the connection.
func (c *Conn) Close(ctx context.Context) error { return c.conn.Close(ctx) }

// IsClosed reports whether the underlying connection is no longer usable.
// pgx closes a connection whose statement was interrupted by its context,
// so a caller that shares one connection can detect that and reconnect.
func (c *Conn) IsClosed() bool { return c.conn.IsClosed() }

// Pool is a pgx connection pool exposed through the port. Unlike [Conn] it is
// safe for concurrent use, and every Exec/Query/QueryRow acquires and releases
// a connection of its own.
type Pool struct {
	pool              *pgxpool.Pool
	hygieneFailures   *atomic.Int64
	hygieneRoundTrips *atomic.Int64
}

// hygieneSQL is the transaction-safe form of DISCARD ALL. PostgreSQL rejects
// DISCARD ALL inside a multi-statement simple-protocol message because that
// message runs in a transaction block, so its individual reset operations are
// sent together instead.
const hygieneSQL = "RESET ROLE; SET SESSION AUTHORIZATION DEFAULT; SELECT pg_advisory_unlock_all(); CLOSE ALL; RESET ALL; DEALLOCATE ALL; UNLISTEN *; DISCARD PLANS; DISCARD TEMPORARY; DISCARD SEQUENCES"

// NewPool opens a pool against url and pings it once, so a bad connection
// string or an unreachable server fails here rather than on first use.
// runtimeParams are PostgreSQL run-time parameters set on every connection the
// pool opens, which is how a caller pins the whole pool to a schema.
func NewPool(ctx context.Context, url string, runtimeParams map[string]string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for k, v := range runtimeParams {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
	// BeforeAcquire below runs DISCARD ALL, which deallocates every named
	// server-side prepared statement on the session. pgx's default mode
	// (QueryExecModeCacheStatement) prepares each distinct SQL text once per
	// connection under a generated name and remembers it client-side, so the
	// next borrower of a recycled session binds a statement the server no
	// longer has and PostgreSQL answers SQLSTATE 26000. CacheDescribe keeps
	// the extended protocol and the client-side description cache but
	// always executes through the unnamed statement, which DISCARD ALL does
	// not touch. This is the same setting pgx documents for any session that
	// is reset between uses (a connection pooler in transaction mode is the
	// usual case; here the pool's own hygiene is that reset).
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	// A pooled connection is a reusable PostgreSQL session. Reset all ambient
	// state before handing an idle session to a borrower; callers must use SET
	// LOCAL for request/tenant context. The transaction-safe components of
	// DISCARD ALL reset session state; DISCARD ALL itself cannot be included in
	// this one multi-statement message. It also does not reset an assumed role
	// or session-level advisory locks, so those are reset explicitly too.
	var hygieneFailures atomic.Int64
	var hygieneRoundTrips atomic.Int64
	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		// RESET ROLE returns from SET ROLE to the login role. The unlock-all call
		// is intentionally unconditional: unlike transaction-scoped locks,
		// session advisory locks survive a transaction and DISCARD ALL.
		// Hygiene runs through the simple protocol: it must not depend on a
		// prepared-statement cache that the discard operations here are about to
		// invalidate. Runtime parameters are sent in PostgreSQL's
		// startup packet and DISCARD ALL restores those startup values, so they
		// do not need a per-acquire set_config round trip.
		// A false result destroys the connection and retries the acquisition
		// on a new one, matching the retired BeforeAcquire semantics.
		hygieneRoundTrips.Add(1)
		if _, err := conn.Exec(ctx, hygieneSQL, pgx.QueryExecModeSimpleProtocol); err != nil {
			hygieneFailures.Add(1)
			return false, nil
		}
		return true, nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Pool{pool: pool, hygieneFailures: &hygieneFailures, hygieneRoundTrips: &hygieneRoundTrips}, nil
}

// ParsePoolBounds returns the MaxConns and MinConns that a pool built from dsn
// would use, including any pool_max_conns/pool_min_conns the DSN carries and
// pgx's defaults otherwise. It opens no connection. Callers that only need to
// validate or rewrite pool bounds use it so they never import pgxpool themselves.
func ParsePoolBounds(dsn string) (maxConns, minConns int32, err error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return 0, 0, err
	}
	return cfg.MaxConns, cfg.MinConns, nil
}

// WithConn lends one physical connection to fn and releases it afterwards.
// The next borrower receives a session cleaned by PrepareConn. fn must not
// retain conn after it returns.
func (p *Pool) WithConn(ctx context.Context, fn func(dbport.Conn) error) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return fn(&Conn{conn: conn.Conn()})
}

// Exec implements [dbport.Execer].
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// Query implements [dbport.Querier].
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow implements [dbport.Querier].
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	return row{inner: p.pool.QueryRow(ctx, sql, args...)}
}

// Begin implements [dbport.Beginner].
func (p *Pool) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// BeginSerializable opens a pooled transaction at PostgreSQL serializable
// isolation, preserving the same optional capability as Conn.
func (p *Pool) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// BeginReadOnly opens a pooled transaction with PostgreSQL read-only access.
func (p *Pool) BeginReadOnly(ctx context.Context) (dbport.Tx, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	return Tx{tx: tx}, nil
}

// Ping verifies the pool can still reach the server.
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// Close closes the pool and every connection in it.
func (p *Pool) Close() { p.pool.Close() }

// HygieneFailures reports sessions rejected while restoring trusted state.
// It carries no tenant, request, or other sensitive labels.
func (p *Pool) HygieneFailures() int64 {
	if p.hygieneFailures == nil {
		return 0
	}
	return p.hygieneFailures.Load()
}

// Saturation is a snapshot of the pool's connection accounting, reported
// without handing out pgxpool's own stat type.
type Saturation struct {
	AcquiredConns     int32
	IdleConns         int32
	MaxConns          int32
	TotalConns        int32
	HygieneRoundTrips int64
}

// Stats reports the pool's current saturation.
func (p *Pool) Stats() Saturation {
	s := p.pool.Stat()
	return Saturation{
		AcquiredConns:     s.AcquiredConns(),
		IdleConns:         s.IdleConns(),
		MaxConns:          s.MaxConns(),
		TotalConns:        s.TotalConns(),
		HygieneRoundTrips: p.hygieneRoundTrips.Load(),
	}
}

var (
	_ dbport.Tx       = Tx{}
	_ dbport.Conn     = (*Conn)(nil)
	_ dbport.Beginner = (*Conn)(nil)
	_ dbport.Conn     = (*Pool)(nil)
	_ dbport.Beginner = (*Pool)(nil)
)
