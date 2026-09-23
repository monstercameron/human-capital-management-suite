# Channel and Team Documentation Hub

**Status:** DRAFT_CONTRACT  
**Owner:** Knowledge/Content and Collaboration Product owners, with AuthZ, Records, Experience, and Search review  
**Started:** 2026-09-21  
**Review trigger:** Before official publication, document database deployment, cross-company sharing, semantic indexing, or agent retrieval

## Product decision

The hub stores Markdown-native documents with immutable versions. A user
cannot edit a deployed document in place. They create a new candidate
version, review it where required, and deploy that version. Deployment
atomically changes the active version pointer for its scope and emits a
versioned publication event. A document's stable ID survives every deploy.

Channels and teams have Docs tabs for endorsed documents. Every employee
may create a private document and explicitly share it with people, a team,
or a channel. A chat link does not grant access. The Knowledge module owns
document truth; Collaboration owns placements in channels/teams; core
routes document and chat APIs. Documents have their own database, separate
from the chat-message database and the core/workflow database.

This is a small-organization knowledge hub for fewer than 1,000 employees:
Markdown pages, document links, review/deploy, history, access grants,
hybrid search, and clear ownership. It is not a generic file drive,
legal-signature service, live coediting engine, or shortcut to publishing
regulated HCM policy. The platform responsibility register currently
defers general Knowledge management beyond a small approved
Promotion-policy corpus. Near-term delivery requires a scope exchange and
acceptance gate; no served document product is claimed here.

## User experience

```text
Workspace hub
  My documents       private documents and candidate versions
  Shared with me     explicit person/team grants
  Team spaces        official team documents and review queue
  Channels           Docs tab beside Messages, Files, Members
  Search             documents + messages as separate result types

Document page
  title · owner · active version · deployed at · review due
  status: Personal / Shared / Team official / Channel official / HCM policy
  Markdown body · outgoing links · backlinks · comments
  version history · compare · propose new version · share · deploy
```

The editor works on a candidate version. Save creates another immutable
candidate revision, not an update to existing bytes. The UI may autosave
local working text, but the server stores each submitted candidate as an
append-only version with an expected base version. Concurrent authors see
a compare/merge conflict and can create a new candidate; the system never
silently overwrites either author. The reader sees the deployed version by
default. A document link may pin a historical version when a stable
citation is needed. Renaming a document or changing its Markdown links or
embedded assets also requires a new version and deployment.

For Markdown, support a bounded CommonMark/GFM-style subset: headings,
lists, tables, code fences, links, images by protected artifact reference,
and accessible alt text. Raw HTML, scripts, arbitrary iframes, executable
includes, and remote image loads are excluded from the served rendering.
The server parses and sanitizes Markdown, bounds document size and nesting,
and stores the exact normalized source bytes and content hash. Rendering
is deterministic for a given version and renderer profile. Imported
Markdown passes the same validation and DLP checks as authored content.

## Data and deployment model

