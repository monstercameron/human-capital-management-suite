# Production Frontend and Governed Page-Composition Plan

## Status and authority

This plan defines the production website, reusable page system, authorization-
resolved presentation model, and customer page-composition program. It refines
the existing experience contract; it does not create business authority, a
second schema, or a second workflow runtime.

The Phase 1 ceiling remains unchanged. Gate A proves the read, draft, and
simulation experience for Promotion. Gate B may add only the separately
authorized Promotion execution path. General customer page composition and the
broader HCM page catalog remain Gate C or later unless a signed release manifest
narrows and admits them.

## Product outcome

Human Capital Management Suite presents one calm, predictable product organized around human jobs
rather than storage tables or product modules. An occasional employee can
finish a task without training; a manager can understand the consequences of a
people decision; a specialist can work efficiently across a queue; and an
authorized customer administrator can compose useful pages without deploying
code or weakening security, accessibility, workflow, or data-governance rules.

The UI is intent-centered:

```text
discover -> orient -> collect/select -> validate -> simulate/compare
         -> review -> confirm -> submit -> work/execute -> track
         -> reconcile -> complete/correct/repair
```

Individual journeys use only the stages they need. The workflow engine and
domain capabilities remain authoritative for state and effects.

## Non-negotiable principles

1. The server resolves what may be discovered, returned, rendered, acted on,
   exported, or configured before data reaches the component tree.
2. Protobuf is the canonical service and payload contract. A page definition
   selects registered presentation mechanics; it never becomes business truth.
3. Go owns production services, rendering, state, routing, and interaction.
   GWC/GoWebComponents is the selected renderer when its qualification remains
   valid; Go SSR is the recovery fallback. Go/WASM progressively enhances the
   semantic server-rendered document.
4. grpcbridge may expose the qualified browser edge. Its admission remains
   conditional; the selected Connect/grpc-gateway fallback preserves the same
   Protobuf behavior.
5. SchemaFlux is not a production generator while its current disqualification
   stands. The deterministic Go compiler operates over the structured sources;
   no model or network call participates in a release build.
6. WebSockets carry filtered invalidation, work-attention, and progress signals,
   not authoritative business payloads. The browser refetches current state
   through the authorized RPC contract.
7. Accessibility, localization, privacy, performance, browser behavior, and
   recovery are publication properties, not post-release polish.
8. Customer customization selects constrained floorplans, regions, widgets,
   bindings, actions, content, and brand tokens. It cannot execute customer
   code, query arbitrary data, create authority, or replace governed controls.
9. Current facts, requested values, simulations, proposals, observations,
   hypotheses, and committed outcomes remain visually and semantically distinct.
10. The browser never infers workflow truth from counters, cached events, or
    client-authored state.

## Participants and operating modes

The product supports five primary operating patterns:

| Participant                      | Usage pattern                            | Default experience                                  |
| -------------------------------- | ---------------------------------------- | --------------------------------------------------- |
| Employee or external participant | Occasional, personal, mobile-heavy       | Guided, reassuring, minimal choices                 |
| Manager                          | Periodic decisions across a bounded team | Guided work plus visible team context               |
| HR/payroll/talent specialist     | Repeated daily queue work                | Compact list-detail workbench                       |
| Customer administrator           | Infrequent high-impact configuration     | Explicit scope, simulation, review, publication     |
| Platform operator/auditor        | Exceptional privileged investigation     | Time-bounded, evidence-heavy, visibly elevated mode |

Guided and workbench modes use the same semantic page definition and action
contracts. The renderer chooses presentation from page purpose, device, task
complexity, and permitted user preference; it does not fork business behavior.

## Stable information architecture

The global shell has six stable destinations:

1. **Home** — attention, recent work, personal essentials, permitted shortcuts.
2. **My Work** — assigned tasks, approvals, drafts, tracked requests, completed work.
3. **People** — authorized worker search, profiles, teams, and contextual actions.
4. **Organization** — organization explorer, positions, headcount, and plans.
5. **Insights** — reports, governed analysis, lineage, and analysis-to-proposal.
6. **Admin** — configuration, security, integrations, operations, and page studio.

Global search and **Start an action** are shell controls, not navigation modules.
Time, pay, benefits, growth, documents, cases, recruiting, and specialist tools
appear through role-relevant hubs, object sections, action discovery, or pinned
workbenches. This keeps the shell stable without exposing irrelevant modules.

