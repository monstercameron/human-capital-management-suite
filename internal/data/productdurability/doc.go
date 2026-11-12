// Package productdurability owns the storage invariants every product
// record obeys: append-oriented material history, half-open effective
// intervals, exact monetary storage, optimistic concurrency on mutable
// coordination rows, atomic ledger projection with outbox persistence,
// storage classification and encryption policy, retention and disposition,
// and projection rebuild from the authoritative chronology.
//
// The package is deliberately mechanical: it holds no domain authority of
// its own. Tenants scope every structure, every mutation is validated
// before it lands, and every state carries a content digest so a tampered
// record is detectable without trusting the store.
//
// This is a design-reference package with no production claim. Nothing in the
// binaries (cmd/hcmnext, cmd/worker, cmd/projector, cmd/scheduler) imports it,
// and it has no PostgreSQL storage. The wired proof of these invariants against
// internal/data/ledger, internal/data/outbox, internal/data/rebuild and
// internal/data/bitemporal lives in internal/data/rebuild/rev08501_wired_test.go
// (TestTodo_REV_085_01).
package productdurability
