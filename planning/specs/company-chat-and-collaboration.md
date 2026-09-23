# Company Chat and Collaboration Mode

**Status:** DRAFT_CONTRACT  
**Owner:** Collaboration Product and Messaging Platform owners, with Security, Privacy, Experience, and Integration Platform review  
**Started:** 2026-09-21  
**Review trigger:** Before a release-scope decision, cross-company pilot, agent execution, external app installation, or live-call implementation

## Purpose and authority

This document designs a native, Slack-inspired collaboration mode for Human
Capital Management Suite. Employees converse in channels and private chats,
share media and governed HCM references, and work with visible agents. It is a
new product surface over the same identity, governance, BusinessIntent,
capability, workflow, and evidence contracts used elsewhere in the suite.

This is a design proposal, not an implemented-feature claim or a change to the
current P1A/P1B release inventory. The [execution plan](../execution-plan.md)
and [next steps](../next-steps.md) currently defer general chat, agents, and
complex cross-tenant sharing. A near-term release requires a recorded scope
exchange and its own acceptance gate. The [Messaging and Notification Plane](messaging-and-notification-plane.md)
continues to own purpose-bound notices, delivery, and workflow signals;
Collaboration owns participant-authored conversation truth.

The initial product is native HCM Next chat. A Slack or Teams bridge is later
integration work, not the storage authority for this product. All employees of
an enabled tenant are eligible to use the chat product, subject to current
conversation policy. Cross-company channels are in scope for the first chat
release. Live calls are deferred much later.

## Product decisions captured

| Decision           | Design commitment                                                                                                                                         |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Audience           | All employees in an enabled tenant, rather than an HR-only surface.                                                                                       |
| Conversation types | Public channels, private channels, one-to-one direct messages, and private group chats.                                                                   |
| Agent role         | Clearly identified agents participate in conversations and may carry approved HCM actions through governed execution.                                     |
| Sharing            | Links, invitations, and references for chats and channels, including cross-company channels. A link alone grants no access.                               |
| Composer           | `/` discovers tools and commands; `@` mentions people and agents; `#` references visible channels and chats.                                              |
| Extensibility      | A versioned, per-conversation API supports posts, events, commands, membership, settings, and integrations.                                               |
| Media              | MP3/WAV voice messages; BMP/PNG/GIF and other approved images; MP4 video; approved interactive web embeds.                                                |
| Calls              | Peer-to-peer one-to-one audio/video and server-backed team calls are planned later, outside the first chat release.                                       |
| Daily use          | Threads, reactions, search, saved items, pins, drafts, unread state, and configurable notifications make chat useful at work.                             |
| Personal layout    | People can reorder chats, create sidebar sections, and resize supported desktop panes without changing anyone else's view.                                |
| Routing and data   | Core owns chat route admission and placement; chat owns message storage in an independent database.                                                       |
| Documentation      | Channels and teams surface official Markdown deployments; people create private documents and share them deliberately. Documents have their own database. |

## Boundary with existing systems

```text
Employee browser -- gRPC-over-WebSocket --+
Installed app ----- HTTP API ---------------+--> Core edge/router
                                             |    route directory in core DB
                                             v
                                     Chat handlers and policy
                                      |       |       |       \
                                   Chat DB  outbox  search  media
                                      |
                            typed HCM action via core API
                                      |
                                      v
                           BusinessIntent / workflow DB
```

The existing `conversation_thread` and `thread_participant` tables are useful
conversation metadata, but they were built for governed communication replies.
They do not provide a served employee message timeline, channel membership
management, live fanout, or this API. The existing inbox is a recipient notice
feed, not a chat history. The `agentsecurity` package supplies tested security
pieces, but no deployed conversational model service is claimed. Reuse these
contracts where their semantics fit; keep one owner for each new behavior.
The [Core-Routed Chat with Separate Message Databases](chat-core-routing-and-isolation.md)
contract owns the route, database, cross-system consistency, and failure
boundary. Chat never joins, queries, or transacts against core/workflow tables.

## Transport and latency architecture

The employee chat interface uses the project's canonical Protobuf/gRPC
contract through its existing browser gRPC-over-WebSocket tunnel and the core
edge/router. Interactive writes and reads are unary RPCs; a server-streaming
watch carries new posts,
membership changes, and other authorized conversation events. The integration
surface is a versioned HTTP API over the same application service, policy,
chat-owned transaction, and event log. HTTP is an interoperability projection,
not a separate source of chat behavior or authorization. Internal agents call
the same capability boundary. Chat handlers may run in the core Go deployment
while using a separate message database and bounded chat workers. Core remains
the route authority; a separate chat deployment is a later scaling option.

`SendPost` is synchronous through validation, current authorization,
idempotency lookup, and a durable transaction that writes the post, its
per-conversation sequence, and an outbox event. Its response returns the
committed post ID, sequence, and revision. The caller does not wait for every
subscriber, search index, notification, webhook, or agent run. Those consumers
advance asynchronously from the committed event. For a cross-company post,
the response distinguishes host commitment from delivery or acceptance by
another company; it never presents a pending relay as delivered. Retries with
the same key return the same committed result.

`WatchConversation` begins from an authorized snapshot or a signed resumable
cursor. Events are ordered by conversation sequence, bounded per subscriber,
and reauthorized at delivery and replay. On a slow consumer or sequence gap,
the server closes with a catch-up cursor; the client fetches bounded history
and resumes. Revocation terminates the stream and invalidates relevant
cursors. Ephemeral typing and presence may use a separately bounded stream;
they are never durable message evidence. Media bytes use protected HTTP
upload and range-download endpoints, while metadata and attachment references
move through RPC.

The edge reuses the repository's admission, authentication, error, cursor,
and backpressure contracts. Both transports must have the same observable
authorization and idempotency outcomes. The protocol choice alone does not
establish a latency benefit: measure send-commit and watch-delivery p50/p95/p99
under concurrent writers, cross-company relays, slow subscribers, and
revocations before setting release targets.

## Workflow performance isolation

