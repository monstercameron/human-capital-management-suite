# Changelog

## 2026-09-13 (PROMOUX-008)

- The Technical details disclosure had no authorization check at all. Both
  render sites gated on `WorkerRef != "" || InstanceID != ""` -- data
  presence -- so every viewer who could see a journey card received the
  worker entity ref and the instance UUID. Diagnostics is now a first-class
  page id, `journey-diagnostics`, granted to hcm_admin/comp_admin and
  explicitly to promotion_operator, and to nobody else: managers and HR
  partners who can approve a promotion still cannot read its machinery.

- The identifiers are withheld from the payload, not just the view. An
  unauthorized caller no longer receives material digests, correlation ids,
  instance ids, the whole Instance message, planned writes, ledger entries,
  evidence ids, nodes, transitions or work-item ids on any journey RPC.
  Worker ref and intent id are deliberately kept -- they are the routing
  keys the profile link and the journey's own address need.

- Presence was its own leak. Because the disclosure appeared exactly when
  internals existed, a viewer could infer "this journey has an instance"
  from the summary alone; gating only the contents would have left that
  open. Presence now derives from a server-computed authorization verdict,
  so unauthorized viewers see no disclosure at all, uniformly. The detail
  page's workflow, outcome and evidence panels -- which had rendered
  unconditionally whenever they held content -- are gated the same way.

- My Work rendered a raw work-item UUID in its default view on a page with
  no disclosure of any kind. It now appears only inside the authorized one.

- The server gate fails closed: a nil role-access store, an empty permission
  table and a missing grant all deny. That is deliberately unlike the
  existing permissive rolling-upgrade default, because this authority never
  existed before and a permissive default would have handed it to everyone.

- Authorized viewers see redacted values (`••••0020`) with per-row copy
  controls that write the full value to the clipboard.

## 2026-09-13 (UXAUDIT-024)

- This todo closed with no production change, and that is the finding. The
  live audit could not reproduce a single RED clause: identity wraps rather
  than truncating (a 67-character tenant name measured scrollWidth ==
  clientWidth), the nav groups are native disclosures with no inner
  scrollbar at all -- scrolling is one thin edge scrollbar on the whole menu,
  which is what GREEN asks for -- and global search is structurally distinct
  from menu filtering, fuzzy, ranked and typed across people, workflows,
  settings and pages.

- Earlier work had fixed the behavior and never written the six tests this
  todo names. Authoring a suite against behavior that already passes is the
  one case where a green first run proves nothing, so every test was
  mutation-verified against a real break in production code: dropping the
  keyword half of the shared navigation metadata, replacing the native
  disclosure with a div, weakening the search authorization filter, removing
  the search scorer's early exit, forking menu filtering off the shared
  aliases, fabricating a default brand logo, and dropping the global search
  input's id. Each failed naming the thing it covers, then passed on revert.

- Search authorization is now a property over sixteen role bundles resolved
  through roleaccess -- the authority product_shell.go actually enforces --
  rather than through the registry fallback it overrides.

- A latent weakness was found and left in place with coverage rather than
  papered over: the search catalog injects Help and Settings candidates
  unconditionally and relies entirely on the authorization filter to strip
  them. Every real role happens to hold help, so no named-role test can see
  that reliance; the zero-grant bundle stays first in the security matrix.

## 2026-09-13 (UXAUDIT-008)

- The People directory keeps a real, dense table down to 761px instead of the
  shared component's stacked-card mode at 1050px. At 1024x768 -- a desktop
  viewport by this todo's own RED -- each worker had been costing 199px as a
  `display:grid` card; rows are 63px now, and 9 of 20 fit above the fold
  against 0 at the original audit. History and Organization keep the shared
  breakpoint untouched; the change is scoped entirely to `.people-directory`.

- Sticky headers detached because CSS `position` never inherits. The existing
  rules stuck `<thead>` and the header `<tr>`, but a plain `<th>`'s own
  computed position stays `static` regardless of what its ancestor declares.
  The header cells now establish their own sticky context, and all six columns
  hold 0px alignment drift even when the region is scrolled fully right.

- A bounded `max-height` and a scrolling `overflow` are a matched pair. The
  first pass at this todo scoped a taller cap unconditionally; being two
  classes deep it beat the card-mode reset's `max-height:none` even inside
  that media query, but did not restate `overflow`, leaving a clamped box
  still declared `overflow:visible` inside a `section` that clips. 16 of 20
  workers rendered below the clipping edge, unreachable at the page's maximum
  scroll, and no test caught it. The cap now lives inside its own
  `min-width:761px` context with `overflow:auto` in the same rule, and the
  invariant is asserted at rule level rather than by substring: any block for
  that selector carrying a bounded max-height must declare scrolling overflow
  in the same block.

- The repeated unavailable-workflow sentence left the visible layout without
  being deleted. PROMOUX-001 requires that server-provided reason; it now
  travels in `title` and an `sr-only` span referenced by `aria-describedby`
  behind a compact badge, and PROMOUX-001's tests pass unmodified.

- Toggling the promotion-eligible filter was triggering a full page reload:
  `eligible` was missing from the directory-only route-change key set. Fixed
  and covered.

## 2026-09-13 (UXAUDIT-004)

- Reporting lines are a real tree now. The root cause was worse than the todo's
  wording suggested: nesting was computed by **matching manager display-name
  text**, so two people sharing a name collapsed together and a rename silently
  rebuilt the hierarchy. Nesting now derives from the authorized relationship
  projection, keyed on the manager's actual worker id.

- Agreement between visual nesting and manager edges is proven generically, not
  by example -- a renderer that indents by array order passes any hand-built
  tree. Two hundred seeded random populations require every nested node's parent
  to equal ground truth and every worker to appear exactly once.

- Hierarchy is never invented. A manager outside the visible population, a
  withheld relationship, and a reporting-line cycle each render as a root
  carrying a distinct explanation -- never reparented under something else,
  never silently dropped. The status mapping is exhaustive, and an unresolved
  relationship cannot become a false root.

- Verified against the running server. Before: the reporting-lines view exposed
  no tree semantics whatsoever -- no tree role, no treeitem, no level -- so
  assistive technology saw disclosure widgets rather than a hierarchy. After:
  one tree, 64 tree items, 19 groups, and every node's declared level checked
  against an independently counted ancestor depth with zero mismatches.

- One organization-node component now serves the flat list, the tree and the
  Myself subtree, which shows the viewer and their own reports rather than the
  whole company.

## 2026-09-13 (UXAUDIT-003)

- The global launcher performs actions instead of duplicating navigation. It had
  been a control labeled "Start an action" containing exactly two plain links --
  Journeys and People -- and nothing you could actually do. It now offers ranked
  authorized actions: six "Start Promotion for ⟨person⟩" entries, each carrying
  the worker it acts on.

- When a viewer genuinely has no authorized actions, the control relabels itself
  rather than fabricating one to fill the menu, and that label is derived live
  from the resolved items rather than carried as a separate prop that could drift
  from reality.

- Unavailable actions explain themselves without disclosing why. A viewer who
  cannot act sees byte-identical reason text across every underlying cause, and
  the rendered markup never carries the specific reason keys -- while an
  authorized viewer sees genuinely different reasons.

- A disclosure regression was caught during implementation rather than shipped:
  the first pass rendered every admitted worker's name into the launcher's server
  markup, hidden but present, leaking names across page-scoped surfaces. Fixed by
  gating the results panel on open state, the pattern the global search already
  used.

- Verified against the running server. The launcher shows real actions; an
  Escape keydown closes it and returns focus to the trigger; an outside pointer
  press closes it too. Those three were exactly what the Go tests could not
  execute, since the behaviour lives in a wasm-gated file this repository's CI
  never runs either.

## 2026-09-13 (UXAUDIT-012)

- Loading skeletons now preserve the geometry of the content that replaces them.
  The defect was literally true rather than hypothetical, and measuring found
  three real mismatches: a loading row declared ten pixels shorter than the work
  row it stood in for -- a shift on every Home, Work and Journeys cold load --
  plus two more on the People and History table proxies.

  The proof compares dimensions rather than asserting a skeleton exists, which is
  the difference between testing this clause and not testing it. It parses the
  built stylesheet with an exact selector matcher, so a density variant cannot be
  mistaken for the base rule, and was verified against the pre-fix values.

- One async-region state model now covers loading, empty, stale, failure and
  resolved, with exhaustive switches and no default branch. Its zero value is
  _loading_, not resolved -- the load-bearing direction here, because a region
  that forgets to set its state then renders the sized skeleton rather than a
  false-resolved empty box, which is precisely the shift.

- A region that collapses when its fetch fails is the same defect as one that
  collapses while loading, so the failure state is proven byte-identical to the
  loading state, not merely similar in size.

- Verified against the running server. Cumulative layout shift measured through a
  performance observer: the People directory resolved twenty rendered rows with a
  shift score of zero -- a real skeleton-to-content transition, not a trivially
  empty page. Software navigation between pages kept the shell and primary
  navigation as the _same DOM nodes_, disproving the remount clause directly,
  with zero shift across the transition.

- Known gap: no governed numeric budget for layout shift exists anywhere in the
  repository -- the performance-budgets surface is an honest stub that refuses to
  invent one -- so the threshold used here is declared as an engineering figure
  and documented as not business-authoritative.

## 2026-09-12 (UXAUDIT-011)

- Experience Studio no longer appears in primary navigation, and the fix is a
  registry rule rather than a special case for that one route -- which the todo
  forbids explicitly. `PageDefinition` gained an admission flag whose zero value
  is false, and both navigation-building loops gate on it. Studio's registry row
  is otherwise untouched; navigation code names no specific route anywhere.

  The default being not-navigable is the load-bearing part. The opposite default
  silently readmits every future unbuilt page, which is how this defect arose in
  the first place.

- The rule generalized rather than special-casing one page, which was the point
  of forbidding a route exception: eleven further registry entries backed by no
  real service -- Policy Studio, Policy simulation, Configuration center,
  Integration operations, Reconciliation workbench, Privacy telemetry,
  Performance budgets, Browser matrix, Assistive tech, Disaster recovery and
  Release gate -- are excluded by the same predicate, proven generically rather
  than by name.

- Omission from a menu is not authorization, and the change moves nothing across
  that boundary. The route still answers for an authorized role with its
  existing explanatory unavailable state, and unauthorized roles are still
  refused by the same visibility check as before, independently of admission.

- Verified against the running server: primary navigation went from 14
  destinations to 13, the only removal being Experience Studio, with nothing
  added and every other destination intact.

- Seven pre-existing tests were updated, each because it encoded the defect
  rather than a contract -- including four that asserted unbuilt stub pages
  _should_ nest under Admin. Those four contracts are about their named tests
  returning deterministic authorization-filtered results, not about navigation
  placement, so inverting the nav assertion does not weaken them.

## 2026-09-12 (PROMOUX-011)

- Every committed promotion transition now emits one authority-filtered
  invalidation carrying a sequence, and those two requirements interact as a
  disclosure bug rather than a cosmetic one. With a global sequence and a filter
  that merely drops undeliverable messages, a viewer who observes 1, 2, 4 has
  learned that event 3 exists and can count promotions they hold no authority
  over.

  The scheme is gap-free renumbering per subscriber. A durable global position is
  allocated inside each writer's own commit, so concurrent writers cannot lose or
  duplicate one -- but that value never reaches the wire; it serves only as a
  revision for stale and reorder protection. Each subscriber gets its own counter
  that advances by exactly one **only when a message is actually delivered**, so
  a transition authorizing nothing for that subscriber never touches their
  counter and their visible sequence is contiguous by construction.

  The test reproduces the leak before fixing it: the naive scheme delivers a
  visible gap, then the real one delivers a contiguous pair. A test that only
  asserted the corrected behaviour would not show the bug was ever reachable.

- Region isolation is proven against real invalidation clients, not a stub: a
  message for one worker's detail region fires exactly the matching client once
  and leaves an unrelated region and a different worker's detail client at zero.
  Last-updated equals the newest transition's own timestamp regardless of input
  order, and an empty set renders empty rather than falling back to now.

- No duplicate renders is counted rather than inferred, because a double render
  converges to the same state and would pass any state assertion. The
  performance check asserts delivered-message counts and bounded per-subscriber
  state rather than elapsed time.

- Fixes a defect introduced earlier in this series: the
  `ledger_payload_disposition` registry row declared a data role and retention
  class that are not declared values, which broke the storage-disposition gate.
  It escaped notice because that gate lives in a different package from the
  schema-classification check run at the time.

## 2026-09-12 (UXAUDIT-006)

- User-facing copy no longer names the implementation. Page subtitles, empty
  states, loading states and error reasons that referred to a journey service, a
  worker projection, an authenticated cell, a canonical gRPC service or a
  server-enforced boundary now describe the task instead, in all three locales --
  a German sentence containing the service name is still a violation.

  The footer that read "Live source · JourneyService" now reads "Live source ·
  Workforce directory", from the single value that also feeds the Settings page's
  data-source fact.

- The durable part is a vocabulary guard, not the individual replacements.
  Without one, the next subtitle to mention a worker projection reintroduces the
  defect silently and this work gets done twice. A documented banned list is
  scanned against every page rendered in every locale, and the guard proves
  itself non-vacuous by first scanning a deliberately seeded banned string and
  requiring a hit.

- Hardcoded strings moved into the locale catalog rather than being reworded in
  place, which is what the todo asks for: several literals living directly in
  page renderers became catalog keys.

- Empty and loading states now give a next step where they previously just
  reported absence -- the organization empty state tells the reader to ask an
  administrator to check their access, and an unavailable admin surface tells
  them to refresh.

- Verified against the running server: nine pages fetched with script and style
  elements stripped are clean of the banned vocabulary in visible text. The only
  remaining occurrence anywhere in the served documents is inside a WebSocket
  tunnel URL in a JSON config island -- a transport endpoint, not copy.

## 2026-09-12 (PROMOUX-007)

- A rejected promotion value now produces a field-linked, localized message
  quoting the exact corrective bound, instead of a page-level message carrying
  field keys, decimal fractions, Go error names or correlation internals.

- One mapper serves both render paths, and that is structural rather than
  asserted: server-side rendering and the enhanced client walk the identical Go
  tree, compiled once native and once for wasm, so there is no second
  implementation that could drift. It keys exclusively on typed finding codes
  and never reads the domain's human-readable message field -- which is exactly
  the field documented as carrying raw decimals, denial reasons and Go error
  text.

- Editing one field clears only that field's error. Proven with two invalid
  fields rather than one, because a single-field test cannot distinguish
  clearing one error from clearing all of them.

- The leak clause is proven in both directions, since asserting only that
  ordinary copy is clean could be satisfied by deleting the support reference
  entirely. A message deliberately stuffed with a decimal fraction, a Go error
  string and a fake correlation id is required to leave no trace in visible copy
  -- while the collapsed diagnostics panel is separately required to carry the
  opaque result digest, so support still has something to act on.

- Mapping is exhaustive and fails closed: every finding code the domain defines
  has an explicit case, a table-driven test proves none reaches the fallback,
  and an unrecognised code carrying an adversarial message yields only the
  generic text.

- Known gap, already covered by the escalation raised at PROMOUX-006: this is
  the fourth component in a row that is fully built and tested but rendered on
  no page a user reaches, because no live promotion-proposal form exists yet.

## 2026-09-12 (PROMOUX-006)

- The promotion form can show an authorized proposer the real compensation
  guardrail -- current exact Money, permitted increase percent, exact minimum
  and maximum annual Money, band position, currency and effective-date basis --
  instead of a dash beside a hidden percentage rule.

- No floating-point arithmetic anywhere on the money or percentage path, proven
  by a test that would actually catch a float rather than by inspection. It
  picks the textbook double-precision trap scaled to salary figures and **first
  proves the trap is real on this platform**, failing with an explicit
  instruction to choose different cents if a float64 subtraction of those
  literals ever does reproduce the exact answer, before asserting the guardrail
  returns it exactly. A test using round numbers would have proven nothing,
  because those survive floats fine.

- The client is proven not to recompute the guardrail adversarially rather than
  agreeably: the projection supplied to the renderer deliberately is _not_ what
  the naive percentage arithmetic would produce, and the rendered output must
  match the projection. A well-formed fixture would have agreed either way and
  hidden a client-side calculation.

- An unauthorized viewer cannot infer pay, which matters because a permitted
  range discloses the baseline -- a maximum of base x 1.18 lets a reader solve
  for base. Every non-value presence state leaves the data fields zero and
  encodes byte-identically, so the cause is not itself a signal; and the
  rendered output is checked with tags stripped for zero digits, no percent
  sign, and none of the authorized render's strings, with an authorized control
  proving the check is not vacuous.

- The locale test distinguishes locales rather than rendering three and
  asserting one string: grouping and decimal separators genuinely swap between
  en-US and de-DE for both money and percent, currency stays correct, and the
  Arabic render is right-to-left with distinct script.

- Known gap, escalated as a group: this is the third card in a row -- after the
  approval disposition and the promotion review -- that is fully built and
  tested but rendered on no page a user reaches. These todos remediate a live
  product audit, so all three are now escalated together to be wired onto a real
  surface and verified in a browser.

## 2026-09-12 (PROMOUX-005)

- A management promotion can no longer reach approval without resolving the
  target manager, organization and reporting-line impact -- and cycle safety is
  real reachability rather than a single-hop guard. The detector walks the
  proposed manager's actual ancestor chain and asks whether the promoted worker
  appears in it, so a cycle closing two or more hops up is caught. A reports to
  B reports to C, promote A to manage C: refused, proven both in memory and
  against real PostgreSQL rows.

  The converse is proven too, because a check that refused every deep chain
  would pass a cycle test while breaking the product: a legitimate four-hop
  chain that closes no cycle is admitted.

- The detector reports three outcomes rather than a boolean that would have to
  guess. A chain it cannot certify -- depth exceeded, a pre-existing loop above
  the proposed manager, a withheld disclosure, a stale or ambiguous resolution
  -- returns UNDETERMINED, and promotion treats that as blocking with its own
  finding. An unresolved chain is never reported safe by omission.

- "Material changes re-run the exact checks" is proven to mean the same checks,
  not a cheaper subset: preflight runs twice, and every pre-existing unrelated
  finding from the first run must reappear verbatim alongside the new cycle
  finding. A re-validation that quietly dropped checks fails.

- Organization semantics stayed in the Organization capability; promotion
  composes a typed impact effect rather than copying graph logic. This also
  added the first production worker-facts adapter in the tree.

- Escalated rather than silently inherited: three cycle checks on the promotion
  commit path test membership in an ancestor list that nothing ever populates,
  so their self-reference half works while their multi-hop half always passes.
  They are dormant while promotions only simulate, and become silent no-ops the
  moment execution is enabled.

## 2026-09-12 (PROMOUX-004)

- A guessed target position is no longer accepted. Any non-empty position
  identifier is decoded as a server-issued revision reference and checked
  against the real Position domain on five separate grounds -- existence,
  compatibility, vacancy, effective-date revision currency and reservation
  ownership -- each with its own test constructing a case where only that ground
  fails, so a mapping that wired four of five cannot pass.

  The decisive proof is on the path a user reaches, not in the evaluator alone: a
  Cell composed the way a live deployment composes one, driven through its
  workspace port, returns BLOCKED with a not-found finding for `POS-ENG-MGR-101`,
  while a request naming no position at all still reaches READY.

- Reservation ownership is settled by the database -- a partial unique index
  PostgreSQL evaluates as part of each insert's own commit, never by a read that
  precedes the write -- proven with twelve concurrent goroutines on independent
  connections. The refusal for an unauthorized position is byte-for-byte
  identical to the refusal for one that does not exist, so the error cannot be
  used to discover which position ids are real.

- Three real defects surfaced while building it. The revision-currency check used
  an ordering comparison that errors outright on the opaque revisions the real
  adapter mints; it now compares for equality, which is the exact binding the
  contract asks for. And building the production position adapter against real
  PostgreSQL caught a timezone bug -- dates were extracted without normalizing to
  UTC first, so a server in another zone shifted every date by up to a day -- and
  an off-by-one translating an exclusive database boundary into the domain's
  inclusive end-date convention.

- One deliberate contract change: the target position is no longer a required
  kernel input. The kernel had been stricter than the domain while proving less
  -- placement has never required a position, only a target job and grade, so the
  presence check guaranteed nothing but a non-empty string and a guessed value
  satisfied it. A named position is now genuinely proven, which is the stronger
  guarantee; job and grade remain blocking.

- Known gap: the durable reservation is still never exercised on the live propose
  path, because only the read-only preview reaches a wired position reader. That
  needs real position rows in the dev corpus and the commit path wired.

## 2026-09-12 (PROMOUX-003)

- One principal can no longer complete both the finance and the manager
  approval on the same proposal, and the refusal is enforced by the database
  rather than by a check that runs before the write.

  `LockApprovalSiblings` issues `SELECT ... FOR UPDATE` over every approval row
  sharing the proposal, ordered by work-item id so racing callers acquire the
  same locks in the same order and cannot deadlock. Because both requirements
  name the same row set, only one transaction can hold them; `Complete` takes
  that lock before reading any state and before writing, so the loser blocks,
  sees the committed conflict, and refuses. Two real connections released from a
  barrier prove it.

- The undifferentiated owner is closed structurally: finance and manager
  approvers are now provably distinct principals derived from one base, instead
  of the single shared approver identity both requirements had been routed to.

- Reassignment is proven not to disturb what is being approved by diffing the
  durable record field by field -- fourteen immutable facts plus the proposal
  reference -- while requiring the owner to have actually moved, so the equality
  claim cannot pass vacuously.

- Separation is evaluated by the approval owner with the UI entirely absent: a
  conflicted principal is refused driving the domain write directly, no HTTP, no
  journey engine, no page. That fills the concrete authority-recheck seam
  EP-WORK-003 left unimplemented and recorded as a boundary when it closed.

- Presentation consumes the disposition rather than recomputing it. The card
  states the five facts the todo names, and both the props and the rendered HTML
  are asserted not to leak: a group-owned item renders a generic protected-group
  phrase with the internal candidate-set reference and its digest required
  absent, and a conflicted viewer's refusal names no second principal.

- Also raised a hardcoded 20s budget in an unrelated serializable-retry test
  that turned machine load into a false failure. Confirmed pre-existing by
  running it against a worktree of unmodified HEAD, where it failed identically;
  every assertion is unchanged.

## 2026-09-12 (UXAUDIT-007)

- The shell stops announcing context that has not changed. An ordinary
  self-context user no longer sees `Acting as yourself` or a `Compensation
Review` badge on every page; the persistent banner is reserved for delegated,
  view-as, elevated and break-glass authority. Verified live: Home's header
  reduces to the product name plus navigation, search, locale and avatar, with
  no banner.

  The RED had two independent sources. `header_identity.go` rendered a hardcoded
  acting label on every page with no dependency on any authority projection at
  all, and the authority banner fired for any valid context including plain own
  authority. Notices now come from one component.

- The disclosure rule is deliberately inverted from this repository's usual
  fail-closed convention, and tested that way. Every unrecognised authority
  state discloses rather than suppresses, because silently hiding a banner would
  tell someone acting under delegated or break-glass authority that they are
  acting as themselves -- a misrepresentation of who they are, and much worse
  than a redundant banner. Only ordinary self context is quiet.

- The second half was data wiring, not rendering: an admitted principal's
  authorized data-processing purpose was being passed as the shell's generic
  authorized scope, from two separate callers. Both stop, and the field that
  received it carries a doc comment pinning its contract against a repeat.