## Page anatomy

Every full page resolves in this order:

1. Application shell: product, tenant/company scope, search, navigation,
   attention, locale/accessibility, and account context.
2. Page identity: title, resource/intent identity, status, freshness, and
   page-wide actions.
3. Authority context: acting role, delegation, organization/legal-entity scope,
   confidentiality, purpose, effective time, and elevated-access state.
4. Local navigation: sections for objects, steps for guided journeys, or views
   for collections. Steps and tabs are never represented as the same concept.
5. Primary region: the current job, one dominant hierarchy, and one primary
   action per action group.
6. Supporting region: authoritative current state, explanation, evidence, or
   summary. It reflows below primary content on narrow surfaces.
7. Utility surface: user-opened activity, help, history, attachments, or
   evidence. It is never the only place for required content.
8. Completion layer: review, consequences, save/cancel/continue, and a focus-safe
   final action.

## Floorplan catalog

The renderer admits a bounded set of versioned floorplans:

| Floorplan          | Purpose                                                                |
| ------------------ | ---------------------------------------------------------------------- |
| Launch             | Orient and expose a small number of relevant starts                    |
| Collection         | Search, filter, compare, and select authorized resources               |
| Object             | Understand one worker, position, case, plan, or configuration          |
| Guided task        | Complete a linear or unfamiliar participant task                       |
| Intent workspace   | Draft, simulate, confirm, submit, track, and repair a governed change  |
| Decision           | Review proposal-bound evidence and make an assigned decision           |
| Monitor            | Track workflow, batch, connector, payroll, or reconciliation progress  |
| Expert workbench   | Diagnose and resolve complex exceptions with dense evidence            |
| Analysis           | Inspect governed metrics, lineage, uncertainty, and proposed follow-up |
| Composition studio | Author, preview, validate, review, and publish a customer page         |

Page definitions use semantic primitives (`stack`, `section`, `grid`, `split`,
`tabs`, `summary_rail`, `task_panel`, `table`, `timeline`, `comparison`, and
`disclosure`). They do not contain pixel coordinates, media-query code, raw
HTML/CSS/JavaScript, or arbitrary database queries. The renderer owns responsive
mapping and document order.

## Generic workflow and page catalog

### Shared participant surfaces

- Home, My Work, Action Finder, Draft Center, task/approval detail, intent
  timeline, confirmation, interruption/recovery, and accessible error pages.
- Every attention item explains what needs action, why the participant is
  involved, its safe subject, due/wait condition, and the single next step.
- High-risk decisions never execute from a queue row; they open proposal-bound
  evidence and consequence review.

### People and self-service

- People directory with authorized fields, bounded queries, privacy-safe facets,
  and no enumeration side channels.
- Worker object page with Overview, Employment, Time and Leave, Pay and Benefits,
  Growth, Documents, and Activity sections resolved independently.
- Personal-information journeys for contact, address, emergency contact,
  preferred name, legal identity, bank, and tax changes.
- Worker actions launch semantic capabilities; the object page is not a mutable
  mega-form.

### Employment changes

- One Intent Workspace covers manager, job, position, location, schedule,
  assignment, transfer, promotion, and compensation proposals.
- Current and proposed facts remain adjacent but distinct.
- Simulation exposes exact diff, policy findings, budget/capacity, conflicts,
  approvals, expected effects, source freshness, and uncertainty.
- Track separates business decision, execution, external consistency,
  reconciliation, obligations, and repair.

### Hire and onboard

- Headcount request, position/requisition, candidate pipeline, candidate object,
  offer, candidate portal, onboarding plan, task completion, provisioning status,
  and worker activation.
- Candidate/external shells expose consent, session expiry, saved progress, and
  only the minimum data required for the current invitation.

### Time and leave

- Routine time-off calendar/balance/request/team-coverage path.
- Time-entry, correction, approval, exception, and payroll handoff surfaces.
- Protected leave uses privacy orientation, evidence tasks, eligibility state,
  determination/notice, leave timeline, extension, and return-to-work rather
  than flattening eligibility into approval.

### Pay, rewards, and benefits