Chat is a high-volume collaboration workload. Routine posts, typing,
presence, unread updates, indexing, previews, media processing, integration
callbacks, and agent inference must never enter the workflow ready/timer
queue or consume a workflow execution lease. Only a typed, authorized HCM
action becomes a BusinessIntent and enters the existing workflow admission
path. The chat response reports that action as proposed, accepted, deferred,
or rejected; it does not wait for workflow completion. Workflow state and
timers remain the authority for progress.

Isolation is enforced at every shared bottleneck:

| Resource        | Chat boundary                                                                                                                                                                                                                                                                                        |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Compute         | Chat handlers and background workers have bounded admission, concurrency, goroutine, CPU, and memory budgets. Workflow workers and timer polling keep reserved capacity. A separate chat deployment is introduced if shared-process load violates the workflow SLO.                                  |
| Database        | Chat and core/workflow have separate databases, credentials, pools, backups, and migrations. Chat transactions touch chat-owned rows only. A dedicated chat database instance is required if a shared server's CPU/I/O/WAL affects workflow. Partition/index message history for its access pattern. |
| Events          | Chat has its own durable outbox/event stream and consumer groups. Chat fanout, search, notifications, and integrations cannot occupy workflow outbox tables, dispatcher slots, or replay budget. Cross-system exchange uses idempotent APIs and events.                                              |
| Network         | Bound live streams and per-tenant subscriptions; slow clients are disconnected to cursor catch-up. Upload, media serving, cross-company relay, and embed proxying have separate bandwidth/concurrency budgets from workflow RPC traffic.                                                             |
| Agents and apps | Model calls, tool discovery, slash commands, webhooks, and callbacks have chat-specific rate, cost, deadline, and retry budgets. Agent-created HCM actions use ordinary workflow criticality and admission; an agent cannot promote a chat event to P0/P1 or bypass tenant budgets.                  |
| Browser         | Chat subscriptions use the separate streaming lane already defined for the production frontend, while workflow navigation and interactive mutations retain reserved foreground capacity.                                                                                                             |

Under pressure, degrade in order: typing/presence and previews; search and
unread refresh; agent responses and app callbacks; then new chat sends with
an explicit retryable overload result. Already committed posts remain
durable. Workflow actions, timer polling, approval processing, and access
revocation retain their reserved capacity. Chat must not retry aggressively
against a saturated shared dependency or turn an agent failure into a
workflow retry storm. Queue depth, age, and admission state are visible to
operators per lane and tenant.

Before enabling chat for all employees, establish a workflow-only baseline
and run mixed workloads with peak channel traffic, reconnect storms, large
channels, uploads, cross-company relay, app callbacks, agent bursts, and
workflow timer/approval peaks. Compare workflow p95/p99 admission-to-start,
timer lateness, completion latency, queue age, error rate, database pool wait,
and throughput to the baseline and its existing SLOs. Set an explicit
allowed regression budget and stop rollout if it is exceeded. Prove chat
overload, search outage, media outage, and chat dispatcher backlog leave
workflow progress and HCM action correctness intact. Use a tenant/cell
rollout, per-lane kill switches, and an immediate chat traffic shed path.

## Conversation and message model

`Conversation` has a stable ID, host tenant, kind (`PUBLIC_CHANNEL`,
`PRIVATE_CHANNEL`, `DIRECT`, `GROUP`), name or participant-derived label,
description, owner, current settings revision, classification ceiling,
retention policy, residency policy, and lifecycle state. A channel has a
discoverability and joining policy. A DM is not silently converted into a group:
adding another person creates a new conversation with a separate history.

`ConversationMembership` identifies a principal and home tenant, role, join
and leave times, source of admission, current policy decision reference, and
history visibility. Membership is distinct from eligibility. Eligibility may
come and go with role assignments, qualifications, allowlist changes, tenant
status, and sharing grants. A membership record never freezes authorization.

`Post` has a conversation-local ordered sequence, immutable post ID, author
principal and home tenant, body in a bounded structured format, parsed entity
references, artifact references, embed references, reply parent, creation time,
and idempotency key. Edits create revisions; deletion creates a visible
tombstone subject to retention and legal hold. A message is committed once
before live fanout and notifications. Search and unread projections are
rebuildable from durable history. Client retries return the original post.
The post schema excludes executable HTML and script. Server-owned parsing,
allowlisted rendering, URL normalization, link-unfurl isolation, and output
escaping apply to human, app, imported, and agent text. Text and structured
references pass classification and DLP checks before commit and again before
cross-company delivery where policy may have changed.

`ShareGrant`, `AppInstallation`, `CommandDefinition`, `Attachment`, and
`EmbedGrant` have separate revisions and owners. Their lifecycle events are
auditable. They do not turn a chat post into an HCM business transaction.

## Everyday collaboration capability map

The first chat release needs a coherent daily-use loop, not just message
transport. The following are product requirements; the exact controls and
quotas are set in the detailed UI and wire contracts before implementation.

| Area                   | Required behavior                                                                                                                                                                                                                                                                                           |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Conversation lifecycle | Create, discover, join or invite, leave, rename where allowed, update topic/description, archive, restore, and transfer ownership. DMs keep stable participant-derived identity; a personal alias does not rename another person's DM. Group chats may have a shared name under an explicit manager policy. |
| Message lifecycle      | Create, read, edit, delete, reply in a thread, react, copy a permalink, and share or forward with source attribution and destination checks. Edits and deletes update search, notifications, unread summaries, pins, agent context, and integration events.                                                 |
| Composition            | Formatting, emoji, code blocks, links with safe previews, paste and drag/drop attachments, per-conversation drafts, and keyboard-first command/mention/channel pickers. Drafts survive navigation but obey logout and access-loss cleanup.                                                                  |
| Focus                  | Per-person unread and mention counts, mark unread/read, followed threads, saved items and reminders, mute and notification preferences, quiet hours, and a consolidated activity view. Mandatory HCM notices remain governed by the notification plane.                                                     |
| Find                   | Authorized search across messages, people, conversations, and released files, with filters for conversation, author, date, type, and thread. Results jump to the exact post and surrounding context only when the viewer can still read them.                                                               |
| Organize               | Star/favorite, hide, mute, custom sidebar sections, drag or keyboard reorder, per-section sort/filter, and a quick conversation switcher. These are personal preferences unless an administrator publishes an optional channel collection.                                                                  |
| Context                | Pins, shared files and links, a Docs tab, conversation topic, members, installed apps and agents, and authorized HCM cards appear in a conversation details surface. Pinned resources and workflow entry points may be organized as reorderable tabs.                                                       |
| Communication          | Presence/status and typing are optional, short-lived indicators; profile identity is clear, including home company in a shared channel. Scheduled posts and announcement-only channels remain explicit release choices.                                                                                     |
| Onboarding             | Directory search, an explainable channel catalog, invitation status, join/leave guidance, and optional administrator-curated channel collections help employees find the right place without exposing restricted names.                                                                                     |
| Workflow               | A conversation can feature governed HCM workflows, commands, or app actions with visible execution state. Lists, polls, and channel templates are possible later extensions with their own access and record rules.                                                                                         |