- The purpose now appears where the todo says it belongs -- inside the affected
  workflow, reading `Purpose: compensation_review` beside a control that reads
  `Exit compensation_review and sign out`. That exit was missing after the first
  fix and was found by the live run: the masthead's principal was built with
  subject, roles and purpose but never a logout destination, though the
  component already knew how to render one. The tests now assert the pairing by
  value, because asserting either half alone is what let the gap through.

## 2026-09-12 (UXAUDIT-001)

- The application shell is usable on a phone. At 320x720 the header is a single
  81px row instead of wrapping to 134px, content starts at 11% of the viewport
  instead of 63%, the visible content area grows from 269px to 639px, and there
  is exactly one page-level scroll region where navigation and content had been
  competing for two. Narrow viewports get an off-canvas overlay drawer with a
  backdrop; opening it does not change content width.

  One shell serves both: a single shared open-state flag feeds the header
  trigger and the sidebar, with no per-viewport fork and no duplicated
  navigation component.

- Two defects were found by driving the live server and fixed, neither of which
  the Go suite could see. The drawer's open rule matched the element, sat later
  in the cascade, was more specific, and carried no `!important` on either side
  -- and still never applied; the sidebar stayed off-canvas whether the class
  was there or not. And the backdrop could never appear at all, because its open
  rule set background, inset, position and z-index but never `display`, leaving
  the base `display:none` in force.

  The more useful outcome is the test that would have caught them. The suite had
  been substring-matching stylesheet text, which proves a rule exists but not
  that it wins the cascade. It now parses a rule's declaration body into a
  property map and requires the open rule to restate the properties the closed
  rule sets, with a different value and an overriding priority. Verified failable
  by reverting each fix on its own.

- Known boundary: focus containment and restoration are not claimed. Calling
  `.focus()` on a real input inside the open drawer leaves `document.activeElement`
  at BODY in the verification environment, so neither focus entering the dialog
  nor returning to the trigger is observable there. Escape-to-close was confirmed
  live, because it does not depend on focus.

## 2026-09-12 (PROMOUX-002)

- Two people can no longer start overlapping promotions for the same worker.
  The todo's REFACTOR clause says it plainly -- the guard belongs to promotion
  admission, and UI suppression is not the integrity boundary -- so the
  guarantee is a database constraint, not an application check.

  `promotion_active_intent_guard` carries a partial unique index on
  `(tenant_id, worker_ref, effective_date) WHERE status = 'ACTIVE'`, and
  `Admit` is a single INSERT .. ON CONFLICT .. RETURNING, never a SELECT that
  precedes the write. Two concurrent transactions may both read no conflict;
  only one can commit, because PostgreSQL evaluates the index as part of each
  INSERT's own commit. Twelve goroutines on independent connections, released
  from a common barrier with distinct idempotency keys, prove exactly one wins.

  The window is modelled as the single effective date the request contract
  carries, so two promotions for one worker on different dates are both
  admitted; only the same date collides.

- The zero-mutation clause is proven by counting rather than by asserting
  success: a counting executor wrapping the real transaction requires every
  mutating statement a refused start issues to target the guard table and
  nothing else, and the security test additionally diffs row counts across
  intents, ledger events, work items and the domain tables. That test calls the
  engine directly, with no HTTP, gRPC or page involved, which is what makes the
  REFACTOR clause true rather than asserted.

- Four pre-existing tests were updated rather than weakened. Each had assumed
  two proposals for one worker's single effective date are both admitted --
  precisely the defect this closes. The digest subtest keeps its original claim
  by computing the content digest on the refused request directly, and gains a
  row count proving no second intent was created.

- Known gap, recorded rather than implied: `Release` is implemented and tested
  but called from no production code, because nothing fires when an intent
  reaches a terminal stage. A guard row is created ACTIVE and never closed, so
  re-proposing for the same worker and date after a rejection is refused
  permanently, and the UI -- which still reads the non-terminal journey scan --
  offers Start where the server refuses. Escalated separately.

- Added two storage-disposition registry rows, including one for
  `ledger_payload_disposition`, which LEDGER-011 shipped without and which had
  left the schema classification gate red since that merge.

## 2026-09-12 (PROMOUX-001)

- The People directory can now tell you _why_ a worker has no promotion
  available, and can filter to the ones that do. Verified in a real browser
  against the running server, which this todo's section requires: 65 people,
  a `Promotion-eligible only` filter, workflow menus on eligible workers, and
  on the rest a server-provided reason instead of the bare `No available
workflows` string the audit found. `?eligible=1` narrows to 38 rows, all of
  them actionable.

  The cause was not a business rule refusing anything. `workforceOptions()`
  published promotion paths only from the fixed four-worker corpus, whose job
  codes do not overlap the sixty demo-workforce codes, so every seeded worker
  resolved zero choices for a structural catalog reason that rendered
  identically to ineligibility. The demo company's own computed ladder now
  feeds the same contract.

  Enumeration leakage is closed in both directions.
  `ResolvePromotionAvailability` checks authorization first and exhaustively,
  so every worker collapses to one withheld code for an unauthorized viewer;
  the security test drives three underlying states through such a viewer and
  requires exactly one distinct reason string, then requires ineligible and
  active-conflict to stay distinguishable for an authorized one.

- Closed a zero-value trap during review: an unset `PromotionAvailability`
  was treated as eligible, and the regression test asserted that an
  unevaluated worker got a launchable action and no reason at all. Any future
  page or partial projection that forgot the field would have offered a
  promotion nobody authorized. The gate now requires an explicit verdict.

- Two independent defects fixed alongside. `demo-people` could not seed at
  all, because photo ingestion demanded bit-identical regeneration of proxies
  whose checked-in bytes had drifted; it now trusts an already-published
  tracked proxy, and no tracked asset bytes were changed. And
  `workeridstore.Store.Reserve` self-committed while the worker row was
  inserted in a separate later transaction, so a failure between them
  permanently orphaned a worker number in an append-only table.

## 2026-09-12 (LEDGER-011)

- Ledger payload disposition: retention, holds and crypto-erasure that cannot
  falsify the record. A new append-only `ledger_payload_disposition` table
  (migration 00285) records the decision; `ledger_event` itself is never
  mutated, because it cannot be.

  The failure this closes is that an erased payload used to be indistinguishable
  from one that never existed -- both read back as a nil `Payload`. `EventRecord`
  now carries an explicit `PayloadState` with no permissive zero value, and the
  withholding happens on the **default** read path: `ReadStream` and `ReadEvent`
  join the disposition table and withhold unconditionally, so the ten existing
  callers across projection, provenance, rebuild, queryplans and the intent store
  inherit it without opting in. The test reads back through the plain reader
  rather than the new view precisely so an opt-in-only withholding would fail.

  Chronology is proven preserved by name rather than in aggregate: all twenty
  `ledger_event` columns are compared before and after and the failure names the
  column that changed, and the real hash-chain verifier runs against PostgreSQL
  on both sides with an identical head asserted. Erasure under an active legal
  hold has no check-then-act gap -- it composes `recordsmeta.DisposeCopy`, which
  evaluates the hold under the same `FOR UPDATE` lock that guards the write.

- Known limits, recorded rather than implied: **crypto-erasure is modelled, not
  real.** The append path never encrypts an inline payload, so there is no key to
  destroy; the symbolic key reference is evidence that a governance decision was
  made, not proof the bytes became unrecoverable. The payload bytes are never
  physically zeroed for either mechanism -- impossible without abandoning
  `ledger_event`'s append-only trigger and grants. Both mechanisms' real
  guarantee is that the read layer never returns the bytes again.

- Also in this commit: 52 new todos the user added across three sections --
  live product UX audit remediation, promotion workflow live-audit remediation,
  and visual-design and interaction polish. They were authored with a `[P1]`
  phase, which is not in the registry's vocabulary and blocked registry
  generation; the 23 affected entries were remapped to `[GATE_C]`, matching the
  phase the other 120 WEB/UX todos already use.

## 2026-09-12 (EP-EVID-001)

- `GetExecutionReceipt` and `ExportIntentEvidence` land in a new
  `internal/transport/evidence`, composing EVIDENCE-001's receipt model,
  EXPORT-001's artifact rendering, the existing long-running-operation record
  and `internal/cryptoagility` for signing. Nothing is reimplemented.

  The clause worth the most here is that a receipt describes what happened at
  execution time. An export reads its lineage source exactly once and every
  artifact derives from that single read, so the manifest can never disagree
  with the bytes it describes. The test proves it by drifting: the injected
  source returns different content on every call and counts them, so an
  implementation that re-read mid-run is caught. Verified failable by injecting
  that regression and watching it fail, then pass again on revert.

  Package integrity is layered and real: signature, then AES-256-GCM
  authentication tag, then manifest digest, then per-artifact digests. A flipped
  ciphertext byte, a stripped signature and a wrong key are each rejected.
  Formula injection is neutralised in the human artifact and the exact bytes
  preserved in the machine artifact, tested with values beginning `=`, `+`, `-`,
  `@`, tab and carriage return. The six lineage hops are asserted one at a time
  rather than by a non-empty check.

- Reported rather than forced: `GetExecutionReceiptResponse`'s receipt field is
  wire-typed `ZeroEffectReceipt`, whose constructor rejects a non-zero effect
  count and whose mode admits only PREFLIGHT and SIMULATE. This todo's clauses
  describe an executed intent's receipt, where effects are legitimately present.
  Putting one inside the other would either misstate the effect count or leave
  the message's own invariant unchecked, so the full lineage-bearing receipt is
  carried by the export operation's result instead. Closing that properly needs
  a proto change.

- The endpoints are contract-complete but not yet reachable in a running server:
  the four ports have in-memory implementations only and `EvidenceService` is
  registered in no composition root. Escalated separately. Key custody is also
  out of scope -- the signer and package key are supplied directly.

- Corrected a stale doc comment on `TestTodo_PROTO_010_Golden` left behind by
  the previous change, which still said ten SERVED and four REFUSED_P1A while
  the assertion underneath it checked thirteen and one.

## 2026-09-12 (repository drift found when CI first ran green)

- Lands the discrete drift the CI fix uncovered, all of it pre-existing and
  masked while the quality gate was red. `internal/configuration` and
  `internal/evidence` had landed without layout roots and are declared now
  (platform/deferred and data/deferred -- a judgment call, reviewable). The
  layer-graph golden, the capability-coverage manifest and the workspace asset
  manifest are re-pinned to what the tree actually produces.

- The evidence-freshness gate passes for the first time. Most of it is the
  bulk-generated allow-list of 986 pre-rule evidence lines, each carrying an
  owner and an expiry so backfill lanes convert them into real Evidence lines
  and delete them as they go. The five gaps that allow-list did not cover are
  closed with real verification rather than more allow-list entries: EDGE-011,
  EP-PROMO-001 and EP-WORK-001 had their named tests re-run on this host and
  their evidence lines completed with the command, environment, toolchain,
  result and branch the gate requires. Where a run could not be reproduced here
  -- the race sweep, and the `test/bootstrap` integration cases -- the evidence
  says so explicitly instead of implying a fresh pass.

- Expanded `EP-WORK-001`'s evidence from `TestTodo_EP_WORK_001_{Property,...}`
  brace notation into the full test names. Brace notation reads fine to a human
  and is invisible to the traceability gate, which matches literal names; it is
  the same shorthand that produced an orphan earlier in this batch.

## 2026-09-12 (EP-INTENT-003)

- `SubmitIntent`, `CancelIntent` and `SupersedeIntent` are served. The
  interesting constraint is that `CancelIntentResponse` carries only an
  `IntentInstance` and the proto has no cancellation-outcome enum, so the four
  answers the contract requires -- CANCELLED, CANCELLATION_PENDING, TOO_LATE,
  REPAIR_REQUIRED -- have to be told through the dimensional tuple alone.
  CANCELLED moves the request state and leaves execution NOT_PLANNED or
  BLOCKED. TOO_LATE moves nothing: execution already committed, and recording
  CANCELLED would falsify the record. CANCELLATION_PENDING moves nothing
  either, because nothing has been decided yet. REPAIR_REQUIRED moves
  execution and refuses without a repair reference.

  `CancelInstance` switches over every execution state and every cancellation
  point by name with no permissive default, and the integration test seeds four
  intents in four conditions and requires the four returned tuples to be
  pairwise distinct reading nothing but the response. A mapping that collapsed
  two outcomes together fails there rather than passing on the common case.

- Supersede never mutates the original, proven by diff rather than by an
  existence check: the original's encoding is captured before and after, only
  the request state, version and two timestamps are cleared, and `proto.Equal`
  must hold over everything else. Authority cannot widen through a supersede --
  tenant, scope, initiator and purpose come from the authenticated caller and
  never from the original -- and that is now asserted by value, with the
  original seeded with a delegation chain the successor must not inherit.

- Discovery was lying. `internal/transport/manifest` still published these
  three methods as `REFUSED_P1A`, so a client reading the discovery document
  would have been told three working endpoints were refused. All three are now
  `SERVED`; `ExecuteIntent` alone stays refused. The ENDPOINT-001 security test
  was widened rather than weakened: it still pins that the refused write stays
  refused, and additionally requires each served write to publish its accepted
  definitions, carry no refusal reason and be idempotency-key deduped, with a
  count assertion so a rename cannot make it vacuous.

- Fixed a latent defect in `Definition.LifecycleProfiles`: it narrowed the
  transition set but kept the full kernel state list, so any narrowed
  definition's profile failed its own reachability validation the moment a
  `lifecycle.Machine` was built from it. Nothing had ever built one, which is
  why it had never fired.

- Known gap, recorded rather than implied: CANCELLATION_PENDING is reported but
  not durably recorded, and no real safe-point reader is wired yet, so a
  cancellation requested against a mid-flight intent is not yet honoured later.
  Escalated separately.

## 2026-09-12 (EP-WORK-003)

- `CompleteWorkItem` and `DecideApproval` land on the humanwork transport,
  replacing the two remaining P1B stub refusals. The clause that defines the
  todo -- an approval records a decision, it must never BE the business change
  it approves -- is proven by absence rather than asserted. A `countingExecutor`
  wraps the real transaction, parses every mutating statement's target table
  against live PostgreSQL, and the integration test walks all recorded writes
  and fails on any table it does not name: `work_item` and
  `work_item_transition` at exactly one write each, everything else a named
  failure. A write to a ledger, a budget reservation or an org-chart row would
  be caught by table name.

  Restricted evidence stays out of responses structurally, not by filtering:
  the wire `WorkItem` carries no evidence field and `ApprovalDecisionResult`
  carries only ids, revision, decision, reason, principal and timestamp. A
  canary evidence id supplied on a completion request is asserted absent from
  the whole marshalled response.

  Both endpoints compose rather than reimplement. WORK-006's
  `CompleteWithAuthorityRecheck` performs the identity, assurance, authorization
  and separation-of-duties recheck; ENDPOINT-004's idempotency `Coordinator`
  supplies the duplicate-decision and mutated-decision clause. The SoD policy
  itself lives in a driver outside this change -- what is proven here is the
  wiring, that a refusal from the recheck port reaches the caller unchanged.

- Fixed alongside: `prepareMutation` required `Dependencies.Claims != nil` for
  all four mutating methods, including the two that never touch Claims. The
  check now covers only the dependencies actually shared, and
  `ClaimWorkItem`/`ReleaseWorkItem` keep their own guards.

- Removed two `TestTodo_EP_WORK_001_Conformance` cases asserting that Complete
  and DecideApproval return the P1B stub `FAILED_PRECONDITION`. Implementing
  them made those assertions false; this is the same treatment EP-WORK-002 gave
  Claim and Release.

## 2026-09-12 (DATAOPS-005, ARCH-GO-019)

- Ticked two more todos the sweep found built but unrecorded. No behaviour
  changed in either; both verified against their contracts.

  DATAOPS-005 enforces RED's replay clause by content addressing rather than
  convention: rowDigest binds the header digest into every row id, the batch
  digest binds source, schema and retrieval time, and tests fail if two stagings
  of identical input produce different digests or identical inputs reproduce
  different error sets. It also closes the zero-value trap at the type level --
  "SourceKindUnspecified is the zero value and is never legal".

  ARCH-GO-019 makes RED's second clause a named diagnostic rather than an
  inference: DuplicateDomainOwner reports "domain package X is also owned by
  process Y", so a split that quietly duplicates ownership is refused with the
  offending pair named.

## 2026-09-12 (CONFORMANCE reconciliation)

- Ticked the five CONFORMANCE todos the backlog sweep identified as built but
  unrecorded: FX-003, INCENTIVE-002, SAFETY-002, SKILL-002 and TAXPROFILE-003.
  No behaviour changed in any of them; each was verified against its contract
  rather than accepted on a green run.

  TAXPROFILE-003 is the one worth reading. Both of its unsafe-default clauses
  are refused explicitly: a registration presence of UNKNOWN fails rather than
  defaulting to resident, and a REDACTED election is refused rather than
  collapsing to zero withholding. Presence is a three-state enum -- present,
  unknown, redacted -- precisely so an absent fact cannot be read as a benign
  zero. That is the same defect class found four times in this session's own
  lanes, already solved here.

  SKILL-002 models expiry as an explicit EXPIRED evidence status with "expired
  evidence never becomes effective" enforced in resolve, so a lapsed credential
  cannot remain qualified.

  SAFETY-002 carries a coverage caveat recorded rather than glossed: the package
  sits at 58.5%, below the 70% floor, but under a governed and unexpired
  below_floor exception (owner backlog, expiry 2026-12-31) logged when it stood
  at 46.5%. The floor is formally waived, not silently violated, and raising the
  package remains that exception's open obligation.

## 2026-09-12 (backlog reconciliation)

- Swept the backlog for todos whose entire declared test matrix already exists,
  after three of the session's closures turned out to be work that was finished
  but never recorded. **22 open todos** match that pattern -- roughly 6% of the
  open count, which materially overstates what is actually left.

- Ticked three of them after verifying each against its contract rather than
  accepting a green run:

  ARCH-GO-022 (P0) -- `TestHumanInteractionBoundaries` enumerates forbidden
  import edges and fails if workflow imports human work or messaging, which is
  the ownership separation RED describes. Naming note: GREEN's prose says
  `internal/work`, but no such package exists; the real one is
  `internal/humanwork` and the test targets it correctly. The prose is stale on
  a name, not the substance.

  DB-EDGE-003 (GATE_B) -- the central RED clause is asserted directly: a commit
  whose outcome is unknown yields ErrCommitAmbiguous with attempts == 1 and
  nothing published, so an ambiguous commit enters idempotent resolution rather
  than being retried.

  CUSTOMER-004 (GATE_B) -- GREEN's requirement that failures create owned
  blockers with expiry is enforced rather than advisory: a waiver presented
  without an expiry is refused.

  UX-003 and A11Y-001 also match the pattern but are deliberately left, having
  been deferred by the user earlier in the session.

## 2026-09-12 (OBS-007)

- Tick OBS-007. The owned-alert incident routing in
  `internal/application/incident_alerts.go` was already implemented with all
  four matrix tests present and passing; it simply sat unticked. No behaviour
  changed.

  Verified against the contract rather than accepted on a green run. Both RED
  clauses are asserted directly: a policy with no PrimaryOwner fails with
  ErrNoOwner, one with no SecondaryRoute fails with ErrNoRoute. The integration
  test drives the storm path against StormLimit/StormWindow and the store's own
  semantic dedupe fence, so a repeated condition does not open a second
  incident. RouteOwnedAlert refuses a tampered alert on a digest mismatch before
  any routing happens, and DetectOwnedAlerts refuses a nil detector rather than
  silently returning an empty set -- which would have looked like "no alerts"
  instead of "no detector".

## 2026-09-12 (CONN-RT-008)

- Close CONN-RT-008: certify connector maturity with an automated conformance
  harness. `certification.go` adds maturity levels and a certification decision
  over ten evidence classes, built on the existing providercontract fixture
  harness. Read as a pure decision over supplied evidence, per RED's "persist
  zero authoritative rows" clause -- nothing stands up a real connector. No
  migration needed.

  Expiry is enforced rather than reduced to a bare bool, which is what RED's
  expired-credential case demands. Every class embeds an observed/expires window
  and `classify` returns MISSING before EXPIRED before FAILED, so an expired
  credential is refused before its pass flag is ever consulted. A `Passed bool`
  alone cannot distinguish "verified last year" from "verified this morning",
  which is precisely the defect the todo names.

  The level cascade matches GREEN exactly: L3 requires all ten classes, and L4
  additionally requires peak load tested at or above the declared quota and
  99.9% availability. The rejection is a typed value carrying code, field,
  state, model version, connector id and requested level, unwrapping to a
  sentinel, so a caller can branch on the family or read the offending class.

  The mutation matrix breaks one class at a time while the other nine stay
  valid, proving each blocks L3 and L4 independently rather than in aggregate,
  and the fuzz oracle ran 13.1M executions asserting no evidence set missing a
  required class or carrying an expired one ever certifies at L3 or above.

## 2026-09-12 (EP-WORK-002)

