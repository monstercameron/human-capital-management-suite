// DURABLE-002 record: the queue model. One durable work queue kept in the
// PostgreSQL databases the product already runs, compared against an
// external message broker and a durable-execution engine, with what each
// costs to operate, what each guarantees, what the product's separated
// databases mean for a job spanning two of them, the product's own workflow
// engine as the candidate for long agent processes, and the recommended
// single queue contract the existing outboxes can adopt without a rewrite.
// Grounded by code reading on 2026-10-22; file references name the evidence.
// The PERFORMANCE matrix test measures a prototype of the recommended
// contract at load with workers killed at random; the SECURITY matrix test
// proves reference-only payloads and replay-time authority rechecks.
package agenttriggerstore

import (
	"fmt"
	"strings"
)

// DurableQueueOption is one compared queue substrate.
type DurableQueueOption struct {
	ID         string
	Title      string
	Guarantees string
	Cost       string
	Verdict    string
}

// DurableQueueOp is one operation of the recommended queue contract.
type DurableQueueOp struct {
	Name string
	Rule string
}

// DurableQueueAdoption maps one existing outbox onto the recommended
// contract without a rewrite.
type DurableQueueAdoption struct {
	Outbox  string
	Mapping string
}

// DurableQueueRecord is the checked-in comparison and recommendation.
type DurableQueueRecord struct {
	Options        []DurableQueueOption
	Recommended    string
	Contract       []DurableQueueOp
	Adoption       []DurableQueueAdoption
	CrossDatabase  string
	WorkflowEngine string
}