| Entity              | Required fields and invariants                                                                                                                                                                                                                                                    |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Document`          | Stable ID, tenant, owner principal or team, home (`PERSONAL`, `TEAM`, `CHANNEL`), lifecycle, classification ceiling, and retention series. Content lives in immutable versions; active pointers are separate.                                                                     |
| `DocumentVersion`   | Immutable version ID, parent/base ID, creator, created time, normalized Markdown, title, locale, classification, embedded artifact references, extracted outgoing links, content hash, renderer profile, and change note. Version rows are append-only.                           |
| `Deployment`        | Document ID, version ID, default or placement scope, reviewer decision, deployer, effective time, optional expiry/review due date, policy snapshot, and prior deployment ID. A deploy creates a record and moves that scope's active pointer atomically in the document database. |
| `DocumentGrant`     | Subject (person, team, channel, or allowed company), action set, issuer, purpose, expiry, revision, and revocation state. Grants are mutable policy records with history; they never mutate content versions.                                                                     |
| `DocumentPlacement` | Team or channel ID, `REFERENCE`/`PINNED`/`OFFICIAL` state, order, and the endorsed deployment pointer. A document may have several placements, each with its own approval and audience path.                                                                                      |
| `DocumentLink`      | Source version and block, target stable document ID, optional pinned target version and block, link label, validation state. Rebuilt from Markdown on every version creation.                                                                                                     |
| `DocumentComment`   | Author, document/version/block anchor, text, resolution state, and its own immutable revision history. Comments do not change Markdown content.                                                                                                                                   |

Normalized Markdown is the canonical body. Derived HTML, previews,
plain-text extraction, link graph, embeddings, and search documents are
rebuildable. Binary attachments stay in protected object storage and are
referenced by immutable artifact IDs. Document data, grants, deployments,
link graph, search index, vector embeddings, and document outbox live in
the document database or its owned object namespace. It has separate
credentials, pool, migrations, backups, worker budget, and recovery from
both chat and workflow storage. The first deployment can still share the
Go binary and core edge; a separate document service is optional.
Core's route directory holds only document ID, host tenant, document shard,
route epoch, and state; it does not store Markdown, grants, or vectors.

Deploy uses compare-and-swap against the current deployment ID for its scope, checks
the candidate hash, current access, reviewer decision, classification,
link status, and retention policy, then commits the new deployment pointer
and outbox event in one document-database transaction. Notifications,
search indexing, embeddings, and chat-tab refresh follow from that event.
If those consumers lag, the canonical document read still returns the new
deployment. A failed deploy leaves the previous version live. Rollback is
a new deployment pointing to a previously stored version, with fresh
authorization and review where required; historical records remain intact.

## Access model: who may do what

Rights are capabilities, not a single `EDIT` bit:

| Capability                      | Personal document                             | Team/channel official document                                                                                                                   |
| ------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `READ_DEPLOYED`                 | Owner and current explicit readers.           | Current authorized team/channel audience or named readers, subject to document policy.                                                           |
| `READ_HISTORY`                  | Owner and explicitly delegated collaborators. | Owners/reviewers and readers only if the version-history policy allows; a current grant does not automatically expose every past classification. |
| `PROPOSE_VERSION`               | Owner or named contributor.                   | Named contributors or maintainers. Channel membership alone never grants authoring.                                                              |
| `REVIEW_VERSION`                | Optional for personal sharing.                | Named reviewer with current team/channel authority; different principal from the candidate author by default.                                    |
| `DEPLOY_VERSION`                | Owner may deploy personal versions.           | Team/channel publisher or manager, after required independent review of that exact version/hash.                                                 |
| `MANAGE_ACCESS`                 | Owner or explicitly delegated manager.        | Team/channel document manager under placement policy; cannot override mandatory denies or cross-company grant limits.                            |
| `PLACE_OFFICIAL`                | Not available from a personal link alone.     | Authorized team/channel manager plus required review and audience validation.                                                                    |
| `COMMENT` / `EXPORT` / `RETIRE` | Separate grants and tenant controls.          | Separate grants, records policy, and audit; admin recovery is a scoped break-glass path, not blanket private read.                               |

At every operation the server combines active tenant, principal/session,
classification, mandatory denies, current grants, placement rules, and
action capability. Grant paths are alternatives, each with its own
constraints: a channel grant requires current channel read access; a team
grant requires current team eligibility; a named-person grant requires
that principal. A direct reader may open the document without learning a
private channel's name. Roles, verified qualifications, and allowlists may
further restrict the document or inherited channel path. A link or search
hit never grants access. Current authorization is rechecked for history,
comments, attachments, exports, every search result, and agent retrieval.

Candidate versions are visible only to their proposers, maintainers,
reviewers, and other explicit draft collaborators. Ordinary readers see
the active deployment. Revocation stops future reads and removes search,
preview, link-title, agent-index, and cached derivative access within the
specified propagation budget. Old versions and embeddings are not a
back door. Document grants do not silently broaden when the document is
placed in another channel; the share dialog shows the proposed audience.

For a cross-company channel, document access remains off by default. It
requires a bilateral channel grant that includes documents, host approval,
home-company eligibility, document classification/egress checks, and
explicit version/placement scope. External collaborators receive `VIEW`
or `COMMENT` by default; proposing, reviewing, deploying, or resharing
requires a separate named cross-company capability.

## Version lifecycle and official status

```text
PRIVATE candidate -> SHARED candidate -> SUBMITTED -> REVIEWED
       |                    |                 |            |
       +---- new candidate--+---- rework -----+            v
                                                    DEPLOYED -> STALE -> RETIRED
                                                        |
                                              next immutable candidate
