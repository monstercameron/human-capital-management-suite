// DURABLE-001 inventory: every multi-step process and what happens when
// the program dies inside it. Each DurableProcess is one row, each
// DurableStep one sub-row answering GREEN's questions: where its state
// lives, what makes it durable, its idempotency key, who resumes it and
// when, the worst case on death immediately before, during and after it,
// whether a person can tell, and the time to resume. Grounded by code
// reading on 2026-10-22; file references name the evidence.
package runstate

import (
	"fmt"
	"strings"
)

// DurabilityNone marks a step nothing makes durable: no transaction, outbox
// row, lease, journal entry, checkpoint or cursor covers it.
const DurabilityNone = "none"

// DurableStep is one sub-row of the inventory.
type DurableStep struct {
	Name           string
	State          string
	Durability     string
	IdempotencyKey string
	Resumer        string
	DieBefore      string
	DieDuring      string
	DieAfter       string
	Visible        string
	ResumeTime     string
}

// DurableProcess is one inventory row: a multi-step process.
type DurableProcess struct {
	ID    string
	Title string
	Steps []DurableStep
}

// DurableFinding is one "nothing does", "stuck" or "repeated" verdict with
// the todo that would fix it.
type DurableFinding struct {
	ID      string
	Process string
	Step    string
	Symptom string
	FixTodo string
}