Message mutation uses a post ID plus expected revision. Authors may edit or
delete their own posts only within tenant-configured policy; a moderator may
act under a separate, audited grant. An edit creates a new immutable revision
with an `edited` marker and changed entity/attachment references. A delete
removes the post from ordinary views and creates a tombstone; preserved record
copies follow retention and legal hold. Edits must re-run validation,
classification, DLP, mention/command parsing, and cross-company disclosure
checks. An edit never retroactively executes a slash command or repeats an
agent/HCM action. A deleted root retains enough non-sensitive structure for
authorized thread replies to remain navigable. Bulk moderation, restoration,
and attachment removal require separate capabilities and audit events.

Drafts, read position, stars, saved items, sidebar order, pane sizes, and
personal aliases are recipient-owned state. Conversation names, topics,
members, pins, tabs, and app installations are shared state with distinct
management rights. The UI must make that distinction clear before a change.

## Documentation hub in channels and teams

The [Channel and Team Documentation Hub](channel-documentation-hub.md) defines
the Docs tab, official team/channel Markdown deployments, private personal
documents, immutable versions, deliberate sharing, document links, hybrid
keyword/semantic search, agent retrieval, and the under-1,000-employee
scaling profile. Documents use their own database, distinct from chat and
workflow storage. A document is a Knowledge-owned object with a stable ID;
placing or referencing it in a channel does not turn the chat post into its
authoritative body or silently grant channel members access. An official
channel document names its endorsed deployed version and reviewer.
Regulated HCM policy remains subject to its separate source-authority
process.

## Visibility, membership, and history

Every discover, join, read, search, subscribe, post, mention, share, and API
management operation re-evaluates current server-side authority. The decision
combines tenant status, host/consumer sharing grants, channel policy,
membership, principal role, qualifications, explicit allowlist, mandatory
denies, and action-specific permissions. Roles come from durable effective
assignments; qualifications come from verified effective-dated facts, not a
profile string or credential claim. Unknown or unavailable facts fail closed
for a restricted channel.

Policy authors choose permitted role IDs, qualification requirements, and
explicit principal IDs. The exact AND/OR composition rule remains open; until
decided, an allowlist entry must not be assumed to override a required
qualification or a mandatory deny. A channel may distinguish `DISCOVER`,
`JOIN`, `READ_HISTORY`, `POST`, `INVITE`, and `MANAGE`. A private conversation
name, member list, message count, mention suggestion, search hit, and timing
must not reveal its existence to an unauthorized caller.

On eligibility or membership loss, live delivery stops, subscriptions close,
cached previews and search projections are invalidated, and subsequent reads
are rechecked. Whether earlier messages remain readable after loss of a role
or qualification is an open product policy. The stored history-visibility
choice (`NONE`, `FROM_JOIN`, or `FULL_HISTORY`) is an input, not an override of
current legal, privacy, tenant, or channel denials.
An already-queued event or valid-looking resume cursor grants no grandfathered
read: emission and replay reauthorize the exact recipient and payload. The
release must set and measure a maximum revocation propagation delay, including
other replicas, app subscriptions, push providers, caches, and media sessions.
If current authority cannot be resolved, restricted reads and deliveries stop.
Search authorization applies before ranking and to counts, facets,
suggestions, snippets, and timing-sensitive responses, not merely returned
post bodies.

## Cross-company channels

A cross-company channel has one host tenant and explicit participating tenant
grants. The host proposes a bounded share; an authorized administrator for
each consumer tenant accepts it. Grants specify conversation, participating
tenant, purpose, permitted actions, classifications, residency/retention
terms, effective interval, and whether resharing is prohibited. Each tenant
may impose stronger restrictions and revoke its participation. A public
channel is public only within its admitted audience, never across all tenants.

The host's database ownership and row-level isolation remain intact. A
federation boundary returns only currently authorized conversation data to
another tenant; consumer callers do not bypass host RLS with a guessed tenant
or conversation ID. Home-tenant eligibility can be attested as an allow/deny
result without exposing the worker's underlying role or qualification record
to the host. Revocation stops live delivery and API access within a defined
propagation budget. Cross-company attachment retrieval and embeds are egress
decisions, not merely message reads. Data ownership, legal hold, export,
deletion, and residency obligations must be agreed before activation.

## Composer and references

The composer stores structured references, not text that happens to look like
an authority-bearing token. Suggestions are server-filtered to the sender's
current audience, and the server resolves every selected ID again on commit.

- `/` opens a command catalog from active capability and installed-app
  manifests. Commands have typed arguments, preview/confirmation behavior,
  risk class, and action-specific authorization. Quoted text, code blocks,
  imported content, and agent output do not auto-invoke commands.
- `@` selects people or agents. The post stores a principal ID and display
  snapshot; notifications are sent only to currently eligible recipients.
  A typed `@agent` mention may trigger an agent run under its own grant.
- `#` selects a visible channel or chat by stable conversation ID. A private
  target is never suggested to outsiders. Viewers who cannot open a referenced
  conversation receive an inert, non-revealing reference.

