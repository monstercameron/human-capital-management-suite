// Advisory-lock governance: DB-EDGE-002 governs PostgreSQL
// advisory-lock scope, namespace and exhaustion.
//
// Approved use defaults to transaction-scoped locks
// (pg_advisory_xact_lock): rollback or pool return releases them, so a
// session lock can never leak. Keys derive from a versioned
// namespace/tenant/resource domain with a collision-tested corpus, and
// every acquisition declares an explicit order and count budget.
// Over-budget acquisition fails with a typed saturation result before
// touching the database, which is the admission response to lock
// pressure. Session-scoped locks require a separately reviewed owner
// and cleanup proof. Advisory locks coordinate; they never become
// business truth.
package uow

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Advisory namespaces partition the 64-bit lock key space by use.
// The version prefix domain-separates derivations across contract
// changes: v1 keys never equal v2 keys for the same inputs.
const (
	AdvisoryNamespacePromotion = "promote/v1"
	AdvisoryNamespaceOutbox    = "outbox/v1"
	AdvisoryNamespaceScheduler = "scheduler/v1"
)

// DeriveAdvisoryKey maps one namespace/tenant/resource triple to a
// lock key deterministically. Tenant and resource never collide across
// namespaces because the namespace is part of the digest.
func DeriveAdvisoryKey(namespace, tenant, resource string) int64 {
	sum := sha256.Sum256([]byte(namespace + "\x00" + tenant + "\x00" + resource))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// AdvisoryBudget bounds one acquisition: order is always ascending key
// order (deadlock-safe), and Count keys must fit MaxLocks.
type AdvisoryBudget struct {
	MaxLocks int
}

// LockSaturation is the typed admission response to lock pressure.
type LockSaturation struct {
	Requested int
	Allowed   int
}

// Error implements error.
func (e *LockSaturation) Error() string {
	return fmt.Sprintf("uow: advisory lock budget exhausted: requested %d, allowed %d", e.Requested, e.Allowed)
}

// AsLockSaturation unwraps a budget refusal.
func AsLockSaturation(err error) (*LockSaturation, bool) {
	if err == nil {
		return nil, false
	}
	if saturation, ok := err.(*LockSaturation); ok {
		return saturation, true
	}
	return nil, false
}

// orderKeys deduplicates and sorts ascending for deadlock-safe order.
func orderKeys(keys []int64) []int64 {
	seen := map[int64]bool{}
	ordered := make([]int64, 0, len(keys))
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			ordered = append(ordered, key)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered
}

// checkBudget enforces the explicit acquisition count budget before
// any SQL evaluates, so pressure fails closed instead of exhausting
// shared memory.
func checkBudget(keys []int64, budget AdvisoryBudget) error {
	if budget.MaxLocks <= 0 {
		return &LockSaturation{Requested: len(keys), Allowed: 0}
	}
	if len(keys) > budget.MaxLocks {
		return &LockSaturation{Requested: len(keys), Allowed: budget.MaxLocks}
	}
	return nil
}

// AcquireXact takes transaction-scoped locks in ascending key order.
// They release on commit, rollback or pool return: no cleanup proof is
// needed because the scope cannot leak.
func AcquireXact(ctx context.Context, tx dbport.Tx, keys []int64, budget AdvisoryBudget) (int, error) {
	ordered := orderKeys(keys)
	if err := checkBudget(ordered, budget); err != nil {
		return 0, err
	}
	for _, key := range ordered {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return 0, err
		}
	}
	return len(ordered), nil
}

// SessionLockPermit is the separately reviewed authorization a
// session-scoped lock requires.
type SessionLockPermit struct {
	Owner        string
	CleanupProof string
}

// AcquireSession takes one session-scoped lock under a reviewed
// permit. The caller owns cleanup: ReleaseSession must run before the
// connection returns to the pool, and the permit's cleanup proof names
// how.
func AcquireSession(ctx context.Context, conn dbport.Conn, key int64, permit SessionLockPermit) error {
	if strings.TrimSpace(permit.Owner) == "" || strings.TrimSpace(permit.CleanupProof) == "" {
		return fmt.Errorf("uow: session advisory lock requires a reviewed owner and cleanup proof")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		return err
	}
	return nil
}

// ReleaseSession releases one session lock. Holding checks run through
// pg_advisory_unlock, which reports whether the lock was held: an
// unbalanced release is an error, never hidden.
func ReleaseSession(ctx context.Context, conn dbport.Conn, key int64) error {
	var released bool
	if err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&released); err != nil {
		return err
	}
	if !released {
		return fmt.Errorf("uow: unbalanced advisory release for key %d", key)
	}
	return nil
}

// SessionLocksHeld reports whether any session lock is held on this
// connection. Pool return with a true result is a leak.
func SessionLocksHeld(ctx context.Context, conn dbport.Conn) (bool, error) {
	var count int64
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid()`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
