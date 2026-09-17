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
package productdurability