Sharing a conversation creates an access-checked link or an invitation flow.
Sharing a post into another conversation creates a new post with source
provenance only after the destination's disclosure policy permits it. A link
never confers membership, qualification, HCM data access, or agent authority.

## Chat interaction design

The desktop workspace has a conversation rail (channels, DMs, unread and
mentions), a message timeline, and an optional context panel for members,
files, links, and governed HCM cards. The mobile workspace uses one pane at a
time with a persistent return path. The composer supports the three token
pickers, attachments, voice recording, and explicit command review. Cross-
company conversations show the participating companies and applicable
sharing boundary before a user posts or shares sensitive content.

On desktop, people can resize the conversation rail, timeline, thread pane,
and context pane within accessible minimum and maximum widths; collapse and
restore secondary panes; and open a thread or another conversation alongside
the current one. Layout persists per person and device class. Keyboard
controls provide the same reordering and resizing as drag interactions, and
zoom or narrow viewports never hide the composer or active approval state.
Conversation ordering supports manual order as well as recent, unread, and
alphabetic views. A shared channel rename uses the manager permission and
updates references by stable ID; personal sidebar labels remain private.

Proposed desktop layout (content and names are illustrative):

```text
+----------------------+--------------------------------------+----------------------+
| Search / new chat    | # workforce-operations      Members | Conversation details |
|                      | Hosted by Company A · Company B      | Members and agents   |
| Channels             +--------------------------------------+ Shared files         |
| # announcements      | Rafael: @Benefits Agent, explain... | Links and embeds     |
| # workforce-ops  3   | Benefits Agent: cited answer        | Pinned HCM cards     |
| # private-leads      |   [Review proposed action]           | Sharing policy       |
|                      | Thomas: #policy-updates              |                      |
| Direct messages      |   [Open authorized reference]        |                      |
| Adrian               |                                      |                      |
| Benefits Agent       +--------------------------------------+----------------------+
|                      | Message...  / tools  @ people  # chats  + media  mic         |
+----------------------+-------------------------------------------------------------+
```

At narrow widths the rail, timeline, and details are separate navigable
views. A conversation opens to its timeline with the composer reachable
without horizontal scrolling. A cross-company badge remains visible in the
header and composer. The client does not optimistically display a post as
committed until the server returns its durable ID and sequence; a pending
local draft is labeled as such and can be retried with the same idempotency
key.

Primary interaction flows:

1. **Find or share:** search reveals only eligible conversations; selecting
   one loads an authorized page. A share action copies a locating link or
   starts an invitation, with the required host and home-company approvals.
2. **Compose:** choosing `/`, `@`, or `#` opens a keyboard-accessible picker.
   The review state shows resolved targets and any cross-company disclosure.
   Commit persists the post, then notifications and stream events fan out.
3. **Attach or embed:** a draft shows quarantine progress. Only a released
   artifact or approved embed can be committed. A refusal explains the
   actionable reason without exposing scanner internals or private policy.
4. **Ask an agent:** a permitted mention starts a bounded run. The agent's
   response cites readable sources, separates inference from facts, and
   offers a proposal card when an action is possible. The card links to the
   authoritative approval and execution states.
5. **Lose access:** the timeline, search, playback, and live stream move to
   one non-disclosing unavailable state. The client clears cached private
   content and discards or cryptographically seals any unsent local draft;
   it cannot display or post that draft after logout or access loss without
   renewed authentication and current conversation authority.

Messages distinguish human, installed-app, and agent authors. A governed HCM
card shows source, status, freshness, and next authorized action; it fetches
protected details on open rather than embedding payroll, medical, case, or
other sensitive fields into a broadly visible post. An agent proposal shows
the exact intended action and approval state. Execution and reconciliation
updates link to the authoritative intent/workflow record.

Keyboard use, screen-reader labels and announcements, focus restoration,
responsive layout at 390 px and 320 px, reduced motion, en-US/de-DE/RTL ar,
and explicit unavailable/reconnecting states are release requirements.

## Agents and HCM actions

An agent is a separately identified participant installed into named
conversations. Its grant limits tenant, companies, conversations, purpose,
tools, data classes, autonomy ceiling, event/mention triggers, rate, cost,
and lifespan. It may see only content authorized for that agent and purpose.
Conversation text, files, embedded pages, and tool results are untrusted
inputs subject to taint, prompt-injection, DLP, and output checks.
Before an agent posts a derived answer, the server checks the disclosure
against the conversation's full authorized audience, classification ceiling,
cross-company egress rules, and future-member history policy. If a fact is
not safe for that audience, the agent sends a private answer or an HCM card
whose details are independently authorized on open. Source permission held
by the requesting person or agent never becomes channel-wide permission.

An agent may explain, analyze, propose, and initiate a governed action within
its ceiling. For a material HCM write, the agent produces a typed draft bound
to exact arguments, source evidence, model/tool versions, and uncertainty.
The existing Capability Gateway, BusinessIntent, simulation, current
authorization, human approval where required, deterministic workflow,
observation, and reconciliation perform execution. The agent never approves
its own request or writes domain tables. Approval must bind the material
proposal; the meaning of an in-chat confirmation remains an open decision.

Posting as an agent is itself a governed communication side effect. Agent
messages and HCM outcomes are separately attributed and linked by correlation
IDs. Duplicate triggers, loops among agents, excessive mentions, stale
authority, and budget exhaustion stop safely and remain diagnosable. A kill
switch disables one agent or the tenant's agent fleet without disabling human
chat or deterministic HCM operations.

## Plugins, apps, agents, and conversation customization

An app/plugin has a versioned manifest declaring commands, event subscriptions,
interactive cards, conversation tabs, bot identity, network callback origins,
requested scopes, and data residency/retention needs. Installation is scoped
to a tenant and named conversations, shows the effective permissions to an
authorized manager, and records an approver. Users can inspect which apps and
agents participate in a chat and remove or report an installation. Upgrade,
scope expansion, suspension, revocation, secret rotation, and uninstall each
have defined lifecycle events; an upgrade cannot silently gain authority.

