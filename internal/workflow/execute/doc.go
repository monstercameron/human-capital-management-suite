// Package execute contains the bounded, caller-driven workflow driver used by
// the prototype. Resume accepts an already-completed WorkItem plus the typed
// APPROVAL or TASK resolution derived from it; it revalidates the exact active
// plan and all material bindings before advancing the parked node.
//
// It is deliberately not a scheduler. Execute starts one immutable proposal,
// runs READY nodes synchronously through an injected StepRunner, and stops as
// soon as durable human work is required. It owns no lease, timer, retry or
// background polling mechanism. Every runtime advancement and every
// continuation it derives are committed in one caller-owned database
// transaction.
//
// Human work is created through WorkItemFactory. The driver supplies a stable
// work-item identity derived from the runtime continuation, so a factory never
// needs process-local deduplication. Timer and signal continuations park the
// instance through the optional TimerFactory and SignalSubscriber ports; a
// driver composed without the matching port refuses them. The runtime audit
// insert is attempted first, but the refusal makes the driver's transaction
// roll back, so neither that insert nor any partial node/instance transition
// becomes durable.
//
// At END, the continuation sink runs TerminalWriter inside TX-006's semantic
// idempotency guard. The writer must keep its ledger, projection and outbox
// writes inside the supplied transaction; this package provides orchestration,
// not a second implementation of those data-plane contracts.
package execute