```

One document can have many candidate versions and one current deployment
per default or placement scope. A personal owner may deploy a
private version directly. To publish as official for a team or channel,
the manager nominates one candidate, a separate reviewer approves its
exact hash, and an authorized publisher deploys it. The current version
remains live during review. A major error can withdraw a deployment
immediately under a distinct emergency capability and audit; ordinary
typos use a new candidate and deploy.

“Official” means endorsed by that team or channel at that deployed
version. HCM, legal, payroll, or regulatory policy authority is separate:
the hub may link or surface an already authoritative Knowledge/source
publication, but a team manager cannot mint that authority. An owner,
backup custodian, and review date are required for official documents.
Owner departure, team deletion, and channel archive route to transfer,
retirement, or records review rather than making pages public or orphaned.

## Links and document graph

Native Markdown links use a canonical internal target such as
`doc:<stable-id>` for the latest authorized deployment or
`doc:<stable-id>@<version-id>#<block-id>` for a pinned citation. An unpinned
link in a channel resolves that channel's endorsed deployment when present;
outside a placement it resolves the viewer's authorized default deployment.
If several grants offer different active versions, the UI requires a
visible scope choice rather than silently switching content. The UI may
offer `[[document title]]` autocomplete, but it stores the stable ID, not
a title or slug. Human-readable URLs are aliases and redirects, never
authorization or identity. Block IDs survive heading renames when possible;
otherwise the link resolver reports a broken anchor without silently
pointing to unrelated text. A pinned version resolves only if that version
was deployed for a scope the current viewer may still access. The renderer
rewrites validated `doc:` targets to safe internal URLs and allowlists
external URL schemes; Markdown text cannot inject executable links.

Version creation extracts outgoing links. Deployment validates that each
target exists and that the intended deployment audience can read it.
An official page cannot present an inaccessible target as a normal link;
its author must fix it, narrow the audience, or mark a clearly restricted
reference that reveals no target title to unauthorized readers. Linking
does not copy or share the target. Backlinks are derived per source version
and shown only when the viewer can read both source and target. Cycles are
allowed as links, but recursive transclusion is not in the first release.
On target retirement, revocation, or deployment, a bounded link checker
updates broken/stale reference status and owner reminders.

## Document search engine

The document database owns search over **all currently authorized
documents**, including a person's private and explicitly shared documents.
Only deployed versions enter general search; candidates are searchable in
a separate collaborator-only draft view. Each indexed record carries tenant,
document ID, version/deployment/placement scope ID, classification, status, locale, owner,
placement/grant revision, and index watermark. Search reads the document
database's current access data rather than trusting an embedding's old ACL.

Use a hybrid engine: PostgreSQL full-text search for exact terms, titles,
phrases, filters, and prefix/fuzzy behavior; semantic embeddings of bounded
Markdown sections for concept matches; then merge and rerank candidates.
Result cards show title, authorized snippet, owner, status, source version,
and why the match appeared. Filters include team, channel, owner, type,
status, date, and locale. Exact title/ID and lexical matches retain a high
priority so semantic similarity does not bury the obvious document.
Keyword search remains usable when embedding generation or vector search
is unavailable.