- Close EP-WORK-002: ClaimWorkItem and ReleaseWorkItem. The RPCs and their
  generated types already existed; this wires real handlers to them, with the
  claim/lease state machine in `internal/humanwork/workitem` rather than in
  transport. No migration needed.

  ENDPOINT-004's Coordinator is composed rather than forked, so exact replay and
  the stale-revision precondition share one mechanism. Exclusivity is
  two-layered by design: the coordinator refuses many concurrent losers early on
  a stale revision, and anyone slipping past is caught by the existing
  `UPDATE ... WHERE item_version = $n` compare-and-swap. No second CAS was
  written.

  Append-only assignment evidence is enforced three ways and proven directly:
  the write paths only INSERT, the database grants refuse a raw UPDATE or DELETE
  (the mutation test executes one and asserts "permission denied for table
  work_item_transition"), and migration 00017's forbid_mutation trigger stands
  behind both.

  Two boundaries recorded rather than glossed. `Invocation` discards raw
  transport headers after admission, so only the message-level idempotency key
  reaches the handler -- replay and revision behaviour are unaffected, but the
  header/message folding is not exercised at this call site. And production
  composition is not connected: `internal/transport/cell/cell.go` has only a
  read port in scope, so both endpoints fail closed with UNAVAILABLE until a
  write port is threaded through the cell builder. Raised separately.

  Two EP-WORK-001 conformance cases asserting Claim and Release return the P1B
  stub refusal were removed, because implementing them made those assertions
  false. The Complete and DecideApproval stub cases remain.

## 2026-09-12 (ENDPOINT-004)

- Close ENDPOINT-004: unify endpoint idempotency, expected revision, ETag and
  conflict semantics. `idempotency.go` resolves the HTTP-header key and the
  message-body key before hashing -- if both are present and disagree the
  request is refused rather than silently preferring one -- and hashes the
  resolved key together with the principal/tenant/capability scope, so the same
  logical request over either transport lands on one digest.

  Two things a naive implementation gets wrong, both closed here. An exact
  replay returns the _stored_ outcome rather than re-executing, which is
  observable because the effect counter stays at one. And the same key with a
  changed payload returns a conflict without touching the effect at all --
  a key-only cache would return the prior success and pass every happy-path
  test.

  The digest encodes its fields length-prefixed, so ("ab","c") and ("a","bc")
  hash differently and concatenation cannot collide across field boundaries.
  `ExpectedRevision` is a pointer so "no precondition requested" stays distinct
  from "expected revision zero", and a zero on either side is refused rather
  than matched. The executor closes its ready channel in a defer, so a failing
  effect cannot strand the waiters.

  No migration needed.

## 2026-09-12 (PERF-005)

- Close PERF-005: size PostgreSQL, queues, timers and artifact throughput. Read
  as a pure sizing model rather than a load test, which is what RED's "persist
  zero authoritative rows, business events, outbox entries, human work and
  provider requests" clause requires. The 100k+ timer drain is a dimension the
  model bounds, not an action any test performs, so nothing here starts
  PostgreSQL or generates load.

  Storage covers rows, index bytes, WAL bytes, locks, connection pool and vacuum
  seconds; timers cover backlog, drain rate and drain budget; artifacts cover
  named object classes each with throughput and memory. Every dimension is a
  capacity/measured pair whose zero value or empty unit is rejected as
  MISSING_OR_INVALID rather than read as within budget, and the headroom ratio
  additionally guards a non-positive limit so it can never divide by zero.
  Headroom, a rollover point at 80% of capacity and the hard limit are derived
  per dimension, and a model missing any dimension is rejected outright.

  The rejection is a typed value carrying the offending field, its state and the
  model version, reachable through `errors.As`, rather than a bare error string --
  a sizing validator that says only "too big" is useless at the moment you need
  it. The mutation matrix flips sixteen fields one at a time and asserts exactly
  one diagnostic each, so the validator cannot be passing on an aggregate.

## 2026-09-12 (PERF-006)

- Close PERF-006: bound external latency, quotas and retries. Most of the
  contract already held and is now pinned by tests rather than rewritten -- the
  four budget dimensions and drain prediction from INTG-015, the 429 backoff
  from `Observe429`, the timeout-after-send ambiguity from CONN-RT-007 (AMBIGUOUS
  is not leaseable, so a retry loop cannot multiply provider calls), and the
  undeclared-quota fail-closed default.

  The one genuine gap was 5xx. RED names it alongside 429, but only `Observe429`
  existed, so a server fault was silently indistinguishable from a rate limit
  despite having different retry semantics -- a 429 carries the vendor's own
  reset, a 5xx does not. Added `ObserveServerFault`/`ServerFaultUntil` and
  `ScheduleReasonServerFault`, tracked in their own map so the two are never
  conflated. Proven by asserting `ThrottledUntil` stays empty for a 5xx-only
  connection and `ServerFaultUntil` stays empty for a 429-only one.

  A non-positive backoff is floored at one second, so observing a fault can
  never produce a zero-length no-op that would amplify rather than bound.

  The FAULT test asserts the actual attempt count for each of the five
  conditions rather than merely that an error surfaced -- "never amplify
  retries" is a counting claim, and a test that only checks for an error would
  pass against an implementation that retried fifty times first.

## 2026-09-12 (ASSURANCE-001)

- Tick ASSURANCE-001. The independent-assurance register in
  `internal/operations/assurance` was already implemented and passing at 96.6%
  coverage with all six matrix tests present; it simply sat unticked, which
  reads as work nobody started. No behaviour changed.

  Verified against the contract rather than accepted on a green run:
  `Assessment.Validate` requires an independent assessor, refuses an assessment
  whose expiry is zero or not after its date, and refuses evidence once the
  clock reaches expiry, so expiry is enforced rather than advisory. A CRITICAL
  or HIGH finding must additionally be RETESTED by an independent assessor with
  a passing result, a name and a date -- so an open critical finding cannot
  validate and therefore cannot permit the gate. `Register.Gate` on an empty
  register returns a decision whose `Allowed` is the zero value false, so
  absence fails closed. `Claim` carries exact scope and excluded surfaces and
  ends "This statement is not a certification".

  The tick itself tripped the traceability gate first: the evidence line used
  `_Golden`-style shorthand for the matrix variants, which the scanner appends
  to the preceding full name, inventing `TestTodo_ASSURANCE_001_Property_Golden`.
  That is the same failure mode as the brace-expansion orphan fixed earlier
  today, in a different spelling. Evidence lines must name each test in full.

## 2026-09-12 (PERF-004)

- Close PERF-004: prove noisy-neighbor and priority fairness. This closed a real
  gap in EVENT-003's own scheduler rather than only testing it.
  `ResourceLedger.TryAdmit` returned a bare bool and `ScheduleResult.Deferred`
  was a plain `[]Candidate`, so a shed decision recorded no reason at all --
  capacity exhausted, tenant share exceeded, backpressure, and already-claimed
  were indistinguishable to a caller, and GREEN ("every shed decision has
  quota/resource evidence") was unsatisfiable.

  `TryAdmit` now returns `(bool, string)`, matching the standard
  `ConnectorLedger.TryReserve` already met, with four exported reason constants
  and a new map distinguishing a backpressure-zeroed resource from one simply
  configured at zero. `ScheduleResult.Deferred` is now `[]Deferral{Candidate,
Reason}`, with `DeferredCandidates()` for resubmission. No path returns false
  with an empty reason. Both signatures changed; every caller in the module is
  inside `internal/data/outbox`, confirmed by a clean `go build ./...`.

  The fairness proof drives its bounds from
  `tools/planning/performance.EnvelopeFixtures()` rather than magic numbers, so
  it fails if a future PERF-ENV-001 change stops declaring the tiers it reads. A
  3000-item flood queued ahead of a small tenant's P0/P1 work still admits all
  ten of those first, the flood caps at its declared share while resource
  capacity is nowhere near reached, and all 2900 deferrals -- every one, not a
  sample -- carry the tenant-share reason. The evidence is a stable constant
  carrying no tenant state, asserted by checking the reason never contains
  either tenant's UUID.

## 2026-09-12 (CICD-006)

- Close CICD-006: the signed release decision manifest, completing the
  CICD-003/004/005/006 chain in one package. `decision.go` gathers eleven
  evidence classes into one canonical, signed document returning
  PROCEED/REMEDIATE/QUARANTINE/STOP. No migration needed.

  Every presence check keys off a real observation rather than a bare bool.
  Source needs repository, ref and commit; test needs a run timestamp and a
  non-zero total, so a run that executed nothing is not evidence; owner needs a
  name and a confirmation time. The sharpest is
  `BlockerEvidence.present() == !CheckedAt.IsZero()` -- "nobody looked for
  blockers" is not the same as "there are no blockers", which is exactly the
  distinction a bare bool would erase. All eleven run before any positive check.

  The four verdicts mean different things rather than being synonyms for "not
  proceed": REMEDIATE is a fixable signal on a trustworthy candidate (failing
  tests, invalid config, stale conformance, a merely paused rollout); QUARANTINE
  is a candidate that is not trustworthy (schema mismatch, a rollback target
  Admit refuses, a rollout already fenced); STOP is absence of evidence or an
  explicit open blocker. A paused rollout remediates where a fenced one
  quarantines, and an open blocker stops rather than remediating -- both proven
  separately.

  Composed rather than re-derived throughout: artifact evidence wraps
  VerifyBundle's Verification, rollback evidence wraps Admit's AdmissionDecision,
  rollout evidence reads a Snapshot's already-computed status, and signing reuses
  the existing KeySource path with the signature excluded from the digest.

## 2026-09-12 (EDGE-007)

- Close EDGE-007: propagate overload, retry and circuit state end to end.
  `internal/connectivity/edge` had no overload, retry or circuit concept -- and
  neither did anywhere else in the repository, so the circuit breaker is
  genuinely new. `overload.go` adds `Coordinator.Decide` producing exactly
  ADMIT/QUEUE/DEFER/DEGRADE/REJECT, a `CircuitBreaker`, and a `RetryLedger`. No
  migration needed.

  This was the third todo in a row over one bounded logical retry budget, after
  EVENT-003 and INTG-015. Rather than a third ledger it composes
  `internal/operations/admission` directly: `DecideBackpressure` for the health
  signal, `Provisioner` as the single replay-safe budget owner, and
  `admission.Outcome` as the RED vocabulary itself. The admission package was not
  modified. Nesting is what makes it work: an edge retry, a connector retry and a
  transport timeout observing the same physical attempt compute the same
  identity and collide on one stored receipt instead of charging three times.

  The action-to-outcome mapping is total on purpose. `BackpressureAction`
  (CONTINUE/SLOW/QUEUE/DEFER/STOP) is a near-match for the RED vocabulary
  without being the same, which is exactly the shape that produces a partial
  mapping where an unhandled value falls through to ADMIT. Unrecognised actions,
  unrecognised retry dispositions and the zero-value circuit state all map to
  REJECT.

  Review fix: staticcheck flagged a leftover `combineOutcomes` helper whose
  most-restrictive-wins logic `Decide` already applies inline. Removed as
  duplication rather than wired in.

## 2026-09-12 (CICD-005)

- Close CICD-005: canary rollout with evidence-bound rollback. `rollout.go` adds
  a staged rollout that advances only while health holds and pauses -- not
  fails, not silently continues -- on any of four breach signals, each with its
  own sentinel. An already-paused rollout refuses `Advance` outright without
  evaluating anything; clearing a pause requires the distinct `Resume`, which
  re-evaluates fresh evidence. No migration needed.

  Rollback does not reimplement digest matching. It calls CICD-004's `Admit`
  directly, so an untrusted bundle fails as unsigned and a validly-signed bundle
  that is not the approved digest/scope fails as incompatible -- rolling back to
  "the previous thing we saw" is not possible. A refused rollback leaves status
  untouched and still records the refusal, so history survives refusals too.

  `evaluateStageHealth` checks presence before value for all four signals, so
  absent evidence is incomplete rather than healthy, and `Reopen` requires both
  health and reconciliation evidence to carry a non-zero timestamp before their
  booleans are read at all.

  Review fix: staticcheck flagged a dead store where a test appended to a
  returned history slice and discarded the result. The append is part of the
  mutation attempt -- a returned slice with spare capacity would write straight
  into the rollout's own backing array -- so it now asserts the caller's copy
  grew to three while the store stayed at two, which is a stronger test than
  deleting the line would have been.

## 2026-09-12 (SVC-008)

- Close SVC-008: host connector execution as a `cmd/worker` role. New
  `connector_role.go` follows the established `messagingDeliveryRole` shape and
  funnels every dispatch through one path: reserve capacity on the
  `ConnectorLedger` (INTG-015), claim a fenced lease on the operation journal
  (INTG-011), obtain a credential bound to the operation's own destination, and
  only then dispatch. Each of RED's four bypasses -- no queue claim, no
  credential lease, no rate policy, no journal entry -- is independently
  unreachable and independently tested. No migration needed.

  Provider neutrality holds: the role imports only the operation journal,
  custody, lease and bootstrap packages. No provider package.

  Scope boundary, recorded rather than glossed: production wiring uses
  fail-closed credential/writer/authorizer stand-ins, because no
  destination-credential authority or connector provider adapter exists yet.
  That follows the `unavailableMessagingTransport` pattern already in
  `messaging_role.go`. The `connector-role` flag defaults to false and its usage
  string says plainly that it is inert until those land. The role's own contract
  is proven against a real journal, ledger and credential source; what is
  deferred is the production authority it binds to.

## 2026-09-11 (CICD-004)

- Close CICD-004: fail-closed deployment admission. `tools/policy/release`
  already built and verified signed bundles (CICD-003) but nothing turned a
  candidate plus an approved target into a typed admit/reject. New
  `admission.go` adds `Admit` with five distinct sentinels so a caller can
  branch on why something was refused. No migration needed.

  The GREEN clause is enforced separately from signature validity, which is the
  part worth stating plainly: after `VerifyBundle` succeeds, `Admit` compares
  the verified manifest digest AND the candidate's scope field-by-field against
  the approved target. A different but validly-and-trustedly-signed bundle, or
  this exact bundle offered for a scope it was not approved for, is refused.
  "It is signed" is never "it is approved".

  All six absence checks -- empty trusted-key set, zero-valued approved target,
  non-positive staleness window, missing scope, unscanned vulnerability
  evidence, conformance record with no timestamp -- run before any positive
  verification, so a missing input can never be shadowed by a check that happens
  to pass. Evaluation time is a parameter rather than a buried `time.Now()`, and
  a future-dated conformance record is stale too.

  Coverage note: the package sat at 66.1%, below the floor, before this todo --
  `Version`, `Verify`, `Verification.Explain`, `NewFixtureKeySource` and
  `VerifyBundleWithScannerEvidence` had no test anywhere in the repository. That
  gap is now closed by a separately-labelled test carrying real assertions,
  including a negative case.

## 2026-09-11 (CONN-RT-007)

- Close CONN-RT-007: normalized observations, ambiguity and targeted redrive.
  Both RED properties were already implemented by INTG-011/INTG-016 and are now
  pinned by tests rather than rewritten -- provider acceptance leaves the
  operation in PROVIDER_ACCEPTED requiring an observation, and a timeout or
  MayHaveSent failure lands AMBIGUOUS, which is not a leaseable state, so a
  blind resend is impossible rather than merely discouraged.

  The genuine gap was the todo's word "normalized". Every existing caller
  hand-built an `Observation` literal, which meant a caller could assert
  `ObservationApplied` for any reason it liked -- the verdict was an assertion,
  not a measurement. New `ProviderReadBack` and `NormalizeObservation` compute
  it by comparison: an ambiguous read is UNKNOWN, a missing record is
  NOT_APPLIED, a digest matching an attempt this operation actually dispatched
  is APPLIED, and anything else found at that resource is CONFLICT. An empty
  observed digest explicitly refuses to match, so a blank read-back cannot pass
  as applied.

## 2026-09-11 (ARTIFACT-006)

- Close ARTIFACT-006: verify and repair artifact-byte integrity.
  `internal/store/object` had no integrity checking of any kind -- nothing
  detected bit rot, compared replicas, quarantined, or produced a receipt. New
  `integrity.go` adds `IntegrityStore` with verify-on-read, quarantine, repair
  and an immutable receipt log. No migration needed.

  The design point that matters: ground truth (digest and generation) is always
  read live from `Stat`, never from anything a replica claims about itself. That
  is what makes a generation swap detectable -- a stale replica whose bytes hash
  exactly to what that generation always produced is internally consistent and
  would otherwise pass.

  Review found a TOCTOU in the one guarantee the todo exists for. `Repair`
  called the exported `Verify`, which releases the mutex, then re-read the
  source replica under a second lock. A concurrent `PublishReplica` landing in
  that window would have made repair copy bytes nothing had verified. Evaluation
  and copy now happen under a single lock hold via the pure `evaluateReplica`,
  so the bytes written are byte-for-byte the ones that passed.

## 2026-09-11 (ARTIFACT-004)

- Close ARTIFACT-004: object bytes are sealed with tenant-bound envelope keys.
  `internal/store/object` handled bytes in the clear; new `sealed.go` adds
  `SealedObjectStore` (Put/Get/Stat/RewrapOne/RewrapBatch) over
  `internal/trust/envelope.Manager`. No crypto was hand-rolled and no migration
  was needed. There is no plaintext fallback: an unregistered tenant KEK or an
  unavailable custody provider returns `ErrEncryptionUnavailable` and leaves the
  store untouched rather than writing cleartext. Rewrap is a compare-and-swap
  per object, so an interrupted batch is resumable and never leaves an object
  unreadable, and crypto-erasure keeps `Stat` truthful about an object that can
  no longer be decrypted.

  Found while wiring it, raised separately rather than fixed here:
  `envelope.Encrypt(ctx, objectID, plaintext)` validates `objectID` and then
  never folds it into the AES-GCM AAD -- `headerBytes` authenticates only
  Version, Tenant, DEKID, Algorithm and Nonce. Two envelopes of one tenant can
  therefore be swapped and both still decrypt, while the parameter reads to any
  caller like context binding. ARTIFACT-004 defends itself with a
  `sealedPayload{ObjectID, Data}` wrapper re-checked after decrypt, which is
  deterministic and fail-closed, but the cryptographic binding belongs in
  TRUST-028 and a change there has envelope-format compatibility consequences.

## 2026-09-11 (INTG-015)

- Close INTG-015: rate-aware fair connector scheduling. `internal/connectivity/
operation` had no rate, quota, concurrency or throttle concept at all, and
  `ProviderError` carried no reset/limit fields, so a provider 429 read as an
  ordinary FAILURE and each one minted another retry. New `quota.go` adds
  `ConnectorQuota`, `ConnectorPolicy` and `ConnectorLedger` with per-tenant,
  per-resource, per-connection and criticality-reserved admission, `Observe429`
  honouring the vendor's own reset time, and queue age plus predicted completion
  on every decision. No migration needed.

  Deliberately not built on `internal/data/outbox`'s EVENT-003 scheduler: a
  connectivity kernel package importing a data-layer consumer package would
  invert the module's layering, and the two need opposite defaults -- an unnamed
  internal resource is unbounded, an unmeasured external vendor must not be. The
  rejection/claim/release shape is kept parallel so they can be reconciled later
  if a shared abstraction is ever justified.

  Review caught the fail-open case before the tick: every check is gated on
  `Limit > 0` / `MaxConcurrent > 0`, so a connection named in the policy map but
  given no quota resolved to the zero value and admitted 50 of 50 reservations.
  That is RED's "unknown quota assumes unlimited" landing on the struct's own
  default -- a caller who sets a tenant share and has not yet looked up the
  vendor's rate limit would have got unlimited vendor traffic. A wholly zero
  quota now means UNDECLARED and resolves to `UnknownConnectorQuota`; a negative
  bound is the explicit unbounded escape hatch. The FAULT test pins both.

## 2026-09-11 (EVENT-003)

- Close EVENT-003: enforce queue priority, backpressure and one global retry
  budget. `Consumer.Poll` orders by criticality only _within_ one tenant's rows
  (`WHERE tenant_id = $1`), so nothing arbitrated across tenants or across a
  shared downstream resource, and a P3/P4 flood could crowd out a P0
  payroll/IAM item. New `outbox.Schedule` + `ResourceLedger` arbitrate a
  combined candidate pool by criticality first, globally, against shared
  resource capacity with an optional per-tenant share.

  `internal/data/outbox` also had no reference to `internal/operations/admission`
  at all, so a downstream slow-down signal never reached the queue.
  `ResourceLedger.ApplyBackpressure` now folds a real
  `admission.BackpressureDecision` into the next pass. And retry accounting
  lived in three unconnected places (outbox backoff, the coordinator's retry
  callback, admission's budgets), so nested layers each charged their own retry
  for one logical attempt; `AttemptIdentity` + `RetryAccount` route every layer
  through one `admission.Provisioner`, so a replayed attempt collides on one
  stored receipt. Wired into `failRow` behind an opt-in `WithRetryAccounting`,
  nil by default, so existing callers are unaffected. No migration needed.

  Review caught two defects in the backpressure switch before the tick. It read
  its baseline from `l.policy[resource]`, whose zero value is capacity 0, so a
  CONTINUE/healthy decision on a resource with no configured policy wedged that
  resource shut permanently -- the signal meaning "everything is fine" causing a
  total stall. Separately, integer division made QUEUE/SLOW on a capacity-1
  resource admit nothing, silently turning SLOW into STOP. Both are now explicit
  branches and the FAULT test pins them.

## 2026-09-11

- Add `hcmnext serve -public-origin` (`HCMNEXT_PUBLIC_ORIGIN`): declares the
  absolute http(s) origin browsers reach the cell at for deployments behind
  TLS-terminating or Host-rewriting proxies. When set it becomes the only
  admitted browser origin, normalized cookies carry `Secure` under https, the
  workspace emits its gRPC tunnel URL and CSP `connect-src` against the
  declared authority, and tunnel upgrades whose `Origin` host matches it
  admit. With no flag the same-origin default for localhost and direct VPS
  serving is unchanged. Ticked as `EDGE-011` (see devlog).
- Close EP-PROMO-001's reopened gap: a facade propose now records the durable
  candidates migration 00024 defines instead of leaving the minted proposal
  in memory only. `internal/intent/app/journey_candidates.go` writes the
  input snapshot (purpose SIMULATION, the request digest and source
  baselines in the canonical body), the proposal revision with its
  EncodeFullProposal payload and produced_by `hcmnext:intent-cell`, the
  ordinal write/approval item sets, and the READY simulation result binding
  both - all inside one tenant-scoped transaction from both
  `journeyEngine.Propose` and `ProposePromotion`. Candidate identities are
  derived under a fixed namespace rather than allocated, so a replayed
  request re-derives the same keys and the stores' ON CONFLICT semantics make
  it a no-op. The generic `SimulateIntent` path still writes none of these
  rows, keeping the P1A zero-authoritative-mutation contract intact.
- Serve the human-work queue read surface (EP-WORK-001): `WorkService`'s
  ListWorkItems and GetWorkItem are now composed on the gRPC surface, the
  websocket tunnel and the Connect edge. Membership, visibility
  classification and the server-computed permitted-action set are the
  workitem package's read rules (new `view.go`); a new durable
  `Store.ListQueue` answers the principal's actionable queue in
  deadline/identity order behind `tenant_isolation` RLS, and an
  application-level `WorkItemQueueReader` port hands the transports the
  raw records. The wire `WorkItem` gains proposal_ref, claimed_by,
  claim_expires_at and repeated permitted_actions; the queue cursor is an
  HMAC-signed token bound to principal/tenant/scope, the queue snapshot
  digest and a five-minute expiry. Absent, invisible and other-tenant
  reads answer the identical non-disclosing NOT_FOUND, and the four
  mutating methods refuse FAILED_PRECONDITION on both transports per the
  P1A contract. Composition golden and conformance rows updated for the
  new `work-item-queue-reader` port.

## 2026-09-10

- Harden the admission retry evidence tables: 00282 scopes both primary
  keys by tenant and repairs the cross-boundary foreign key from 00280,
  and 00283 forces row-level security on both tables (00280 enabled RLS
  without FORCE, which the parity gate rejects). 00281's Down now refuses
  with P0001 like its neighbors so the admissionstore evidence wall holds;
  manifest re-signed and evidence reports regenerated.

- Fence concurrent duplicate outbox delivery: ConsumerGroup.Dispatch now
  tracks one in-progress dispatch per applied key so two concurrent
  deliveries of the same record cannot both pass the applied check and run
  the handler; waiters block until the runner finishes, then re-check.
  Also give the lease-expiry break test Run its own connection: sharing one
  connection with the test's read-polling loop failed the first Begin with
  "conn busy" so Run returned before any delivery (same pattern the
  idempotency test already documents).

- Clear all 266 staticcheck findings to zero (unused code, deprecated
  APIs, ineffassign, staticcheck style batch) with no behavior change,
  except one genuine swallowed error in the rules engine that now
  returns; quality gate fully green. The todo-traceability scanner now
  skips dot-directories so lane scratch files never register ghost tests.
- Repair the gates the sweep's blast radius exposed: add the missing
  frontenddev ownership row (the rebrand left the dev server without one),
  correct the release-admission SBOM pin to the checked-in file's digest,
  fix the P1A manifest's forbidden-import prefixes to the renamed module
  path, and refresh the Phase 1 manifest golden for the current tree.

- Backfill governance evidence for 43 completed todos so GOV-017 and
  GOV-003 pass with no new allowlist entries: LEAVE/AVAIL/BAL/INTENT/
  LEGAL/MSG/REPLAN/WORK/WF-DISC/OBS-013/WEB-024..036 evidence now names
  the passing Go test and its `go test` result (all re-verified green),
  and UX-006/007/008 plus CLIENT-001/002 record their passing vitest
  suites behind a reviewed traceability allowlist (see devlog).

- Pin exact dependency-roles rows for the OTel and genproto modules so
  the admission gate passes with vuln evidence required; no dependency
  versions change.

- Fix four staticcheck findings the merge introduced (struct conversion,
  raw regexp literal, two dead helpers) so the quality gate failure set
  matches pre-merge main; the remaining findings are pre-existing debt.

- Record the formatting sweep and UX evidence (docs): prettier table
  rewraps across the rebrand-renamed specs, plan and design mock, the
  supply-chain provenance array collapse, and the blind UX review log
  that evidences the UI refinement batch — no content changes.

- Land the product UI refinement (WEB-027, WEB-037, WEB-039, WEB-040,
  WEB-045, WEB-063): blind-review repairs across shell, navigation,
  launcher, identity and record pages — popover focus containment,
  hydration-gated router mount, explicit shell props, worker-ID page
  previews, i18n and typed-style migrations — with the wasm shell
  contract holding and embedded journey assets regenerated.

- Land the worker/journey/diagnostic hardening (no dedicated ticks —
  registry gap, see devlog): bounded worker-ID previews replacing an
  unbounded exclusion loop, worker-specific declared baselines with
  unknown-worker refusal, shared declared-baseline fixtures, and
  bounded error-type diagnostics with a capability grant so log fields
  never carry payloads — all tested, all above their coverage
  baselines.

- Land the domain kernels (CONF-006, PROMO-008, DATA-015, EVIDENCE-001,
  OPS-006, DOC-SIGN-001, ARCH-GO-024): payroll correction with
  bitemporal repair, promotion persistence traces, transaction lineage,
  business execution receipts, epoch-fenced workload drain, proof-bound
  e-signature ceremonies and the configuration kernel — all pure,
  deterministic and matrix-tested.

- Land the planning and policy tooling (GOV-003, GOV-021, GOV-026,
  GOV-028, GOV-029, GOV-030, CLOSE-001, WF-DISC-001 through WF-DISC-010,
  ROLLOUT-001, IAC-001, ARCH-GO-024): design-closure and ownership
  registers, direct-capability and oracle-strength/specificity gates,
  engine-ownership and IaC-stack validators, the full workflow-design
  toolchain, rollout-plan activation, config boundaries and a crosswalk
  test repair tracking the implemented E-16 control — all matrix-tested
  above the floor. (The traceability dot-directory test stays unlanded:
  its package is red on pre-existing evidence-less ticks; see devlog.)

- Land the conformance proofs (CONF-022, CONF-023, CONF-024, WF-STEP-007,
  WF-STEP-008): bounded bulk acknowledgement with honest legal
  satisfaction, hostile-content-proof case triage, the twelve-case
  adversarial edge matrix, and bounded PARALLEL plus typed JOIN
  strategies — every verdict deterministic, sealed and matrix-tested.

- Land the timer-resume span links (OBS-013): the drift-checked timer
  row's stored causal identity opens exactly one hcmnext.timer.resume
  span back to the parked trace — nil, expired, tampered or missing
  links advance unlinked with identical business behavior, human-work
  resumes never open one, and the resume span emits no log line.

- Land the agent trust kernels (AGENT-003, AGENT-004, AGENT-005):
  owner-bound draft ingestion with fuzz-pinned refusals, immutable eval
  runs gating publication with kill-switch lease revocation and
  version-bound incidents, and one action compiler emitting attributed
  receipt-bound drafts for concierge and domain agents — invented
  definitions, hidden uncertainty and bulk loops refused with zero
  draft.

- Land the rule re-evaluation kernel (RULE-004): approved promotion
  plans re-run their rules at execution time — identical inputs confirm,
  moved inputs require re-approval even when the tier holds, moved tiers
  and unknown inputs invalidate, and every verdict cites both rule
  versions with tamper-evident input digests.

- Land the schedule kernels (INTENT-017, SCHED-003): frozen occurrences
  convert to exactly one intent with DST-fold twins named apart and
  replay/leader-overlap/post-crash convergence, and the dispatcher
  ingests occurrence/event firings in sequence with redelivery replay,
  rewind refusal and stale/poison handling — revisions surface as
  review receipts, never silent adoption.

- Land the compensation kernels (CONF-020, COMP-005): standalone
  compensation change with exact-decimal simulation, live-budget-fence
  reservation, atomic commit and failed-effect-only repair, plus parent
  and bounded child intent composition with omitted components carried
  at exact revisions — every negative case sentinel-typed.

- Land the governed cancellation kernels (WF-RUN-010, WF-RUN-011,
  WF-STEP-009): pure cancellation decisions over pinned subworkflow
  children — clean cancel keeps per-child reports and intact history,
  irreversible effects route to compensation or refuse, unknown states
  repair, detached children keep accountable owners, and expansion
  pins versions, attenuates scope, bounds depth/fanout and rejects
  cycles with sentinel-typed errors.

- Land the headcount requisition proof (CONF-021): approval authorizes
  capacity only over the HEADCOUNT-001 API with no production change —
  requester-chosen approvers and missing quorum/SoD refuse, external
  REQUESTED never counts as created, competing proposals fence exact
  decimal FTE/count/budget with ErrCapacityConflict, and the lifecycle
  transcript is golden-pinned.

- Land the execution conflict preflight (CONFLICT-004): execution-time
  re-evaluation of the approval-pinned intent set against the current
  set under one versioned policy — post-approval collisions block with
  a per-overlap explanation, post-approval creations need reapproval,
  drift needs replan, identical sets clear, and the read-only
  evaluation carries a deterministic digest.

- Land the availability revision (AVAIL-003): append-only per-worker
  revision log with prepare/commit two-phase recovery — availability
  never exists without its owning leave event, stale heads, inverted
  or overlapping incompatible intervals, off-vocabulary states and
  duplicate leave effects all refuse, recovery commits unfinished
  prepares exactly once, and zero-padded sequence IDs keep History in
  append order past the tenth revision.

- Land configuration rollback (CP-009): the exact prior bundle is
  revalidated by digest, re-signed through the caller's capability and
  activated at a new epoch with receipts — history intact, mutable
  labels, revoked digests and pinned live-workflow versions refused.

- Land the legal-name kernel (CONF-019): structured multilingual names
  with NFC normalization and single-script parts, scanned evidence
  cites, jurisdiction obligations, exact approval digests and one
  atomic revision — downstream effects authorize separately, never
  implicitly.

- Land the admission provisioner (ADMISSION-002): durable retry-budget
  provisioning per logical operation with replay-safe attempt
  identities, bounded refunds and one stable repair route — the
  provisioner owns the counters, the pure policy stays the only
  decision.

- Land the runtime kernels (WF-RUN-006, WF-RUN-007): the immediate-caller
  retry policy (capped backoff, shared budget, DO_NOT_RETRY honored
  above all) and poison-node quarantine that never drops work and never
  reports success — exhausted nodes route to BLOCKED, REPAIR_REQUIRED
  or QUARANTINED with everything retained.

- Land the intent kernels (INTENT-016, INTENT-018, INTENT-019,
  INTENT-021, INTENT-026, INTENT-027, WF-DISC-011, REPLAN-002,
  REPLAN-004): deterministic child emission, event-to-intent policy
  conversion, bulk batch compilation, composition with DAG validation,
  universal preflight, governed lifecycle surfaces, approval reuse and
  successor proposals — every negative case typed, every digest sealed.

- Land the org kernels (CONF-007, CONF-025): the governed manager-change
  lifecycle (propose through reconcile, cycles and overlaps refused,
  tamper-evident approvals) and the conformance-only ChangeManager
  intent compiler with typed block/replan negatives and zero production
  exposure.

- Land the outbox consumer group (EVENT-002): idempotent dispatch with
  per-partition checkpoints, fenced duplicates and per-record poison
  isolation with expiry requeue; checkpoints commit with application,
  never ahead of it.

- Land the contact kernels (CONF-017, CONF-018): typed endpoint updates
  with tenant isolation, stale-revision refusal and verification
  challenges, plus ordered emergency-contact sets with priority
  uniqueness, governed person linkage and minimum disclosure.

- Land the work-item kernels (WORK-007, WORK-008, WORK-009): fair
  queue ordering with legal-authority precedence and reserved tenant
  capacity, compartment authorization with recorded access decisions,
  and typed restricted-review findings behind evidence grants.

- Land the delivery kernels (MSG-007, MSG-008, MSG-009, MSG-010,
  MSG-013): honest attempt/recipient lifecycles, typed workflow
  signals, bounded failure reconciliation, content-gated redaction
  scanning and legal-notice assessment — provider acceptance alone
  never proves delivery, and protected content never reaches an
  operational surface.

- Land the balance kernels (BAL-012, BAL-013): the atomic posting
  transaction (one head per account, idempotent receipts, failpoint
  rollback) and the availability dependency index that emits typed
  REPLAN_REQUIRED findings instead of editing approved plans.

- Land the legal entitlement kernels (LEGAL-003, LEGAL-004, LEGAL-006):
  entitlement composition that never lets company policy narrow statutory
  protection, watermark-bound rule-change impact assessment, and the
  sealed obligation lifecycle (open/satisfy/waive/block/age) —
  kernel-pure and digest-sealed.

- Land the Medical Leave domain kernels (LEAVE-002–LEAVE-011, LEAVE-014,
  LEAVE-017): snapshot intake, eligibility resolution, entitlement planning,
  simulation/proposal, transaction compile, atomic commit, determination
  rendering, effect reconciliation, replan, successor intents and the
  restricted evidence-review loop — kernel-pure, digest-sealed, zero
  workforce mutation outside the governed commit path.

- Persist Medical Leave and Return-to-Work domain state (DB-023): migration
  00281 adds twelve tenant-scoped tables — request/record revisions, program
  eligibility, entitlement segments, absence and availability links, balance
  postings, evidence refs, work restrictions, obligations, intent links and
  role-gated medical detail — with CAS revisions, FK/idempotency/overlap/
  employment/compartment guards under RLS.

- Add the idempotent Leave process anchor (LEAVE-016): one capability
  invocation persists the `RequestLeave` intent as `CHANGE_REQUEST` with its
  child bindings, the requested revision and the `LeaveRequested` provenance;
  replays return the stored anchor, conflicting client-request reuse refuses,
  and no workforce mutation or external effect is performed.

## 2026-09-09

- Give promotion comparisons full content width so desktop pay deltas remain visible.

- Show promotion change comparisons before request metadata in the review panel.

- Keep promotion confirmation cancellation accessible and clear inherited primary
  gradients from secondary journey buttons.

- Correct promotion effective-date guidance: simulation applies promotion policy,
  rather than universally forbidding past dates.

- Reconcile shell golden fixtures with verified non-modal launcher accessibility;
  the full product UI package passes again.

- Make Help task-oriented and permission-aware, with self-service destinations
  and localized access guidance instead of inaccessible promotion instructions.

- Correct profile workflow empty-state advice for users without create permission;
  job-ladder remediation is reserved for authorized requesters.

- Align action-launcher dialog/combobox accessibility with non-modal keyboard
  behavior and distinguish unmatched searches from unavailable authorized actions.

- Make Insights totals use the same discoverable workflow population as the
  other metrics; cover denied and missing-verdict records in regression tests.

- Seed menu queries in the production loading shell so hydrated search inputs
  agree with filtered results after reload; cover escaping and whitespace.

- Promote matching support pages into menu search results without duplicates,
  retain recovery links and show an explicit no-results state. Restore the full
  product UI test suite with current localized Settings expectations.

- Preserve incomplete worker-ID numeric edits, disable invalid submissions with
  localized guidance, and apply theme-aware native disabled-button styling.

- Make worker-ID examples follow unsaved form edits using shared non-allocating
  formatting. Replace unbounded reserved-range scanning with increment-aligned
  range jumps; add draft, exhaustion and large-range regression coverage.

- Refine Admin navigation and role-visibility layout: omit unconfigured hero
  actions and collapse the role summary behind a localized native disclosure,
  preserving the visible policy boundary and role-filtered configuration cards.
  Add focused rendering regressions and verify the updated desktop UI.

- Tick WF-DISC-010: new kernel-pure `tools/planning/scenariomatrix`
  generates adversarial scenario matrices from every workflow design
  (22-template table; live snapshot yields 230 scenarios plus 78 typed
  justifications across all 14 definitions); package tests PASS
  (incl. concurrent race test), vet clean, cover 89.8%.

- Tick WF-DISC-009: new kernel-pure `tools/planning/designownership`
  validates every workflow design against model/engine/authority
  ownership with exact resolutions plus atomic CONTRACTED-blocking todo
  candidates (live snapshot: 8 resolutions, 46 candidates, full
  reference coverage); package tests PASS, vet clean, cover 83.0%.

- Tick WF-DISC-008: new kernel-pure `tools/planning/directcapability`
  proves DIRECT dispositions hold no durable machinery or state mutation
  and that executions pin authorization, return honest typed results and
  promote actions only as new unexecuted intents (live snapshot: all 5
  DIRECT designs prove pure); package tests PASS, vet clean, cover 92.0%.

- Tick WF-DISC-007: new kernel-pure `tools/planning/workflowexpansion`
  compiles archetype recipes plus domain profiles plus intent deltas into
  complete ordered responsibility graphs with typed nodes, sequence plus
  repair edges, ordered effects and stable digests (live snapshot expands
  all 14 definitions); package tests PASS, vet clean, cover 90.4%.

- Tick WF-DISC-006: new kernel-pure `tools/planning/workflowdesignjoin`
  joins each drafted definition to exactly one design disposition by
  versioned definition ref (live snapshot joins 14/14 with zero missing,
  duplicate or alias-only records); extends `tools/planning/workflowdesign`
  records with the optional validated `definition` field and grows the
  seed corpus to the 14 accepted definitions; both suites PASS, vet
  clean, cover 84.6% plus 84.3%.

- Tick CLOSE-001: new kernel-pure `tools/planning/designclosure`
  compiles the design-closure register joining every accepted scope item
  to source, owner, phase, decision state, artifact, todos, tests,
  evidence digest and expiry with gated blockers and reconciling totals;
  live snapshot resolves 14 rows over 14 accepted intents; package tests
  PASS, vet clean, cover 94.0%.

- Tick GOV-028: new kernel-pure `tools/planning/oraclespecificity`
  rejects placeholder RED/GREEN oracles (`PLACEHOLDER_RED_ORACLE`,
  `PLACEHOLDER_GREEN_ORACLE`) and unbounded persistence/ledger/outbox/
  human-work/provider claims (`MISSING_PROHIBITED_EFFECT_ORACLE`) with
  live-backlog conformance and fuzz coverage; package tests PASS
  (incl. a 60s 3.27M-exec fuzz run), vet clean, cover 90.2%.

- Tick GOV-021: new kernel-pure `tools/planning/oraclestrength`
  classifies todo oracles as `WEAK_ORACLE` when they assert only
  execution signals, lean on a coverage percentage, omit
  prohibited-effect bounds, snapshot unstable output, accept either/or
  alternatives or name no concrete failing case; package tests PASS
  (incl. fourteen oracle mutations), vet clean, cover 84.0%.

- Tick WF-DISC-005: new kernel-pure `tools/planning/workflowdesign`
  defines the machine-readable WorkflowDesignRecord contract with closed
  DIRECT/WORKFLOW dispositions, explicit NOT_APPLICABLE reason codes,
  canonical digests and stable Go/Protobuf registries regenerated by
  `cmd/workflowdesign`; package tests PASS (incl. sixteen seeded
  single-gap mutations each yielding their exact code), vet clean,
  cover 85.7% plus 83.3% for the command.

- Surface promotion actions ahead of supporting details and simplify stage
  descriptions. Keep established quiet journey feeds reconnecting after normal
  transport deadlines, while retaining bounded retries for immediate failures.

- Correct Jane's demo promotion baseline to her declared reference inputs;
  prevent other workers from inheriting Omar's compensation. Add safe OTel
  baseline-source events and worker-specific regression coverage.

- Add safe bounded request-error diagnostics for promotion troubleshooting;
  distinguish schema mismatch, database conflicts, cancellation and deadlines
  without logging internal payloads. Fix stale approval success notices when
  live promotion updates deliver a recorded ledger outcome, with regression tests.

- Tick IAC-002: new kernel-pure `tools/policy/iacstack` builds the four
  isolated stacks (dev/test/stage/production-cell) from one pinned module
  graph with reviewed variables, per-stack credentials, explicit bounds and
  redacted deterministic plan summaries; package tests PASS (incl. a
  seeded-mutant sensitivity probe), vet clean, cover 87.8%.

- Tick WF-DISC-001: new kernel-pure `tools/planning/workflowregistry`
  registers the 42-file workflow research corpus with truthful discovery
  states, unique ids, index-link integrity and accepted-intent resolution
  (live gaps reported as exact findings: 12 missing owners, 4 missing
  intents, 3 missing kernel families, 5 unregistered samples, 3 non-catalog
  targets); package tests PASS (incl. a seeded-mutant probe), vet clean,
  cover 92.9%.

- Tick WF-DISC-002: new kernel-pure `tools/planning/workflowarchetypes`
  proves the six HR catalogs (198 rows) name only defined archetypes with
  explicit dependencies, data and step deltas and no duplicate intent
  placement; package tests PASS (incl. a seeded-mutant probe), vet clean,
  cover 88.4%.

- Tick WF-DISC-003: new kernel-pure `tools/planning/workflowdecisions`
  turns the eight register questions into owned safe boundaries (closed
  safe defaults executed against negative fixtures, strict deadlines,
  decision revisions); package tests PASS (incl. a seeded-mutant probe),
  vet clean, cover 89.9%.

- Tick WF-DISC-004: new kernel-pure `tools/planning/workflowpromotion`
  gates exploratory-to-contracted promotion on the complete closure set
  with ed25519 signed receipts (tamper-evident, expiry-enforced);
  package tests PASS (incl. a seeded-mutant probe), vet clean, cover
  86.5%.

- Tick IAC-001: the provider-neutral infrastructure resource contract in
  `tools/policy/iac` lands verified (nine-kind catalog, seeded fault/security/
  recovery oracles, race plus benchmark; package tests PASS, vet clean, cover
  94.6% against the 70% floor).

- Tick NEXT-008: the gate-dependency and assurance-closure checker in
  `tools/policy/phaseone` lands verified (later-phase/cycle/self-certification
  rejection with shortest witnesses, golden closure bytes, Gate B independent
  assurance plus verified cross-store restore; package tests PASS, vet clean,
  cover 87.2% against the 70% floor).

- Tick GOV-027: the intent-gap compiler in `tools/planning/intentmanifests`
  lands verified (23-dimension property suite, golden digest, live-catalog
  conformance at 238 gaps, duplicate-contract mutation oracles, fuzz seed
  corpus; package tests PASS, vet clean, cover 70.7% against the 70% floor).

- Refine workforce exploration: full-width team disclosures, readable worker
  cards, native reporting-line expanders, localized counts and safe handling of
  ambiguous manager names. Workforce now precedes supporting business metadata.

- Fix Journeys remaining on its loading screen when data arrives before the
  render subscription mounts; add a deterministic WASM lifecycle regression.

- Fix Start an action dismissal when focus leaves or the user clicks outside;
  preserve internal focus navigation and clean up browser listeners and timers.

- UX refinement (uncommitted): truthful explicit-role assignment display,
  worker-number disambiguation, published promotion-eligibility checks shared
  with the workflow form, and permission-aware task links in Help.

- UX refinement (uncommitted): separate breadcrumbs from page headings, clarify
  organization-wide appearance, simplify Home promotion entry and empty-work
  guidance, improve navigation label space, and explain unavailable page editing.

- OBS-013 timer-resume span links (branch eng/obs-013-resume-links): timer
  wake advancements open a finite `hcmnext.timer.resume` span linked to the
  parked trace from the drift-checked row's stored causal identity, with a
  fresh attempt reusing the stored logical operation; nil/expired/invalid
  links advance unlinked with byte-identical receipts. `timer_id` joins the
  telemetry attribute allowlist (contract + compiled mirror). Stale OBS-012
  fallback comment corrected to the landed topology contract.
- Rebrand to Human Capital Management Suite (uncommitted batch, this
  change): Go module `github.com/monstercameron/hcm-next` becomes
  `github.com/monstercameron/human-capital-management-suite` (go.mod, all
  imports, `.proto` `go_package`, regenerated protobuf output with the
  lock-pinned buf 1.72.0 / protoc-gen-go v1.36.12); sibling module
  `hcm-next-executor` becomes `human-capital-management-suite-executor`;
  npm scope `@hcm-next/*` becomes `@human-capital-management-suite/*`
  with lockfile relinked. Display strings, brand pack, docs, CI scripts
  and live planning specs follow the new name. Every affected golden was
  regenerated through its own sanctioned path (schemaflux/modelgen,
  buf-breaking baseline, connectorsdk, deferredschema, crosscut,
  phase-one manifest, archdoc, productslice, conformance, SBOM + signed
  windows/arm64 provenance, vuln-impact pins) with diffs verified to be
  exactly the rename plus derived digests. Crypto domain separators,
  persisted provenance/version markers, protocol audiences/issuers, client
  storage keys, DB identifiers, env vars, binary/proto-package names and
  dated history intentionally keep the old strings (compat surface);
  details in `planning/devlog/2026-09-09-rebrand.md`. Verified: whole-repo
  `go build`, executor build, `tools/gen/...`, policy/conformance/
  planning suites, `tsc` all workspaces, vitest root (159 tests) plus all
  workspace suites, eslint/prettier/gofmt gates. Known-red and unrelated:
  `sbomgen` cmd tests (license-completeness gate over unchanged tool
  deps), hermetic depadmission real-repo test (needs module network), and
  gateevidence migration-checksum test (fails identically at HEAD).

## 2026-09-08

- `OBS-016` telemetry correlation (this batch, uncommitted): new
  `internal/platform/telemetry/correlation` package joins structured logs,
  traces, metrics and business identifiers through owned business causation
  without conflating authority — trace identity can never become an
  idempotency, authorization or evidence key, metric labels refuse
  correlation/trace identity, and every signal join carries its own
  tenant/purpose scope. `effect.dispatch.duration` histogram joins the
  metric catalog (Go + `definitions/telemetry/metrics-catalog.yaml`) with
  an OTel adapter instrument and exemplar extraction. Full six-test matrix
  (primary, property, golden, security, conformance, mutation) plus two
  exemplar linkage tests pass; whole `./internal/platform/telemetry/...`
  tree green.

- Lane-work group commits (this batch) - `185f252f` (policy), `2133aa41`
  (data), `2dbc1db6` (domain), `f895257c` (trust), `d790d98c` + `a97b9cd6`
  (operations), `13f9f402` (transport), `6ec0e1a8` (ui), plus this docs
  commit. One held-back file (`scheduler.go`, pre-existing exactly-once
  race flake) and two held-back tool packages (signed P1A manifest needs
  the key holder; 5 ancient todos cite tests that never existed) stay
  uncommitted; details in
  `planning/devlog/2026-09-08-group-commits.md`. Traceability orphans cut
  261 to 5; fabricated benchmark names and inflated matrix names in lane
  evidence repaired with verified replacements.

- `DB-EDGE-003` (partial, uncommitted): adds read-only durable START resolution, explicit `RESOLVED` wire vocabulary and retry-only callback metadata. PostgreSQL recovery and adapter tests pass; application mapping and production admission composition are still under review. Resolution does not claim workflow completion or authorize replay.

- Durable retry-budget storage (this batch) binds tenant, service, dependency, logical operation and policy version, with immutable consumption receipts and PostgreSQL concurrency tests. Independent store coverage reached 81.7% before the latest attempt-identity refinement; production provisioning and consumption wiring remain open.

- Code-style discovery excludes only the repository-root `.artifacts` subtree, while retaining checks for real source and similarly named directories. The normal code-style gate now runs isolated-fixture regression tests before scanning. This tooling correction is pending commit.

- `READINESS-003` committed as `5440ed9`: deterministic, effect-free readiness evaluation with exact requirement/resolution pins, bounded explanations and fixed golden/integrity regressions. The two-file atomic commit passed the complete pre-commit hook, including formatting, lint, vet, coverage, policy checks, workspace and nested Go tests, and build.

- `MERIT-002` (this batch) completes exact budget/finalization conformance and durable, tenant-bound compensation child-intent emission. PostgreSQL tests prove rollback, concurrent deduplication, conflicting payload refusal and restart retrieval; timestamps retain nanosecond identity. Independent domain/store coverage is 70.1%/76.2%, vet clean.

- `MOBILITY-002` (this batch) supports targeted multi-host successors, strict dated extensions, scoped reevaluation, non-authoritative vendor reconciliation and independent return closure. Review fixed parent-slice mutation and ambiguous host targeting. Independent tests/vet pass at 71.6% coverage.

- `FX-002` (this batch) provides exact direct, inverse and approved triangulated Money conversion. Rational-oracle review exposed directed sub-unit and near-tie double-rounding defects; kernel division now rounds once from exact integers. Independent domain/kernel suites pass at 71.7%/71.2%, with vet clean.

- `TAXPROFILE-002` (this batch) validates and corrects effective-dated elections with durable successor fencing, preserved legacy rows, exact decimal scale and nanosecond intervals. Independent domain tests/vet pass at 71.3%; PostgreSQL review passes at 73.8%, with root verification of sub-microsecond bounds. Closed-payroll corrections remain governed drafts.

- `SERVICE-003` (this batch) preserves prior service explanations and emits scoped correction drafts, including dependencies affected by removed credit or rule-only changes. Protected outcomes require governed correction. Independent matrix tests and vet pass at 78.4% coverage; no execution authority is inferred from a draft.

- `LEGAL-013` (this batch) implements cited locality-only preemption before trigger evaluation, exact registered overlay resolution, and signed unregistered-locality evidence with optional fail-closed policy. Five state golden fixtures and real resolver/evaluator tests pass; package coverage is 82.2%, with vet clean. No legal-review approval is fabricated.

- `AGENT-002` (this batch) adds opaque semantic evidence, payload-bound admission/result receipts, fail-closed detector handling, and source-citation/taint propagation through inference. Review closed caller-forged facts, admissions and inference laundering. Package tests/vet pass at 95.6%; independent fuzzing passed 42,824 executions.

- `READINESS-004` (this batch) adds bounded, idempotent follow-up drafts with explicit human-review and governance-approval requirements. Requirement/evaluation/policy pins and gate metadata are integrity-bound; no execution authority is granted. Sol and root review completed; package tests and vet pass at 76.8% coverage.

- `CAREER-002` (this batch) passes domain conformance review. Recommendations require verified consent, preference and eligibility evidence; application drafts require revalidation of the exact assessment and current action basis. Publicly forged assessments and withdrawn consent/roles cannot bypass the verifier. Package tests and vet pass at 45.5% coverage under the existing exact exception; live recruiting adapters remain separate.

## 2026-09-07

- `SAFETY-002` and `SUCCESSION-002` (this batch) pass domain conformance review. Safety resubmission requires a fresh observed rejection and preserves filing/payment/restriction lineage. Succession verifies readiness freshness and correction-source ownership through injected authority ports and withholds confidential candidate metadata. Scoped tests/vet pass at 58.5% and 52.6% coverage under existing exact exceptions; live filing/payment/authority adapters are not claimed.

- `CBA-002` (this batch) adds typed constraint composition with explicit policy, mandatory-bound preservation, separate minimum/maximum provenance and stale seniority rejection. Review removed a mutable registry and fixed zero-bound ambiguity, mixed units, duplicate identities and order-dependent provenance. Scoped tests/vet pass; 52.9% package coverage uses the existing exact exception, while composition paths exceed 91%.

- `CONTACT-002` and `SERVICE-002` (this batch) pass scoped review and tests. Contact reference-store reissue now binds exact retry receipts; primary selection is permutation-safe and rejects equivocation. Service separates total credited days from continuous service, handles refused/trailing breaks, and keeps legacy model digests while versioning expanded evaluations. Coverage is 54.4% under the existing contact exception and 77.4% for service; scoped vet passes.

- `ASSET-002` (this batch) passes domain conformance review at 75.0% coverage. Loss/return transitions preserve worker attribution, recovery retries bind the exact operation, and offboarding obligations resist caller slice mutation and cannot close with unresolved returns. Scoped tests and vet pass; external inventory observations remain non-authoritative.

- `PROOF-002` and `EQUITY-002` (this batch) pass domain conformance review. Regression fixes prevent future-dated evidence from authorizing work early, preserve historical eligibility across corrections, keep pre-vesting forfeitures effective after the scheduled vest date, preserve grant worker identity, and reject mutated effect digests. Package tests pass at 88.3% and 73.7%; scoped vet passes. These are domain contracts, not live scheduling or payroll adapters.

- `ATTEND-002` (this batch) adds exact late/early/missing/unscheduled findings and typed punch directions. Review fixed slice-order-dependent results, ambiguous assignments, and overlapping missing-side durations. The direction-aware contract advances to version 2; targeted tests pass at 87.3% coverage, including DST, split shifts, invalid sequencing, and no-double-count regressions.

- `BEN-002` (this batch) adds pure, version-bound enrollment windows with exact calendar/date boundaries, explicit open/closed/conditional/unknown outcomes, and typed overlap rejection. Luna implementation and Sol refinement passed integration review; benefits tests and vet pass with 78.3% coverage. This is domain-support behavior, not a deployed enrollment workflow.

- Non-web domain work (this batch): `CRM-002` adds tenant-bound prospect attribution and consent outcomes, including safe invalid-consent handling; fixes membership error sentinels and nil-store reads. `READINESS-003` adds deterministic readiness outcomes with pinned requirement/resolution digests and bounded explanations. Luna implementations received Sol refinement and integration review; package tests and vet pass at 76.1% and 75.8% coverage respectively. Other first-wave changes remain under review or await runtime/storage integration and are not marked complete.

- Typed-CSS migration (committed as `a51c2eb9`) - every hand-written stylesheet is now built from GWC typed constructors: the productui 67-const platform sheet plus focus boundary, the customer-theme/glyph emitters (byte-identical), the workspace login/product shells, the journey renderer, the ssrshell shell sheet, and the tokens workspace/mode/responsive sheets. No string-CSS stylesheets remain outside tests and fixtures. Rendering is unchanged by construction (ordered old-vs-new sheet proofs; per-sheet cssdiff-clean) — the only intentional byte deltas are the serializer's canonical forms (trailing semicolons, sorted declarations, `@media ` spacing, content-hashed @keyframes names with verified animation-name linkage). Pins refreshed accordingly: tokens mode-contract SHA, ssrshell golden HTML + digest, workspace ux002 golden, ~90 test oracles re-anchored to canonical emission with raw-output proof. Two real defects were caught and fixed in flight: a focus-selector backslash corruption and hardcoded keyframe hashes in journey Raw shorthands (converted to Keyframes + Animation longhands; one irreducible dual-animation shorthand stays Raw, guarded by the new linkage tests). Known latent quirk preserved as-is: eight `@media(print)` blocks never match (unknown media feature) and stay dead via `@media (print)` rather than silently going live.
- `WEB-040` adds the global action launcher shell control: a "Start an action" trigger opening a locally-filtered dialog of authorized starts only (promotion start behind the Journeys create grant, worker selection behind People view), intersected with the authorized navigation so a denied or omitted page is never advertised; navigation-only hrefs, narrow props, deterministic derivation, dialog/trigger accessible names, en-US/de-DE/RTL ar catalog entries, and a token-driven stylesheet with forced-colors, reduced-motion, and print handling. Shell and navigation goldens re-pinned to the launcher-bearing tree.
- `WEB-044` adds meaningful breadcrumb resolution to the page header: the trail walks the canonical PageDefinitions ParentNav chain (Work > Work History, Admin > roles/worker-ids/org-visibility/appearance/studio), drops ancestors the identity cannot open through the same navigationDestinationAuthorized projection the search and scope links use, names the selected worker on a profile instead of repeating the generic page title, and stays silent for unknown pages and single generic crumbs. Rendered as a localized nav landmark with an ordered list, software-routed ancestor links, an aria-current page marker, and aria-hidden separators; fully typed GWC CSS (dark-mode sheet stays the final cascade layer). Two older document-wide aria-current assertions were re-scoped to the primary-nav landmark with explicit breadcrumb-marker assertions added. tools/uxqual Go suites 26/26 green; playwright browser gate 18/20 with the same 2 accessible-name failures reproducing on clean-HEAD fixtures (pre-existing, unrelated).
- `WEB-045` adds the canonical page-identity header: `ResolvePageIdentity` derives the stable page id, registry title/subtitle, scope label, settings destination (withheld when the projection withholds settings), and acting-self label in one governed resolution, and `PageIdentityHeader` renders the head with a `data-hcm-page` identity stamp above the breadcrumb trail. The old inline `pageHeader` is gone (single caller rewired). WEB-037 and WEB-039 goldens re-pinned after strip-proof that the only byte delta is the new attribute; one file rename (`page_identity.go` to `header_identity.go`) keeps the route-adapter markup rule green. Browser gate 18/20 with the same 2 pre-existing failures.
- `WEB-046` adds the contextual utility drawer to the topbar: `utilityDrawerSections` derives Related (registry parent plus authorized siblings) and Actions (worker-bound workflow launches behind the Journeys create grant) sections, `UtilityDrawer` renders the trigger plus a hidden-until-open dialog with a close control, and an empty context renders a fragment so no dead control ships. Fully typed GWC CSS, en-US/de-DE/RTL ar labels. No golden fallout (empty drawer is zero bytes on Home). Browser gate 18/20 with the same 2 pre-existing failures.
- `WEB-047` adds the responsive mobile shell collapse: the launcher and drawer trigger labels move into collapsible spans that hide at 430px and below (icon-only triggers, accessible names and touch targets intact), owned by a new typed `MobileShellStylesheet` layer using logical properties only. WEB-037/WEB-039/WEB-040/WEB-046 goldens re-pinned after strip-proofs that the label-span wrap is the entire byte delta. Browser gate 18/20 with the same 2 pre-existing failures.
- `WEB-048` proves shell keyboard and landmark navigation: `ResolveShellLandmarks` inventories the named landmarks (complementary, navigation set, main) from the same sources the renderers use, with per-page/role/locale coverage; the drawer dialog closes on Escape through one shared `drawerEscapeCloses` predicate matching the search/launcher contract; and a new `web048_keyboard_landmarks` playwright spec proves DOM-order tab sequence, landmark roles, and 390px reflow in real Chromium. Full browser gate 24 passed with the same 2 pre-existing ux003 failures.
- `WEB-049` adds the tenant-federation entry experience: an identity without a tenant reaches an entry gate (tenant-grouped issuer links with protocol/assurance metadata, honest empty state, tenant-scoped chrome suppressed) instead of tenant content. Entries arrive as server-composed data from the issuer registry — the frontend authors nothing. Fully typed CSS, en-US/de-DE/RTL ar labels, no golden fallout. Browser gate 24 passed with the same 2 pre-existing failures.
- `WEB-050` adds accessible sign-in recovery to the entry gate: a labelled recovery section of server-composed links with a fail-closed destination policy (workspace-relative, https, and mailto only — script/data/network-relative/plain-http dropped with no invented fallback). Typed CSS, en-US/de-DE/RTL ar titles, no golden fallout. Browser gate 24 passed with the same 2 pre-existing failures.
- `WEB-051` proves accessible-authentication conformance: a 3-state by 3-locale sweep over the entry gate (titled heading, resolving references, discernible safe links, DOM-order focus, aria-hidden hygiene, unique ids, keyed copy, status-role empty states) passes on current code with no fixes owed, pinned by matrix digest `6380d315…`. No production change, no fallout.
- `WEB-052` adds the session-expiry warning: a labelled banner between topbar and content rendering the server's detail text with a re-authentication link and a labelled dismiss control. Timing truth stays server-side via a nilable `View.SessionWarning` projection — no threshold logic in presentation. Typed CSS, en-US/de-DE/RTL ar copy, no fallout. Browser gate 24 passed with the same 2 pre-existing failures.
- `WEB-053` adds reauthentication work restoration: the warning's re-auth link carries the current address as a `resume` target composed from the shell's own current-address primitive, preserving existing destination query and passing unparsable/out-of-shell bases through untouched. No new state, no new copy, no fallout. Browser gate 24 passed with the same 2 pre-existing failures.
- `WEB-054` adds risk-bound step-up authentication: a prompt naming the exact sensitive action, the server's reason, and the challenge destination behind the shared shell-destination policy, mounted after the session warning. The prompt authorizes nothing; dismissal is page-load state. Typed CSS, en-US/de-DE/RTL ar copy, no fallout. Browser gate 24 passed with the same 2 pre-existing failures.
- `WEB-055` adds the delegation-context selector to the topbar: assumable delegated grants (delegator, expiry, elevation) each closing over its exact server-projected pair through the switcher's exchange contract, rendered only when a grant beyond the current context exists. Typed CSS, en-US/de-DE/RTL ar label, no fallout. Verified with the native Linux Go toolchain (Windows interop down); browser gate deferred until Chromium runs again.
- `WEB-056` adds the persistent acting-authority banner: whenever the server projects a valid current authority context, a labelled strip between the step-up prompt and the content grid names exactly that authority (tenant, acting context, delegator, expiry, elevation) through the switcher's own detail builder — own authority included, so persistence covers the authority acted under, not just delegations. No projection means no strip; no new authority source. Typed CSS, en-US/de-DE/ar title, no fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium). Shared workspace fixtures refreshed: the committed renders predated the tokens typed-CSS migration (non-canonical serialization), now re-anchored to canonical emission.
- `WEB-057` adds the break-glass activation experience: when the server projects an emergency activation, a prompt after the authority banner names the incident, reason, narrow capabilities, and bounded window, with an activation link through the shared destination policy and a labelled dismiss. The prompt authorizes nothing (grant-opening stays a server decision); no incident reference means no prompt, an unsafe destination means no link. Typed CSS, en-US/de-DE/ar copy, no fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-058` adds safe view-as policy simulation: when the server projects a simulation, a panel after the break-glass prompt headlines the simulation (no authority assumed) and names the simulated subject, outcome, redaction-safe reason, matched rules, policy versions, and evidence reference, with an exit back to the viewer's own view. Presentation evaluates nothing; no subject means no panel, an unsafe exit means no link, and the panel is deliberately not locally dismissible. Typed CSS, en-US/de-DE/ar copy, no fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-059` adds logout and revocation convergence: a non-nil server-projected signed-out state suppresses every authority surface (session warning, step-up, authority banner, break-glass, simulation, context switcher, delegation selector — stale projections cannot resurrect them) and makes the signed-out panel the content, naming the revoked grants with a single sign-in path. Typed CSS, en-US/de-DE/ar copy. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-063` enforces record-level page authorization: the person and self-service routes admit only disclosable records (otherwise the no-data unavailable recovery), project every profile fact through its field verdict, and keep the generic page label in the breadcrumb for unadmitted records. Silent-server renders are byte-identical to full-allow verdicts. Also fixed: `PageSizeControl` emitted hidden inputs in map order (nondeterministic markup). No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-060` enforces authentication telemetry privacy: the shared destination policy now fails closed on hrefs carrying credential-bearing query parameters (token, secrets, passwords — exact, case-insensitive key match), so authenticator material can never reach the logged URL surfaces through recovery, re-auth, step-up, break-glass, simulation-exit, or sign-in links. A 4-configuration by 3-locale sweep proves the auth documents expose no password inputs, no ungoverned hidden fields, no inline handlers, and no secret-named data attributes; tainted projections lose exactly the link, never the prompt. No fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-061` adds the authorized presentation projection: a pure, presentation-owned projector mapping server-composed record/field verdicts (allow, redacted, denied, withheld) to localized display — allowed values render, redacted reads as redacted, denied as unavailable with the server's reason, withheld and anything unknown as withheld; missing verdicts and non-disclosable subjects project to uniform withholding with no values. No trust import, no policy evaluation in presentation; the foundation WEB-062 builds on. No fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-062` enforces resource discoverability decisions: the three record-bearing search loops (people, promotion actions, work instances) admit only disclosable records once the server sends verdicts through the new `View.RecordVerdicts`, and advertise projected labels instead of raw values, via `DiscoveryAdmitted`/`DiscoveryLabel`. A silent server keeps the current discovery set byte-for-byte. Per-field description scrubbing stays with WEB-063's record pages. No fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-063` enforces record-level page authorization: the person and self-service routes admit only disclosable records (otherwise the no-data unavailable recovery), project every profile fact through its field verdict, and keep the generic page label in the breadcrumb for unadmitted records. A silent server renders byte-identically to full-allow verdicts. Also fixed: `PageSizeControl` emitted hidden inputs in map order (nondeterministic markup). No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-064` resolves current and proposed organization scope: each role-visibility editor now shows the resolved unit list behind its current policy and its draft through one pure resolver (allowlist admits listed, denylist admits unlisted, viewer-relative and unknown modes resolve to nothing; no invented scope), with localized copy and typed CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-065` enforces data-domain presentation scope: each role-visibility editor now resolves the admitted spec data domains (People, Position, Organization, Compensation) behind its policy through one pure resolver (`ResolveDataDomainScope`: granted names intersected with available, case-insensitive, blanks and unknown names resolve to nothing, sorted, de-duplicated, inputs never mutated) and renders admitted-domain chips or an honest empty state — non-admitted domains are withheld from the markup entirely. Display-only resolution; authorization stays server-side. Typed CSS (chips stack under 620 px), en-US/de-DE/ar copy. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-066` implements all field-disposition renderings: the shared person/myself profile adapter now honors the six spec dispositions through `ProjectField` — SHOW renders, MASK/SUMMARY_ONLY/DERIVED_ONLY render the server-composed stand-in (blank stand-ins withhold), REDACT reads as redacted, HIDE omits the row entirely with no text or reason, and unknown dispositions withhold. Blank dispositions keep the exact legacy effect rendering (WEB-063 golden and silent≡allow conformance untouched). No new copy, no new CSS — dispositions reuse existing fact rows. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-067` enforces population-query presentation limits: the workforce directory now derives rows, facet options, and counts from the admitted population (`admittedPeople`: undisclosable and verdict-less records leave the population once the server speaks, silent servers keep the current set, order preserved, inputs never mutated). Governed all-disclosable verdicts render byte-identically to a silent server. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-068` implements semantic action-availability states: capability cards and the visibility save row now render their semantic state (`ResolveActionAvailability`: granted is available, denied-but-safe is unavailable, denied-and-unsafe is hidden). Unavailable cards show the reason with no live link to the action; hidden capabilities leave the page; the grant-less save stays disabled with its reason and the Manage-roles recovery link. Blank states keep the legacy live rendering byte-identical. Typed GWC CSS untouched (existing muted/button styles), new en-US/de-DE/ar reason copy. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-069` enforces export and artifact visibility: the work queue, history extract, insights metrics, and home activity now draw from the admitted work population (`admittedWork`), so denied journey artifacts — rows, proposal facts, summaries, and every derived count — never render. Silence is population-scoped: person-record verdicts alone leave journeys ungated (dropping journeys for a verdict about a person would invent authority), which keeps WEB-063's pinned profile contract byte-identical. No dedicated export affordance exists in the UI; the admitted-population rule governs every extractable listing an export draws from. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-070` filters real-time events by current authority: the shell attention count now counts admitted open instances only (`OpenWorkItems(admittedWork(view))`), so a denied journey keeps no share of the notification count. Silent servers keep the current count, person-only verdict maps leave journeys ungated per the WEB-069 scoping rule, and page-visibility keeps its honest restricted state. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-071` invalidates presentation caches on authority drift: the roles permission row now reconciles its client draft against the incoming grant snapshot (`reconcileRolePermissionDraft`: local edits survive re-renders, version bumps, changed grants, or role/page switches reset to the server grants, so a stale draft can never save revoked grants back), and break-glass dismissal is keyed by incident reference (a new emergency always resurfaces). Single server renders are byte-identical to the legacy seeding. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-072` proves cross-surface authorization noninterference: silence is now population-scoped symmetrically (`peopleVerdictsPresent` mirrors `workVerdictsPresent`) — person verdicts gate the directory but leave journeys ungated, journey verdicts gate journeys but leave the directory ungated, and mixed maps gate each population independently. A work-addressed map previously emptied the directory, inventing authority over workers. Discovery search keeps its pinned map-global contract; page populations gate per-population. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-073` persists immutable PageDefinition revisions: render-free snapshots of registry definitions feed an append-only, digest-addressed revision log (`PageRevisionLog`) — identical re-records are idempotent, conflicting content at an existing version is refused, versions must be positive, latest tracks the maximum. Snapshots and retrievals deep-copy; canonical JSON bytes plus sha256 bind each version, and `MarshalRevision`/`ParseRevision` round-trip persisted bytes with tamper failing closed. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-074` implements the page draft lifecycle: mutable working copies bound to a base revision (`NewPageDraft`, blank `NewPageDraftFromScratch`), free edits that never touch the log, ordered `PublishDraft` rules (identical replays idempotent, conflicting content refused, stale bases refuse so lineage never forks silently, from-scratch refuses versioned pages, retargeted working copies refuse), and discard-by-dropping. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-075` validates floorplan compatibility: the spec floorplan catalog registers as a presentation contract (10 versioned floorplans, 11 semantic primitives), and `ValidateFloorplanCompatibility` verdicts one composition — known floorplan, supported version, registered primitives — accumulating stable reasons in fixed order. Drafts validate through their draft-scoped composition (`PageDraft.Composition` added; snapshot shape and WEB-073/074 digests untouched). No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-076` validates semantic-region composition: the page-anatomy regions register as a contract (7 composable regions in resolution order; shell platform-owned), and `ValidateRegionComposition` verdicts one composition — registered regions, non-decreasing anatomy order, exactly one primary once composition starts — with stable accumulated reasons. Drafts validate through `PageComposition.Regions` alongside the floorplan layer. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-077` validates typed widget bindings: a one-per-tier widget registry (governed/content/external) and `ValidateWidgetBinding` verdicts one binding — registered widget, supported version, known authority class (canonical/external/manager/agent), declared classification and source, display form for masked bindings, and no sensitive context on external embeds — with stable accumulated reasons. Classification ceilings pin as opaque metadata (no taxonomy invented; comparison awaits the classification step). Drafts validate through `PageComposition.Widgets`. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-078` validates semantic action bindings: `ValidateActionBinding` verdicts one binding — bare capability reference (endpoint-shaped values refused: the client never synthesizes endpoints), current action token, positive expected version, idempotency key, named input type — with stable accumulated reasons. No static capability allowlist: availability is runtime gateway authorization proved by the token, and allowlisting it would be a second authority source. Drafts validate through `PageComposition.Actions`. No new copy, no new CSS. No other fallout. Verified with the native Linux Go toolchain; browser gate deferred (no launchable Chromium).
- `WEB-079` enforces page classification ceilings: the publication ladder pins public < internal < confidential < restricted (registry labels plus the platform catalog's restricted tier; the Classification service taxonomy stays authoritative server-side), and `ValidateDraftCeiling` verdicts one draft against its declared `PageComposition.ClassificationCeiling` — bindings above the ceiling refused, undeclared ceilings and unclassified/unranked bindings failing closed — with stable accumulated reasons. The ceiling gates composed binding data only. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-080` resolves page configuration precedence: `ResolvePageConfiguration` merges scoped layers (company override, enterprise-group default, tenant default, platform default) into one composition — most-specific-valid-scope wins per field with the winning scope and policy version recorded, broader differing declarations recorded as overrides, and the classification ceiling accumulating instead (narrower scopes may tighten but never weaken a broader denial; refused weakenings recorded as denials). Unknown scopes, duplicate scopes, and unranked ceilings fail closed. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-081` migrates page definitions across widget versions: `MigrateCompositionWidgets` advances behind bindings to the registry's pinned versions with per-binding lineage (widget, from-version, to-version), leaving current bindings untouched and every non-version field identical. Unknown widgets and versions outside the registry fail closed with the registry's own unsupported-version wording; cross-widget replacement awaits pinned replacement behavior the registry does not declare yet. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-082` implements scoped page publication rollout: `PageRollout` binds one immutable revision (page, version, digest) to target organization-applicability scopes with effective dates (zero means live at publication). `ValidatePageRollout` verdicts the structure, `VerifyRolloutTarget` binds the target to an actually-published log revision (unknown or tampered targets refuse), and `RolloutLiveAt` answers scope liveness mechanically. Rollout advances forward only; durable rollout state stays with the studio platform. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-083` implements page rollback and retirement: `VerifyRollbackTarget` verifies a structurally valid rollback to a strictly older revision over live scopes only — cross-page, forward-or-equal, out-of-scope, unvalidated, and unpublished live or destination targets refuse — and `PageRetirement` carries a dated end-of-life with a mandatory recorded reason, verified against publication history, darkening serving through `RolloutServableAt` (foreign retirements never apply). No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-084` reports page dependency impact: `ReportDependencyImpact` inventories one contract change (registered widget or catalog floorplan, with zero meaning retirement) across page compositions, listing every page sorted with its hit status and stable reasons — behind versions hit, retirements hit any referencing version, identical reasons deduplicate. Unknown change kinds, blank contracts, and negative versions fail closed. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-085` implements the page-definition inventory: `BuildPageInventory` unions revision-log pages, composed pages, rollout pages, and retirement pages into one sorted inventory — latest pinned revision, live latest-version rollout scopes, active retirement, and impacted contracts per page — with superseded-version rollouts excluded and malformed contract changes failing the build closed. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-086` implements new-page purpose and audience setup: `PageComposition` carries purpose and audience from the opening authoring step, and `ValidatePagePurpose` verdicts the declaration — blank purpose or audience (whitespace included) refuses with stable reasons. Both stay free-text with no length rule, and audiences stay declared strings rather than a second authorization vocabulary. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-087` implements the governed floorplan chooser: `ChooseFloorplan` resolves one author pick against the registered catalog — unregistered picks refuse, unset versions pin the catalog current, explicit older versions pin as asked, future or negative versions refuse — reusing the compatibility verdict wording. Pinned choices compose cleanly through compatibility validation. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-088` implements the semantic outline editor: `ApplyOutlineEdits` adds and removes primitives and regions against the registered sets (platform-owned regions refuse with the validator's wording), applied atomically in edit order with the resulting outline. Convergent edits succeed silently for idempotent replay; moves stay out for the reorder step and ordering validates downstream. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-089` implements keyboard page-region reordering: `MoveOutlineRegion` moves one region one step earlier or later (one press, one step; walks loop the call), reusing the outline verdict. Directions stay axis-neutral for vertical, horizontal, and RTL layouts; boundary moves converge silently, absent regions and unknown directions refuse, and first occurrences travel on duplicates. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-090` implements the permitted widget catalog: `PermittedWidgetCatalog` lists every registered widget sorted with its status under one page ceiling — permitted exactly when the classification limit sits at or below the ceiling, with stable refusal reasons otherwise. Undeclared ceilings clear nothing and unranked labels refuse. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-091` implements the authorized binding inspector: `InspectBindings` reports per-binding findings in composition order — kind, stable reference, index, validity with verbatim validator reasons, and authorization-relevant notes in fixed order. Token presence is noted but token material never reaches the report; authorization itself stays server-side. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-092` implements the sanitized content editor: `EditBindingContent` sets one content-tier widget binding's replacement values — out-of-range targets, unregistered widgets, governed/external tiers, raw markup in either value, and display-less masked edits refuse with stable reasons. The markup sniff refuses tag-open grammar conservatively; deeper policy stays with the content-safety gates. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-094` implements the page-validation findings panel: `ValidateComposition` runs the whole draft chain — purpose, floorplan, regions, widgets, actions, ceiling — reporting every step finding in flow order with an overall publication gate. Every step always runs so failures never hide each other; reasons stay verbatim from the step validators. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-095` implements semantic page diff review: `DiffCompositions` reports changed scalars (ceiling moves annotated tightened/loosened only when both sides rank), added/removed/moved members in stable orders with occurrence-paired duplicates, and type/capability-paired binding changes — version arrows, input moves, token granted/withdrawn, differing details. Token values and idempotency keys rotate silently. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-096` implements governed page publication controls: `PublishGoverned` wraps the mechanical publish with the asset-pipeline gates in order — composition validation, preview plan validity with evidence equal to the plan's expansion for the draft page, named reviewer approval — then records. First failing gate reports; retries stay idempotent and mechanical conflicts still surface. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-097` implements the authorization-resolved Home floorplan: `ResolveHomeFloorplan` resolves the registered section order (attention, summaries, quick-actions, recent-work, announcements) per authorized projection — the attention slot gates on the work surface, the rest place with per-viewer contents resolved in their section todos. Gate pages are registered surfaces; legacy projections keep every slot. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-098` implements the unified attention list: `AttentionList` filters one admitted work stream to non-terminal items in admission order — the attention section's governed content. Terminal items belong to continuity and history; presentation assigns no priority without a stated rule. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-099` implements recent-work continuity: `RecentWork` filters one admitted work stream to terminal items in admission order — the recent-work slot's governed content, attention's mirror. The two partition the admitted stream exactly with no shared items; output never aliases input. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-100` implements personal-essential summaries: `SummarizePersonal` derives one `PersonalSummary` per person present in the admitted stream — active and completed counts in order of first appearance. Derived-only with no new truth; totals reconcile item-for-item with the attention and recent-work lists. No new copy (counts only), no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-101` implements configurable quick actions: `ResolveQuickActions` resolves viewer-chosen action IDs against the available launcher items in chosen order — repeats collapse to first mention, unknown IDs drop fail-closed. Items pass through untouched; the catalog owns the truth. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-102` implements governed announcement regions: `GovernAnnouncements` splits one announcement stream into polite and assertive regions in admission order — the first assertive claim wins, later assertive claims join the polite region. Region membership is the governance; items pass through untouched and the shell keeps its single-assertive policy. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-103` implements the My Work collection: `MyWorkItems` scopes one work stream to the viewer's collection — items whose PersonRef equals the viewer's PersonID in admission order. Empty viewer identities and empty refs match nothing fail-closed, so unassigned work never leaks into a collection. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-104` implements task and approval filtering: `FilterWorkCollection` types the four collection views (tasks, approvals, blocked, completed) with unknown filters matching nothing fail-closed. `ParseWorkCollectionFilter` resolves request strings and the provider's string matcher now delegates to the typed path, proven per request string. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-105` implements the Draft Center view: `DraftCenterItems` composes the UF-002 save-and-resume set — viewer-owned, open, not-awaiting-approval items in admission order — from the My Work collection and the typed collection views. Composition-only with no new truth. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-106` implements tracked-request summaries: `SummarizeTracked` projects one `TrackedSummary` per admitted work item in admission order — exactly the tracking fields (status, due) with openness derived from terminality. Derived-only: openness reconciles with the attention and recent-work lists and no dates are interpreted. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-107` implements completed-work history: `CompletedHistory` projects one `CompletedEntry` per terminal work item in admission order — identity plus completion evidence, with empty stamps staying empty rather than invented. Record-only: history covers precisely the recent-work list. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-108` implements responsive My Work list-detail behavior: `ResolveListDetailPanes` decides which collection panes a layout shows for a selection state — wide shows both, narrow shows detail on selection and list otherwise, unknown layouts fail closed to the list. Pixel-free: CSS keeps owning pixels while Go governs pane visibility. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-109` implements the authorized People directory: `SortPeopleDirectory` types the five directory sort fields and both directions with the documented name/ascending defaults. `ParsePeopleSort` resolves request strings and the directory sort path now delegates to the typed contract, proven per request pair with typed orders equal to legacy string orders. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-110` implements the bounded people-query builder: `BuildPeopleQuery` composes one bounded directory query from raw request parts — trimmed and lowered strings, the typed sort contract, floored pages, the allowlisted page size. Single-source: built sorts drive the governed directory order. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-111` prevents people-search enumeration leakage: `SearchPeopleDirectory` separates search from browse — blank queries match nothing instead of returning everyone admitted, while text queries reuse the directory's normalized index with team/location narrowing. Every hit contains its query and stays inside its population. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-112` implements the worker identity header: `ResolveWorkerIdentity` is the single governed constructor for worker header facts — name and role through the discovery projection, initials and photo passing through. Generalizes the exact rule the search surface already follows so denied names project identically everywhere. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-113` implements the worker overview section: `ResolveWorkerOverview` resolves the overview fact set (worker number, job code, level, hire date, employment/time types, record source/created) with the page adapter's exact behavior — silent passthrough, HIDE omission. Independent constructor as the section's governed content. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-114` implements the worker employment section: `ResolveWorkerEmployment` resolves the page's exact organization fact set (unit, manager, position, location, company, business unit, cost center, arrangement) over a new shared section engine — the overview constructor now delegates to it, proven unchanged by its own matrix. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-115` implements the worker time and leave section: `ResolveWorkerTimeLeave` resolves the spec'd section with zero facts over the shared section engine — the worker record carries no time/leave facts, so the constructor is the independent resolution point rather than invented balances. Title keys added to the en-US catalog (de falls back); verdicts conjure nothing. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-116` implements the worker pay and benefits section: `ResolveWorkerPay` resolves the page's exact compensation fact set (base pay, bonus target, pay zone, pay frequency) over the shared section engine, now locale-aware for money/percentage formatting — all sections proven unchanged by their matrices. Format-faithful byte for byte. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-117` implements the worker growth section: `ResolveWorkerGrowth` resolves the specified section with zero facts over the shared section engine — the worker record carries no growth facts and launchable workflows are view-coupled, so the constructor is the independent resolution point rather than invented ratings or goals. Title keys added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-118` implements the worker documents and activity sections: `ResolveWorkerDocuments` and `ResolveWorkerActivity` resolve both spec'd sections with zero facts over the shared section engine — no authorized records project document or activity facts today, so the constructors are the independent resolution points rather than invented entries or headers. Title keys added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-119` implements contextual worker action discovery: `DiscoverWorkerActions` resolves the actions launchable for one worker from a workflow registry and an optional query — launch hrefs bind against the worker ID, static hrefs pass through, and the trimmed case-folded query narrows over name, category, and description with the launcher's exact rule. The registry is never mutated. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-120` implements the responsive worker object page layout: `WorkerPageLayout` with `ResolveWorkerPageLayout` selecting wide or narrow from a viewport width and `ResolveWorkerPagePanes` deciding the panes per layout. The typed `WorkerPageNarrowMaxWidth` breakpoint (1040) mirrors the stylesheet's own MaxW(1040) collapse of .person-layout to one column. Unknown widths fail closed to narrow and unknown layouts to the detail stack; both known layouts show the detail stack and launcher rail with CSS owning the stacking. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-122` implements durable draft autosave presentation: `ResolveDraftAutosave` presents one page draft's autosave status against its durable save record — saved when a page-bound record holds the draft's digest, unsaved when the working copy moved on or no record exists, failed when the record carries an attempt error. Timestamps render in the locale time zone; foreign-page or pageless records never present as this draft's save. Copy keys added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-123` implements current-versus-proposed field presentation: `CompareFieldValue` presents one field's already-formatted current value against its proposed value — equal text is unchanged, anything else is changed with the catalog's change marker. Formatting stays with the values' owners; whitespace counts and two unreported values are unchanged. Marker copy added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-124` implements guided proposal collection: `ResolveProposalGuide` turns a proposal's supplied facts — worker, current/proposed values, effective date — into ordered steps with completion derived from the inputs. Incomplete steps name their requirement, the guide points at the first incomplete step (-1 when ready), and readiness means every fact present with submission staying server authority. Step copy added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-125` implements accessible validation summary resolution: `ResolveValidationSummary` resolves the summary model for one validation state and its registered link fields — errors only in state order, entry text through the component's own fail-closed copy helper so model and announcement never drift, links only to registered fields. A clean state resolves to an empty model; warnings never announce. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-126` implements simulation comparison presentation: `CompareSimulations` presents the delta between two simulated views without evaluating policy — localized outcomes with a change verdict, rule identifiers diffed into added, removed, and kept sets in projection order, and both version lines. Blanks drop and duplicates collapse mirroring the panel; the comparison carries the panel's no-authority notice. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-127` implements policy and obligation explanations: `ExplainObligation` explains one obligation state through reviewed copy with untokenized states failing closed, and `ExplainPolicyOutcome` explains one simulated policy outcome pairing with the panel's outcome under its no-authority notice. Explanations describe already-projected values and never evaluate policy. Explanation copy added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-128` implements proposal confirmation review: `ResolveProposalConfirmation` resolves the confirmation review for one proposal's facts by composing the values comparison, the collection guide, and the missing-fact summary. Ready means every fact present and the change shown; it never authorizes submission. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-129` implements approval progress presentation: `ResolveApprovalProgress` resolves approval progress for one publication request by reading the governed pipeline gates — composition validation, preview evidence, review approval — in pipeline order without running the mechanical publish. Blocked stages name their requirement; complete means every gate passes but never publishes. Stage copy added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-130` implements execution-status dimensions resolution: `ResolveStatusDimensions` resolves the dimensions model for one status projection — all five lifecycle dimensions through the surface's own token mapping, absent presences through the presence tokens, invalid enums failing closed, and the same joined accessible label. Unavailable projections resolve to the surface's unavailable copy with no items. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-131` implements reconciliation and repair presentation: `ResolveReconciliation` resolves the reconcile-and-repair posture from the resolved consistency and execution dimension keys — trouble and invalid keys to attention, repairing consistency to repair, consistent to healthy, everything else idle. Attention names the troubling dimensions; repair stays service responsibility. Posture copy added to the en-US catalog (de falls back). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-132` implements interruption and recovery continuity: `ResolveResumeHref` governs the re-authentication resume policy — relative workspace destinations take the current-address target with queries surviving and stale targets replaced, while absolute URLs, foreign hosts, non-workspace paths, and unparsable bases pass through untouched. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-133` implements the headcount-request page: the canonical registry gains the headcount page — identity, route, hiring-role visibility — with an honest fallback that exposes no requisitions until the governed requisition service (RECRUIT-001) publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the registry count pin moves 17 to 18. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-134` implements the position-request page: the canonical registry gains the position page — identity, route, hiring-role visibility — with an honest fallback that exposes no positions until the governed position service publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the registry count pin moves 18 to 19. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-135` implements the requisition workspace page: the canonical registry gains the requisition workspace — identity, route, hiring-role visibility — with an honest fallback that exposes no requisitions until the governed requisition service (RECRUIT-001) publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the registry count pin moves 19 to 20. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-136` implements the candidate pipeline page: the canonical registry gains the candidate pipeline — identity, route, hiring-role visibility — with an honest fallback that exposes no candidates until the governed candidacy service (RECRUIT-001) publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the registry count pin moves 20 to 21. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-137` implements the candidate object page: the canonical registry gains the candidate object page — identity, route, hiring-role visibility — with an honest fallback that exposes no candidate facts until the governed candidacy service (RECRUIT-001) publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 40 to 41 and the registry count pin moves 21 to 22. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-138` implements interview scheduling: the canonical registry gains the interview scheduling page — identity, route, hiring-role visibility — with an honest fallback that exposes no slots, interviewers, or confirmations until the governed scheduling service publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 41 to 42 and the registry count pin moves 22 to 23. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-139` implements structured candidate evaluation: the canonical registry gains the evaluation page — identity, route, hiring-role visibility — with an honest fallback that exposes no criteria, ratings, or recommendations until the governed evaluation service publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 42 to 43 and the registry count pin moves 23 to 24. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-140` implements offer review and acceptance: the canonical registry gains the offer page — identity, route, hiring-role visibility — with an honest fallback that exposes no offer terms and accepts nothing until the governed offer service publishes, with acceptance staying server authority and a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 43 to 44 and the registry count pin moves 24 to 25. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-141` implements the external candidate portal: the canonical registry gains the portal — identity, route, role-less visibility for candidates outside the workspace — with an honest fallback that exposes no postings, applications, or candidate data until the governed candidacy service publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 44 to 45 and the registry count pin moves 25 to 26. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-142` implements the onboarding-plan page: the canonical registry gains the onboarding page — identity, route, hiring-role visibility — with an honest fallback that exposes no tasks, owners, or due dates until the governed onboarding service publishes, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 45 to 46 and the registry count pin moves 26 to 27. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-143` implements onboarding task completion: the canonical registry gains the onboarding-tasks page — identity, route, hiring-role visibility — with an honest fallback that completes nothing and exposes no task state until the governed onboarding service publishes, completion staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 46 to 47 and the registry count pin moves 27 to 28. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-144` implements worker-activation readiness: the canonical registry gains the activation-readiness page — identity, route, hiring-role visibility — with an honest fallback that activates nothing and exposes no readiness signals until the governed activation service publishes, activation staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 47 to 48 and the registry count pin moves 28 to 29. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-145` implements the employee time hub: the canonical registry gains the time-hub page — identity, route, employee visibility — with an honest fallback that records nothing and exposes no balances, requests, or leave cases until the governed time service publishes, time truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 48 to 49 and the registry count pin moves 29 to 30. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-146` implements accessible time entry: the canonical registry gains the time-entry page — identity, route, employee visibility — with an honest fallback that records nothing and exposes no time records until the governed time service publishes, time truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 49 to 50, the insights entry 50 to 51, and the registry count pin moves 30 to 31. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-147` implements time correction: the canonical registry gains the time-correction page — identity, route, employee visibility — with an honest fallback that corrects nothing and exposes no time records until the governed time service publishes, time truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 50 to 51, the insights entry 51 to 52, and the registry count pin moves 31 to 32. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-148` implements manager time approval: the canonical registry gains the time-approval page — identity, route, manager visibility — with an honest fallback that approves nothing and exposes no approval state until the governed time service publishes, approval staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 51 to 52, the insights entry 52 to 53, and the registry count pin moves 32 to 33. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-149` implements the time-exception workbench: the canonical registry gains the time-exceptions page — identity, route, manager visibility — with an honest fallback that resolves nothing and exposes no exception state until the governed time service publishes, resolution staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 52 to 53, the insights entry 53 to 54, and the registry count pin moves 33 to 34. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-150` implements time-off balance and calendar: the canonical registry gains the time-off page — identity, route, employee visibility — with an honest fallback that books nothing and exposes no balances or calendar until the governed time service publishes, time truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 53 to 54, the insights entry 54 to 55, and the registry count pin moves 34 to 35. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-151` implements the time-off request journey: the canonical registry gains the time-off-request page — identity, route, employee visibility — with an honest fallback that requests nothing and exposes no request state until the governed time service publishes, request truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 54 to 55, the insights entry 55 to 56, and the registry count pin moves 35 to 36. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-152` implements team-coverage review: the canonical registry gains the team-coverage page — identity, route, manager visibility — with an honest fallback that shows nothing and exposes no coverage state until the governed time service publishes, coverage truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 55 to 56, the insights entry 56 to 57, and the registry count pin moves 36 to 37. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-153` implements protected-leave intake: the canonical registry gains the protected-leave page — identity, route, employee visibility — with an honest fallback that opens nothing and exposes no case state until the governed leave service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 56 to 57, the insights entry 57 to 58, and the registry count pin moves 37 to 38. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-154` implements restricted leave-evidence tasks: the canonical registry gains the leave-evidence page — identity, route, HR-partner-only visibility — with an honest fallback that completes nothing and exposes no evidence state until the governed leave service publishes, evidence truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 57 to 58, the insights entry 58 to 59, and the registry count pin moves 38 to 39. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-155` implements the leave-status timeline: the canonical registry gains the leave-timeline page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no case history until the governed leave service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 58 to 59, the insights entry 59 to 60, the admin entry 60 to 61, and the registry count pin moves 39 to 40. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-156` implements return-to-work planning: the canonical registry gains the return-to-work page — identity, route, employee visibility — with an honest fallback that plans nothing and exposes no return plans until the governed leave service publishes, plan truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back); the organization entry renumbers 59 to 60, the insights entry 60 to 61, the admin entry 61 to 62, and the registry count pin moves 40 to 41. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-157` implements the employee pay summary: the canonical registry gains the pay-summary page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no pay figures until the governed pay service publishes, pay truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 91) so the governed tail stops renumbering; the registry count pin moves 41 to 42. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-158` implements accessible pay statements: the canonical registry gains the pay-statements page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no statement data until the governed pay service publishes, statement truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 92); the registry count pin moves 42 to 43. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-159` implements pay-discrepancy intake: the canonical registry gains the pay-discrepancy page — identity, route, employee visibility — with an honest fallback that files nothing and exposes no case state until the governed pay service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 93); the registry count pin moves 43 to 44. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-160` implements manager compensation proposals: the canonical registry gains the comp-proposals page — identity, route, manager visibility — with an honest fallback that proposes nothing and exposes no proposal state until the governed compensation service publishes, proposal truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 94); the registry count pin moves 44 to 45. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-161` implements salary-range and budget comparison: the canonical registry gains the salary-comparison page — identity, route, manager visibility — with an honest fallback that compares nothing and exposes no band data until the governed compensation service publishes, band truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 95); the registry count pin moves 45 to 46. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-162` implements compensation-cycle populations: the canonical registry gains the cycle-populations page — identity, route, manager visibility — with an honest fallback that scopes nothing and exposes no population data until the governed compensation service publishes, population truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 96); the registry count pin moves 46 to 47. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-163` implements the compensation worksheet: the canonical registry gains the comp-worksheet page — identity, route, manager visibility — with an honest fallback that shows nothing and exposes no worksheet rows until the governed compensation service publishes, worksheet truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 97); the registry count pin moves 47 to 48. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-164` implements compensation calibration: the canonical registry gains the comp-calibration page — identity, route, manager visibility — with an honest fallback that calibrates nothing and exposes no calibration state until the governed compensation service publishes, calibration truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 98); the registry count pin moves 48 to 49. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-165` implements the benefit-program overview: the canonical registry gains the benefits-overview page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no program data until the governed benefits service publishes, program truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 99); the registry count pin moves 49 to 50. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-166` implements benefit-plan comparison: the canonical registry gains the benefits-compare page — identity, route, employee visibility — with an honest fallback that compares nothing and exposes no plan data until the governed benefits service publishes, plan truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 100); the registry count pin moves 50 to 51. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-167` implements benefit enrollment: the canonical registry gains the benefits-enroll page — identity, route, employee visibility — with an honest fallback that enrolls nothing and exposes no enrollment state until the governed benefits service publishes, enrollment truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 101); the registry count pin moves 51 to 52. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-168` implements payroll and benefit reconciliation status: the canonical registry gains the pay-benefit-recon page — identity, route, manager visibility — with an honest fallback that reports nothing and exposes no agreement state until the governed reconciliation service publishes, reconciliation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 102); the registry count pin moves 52 to 53. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-169` implements the employee Growth home: the canonical registry gains the growth-home page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no growth data until the governed growth service publishes, growth truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 103); the registry count pin moves 53 to 54. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-170` implements goal planning: the canonical registry gains the goal-planning page — identity, route, employee visibility — with an honest fallback that plans nothing and exposes no goal data until the governed growth service publishes, goal truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 104); the registry count pin moves 54 to 55. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-171` implements governed feedback: the canonical registry gains the governed-feedback page — identity, route, employee visibility — with an honest fallback that exchanges nothing and exposes no feedback data until the governed growth service publishes, feedback truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 105); the registry count pin moves 55 to 56. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-172` implements manager check-ins: the canonical registry gains the manager-checkins page — identity, route, manager visibility — with an honest fallback that runs nothing and exposes no check-in data until the governed growth service publishes, check-in truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 106); the registry count pin moves 56 to 57. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-173` implements the performance-review workspace: the canonical registry gains the perf-review page — identity, route, manager visibility — with an honest fallback that reviews nothing and exposes no review data until the governed growth service publishes, review truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 107); the registry count pin moves 57 to 58. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-174` enforces review-participant visibility: the canonical registry gains the review-participants page — identity, route, manager visibility — with an honest fallback that discloses nothing and exposes no participant data until the governed growth service publishes, participant truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 108); the registry count pin moves 58 to 59. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-175` implements the governed skills profile: the canonical registry gains the skills-profile page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no skill data until the governed growth service publishes, skill truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 109); the registry count pin moves 59 to 60. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-176` implements assigned learning: the canonical registry gains the assigned-learning page — identity, route, employee visibility — with an honest fallback that assigns nothing and exposes no assignment data until the governed learning service publishes, assignment truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 110); the registry count pin moves 60 to 61. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-177` implements career-opportunity discovery: the canonical registry gains the career-discovery page — identity, route, employee visibility — with an honest fallback that discovers nothing and exposes no opportunity data until the governed growth service publishes, opportunity truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 111); the registry count pin moves 61 to 62. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-178` implements the manager talent workbench: the canonical registry gains the talent-workbench page — identity, route, manager visibility — with an honest fallback that shows nothing and exposes no talent data until the governed growth service publishes, talent truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 112); the registry count pin moves 62 to 63. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-179` implements talent calibration: the canonical registry gains the talent-calibration page — identity, route, manager visibility — with an honest fallback that calibrates nothing and exposes no calibration data until the governed growth service publishes, calibration truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 113); the registry count pin moves 63 to 64. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-180` implements succession planning: the canonical registry gains the succession-planning page — identity, route, manager visibility — with an honest fallback that plans nothing and exposes no succession data until the governed growth service publishes, succession truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 114); the registry count pin moves 64 to 65. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-181` implements the organization explorer: the canonical registry gains the org-explorer page — identity, route, employee visibility — with an honest fallback that explores nothing and exposes no org data until the governed organization service publishes, org truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 115); the registry count pin moves 65 to 66. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-182` implements the accessible organization outline: the canonical registry gains the org-outline page — identity, route, employee visibility — with an honest fallback that outlines nothing and exposes no org structure until the governed organization service publishes, org truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 116); the registry count pin moves 66 to 67. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-183` implements effective-date organization navigation: the canonical registry gains the org-effective-date page — identity, route, employee visibility — with an honest fallback that navigates nothing and exposes no historical org data until the governed organization service publishes, org truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 117); the registry count pin moves 67 to 68. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-184` implements the position object page: the canonical registry gains the position-object page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no position data until the governed position service publishes, position truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 118); the registry count pin moves 68 to 69. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-185` implements position occupancy presentation: the canonical registry gains the position-occupancy page — identity, route, employee visibility — with an honest fallback that presents nothing and exposes no occupancy data until the governed position service publishes, occupancy truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 119); the registry count pin moves 69 to 70. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-186` implements the headcount-plan workspace: the canonical registry gains the headcount-plan page — identity, route, employee visibility — with an honest fallback that plans nothing and exposes no plan data until the governed headcount service publishes, plan truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 120); the registry count pin moves 70 to 71. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-187` implements workforce scenario authoring: the canonical registry gains the workforce-scenario page — identity, route, employee visibility — with an honest fallback that authors nothing and exposes no scenario data until the governed headcount service publishes, scenario truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 121); the registry count pin moves 71 to 72. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-188` implements governed population building: the canonical registry gains the governed-population page — identity, route, employee visibility — with an honest fallback that builds nothing and exposes no population data until the governed headcount service publishes, population truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 122); the registry count pin moves 72 to 73. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-189` implements cost and capacity simulation: the canonical registry gains the cost-capacity page — identity, route, employee visibility — with an honest fallback that simulates nothing and exposes no simulation data until the governed headcount service publishes, simulation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 123); the registry count pin moves 73 to 74. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-190` implements reorganization proposals: the canonical registry gains the reorg-proposals page — identity, route, employee visibility — with an honest fallback that proposes nothing and exposes no proposal data until the governed organization service publishes, proposal truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 124); the registry count pin moves 74 to 75. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-191` distinguishes planned state from committed truth: the canonical registry gains the planned-vs-committed page — identity, route, employee visibility — with an honest fallback that distinguishes nothing and exposes no plan or commitment data until the governed organization service publishes, plan and commitment truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 125); the registry count pin moves 75 to 76. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-192` implements responsive organization exploration: the canonical registry gains the org-responsive page — identity, route, employee visibility — with an honest fallback that explores nothing and exposes no org data until the governed organization service publishes, org truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 126); the registry count pin moves 76 to 77. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-193` implements the employee Help hub: the canonical registry gains the help-hub page — identity, route, employee visibility — with an honest fallback that helps with nothing and exposes no help data until the governed help service publishes, help truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 127); the registry count pin moves 77 to 78. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-194` implements authorized knowledge search: the canonical registry gains the knowledge-search page — identity, route, employee visibility — with an honest fallback that searches nothing and exposes no knowledge data until the governed help service publishes, knowledge truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 128); the registry count pin moves 78 to 79. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-195` implements HR service-request intake: the canonical registry gains the hr-service-request page — identity, route, employee visibility — with an honest fallback that intakes nothing and exposes no request data until the governed help service publishes, request truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 129); the registry count pin moves 79 to 80. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-196` implements confidential case intake: the canonical registry gains the confidential-case page — identity, route, employee visibility — with an honest fallback that intakes nothing and exposes no case data until the governed help service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 130); the registry count pin moves 80 to 81. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-197` implements safe participant case status: the canonical registry gains the case-status page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no case data until the governed help service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 131); the registry count pin moves 81 to 82. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-198` implements restricted case messaging: the canonical registry gains the case-messaging page — identity, route, employee visibility — with an honest fallback that messages nothing and exposes no message data until the governed help service publishes, message truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 132); the registry count pin moves 82 to 83. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-199` implements the specialist Case Center: the canonical registry gains the case-center page — identity, route, specialist visibility — with an honest fallback that centers nothing and exposes no case data until the governed help service publishes, case truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 133); the registry count pin moves 83 to 84. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-200` implements case assignment and recusal: the canonical registry gains the case-assignment page — identity, route, specialist visibility — with an honest fallback that assigns nothing and exposes no assignment data until the governed help service publishes, assignment truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 134); the registry count pin moves 84 to 85. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-201` implements restricted case evidence review: the canonical registry gains the case-evidence page — identity, route, specialist visibility — with an honest fallback that reviews nothing and exposes no evidence data until the governed help service publishes, evidence truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 135); the registry count pin moves 85 to 86. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-202` implements case finding and disposition: the canonical registry gains the case-disposition page — identity, route, specialist visibility — with an honest fallback that finds nothing and exposes no finding data until the governed help service publishes, finding truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 136); the registry count pin moves 86 to 87. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-203` implements case appeal: the canonical registry gains the case-appeal page — identity, route, employee visibility — with an honest fallback that appeals nothing and exposes no appeal data until the governed help service publishes, appeal truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 137); the registry count pin moves 87 to 88. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-204` proves case-view redaction and audit: the canonical registry gains the case-redaction page — identity, route, specialist visibility — with an honest fallback that proves nothing and exposes no redaction data until the governed help service publishes, redaction truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 138); the registry count pin moves 88 to 89. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-205` implements exit initiation: the canonical registry gains the exit-initiation page — identity, route, employee visibility — with an honest fallback that initiates nothing and exposes no exit data until the governed lifecycle service publishes, exit truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 139); the registry count pin moves 89 to 90. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-206` implements exit reason and effective-date collection: the canonical registry gains the exit-details page — identity, route, employee visibility — with an honest fallback that collects nothing and exposes no exit data until the governed lifecycle service publishes, exit truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 140); the registry count pin moves 90 to 91. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-207` implements offboarding impact simulation: the canonical registry gains the offboarding-impact page — identity, route, employee visibility — with an honest fallback that simulates nothing and exposes no impact data until the governed lifecycle service publishes, impact truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 141); the registry count pin moves 91 to 92. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-208` implements exit review and approval: the canonical registry gains the exit-review page — identity, route, manager visibility — with an honest fallback that reviews nothing and exposes no review data until the governed lifecycle service publishes, review truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 142); the registry count pin moves 92 to 93. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-209` implements the offboarding plan: the canonical registry gains the offboarding-plan page — identity, route, manager visibility — with an honest fallback that plans nothing and exposes no plan data until the governed lifecycle service publishes, plan truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 143); the registry count pin moves 93 to 94. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-210` implements manager and work reassignment review: the canonical registry gains the reassignment-review page — identity, route, manager visibility — with an honest fallback that reviews nothing and exposes no reassignment data until the governed lifecycle service publishes, reassignment truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 144); the registry count pin moves 94 to 95. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-211` implements final-pay and benefit status: the canonical registry gains the final-pay page — identity, route, employee visibility — with an honest fallback that shows nothing and exposes no pay data until the governed lifecycle service publishes, pay truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 145); the registry count pin moves 95 to 96. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-212` implements access and equipment reconciliation: the canonical registry gains the access-equipment page — identity, route, manager visibility — with an honest fallback that reconciles nothing and exposes no reconciliation data until the governed lifecycle service publishes, reconciliation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 146); the registry count pin moves 96 to 97. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-213` implements final-document delivery: the canonical registry gains the final-documents page — identity, route, employee visibility — with an honest fallback that delivers nothing and exposes no document data until the governed lifecycle service publishes, document truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 147); the registry count pin moves 97 to 98. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-214` implements offboarding external-effect status: the canonical registry gains the offboarding-effects page — identity, route, manager visibility — with an honest fallback that shows nothing and exposes no effect data until the governed lifecycle service publishes, effect truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 148); the registry count pin moves 98 to 99. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-215` implements retained-obligation presentation: the canonical registry gains the retained-obligations page — identity, route, employee visibility — with an honest fallback that presents nothing and exposes no obligation data until the governed lifecycle service publishes, obligation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 149); the registry count pin moves 99 to 100. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-216` implements exit completion and correction: the canonical registry gains the exit-completion page — identity, route, manager visibility — with an honest fallback that completes nothing and exposes no completion data until the governed lifecycle service publishes, completion truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 150); the registry count pin moves 100 to 101. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-217` implements the report catalog: the canonical registry gains the report-catalog page — identity, route, manager visibility — with an honest fallback that catalogs nothing and exposes no report data until the governed reporting service publishes, report truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 151); the registry count pin moves 101 to 102. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-218` distinguishes certified and customer reports: the canonical registry gains the report-types page — identity, route, manager visibility — with an honest fallback that distinguishes nothing and exposes no report data until the governed reporting service publishes, report truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 152); the registry count pin moves 102 to 103. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-219` implements the analysis floorplan: the canonical registry gains the analysis-floorplan page — identity, route, manager visibility — with an honest fallback that lays out nothing and exposes no analysis data until the governed reporting service publishes, analysis truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 153); the registry count pin moves 103 to 104. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-220` enforces authorized analysis filters: the canonical registry gains the analysis-filters page — identity, route, manager visibility — with an honest fallback that filters nothing and exposes no filter data until the governed reporting service publishes, filter truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 154); the registry count pin moves 104 to 105. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-221` implements result lineage presentation: the canonical registry gains the result-lineage page — identity, route, manager visibility — with an honest fallback that presents nothing and exposes no lineage data until the governed reporting service publishes, lineage truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 155); the registry count pin moves 105 to 106. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-222` implements data-freshness presentation: the canonical registry gains the data-freshness page — identity, route, manager visibility — with an honest fallback that presents nothing and exposes no freshness data until the governed reporting service publishes, freshness truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 156); the registry count pin moves 106 to 107. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-223` implements aggregate suppression states: the canonical registry gains the aggregate-suppression page — identity, route, manager visibility — with an honest fallback that suppresses nothing and exposes no suppression data until the governed reporting service publishes, suppression truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 157); the registry count pin moves 107 to 108. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-224` implements governed report export: the canonical registry gains the report-export page — identity, route, manager visibility — with an honest fallback that exports nothing and exposes no export data until the governed reporting service publishes, export truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 158); the registry count pin moves 108 to 109. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-225` implements authorized report sharing: the canonical registry gains the report-sharing page — identity, route, manager visibility — with an honest fallback that shares nothing and exposes no sharing data until the governed reporting service publishes, sharing truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 159); the registry count pin moves 109 to 110. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-226` implements safe natural-language analysis: the canonical registry gains the nl-analysis page — identity, route, manager visibility — with an honest fallback that answers nothing and exposes no answer data until the governed reporting service publishes, answer truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 160); the registry count pin moves 110 to 111. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-227` implements analysis-to-proposal handoff: the canonical registry gains the analysis-handoff page — identity, route, manager visibility — with an honest fallback that hands off nothing and exposes no handoff data until the governed reporting service publishes, handoff truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 161); the registry count pin moves 111 to 112. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-228` implements accessible data visualization: the canonical registry gains the accessible-viz page — identity, route, manager visibility — with an honest fallback that visualizes nothing and exposes no visualization data until the governed reporting service publishes, visualization truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 162); the registry count pin moves 112 to 113. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-229` implements the authorization-resolved Admin home: the existing admin page now binds each capability card to its target page and suppresses cards for pages the admitted identity may not open — the platform admin keeps all six cards, a manager keeps only the Journey service card, and employee or role-less viewers are offered no admin routes — while the unrestricted component preview keeps its full card set and transport authorization remains the enforcement boundary. No registry count change. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-230` implements the Policy Studio: the canonical registry gains the policy-studio page — identity, admin route, platform-admin visibility — with an honest fallback that authors nothing and exposes no policy data until the governed policy service publishes, policy truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 163); the registry count pin moves 113 to 114. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-231` implements authorization-policy simulation: the canonical registry gains the policy-simulation page — identity, admin route, platform-admin visibility — with an honest fallback that simulates nothing and exposes no simulation data until the governed policy service publishes, simulation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 164); the registry count pin moves 114 to 115. The new admin child extends the pinned Admin submenu (7 to 8) and the roles-page utility drawer (parent plus 6 siblings). No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-232` implements the configuration center: the canonical registry gains the configuration-center page — identity, admin route, platform-admin visibility — with an honest fallback that centers nothing and exposes no configuration data until the governed configuration service publishes, configuration truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 165); the registry count pin moves 115 to 116. The new admin child extends the pinned Admin submenu (8 to 9) and the roles-page utility drawer (parent plus 7 siblings) with its golden digest re-pinned. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-233` implements integration operations: the canonical registry gains the integration-operations page — identity, admin route, platform-admin visibility — with an honest fallback that operates nothing and exposes no operation data until the governed integration service publishes, operation truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 166); the registry count pin moves 116 to 117. The new admin child extends the pinned Admin submenu (9 to 10) and the roles-page utility drawer (parent plus 8 siblings) with its golden digest re-pinned. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-234` implements the reconciliation and repair workbench: the canonical registry gains the reconciliation-workbench page — identity, admin route, platform-admin visibility — with an honest fallback that repairs nothing and exposes no repair data until the governed repair service publishes, repair truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 167); the registry count pin moves 117 to 118. The new admin child extends the pinned Admin submenu (10 to 11) and the roles-page utility drawer (parent plus 9 siblings) with its golden digest re-pinned. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-235` instruments privacy-safe frontend telemetry: the canonical registry gains the privacy-telemetry page — identity, admin route, platform-admin visibility — with an honest fallback that instruments nothing and exposes no telemetry data until the governed telemetry service publishes, telemetry truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 168); the registry count pin moves 118 to 119. The new admin child extends the pinned Admin submenu (11 to 12) and the roles-page utility drawer (parent plus 10 siblings) with its golden digest re-pinned. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-236` enforces frontend performance budgets: the canonical registry gains the performance-budgets page — identity, admin route, platform-admin visibility — with an honest fallback that enforces nothing and exposes no budget data until the governed performance service publishes, budget truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 169); the registry count pin moves 119 to 120. The new admin child extends the pinned Admin submenu (12 to 13) and the roles-page utility drawer (parent plus 11 siblings) with its golden digest re-pinned. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-237` qualifies the production browser matrix: the canonical registry gains the browser-matrix page — identity, admin route, platform-admin visibility — with an honest fallback that qualifies nothing and exposes no matrix data until the governed qualification service publishes, matrix truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 170); the registry count pin moves 120 to 121. The new admin child extends the pinned Admin submenu (13 to 14) and the roles-page utility drawer (parent plus 12 siblings) with its golden digest re-pinned. The extended matrix adds security (no admin route leaks to unauthorized viewers), integration (Admin submenu ends with the matrix), and fault (load error plus unknown locale stay honest) coverage. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-238` qualifies assistive-technology compatibility: the canonical registry gains the assistive-tech page — identity, admin route, platform-admin visibility — with an honest fallback that qualifies nothing and exposes no compatibility data until the governed qualification service publishes, compatibility truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 171); the registry count pin moves 121 to 122. The new admin child extends the pinned Admin submenu (14 to 15) and the roles-page utility drawer (parent plus 13 siblings) with its golden digest re-pinned. The extended matrix adds security, integration (submenu membership, order-decoupled), and fault coverage. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-239` proves frontend disaster recovery: the canonical registry gains the disaster-recovery page — identity, admin route, platform-admin visibility — with an honest fallback that proves nothing and exposes no recovery data until the governed recovery service publishes, recovery truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 172); the registry count pin moves 122 to 123. The new admin child extends the pinned Admin submenu (15 to 16) and the roles-page utility drawer (parent plus 14 siblings) with its golden digest re-pinned. The extended matrix adds security, integration, and fault coverage. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-240` gates the production frontend release: the canonical registry gains the release-gate page — identity, admin route, platform-admin visibility — with an honest fallback that gates nothing and exposes no release data until the governed release service publishes, release truth staying server authority, plus a recovery link home. Page and fallback copy added to the en-US catalog (de/ar fall back). The page lands in the post-settings extension range (order 173); the registry count pin moves 123 to 124. The new admin child extends the pinned Admin submenu (16 to 17) and the roles-page utility drawer (parent plus 15 siblings) with its golden digest re-pinned. The extended matrix adds security, integration, and fault coverage. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- `WEB-093` implements multidimensional page preview: `PreviewPlan` binds page, fixture, and registered dimension axes (locale from the product catalog, concrete color presets, author-chosen positive viewport widths), and `ExpandPreviewPlan` expands the cartesian case matrix with stable IDs — first axis slowest, declared case order kept. Invalid plans refuse and never expand. No new copy, no new CSS. No other fallout. Verified with the Windows Go toolchain via WSL interop; browser gate deferred (no launchable Chromium).
- Agent delivery loop (committed as `b0603fe1`) - `AGENTS.md` step 6 now requires responsive visual inspection (desktop plus 390 px and 320 px, light and dark where supported, no overlap, truncation, or console diagnostics with the widths and states recorded) and, where the surface renders text or controls, the applicable `tools/uxqual/` i18n (en-US, de-DE, RTL ar) and accessibility/WCAG checks with the suites recorded. Unit (step 4) and end-to-end (step 5) coverage already existed, so no duplicate step was added. Step 7 now also requires the `CHANGELOG.md` and current devlog updates as part of the loop, not just under git discipline.