Plugins may add a tool in `/`, a message action, a context tab, or an
interactive card. Every interaction has a typed payload, current actor and
conversation context, idempotency key, bounded execution time, and a visible
result or failure. Third-party UI code does not run with the chat origin's
credentials. Extension tabs and embeds use the isolated embed policy; cards
use a server-owned component schema. A plugin cannot read all history merely
because its command is installed. Event subscriptions and backfills are
separate grants and are rechecked on delivery.

Agents are installed and listed using the same discovery and lifecycle
surface, with extra model/tool policy and HCM action controls. A person can
DM an available agent, mention it in an admitted channel, inspect its status
and capabilities, and distinguish its authored messages from app notices or
human posts. Autonomy settings specify trigger sources, eligible
conversations, rate/budget ceilings, escalation, and pause controls. The
agent's memory or summaries cannot retain conversation data beyond the
conversation's current access and records policy.

The integration HTTP API, webhook/event feed, command callbacks, and native
RPC path share the same post revisions, conversation settings, app lifecycle,
and authorization decisions. API clients can create, read, update, and delete
past posts only within their author/moderator grant, policy window, and
retention constraints. They can manage memberships and settings only within
their delegated scope. The contract must specify edits, tombstones,
reactions, pins, tabs, unread state, app installations, agent controls,
pagination, optimistic revisions, rate limits, and event replay; a bare
post/send API would not support a complete integration ecosystem.

## Per-conversation HTTP API and extensions

The integration HTTP API uses conversation IDs as resource scopes, rather
than issuing a long-lived secret or a custom server per chat. Proposed
operations:

```text
POST   /v1/conversations
GET    /v1/conversations/{id}
PATCH  /v1/conversations/{id}                  settings, If-Match required
POST   /v1/conversations/{id}:archive
POST   /v1/conversations/{id}:restore
GET    /v1/conversations/{id}/members
GET    /v1/conversations/{id}/posts             bounded cursor page
POST   /v1/conversations/{id}/posts             idempotency key required
GET    /v1/conversations/{id}/posts/{post}
PATCH  /v1/conversations/{id}/posts/{post}      edit, If-Match required
DELETE /v1/conversations/{id}/posts/{post}      tombstone, If-Match required
POST   /v1/conversations/{id}/posts/{post}/reactions
DELETE /v1/conversations/{id}/posts/{post}/reactions/{emoji}
POST   /v1/conversations/{id}/pins
DELETE /v1/conversations/{id}/pins/{pin}
GET    /v1/conversations/{id}/tabs
PATCH  /v1/conversations/{id}/tabs              authorized reorder
GET    /v1/conversations/{id}/search            authorized bounded query
GET    /v1/conversations/{id}/unread            recipient-owned state
PATCH  /v1/conversations/{id}/read-position     recipient-owned state
GET    /v1/conversations/{id}/events            bounded pull or SSE cursor
POST   /v1/conversations/{id}/members:invite
POST   /v1/conversations/{id}/members:accept
PATCH  /v1/conversations/{id}/members/{member}
DELETE /v1/conversations/{id}/members/{member}
GET    /v1/conversations/{id}/shares
POST   /v1/conversations/{id}/shares            host proposes grant
POST   /v1/conversations/{id}/shares/{grant}:accept
POST   /v1/conversations/{id}/shares/{grant}:revoke
GET    /v1/conversations/{id}/commands
POST   /v1/conversations/{id}/commands:invoke
GET    /v1/conversations/{id}/integrations
POST   /v1/conversations/{id}/integrations      installation grant
POST   /v1/conversations/{id}/integrations/{app}:revoke
GET    /v1/conversations/{id}/agents
PATCH  /v1/conversations/{id}/agents/{agent}    delegated settings
POST   /v1/conversations/{id}/attachments:initiate
POST   /v1/conversations/{id}/attachments:complete
POST   /v1/conversations/{id}/embeds:authorize
GET    /v1/me/chat-layout                      recipient-owned preferences
PATCH  /v1/me/chat-layout                      sections, order, pane sizes
```

The generated Protobuf/gRPC contract is canonical; HTTP bindings follow the
existing endpoint contract. The employee UI calls unary RPCs such as
`SendPost`, `ListPosts`, `ManageMember`, and `UpdateSettings`, and watches a
server stream such as `WatchConversation`. These names are illustrative until
the wire contract is authored. Installed apps can manage membership and settings
in the first API release, but each operation has a distinct resource-scoped
grant. A tenant app may manage only the members and settings delegated to it:
it cannot remove another company's members, relax host policy, change
retention/residency, or enlarge its own scope. Higher-impact settings require
owner approval and a current revision. Apps use scoped machine identity and
revocable installations, not chat-link credentials.
This list is a resource sketch, not a wire contract. Before implementation,
each operation needs canonical Protobuf messages, a capability manifest,
idempotency/revision policy, standardized errors, and HTTP exposure decision.
Search, unread, upload, embed, and event-subscription contracts must be
specified as carefully as post and membership writes. A slash command acts
with the intersection of the invoking person's authority, the installed
app's grant, the conversation policy, and the command capability; installing
an app never enlarges the invoker's HCM authority.

Events carry stable ID, conversation sequence, schema version, actor,
correlation, classification, and payload appropriate to the subscriber's
current grant. Reconnect uses a signed, expiring cursor bound to tenant,
principal/app, conversation, and filters. Delivery is at-least-once with
idempotent consumers; no event exposes a post after revocation. Webhooks,
slash-command providers, and bot callbacks reuse Integration Platform
identity, signing, quotas, retry, and health contracts. Existing machine-
client authentication and public event API work are prerequisites, not
already-served dependencies.

## Attachments, voice messages, and interactive embeds