For the under-1,000-employee envelope, begin with search tables in the
document PostgreSQL database and a vector extension such as pgvector if
dependency review permits it. An external search cluster is an optional
measured scaling move. Start with exact vector ranking over a bounded,
authorized candidate set; introduce approximate HNSW/IVF indexing only
after recall, latency, tenant isolation, and access-filter tests pass.
Approximate vector indexes may apply filters after candidate retrieval,
which can hurt recall; per-tenant partitioning or exact filtered search
may be preferable for small tenants. See [PostgreSQL text search](https://www.postgresql.org/docs/current/textsearch-controls.html)
and [pgvector filtering guidance](https://github.com/pgvector/pgvector/blob/master/README.md#filtering).

An async indexing worker consumes document deployments and revocations.
It chunks the exact deployed Markdown version, strips non-content
navigation, records chunk offsets/heading IDs, model and parser versions,
and the content hash, then creates embeddings under tenant-scoped policy.
No external model receives document text without approved egress and DLP;
a local model is an option. Re-deploy, retire, classification change,
grant revocation, or model migration invalidates or rebuilds derivatives.
Search checks eligibility before returning results, counts, facets,
snippets, suggestions, or backlinks. It never treats vector similarity as
authorization. A stale index may omit a newly deployed document; it must
not disclose a revoked one. Index watermarks and rebuild/reconciliation
status are observable.

Agents use the same authorized search/read API under their own purpose and
the requesting audience. Answers cite document ID, deployed version,
official/personal status, and effective date. A candidate or personal note
cannot be presented as authoritative HCM policy. Search/embedding outages
degrade discovery or agent answers, not workflow execution.

## Storage, records, and scale

The document database has separate connection capacity, credentials,
migrations, backups, and restore tests from chat and workflow databases.
Markdown versions are immutable and bounded; attachments are in a
document-owned protected object namespace. Index, embeddings, previews,
and link graph are rebuildable from versions and deployment events.
Document saves/deploys do not use the chat outbox or workflow timer queue.

For planning, qualify 1,000 accounts, 50,000 documents, 500,000 immutable
versions, 200 simultaneous readers, 30 simultaneous proposers, and bursty
chat traffic. These are test inputs, not predicted customer usage. Measure
open, candidate creation, deploy, keyword and semantic search p95/p99,
index lag, recall on a curated query set, database pool wait, vector index
size, storage growth, and workflow timer/admission latency. Small-tenant
operation should not require a distributed search cluster or CRDT service.
Split document compute or search only when measured throughput, recovery,
residency, or cost evidence requires it.

Versions, deployments, comments, grants, attachments, exports, embeddings,
and other derivatives enter the records inventory. A delete/retire action
creates a tombstone; retention and legal hold govern actual disposition.
Restoring a prior version creates a new deployment record. Export includes
raw Markdown, a version/asset manifest, stable link map, and provenance;
portable HTML/PDF is a derived format. Import passes scanning, parsing,
classification, provenance, and link remapping before any deployment.

## API and acceptance

Canonical RPC and integration HTTP projection need operations to create a
document; propose/list/read/compare versions; submit/review/deploy/withdraw;
grant/revoke; place/unplace; search; resolve links/backlinks; comment;
export; and transfer ownership. A content write creates a new version with
an idempotency key and expected base. Deploy requires expected current
deployment, approved exact hash where applicable, and returns the new
deployment ID. Events carry stable IDs, version/deployment IDs, and policy
revisions; consumers are idempotent.

1. No API, editor, administrator action, or database path updates a
   `DocumentVersion` body/title/link/asset field in place. Every change
   creates a new immutable version; deploy atomically switches the active
   pointer or leaves the previous deployment intact.
2. Personal documents start private. Sharing, chat links, placement,
   official endorsement, and cross-company access require distinct actions
   with an audience preview and current authorization.
3. Channel membership alone permits neither proposing nor deploying a
   version. Team/channel official deploy has a recorded reviewer of the
   exact version hash and an authorized publisher; HCM policy authority
   remains separate.
4. Pinned links resolve the same version after later deploys. Latest links
   resolve the current authorized deployment. Broken, retired, restricted,
   and cross-company targets reveal no unauthorized title or content.
5. Keyword and semantic queries over private, shared, and official docs
   return only currently authorized deployments. Revocation removes
   snippets, counts, backlinks, vectors, and agent retrieval within the
   defined budget; stale indexes cannot leak content.
6. A vector outage preserves keyword search, and a search outage preserves
   direct document reads and deploys. Neither stalls workflow.
7. The planning workload and mixed chat peak keep workflow within its SLO
   and agreed regression budget, with measured search recall and latency.

## Open decisions

- First-release embedding model and approved deployment location, including
  whether any document classification may leave the tenant-controlled
  boundary for embedding.
- Exact candidate-version retention and whether autosaved local text may
  persist across devices before a candidate is submitted.
- Whether non-policy official team documents may use self-review in a
  tenant-configured low-risk class. Independent review is the default.
- Whether version history is visible to all current readers or only
  collaborators/reviewers; older versions may have different sensitivity.
- Initial document database placement: its own PostgreSQL instance or a
  separate logical database on a shared server, subject to measured
  workflow/chat resource isolation.

## Related contracts

[Company Chat and Collaboration Mode](company-chat-and-collaboration.md),
[Core-Routed Chat with Separate Message Databases](chat-core-routing-and-isolation.md),
[Platform Responsibility Boundaries](platform-responsibility-boundaries.md),
[Organization Scope and AuthZ](organization-scope-and-authz.md),
[Records Management](records-management-and-disposition.md), and
[Data Classification and DLP](data-classification-and-dlp.md).

## Review history

- 2026-09-21: Initial channel/team documentation-hub proposal.
- 2026-09-21: Revised for Markdown-native immutable versions, deploy-only
  publication, separate document database, explicit access capabilities,
  document link graph, and hybrid keyword/semantic search.
