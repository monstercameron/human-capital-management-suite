// Package dbport declares the transaction and query port every data-owning
// package speaks, with no PostgreSQL driver type anywhere in it (owner: data
// plane; LIB-004, ARCH-GO-012).
//
// # Why the port exists
//
// The append path, the projection applier, the outbox and the aggregate
// repositories all need the same four capabilities: run a statement, read many
// rows, read one row, and be bound to a transaction the caller owns. Those are
// database capabilities, not pgx capabilities. Naming pgx.Tx, pgx.Rows or
// pgx.Conn in a signature makes every caller - including the semantic ports in
// internal/ledger, which must stay independent of PostgreSQL - import the
// driver just to say what it needs.
//
// So the capabilities are declared here as ordinary Go interfaces over
// context, strings and any. internal/data/pgxadapter is the one package that
// knows pgx exists; it wraps a pgx connection, transaction or pool to satisfy
// these interfaces. A package that only ever speaks this port can be exercised
// against any implementation, and the driver stays replaceable at exactly one
// seam.
//
// # What is deliberately absent
//
// There is no Begin on Tx (no nested transactions or savepoints are used), no
// batch, no copy-from and no connection lifecycle. Those are adapter
// capabilities: a caller that genuinely needs one reaches for the adapter's own
// type at a composition root rather than widening this port for everyone.
package dbport

import (
	"context"
	"errors"
)

// ContextWithTx carries a transaction across adapter boundaries. The caller
// still owns its lifetime; consumers must never commit or roll it back.
func ContextWithTx(ctx context.Context, tx Tx) context.Context {
	return context.WithValue(ctx, transactionContextKey{}, tx)
}

// TxFromContext returns a caller-owned transaction carried by ContextWithTx.
func TxFromContext(ctx context.Context) (Tx, bool) {
	tx, ok := ctx.Value(transactionContextKey{}).(Tx)
	return tx, ok && tx != nil
}

type transactionContextKey struct{}

// ErrNoRows is what [Row.Scan] returns when a single-row query selected
// nothing. It carries pgx's own wording so an error string logged before this
// port existed reads identically after it, but it is this package's sentinel:
// callers test it with errors.Is and never import the driver to do so.
var ErrNoRows = errors.New("no rows in result set")

// Row is one deferred single-row result. Scan reports [ErrNoRows] when the
// query matched nothing; any other error is the query's own.
type Row interface {
	Scan(dest ...any) error
}

// Rows is a forward-only cursor over a multi-row result. The usual shape is
// Query, defer Close, loop on Next, then check Err - Next reports false both at
// the end of the result and on failure, so Err is what distinguishes them.
type Rows interface {
	// Next advances to the next row, reporting false at the end of the result
	// or after a failure.
	Next() bool
	// Scan reads the current row into dest.
	Scan(dest ...any) error
	// Err returns the failure that ended the iteration, if any.
	Err() error
	// Close releases the cursor. It is safe to call more than once.
	Close()
}

// Execer runs a statement that returns no rows and reports how many rows it
// affected. The count is returned directly rather than a driver command tag:
// the row count is the only part of a command tag this repository reads, and
// returning it plainly keeps the driver's type off every caller's signature.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (rowsAffected int64, err error)
}

// Querier reads. Query streams a result the caller must close; QueryRow defers
// both the query and its failure to Scan.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// Conn is a handle that can both write and read without the caller holding a
// transaction. A single statement needs nothing more; a caller that issues two
// statements which must succeed or fail together needs a [Tx] instead, because
// on a bare connection each statement commits on its own.
type Conn interface {
	Execer
	Querier
}

// Tx is a transaction the caller opened and is responsible for finishing.
// Every statement run through it is part of that transaction, and nothing in
// this repository commits on a caller's behalf.
type Tx interface {
	Execer
	Querier
	// Commit makes the transaction's work durable.
	Commit(ctx context.Context) error
	// Rollback discards it. Rolling back an already-finished transaction is
	// not an error, so `defer tx.Rollback(ctx)` is the correct guard.
	Rollback(ctx context.Context) error
}

// Beginner opens transactions. A pool and a connection both satisfy it.
type Beginner interface {
	Begin(ctx context.Context) (Tx, error)
}