The first chat release accepts recorded voice messages as MP3 (`audio/mpeg`)
and WAV (`audio/wav` or recognized WAV variant), images including BMP
(`image/bmp`), PNG (`image/png`), GIF (`image/gif`), and video MP4
(`video/mp4`). Other types require an explicit allowlist and a matching
renderer. A recording control, playback controls, upload progress, posters or
thumbnails, text alternatives, caption support, and reduced-motion behavior
are part of the accessible experience. Live media calls are separate.
Businesses also commonly exchange documents and working files. PDF, DOCX,
XLSX, CSV, PPTX, and plain text are candidates for the first release, subject
to a product decision on preview, download-only treatment, size limits,
classification, and whether files may be shared across companies. A file
type is not admitted merely because object storage can persist its bytes.

Upload is authenticated and resumable, then held in quarantine. The server
checks byte signatures and media structure rather than trusting filename or
declared MIME type, scans for hostile content, enforces size/duration and
decompression budgets, classifies and DLP-checks, strips unsafe metadata, and
publishes only an approved derivative. The committed post references an
immutable protected artifact. Every range request, playback, download, share,
and cross-company transfer rechecks viewer and destination policy; ordinary
logs contain no media bytes. Retention and legal hold apply to the original
and derivatives. Current artifact/quarantine libraries do not serve this full
path or recognize all listed media formats.

Interactive web content uses an approved-origin `EmbedGrant` bound to tenant,
conversation, site origin, purpose, classification limit, allowed browser
features, and expiry. The embed runs in a restrictive browser sandbox with
content-security policy and no ambient HCM credentials, workflow context,
hidden channel data, or tool execution. Any `postMessage` bridge has an exact
origin, schema, capability, and per-message authorization check. Link preview
fetches cannot reach private network destinations or leak user credentials.
For cross-company conversations, every participating company's egress and
embed restrictions apply. Unapproved URLs remain ordinary links.
The embed must run on an origin isolated from the HCM application, with
explicit controls for cookies, storage, downloads, navigation, popups, camera,
microphone, clipboard, and fullscreen. Redirects and origin changes require
re-admission. A third-party site may refuse framing; the UI then falls back
to an external link. External content may change after the post is written,
so the record preserves the approved URL, origin, policy decision and any
captured preview digest without claiming the live page is immutable evidence.

## Much-later real-time calling track

One-to-one calls may use peer-to-peer audio/video with signaling and relay
fallback. Team calls use a server-backed media service with bounded capacity.
Both share conversation admission but need distinct live-session identity,
network, encryption, accessibility, incident, quality, and consent contracts.
Recording, transcription, screen sharing, guest admission, moderation,
residency, and retention are separate decisions. Neither call path is a
dependency of the first chat release or of recorded voice messages.

## Failure and acceptance contract

The first release is accepted only when tests and a served-browser scenario
prove at least the following:

1. A role, qualification, allowlist, tenant, company grant, or membership
   revocation stops discovery, history, search, notifications, streams, media,
   embeds, agent reads, and API calls under the chosen history policy.
2. Cross-company invitation and revocation preserve host RLS and each
   participating tenant's mandatory restrictions; a link or app installation
   alone never reveals a private conversation.
3. Concurrent and retried posts receive one sequence and one persisted
   identity; restart/reconnect resumes without loss or invented history.
4. `/`, `@`, and `#` resolve authorized canonical IDs, reject stale or forged
   selections, and do not execute tokens in quoted or imported content.
5. Installed apps can exercise delegated membership/settings operations but
   cannot expand their grant, manage another company's membership, or bypass
   revision and approval rules.
6. An agent can prepare and carry one approved HCM action through the same
   governed path as the ordinary product UI, with no direct domain write,
   self-approval, cross-channel authority gain, or duplicate effect.
7. MP3, WAV, BMP, PNG, GIF, and MP4 uploads are verified and playable only
   after quarantine; mismatched, malicious, oversized, revoked, and foreign-
   tenant artifacts never reach a viewer or agent.
8. Approved interactive embeds stay isolated; origin spoofing, frame escape,
   credential access, unapproved network destinations, and forged bridge
   messages fail closed.
9. Desktop and 390/320 px browser paths, keyboard and screen-reader use,
   en-US/de-DE/RTL ar, reduced motion, and unavailable/reconnect states are
   inspected on the actual served UI.
10. Native RPC and integration HTTP calls produce equivalent post, membership,
    settings, error, and authorization outcomes. A send acknowledgment proves
    one durable commit; a failed fanout is recoverable from the event log.
    Slow or disconnected watchers resume by cursor without loss, duplicate
    effects, or disclosure after revocation.
11. An author and an authorized moderator can edit/delete under their
    different policies; revision conflicts, expired edit windows, held
    records, cross-company disclosure changes, and retried mutations produce
    predictable outcomes. Search, pins, notifications, threads, agents, and
    integration consumers converge on the new revision or tombstone.
12. A person can rename a managed channel, name a group chat, arrange their
    own sidebar, and resize desktop panes. Another person's layout and DM
    labels do not change. The same controls work with keyboard and zoom.
13. Installing, upgrading, suspending, and revoking an app or agent changes
    its commands, tabs, events, and access at the specified boundary without
    residual delivery from a stale subscription or callback.
14. A mixed-load and overload run shows workflow admission, timer lateness,
    approvals, and completion remain within their existing SLOs and the
    agreed regression budget while chat queues, streams, search, media,
    agents, and integrations saturate. Chat shedding does not lose committed
    posts or create duplicate HCM actions.
15. Core resolves and fences every conversation route while chat serves the
    timeline from its independent database. A review finds no chat access to
    core/workflow tables, credentials, outbox, or backup set; route failure,
    retry, movement, and cross-company admission pass the separate
    architecture contract's acceptance scenarios.

Capacity qualification must use mixed-tenant and cross-company workloads,
large and small conversations, concurrent writers, attachments, unread/search
updates, live subscribers, and revocations. Set latency, retention-growth,
bandwidth, storage, and cost budgets from measurements before all-employee
activation; existing inbox benchmarks are not chat capacity evidence.

## Business gap audit (2026-09-21)

The initial draft names security and records obligations but does not yet
specify the operating controls below. A release gate means a tenant-wide or
cross-company launch needs a concrete owner, contract, and test. A product
choice needs a scope decision before the release inventory is frozen. Later
work is useful but does not block the first chat release by default.