- `WEB-039` makes primary navigation a bounded authorization-resolved projection: supplied answers remain authoritative across favorites, fuzzy search, locale and shell shortcuts; malformed or empty answers fail closed; canonical destination metadata comes from the trusted registry; and destination RPC/page authorization remains an independent enforcement boundary. Native, actual WASM, i18n, accessibility, security, performance and responsive visual gates cover the component.
- `WEB-038` adds the bounded tenant and acting-context switcher to the stable GoWebComponents shell: exact server-resolved authority pairs, explicit delegation/elevation presentation, generation-fenced exchange, adapter-private projection staging and transactional scoped-state replacement. Component props contain no page-wide view or authority, and native, actual WASM, accessibility, i18n, theme, performance and responsive visual gates cover the boundary.
- `WEB-037` makes the GoWebComponents application shell a stable hydrated boundary: server-rendered chrome survives startup, HistoryRouter replaces only the outlet, component-local state and DOM identity persist, stale route loaders cancel, and focus/title/live-region behavior remains accessible without page reloads.
- `WEB-036` adds bounded sequence-based invalidation reconnect and authoritative catch-up: private non-authoritative local checkpoints, exact gap handling, commit-after-refetch semantics, progress-reset consecutive-failure budgets, exponential backoff, renewed authorization scope, deterministic redacted observability, and actual Go/WASM browser coverage. It does not invent a live invalidation endpoint or move server cursor-signing authority into the client.
- `WEB-035` adds the bounded filtered browser invalidation client: canonical hint decoding, tenant/projection/subject authorization, authoritative refetch-before-cursor-commit, retry-safe bounded queues, identifier-free observability and an actual same-origin Go/WASM WebSocket adapter. It deliberately leaves live endpoint composition and reconnect catch-up to the next governed transport contract rather than inventing another API.
- Completed-but-unticked todos - 36 open todos whose primary and matrix tests already existed and passed (AGENT-001, ALIGN-003, ANALYTICS-001, ATTEND-001, CONF-002, CONF-008, DISCLOSURE-001, DOC-EVIDENCE-001, DOC-INTAKE-001, GOV-030, GOVERN-003, KNOW-002, LEGAL-012, LEGAL-TOOL-001..009/012/013, PACK-002, POP-010, READINESS-CONF-001, ROLLOUT-007, SECARCH-006/007/008/011/016/023, SUPPLY-003, WF-RUN-005) are ticked with evidence naming the tests, the commit that wrote them and the passing package run; 960 of 1675 todos are now complete. `sbom.ModGraph` requires a go.mod at its root so a temp directory under the checkout no longer resolves to the repository module.
- `WEB-034` qualifies the Go/WASM browser RPC boundary against the generated Journey service: exact method and message types, bounded metadata and payloads, server-issued bearer injection, deadline/cancellation propagation, typed status preservation and leak-free streams. The adapter is exercised through the real GoGRPCBridge WebSocket tunnel and embedded PostgreSQL, and the rebuilt production bundle was verified against authorized live data in the Codex browser.
- Backend performance wave (`PERFOPT-001`..`006`) - Measured, behaviour-preserving optimizations on the backend hot paths: the pool's per-acquire session hygiene is one round trip instead of three plus one per runtime parameter (acquire and release 140 us to 40 us, a `QueryRow` 170 us to 92 us on the embedded server); each HTTP edge request authenticates once instead of twice (end-to-end 50 us to 35 us); `dbport` gains a batch primitive that the ledger multi-stream append, checkpoint store, intentcontrol and outbox writes use (statements per three-stream, ten-event append 51 to 33, heads locked in one ordered query, one shared statement builder for both append paths); a catalog-driven foreign-key audit added 65 indexes that store queries filter or join on (migration 00262) and records the 22 left unindexed by decision; per-call regular-expression compiles are hoisted or cached behind an AST check with explicit `regexhoist:dynamic` exemptions; allocations fell by a third on the envelope, ledger evidence and cycle explanation paths with goldens proving byte-identical output against the pre-change code.
- `WEB-031` closes the browser-state boundary around a random per-tab history ledger only, with hostile-storage fault coverage, real GWC history traversal, and no browser-persisted credentials, authority, business records, or configuration truth.
- `WEB-032` generates and embeds a bounded frontend asset-integrity manifest, authenticates and integrity-verifies the shim and WASM loader path, pre-indexes request metadata, validates the exact routable catalogue at startup, and publishes deterministic gzip representations. Interrupted multi-file publication fails closed during startup validation; bundle-wide filesystem atomicity is not claimed on Windows.
- `WEB-033` centralizes production CSP for every HTML response, removes origin-wide script and network authority, scopes asset and gRPC-tunnel connections by path, blocks inline event/style attributes, converts configurable table widths to a closed CSS-class contract, and hardens redirects, refusals, hosts, hashes and cached responses. Trusted Types remains explicitly blocked by GWC v5's private `template.innerHTML` fast paths rather than being enabled in a way that breaks mounting.
- Frontend qualification now treats the 17-page by 3-locale i18n/accessibility suite as a mandatory acceptance gate alongside interaction-latency, native, compiled `js/wasm`, security, integration, fault, and Codex-browser checks.