// DurableInventory returns one row per multi-step process.
func DurableInventory() []DurableProcess {
	return []DurableProcess{
		{
			ID:    "mention-answer",
			Title: "Answering a mention (admission, claim, model call, checkpoint, tool effect, continuation, validation, delivery, receipt)",
			Steps: []DurableStep{
				{
					Name: "post", State: "chat_post + chat_idempotency + outbox row in one transaction (internal/data/chatstore/store.go)",
					Durability: "transaction", IdempotencyKey: "(tenant, conversation, client key)",
					Resumer:   "sender retry before commit; nothing after (detached in-memory goroutine, persona_chat_invocation.go)",
					DieBefore: "nothing written; sender retries", DieDuring: "rollback; sender retries",
					DieAfter:   "post-commit goroutine dies before start: mention LOST, no re-trigger; post visible, answer never arrives, no card",
					Visible:    "post visible; missing answer indistinguishable from slow answer until recovery card",
					ResumeTime: "none found (recovery scans persona_invocations only, never the outbox gap)",
				},
				{
					Name: "admission", State: "immutable agent_run_request row (admission_repository.go)",
					Durability: "transaction", IdempotencyKey: "arreq_sha256(tenant, kind, key); same digest replays, changed payload refuses with conflict",
					Resumer:   "dispatcher re-reads and re-verifies authority (persona_background_runtime_dispatch.go)",
					DieBefore: "claim outcome stands", DieDuring: "rollback; claim redrives admission",
					DieAfter: "duplicate returns the same record; refused admission is terminal",
					Visible:  "ADMISSION_REFUSED card, or the run proceeds", ResumeTime: "1s admission poll",
				},
				{
					Name: "claim", State: "persona_invocations CLAIMED row, then grant column, separate transactions under FOR UPDATE (agentinvocationstore/store.go, 60s bound)",
					Durability: "transaction", IdempotencyKey: "pinv_sha256(tenant, conversation, post, persona[, attempt])",
					Resumer:   "recovery finalizes never-admitted claims as ANSWER_INTERRUPTED after 3 minutes (agentrun_recovery_dispatch.go)",
					DieBefore: "lost; surfaced as interrupted card", DieDuring: "lost; surfaced as interrupted card",
					DieAfter: "durable claim drives admission",
					Visible:  "yes: retryable failure card keyed (tenant, invoker, post)", ResumeTime: "3 minutes + poll/sweep",
				},
				{
					Name: "model call", State: "no row; one in-memory 50ms transient retry with a DISTINCT step id, deliberately not provider-idempotent (agentux_answer_retry.go); spend unjournaled",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (retry mints a new step id)",
					Resumer:        "recovery: interrupts if spend occurred, else re-executes once (agentrun_recovery_dispatch.go)",
					DieBefore:      "re-runnable; nothing spent", DieDuring: "paid but unjournaled spend; ANSWER_INTERRUPTED, person asks again",
					DieAfter: "before the checkpoint lands: same as during",
					Visible:  "yes: interrupted card; lost-call cost surfacing ungrounded", ResumeTime: "lease TTL + poll/sweep",
				},
				{
					Name: "checkpoint", State: "agent_run_execution row + ADMISSION checkpoint in one transaction (internal/data/agentrunstate/store.go); run id equals admission id",
					Durability: "transaction", IdempotencyKey: "run id = admission id; duplicate create returns the row after a digest check",
					Resumer:   "wake creates the run if missing; admitted-without-run waits while the deadline is future, else interrupts",
					DieBefore: "per model call", DieDuring: "rollback; admission redrives",
					DieAfter: "durable READY run",
					Visible:  "answer or card only; READY spinner ungrounded (step board is in-memory)", ResumeTime: "poll + sweep",
				},
				{
					Name: "run-claim", State: "RUNNING + fence + lease set by CAS with an authority recheck (run.go Claim)",
					Durability: "lease + transaction", IdempotencyKey: "worker lease; second worker is refused Busy",
					Resumer:   "wake drives Claim after Recover; refuses when the model may already have started",
					DieBefore: "READY; picked up", DieDuring: "READY; picked up",
					DieAfter: "lease expiry (TTL at most 900s, 60s local) returns the run to READY, or FAILED",
					Visible:  "spinner until expiry", ResumeTime: "TTL + poll/sweep",
				},
				{
					Name: "tool effect", State: "effect journal: UNKNOWN intent recorded BEFORE the call, terminal-only resolution, delivery blocked while UNKNOWN (run.go)",
					Durability: "journal entry", IdempotencyKey: "persona-tool-idem-<hex16> = sha256(run, step, proposal); duplicate id/key refused",
					Resumer:   "owner ReconcileEffect with receiver evidence; persona recovery REFUSES to interrupt UNKNOWN runs and no persona reconcile caller was found",
					DieBefore: "clean; nothing recorded", DieDuring: "STUCK in RECONCILING, unbounded, no surfacing",
					DieAfter: "before resolution: same as during",
					Visible:  "partial at best; WAITING/RECONCILING left to the owner", ResumeTime: "unbounded; manual/owner only",
				},
				{
					Name: "continuation", State: "second model turn, same shape as model call (persona_run_executor.go); effect already resolved so recovery may interrupt",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (new step id per attempt)",
					Resumer:        "recovery interrupts or re-executes once",
					DieBefore:      "re-runnable", DieDuring: "paid but unjournaled spend; ANSWER_INTERRUPTED",
					DieAfter: "before the next checkpoint: same as during",
					Visible:  "yes: interrupted card", ResumeTime: "lease TTL + poll/sweep",
				},
				{
					Name: "validation", State: "persona_final_outputs row (ON CONFLICT DO NOTHING) then VALIDATION checkpoint; the two writes are NOT atomic (persona_run_executor.go, final_output_store.go)",
					Durability: "transaction", IdempotencyKey: "output_id unique; digest mismatch refused",
					Resumer:   "wake recovers the output row, backfills the checkpoint, delivers without re-running the model",
					DieBefore: "per model call", DieDuring: "orphaned output until wake backfills, then delivered once",
					DieAfter: "durable validated output",
					Visible:  "OUTPUT_REJECTED card on rejection", ResumeTime: "TTL + poll/sweep",
				},
				{
					Name: "delivery", State: "three separate writes: fenced chat commit, persona_reply_receipt (ON CONFLICT DO NOTHING, 5s budget), DELIVERY checkpoint to COMPLETED",
					Durability: "transaction", IdempotencyKey: "chat client_key = admission id; receipt (tenant, invocation)",
					Resumer:   "wake redelivers; same key returns the same post",
					DieBefore: "per earlier steps", DieDuring: "commit reached or not; receipt decides",
					DieAfter: "after commit before receipt: ORPHAN VISIBLE ANSWER with a later interrupted card possible; after receipt before checkpoint: exactly-once visible; then COMPLETED",
					Visible:  "yes; attribution needs the receipt", ResumeTime: "TTL + poll/sweep",
				},
				{
					Name: "receipt", State: "persona_reply_receipt row (reply_receipt.go)",
					Durability: "transaction", IdempotencyKey: "(tenant, invocation) first-wins",
					Resumer:   "wake re-receipts; replays return the run",
					DieBefore: "delivery outcome stands", DieDuring: "rollback; wake retries",
					DieAfter: "durable; attribution provable",
					Visible:  "yes", ResumeTime: "TTL + poll/sweep",
				},
			},
		},
		{
			ID:    "agents-page-task",
			Title: "A task on the Agents page",
			Steps: []DurableStep{
				{
					Name: "create", State: "agent_task + events in one transaction; duplicate id refused (agentrunstore/store.go)",
					Durability: "transaction", IdempotencyKey: "none: id is random agt_+16B per attempt, retries make DIFFERENT tasks",
					Resumer:   "user confirms long tasks; quick tasks confirm immediately; nothing auto-confirms",
					DieBefore: "nothing", DieDuring: "rollback",
					DieAfter: "before confirm (quick path): possibly STUCK UNCONFIRMED; the tick drives RUNNING+confirmed only",
					Visible:  "yes: task row", ResumeTime: "user action; quick drives synchronously to 10s, then detached under a 3-minute bound",
				},
				{
					Name: "drive", State: "CAS claim (step RUNNING + WorkerLease + version+1), unjournaled execute, separate finish transaction (uxblind_a5_runtime.go)",
					Durability: "lease + transaction", IdempotencyKey: "worker lease; concurrent Drive returns the row as-is",
					Resumer:   "TickTenant RecoverStale reaps RUNNING leases older than 5 minutes: tier<T2 to PENDING, tier>=T2 PAUSED AMBIGUOUS_EFFECT for owner ReconcileEffect; drives at most 64 tasks x 32 steps",
					DieBefore: "PENDING; tick drives", DieDuring: "after 5 minutes re-runnable, or paused for owner reconcile when effect state is ambiguous",
					DieAfter: "durable ledger append",
					Visible:  "yes, with FailureCode", ResumeTime: "5 minutes + tick (period ungrounded)",
				},
				{
					Name: "park-wake", State: "wake + last_wake_event columns; Park clears the lease; ClaimWake dedupes by event id; approvals carry a 24h StaleAfter",
					Durability: "transaction", IdempotencyKey: "timer:<task>:<version>:<due> and wake event ids",
					Resumer:   "tick delivers due wakes and completes from durable LastWake; sweep fails stale wakes as LOST_WAKE and expires past-deadline tasks (7 days, 24h for page tasks)",
					DieBefore: "prior state stands", DieDuring: "rollback; wake redelivered",
					DieAfter: "durable wait; timer fires on tick; ignored approval FAILS LOST_WAKE at 24h",
					Visible:  "yes", ResumeTime: "tick (period ungrounded) + due times",
				},
				{
					Name: "cancel-pause-resume", State: "single CAS each; PauseAuthority voids approvals atomically (task_recovery.go)",
					Durability: "transaction", IdempotencyKey: "row version CAS",
					Resumer:   "caller retries",
					DieBefore: "unchanged", DieDuring: "rollback; retry",
					DieAfter: "durable terminal or paused state",
					Visible:  "yes", ResumeTime: "immediate",
				},
			},
		},
		{
			ID:    "scheduled-announcement",
			Title: "A scheduled announcement occurrence",
			Steps: []DurableStep{
				{
					Name: "publish", State: "TWO NON-ATOMIC writes, schedule-first: agent_announcement CAS row + agent_schedule_state + audit (agentux_proactive_announcement.go, agenttriggerstore/schedule.go)",
					Durability: "transaction", IdempotencyKey: "control client key into agent_announcement_command + digest; same key+digest replays seen",
					Resumer:   "human retries",
					DieBefore: "nothing", DieDuring: "one half may commit",
					DieAfter: "between the writes: ORPHAN schedule row (inference); CheckCurrent fails, never planned, no cleaner found",
					Visible:  "yes: control error + announcement list", ResumeTime: "human retries",
				},
				{
					Name: "plan", State: "stale pendings settled FAILED + acked; Plan freezes Requests + advances cursor atomically (cursor CAS, storm cap, outbox ON CONFLICT DO NOTHING)",
					Durability: "transaction", IdempotencyKey: "scheduler source key",
					Resumer:   "next TickTenant; NO PRODUCTION DRIVER (tests only)",
					DieBefore: "cursor unchanged; next tick re-plans (CatchUpOnce/Max1)", DieDuring: "cursor unchanged; next tick re-plans",
					DieAfter: "pending occurrence durable",
					Visible:  "missing post/history only", ResumeTime: "next tick (never automatic: no driver)",
				},
				{
					Name: "replay-admit-ack", State: "CheckRequest, Admit, digest-checked ack; failures stay pending; terminal occurrences acked even on run error (scheduled/outbox.go)",
					Durability: "transaction", IdempotencyKey: "(tenant, kind, key-digest); field change refuses with conflict",
					Resumer:   "next TickTenant; no production driver",
					DieBefore: "delayed replay", DieDuring: "run started before its row: re-runs, and the paid model call may be repeated",
					DieAfter: "row before ack: recovery finds the terminal row and acks without repost",
					Visible:  "history", ResumeTime: "next tick (no driver)",
				},
				{
					Name: "run", State: "advisory fence; run id = admission id; CAS checkpoints; model runs 2 checkpointed attempts; seal pins identity to source key; failures land terminal FAILED/REFUSED rows",
					Durability: "checkpoint", IdempotencyKey: "run id = admission id",
					Resumer:   "next TickTenant; no production driver; manual POST_NOW retries FAILED only",
					DieBefore: "replay redrives", DieDuring: "model cost possibly incurred (refund ungrounded); replay re-executes",
					DieAfter: "FAILED is terminal",
					Visible:  "history", ResumeTime: "next tick (no driver)",
				},
				{
					Name: "deliver", State: "conversation lease + audience re-resolve in fence; chat commit with IdempotencyKey = admission id (same-tx insert-or-return); delivery checkpoint",
					Durability: "transaction", IdempotencyKey: "admission id as chat client key",
					Resumer:   "recovery finds the post via the idempotency JOIN on the admission id and records POSTED without repost",
					DieBefore: "run outcome stands", DieDuring: "commit reached or not; idempotency decides",
					DieAfter: "commit before row: recorded POSTED without repost; re-driven ticks yield a single message",
					Visible:  "message visible; history converges", ResumeTime: "tick-driven (no driver)",
				},
				{
					Name: "project", State: "RecordOccurrence append-only first-wins; different result refuses with revision error; owner history",
					Durability: "transaction", IdempotencyKey: "occurrence identity first-wins",
					Resumer:   "terminal; no resume",
					DieBefore: "delivery stands", DieDuring: "rollback; redriven",
					DieAfter: "durable history",
					Visible:  "owner history + Last* reads", ResumeTime: "immediate",
				},
			},
		},
		{
			ID:    "reminder",
			Title: "A reminder (ambient set/deliver, saved-for-later)",
			Steps: []DurableStep{
				{
					Name: "set-change-snooze", State: "one Control transaction (membership FOR UPDATE, ExpectedRevision, guards, source recheck, future-time, cancel prior, rev++, SET) + one schedule row per fire time (agentux_ambient_controls.go, agentux_ambient_schedule.go)",
					Durability: "transaction", IdempotencyKey: "ambient-sha256(tenant, offer, rev, fire); same-digest skip, different invalid",
					Resumer:   "human retries; PRIVATE EnsureDM at set",
					DieBefore: "error; retry safe", DieDuring: "rollback; retry safe",
					DieAfter: "durable; schedule-save joining the chat transaction is ungrounded, and a split orphan is inert via the current() recheck (inference)",
					Visible:  "card state", ResumeTime: "human retries; delivery on tick",
				},
				{
					Name: "deliver", State: "tick rechecks current(), Plans, Replays, retires delivered; inbox rechecks the offer, matches fire time, admits; delivery re-reads body/digest/fire in a transaction then DM+Notify or PublicMessage (NotifyPerson is a no-op; the unread count is the notice)",
					Durability: "transaction", IdempotencyKey: "Source.Key; PUBLIC via chat idempotency, PRIVATE via DM SendPost key",
					Resumer:   "next TickTenant; no production driver; fires within the last 14 days delivered once when back",
					DieBefore: "next tick (within 14 days; older ungrounded)", DieDuring: "plan reached or not; re-driven",
					DieAfter: "plan before ack: re-admit dedupes + chat idempotency yield one post; post before ack/retire: same, then ack+retire; re-driven ticks converge",
					Visible:  "channel/DM message; post-delivery card update ungrounded (stays SET)", ResumeTime: "next tick (no driver)",
				},
				{
					Name: "saved-remind", State: "ChangeSaved sets due + clears reminded_at; DeliverSavedReminder SELECT FOR UPDATE + key saved:<sha256> + in-tx NotifySaved + reminded_at=now (chatsave_store.go, chatsave_service.go)",
					Durability: "transaction", IdempotencyKey: "saved:<sha256>",
					Resumer:   "NONE: no production caller of DispatchSavedReminders and no production notice implementer (verified); dues accumulate and are never delivered",
					DieBefore: "pending; crash rolls back to pending", DieDuring: "pending; crash rolls back to pending",
					DieAfter: "dues stuck pending forever (unwired)",
					Visible:  "partial: silent due date in ListSaved", ResumeTime: "never (unwired)",
				},
			},
		},
		{
			ID:    "ambient-read",
			Title: "A message read by an ambient agent",
			Steps: []DurableStep{
				{
					Name: "drain", State: "cursor consumer over chat_outbox post events; advances the cursor per event AFTER processing; never marks publisher receipt, never reads payload (agentux_ambient_outbox.go)",
					Durability: "cursor", IdempotencyKey: "outbox id cursor",
					Resumer:   "TickTenant drains only when the screen model is composed, else silent no-op; no production driver",
					DieBefore: "redrain re-processes (inner dedupe below)", DieDuring: "redrain re-processes",
					DieAfter: "cursor before advance: redrain re-processes",
					Visible:  "missing card only", ResumeTime: "next tick (no driver)",
				},
				{
					Name: "reserve", State: "one transaction: advisory locks, grant/install/member/optout, budget counts from READ rows, >=2 reads, caps, screen gate, parent read, insert RESERVED (agentux_ambient_candidate.go)",
					Durability: "transaction", IdempotencyKey: "reservation row identity",
					Resumer:   "NONE: grounded comment says a crashed reserved read is not automatically retried",
					DieBefore: "nothing; redrain re-attempts", DieDuring: "rollback; redrain re-attempts",
					DieAfter: "commit before model: RESERVED durable, model never called, LOST until an edit or new request; whether RESERVED suppresses retries via budget counts is ungrounded",
					Visible:  "missing card; trace ungrounded", ResumeTime: "new trigger only",
				},
				{
					Name: "model", State: "model call OUTSIDE any transaction, 30s timeout (agentux_ambient_service.go); no journal, no lease",
					Durability:     DurabilityNone,
					IdempotencyKey: "none",
					Resumer:        "none; error lands a FAILED row and stops",
					DieBefore:      "lost until new trigger", DieDuring: "lost until new trigger",
					DieAfter: "lost until new trigger",
					Visible:  "missing card", ResumeTime: "new trigger only",
				},
				{
					Name: "result", State: "one result transaction: re-lock, source/authority recheck, outcome row ON CONFLICT DO NOTHING, cancel-on-delete, proposal validation, pure audience fn, deterministic offer id, live-dedupe, terminals honored, rev++ re-offer, single save + payload-free invalidation event (agentux_ambient_decision.go, service.go)",
					Durability: "transaction", IdempotencyKey: "ambient-sha256(tenant, post, agent)[:16]",
					Resumer:   "redrain re-attempts subject to budget",
					DieBefore: "lost; redrain re-attempts", DieDuring: "rollback; redrain re-attempts",
					DieAfter: "card durable + broadcast; private offers filtered in SQL",
					Visible:  "yes (incl. SOURCE_CHANGED/CANCELLED)", ResumeTime: "next tick (no driver)",
				},
			},
		},
		{
			ID:    "offer-accept",
			Title: "A to-do or reminder offer and its acceptance",
			Steps: []DurableStep{
				{
					Name: "offer", State: "created by the ambient result transaction (see ambient-read/result); deterministic offer id",
					Durability: "transaction", IdempotencyKey: "ambient-sha256(tenant, post, agent)[:16]",
					Resumer:   "durable at creation; none needed",
					DieBefore: "ambient result outcome stands", DieDuring: "ambient result outcome stands",
					DieAfter: "durable offer card",
					Visible:  "yes: offer card", ResumeTime: "immediate",
				},
				{
					Name: "accept", State: "single transaction per action, no outbox/lease/journal: PRIVATE upserts agentux_ambient_task by offer id owner-pinned; PUBLIC writes chat_channel_todo + revision + inventory in the same transaction (agentux_ambient_controls.go, agentux_ambient_task.go); auto-accept for PUBLIC TASK + automatic_public grant at offer time",
					Durability: "transaction", IdempotencyKey: "offer/task id + ExpectedRevision + item dedupe (item id = offer id)",
					Resumer:   "human retries; cancel removes only unfinished items with a proving offer",
					DieBefore: "not applied; caller retries", DieDuring: "rollback; caller retries",
					DieAfter: "durable; same text/completed is a no-op, change needs authorizing offer state",
					Visible:  "yes (ADDED, task lists, channel list)", ResumeTime: "human retries",
				},
			},
		},
		{
			ID:    "support-email",
			Title: "A support email becoming a ticket and an alert (simulation-only ingest; live path local-dev only; production serve passes nil)",
			Steps: []DurableStep{
				{
					Name: "validate", State: "memory only (agentux054_support_desk.go)",
					Durability:     DurabilityNone,
					IdempotencyKey: "support-email:<uuidv5> derived after validation",
					Resumer:        "owner re-POSTs",
					DieBefore:      "lost; re-POST", DieDuring: "lost; re-POST",
					DieAfter: "validated in memory only; crash before receipt loses the submission",
					Visible:  "400/403", ResumeTime: "immediate (next re-POST)",
				},
				{
					Name: "store-object", State: "filesystem $ROOT/support-objects/<tenant>/<sha256>.json, temp+Sync+rename, no transaction (agentux054_support_ports.go)",
					Durability:     DurabilityNone,
					IdempotencyKey: "content sha256; same bytes are a no-op",
					Resumer:        "owner re-POSTs",
					DieBefore:      "lost; re-POST", DieDuring: "lost or orphan temp (temp removed; GC ungrounded)",
					DieAfter: "durable file without a receipt row until S2",
					Visible:  "503", ResumeTime: "next re-POST",
				},
				{
					Name: "receipt", State: "support_inbox_message PK (tenant, message), append-only; INSERT ON CONFLICT DO NOTHING + byte-compare in a transaction (agentux_demo_inbox.go, migration 00043)",
					Durability: "transaction", IdempotencyKey: "(tenant, message); byte mismatch refuses 409",
					Resumer:   "owner re-POSTs",
					DieBefore: "rollback", DieDuring: "rollback",
					DieAfter: "durable; ReceivedAt immutable",
					Visible:  "503 or Replayed", ResumeTime: "next re-POST",
				},
				{
					Name: "fence-verify", State: "advisory xact lock + in-memory target/digest compare (agentux_demo_effects.go, desk.go)",
					Durability:     DurabilityNone,
					IdempotencyKey: "message identity within the request",
					Resumer:        "owner re-POSTs; lock auto-releases",
					DieBefore:      "lost; re-POST", DieDuring: "lock released; re-POST",
					DieAfter: "in-request verdict only; crash before the ticket step loses it",
					Visible:  "503", ResumeTime: "next re-POST",
				},
				{
					Name: "plan", State: "plan file + support_run_plan row PK (tenant, message) (migration 00044); deterministic planner; insert-once + compare in a transaction",
					Durability: "transaction", IdempotencyKey: "(tenant, message) insert-once",
					Resumer:   "owner re-POSTs; re-plan over same bytes converges",
					DieBefore: "receipt stands; re-POST re-plans", DieDuring: "file/row split: orphan file, re-plan over same bytes",
					DieAfter: "durable plan",
					Visible:  "503 until planned", ResumeTime: "next re-POST",
				},
				{
					Name: "reserve", State: "support_effect_reservation PK (tenant, skill, message); cap 25 per skill per UTC day; re-reserve is a no-op",
					Durability: "transaction", IdempotencyKey: "(tenant, skill, message)",
					Resumer:   "owner re-POSTs; no delete path, no cleaner",
					DieBefore: "no slot held", DieDuring: "rollback; no slot held",
					DieAfter: "fail after reserve: reservation slot stuck leaked until midnight, no delete; notice ungrounded",
					Visible:  "503", ResumeTime: "midnight UTC (cap reset)",
				},
				{
					Name: "ticket", State: "task + event + idempotency in one transaction (repository.go)",
					Durability: "transaction", IdempotencyKey: "support.create_ticket:<message> + uuidv5 id; replay returns the record",
					Resumer:   "owner re-POSTs",
					DieBefore: "reservation stands; re-POST continues", DieDuring: "rollback; re-POST continues",
					DieAfter: "durable ticket; fail surfaces 503 stored-no-ticket",
					Visible:  "ticket visible", ResumeTime: "next re-POST",
				},
				{
					Name: "alert", State: "post + outbox + idempotency in one transaction (chatstore/store.go); ClientKey support.alert_channel:<message>; body never carries email text; NO production outbox consumer (tests only)",
					Durability: "transaction", IdempotencyKey: "support.alert_channel:<message>",
					Resumer:   "owner re-POSTs; retry completes a ticket-without-alert with no duplicate",
					DieBefore: "ticket without alert; retry completes", DieDuring: "ticket without alert; retry completes",
					DieAfter: "durable alert post; fanout limited to direct reads (no production consumer)",
					Visible:  "alert post visible", ResumeTime: "next re-POST",
				},
			},
		},
		{
			ID:    "voice-transcription",
			Title: "Voice transcription (no outbox/lease/journal; pending-polling)",
			Steps: []DurableStep{
				{
					Name: "send", State: "Validate, Upload, Post, Request are NOT one transaction (service.go)",
					Durability: "transaction", IdempotencyKey: "PK (tenant, conversation, post, artifact) + revision CAS",
					Resumer:   "user re-sends the same key",
					DieBefore: "nothing; re-send", DieDuring: "partial path; re-send",
					DieAfter: "after post commit before the request row: STUCK, no sweeper (ungrounded); retry 404s",
					Visible:  "post without transcript", ResumeTime: "user re-sends",
				},
				{
					Name: "poll", State: "3s poll, batch 20, 4 slots, only when Tenants set (chatvoice_serve.go); NO lease; CAS at save",
					Durability:     DurabilityNone,
					IdempotencyKey: "none at poll time (CAS at save)",
					Resumer:        "the poll loop itself",
					DieBefore:      "rows stay pending", DieDuring: "rows stay pending",
					DieAfter: "two workers poll the same row: repeated provider call, one CAS winner",
					Visible:  "pending transcript", ResumeTime: "3s poll",
				},
				{
					Name: "save", State: "provider call OUTSIDE the transaction; one worker-only CAS save transaction (transcripts.go); Settle runs even on failure; budget IN-MEMORY (chatvoice_ops.go)",
					Durability: "transaction", IdempotencyKey: "PK + revision CAS; completion via re-read only",
					Resumer:   "poll redrives; no auto-retry, click advances to next poll",
					DieBefore: "pending; redriven", DieDuring: "between the provider call and the save: repeated charge (DOUBLE CHARGE); crash wipes budget spend",
					DieAfter: "durable transcript",
					Visible:  "transcript or pending", ResumeTime: "3s poll",
				},
				{
					Name: "retry", State: "unavailable/failed to pending, rev+1, one transaction (chatvoice_actions.go)",
					Durability: "transaction", IdempotencyKey: "revision CAS",
					Resumer:   "user clicks; no auto-retry",
					DieBefore: "stays failed; click retries", DieDuring: "rollback; click retries",
					DieAfter: "audio lost: retries re-fail forever, stuck failing with no recovery (openai.go)",
					Visible:  "failed transcript", ResumeTime: "user click + poll",
				},
			},
		},
		{
			ID:    "document-indexing",
			Title: "Document indexing (nearest to a durable queue: lease-claimed jobs)",
			Steps: []DurableStep{
				{
					Name: "enqueue", State: "job row written inside the document write transaction (index_jobs.go)",
					Durability: "transaction", IdempotencyKey: "UNIQUE (tenant, doc, version, model); conflict requeues only skipped rows, never resurrects failed (backfill only)",
					Resumer:   "immediate; job visible in IndexQueueStats",
					DieBefore: "vanish together (same tx)", DieDuring: "vanish together (same tx)",
					DieAfter: "durable queued job",
					Visible:  "IndexQueueStats", ResumeTime: "immediate",
				},
				{
					Name: "notify", State: "in-memory channel post-commit (indexer.go)",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (hint only)",
					Resumer:        "polls run regardless of the hint",
					DieBefore:      "poll still finds the job", DieDuring: "at most 2s delay",
					DieAfter: "hint consumed or lost; harmless",
					Visible:  "no", ResumeTime: "poll interval",
				},
				{
					Name: "claim", State: "one transaction: running + owner + lease + attempts++ over FOR UPDATE SKIP LOCKED (index_jobs.go); lease 2 minutes",
					Durability: "lease", IdempotencyKey: "lease_owner + lease_until",
					Resumer:   "lease expiry reclaims; poll redrives",
					DieBefore: "queued; claimed next", DieDuring: "rollback; queued",
					DieAfter: "commit then death: job stuck 2 minutes on a dead lease, then reclaimed",
					Visible:  "IndexQueueStats", ResumeTime: "lease + poll",
				},
				{
					Name: "embed-store", State: "provider call OUTSIDE the transaction; vector insert ON CONFLICT DO NOTHING (search_vectors.go)",
					Durability: "transaction", IdempotencyKey: "vector identity insert-once",
					Resumer:   "lease expiry redrives; no backoff",
					DieBefore: "re-driven; re-embeds", DieDuring: "re-embeds: repeated embedding spend, at-least-once, never half-state",
					DieAfter: "durable vectors",
					Visible:  "search coverage", ResumeTime: "lease expiry",
				},
				{
					Name: "outcome", State: "conditional finish; failed after 5 tries; backoff 5s*2^n capped 10 minutes; context-cancel records NOTHING; lost is NOT persisted",
					Durability: "transaction", IdempotencyKey: "job id finish-once",
					Resumer:   "backoff redrive; failed rows wait for manual backfill via CLI + prepare; nobody automatic",
					DieBefore: "redriven on backoff", DieDuring: "cancel records nothing; redriven",
					DieAfter: "failed jobs stuck until manual backfill (unbounded); lost transitions not persisted",
					Visible:  "IndexQueueStats", ResumeTime: "backoff; failed waits for manual backfill (unbounded)",
				},
				{
					Name: "restart", State: "restart polls only constructor tenants + Notify (indexer.go); production seeds one tenant; watermark path has ZERO production callers; no model means no indexer",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (in-memory tenant set)",
					Resumer:        "reconstruct; other tenants idle (no sweeper)",
					DieBefore:      "jobs wait", DieDuring: "jobs wait",
					DieAfter: "jobs for unseeded tenants wait with no resumer",
					Visible:  "IndexQueueStats (seeded tenant only)", ResumeTime: "restart + poll (seeded tenants)",
				},
			},
		},
		{
			ID:    "chat-indexing",
			Title: "Chat indexing (grounded negative: no index jobs, tables or watermarks)",
			Steps: []DurableStep{
				{
					Name: "write-index", State: "PostgreSQL indexes maintained on write; no separate index pipeline",
					Durability: "transaction", IdempotencyKey: "post identity",
					Resumer:   "none needed",
					DieBefore: "write outcome stands", DieDuring: "rollback with the write",
					DieAfter: "indexed by the write itself",
					Visible:  "search hits", ResumeTime: "next request",
				},
				{
					Name: "registry", State: "in-memory Registry rebuilt per boot + per-request fan-out; recent history in-process (chatsearch_source.go, chatsearch_recent.go); no index holds a historic audience snapshot",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (rebuilt, not keyed)",
					Resumer:        "next boot rebuilds identically; registry failure aborts startup",
					DieBefore:      "rebuilt at boot", DieDuring: "rebuilt at boot",
					DieAfter: "recent history wiped on restart (by design)",
					Visible:  "search scope narrows after restart", ResumeTime: "next boot",
				},
				{
					Name: "search", State: "100-row windows in their own transactions; fail-closed; atomic response",
					Durability: "transaction", IdempotencyKey: "request cursor",
					Resumer:   "next request",
					DieBefore: "no effect", DieDuring: "request fails; retried by caller",
					DieAfter: "read-only; nothing to resume",
					Visible:  "request error", ResumeTime: "next request",
				},
			},
		},
		{
			ID:    "version-review",
			Title: "Version review",
			Steps: []DurableStep{
				{
					Name: "draft", State: "one transaction + advisory lock (version_draft.go); versions/owners/lifecycle events DRAFT/IN_REVIEW/PUBLISHED/SUSPENDED + RETIRED",
					Durability: "transaction", IdempotencyKey: "none: version = latest+1, duplicate refused with conflict",
					Resumer:   "human retries; no resumer",
					DieBefore: "nothing", DieDuring: "rollback; nothing",
					DieAfter: "durable draft",
					Visible:  "lifecycle read", ResumeTime: "immediate (human)",
				},
				{
					Name: "request-review", State: "one transaction: From must equal current, event INSERT ON CONFLICT refuses with conflict (store.go)",
					Durability: "transaction", IdempotencyKey: "fresh id/attempt; post-commit retry fails safe",
					Resumer:   "human retries",
					DieBefore: "still DRAFT", DieDuring: "still DRAFT",
					DieAfter: "durable IN_REVIEW",
					Visible:  "lifecycle read", ResumeTime: "immediate (human)",
				},
				{
					Name: "issue", State: "single transaction on a dedicated pool (review_authority_pool.go, review_issuer.go); evidence expires min(issued+30d, grant)",
					Durability: "transaction", IdempotencyKey: "none: fresh uuid/attempt per call",
					Resumer:   "reviewer retries; no auto-resume",
					DieBefore: "no decision; retry safe", DieDuring: "no decision; retry safe",
					DieAfter: "after commit before response: retry mints a repeated decision row; latest-wins mitigates (current_review_query.go)",
					Visible:  "review decision read", ResumeTime: "immediate (reviewer)",
				},
			},
		},
		{
			ID:    "evaluation",
			Title: "Evaluation",
			Steps: []DurableStep{
				{
					Name: "run-case", State: "live per-case: synthetic run + journal checkpoint/case in one transaction per append + ON CONFLICT DO NOTHING + digest compare, mismatch refuses (store.go, migration 00032, immutable PK tenant/invocation); local-dev is in-memory with no journal",
					Durability: "journal entry", IdempotencyKey: "PK (tenant, invocation); rerun needs byte-match",
					Resumer:   "operator reruns the whole suite; NO batch progress/resume: the loop runs all, any failure yields no report (persona_suite.go)",
					DieBefore: "case not run; rerun covers", DieDuring: "orphan case rows; the whole suite must be repeated from scratch (paid calls repeated); rerun id determinism ungrounded",
					DieAfter: "durable case evidence",
					Visible:  "no report until the suite completes", ResumeTime: "full rerun",
				},
				{
					Name: "seal", State: "in-memory digest over case evidence",
					Durability:     DurabilityNone,
					IdempotencyKey: "none (recomputed)",
					Resumer:        "recompute",
					DieBefore:      "nothing sealed; recompute", DieDuring: "nothing sealed; recompute",
					DieAfter: "in-memory seal only; crash recomputes",
					Visible:  "no", ResumeTime: "recompute",
				},
				{
					Name: "sign-record", State: "Issue validates against sealed profile/suite/model/freshness (persona_evaluation_issuer.go); Record is a caller-transaction insert, zero rows refuses with conflict (evaluation_evidence.go, immutable PK tenant/run, migration 00013)",
					Durability: "transaction", IdempotencyKey: "PK (tenant, run); admin uuid/run; prepare uses a deterministic run id and tolerates conflict",
					Resumer:   "admin re-issues; full rerun on failure",
					DieBefore: "no record; re-issue", DieDuring: "rollback; re-issue",
					DieAfter: "durable evidence; freshness at most 30 days",
					Visible:  "{Status, Passed, Failed, FailingCases}", ResumeTime: "immediate (re-issue + rerun)",
				},
			},
		},
		{
			ID:    "publication-provisioning",
			Title: "Publication with provisioning",
			Steps: []DurableStep{
				{
					Name: "provision", State: "order first (provision BEFORE lifecycle commit; refused with no provisioner); local provisioner runs sequential idempotent steps with NO encompassing transaction: evidence compare, in-memory eval, deployment file byte-compare else temp+rename, run-policy SELECT-then-INSERT, route current-check then publish rev 1..64, principal check to core INSERT to persona Provision (agentux_general_runtime_provisioner.go); route publisher is a single transaction + advisory lock + MAX(rev)+1 CAS; non-local provisioner ungrounded",
					Durability: "transaction", IdempotencyKey: "evidence compare, file bytes, route check, binding, grant freshness",
					Resumer:   "human re-issues; retry idempotent with no duplicate revision; principal-without-binding reconciled",
					DieBefore: "still IN_REVIEW; re-issue", DieDuring: "mid-death: partial runtime, still IN_REVIEW; retry converges",
					DieAfter: "provisioned runtime awaiting publish",
					Visible:  "lifecycle read", ResumeTime: "immediate (human re-issue)",
				},
				{
					Name: "publish", State: "single transaction: lock, scan, in-transaction evidence re-resolution, owner checks, label seal, INSERT PUBLISHED ON CONFLICT refuses (store.go)",
					Durability: "transaction", IdempotencyKey: "fresh id but From==current fails safe; label kept/advanced",
					Resumer:   "human retries",
					DieBefore: "still IN_REVIEW", DieDuring: "rollback; still IN_REVIEW",
					DieAfter: "durable PUBLISHED",
					Visible:  "lifecycle read", ResumeTime: "immediate (retry)",
				},
				{
					Name: "import-draft", State: "portable import to draft: one transaction over content + manifest + head + DRAFT, immutable DRAFT-only (agent_portable.go)",
					Durability: "transaction", IdempotencyKey: "none: retry mints a fresh agent id",
					Resumer:   "manual retry",
					DieBefore: "nothing; retry", DieDuring: "rollback; retry",
					DieAfter: "repeated import creates a second draft",
					Visible:  "draft list", ResumeTime: "immediate (manual retry)",
				},
				{
					Name: "populate", State: "population provision: one transaction over all employees; per-subject lock; UUIDv5; ON CONFLICT DO NOTHING + verify (agent_population_provisioner.go)",
					Durability: "transaction", IdempotencyKey: "UUIDv5 subject identity; idempotent rerun",
					Resumer:   "operator reruns",
					DieBefore: "nothing; rerun", DieDuring: "rollback; rerun",
					DieAfter: "durable population",
					Visible:  "population read", ResumeTime: "immediate (rerun)",
				},
			},
		},
		{
			ID:    "staged-rollout",
			Title: "Staged rollout (immutable plan by digest, progress rev/cursor/stage, immutable approval; migration 00029)",
			Steps: []DurableStep{
				{
					Name: "preview", State: "one transaction: plan + PREVIEWED/rev1/cursor0 (agent_rollout_service.go)",
					Durability: "transaction", IdempotencyKey: "rollout_id uuid; duplicate refuses with conflict",
					Resumer:   "operator retries with a new id (old lingers, harmless)",
					DieBefore: "nothing; retry", DieDuring: "rollback; retry",
					DieAfter: "durable preview",
					Visible:  "READ", ResumeTime: "immediate",
				},
				{
					Name: "approve", State: "one transaction FOR UPDATE: digest + expected-rev + PREVIEWED fences, approval row, to APPROVED rev+1 (version_rollout.go)",
					Durability: "transaction", IdempotencyKey: "(digest, expected-rev); mismatch refuses preview-stale",
					Resumer:   "operator retries on current preview",
					DieBefore: "still PREVIEWED", DieDuring: "rollback; still PREVIEWED",
					DieAfter: "durable APPROVED",
					Visible:  "READ", ResumeTime: "immediate",
				},
				{
					Name: "advance", State: "one transaction per candidate: revalidate + fence, apply (fences, candidate==plan[cursor], locks, install FOR UPDATE + snapshot compare, in-tx evidence re-resolution, approval + scope checks, install CAS + cursor+1/rev+1 in the SAME transaction)",
					Durability: "transaction", IdempotencyKey: "(digest, rev) CAS + cursor equality: exactly once",
					Resumer:   "operator retries; cursor persisted between transactions",
					DieBefore: "cursor stands; retry continues", DieDuring: "rollback; retry continues",
					DieAfter: "durable advance; no double-apply (derived)",
					Visible:  "READ, CATALOG", ResumeTime: "immediate (retry continues)",
				},
				{
					Name: "promote", State: "CANARY_COMPLETE to APPROVED by the same approver; NO background advancer; NO TTL",
					Durability: "transaction", IdempotencyKey: "approval identity",
					Resumer:   "operator acts; nothing automatic",
					DieBefore: "stage stands; operator acts", DieDuring: "rollback; operator acts",
					DieAfter: "durable stage; canaries wait for the operator with no deadline",
					Visible:  "READ, CATALOG", ResumeTime: "operator-driven (no deadline)",
				},
			},
		},
		{
			ID:    "channel-gate",
			Title: "A channel gate submission becoming a membership",
			Steps: []DurableStep{
				{
					Name: "submit", State: "ONE TRANSACTION PER MUTATION: Transact locks the conversation, runs fn, appends revs/versions/answers/audits, writes one outbox row/event, upserts state (chat_gate_state rev + state_json); Membership() in the same transaction; membership-fence and leave-erasure triggers backstop (chatgate_repository.go, migrations 00022/00023)",
					Durability: "transaction", IdempotencyKey: "(person:key) receipts with fingerprint (same key+input replays, different refuses) + ExpectedRevision CAS + digest submission id",
					Resumer:   "same-key retry (no duplicate) or new key (supersedes)",
					DieBefore: "full rollback (derived)", DieDuring: "full rollback (derived)",
					DieAfter: "durable: auto-admits + membership in the same transaction, or queued for review by rule mode",
					Visible:  "summaries, roster, basis log", ResumeTime: "immediate (retry)",
				},
				{
					Name: "review", State: "Review/ReviewMany with revision CAS (chatgate_service.go); no auto-review",
					Durability: "transaction", IdempotencyKey: "ExpectedRevision CAS",
					Resumer:   "reviewer retries",
					DieBefore: "still queued", DieDuring: "rollback; still queued",
					DieAfter: "durable decision",
					Visible:  "summaries, roster", ResumeTime: "reviewer-driven",
				},
				{
					Name: "deliver-event", State: "consumers read via PendingOutboxAfter + cursors and dedupe on Event.ID (store.go); DeliverEvent routes in-transaction and delivers outside: at-least-once; consumer implementation ungrounded",
					Durability: "outbox row", IdempotencyKey: "Event.ID dedupe",
					Resumer:   "consumer cursor redrive",
					DieBefore: "event still pending; redriven", DieDuring: "routed or not; at-least-once redelivery replays by event id",
					DieAfter: "delivered (at-least-once)",
					Visible:  "roster + downstream effects", ResumeTime: "consumer-driven",
				},
			},
		},
		{
			ID:    "feedback-stop-retry",
			Title: "Feedback, stop and retry",
			Steps: []DurableStep{
				{
					Name: "feedback", State: "request + current rows, voter receipt required, one transaction, rev bump (agentux_r5srv_feedback.go)",
					Durability: "transaction", IdempotencyKey: "client key 8-128 chars; same key+payload is a no-op, different refuses; undo is a new keyed request",
					Resumer:   "caller retries safely",
					DieBefore: "rollback; UI unsaved", DieDuring: "rollback; UI unsaved",
					DieAfter: "durable vote",
					Visible:  "Active/Helpful echoed", ResumeTime: "immediate",
				},
				{
					Name: "stop", State: "same-row CAS: CancelRequested=true, lease cleared, to CANCELLED (or RECONCILING when UNKNOWN); one conflict retry; NO-OP rechecker (run.go, persona_actions.go); the model call is NOT preempted and lands at the next CAS",
					Durability: "transaction", IdempotencyKey: "key validated but NOT persisted: state-check idempotency only",
					Resumer:   "caller retries (UI retry ungrounded)",
					DieBefore: "cancel lost; run continues", DieDuring: "cancel lost; run continues",
					DieAfter: "durable CANCELLED",
					Visible:  `"state":"CANCELLED"`, ResumeTime: "immediate",
				},
				{
					Name: "retry", State: "NEW invocation with attempt N in the hash (uxblind_p8_invocation.go); old invocation untouched, page shows newest; eligibility gates (MODEL_UNAVAILABLE/ANSWER_INTERRUPTED, plus ADMISSION/EXECUTION_UNAVAILABLE on post rows); only after the prior run ended answerless (enforcement ungrounded)",
					Durability: "transaction", IdempotencyKey: "same attempt yields the same id, so Claim replays",
					Resumer:   "user-driven; death behaves as claim/admission/run-create",
					DieBefore: "old outcome stands", DieDuring: "new invocation outcome per its own steps",
					DieAfter: "durable new invocation",
					Visible:  "new row; card retryable + retry_at", ResumeTime: "user-driven",
				},
				{
					Name: "surface-failure", State: "errors land in persona_invocation_failure via a 5s detached write (persona_chat_surface_failure.go); key (tenant, invoker, post) first-wins",
					Durability: "transaction", IdempotencyKey: "(tenant, invoker, post) first-wins",
					Resumer:   "run recovery owns the card once a run exists; multi-agent multiplex ungrounded",
					DieBefore: "no card; run card may follow", DieDuring: "no card; run card may follow",
					DieAfter: "GAP: the failure row does NOT finish an invocation that has a run, so the card follows the run and a dead run leaves a stuck card",
					Visible:  "failure card (when it resolves)", ResumeTime: "run recovery",
				},
			},
		},
		{
			ID:    "preparation-command",
			Title: "The preparation command (one-shot, sequential, NO encompassing transaction; failure aborts with a partial receipt)",
			Steps: []DurableStep{
				{
					Name: "run-all", State: "per-starter sequence (agentux_general_prepare.go): manifest compare-or-next-version, version reuse/upgrade/create, DRAFT to IN_REVIEW (skipped on rerun), review-if-none (idempotent 1h grant + lock), in-memory eval, settled-skip, renew lapsed via n+1 + recursion, eval Issue with deterministic run id tolerating conflict, chat identity lookup-or-register, role visibility (conflicts ignored), direct conversation ensure+repair, publish-or-reprovision, retire-duplicates (idempotent) + install/reinstall",
					Durability: "transaction", IdempotencyKey: "deterministic eval run id, manifest digest, file bytes, route check, binding, grant freshness",
					Resumer:   "operator reruns; no daemon; rerun skips completed steps and converges to no change",
					DieBefore: "step rolls back; rerun covers", DieDuring: "step rolls back; rerun covers",
					DieAfter: "completed steps skipped on rerun; converges",
					Visible:  "summary + receipts", ResumeTime: "immediate (operator rerun)",
				},
				{
					Name: "version-split", State: "PutVersion + AppendLifecycle are separate writes (starter_prepare.go)",
					Durability: "transaction", IdempotencyKey: "version identity",
					Resumer:   "operator reruns; recovery across the split is UNGROUNDED",
					DieBefore: "no version; rerun creates", DieDuring: "no version; rerun creates",
					DieAfter: "between-write crash leaves a version without lifecycle; recovery ungrounded, possibly stuck",
					Visible:  "summary + receipts", ResumeTime: "operator rerun (split recovery ungrounded)",
				},
				{
					Name: "tail", State: "ambient installs, alerts, history, support seed via stable keys; queue step is a no-op (agentux049_prepare_tail.go); deployment files temp+rename; eval key O_EXCL + Sync",
					Durability: "transaction", IdempotencyKey: "stable tail keys; support keys; manifest digest",
					Resumer:   "operator reruns; expiry: grant 24h, eval 30d; old versions kept",
					DieBefore: "tail reruns", DieDuring: "step rolls back; rerun covers",
					DieAfter: "durable tail state",
					Visible:  "summary + receipts", ResumeTime: "immediate (operator rerun)",
				},
			},
		},
	}
}