// DurableQueueModel returns the queue-model record.
func DurableQueueModel() DurableQueueRecord {
	return DurableQueueRecord{
		Options: []DurableQueueOption{
			{
				ID:    "postgres",
				Title: "A queue kept in PostgreSQL beside the data it acts on",
				Guarantees: "Enqueue commits in the same transaction as the state change it acts on, so " +
					"a job can never exist without its cause or vice versa. Claim takes a batch under " +
					"FOR UPDATE SKIP LOCKED with a lease (visibility timeout) and a heartbeat that extends " +
					"it; every write carries a fencing token, so a worker that lost its lease cannot write. " +
					"Retries are bounded with backoff, then the job rests in a dead-letter state a person " +
					"can see, replay and discard. Ordering holds per key, tenants get fairness plus limits, " +
					"and row-level security keeps a job from ever being claimed across tenants. Delivery is " +
					"at-least-once with idempotent handlers: duplicates are expected and absorbed by keys, " +
					"never by hoping the network delivers once. The building blocks already exist in-tree: " +
					"the core outbox consumer (internal/data/outbox/consumer.go: batch 32, 30s lease, " +
					"SKIP LOCKED claim, ABANDONED poison state), document_index_job (nearest to this " +
					"contract: same-tx enqueue, SKIP LOCKED claim with expired-lease reclaim, attempts, " +
					"next_attempt_at), chatrender_job (lease_token fence, attempts capped at 3), and the " +
					"workflow_lease monotonic fence (migrations/00026).",
				Cost: "No new infrastructure: PostgreSQL already runs in production, local-dev and the " +
					"embedded test cache, with backups, monitoring and tenant RLS the product already " +
					"operates. Throughput is bounded by hot claim rows; the prototype measures it instead " +
					"of asserting it (see the PERFORMANCE test). Cross-database work stays an " +
					"outbox-here/admit-there pair, never a distributed transaction.",
				Verdict: "RECOMMENDED: the only option that removes machinery instead of adding it.",
			},
			{
				ID:    "broker",
				Title: "An external message broker",
				Guarantees: "High throughput, ordering per partition, retention with replay. But enqueue " +
					"cannot join the PostgreSQL transaction that changed the state, so every producer keeps " +
					"its outbox and gains a relay to the broker: the dual-write problem moves, it does not " +
					"leave. Redelivery still makes it at-least-once in practice, so consumers need the same " +
					"idempotency keys the postgres queue needs.",
				Cost: "A new stateful system to run, secure, back up, tenant-isolate and teach to " +
					"local-dev, CI and the embedded-test story. The RLS equivalent must be rebuilt as " +
					"broker ACLs per tenant, and the relay itself needs the leases, fences and dead-letter " +
					"handling this record specifies. Operating cost is permanent; the outbox complexity it " +
					"was meant to remove stays.",
				Verdict: "REJECTED: pays for a second durable system while keeping the outbox problem.",
			},
			{
				ID:    "durable-execution",
				Title: "Adopting a durable-execution engine",
				Guarantees: "Claimed exactly-once execution through event-sourced replay, durable timers " +
					"and sleeps, versioned workflows. The claim holds only inside the engine's own " +
					"sandbox: every call to the product's databases, the model, or chat is still an " +
					"at-least-once effect needing the same idempotency keys.",
				Cost: "Heaviest of the three: a new control plane plus workers plus version discipline, " +
					"and every existing outbox rewritten as workflows, violating the no-rewrite constraint. " +
					"Worse, the product already owns a workflow engine (internal/workflow) with leases, " +
					"fences, timers, checkpoints and replay; adopting an external one duplicates it instead " +
					"of driving it.",
				Verdict: "REJECTED as the queue. The product's own workflow engine remains the candidate " +
					"for LONG processes (see below); the queue drives it, it does not replace it.",
			},
		},
		Recommended: "postgres",
		Contract: []DurableQueueOp{
			{Name: "enqueue", Rule: "Insert the job in the same transaction as the state change. Payload carries references and digests only, never content or credentials."},
			{Name: "claim", Rule: "Take a batch FOR UPDATE SKIP LOCKED over due rows (queued, or leased past visibility timeout), rotate tenants for fairness within per-tenant limits, set owner + lease + fence+1 + attempts+1. RLS scopes every claim to its tenant."},
			{Name: "heartbeat", Rule: "The lease holder extends visibility with its fencing token; a lost lease cannot extend and the job becomes reclaimable when visibility expires."},
			{Name: "complete", Rule: "Fenced write: owner + fence must match or the completion is refused, so a worker that lost its lease cannot complete. Authority holds from claim; replay rechecks it."},
			{Name: "fail", Rule: "Bounded retries with backoff (next attempt not before now + backoff); past the bound the job rests dead-lettered with its reason, visible to a person."},
			{Name: "dead-letter", Rule: "A dead job stays readable with attempts, last error and payload refs; an authorized operator may replay it (authority rechecked) or discard it with a reason."},
			{Name: "replay", Rule: "Re-queue a dead job for immediate delivery. Replay re-checks authority at execution time: a revoked grant fails the replay closed."},
		},
		Adoption: []DurableQueueAdoption{
			{Outbox: "core outbox (migrations/00006)", Mapping: "enqueue=insert PENDING; claim=Consumer.Poll; heartbeat=new (30s IN_FLIGHT becomes visibility); complete=DELIVERED; fail=FAILED with attempts; dead-letter=ABANDONED surfaced; replay=new; criticality maps to priority, ordering_key stays."},
			{Outbox: "chat_outbox + cursor (chatstore)", Mapping: "Cursor pull stays a valid claim variant where consumers never compete (cursor = lease held by one reader); competing consumers add the fenced claim. Retention prune stays."},
			{Outbox: "agent_schedule outbox (agentstore 00025)", Mapping: "enqueue=AppendWindow; claim=Pending + fence; complete=Acknowledge with digest verify; fail=stays pending becomes bounded fail; dead-letter=new surfacing; replay=re-plan."},
			{Outbox: "agent_run_request + execution (agentstore 00003/00005)", Mapping: "enqueue=admit (UNIQUE tenant/kind/key-digest already); claim=run Claim with fence already; heartbeat=agentrun_recovery_lease already; complete=terminal checkpoint; dead-letter=NEEDS_REPAIR surfaced."},
			{Outbox: "agent_outbox cross-db (agentstore 00004)", Mapping: "Stays the cross-database leg: enqueue-here + idempotent crash reconcile; the receiving database admits through its own enqueue. Per-destination receipts stay."},
			{Outbox: "document_index_job (documenthubstore 00019)", Mapping: "Nearest to the contract already: add heartbeat (2m lease becomes visibility) and dead-letter surfacing for failed; lost becomes a persisted dead-letter reason."},
			{Outbox: "chatrender_job (chatstore 00024)", Mapping: "lease_token IS the fence: claim=ClaimRendering, complete=CompleteRendering (already requires the live token), fail=failed at 3 attempts; add replay."},
			{Outbox: "time_outbox (timestore 00005)", Mapping: "Sequence-cursor pull stays a valid claim variant; competing consumers add the fenced claim."},
			{Outbox: "project_outbox (projectstore 00009)", Mapping: "Bounded pull (100/batch, caller-held cursor) maps to claim with batch=100; add fenced complete + dead-letter."},
			{Outbox: "workflow_ready_work + workflow_timer (00026)", Mapping: "Claim/fire/expire stay caller-driven per the WF-RUN-000 gate; the queue worker becomes a caller, holding workflow_lease fences per advance. Digests stay the dedupe keys."},
			{Outbox: "jobs partitions (internal/data/jobs)", Mapping: "Claim/reclaim-after-TTL + watermark resume already; heartbeat replaces the executor-local in-memory lease manager; checkpoints stay."},
			{Outbox: "delivery_attempt (00031)", Mapping: "Idempotency-keyed attempts map to claim/complete; AMBIGUOUS rests as dead-letter-unknown until the receiver resolves it."},
		},
		CrossDatabase: "Core, chat, agents and documents (plus project, work-order, time and authority) are " +
			"separate databases and pools by design: ServeConfig carries a DatabaseURL per plane, the agent " +
			"database must differ from core/chat/documents, and pools are never shared " +
			"(internal/application/agent_database.go, config.go). There is no shared transaction across " +
			"planes, so each plane keeps its own queue table beside the data it acts on and same-tx " +
			"enqueue is preserved everywhere. A job spanning two planes is two jobs plus a re-admission: " +
			"outbox-here/admit-there with source-key idempotency, the agent_run_request " +
			"UNIQUE(tenant, kind, key-digest) pattern, with recovery re-admitting after CheckRequest " +
			"(agent_schedule_recovery.go). The RLS setting differs per plane (app.tenant_id vs " +
			"hcmnext.tenant_id) and is re-established on every hop. Cost of the alternative (one shared " +
			"queue database): every enqueue becomes a cross-database dual-write, which is exactly the " +
			"failure mode the postgres option removes.",
		WorkflowEngine: "The real engine is internal/workflow, not internal/flow (form-flow governance) and " +
			"not internal/engines (pure in-memory rules). It gives long agent processes: durable timers as " +
			"rows with fenced Fire + double CAS and digest-pinned provenance (a reminder due in three days " +
			"is a stored fact, not sleeping code); monotonic-fence leases with verified fenced writes; " +
			"3-transaction takeover recovery with idempotent dispositions; record-only replay that refuses " +
			"on trace divergence; pause/continuation, append-only checkpoints, version migration, signals, " +
			"and work items with SLA + approvals. It does NOT give any scheduler, ticker, reaper or worker " +
			"loop: the WF-RUN-000 gate leaves all firing, claiming and expiry caller-driven. Division of " +
			"labor: short jobs run on the queue; long multi-wait processes run as engine instances DRIVEN " +
			"by queue deliveries; engine nodes never sleep in worker memory and workers never own engine " +
			"state.",
	}
}

