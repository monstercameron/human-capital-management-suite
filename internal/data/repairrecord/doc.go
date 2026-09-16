// Package repairrecord is WF-RUN-016's durable, tenant-scoped, append-only
// record of which RepairPlan executions already reached the corrective effect.
//
// # Why this package exists rather than a map in the executor
//
// internal/workflow/execute.RepairExecutor is the REPAIR execution mode:
// diagnose, simulate, approve, execute, observe, verify. It already refused to
// redrive the same fenced repair twice, but the fact it refused on lived in a
// process-local map. A cell restarted between the corrective effect and its
// reconciliation forgot that the effect had ever run, and the next operator
// presenting the same plan drove a payroll or IAM mutation the external system
// had already accepted. A repair runs precisely because consistency is already
// degraded, so duplicating its effect is the one failure it must never add.
//
// # The three stages, and why the claim comes first
//
// One repair fence writes at most three rows, none of which is ever rewritten
// (migrations/00311 gives the table a forbid_mutation trigger and grants only
// SELECT and INSERT):
//
//   - [StageClaimed] is appended BEFORE the effect port is called. Its
//     presence means "an attempt reached the effect boundary under this
//     fence" -- not "the effect succeeded". A process that dies immediately
//     after it leaves only this row, and the executor then refuses to re-run
//     the effect at all: the prior attempt's outcome is unknown, and an
//     unknown external mutation must be diagnosed again, never repeated.
//   - [StageExecuted] is appended once the provider accepted the redrive,
//     carrying the effect identities so a restart resumes at observe and
//     verify without touching the external system a second time.
//   - [StageSettled] is the terminal revalidation answer, so a replay of the
//     same plan returns the original decision rather than re-deciding it.
//
// Writing the claim first is what makes the guarantee "never re-run", rather
// than the weaker "usually not re-run" a success-only record would give.
//
// # The claim is a decision, not a check
//
// [Append] issues one INSERT .. ON CONFLICT DO NOTHING .. RETURNING. It is
// never a SELECT followed by a conditional INSERT: two cells racing for the
// same fence serialize on this table's own primary key at commit, so exactly
// one of them is told it won the claim and exactly one reaches the effect
// port. The other is told the row already exists and reads the history
// instead. This is the same argument migrations/00286, 00287 and 00288 make
// for their own single-statement decisions.
//
// # What is stored
//
// Execution identities only: fence, plan digest, the parent transaction's
// original semantic key, the failed effect key, and the effect, observation
// and reconciliation references, states and digests. No business payload and
// no provider response body ever reaches this table, exactly as
// internal/operations/repair's own fence store promises.
//
// Every method takes its [Executor] explicitly and the caller must already
// have scoped its transaction to a tenant (internal/data/tenancy.WithTenant):
// the table is row-level-security protected and force-enabled, so an unscoped
// statement reads and writes nothing rather than crossing tenants.
package repairrecord