| Priority       | Gap                               | Required design decision or behavior                                                                                                                                                                                                                                                                                                           |
| -------------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RELEASE GATE   | Product proof and scope exchange  | Name the design partner, the HCM work chat improves, adoption and reliability targets, migration cost, and the work displaced from the current promotion pilot. Native chat is a separate operating commitment; library completeness is not evidence that employees will adopt it.                                                             |
| RELEASE GATE   | Employee lifecycle                | Define SSO/federated sign-in, employee provisioning, leave/transfer/termination, suspended accounts, agent/app identity, session and device revocation, and how membership and unread state change. A terminated user must lose streams, media, embeds, and API access without waiting for a token to expire.                                  |
| RELEASE GATE   | Channel administration            | Define who creates, renames, archives, transfers ownership of, and restores channels; who approves cross-company participation; and how orphaned channels and departed owners are handled. Admin inspection of private conversations needs its own authority and evidence.                                                                     |
| RELEASE GATE   | Records and discovery             | Define retention schedules by conversation type and tenant, legal hold over posts, edits, tombstones, reactions, files, voice/video, embeds and agent output, reproducible export, privacy requests, and the host/consumer company responsibilities for each. User-visible deletion and preserved compliance copies must have distinct states. |
| RELEASE GATE   | Abuse and workplace safety        | Add report/block/mute controls, spam and bot-rate response, harassment escalation, moderation authority, and a protected HR/case route. Moderation must not turn general chat into unrestricted access to confidential employee-relations material.                                                                                            |
| RELEASE GATE   | Notification policy               | Add per-conversation mute, mentions-only/all-message preferences, quiet hours, vacation/absence handling, device preferences, and digest/fallback policy. Mandatory HCM work notices remain separate from optional chat noise and cannot be suppressed accidentally.                                                                           |
| RELEASE GATE   | Reliability and recovery          | Define availability and delivery SLIs, reconnect and offline-draft behavior, backup/restore, regional outage, replay, degraded search/media/agent states, and tested recovery objectives. A chat outage must not leave an approved HCM action in an invented success state.                                                                    |
| RELEASE GATE   | Cross-company controls            | Define organization verification, invitation expiry, bilateral consent, external participant offboarding, information barriers, retention/export conflicts, residency, encryption ownership, incident contacts, and revocation evidence. Distinguish a company inside one tenant from a separate tenant.                                       |
| RELEASE GATE   | Integration operations            | Add app review, install consent, owner transfer, key/token rotation, webhook health and replay, per-chat API quotas, versioning/deprecation, and a tenant sandbox. Full settings management requires explicit separation between host settings and a consumer company's delegated settings.                                                    |
| PRODUCT CHOICE | Conversation policy details       | Threads, reactions, pins, saved items, and edit/delete are now first-release requirements. Set edit windows, moderator rights, reminder delivery, and presence/typing policy. Scheduled posts and announcement-only channels remain release choices.                                                                                           |
| PRODUCT CHOICE | Employee access paths             | Decide whether responsive web is sufficient for all employees or whether native mobile apps, push delivery, managed-device restrictions, and low-connectivity support are release requirements for frontline workers.                                                                                                                          |
| PRODUCT CHOICE | Migration and coexistence         | Decide whether businesses need import from an incumbent chat, directory/channel mapping, historical permissions, dual-running, export verification, and cutover communications. Do not import history without its retention and consent provenance.                                                                                            |
| PRODUCT CHOICE | Media and accessibility           | Set per-type size/duration/storage budgets, thumbnail and transcoding rules, voice-message text alternative or transcription policy, video caption expectations, and treatment of animated content. Cross-company playback may have a stricter egress ceiling than text.                                                                       |
| PRODUCT CHOICE | Business files                    | Decide which office documents, spreadsheets, presentations, archives, and plain-text files can be uploaded, previewed, downloaded, searched, exported, and shared across companies. The first media list covers voice, images, and video, not ordinary business documents.                                                                     |
| PRODUCT CHOICE | Embed catalog                     | Decide whether tenant admins approve origins, individual apps, or exact URLs; how consent and third-party availability are presented; and the fallback when a site refuses framing. Embed interaction must not silently create an HCM or third-party transaction.                                                                              |
| LATER          | Business operations and economics | Define usage analytics, adoption/support dashboards, storage and media cost attribution, chargeback or plan limits, and abuse-related cost alerts once the commercial model is chosen. Analytics must not expose private conversation content.                                                                                                 |
| LATER          | Rich collaboration                | Polls, task boards, advanced presence, translation, and meeting artifacts need separate product evidence; none follows automatically from storing posts.                                                                                                                                                                                       |