// RenderDurableQueueModel renders the record as text.
func RenderDurableQueueModel() string {
	var out strings.Builder
	record := DurableQueueModel()
	out.WriteString("DURABLE-002 queue-model record (code-read 2026-10-22)\n")
	out.WriteString("Depends on the DURABLE-001 inventory; feeds DURABLE-003 continuity and the DURABLE-004 proof harness.\n\n")
	for _, option := range record.Options {
		fmt.Fprintf(&out, "## option %s: %s\n", option.ID, option.Title)
		fmt.Fprintf(&out, "guarantees: %s\n", option.Guarantees)
		fmt.Fprintf(&out, "cost: %s\n", option.Cost)
		fmt.Fprintf(&out, "verdict: %s\n\n", option.Verdict)
	}
	fmt.Fprintf(&out, "recommended: %s\n\n", record.Recommended)
	out.WriteString("## contract\n")
	for _, op := range record.Contract {
		fmt.Fprintf(&out, "- %s: %s\n", op.Name, op.Rule)
	}
	out.WriteString("\n## adoption (no rewrite)\n")
	for _, row := range record.Adoption {
		fmt.Fprintf(&out, "- %s => %s\n", row.Outbox, row.Mapping)
	}
	fmt.Fprintf(&out, "\n## cross-database\n%s\n\n## workflow engine\n%s\n", record.CrossDatabase, record.WorkflowEngine)
	return out.String()
}