- Employee pay summary optimizes for amount, explanation, statement/document,
  discrepancy reporting, period, and freshness rather than dashboard decoration.
- Manager reward proposals reuse the Intent Workspace.
- Compensation-cycle workbench covers frozen population, budget, proposals,
  validation, calibration, submit, approval, publication, and reconciliation.
- Benefit enrollment uses program overview, event, plan comparison, dependent/
  evidence tasks, cost review, confirmation, and coverage status.

### Performance and growth

- Growth home, goals, feedback, check-ins, reviews, skills, learning, career
  opportunities, calibration, and succession workbenches.
- Ratings, observations, evidence, recommendations, and model suggestions keep
  distinct provenance and certainty.

### Organization and workforce planning

- Organization explorer with an equivalent accessible outline, effective-date
  state, occupancy, vacancy, and scheduled changes.
- Position object pages, headcount requests, workforce scenarios, population
  freeze, cost simulation, review, and publication.
- Scenario state never appears as committed worker truth.

### HR help and cases

- Help hub, knowledge search, minimal request intake, confidential route, safe
  participant status, specialist Case Center, evidence, tasks, findings,
  disposition, communication, recusal, and appeal.
- Participant and specialist projections are separate; restricted internal notes
  never leak through layout, counts, notifications, or history.

### Exit and offboarding

- Initiation, notice/policy validation, impact simulation, review, approvals,
  offboarding plan, final-pay state, access/equipment reconciliation, documents,
  completion, and retained obligations.
- A termination is never one destructive form submission.

### Insights, administration, and operations

- Report catalog, certified/customer report distinctions, analysis, definitions,
  lineage, freshness, suppression, export, sharing, and governed proposal start.
- Configuration centers for policies, workflows, reference data, brands, pages,
  integrations, localization, and publication history.
- Operations queues and repair workbenches distinguish observed discrepancy,
  diagnosis, proposed repair, approval, execution, verification, and evidence.

## Authorization-resolved presentation

Authentication creates a `PrincipalContext`; it never grants business authority.
The presentation projection is the intersection of principal/delegation, tenant,
organization and legal-entity scope, relationship, capability, resource,
population, data domain, fields, purpose, channel/context, time, risk, and policy
version.

The resolver independently decides:

- discoverability of the page/resource/action;
- resource and population filter;
- data-domain and field use;
- field disposition (`SHOW`, `MASK`, `REDACT`, `HIDE`, `SUMMARY_ONLY`, or
  `DERIVED_ONLY`);
- allowed read/filter/sort/edit/approve/export/analysis/agent uses;
- current semantic actions and action tokens;
- step-up, review, notice, retention, reconciliation, and evidence obligations;
- explanation detail safe for the current principal;
- expiry, invalidators, watermarks, and policy fingerprints.

Hidden resources do not appear in routes, autocomplete, counts, facets, cache
keys, browser storage, telemetry, WebSocket messages, errors, timing-sensitive
pagination, exports, or source markup. Temporarily unavailable actions are shown
only when their existence is safe and the user can resolve the condition.

Authorization is evaluated at discovery, page/query resolution, draft open,
simulation, proposal creation, task assignment, task open, decision, execution,
result read, export creation, and artifact retrieval. Material actions fail
closed on stale or contradictory mandatory decisions.

Current and proposed organization scopes are evaluated independently. Delegated
authority is visibly identified, purpose/time/capability bounded, non-expansive,
and recorded on every action. Break-glass mode requires explicit activation,
step-up, justification, narrow scope, short expiry, enhanced monitoring, and
post-use review; it is never ordinary impersonation.

## Authentication and session experience

- Prefer customer federation and phishing-resistant authentication where
  available while preserving password-manager, paste, assistive-technology, and
  accessible recovery compatibility.
- Step up at a meaningful risk boundary, not on every navigation event.
- Explain the protected action, work-preservation behavior, and elevated-session
  duration before reauthentication.
- Warn before safe session expiry. Persist drafts without persisting authority-
  bearing action tokens. After authentication, refetch and reauthorize before
  restoring the logical stage.
- Never automatically replay a material action after sign-in or reconnection.

## Customer configurability model

### Level 1: personal presentation preferences

Density, default permitted view, optional columns, sort/filter, expanded
sections, saved searches, and pinned permitted actions. Preferences cannot alter
authority, mandatory disclosures, workflow state, or required controls.

