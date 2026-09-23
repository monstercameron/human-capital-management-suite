# Core-Routed Chat with Separate Message Databases

**Status:** DRAFT_CONTRACT  
**Owner:** Core Routing, Collaboration, Workflow, Data, and Reliability owners  
**Started:** 2026-09-21  
**Review trigger:** Before chat persistence implementation, message-store sharding, cross-company pilot, or all-employee activation

## Decision

Core owns chat routing. The first implementation may run chat RPC handlers and
workers in the existing Go application, behind the existing gRPC-over-WebSocket
and HTTP edges. Chat messages live in a separate chat database with its own
connection pool, migrations, indexes, retention, backups, and outbox. Workflow
and HCM tables remain in their existing core data boundary. A separate chat
service or network hop is an optional later scaling move, not a prerequisite.

The reason for the separate database is message volume and concurrent chat
workload. Message inserts, reads, search projection, and retention jobs must
not contend for the workflow database's connections, locks, WAL, vacuum,
storage, or backup window. Separate logical databases on one database server
avoid table and connection-pool contention but still share CPU, I/O, and WAL
capacity. A dedicated chat database instance/cluster is required when mixed
load tests show that shared server resources threaten workflow SLOs. For a
large all-employee rollout, budget and test dedicated chat database capacity
up front.

## Logical architecture

```text
Browser RPC/stream              Integration HTTP API
        \                              /
         v                            v
           Core edge and route authority
          conversation + document placement
                       |
          +------------+------------+
          |            |            |
          v            v            v
     Chat module   Knowledge    BusinessIntent /
                   module       workflow module
          |            |            |
          v            v            v
     Chat DB      Document DB    Core/workflow DB
     messages     Markdown       HCM, approvals,
     outbox       releases       timers, outbox
                  links, search
```

The diagram shows module and storage boundaries, not required processes.
Initially the handlers may share a Go binary and deployment. Each module has
its own repository interfaces and credentials. A chat repository cannot open
the core/workflow database, and a workflow repository cannot query messages.
No cross-database joins, foreign keys, or shared outbox tables are used.
Media bytes use a separate protected object namespace; message records hold
references. Search uses a chat-owned projection or index.

The [Channel and Team Documentation Hub](channel-documentation-hub.md) has
its own document database for immutable Markdown versions, deployments,
grants, link graph, and keyword/vector search. The Knowledge module may run
in the same Go deployment, but its repository, credentials, pool, indexes,
workers, outbox, backups, and migrations are separate from both message
and workflow storage. Large attachments remain in protected object storage.

## What core routing owns

The core router resolves an opaque conversation ID to the host tenant and
chat database shard. It authenticates the caller, applies ingress limits,
selects the chat handler/shard, and passes signed or in-process trusted
principal context. Chat still checks current conversation membership and
visibility policy. A route never grants permission by itself. For a
cross-company channel, the host tenant's chat shard owns the canonical
timeline; each participant's home-tenant eligibility and the bilateral grant
are checked before access.
Document IDs route through the same core edge to a Knowledge-owned document
shard. The document database evaluates its grants and deployment scope;
neither a conversation route nor a channel link grants document access.

The route directory stores only `conversation_id`, host tenant, chat shard,
route epoch, state, and placement-policy version. It does not store messages,
member lists, unread counts, search text, or attachment metadata. A tenant
default shard plus explicit exceptions is sufficient initially. Route lookup
is cached and versioned, so sending a message does not query the core
database on every RPC. The route directory may live in the core database;
the message workload never does.

Conversation create crosses the databases. Core reserves an ID and a
`PENDING` route; chat creates the conversation and its initial state
idempotently in the selected chat database; core then activates the route.
A small reconciler resolves a crash between those steps. No post is accepted
through a `PENDING` route. The system does not claim an atomic transaction
across the two databases.

## Chat write and read path

1. The router resolves the chat shard and forwards `SendPost`, `ListPosts`,
   or `WatchConversation` to the chat module. The first deployment may use a
   direct in-process call; later it may use gRPC without changing the client
   contract or ownership.
2. Chat validates identity, current policy, membership, message size,
   classification, and idempotency. Its transaction writes post, ordered
   per-conversation sequence, idempotency record, and chat outbox event in
   one chat database commit.
3. `SendPost` returns the durable post ID and sequence. Chat workers consume
   its own outbox for live streams, unread/search projections, notifications,
   and integrations. Slow consumers resume from bounded cursors.
4. Chat reads use chat indexes/partitions and bounded cursor pages. Search,
   media processing, and retention scans run outside the send path and have
   separate concurrency budgets.

Conversation-local ordering avoids a single global message lock. The shard
key is host tenant plus conversation ID. Start with one chat database if it
meets measured load; add chat database shards when tenant size, write rate,
retention, or residency requires them. A route epoch fences writes during
movement: only the current shard accepts a given conversation's writes.
Changing a shard is a controlled copy, verify, cutover, and drain operation,
never an uncontrolled dual write.

