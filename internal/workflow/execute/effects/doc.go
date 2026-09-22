// Package effects supplies the two production ports
// internal/workflow/execute's own bounded driver (Options.DB/Steps/
// WorkItems/Terminal/Guard/Retention) does not itself provide an
// implementation for:
//
//   - [PolicyResolver], a data-driven runtime.WorkflowResolver: which
//     compiled workflow/pin a start request binds is looked up in a small
//     ordered table the composition root supplies, never a graph a business
//     service embeds.
//   - [LedgerTerminalWriter], an execute.TerminalWriter for a workflow
//     instance's COMPLETE continuation: it reads the instance's own
//     completed work-item decisions and invokes the promotion settlement
//     capability (internal/data/promotioncommit.Settler), which owns the
//     governed business writes -- the promotion outcome ledger event,
//     projection checkpoint and outbox message, the payload-schema
//     registration and the admission-guard and budget-hold releases --
//     inside the same transaction internal/workflow/execute's own
//     continuation sink already wraps in
//     internal/transaction/idempotency.Guard. The writer records only the
//     capability's typed result.
//
// Neither type owns a workflow graph, a clock, a goroutine or a retry loop:
// every instant is the caller's own RecordedAt, and every write happens
// exactly once inside the transaction execute's driver supplies.
package effects