Every durable preference is server-owned and keyed by tenant plus authenticated
principal: locale and accessibility choices, navigation state and favorites,
table page sizes/filters/sorts/columns, saved searches, and workflow-use ranking.
The browser may hold an in-memory optimistic projection while a request is in
flight, but local/session storage is not a source of configuration truth. Writes
use optimistic versions and reload from the authoritative value after conflict.

### Level 2: organization page variants

Authorized administrators may choose optional sections, reorder compatible
regions, set safe defaults, specialize explanatory content, apply qualified
brand packs, and select approved report widgets. Variants inherit from a product
page and migrate through versioned compatibility rules.

Tenant brand and styling choices are stored by the same server preference
boundary, separately versioned from a person's presentation preferences, and
may be changed only by an authorized customer administrator.

### Level 3: customer-composed pages

Experience Studio guides an author through purpose/audience, floorplan, regions,
registered widgets, authorized bindings, permitted actions, visibility
conditions, fixture preview, validation, review, and immutable publication.
Composition is outline-first with visual preview. Drag operations always have
keyboard-accessible move controls.

### Level 4: governed platform controls

Authentication, tenant/authority context, field masking, proposal digest,
confirmation, approval identity, execution/reconciliation truth, evidence,
errors, and accessibility behavior are platform-owned and cannot be replaced.

A page definition declares purpose, audience, required capabilities, resource
types, domains, widgets, actions, classification ceiling, export behavior,
organization applicability, risk, and lifecycle. Publication intersects those
requirements with the author's configuration authority, widget trust limits,
tenant policy, authorization, data classification, accessibility, localization,
performance, and content-safety gates. A published page never freezes permission;
opening it always evaluates current policy.

## Experience Studio and Policy Studio

Experience Studio provides page inventory, dependency/compatibility status,
outline composition, binding inspector, safe content editor, multi-dimensional
preview, findings, semantic diff, reviewer assignment, effective dating,
rollout, rollback, and retirement.

Policy Studio remains separate. It authors versioned subject, capability,
resource, organization, relationship, data-domain, field, purpose, condition,
delegation, deny, and obligation rules. Its simulation shows principals and
resources gaining/losing access, changed fields/actions, inherited denies,
new obligations, separation-of-duty conflicts, and before/after page projections.
A page author is not implicitly a policy author.

## Responsive, accessible, and localized behavior

- Task/prose layouts target a readable 640–760px measure; object/forms use
  roughly 960–1120px; list-detail workbenches may use 1280–1440px; expert tables
  use available width deliberately.
- Every page reflows at 320 CSS pixels without loss of content or action except
  intrinsically two-dimensional material with an accessible alternative.
- Native semantic controls, visible unobscured focus, logical DOM order, useful
  headings, linked error summary/field errors, reduced motion, high contrast,
  zoom, non-color status cues, and accessible authentication are required.
- Touch targets use a comfortable product target above the WCAG minimum.
- Locale changes text, formatting, names, addresses, business time, currency,
  collation, and direction without changing canonical values or legal scope.
- Long translations, right-to-left layout, mixed scripts, time-zone boundaries,
  and accessible generated documents are publication fixtures.

## Frontend runtime and transport

```text
net/http shell + semantic SSR
          |
          +-- GWC component tree
          +-- Go/WASM progressive enhancement islands
          +-- generated static assets with strict CSP and integrity
          |
qualified browser projection (grpcbridge or selected fallback)
          |
canonical Protobuf/gRPC capability services
          |
identity/governance -> workflow -> domain -> ledger/outbox/projections
          |
filtered WebSocket invalidation and attention signals
```

The initial document must remain useful before WASM starts. Hydration preserves
server meaning, focus, form values, errors, authorization dispositions, and
idempotency. Slow or failed WASM falls back to normal semantic form submissions
for critical journeys. Browser caches and storage contain no unbounded sensitive
records or reusable authority. Connection recovery uses sequence/watermark
catch-up and authoritative refetch.

### Asynchronous work coordination

The Go/WASM client treats every finite network operation as scheduled work,
not as an unbounded goroutine launched from a component. The production
composition owns one bounded foreground multiplexer with these rules:

- submission returns immediately so a browser event handler never waits for
  gRPC, a queue slot, or another task;