// DurableFindings returns every "nothing does", "stuck" and "repeated"
// verdict in the inventory with the todo that would fix it.
func DurableFindings() []DurableFinding {
	return []DurableFinding{
		{ID: "F-mention-postcommit", Process: "mention-answer", Step: "post", Symptom: "post-commit mention fan-out is an in-memory goroutine with no re-trigger: a death before it starts loses the mention", FixTodo: "DURABLE-003"},
		{ID: "F-mention-model", Process: "mention-answer", Step: "model call", Symptom: "nothing durable covers the model call: paid but unjournaled spend ends ANSWER_INTERRUPTED", FixTodo: "DURABLE-003"},
		{ID: "F-mention-tool", Process: "mention-answer", Step: "tool effect", Symptom: "an effect left UNKNOWN is stuck in RECONCILING unbounded with no surfacing and no persona reconciler", FixTodo: "DURABLE-003"},
		{ID: "F-mention-continuation", Process: "mention-answer", Step: "continuation", Symptom: "nothing durable covers the continuation call: paid but unjournaled spend ends ANSWER_INTERRUPTED", FixTodo: "DURABLE-003"},
		{ID: "F-mention-delivery-split", Process: "mention-answer", Step: "delivery", Symptom: "chat commit, receipt and checkpoint are three separate writes: an orphan visible answer plus a later interrupted card is possible", FixTodo: "DURABLE-003"},
		{ID: "F-task-unconfirmed", Process: "agents-page-task", Step: "create", Symptom: "a quick task that dies after create but before confirm is stuck UNCONFIRMED: the tick drives RUNNING+confirmed only", FixTodo: "DURABLE-003"},
		{ID: "F-task-ambiguous-owner", Process: "agents-page-task", Step: "drive", Symptom: "a tier>=T2 step that dies mid-effect pauses AMBIGUOUS_EFFECT with step spend unjournaled and waits for an owner reconcile with no deadline", FixTodo: "DURABLE-006"},
		{ID: "F-announce-publish-split", Process: "scheduled-announcement", Step: "publish", Symptom: "announcement CAS and schedule write are non-atomic: a death between them orphans a schedule row that is never planned and never cleaned", FixTodo: "DURABLE-003"},
		{ID: "F-announce-no-driver", Process: "scheduled-announcement", Step: "plan", Symptom: "TickTenant has no production driver, so a planned occurrence never auto-resumes", FixTodo: "DURABLE-006"},
		{ID: "F-announce-model-reexec", Process: "scheduled-announcement", Step: "replay-admit-ack", Symptom: "a death after the run starts but before its row re-runs the occurrence and may repeat the paid model call", FixTodo: "DURABLE-003"},
		{ID: "F-reminder-no-driver", Process: "reminder", Step: "deliver", Symptom: "TickTenant has no production driver, so due reminders never auto-deliver", FixTodo: "DURABLE-006"},
		{ID: "F-reminder-saved-unwired", Process: "reminder", Step: "saved-remind", Symptom: "DispatchSavedReminders has no production caller and no production notice implementer: dues are stuck pending forever", FixTodo: "DURABLE-006"},
		{ID: "F-ambient-no-driver", Process: "ambient-read", Step: "drain", Symptom: "TickTenant has no production driver, so the outbox drain never runs on its own", FixTodo: "DURABLE-006"},
		{ID: "F-ambient-reserved-lost", Process: "ambient-read", Step: "reserve", Symptom: "a reservation committed before a death that kills the model call is never retried: the read is lost until an edit or new request", FixTodo: "DURABLE-003"},
		{ID: "F-ambient-model", Process: "ambient-read", Step: "model", Symptom: "nothing durable covers the ambient model call: no journal, no lease", FixTodo: "DURABLE-003"},
		{ID: "F-support-validate", Process: "support-email", Step: "validate", Symptom: "nothing durable covers validation: a crash before receipt loses the submission and the sender re-POSTs", FixTodo: "DURABLE-003"},
		{ID: "F-support-object", Process: "support-email", Step: "store-object", Symptom: "nothing transactional covers the email object file: temp+rename with no journal and ungrounded GC", FixTodo: "DURABLE-003"},
		{ID: "F-support-fence", Process: "support-email", Step: "fence-verify", Symptom: "nothing durable covers the fence verdict: an xact lock plus memory that a crash discards", FixTodo: "DURABLE-003"},
		{ID: "F-support-reserve-leak", Process: "support-email", Step: "reserve", Symptom: "a failure after reserve leaks the slot stuck until the midnight cap reset: no delete, no cleaner", FixTodo: "DURABLE-006"},
		{ID: "F-support-alert-consumer", Process: "support-email", Step: "alert", Symptom: "the alert outbox row has no production consumer, so fanout is limited to direct reads", FixTodo: "DURABLE-006"},
		{ID: "F-voice-send-split", Process: "voice-transcription", Step: "send", Symptom: "post commit and request row are not one transaction: a death between them leaves the upload stuck with no sweeper and 404ing retries", FixTodo: "DURABLE-003"},
		{ID: "F-voice-poll-double", Process: "voice-transcription", Step: "poll", Symptom: "nothing leases the poll: two workers poll the same row and repeat the provider call (one CAS winner)", FixTodo: "DURABLE-003"},
		{ID: "F-voice-double-charge", Process: "voice-transcription", Step: "save", Symptom: "the provider call sits outside the save transaction: a death between them repeats the charge, and the in-memory budget spend is wiped", FixTodo: "DURABLE-003"},
		{ID: "F-voice-retry-audioloss", Process: "voice-transcription", Step: "retry", Symptom: "lost audio leaves retries re-failing forever, stuck with no recovery", FixTodo: "DURABLE-006"},
		{ID: "F-doc-notify", Process: "document-indexing", Step: "notify", Symptom: "nothing durable carries the wakeup hint: an in-memory channel the polls do not need", FixTodo: "DURABLE-002"},
		{ID: "F-doc-claim-lease", Process: "document-indexing", Step: "claim", Symptom: "a death after claim leaves the job stuck 2 minutes on a dead lease before reclaim", FixTodo: "DURABLE-002"},
		{ID: "F-doc-reembed", Process: "document-indexing", Step: "embed-store", Symptom: "the embedding call sits outside the store transaction: redrive repeats the embedding spend", FixTodo: "DURABLE-003"},
		{ID: "F-doc-failed-manual", Process: "document-indexing", Step: "outcome", Symptom: "failed jobs are stuck until manual backfill (unbounded) and lost transitions are never persisted", FixTodo: "DURABLE-006"},
		{ID: "F-doc-restart-tenants", Process: "document-indexing", Step: "restart", Symptom: "nothing durable holds the poll tenant set: unseeded tenants idle with no resumer and the watermark path is dead", FixTodo: "DURABLE-006"},
		{ID: "F-chat-registry-mem", Process: "chat-indexing", Step: "registry", Symptom: "nothing durable holds the search registry or recent history: both are rebuilt or wiped every boot", FixTodo: "DURABLE-006"},
		{ID: "F-review-issue-key", Process: "version-review", Step: "issue", Symptom: "the decision issue carries no idempotency key: a retry after commit mints a repeated decision row (latest-wins mitigates)", FixTodo: "DURABLE-003"},
		{ID: "F-eval-no-resume", Process: "evaluation", Step: "run-case", Symptom: "the suite keeps no batch progress: a crash orphans case rows and the whole paid suite must be repeated", FixTodo: "DURABLE-003"},
		{ID: "F-eval-seal", Process: "evaluation", Step: "seal", Symptom: "nothing durable holds the seal digest: recomputed after every crash", FixTodo: "DURABLE-003"},
		{ID: "F-import-second-draft", Process: "publication-provisioning", Step: "import-draft", Symptom: "the import carries no idempotency key: a repeated import creates a second draft", FixTodo: "DURABLE-003"},
		{ID: "F-rollout-no-advancer", Process: "staged-rollout", Step: "promote", Symptom: "no background advancer and no TTL: canaries wait for the operator with no deadline", FixTodo: "DURABLE-006"},
		{ID: "F-gate-consumer", Process: "channel-gate", Step: "deliver-event", Symptom: "the gate outbox consumer implementation is ungrounded: redelivery relies on cursor replay the record cannot verify", FixTodo: "DURABLE-002"},
		{ID: "F-stop-cancel-lost", Process: "feedback-stop-retry", Step: "stop", Symptom: "the cancel key is validated but never persisted: a death before the CAS loses the cancel and the run continues", FixTodo: "DURABLE-003"},
		{ID: "F-feedback-card-stuck", Process: "feedback-stop-retry", Step: "surface-failure", Symptom: "the failure row does not finish an invocation that has a run: a dead run leaves a stuck card", FixTodo: "DURABLE-005"},
		{ID: "F-prepare-version-split", Process: "preparation-command", Step: "version-split", Symptom: "PutVersion and AppendLifecycle are separate writes with ungrounded recovery across the split: possibly stuck", FixTodo: "DURABLE-003"},
	}
}

