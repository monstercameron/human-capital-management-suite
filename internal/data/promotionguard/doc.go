// Package promotionguard is PROMOUX-002's active-intent guard: the one
// database-enforced admission control that lets exactly one promotion exist
// for a given worker and overlapping effective window.
//
// # Why this package exists rather than a check in application code
//
// The live-audit defect PROMOUX-002 closes was two concurrent "Start
// Promotion" calls for the same worker, each of which read "does a
// nonterminal promotion already exist for them?", saw no, and then wrote a
// new one. Moving that read closer to the write narrows the race window but
// can never close it: two transactions can always both complete the read
// before either commits the write, no matter how little code sits between
// them. The only way to make "exactly one wins" true under real concurrency
// is to make the decision and the write the same atomic statement, evaluated
// by the database as part of committing it. [Admit] is that statement.
//
// # The guarantee, precisely
//
// migrations/00286 declares promotion_active_intent_guard_one_active_window,
// a partial UNIQUE index on (tenant_id, worker_ref, effective_date) WHERE
// status = 'ACTIVE'. [Admit] never performs a bare SELECT to decide whether
// to insert: it issues one INSERT .. ON CONFLICT .. DO UPDATE .. RETURNING
// statement that is itself the admission decision. PostgreSQL evaluates the
// unique index as part of that statement's own commit, so of two genuinely
// concurrent Admit calls for the same (tenant, worker, effective date), the
// database -- not this package's control flow -- decides which one's INSERT
// lands first; the second's ON CONFLICT clause fires against a row the first
// one just created, not against a state either caller read in advance.
//
// # Replays versus conflicts
//
// A caller retrying its own request (a double-click, a network retry, a
// resumed client) presents the same idempotency key it used the first time.
// [Admit] treats that as a replay: the ON CONFLICT DO UPDATE clause matches
// only when the conflicting row's idempotency_key equals the caller's own,
// and when it matches, [Admit] reports the existing reservation (and its
// confirmed intent, once one exists) rather than refusing. A second, later
// caller proposing a genuinely different promotion for the same worker and
// date presents a different idempotency key; the DO UPDATE's WHERE clause
// then matches nothing, no row is touched, and [Admit] reports a conflict.
// One SQL statement answers both questions -- "do I already own this slot"
// and "is it free" -- because splitting them back into a read followed by a
// write would reopen exactly the race this package exists to close.
//
// # The two-phase reservation
//
// A promotion's real intent id does not exist yet at the moment admission
// must be decided: internal/intent's identifiers are minted inside
// CreateIntent, which this package's caller (internal/intent/app) invokes
// only after [Admit] succeeds. So [Admit] reserves the slot under the
// caller's idempotency key with intent_id left NULL, and the caller invokes
// [Confirm] once CreateIntent returns the real id. A process that crashes
// between the two leaves a reservation with no intent_id, which is a safe,
// recoverable state: the same caller's retry presents the same idempotency
// key, [Admit] reports the same reservation (Replay=true, IntentID still
// zero) rather than creating a second one, and the retry proceeds to call
// CreateIntent and [Confirm] again. See [Admit]'s TestTodo_PROMOUX_002_Recovery
// for the exact sequence this proves.
//
// # What this package does not do
//
// It does not decide when a promotion is done. The promotion terminal writer
// calls [Release] in the same transaction as the terminal ledger/outbox fact;
// this package only applies the tenant-scoped guarded state transition. A
// different caller that reaches a terminal stage must make the same explicit
// release decision at its own authoritative commit boundary.
package promotionguard