- interactive mutations run ahead of queued projections, while a bounded
  priority burst guarantees background refreshes eventually run;
- a navigation or search read supersedes and cancels the older read whose
  answer can no longer be rendered;
- repeated submissions of the same non-idempotent action share the original
  completion and do not issue a second RPC;
- queue and active counts are bounded and observable; overload refuses the new
  action visibly rather than hiding it or growing memory without limit; and
- long-lived WebSocket/gRPC subscriptions use a separate streaming lane, emit
  invalidation only, and cannot consume capacity needed by navigation or a
  user action.

The router still owns route-generation cancellation and loading projections.
The task multiplexer controls execution capacity; it does not cache business
truth, infer completion, retry a mutation, or replace server idempotency.

## Content, brand, and widget trust

- Semantic brand tokens resolve through platform, tenant, company,
  organization, and experience scope. Contrast, focus, zoom, high-contrast,
  print/document, and reduced-motion requirements cannot be overridden.
- Governed widgets own workflows, approvals, money, legal evidence, and other
  material actions through strict typed contracts.
- Content widgets accept sanitized allowlisted content only.
- External embeds are sandboxed, origin/egress allowlisted, independently
  authorized, and denied workflow credentials or sensitive context by default.
- Widget registration pins version, input/output schema, classifications,
  supported surfaces, actions, accessibility contract, performance budget,
  lifecycle, and replacement behavior.

## Quality and release evidence

The production website requires deterministic evidence for:

- unit, property, fuzz, race, integration, fault, security, conformance,
  recovery, browser, accessibility, localization, visual-regression, mutation,
  and performance tests as applicable;
- SSR/WASM semantic parity and direct-gRPC/browser-edge parity;
- Chrome, Edge, Firefox, Safari/WebKit, mobile viewport, keyboard, touch, voice,
  NVDA, JAWS where contracted, and VoiceOver qualification;
- page load, WASM size/startup, interaction latency, list virtualization,
  memory, WebSocket fan-out/reconnect, and server-render budgets;
- authorization noninterference across HTML, RPC, logs, traces, metrics, caches,
  events, exports, and timing-observable collections;
- session interruption, reconnect, stale proposal, authorization change,
  rollback, disaster recovery, and inaccessible-enhancement fallbacks.

Telemetry records bounded page/floorplan/widget versions, outcome class,
latency, errors, and correlation/evidence identifiers. It never records tenant,
organization, person, compensation, case, free-text, or field values as metric
dimensions or uncontrolled span attributes.

## Delivery sequence

1. Freeze plan, page/floorplan/widget contracts, semantic tokens, and test
   harnesses.
2. Qualify SSR/GWC/WASM, browser edge, accessibility, security, localization,
   and performance against the Promotion workspace.
3. Deliver shell, Action Finder, People lookup, Promotion Intent Workspace,
   My Work decision, status/timeline, and inspector under Gate A/B authority.
4. Stabilize reusable Home, My Work, People, Organization, Insights, and Admin
   floorplans without broadening execution authority.
5. Add workflow families in dependency order: bounded self-service and team
   changes; hire/onboard; time/leave; rewards/benefits; growth; planning;
   cases; exit; reports and operations.
6. Admit organization variants, then governed customer composition, then agent-
   assembled ephemeral workspaces only after the same validation gates pass.

## Alignment with controlling plans