This audit is informed by the repository's existing security, privacy,
records, and integration contracts and by current business-chat controls in
[Slack's notification documentation](https://slack.com/help/articles/201355156-Configure-your-Slack-notifications),
[sidebar sections](https://slack.com/help/articles/360043207674-Organize-your-sidebar-with-custom-sections),
[sidebar resizing](https://slack.com/help/articles/212596808-Adjust-your-sidebar-preferences),
[message editing](https://slack.com/help/articles/202395258-Edit-or-delete-messages),
[threads](https://slack.com/help/articles/115000769927-Use-threads-to-organize-discussions),
[search](https://slack.com/help/articles/202528808-Search-in-Slack.),
[saved items](https://slack.com/help/articles/360042650274-Save-messages-and-files-for-later),
[conversation tabs](https://slack.com/help/articles/32562841868307-Add-and-manage-tabs-in-channels-and-direct-messages),
[Slack's legal-hold documentation](https://slack.com/help/articles/4401830811795-Create-and-manage-legal-holds),
and [Microsoft Teams' shared-channel compliance documentation](https://learn.microsoft.com/en-us/microsoftteams/shared-channels).
Those products illustrate operating questions; they do not define HCM Next's
policy or certify this design.

## Adversarial review verdict (2026-09-21)

The proposed feature set is coherent as a product direction but is not yet an
implementable release contract. The following findings must be closed in
existing plan/spec ownership and executable conformance work before the
affected feature is marked ready:

1. **Cross-tenant authority (blocker):** the current organization/AuthZ plan
   explicitly defers complex cross-tenant sharing. Select the message/file
   ownership and federation model, explain how it preserves tenant RLS, and
   prove two-tenant admission, revocation, retention, export, and restore.
   A share grant alone does not make cross-tenant storage or legal duties work.
2. **Unresolved authorization rules (blocker):** settle role/qualification/
   allowlist composition and post-revocation history before implementing
   membership queries or signed cursors. Define the maximum propagation delay
   and prove in-flight events, stale search indexes, agent context, media
   sessions, and installed apps obey revocation.
3. **Agent execution (blocker):** settle trigger modes and approval semantics.
   Prove the agent's tool authority is the intersection of its own grant,
   invoker/delegation, conversation, tenant, and capability policy. An agent
   reply must be safe for every recipient of that conversation, not merely
   safe for the requesting person.
4. **Public and app API (blocker):** the route sketch needs canonical protobuf,
   capability manifests, authentication, error/idempotency/revision rules,
   app install/revoke operations, event subscriptions, search, unread,
   artifacts, and embeds. Full membership/settings access must be separated
   into host, consumer-company, owner, moderator, and app grants.
5. **Content and records (blocker):** specify text sanitization and DLP,
   office-file admission, immutable message revisions, legal-hold copy
   inventory, cross-company export/deletion, embed snapshots versus live
   external content, and a served upload/quarantine path. Current tested
   libraries do not supply an end-to-end chat content pipeline.
6. **Commercial and operational proof (blocker):** record the scope exchange,
   launch owner, target users and workflow, capacity and cost ceilings,
   migration approach, incident response, and an observed pilot. The first
   all-employee activation must follow these gates, not precede them.

## Delivery sequence and scope exchange

1. **Release decision:** record the work exchanged from current P1A/P1B or
   approve a separate near-term chat gate; assign product, security, privacy,
   API, and operations owners. Do not relabel existing library tests as a
   shipped chat feature.
2. **Conversation core:** tenant-owned posts, revisions, membership,
   authorization, search, retention, API, and live cursor.
3. **Cross-company federation:** bilateral grants, identity mapping,
   eligibility attestation, residency and revocation, then a real two-company
   conformance scenario.
4. **Product experience:** channels, DMs, private groups, composer syntax,
   message lifecycle, threads, search, saved items, sharing, personal
   organization, resizable desktop panes, accessible desktop/mobile UI, and
   notifications.
5. **Media and embeds:** served upload/quarantine/playback for the named
   formats, cross-company egress, and approved isolated web content.
6. **Agent and integration path:** scoped installations, commands/events,
   visible agent participation, governed approval-to-execution handoff,
   kill switch, and adversarial tests.
7. **Later calling gate:** peer-to-peer one-to-one and server-backed team
   audio/video after the chat release has separate evidence and funding.

The order is an implementation dependency sequence, not permission to deploy
an incomplete tenant-wide product. All first-release commitments above must
pass their acceptance gate before general activation.

## Open decisions

- Exact role/qualification/allowlist composition: all configured conditions,
  any matching condition, or an administrator-selected rule mode.
- History after qualification or role loss: none, eligible-period history,
  or a bounded channel-specific choice.
- Agent trigger policy: mention/direct message only, subscribed workflow
  updates, or proactive unsolicited initiation.
- What constitutes approval for agent-carried HCM actions: existing HCM
  approval only, a chat confirmation plus that workflow, or tenant-defined
  preapproval for specified low-risk actions.
- Host and consumer responsibilities for cross-company retention, legal
  hold, export, deletion, and incident response.
- Whether a recording control must transcode unsupported browser-native
  capture formats to MP3/WAV or may attach the original as an additional
  approved type.
- Measured p95/p99 targets for send commitment and live delivery, including
  cross-company relay status semantics and overload behavior.
- Message edit/delete time windows, moderator powers and restoration policy;
  exact presence/typing behavior and whether scheduled or announcement-only
  posts enter the first release.

## Related contracts

[Messaging and Notification Plane](messaging-and-notification-plane.md),
[Core-Routed Chat with Separate Message Databases](chat-core-routing-and-isolation.md),
[Channel and Team Documentation Hub](channel-documentation-hub.md),
[Organization Scope and AuthZ](organization-scope-and-authz.md),
[Capability Registry](capability-registry-and-lifecycle.md),
[Experience UI and Branding](experience-ui-and-branding.md),
[Workflow Runtime](workflow-runtime.md),
[HTTP and gRPC Endpoint Contract](http-grpc-endpoint-contract.md),
[Data Classification and DLP](data-classification-and-dlp.md),
[Records Management](records-management-and-disposition.md), and
[Integration Platform](integration-platform.md).

## Review history

- 2026-09-21: Initial design from the planning session. Decisions above are
  user-selected; unresolved policy choices remain explicit. No served
  implementation or release-scope change is claimed.
- 2026-09-21: Business gap scan added release gates and product choices for
  identity lifecycle, administration, records, safety, notifications,
  reliability, cross-company controls, integrations, and adoption.
- 2026-09-21: Independent adversarial review plus owner review tightened
  revocation, search, message/agent disclosure, API completeness, embeds,
  files, and the cross-tenant/scope-exchange blockers. This is a design
  review, not passing conformance evidence.
- 2026-09-21: Added the daily collaboration feature map, historical message
  mutation rules, personal layout and chat organization, app/agent lifecycle,
  and corresponding API and acceptance requirements.
- 2026-09-21: Added workflow performance isolation across compute, database,
  event dispatch, network, agents, and browser scheduling, with mixed-load
  rollout and overload acceptance gates.
- 2026-09-21: Core route ownership and an independent chat database became
  explicit. The separate architecture contract defines route, storage,
  consistency, migration, and outage behavior.
- 2026-09-21: Clarified that the required split is separate message
  databases and bounded concurrent chat work. A separate chat service is a
  measured scaling option, not a starting requirement.
- 2026-09-21: Added the linked documentation hub design for official
  channel/team pages and private, shareable personal documents.