## 2026-09-06

- Artifact root (this batch) - `.artifacts/` is the single ignored home for binaries, lane output, coverage, Go temp and test binaries, lane build caches and the embedded PostgreSQL cache; the pre-commit hook and `scripts/run-lane.sh` route `GOTMPDIR`, `TMP`, `TEMP` and `HCMNEXT_TEST_PG_CACHE` there; `scripts/build.sh` builds into `.artifacts/bin`.
- Gate-exposed repairs (this batch) - The first whole-module run of the coverage gate surfaced 42 packages that fail alone; the planning corpus (registry parser accepting only one evidence date, three malformed front-end todos, stale registry, goldens and layout declarations), the data-plane regressions (provenance republish, blank signal keys, custody derivation, job architecture digests, deferred previews, worker-number uniqueness) and the policy-tool pins are repaired and committed; what remains red belongs to the front-end session (seven ticks without evidence, firewall imports, the workspace registry, migration 00238's DELETE grant).
- Quality gates (this batch) - Pre-commit now enforces format, lint, unit tests and a 70% statement-coverage floor: `tools/quality/covergate` gates every package with staged Go files (and the whole module in CI) against `definitions/toolchain/coverage-gate.yaml`; `AGENTS.md`, a thin `CLAUDE.md`, `.claude/settings.json`, the gate-runner subagent and the vendored Karpathy guidelines skill are tracked.
- `c26f730`, `725ef15`, `e885a59`, `6d81163`, `c2fdf94` (Gate A backend closure) - The last backend Gate A todos landed and are ticked (904): typed ProposeIntoManagement, workflow inspection, long-running operation and health endpoints wired through both edges with a durable operation store (00261) and a fail-closed cursor key; scheduler timer and signal roles; worker messaging delivery role; incumbent capability inventory. Review fixes and architecture-rule repairs (ceremony consumers, cross-plane allowlists, worker capability seam, mobility registry) included.
- `d121105` plus the interaction-gate follow-up - Kept product navigation on a persistent shell, scoped loading and refresh states to unresolved regions, reused baseline route projections, bounded journey workforce previews, pre-indexed People sorting and filtering, made reusable table rendering scale predictably, and shipped compressed cacheable WASM assets. CI now enforces named p95 budgets for loading feedback, every registered leaf page, the persistent shell, a 10,000-worker People interaction, a 1,000-by-12 table and the journey workforce preview.
- `d365555`, `6de21e2`, `cc9ecac` (front-end session) - Role access control, page permissions and organization visibility: migrations 00182/00238/00239 with their stores, journey RPCs, the roles and organization visibility pages, history navigation and login personas.
- `a7d621a`, `3180651`, `66318d1`, `85f7f5f`, `92ea605`, `c000a21` (security test wave) - Every authentication, authorization and security file now has a package test that exercises its exported surface and deny branches (25 lanes, 110 packages, test-referenced symbols 51% -> 67%); twenty-two production defects the new tests caught are fixed (trailing-JSON acceptance in OIDC and workload identity, foreign-tenant edges through trust/authz, unenforced attestor authority, root-as-leaf issuance, caller-supplied step-up and missing tenant binding in garnishment, free-text validation override in pay methods, forged authorization decisions in product-query invalidation, id-keyed connector operations without tenant checks, and others). The authorization batch closed with ticks 871 -> 895; connector operation queue, journal and credential lease tables (00235-00237) gained their PostgreSQL store; storage-disposition, storeboundaries, vulnimpact, layer-graph, P1A manifest and authority-gate corpora refreshed.
- `ab184e2` - Added the company metadata stores and migrations 00070-00131 (asset, benefits, career, CBA, commercial, contact, content registry, CRM, custom objects, employee relations, equity, FX, HR case, identity privacy, incentive, merit, mobility, pay input, pay method, performance, planning, safety, scheduling, skill, subscription, succession, survey, tax profile, trust; attestation responses; reference dataset release and adoption), each with a tenant-isolated store package, disposition row and tests; goose statement markers on plpgsql blocks; regenerated model manifests and the re-signed P1A manifest.
- `64ef19a` - Added the metadata behaviour behind the stores: location normalization and correction, job architecture assignment and publication, custom-object events, capabilities and search, commercial entitlement snapshots, benefit plan years, HR cases, the flat promotion commit command, the canonical byte stream engine and the bounded expression compiler.
- `08cd05c` - Landed session rotation and revocation fan-out (AUTHN-004/009), the attestation lifecycle (ATTEST-004..008), access lifecycle and drift reconciliation (ACCESS-003/004, ARTIFACT-005), entitlement across channels (CROSS-CONF-002), the reference dataset lifecycle (REFDATA-001, CONFIG-003) and the promotion entitlement and sensitive-access gates.
- `3bcf4b7` - Workflow and execution follow-ups: continuation target attempts, the configuration promotion step, platform config promotion and the end-to-end tests that exercise them.
- `ea9067a` - Product workspace, UI component and UX qualification client updates from the front-end session, with their code-style, ESLint and package configuration.
- `14442de` - Gate A wave tables and stores (migrations 00132 onward), the PostgreSQL outbox queue schema and claim protocol (EVENT-001), artifact byte storage, regenerated model manifests and disposition rows.
- `aca251d` - Workflow, integration and DataOps mappings on the shared transformation engine (XFORM-008), commercial pilot evidence (COMM-002/003), experience preferences.
- `a13bb8b` - Privacy processing inventory (PRIV-001), governance decision inputs, snapshots and composition (GOVERN-001/002), retention simulation (RECORDS-DISP-001), masked datasets (MASK-001).
- `5af163d` - Reliability and telemetry policy (OPS-001..005), recovery matrix and immutable backups (RECOVERY-001/002), control-plane distribution and feature evaluation (CP-004..006), IaC selection and provisioning contracts (IAC-003/004/005/013), connector health and idempotency governance (INTG-017/018, MSG-006, IDEMP-001), scheduler and worker roles (SVC-005/010, WF-RUN-005), bounded transports and the connector sandbox (CONN-RT-002/006), design-partner wedge records (WEDGE-001..009), workload envelopes.
- `04aba0e` - Endpoint header, deadline and budget policy with the transport-parity harness (ENDPOINT-006..008), gRPC deadline classification (RPC-EDGE-001), safe versus exact exports (EXPORT-001), AWS SDK qualification (LIB-018), application and command wiring.
- UI and docs commits (this batch) - front-end session workspace updates; devlog section 6, the changelog, planning and definitions updates, and the gitignore block for build and test artifacts that lanes leave in the checkout.
- Docs commit (this entry) - Ticks 657 -> 759 (registry 1666); twelve backfilled todos for code that had none; four stale checkboxes flipped; every evidence line now carries its go test command in the checked form; storage disposition through 00131; capability-coverage and intent-coverage goldens; table governance policy tools (ALIGN-008..015); coverage inventories.

## 2026-09-05

- `829e3a9` - Added the durable stores and migrations 00040-00069 the promotion closure needs (reconciliation jobs, session store, position/budget reservations, attestation, payroll run, pay-GL, job architecture, location, tenant placement, balance accumulators, access identity, legal evidence, outbox lease fence, consumer positions, record copy links, replay snapshots, connector operation journals, intent outcome references); `pgtest` now sweeps orphaned embedded-PostgreSQL runtime directories.
- `89d8c1b` - Landed the transaction plane: plan preparation against stream heads (TX-003), the commit coordinator with idempotent replay and crash-at-every-boundary proofs (TX-004), evidence resolution (TX-005), immutable corrections (TX-007), cancel-before/after commit under an advisory lock (TX-008), the crash-boundary conformance suite (TX-009).
- `5b64e58` - Made the compiled promotion execute plan terminate accurately: revisited nodes activate at attempt N+1 and continuations carry the attempt, approval completion and resume address the open attempt, WAIT timers are activation-qualified in identity and key, OBSERVE retry exhaustion routes to the repair terminal, `end_blocked` completes; SHADOW mode, worker-death recovery, RepairPlan execution mode, telemetry per advancement.
- `ad58d6a` - Bound workflow terminals back onto the Intent as one OutcomeReceipt carrying commit-receipt and repair references (INTENT-007), closure under the completion policy (INTENT-008), atomic approval election, pre-execution revalidation, rendered-digest binding and separation of duties with idempotent replay (APPROVAL-003/005/006/008), journey stages for the execute plan.
- `1352e3d` - Added governed connector operations (journals before dispatch, lease-time revalidation, per-resource causal order, exactly-once dispatch, UNKNOWN after timeout, isolated redrive), reconciliation completion (RECON-002) and RepairPlan revalidation (REPAIR-002).
- `d3c2a53` - Added the bounded people, organization and compensation writes with append-only evidence inside the Promotion local ACID commit (PEOPLE-004, ORG-003, COMP-004, PROMO-005) and the transaction-invariant suite (MODEL-025), plus domain packages for the persisted stores and engine updates.
- `b524715` - Wired serve for the execute plan: root redirect, health listener, timer dataset flags, scheduler as an initial process role, `-workflow-plan=execute`, browser Origin/Host/CSRF policy, journey stage wire values, serve-graph golden.
- `760eb1b` - Product workspace UI, journey client labels and actions for the execute-plan stages, UX qualification tooling; code-style allow-list for the vendored wasm shim, the design demo and the race-policy script.
- `0a56524` - Promotion end-to-end suite (nine scenarios, PROMO-009, WF-RUN-016 repair execution), acceptance matrix 24/24 PROVEN, bootstrap and otelmw tests aligned, architecture golden, gate-evidence signing, policy and planning tools.
- Docs commit (this entry) - Todos 588 -> 657, storage disposition through 00069, P1A manifest re-signed over 62 migration files with recorded gaps, telemetry allow-list rows, process/dependency roles, coverage inventories, workflow catalogue, user stories, security research, devlog `planning/devlog/2026-09-05-promotion-termination-wave.md`.

## 2026-09-03

- `aef1496` - Moved `internal/kernel/canonical` and `internal/kernel/digest` to `internal/engines/wire/canonical|digest`; updated all import sites, docs, and architecture firewall roles; added `ledger.NewAppenderWithClock` for deterministic clock pinning.
- `a56a251` - Hardened data plane (aggregates, ledger/hashchain/lineage, artifacts, bitemporal, projection/critical mapper, outbox, provenance, tenancy, health); added `dbport`/`pgxadapter` abstraction and `pgtest` isolation/lock helpers plus seed conformance fixtures.
- `9fedc43` - Added legal obligation model, pack definitions/releases, and extract pipeline (matrix, research, states); ported CA/NY rulepacks and seeded 50-state `definitions/legal` packs.
- `ce27a9a` - Rewired intent cell/pgstore/workspace, added `transaction/idempotency` (TX006) and full workflow runtime lanes (frontier, inspect, runtime, version, wait-step) plus `connectivity/observe` fixes.
- `56c1c93` - Added transport cell/OTel middleware, admin gRPC + `hcmctl`, humanwork workitem/workspace handler, dev token minting, and CLI/projector/worker wiring.
- `a862456` - Landed architecture qualifications, operation definitions, capability coverage/P1A manifests, generated admin/wire contracts, migrations, and planning/quality toolchains.
- `7e9d649` - Synced docs and harnesses (README, state research + us-federal, admin proto, serve/workspace conformance harnesses, todos, layout docs).
- `b42414a` - Repaired vet and test lanes: added workflow_continuation/advancement_receipt migrations, fixed stale replay head check, regenerated P1A evidence/golden, and scaffolded missing package unit tests.
- `3692405` - Backfilled per-file unit tests to 100% file coverage (577 files, 25k+ lines): dedicated \*\_test.go for every Go source lacking a direct counterpart across internal/, tools/, cmd/, and gen/.
- `b3c277e` - Implemented DB-013: materialized 16 governance/AuthZ/legal/evidence tables (00021), updated storage disposition, added Go governance store with digest/interval validation (cross-tenant delegation and unversioned authority rejected), and landed TestTodo_DB_013 (7 subtests).

## 2026-05-16

- `51fe847` - Aligned the AI chat Playwright fixture with the seeded demo
  employee (`Jane Doe` / `emp_123`) so quick-action prompt assertions match the
  console's default subject context.
- `9e24c2f` - Added Playwright regression coverage for an AI-generated
  termination page using `name`-keyed fields and `$selectedSubject` summaries.
  Updated `WorkflowPageRenderer` to treat field `name` as a stable fallback id,
  and hid the assistant FAB while the panel is open while preserving focus
  restoration on close.
- `c62d4a2` - Restored the workflow action-bar `systemError` import used by
  generated-page submit error mapping after the fetch-boundary refactor.
- `e55f62d` - Fixed the code-style checker so export declarations without a
  module specifier do not crash the AST walk. Added a console HTTP boundary
  helper and routed AI chat/start-workflow fetch calls through it so the
  external-call rule passes without weakening the architectural check.
- `9f99732` - Applied Prettier formatting to the AI assistant backend, console
  runtime, tests, and documentation changes after enabling format enforcement
  in `test:all`.
- `c06dcc5` - Documented the implemented AI assistant UI generation flow in
  `docs/ai-ui-generation.md`, linked it from `docs/plan.md`, and replaced the
  stale TODO inventory with completed four-workstream status plus follow-up
  backlog.
- `b0d640c` - Added GitHub Actions for Node and Go test jobs on push, pull
  request, and manual dispatch. Added pre-commit enforcement for lint-staged,
  code-style checks, Go checks, and `test:all`; added TypeScript and Go style
  checkers; moved shared runtime dependency types out of the API layer; replaced
  high-signal workflow magic literals with typed constants; and gofmt-formatted
  the touched Go block code.
- `4c7bc8a` - Added the console AI assistant surface and generated-page runtime:
  floating `AiChatPanel`, quick-action chips, `useChat`, `useStartWorkflow`,
  assistant-view rendering and route cleanup in `ConsoleShell`, generated page
  form state, subject picker widget, action-bar workflow submit wiring, branded
  panel styles, and unit/Playwright coverage for the assistant and renderer
  paths.
- `fbd5240` - Added the AI chat and workflow UI generation backend:
  `POST /api/ai/chat`, chat tool dispatch for workflow/employee/task actions,
  direct `POST /api/ai/generate-ui`, `AiClient.generatePageDefinition`,
  canonical widget type constraints, OpenAI/null-client implementations,
  workflow config lookup and hashing, UI generation error factories, and API,
  provider, registry, and E2E tests.
- `2f1a7ac` - Added runtime package dependencies for the AI assistant:
  `dotenv` for API environment loading plus `@tanstack/react-query`,
  `react-markdown`, and `remark-gfm` for the console assistant experience.
- `2d00188` - Enhanced the console app shell with a demo auth session (login screen,
  workspace card, session storage using `fromThrowable` at the UI boundary). Expanded
  `WorkflowPageRenderer` with richer interaction handling, extended `hcm-controls` with
  new field types, updated field registry and widget index tests, and expanded Playwright
  component gallery and UX smoke test coverage.
- `22afae4` - Expanded the brand token system with gap, padding, sizing, shadow, opacity,
  scrollbar, and chart accent tokens. Extended `style-lab-vars` to expose the new tokens
  as CSS variable mappings, added matching `StyleLabRail` sliders for live editing, and
  updated `styles.css` to consume the new variables.
- `816cd8c` - Added vitest test suites for the termination workflow: `ai-review-graph.test.ts`
  covers auto-advance through `ai_review` nodes, `completedNodes` outcome reporting,
  fallback via `outcomes[0]`, and stop-boundary behavior; `termination-workflow-config.test.ts`
  validates config loading, schema validation, AI review node wiring, preflight block
  reference, and start actor requirement.
- `f39a608` - Wired `ai_review` node auto-advance and async AI execution into the workflow
  runtime service. `aiReviewAutomaticRouteKeys` maps every `ai_review` node to `'completed'`
  before graph advance; `executeAiReviewNodes` fires async AI calls for traversed nodes and
  appends `AiChangeReviewGenerated` or `AiChangeReviewFailed` ledger events. AI failures
  never block workflow progression (`failurePolicy: 'continue'`).
- `bfb4269` - Added the employee termination workflow config (`employee.termination`):
  HR-initiated input collection, Go compliance preflight block, `ai_review` node for risk
  assessment, HR director approval, plan-transaction block, projection write, payroll and
  benefits external writes, ledger event recording, and full timeline summaries. Registered
  in the filesystem workflow config registry.
- `5712fb5` - Added Go termination preflight and plan-transaction blocks.
  `system.employee_data.termination.preflight` validates employment status, termination
  type, effective date, and business reason; computes tenure, risk level, statutory notice
  days, COBRA window, and warnings. `system.employee_data.termination.plan_transaction`
  produces an HRIS internal write, `fake_payroll/processFinalPay` and
  `fake_benefits/triggerCobra` external calls, and an `employment.status` projection patch.
  Registered in both executor entry points.
- `92aa225` - Added `ai_review` to the workflow graph node type union and validation
  allowlist. Exported `WorkflowAiReviewNodeConfig` (changeType, visibleFields,
  currentStateTemplate, proposedStateTemplate, failurePolicy).
- `15ed808` - Added foundation constants for the employee termination workflow:
  `TerminationSubmitted`, `TerminationPreflighted`, `TerminationExecuted` ledger event
  types; five termination action permissions; and the `employee.termination` workflow intent.

## 2026-05-15

- `011fdf9` - Marked atomic runtime and field components complete in the TODOS tracker.
- `7afe6cc` - Expanded the console control library with canonical field types, new widget
  categories (ui-widgets, graph-widgets, workflow-widgets), updated field registry and
  shared config, broad style refinements, and added Playwright E2E setup with component
  gallery visual and console UX smoke tests.
- `6e9d830` - Added structured logging throughout the workflow runtime and admin services,
  covering intent start/create, transitions, idempotent replay, external writes, and all
  admin lifecycle operations (import, validate, publish, deprecate, repair actions).
- `2ad6b31` - Added request-scoped logging to the Node API layer: per-request logger
  injected via DI, executor client re-emits Go block logs into the unified stream, and
  HTTP request/completion events logged at appropriate levels.
- `d909276` - Added structured slog logging to the Go executor service with per-request
  child loggers carrying block, requestId, correlationId, workflowInstanceId, and actorId.
- `9bcf5a1` - Added structured logging infrastructure to the platform foundation: extended
  LogContext with a service field and added JSON-to-stderr fallback at async, sync, and
  database transaction exception boundaries.
- `1412922` - Ignored Playwright test artifacts, output directories, and image files.
- `e398414` - Added the dynamic workflow UI console, UI contracts/runtime
  packages, brand token rendering, configurable control and widget libraries,
  style lab, broad HR widget catalog, route menu organization, and frontend UI
  planning docs.
- `8017b40` - Added the workflow admin runtime foundation, including JSON
  workflow draft/publish APIs, validation and preview services, simulation,
  debugger and repair helpers, workflow admin persistence, graph runtime
  support, E2E coverage, and admin/demo documentation.
- `fdf115a` - Extracted shared Go block helpers for strict decoding, date
  validation, risk/fact construction, transaction field validation, and string
  normalization across deterministic blocks.
- `2a8b2e7` - Hardened `.gitignore` coverage for local build artifacts, runtime
  scratch data, editor state, and secret material.
- `ddc16da` - Updated the generic runtime TODOs and workflow schema docs after
  moving the active workflow path to generic runtime execution.
- `71191dd` - Moved workflow E2E coverage to the public API routes and added a
  generic-runtime contract test proving a new workflow intent can run without a
  workflow-specific TypeScript service edit.
- `a2220ba` - Replaced workflow-specific TypeScript services with the generic
  runtime, config-driven workflow schemas, external-write client dispatch, and
  reusable approval, transaction, permission, timeline, and record handling.
- `68968e3` - Added the generic Go block output contract, customer-facing SDK
  aliases, block contract docs, and generic route/transaction fields across
  existing deterministic blocks.
- `2c63cef` - Added the generic workflow runtime TODO plan that captures the
  remaining work to remove workflow-specific TypeScript branches and move
  business behavior into config and Go blocks.
- `0d88740` - Added the position headcount requisition approval workflow,
  HarborCare fixtures, E2E coverage, API demo docs, approval-gate schema docs,
  and completed approval workflow TODOs.
- `434c493` - Added the approval-gate data model and evaluator, including
  `approval_groups`, approval task metadata, migration coverage, permissions,
  ledger events, and unit tests for sync/async gate rules.
- `821bc57` - Extracted reusable workflow runtime helpers for access context,
  JSON field parsing, projection patches, timeline views, ledger events,
  response shaping, and transition attempts.
- `8d45464` - Added workflow registry admin tooling for listing, validating,
  importing, publishing, and resolving checked-in workflow configs.
- `cc313ac` - Updated the changelog after the org-transfer available-action
  fixes.
- `ca3e00f` - Allowed scoped org-transfer approvers to retrieve available
  approval actions without requiring broad workflow visibility.
- `196f8e9` - Fixed the org-transfer E2E available-action helper used by the
  expanded approval-chain test.
- `0df392c` - Updated the changelog after the final org-transfer test assertion
  fix.
- `312c71d` - Tightened the org-transfer E2E approval-task assertion to coerce
  the task identifier through the same string path used by workflow transitions.
- `8ceb7dd` - Updated the changelog after expanding org-transfer E2E coverage.
- `97d8e12` - Expanded org-transfer E2E coverage for the HR-started workflow,
  approval routing, execution, idempotent replay, projection changes, and
  filtered timeline checks.
- `e29c0b0` - Updated the changelog after the org-transfer metadata alignment
  commit.
- `0b37642` - Aligned the org-transfer demo fixture date and approval metadata
  with the workflow configuration used by the org-transfer runtime.
- `b72f7fa` - Started the root changelog and documented the initial logical
  implementation commits.
- `503fcc2` - Updated workflow planning documentation, including the workflow
  schema spec, org-aware RBAC plan, API demo notes, project layout notes, and
  detailed TODO workstreams.
- `ff81534` - Added workflow execution improvements, compensation and org-transfer
  workflow configs, Go execution blocks, simulated third-party compensation APIs,
  employee/RBAC E2E coverage, and V0 legal-name guardrail tests.
- `923b887` - Added the org-aware RBAC data foundation: org units,
  relationships, worker assignments, role bindings, expanded demo seed data, and
  reusable employee access filtering helpers.