| Controlling source                                                                   | Alignment and non-expansion rule                                                                                                                                               |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `planning/plan.md`                                                                   | Uses the canonical planes, BusinessIntent model, source authority, workflow, governance, transaction, ledger, reconciliation, and evidence boundaries.                         |
| `planning/execution-plan.md`                                                         | Preserves the P1A zero-effect Promotion slice and separately gated P1B write authority; broader pages remain later-phase contracts.                                            |
| `planning/next-steps.md`                                                             | Keeps signed selection, topology, customer, jurisdiction, and authority decisions ahead of production claims.                                                                  |
| `planning/specs/go-only-technology-constitution.md`                                  | Uses Go, Protobuf/gRPC, PostgreSQL, OTel, qualified GWC/grpcbridge, SSR/fallbacks, and no production JS application runtime.                                                   |
| `planning/specs/experience-ui-and-branding.md`                                       | Expands its PageDefinition, WidgetDefinition, provenance, brand, lifecycle, trust-tier, and publication model without changing its authority boundary.                         |
| `planning/specs/default-product-slice-alignment.md`                                  | Joins each default page and action to its business owner, PostgreSQL disposition, projection, recovery path, and release evidence without exposing tables as product concepts. |
| `planning/user-flows/*`                                                              | Maps UF-A1 through UF-A10 and the initial twenty flows onto a bounded set of floorplans rather than creating a second flow vocabulary.                                         |
| `planning/specs/organization-scope-and-authz.md`                                     | Applies multidimensional, current/proposed, effective-dated, field/population/purpose-aware authorization before rendering and again at action time.                           |
| `planning/specs/governance-decision-and-obligation-composition.md`                   | Treats deny, contradiction, stale authority, and obligations as authoritative workflow inputs rather than presentation hints.                                                  |
| `planning/specs/workflow-runtime.md`                                                 | Presents durable workflow/work-item state and invokes public transitions; no UI-owned counters, scheduling, or forced mutation.                                                |
| `planning/specs/messaging-and-notification-plane.md`                                 | Uses notifications only to draw attention into an authenticated workspace; protected content stays inside it.                                                                  |
| Globalization, legal, privacy, records, accessibility, telemetry, and recovery specs | Treats their decisions and obligations as compilation/publication gates and preserves jurisdiction, purpose, retention, evidence, and restore semantics.                       |

## Research basis

The structure draws on stable usability findings: visible system status,
recognition rather than recall, progressive disclosure, focused questions,
review-before-submit, field-linked error recovery, responsive semantic grids,
enterprise floorplans, and component-based customer composition. WCAG 2.2 and
NIST SP 800-63-4 are normative inputs for accessibility and session design;
OWASP's authorization guidance supports least privilege, deny-by-default, and
permission validation on every request. External patterns inform usability only;
the Human Capital Management Suite authority and workflow contracts remain controlling.

The visual-design sequence and current mockups are tracked in
[Page Design Inventory](../design/page-design-inventory.md).

## Explicit exclusions

- No arbitrary customer HTML, CSS, JavaScript, SQL, WASM, or server plugin.
- No page-level bypass of capability, governance, workflow, transaction, or
  source-authority contracts.
- No client-only field masking, authorization, workflow advancement, or audit.
- No generic visual page builder in Gate A/B.
- No broad HCM execution claim from a page prototype or passing renderer test.
- No accessibility conformance claim without real browser and assistive-
  technology evidence.
- No production use of disqualified SchemaFlux behavior.

## Small-business operations experience (2026-10-22)

The [expansion plan](../small-business-platform-ideas.md) adds role-relevant business workbenches through the existing page, floorplan, widget, action, permission and token registries. Home, My Work, authorized global search, Start an action and pinned destinations discover the enabled capabilities. Adding a domain does not automatically add a permanent global-navigation destination. Domain lists/details use the same Go SSR, Go/WASM, semantic HTML, theme, typography, input, table/list, form, dialog and recovery components as existing pages.

Every implementation issue supplies its concrete page/route, actor, labels and fields, validation, primary/secondary actions, status transitions and test oracle. Shared acceptance includes loading with a bounded outcome, first-use empty guidance, truly empty results, permission denial, dependency outage, offline capture where supported, revision conflict, partial outcome and successful durable completion. A disabled action explains the reason and a safe next step without revealing forbidden records. Server errors preserve authorized unsaved values; destructive/financial confirmations bind the exact revision and scope.

All flows work at 1280x720, 390px and 320px, in light/dark themes, en-US/de-DE/ar RTL and with keyboard-only operation. Labels, error summaries, focus placement/return, announcements, touch target sizing, zoom/reflow and contrast follow the existing accessibility contracts. Drag/drop has an equivalent form action; scans have a manual entry path; calendars have an accessible list/agenda. Structured money, dates, timezones, units and identifiers preserve canonical values while formatting by locale.

External customer/supplier portals consume the same domain capabilities through explicit resource-scoped grants and field projections. They do not reuse an employee session or expose internal navigation, staff notes, costs, payroll, other customers, or cross-company records. Portal-specific framing may simplify the shell without introducing another application stack. Cross-object links obey current authorization at search, preview, opening and action time.