// RenderDurableInventory renders the inventory and its findings as text.
func RenderDurableInventory() string {
	var out strings.Builder
	out.WriteString("DURABLE-001 crash inventory (code-read 2026-10-22)\n")
	out.WriteString("Shared substrate: runstate leases + fences (run.go), effect journal with UNKNOWN-first intents, core outbox with 30s IN_FLIGHT leases, jobs watermark checkpoints, idempotencystore first-claim-wins.\n")
	out.WriteString("Shared gap: agentServedAssembly.TickTenant fans out to announcement/ambient/restore/schedule workers but has NO production caller (tests only).\n\n")
	for _, process := range DurableInventory() {
		fmt.Fprintf(&out, "## %s: %s\n", process.ID, process.Title)
		for _, step := range process.Steps {
			fmt.Fprintf(&out, "- %s\n", step.Name)
			fmt.Fprintf(&out, "  state: %s\n", step.State)
			fmt.Fprintf(&out, "  durable: %s\n", step.Durability)
			fmt.Fprintf(&out, "  key: %s\n", step.IdempotencyKey)
			fmt.Fprintf(&out, "  resumer: %s\n", step.Resumer)
			fmt.Fprintf(&out, "  die-before: %s\n", step.DieBefore)
			fmt.Fprintf(&out, "  die-during: %s\n", step.DieDuring)
			fmt.Fprintf(&out, "  die-after: %s\n", step.DieAfter)
			fmt.Fprintf(&out, "  visible: %s\n", step.Visible)
			fmt.Fprintf(&out, "  resume: %s\n", step.ResumeTime)
		}
		out.WriteString("\n")
	}
	out.WriteString("## findings\n")
	for _, finding := range DurableFindings() {
		fmt.Fprintf(&out, "- %s [%s/%s]: %s (fix: %s)\n", finding.ID, finding.Process, finding.Step, finding.Symptom, finding.FixTodo)
	}
	return out.String()
}
