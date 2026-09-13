// Package promotioninvalidation is PROMOUX-011's durable ordering primitive:
// the one database-enforced counter that gives every committed promotion
// transition a distinct, monotonically increasing position for one tenant's
// one invalidation projection, before any per-viewer authority filtering or
// per-subscriber renumbering ever sees it.
//
// # Why a database counter, not an in-memory one
//
// A promotion transition can be committed by any of several concurrent
// server processes and goroutines. If two of them minted the "next" position
// by reading the current value and writing back one more, two concurrent
// committers could both read the same value and hand out the same position
// to two different transitions -- exactly the "lost or reordered
// invalidation" TestTodo_PROMOUX_011_Race exists to rule out. [NextSequence]
// is never a SELECT followed by a conditional write: it is one
// INSERT .. ON CONFLICT .. DO UPDATE .. RETURNING statement, the same shape
// internal/data/promotionguard and internal/data/positionguard already use,
// so PostgreSQL's own row-level locking during that statement's commit is
// what serializes concurrent callers, not this package's control flow.
//
// # What this position is used for, and what it is not
//
// The returned value becomes one committed transition's global ordering
// position -- durable, gapless within one (tenant, projection) counter, and
// safe to use as a productquery.InvalidationItem's Revision so a client that
// receives messages out of order can refuse a stale one
// (tools/uxqual/invalidation's admissionRevisions check). It is deliberately
// never placed on the wire as a message's SourceSequence: doing that would
// let an authority-filtered viewer infer a skipped transition's existence
// from a gap in what they receive, which is the exact disclosure bug
// PROMOUX-011's clause on sequence numbers names.
// internal/domains/promotion.SubscriberSequencer is what a subscriber's
// wire-visible sequence actually comes from, and it never reads this table.
package promotioninvalidation
