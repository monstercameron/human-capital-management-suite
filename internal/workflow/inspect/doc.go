// Package inspect renders the governed execution inspector's read-only
// projection over persisted workflow runtime state (owner: workflow-runtime;
// phase: P1A; WF-RUN-019).
//
// # What it is
//
// [Build] takes one [runtime.Instance], its recorded [runtime.NodeExecution]
// rows and an already-evaluated [Authorization], and returns a [View]: the
// traversal planning/specs/workflow-runtime.md "Execution Inspector and
// Deterministic Replay" describes, in the order it describes it —
//
//	definition -> instance -> node -> governance -> transaction ->
//	connector -> observation/reconciliation -> trace
//
// — plus the instance's runtime status, its five lifecycle dimensions (never
// collapsed into one) and its current frontier.
//
// # Build projects; Load reads; neither executes
//
// [Build] performs no I/O: it projects state a caller already loaded, which is
// why it is trivially safe to call concurrently. [Load] is the reader: given a
// tenant-scoped transaction, a tenant, an instance id and the authorization
// decision, it loads the durable record itself -- the instance row and its
// pinned compiled version from the durable registry (record digest and
// status), node executions, every durable timer (so retry backoffs and
// waits are rendered from the rows the timer scheduler holds, never computed
// here), work items and their transitions, the pinned execution context and
// its digest, advancement receipts re-verified against their digests, the
// instance lease history, the latest checkpoint, and the outbox rows and
// reconciliation jobs behind every recorded effect reference -- then renders
// it through [Build] and a [DurableView]. A record family no durable store in
// this repository can answer for a workflow instance (the business
// transaction behind business_transaction_id; connector operations, which no
// workflow writer links to an instance) is listed as UNAVAILABLE with its
// reason, never rendered as an empty list.
//
// Neither function writes or can change business state: an inspector that
// could act would be an intervention API, which is a different todo and a
// different authority.
//
// # Redaction is explicit, and omission is reported
//
// WF-RUN-019's RED clause has two halves, and they pull in opposite
// directions: an inspector must not omit the current node, attempt, retry,
// proposal, baseline, policy, effect or repair references, and must not leak
// protected input or output content. So references are always rendered, and
// the ones that point at protected artifacts are [Ref] values that carry
// either the reference or the reason it was withheld — never a bare empty
// string that reads as "there was nothing there".
//
// The same rule governs whole sections. A denied section is named in
// [Completeness.Redactions] rather than silently dropped, and any datum the
// projection expected but did not receive — a frontier node with no recorded
// execution, most of all — is named in [Completeness.Gaps]. A view is
// [Completeness.Complete] only when nothing was denied and nothing was
// missing, which is what stops a partial view from being read as a full one.
//
// The redaction shape follows internal/domains/intelligence's ExplainTransaction:
// an authorization decision is an input, not something this package computes;
// section and field rulings are separate; and whether the instance may be
// known to exist at all is a third, separate ruling, because the existence of
// a promotion workflow is itself information.
package inspect