## Concurrency and workflow protection

Go goroutines provide concurrency, but chat work must be bounded. Use
separate chat admission semaphores, database pools, stream limits, and worker
queues. Limit work per tenant and per conversation so a large channel or
reconnect storm cannot monopolize the process. A watcher that falls behind
switches to cursor catch-up; do not accumulate an unbounded in-memory queue
or one expensive goroutine per event. Posts are sequenced by short database
transactions, not by a process-wide mutex.

Workflow timers, approvals, and BusinessIntent processing retain reserved
worker and request capacity. Chat typing, previews, search indexing, agent
inference, webhook callbacks, uploads, and media jobs use chat-owned queues.
Under pressure, shed ephemeral/chat-derived work first, then reject new
chat sends with a retryable overload result. Do not put ordinary chat events
into the workflow ready queue or workflow outbox. An agent-requested HCM
action goes through the typed BusinessIntent API and its normal admission
and priority rules. The chat message never becomes an HCM commit.

Sharing a Go process does leave CPU, memory, garbage collection, and network
as common resources. Admission budgets and load tests are required, and the
chat handlers/workers can move to a separate deployment if those controls
cannot keep workflow within SLO. This scaling move leaves core route
ownership and the separate chat database unchanged.

## Cross-database consistency and failure behavior

Every owner commits its own state and outbox. Cross-boundary commands and
events carry stable IDs and idempotency keys; retries reconcile ambiguous
outcomes. Chat may display a governed HCM proposal as pending while workflow
admission is unavailable, but it cannot report the action as executed.
Workflow outcomes are read through an API or a permissioned event consumer,
never a database join.

| Failure                       | Behavior                                                                                                                                              |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Chat database unavailable     | Chat sends/reads fail or return retryable unavailable; no post is acknowledged without a commit. Workflow continues.                                  |
| Core route lookup unavailable | Valid short-lived cached routes may serve existing conversations under the policy freshness contract. New routes and expired entries fail closed.     |
| Chat outbox/search behind     | Committed posts stay readable; live/search/notification lag is visible and catches up from the chat outbox. Workflow dispatch is unaffected.          |
| Workflow unavailable          | Human chat continues. HCM action requests remain proposed/deferred or fail visibly and retry idempotently.                                            |
| Route migration interrupted   | Route epoch and shard fencing admit one writer. Reconciliation completes or rolls back the move before new writes resume.                             |
| Access revoked                | Router and chat invalidate affected caches/streams and recheck sensitive reads, media, agents, and integrations within the defined revocation budget. |

## Scaling and acceptance gates

Measure message send p95/p99, large-channel fanout, active streams,
connection-pool wait, chat database IOPS/WAL/vacuum, partition size, search
lag, outbox age, retention job load, and per-tenant fairness. In the same
mixed test, measure workflow admission-to-start p95/p99, timer lateness,
approval latency, queue age, and HCM transaction errors against a
workflow-only baseline. Set a permitted regression budget before broad
activation. If a shared database server or Go process exceeds it, isolate
the chat database instance or chat compute deployment before rollout.

Acceptance requires:

1. Chat and core/workflow use separate databases, credentials, migrations,
   connection pools, backups, and outboxes. Repository and runtime checks
   find no direct cross-database query.
2. Concurrent sends to many conversations scale without a global lock;
   retries create one post and a stable per-conversation order.
3. A large channel, reconnect storm, retention scan, search backlog, and
   agent burst do not push workflow beyond its SLO/regression budget.
4. Chat database outage leaves workflow healthy, and workflow outage leaves
   ordinary chat usable.
5. Route create/retry, stale cache, shard migration, and cross-company access
   preserve one canonical host timeline and current authorization.

## Open decisions

- First chat database deployment: its own database on a shared PostgreSQL
  server, or a dedicated instance from the start. The load/SLO gate selects
  the answer; broad all-employee rollout should plan for dedicated capacity.
- Initial shard count and placement rule, based on measured tenant and
  conversation distribution.
- Maximum route-cache and policy-cache age, especially after revocation.
- The threshold for moving chat handlers/workers from the core Go deployment
  to a separately scaled deployment.

## Related contracts

[Company Chat and Collaboration Mode](company-chat-and-collaboration.md),
[Channel and Team Documentation Hub](channel-documentation-hub.md),
[Platform Plane Model](platform-plane-model.md),
[Organization Scope and AuthZ](organization-scope-and-authz.md),
[Workflow Runtime](workflow-runtime.md), and
[HTTP and gRPC Endpoint Contract](http-grpc-endpoint-contract.md).

## Review history

- 2026-09-21: Simplified the earlier service-isolation proposal after the
  clarification that the required separation is databases for message
  volume and concurrent chat processing; a separate service is optional.
