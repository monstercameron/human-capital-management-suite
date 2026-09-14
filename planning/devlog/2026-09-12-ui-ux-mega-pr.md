# 2026-09-12 UI and UX mega PR

This topic branch is closing the live UI/UX audit, promotion-experience and
interaction-polish backlog in tested groups. It deliberately keeps one pull
request while preserving reviewable commits by concern.

## Published information architecture checkpoint

The registry now distinguishes a registered route from a published navigation
destination. This keeps direct unavailable-state routes for Experience Studio
and eleven other Admin fallbacks, while the menu, global search and utility
drawer show only working, role-authorized destinations. A forged navigation
projection cannot put an unpublished page back into the shell. Existing
fallback route tests were adjusted to prove the direct route and its absence
from live navigation rather than assuming the former 17-child Admin menu.

Home's major headings now describe attention, activity and starting a request
instead of presenting promotion as the whole HCM product. The figures remain
honestly scoped to promotion journeys and visible employees. The first live
Codex-browser pass exposed a misaligned scope sentence in the activity card;
the shared summary component and its CSS were corrected, then the
running Go/WASM app was rebuilt, restarted and retested. The final desktop
dark view aligns the heading, scope and facts. English, German and Arabic RTL
views render the new labels without fallback, and Admin search, global search,
the submenu and direct Studio unavailable state were exercised. Other Home
body copy still falls back to English; UXAUDIT-006 remains open for it. This
browser-control API did not provide exact 390px/320px viewport control, so no
new mobile screenshot is claimed at this checkpoint.

The named UXAUDIT-005 primary, golden, browser-facing render, security and
conformance tests pass. `go test -count=1 -cover ./internal/humanwork/productui`
passes at 92.4%, `go test -count=1 ./internal/humanwork/workspace ./test/workspace`
passes, and `npm test` passes. A stale workspace regression expected a legacy
unreserved `POS-HRBP-301` value; PROMOUX-004 intentionally removed that
fabricated selection, so the test now asserts no target position is prefilled
while preserving its exact money canonicalization check. This correction is
test-only and does not change the production promotion behavior.

## Main synchronization after UXAUDIT-005

After committing the published-navigation work as `4e7f77d2`, this topic branch
merged the latest local `main` by the repository-required no-rebase path. The
merge brings in PROMOUX-006's exact compensation guardrail, preserves both
changelog streams, and regenerates the architecture golden against the combined
package graph rather than retaining either conflicting digest. Focused
promotion, workforce, product UI and architecture tests pass in the combined
tree. The guardrail is still not mounted on the live promotion page, as the
incoming changelog explicitly notes; this synchronization does not claim that
visual integration is complete.

## Trace-pivot hardening

The promotion execution review exposed a telemetry contract mismatch: workflow
instrumentation emitted `instance_id` and `attempt`, while the shared filtered
provider admits the topology vocabulary `logical_operation_id` and `attempt_id`.
The provider therefore removed the very attributes an operator needs to move
between a trace and the workflow inspector. Instrumentation now uses the canonical
keys, and the allowlist admits the bounded `node_id` and `terminal_code` span
attributes. Focused execution and telemetry contract tests pass.

## Promotion live-test findings

The Jane promotion was advanced through both approvals and its durable
effective-date wait by using the fenced local-development clock. The live page
reached Recorded and correctly describes a recorded promotion outcome; it does
not claim the employee record was mutated because the production composition does
not yet include that writer.

Manual testing then found that Dominic, the recorded manager reviewer, could see
Jane in the journey list but could not reopen the completed journey. The case
relationship was effective from assignment time while the governed historical
read evaluates at journey creation time. The relationship now covers the case
from its creation but remains known and recorded only from the durable WorkItem,
and access is still restricted to its actual completed reviewer. The focused
authorization tests and a live sign-in as Dominic now both pass.

The execution composition also now distinguishes the finance and manager
reviewers and propagates one application clock and telemetry provider through
the execution driver and scheduler. The local-development clock is accepted
only on the loopback development profile. A clean browser rebuild under the
ordinary clock confirmed that Dominic can reopen Jane's Recorded journey and
that identifiers and technical diagnostics are absent from his response.

While running the promotion integration package, the serializable-start case
expired its 20-second context during embedded PostgreSQL startup before the
operation began. The fixture now creates that context after database bootstrap;
the operation keeps the same bounded deadline and the isolated integration
case passes in 15 seconds.

## Still partial

PROMOUX-014 is not complete: the bounded local-development clock works, but the
waiting page still needs a typed durable wait projection with timezone, scheduled
action, remaining checks, notification behavior and authorized intervention.
OBS-024 also remains weaker than its recorded acceptance claim because the
default execution evidence sink is process-local. Neither item is being reticked
or represented as complete in this checkpoint.

## Product component checkpoint

The shared Go component layer has been grouped separately from the browser
runtime and backend. It includes the current page refinements, common tables,
navigation and popover structures, loading states, visual tokens, localization
and accessibility contracts. `go test -count=1 ./internal/humanwork/productui`
passes. Browser navigation, transport invalidation and generated WASM remain in
the next checkpoint so a component-only review does not conceal runtime defects.

## Browser-runtime checkpoint

The browser group connects those components to the production Go/WASM client:
sequenced invalidation, localized loading regions, persistent shell navigation,
route-aware focus and scroll behavior, sign-in recovery, and the five-stage
promotion presentation. It also carries the latency, hydration and WCAG evidence
helpers that qualify those interactions. The generated WASM was rebuilt from
this source before the Dominic historical-access retest.

## Main synchronization checkpoint

The topic branch merged the latest local `main` after the four concern commits
were clean. This repository prohibits rebasing, so periodic synchronization uses
ordinary merge commits at verified checkpoints. The merge brought in
`PROMOUX-001`, `PROMOUX-002`, `PROMOUX-003`, `UXAUDIT-001`, and `UXAUDIT-007`.

Integration review preserved the database-backed active-promotion guard while
removing a contradictory worker-wide scan for the two callers that already hold
an effective-date reservation. The approval path now acts as the exact routed
reviewer: demo deployments may bind two real people, while other compositions
derive distinct finance and manager authority-class identities from one base.
The product UI, workspace, intent, execution, promotion guard, approval,
generated-registry, architecture, and UX quality suites pass at this checkpoint.

## Authorized action-launcher checkpoint

The global launcher now has a server-projected semantic action inventory instead
of treating every page link as an action. Promotion is admitted only when the
viewer has both the People read surface and the journey-create capability; the
Go/WASM client rejects missing, malformed, unknown and revoked projections rather
than reconstructing authority locally. Ordinary authorized destinations remain
available through the same fuzzy-ranked component, and the trigger changes to
`Go to` when there are no executable actions.

The shared popover now closes when focus genuinely leaves, on an outside pointer,
or on Escape. Escape returns focus to the trigger, while ordinary focus movement
does not steal it back. The launcher uses the software router for both actions and
destinations. Its phone presentation escapes the header scrollport, keeps a
bounded viewport-height result list, and cooperates with the compact expanding
global search at 390px and 320px.

Manual Codex-browser testing covered Rafael's authorized action path, fuzzy
`promte` matching, keyboard dismissal and focus restoration, Samuel's denied
action projection, desktop dark and light themes, limited motion, German,
Arabic/RTL, 390x844 and 320x720. The live run found two defects before closure:
the first narrow layout clipped the popover inside the top bar, and non-English
destination descriptions fell back to English. The popover is now fixed to the
phone viewport and the ordinary page catalog installs localized descriptions for
all supported locales. No Playwright run is claimed; the user requested the Codex
browser, and the accessibility tree plus screenshots were used directly.

Focused Go tests pass across `internal/humanwork/productui` (92.4% coverage),
`internal/humanwork/workspace`, `tools/uxqual/productclient`,
`tools/uxqual/journeyclient` and `tools/uxqual/cmd/journeywasm`. Go vet, gofmt,
diff hygiene and js/wasm compilation pass. `npm test` also passes its 27 root test
files / 159 tests and all workspace suites. UXAUDIT-003 is complete; the broader
promotion completion gate UXAUDIT-002 and the remaining audit backlog are still
open. The first full commit hook also caught the checked-in architecture fixture
at the pre-change package graph. It was regenerated with the gate-prescribed
`archdoc` command and reviewed: the delta records one additional package and two
within-module edges, plus the corresponding graph and document digests.

## Authorized organization hierarchy checkpoint

The organization surfaces no longer infer reporting lines by comparing display
names or treating every missing manager as a root. The journey contract carries
a closed authorized relationship projection for root, visible, orphan and
withheld states. A visible relationship includes only the stable worker endpoint
admitted to the same response. The server resolves overloaded legacy references,
fails ambiguous endpoints and cycles closed, and removes both the relationship
reference and endpoint when policy admits only the existence of an outside
manager. Explicit field-level denials also prevent manager names from leaking
through relationship summaries.

Organization, explorer, outline, responsive and Myself views now use the same
typed ownership node. The native disclosure hierarchy supplies keyboard-correct
expand and collapse behavior without claiming an incomplete ARIA tree pattern;
logical indentation and connectors adapt to RTL, narrow widths and forced-color
rendering. Flat and tree presentations expose the same authorized person facts,
relationship explanation and report count. Search operates on the admitted
population and adds matching ancestor context, so a filter cannot turn an
ordinary report into an apparent root. Selection, locale, collapsed navigation
and query state survive software navigation, while a revoked selection is
cleared. The outline route is tree-only and canonicalizes contradictory
`org_view=flat` state back to `tree` without losing the rest of the address.

Manual Codex-browser review covered Rafael's 64-person administrative projection
and Samuel's seven-person employee projection. Samuel sees the withheld-parent
explanation at the visible root and receives a profile link only for himself.
The hierarchy was exercised with native keyboard disclosure controls on desktop,
at 390x844 and 320x720, in dark and light themes, and with German plus Arabic RTL
copy. The narrow layouts kept the hierarchy within the content viewport, the
Arabic route used translated titles and controls, the outline URL canonicalized
while retaining person/search/navigation state, and the browser error and warning
logs were empty. The final adversarial pass found no concrete release blocker.

Named UXAUDIT-004 primary, integration, browser, accessibility, property and
regression tests pass. The complete affected Go package set passes, product UI
coverage remains 92.4%, protobuf and model regeneration is idempotent, and the
product client, journey client and journey WASM js/wasm test binaries compile.
The root npm test matrix, formatting, lint, code style, Go vet, decomposition,
drift, API, substrate, engine and build gates pass. A native Windows race run was
attempted but cannot execute on this host because CGO is disabled and GCC is not
installed; the repository's race-coverage policy gate passes. Browser evidence
uses the Codex accessibility and screenshot surfaces as requested, not
Playwright. UXAUDIT-004 is complete; later flat-organization density and polish
work remains tracked independently by UXAUDIT-020.

## Main synchronization after UXAUDIT-004

The clean branch merged `main` with PROMOUX-004's reserved position candidate
and PROMOUX-005's real reporting-line cycle safety. Both changelog streams were
preserved, and the generated architecture golden was recomputed from the merged
package graph rather than choosing either stale hash. Product UI, promotion,
position, organization, intent and architecture focused tests passed.

The merge hook found one genuine composition regression: the sandbox's author
was also trying to decide its own promotion as a caller unrelated to the routed
WorkItem owner. The sandbox proof now issues two verified tenant-scoped
credentials: the author proposes and executes, then the exact configured
approver decides. Its PRIMARY and RECOVERY tests pass with the production
separation-of-duties rule intact. This is a merge integration fix, not a
relaxation of approval authorization.

## UXAUDIT-006 copy and localization in progress

The product-shell pass replaces raw service and access vocabulary in ordinary
Admin, Insights, Settings, Studio, People and Person states with task-oriented
copy. The profile no longer displays the raw record-source marker or storage
version by default; the underlying authorized record and evidence projection
remain intact. Fact-status labels and the directory/profile/workflow-launcher
copy now resolve through the shared en-US, de-DE and Arabic catalogs. A live
Codex-browser check in dark Arabic RTL confirmed the People-to-Adrian-profile
path, the localized promotion launcher, the localized history heading, and a
directory summary that no longer exposes the raw `compensation_review` scope.

The same browser pass exposed a material remaining gap: opening the promotion
form from Adrian's Arabic profile still renders predominantly English copy and
implementation language in the separate journey renderer and client projector.
The follow-up wired the resolved product locale into the embedded journey client
without refetching the workforce, then moved the focused promotion heading,
subject facts, form fields, actions, help and empty request view into shared
catalog keys for en-US, de-DE and Arabic. The shared journey engine unavailable
callout also hides a raw composition notice and presents localized recovery
guidance. The old test that demanded internal compensation-policy references in
ordinary pay guidance was corrected to forbid them while still asserting the
exact percentage range when the server supplies one.

The rebuilt Go/WASM app was manually reopened in the Codex browser at Adrian's
Arabic RTL promotion page. Its heading, profile link, subject facts, required
field labels, help and submit action now appear in Arabic. Selecting the only
published next role revealed a second defect: the seeded demo career path has
no minimum or maximum base-pay increase in its authoritative workforce answer,
so the former templated sentence displayed an empty range. The client now gives
an honest localized unavailable-range explanation instead of inventing numbers;
the live recheck showed that explanation after role selection. The ordinary
profile title now prefers the server's human-readable job title over the raw
job code. The shared history-navigation accessible labels found in the same
Arabic pass now also have direct German and Arabic translations. A fresh
post-rebuild browser check confirmed the Arabic labels and the server-provided
`Senior Account Executive` title. The form was scrolled to its submit area in
dark RTL; labels, field help, action and layout remained readable.

Switching the live form to German exposed a transient-menu defect: the language
menu remained over the content after software navigation. The shared popover
controller now dismisses activated links after their click, leaving unrelated
pointer/focus grace behavior intact. A policy unit test covers link versus
non-link events. Another process took the default API ports during the final
build, so validation moved to an isolated local-dev cell on 18080/18443 and
gateway on 18768 without stopping or replacing the other process. A new
Codex-browser sign-in as Rafael, People → Adrian profile → Start Promotion,
then Arabic-to-German locale switch showed the menu closed, German copy in the
form, and an unobscured desktop layout. This was real Go/WASM production code,
not the JavaScript mockup.
No promotion was submitted during this copy review.

At this checkpoint `go test -count=1` passes for `internal/humanwork/productui`,
`internal/humanwork/workspace`, `test/workspace`, `tools/uxqual/productclient`,
`tools/uxqual/journeyclient`, `tools/uxqual/render/journey` and
`tools/uxqual/cmd/journeywasm`. The root `npm test` suite and Go vet on the
affected rendering/client packages also pass. New projector, renderer and client
locale-change tests assert translated form text, preserved loaded data, and no
unresolved percentages or policy identifiers. The browser checks used the Codex
accessibility tree and screenshots, not Playwright. UXAUDIT-006 remains unticked
and uncommitted while broader empty/error copy and full locale coverage are
still under review.

A subsequent live German promotion-form review found that the employee fact
card still displayed `SAL-AE3 · P4` as the current role and formatted its pay
with English separators. The shared projection now carries the authorized
human-readable title and formats base pay with the resolved product locale;
the shared card shows `Senior Account Executive · P4` and `135.000,00 USD`
in German. This was verified after rebuilding the embedded Go/WASM asset and
restarting only the isolated local-dev backend, leaving the other checkout's
servers alone. Native projector and renderer tests cover the business label
and the en-US/de-DE money representations. UXAUDIT-006 is still partial:
ordinary error notices, remaining journey detail copy and the broader
en-US/de-DE/RTL surface matrix need review before its checkbox is honest.

The next German browser pass exercised an incomplete promotion proposal. The
browser's native required-field bubble was English and prevented the enhanced
client from displaying localized guidance. The enhanced proposal form now sets
the DOM `noValidate` property so the Go/WASM client owns that validation; the
plain POST fallback retains native required fields. Missing inputs show a
localized summary and field-linked messages in en-US, de-DE and Arabic without
making a proposal RPC. Correcting the next-role field in the live Codex browser
cleared its error while the still-missing position, pay and justification
errors stayed visible. Assistive severity prefixes now resolve through the
same locale catalog (`Warnung` and `Fehler` in the tested German path). The
property name is deliberately camel-case: GWC assigns raw boolean values as
DOM properties, and a lowercase `novalidate` key did not disable the browser
bubble in the live app. Renderer and client regression tests cover enhanced
versus plain forms, localized messages, independent error clearing and zero
RPCs for incomplete input. Targeted Go tests and vet pass. No proposal was
submitted during this validation pass.

The next refinement added a per-attempt focus cue and a field-linked error
summary. The browser adapter retries focus across the bounded GWC render
window, then scrolls the first invalid control into view; repeating the same
invalid submit increments the cue and refocuses it. The summary derives its
links from the exact invalid form fields, so its labels follow the localized
form and corrected fields disappear from the list. Live clicking the German
salary link focused the salary input without changing the product route;
repeating the invalid submit refocused Next role. A second screenshot review
found the inline links too cramped, so they now render as wrap-safe tokenized
link chips. The final German dark screenshot showed readable spacing and no
overflow at the desktop viewport. Native renderer/client/command/product UI
tests passed together with the repository-local temporary directory; Go vet
also passed. An earlier default-temp test process printed all package passes
but exited nonzero while deleting its Windows test executable, which is why
the successful local-temp rerun is the cited gate. The rebuilt Go/WASM asset
and isolated backend were restarted in the correct embed order. No proposal
was submitted.

UXAUDIT-006 and PROMOUX-007 remain open: server-side pay refusals still need
one typed localized mapper with corrective bounds and a support reference in
authorized diagnostics. The German page also still presents the browser-native
date input as `12/01/2026`, so localized date presentation needs review.

The machine reached roughly 350 MB free during a final server rebuild. The
linker could not allocate another temporary executable. The exact stale Go
build directories under this worktree's `.artifacts/tmp` were inspected, but
the environment rejected their removal, so no broader cleanup was attempted.
The browser-tested revision was retained and its Go/WASM asset regenerated;
the isolated dev server was restored from that revision's already-built
executable, with the other checkout's server untouched. A new source or asset
change will require more free disk before its server embed can be validated.

## Typed promotion refusals and live demo-path audit

The local disk recovered, and the affected Go tests passed again. Journey
notices and proposal field messages now resolve from the shared en-US, de-DE
and Arabic catalogs. A German/Arabic harness test checks an owned pay-field
refusal through the Go journey client and ensures provider prose, rule text
and correlation identifiers never appear in ordinary markup. The transport
no longer scrapes a rejected field out of `err.Error()`: the journey port
carries a typed field and stable reason, and untyped or unrecognized
coordinates project only a generic request-level violation. Detailed wording
is retained only in the owned diagnostic. Transport tests exercise typed,
legacy and hostile refusals.

The Codex browser exposed a separate promotion break: the demo workforce
publishes Adrian's SAL-AE3/P4 to SAL-DIR/M4 next step, while server admission
consulted only the fixed conformance corpus. The server now admits edges from
the same deterministic demo ladder used by the options projection. A unit
regression checks every published demo edge against admission, wrong-org
rejection, and exact-money parsing. This is not yet a complete governed pay
rule for those edges; a broad parseable amount can still reach preflight.

The first live retry after that change returned a generic error. A read-only
migration status showed the shared local dev database at version 285 with
promotion guard migrations 286 and 287 pending; both were applied with the
repository migrator, without resetting data. The next Codex-browser retry
created blocked demo journey `01a09881-fa40-7a34-bcdd-9ff1e227848e`, proving
the previously offered role now passes the ladder gate. It also exposed two
remaining blockers: the free-text target position was not a real vacant slot,
and the 100,000 USD proposal is below the current 135,000 USD. The detail
page still renders much of its content in English under German locale. No
approval or employee/pay change was made; this intentionally blocked request
remains local demo data. PROMOUX-007, UXAUDIT-006 and the usable promotion-path
work remain open pending an authorized corrective Money bound, a discoverable
or optional target-position contract, real integration coverage and a
localized detail-state review.

## Promotion path reaches finance review in the isolated live stack

The follow-up removed `desired_position_id` from the required form contract
while retaining it as an optional, validated reference. The proposal kernel
now omits the POSITION subject when none was supplied, rather than creating
an invalid empty subject. The live German form labels the field optional and
advises against guessing a vacancy. An embedded-Postgres regression proves a
proposal without a position creates a durable journey.

The offered HarborCare ladder now carries an authored 5%–50% base-increase
range. Server admission enforces those bounds in exact Money arithmetic and
refuses sub-cent input instead of rounding it; penny and sub-cent boundary
tests pass. The sample company also now publishes a tenant-scoped,
versioned pay-band catalog: each staffed role's published base is the
midpoint, with explicit 80%/120% bounds in each seeded pay zone. Bands carry
source authority and provenance, and remain inaccessible to another tenant.
Tests cover every published promotion target and preserve the fixed
conformance corpus. This is an illustrative HarborCare policy, not a
production tenant pay-band implementation.

The isolated backend was rebuilt with this catalog. In the Codex browser,
Naomi's prior blocked request `01a0988a-1222-7c92-a440-1d9bcc3cbf52`
reprojected as Proposed under the new catalog, the missing-band check
disappeared, and the guarded Start approval workflow action advanced it to
Finance approval with an assigned finance review. No employee or pay record
was changed. The retroactive change to a previously blocked journey is a
reproducibility concern requiring a separate audit of policy pinning before
release. A missing-data finding now has a distinct `Needs information`
severity and warning tone rather than the misleading `Information` badge.

The browser screenshot still shows substantial English copy in German locale
(journey header, action, detail, dates and money); UXAUDIT-006 remains open.
PROMOUX-007 remains open because authorized corrective pay bounds and a
support reference are not yet surfaced consistently. The observed state is
finance-review start, not completed promotion.

A visual pass after the transition found the finance stage still instructed
the requester to start a workflow that had already started. The shared stage
projector now distinguishes Proposed (not yet begun) from Finance approval
(assigned and waiting), with a unit regression and a Codex-browser reload
confirming the corrected copy. The German translation gap remains open.

The broader `test/bootstrap` package is not green yet. Its fixed-corpus
promotion fixtures still supplied an unresolvable free-text target position;
the shared test helpers now omit that optional field in tests that do not
exercise the position picker, and the direct journey/transport cases pass
again. The transport security assertions were updated to inspect the typed
field violation instead of expecting an internal field key in the public
error sentence. The latest focused package run still fails a legacy multi-intent scenario:
they create several simultaneous active promotions for the same worker,
which the newer active-promotion admission guard correctly refuses. Those
scenarios need redesign around distinct workers or a different intent family;
the guard must not be loosened to make them pass. A full package rerun after
the fixture changes is pending, and this is not a claimed backend-wide green
gate.

## Bootstrap parity and localized promotion detail checkpoint

The remaining bootstrap failures were fixture conflicts with the durable
active-promotion guard, not evidence that the guard should be relaxed. The
NEXT-004 parity case now replays the same material promotion over the edge
transport and checks that both surfaces name one intent. Its negative replay
case changes the business reason under the same key and expects a conflict.
The shared protobuf-Struct fixture uses deterministic wire encoding, since
the idempotency profile binds typed bytes. The service also compares a replay
digest recomputed under the incumbent intent ID, because that ID is part of
the digest domain. The execution-authority edge case uses an independent
cell to prove a second executable promotion without issuing a conflicting
request for Omar. The P1A transport test simulates the same promotion through
both surfaces. The two focused cases and the complete
`go test -count=1 ./test/bootstrap` package now pass. No production
active-request restriction was weakened.

The first live German detail pass exposed English navigation, promotion title,
stage chip and all five step descriptions above the fold. The list and detail
stage labels, profile/list links, hero labels, five-step explanations and
screen-reader step-state words now resolve through the shared en-US, de-DE
and Arabic catalogs. Projector and renderer tests cover the three locales,
every known stage's step text, the current-step ARIA state and list/detail
consistency. `go test -count=1` passes in `internal/humanwork/productui`,
`tools/uxqual/journeyclient` and `tools/uxqual/render/journey`; gofmt and
`git diff --check` are clean. The Go/WASM bundle and isolated backend were
rebuilt in that order. In the Codex browser, Naomi's Finance approval detail
showed a German heading, chip and five translated stages; switching to Arabic
kept the five-stage hierarchy and current state readable in RTL. No action was
submitted and no employee/pay record changed in this visual check. The first
reload briefly stayed on a loading skeleton after the server had responded;
navigating to the Journeys list and reopening the record recovered, while the
next rebuilt reload settled normally. This needs a dedicated hydration review
before being called a resolved performance issue.

UXAUDIT-006 remains open: the detail comparison, findings, outcome and history,
the loading notice, money and date/time formatting still mix English and
locale-specific text. PROMOUX-010's confirmation layout also remains open.

## Localized promotion detail and RTL money checkpoint

The promotion detail's business comparison, checks, effective-date window,
pending/recorded outcome and history now use shared en-US, de-DE and Arabic
catalog keys. Exact decimal Money still uses rational arithmetic; the client
formats currency, signed deltas and percentages through the locale formatter
without binary floating point. The shared date formatter now renders German
day.month.year and Arabic day–month-name–year instead of ISO fallback. The
ordinary history maps the server's known finance/manager review transitions to
localized labels, identifies workflow-originated events as System, and omits
raw workflow-node names; authorized diagnostics retain the node evidence.

The Codex browser showed a bidi defect in the Arabic signed pay change. Money
values and the delta now carry directional isolation, and the hero pay line
isolates its LTR text inside the RTL-aligned container. A second live pass
confirmed the signed amount reads in order without pulling the hero amount to
the left. The same pass found that an unrecorded Finance-review request still
headed its pending explanation “Recorded outcome”; the component now uses
“What happens next” until a ledger outcome exists. The context navigation's
accessible name and the visible diagnostics disclosure label also resolve
through the locale catalog.

Named UXAUDIT-006 i18n, browser, accessibility and regression tests cover
the comparison, exact Money/date formatting, review-history mapping, raw-node
suppression, RTL isolation, and pending-versus-recorded heading. Full
`go test -count=1` passes in `internal/experience/localize`,
`internal/humanwork/productui`, `tools/uxqual/journeyclient` and
`tools/uxqual/render/journey`; the rebuilt workspace and WASM command
packages (`internal/humanwork/workspace`, `tools/uxqual/cmd/journeywasm`)
also pass. `npm test`, targeted `go vet`, `gofmt -l` on the edited Go files
and `git diff --check` pass.
The Go/WASM asset was rebuilt before the isolated backend, then the Codex
browser was reloaded and visually checked in dark German and Arabic RTL at
desktop width. The final German screenshots show the comparison, checks,
history and pending outcome without clipping. No promotion action or employee
record change occurred in this pass.

UXAUDIT-006 is still open. The seeded business reason is English user data,
and some other server-generated finding and loading/diagnostic body copy
remains untranslated. The known Finance budget-observation finding now resolves
from its stable code through the shared locale catalog, not by parsing its
English sentence. The targeted projector test passes and the isolated Go/WASM
stack was rebuilt again; Codex-browser screenshots confirm the full finding
card reads in German and Arabic RTL without clipping. This live pass did not cover light
theme or 390/320 px widths for this change. PROMOUX-010 and the broader
promotion-completion and UI/UX gates also remain open; no PR is claimed.

One more Codex-browser RTL pass exposed the authored English reason crossing
the history spine. Proposal facts and history details now use automatic text
direction without translating user data, while the timeline spine uses CSS
logical inline-start instead of a hardcoded left edge. The rebuilt Arabic
detail was reloaded and visually checked: the reason stays readable and the
spine follows the right-hand RTL event dots. A named regression guards the
logical-position rule; the renderer package test passes after the change.

## Promotion action and recovery language checkpoint

Start, Approve and Reject action cards and their shared confirmation controls
now receive localized content props rather than selecting English prose inside
the renderer. Loading notices carry a typed busy flag and catalog keys, so the
loading proxy no longer recognizes activity by comparing an English title.
Successful transitions likewise carry stable title/detail keys. The waiting
notice describes a recorded promotion outcome after effective-date checks; it
does not promise an employee-record mutation that the current composition does
not perform.

Client-side refusals for overload, unknown actions, missing employees, invalid
target roles, stale employee facts, missing rejection reasons and unavailable
journey links now resolve through the en-US/de-DE/ar catalog. The watch-stopped
warning uses task-oriented recovery copy rather than naming an engine or feed.
Transport refusals retain their typed safe mapping, but now also carry catalog
keys so a locale change cannot leave an earlier notice in the old language.
The first client test run caught a real regression: the employee-specific
invalid-input title was replaced by the generic proposal title during page
projection. A dedicated localized title key fixed it; the existing field-error
test then passed. Missing, stale and unauthorized journey selectors still
produce the same safe notice.

`go test -count=1` passed for `tools/uxqual/journeyclient`,
`tools/uxqual/cmd/journeywasm`, `internal/humanwork/workspace` and
`internal/humanwork/productui`. The client test command reported all test
results green but then Windows denied deletion of its temporary test binary;
the repository's documented Windows rule treats that cleanup line separately
from a test failure, and a fresh rerun exited cleanly. Targeted Go vet, gofmt
and diff hygiene passed; `npm test` passed across the root and workspace
Vitest suites. The Go/WASM asset and
isolated backend were rebuilt, and a fresh Codex-browser sign-in as Rafael at
port 18768 showed the Arabic dark-theme unavailable-journey notice, its
recovery link and a successful return to the Arabic Journeys list. The
ordinary page did not expose the intentionally unknown selector or transport
diagnostics. Action/confirmation localization is covered by component tests,
but an authorized live action card was not available in this browser pass;
desktop light and 390/320 px visual checks remain. UXAUDIT-006 and
PROMOUX-010 remain open, and no PR is claimed at this checkpoint.

## Compact promotion confirmation checkpoint

The old expanding review disclosure pushed the final approval below the
viewport. Start, Approve, Reject and any later consequential action now render
one shared native dialog with the employee, proposed placement and pay,
effective date, consequence, reason and a fixed action footer. The Go/WASM
controller opens it from the compact action card, focuses its heading, closes
on Cancel or Escape, restores the opener on cancellation, and marks a pending
submission busy while disabling both modal buttons. The exact approval review
was manually checked in the isolated Codex browser with an assigned Finance
reviewer; it recorded one real local-development decision and advanced Naomi
to Manager approval. A second browser pass as Dominic inspected both modal
variants, confirmed Cancel and Escape return focus, followed keyboard Tab
through the dialog, and recorded the Manager approval. The journey is now
Waiting for effective date, not falsely presented as an employee-record write.

The live rejection dialog exposed a misleading hint: it said the manager would
see the reason even when the manager was the actor. The en-US, de-DE and Arabic
catalogs now name the requester instead. The rebuilt dark desktop browser
showed the corrected English text without clipping. Component tests now check
the shared dialog across five consequential action IDs, its labeled/focusable
structure, bounded CSS and the translated rejection audience. A new gated
client regression holds the decision RPC open: the review and busy notice
remain projected, and the production task scheduler sends exactly one decision
despite a duplicate click. Without a task scheduler the test first sent two
RPCs, which confirmed why the production composition, rather than the native
test seam, must be used for the concurrency assertion.

`go test -count=1` passed for `tools/uxqual/render/journey`,
`tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm` and
`internal/humanwork/productui`; `npm test`, targeted native and js/wasm
`go vet`, gofmt and `git diff --check` passed. The Go/WASM asset and isolated
backend were rebuilt in order. The first live Manager success check found one
remaining focus defect: after the modal closed, focus landed on the web area
instead of its success notice. The controller now retries result focus through
a bounded render window after GWC commits. A second live browser pass started
Samuel's proposal and recorded the Finance approval; after each consequential
action, keyboard focus landed on the visible success notice. Slow-network
modal geometry, exact 390/320 px widths, light theme and reduced-motion visual
checks are still outstanding. PROMOUX-010 stays open; UXAUDIT-006 and the mega
PR remain open as well.

## Promotion select-state alignment checkpoint

Manual entry from People into Peter's untouched promotion exposed a real
state/display disagreement: the client correctly held no target job or grade,
but the browser showed the sole published job and grade while the pay-rule
help still asked for a selection. The shared native select renderer did not
emit the empty value because GWC's typed `Props.Value` elides empty strings.
It now emits that value explicitly for the placeholder option, while retaining
the normal typed value for nonempty choices. A focused
renderer regression pins the empty choice; the existing client regression
still requires consequential fields to begin empty. After rebuilding Go/WASM
before the embedded server, the isolated Codex-browser pass showed both
placeholders after reload and after People -> Peter -> Start Promotion. Choosing
FIN-DIR then selected its unique M4 grade and displayed the 5–50% base-pay
rule; clearing the role restored both placeholders and the initial help.
`go test -count=1 -cover ./tools/uxqual/render/journey` passed at 96.4%,
the affected client, WASM command and product UI package tests passed, as did
targeted vet, gofmt, `git diff --check` and `npm test`. The staged coverage
command passed with no staged Go packages, which is not a coverage claim for
this uncommitted change. No proposal was submitted for Peter. This closes the observed
state/display defect, not the broader PROMOUX-010 or mega-PR gates.

## Promotion route-hydration refinement checkpoint

The previous select-state checkpoint did not settle every return-navigation
case. A later live pass found the promotion form and request list could remain
on a loading proxy after their service reads had succeeded. Temporary
diagnostics showed the journey store held a ready answer and active subscribers
while the route-owned GWC leaf still displayed its earlier busy snapshot; the
diagnostic instrumentation was removed after identifying the render race.

The embedded journey client now suspends its route and watch on departure so a
return to the same employee rereads current authority and starts a fresh draft.
The product router keys the journey leaf by its source address, and the live
store subscription posts functional revision updates through GWC's frame inbox
after the leaf commits. The loader applies the journey route only after its
own cancellation check. Native and js/wasm regressions cover fresh re-entry,
an answer arriving between read and subscription, burst updates, and distinct
form/list route fibers.

After rebuilding the Go/WASM asset and isolated local-dev backend, the Codex
browser at port 18768 showed Peter's empty promotion form, form-to-list, list-
to-Samuel-detail and detail-to-list software navigation without a stuck loading
proxy. The dark desktop request list was visually inspected at 1280×720. No
proposal or decision was submitted in this pass; the other checkout's port
8768 was untouched. Focused native tests passed for journeyclient (86.2%),
journey renderer (96.3%), journeywasm command (84.1%) and workspace (65.3%,
covered by the existing exact-path below-floor exception). The four focused
js/wasm route tests, targeted Go vet, `npm test`, gofmt and `git diff --check`
also passed. This is not a full E2E, narrow-viewport, light-theme or
reduced-motion clearance, and UXAUDIT-002, UXAUDIT-006 and PROMOUX-010 remain
open. No PR is claimed at this checkpoint.

## People directory scroll and row-action checkpoint

The isolated local-dev Postgres, cell and same-origin gateway were restored on
5432, 18080/18443 and 18768; the other checkout's 8768 server was not touched.
The first Codex-browser People pass found that the repeated unavailable-workflow
paragraph had become a narrow in-flow disclosure: opening it expanded the
employee row and made a dense table worse. The page also needed two competing
vertical scrollports to reach all rows. The shared transient popover now keeps
the server-resolved reason behind a compact named control, while People alone
uses the main page scrollport and floats its desktop action panels. Tighter
filter and column widths fit the five filter controls on one line at the
isolated browser's desktop width without the oversized checkbox. The table
still comes from the configurable `DataTable` component; no People-specific
sort or pagination path was introduced.

After rebuilding the Go/WASM asset and embedded backend, the live dark desktop
browser showed more than six People rows in view, an aligned sticky header
after scrolling, both directions of Role sort updating the rows in place at
the current scroll position, a compact unavailable explanation that did not
stretch its row, and Escape returning focus to the collapsed trigger. Setting
Rows per page to 100 rendered all 64 authorized people; the final row's
explanation was also reachable. The 100-row choice remained selected after
navigating to a clean People URL and reloading it. The first full package test
run earlier in the iteration exceeded only the existing 5 ms interaction-p95
gate while npm ran concurrently; the isolated rerun passed. After the scroll
refinement,
`go test -count=1 -cover ./internal/humanwork/productui` passed at 92.4%, as did
targeted `go vet`, gofmt, `git diff --check` and `npm test`.

UXAUDIT-008 remains open. The current automated layout and 100-row assertions
are not mislabeled as a real browser or timed performance test. The Codex
browser's temporary 390/320 px viewport override produced screenshots with
a large black capture gutter rather than an unambiguous app-width result, so
those widths are not claimed as passed; the override was reset. A 100-row
timing budget, reliable narrow-width and light-theme checks, and the remaining
end-to-end matrix are still required.

## People filter localization and timing refinement

A fresh isolated Codex-browser pass found the location selector's label clipped
on desktop. Rebalancing the three filter tracks fixed English, but the longer
German label still clipped; the second track adjustment left the entire German
“Alle Standorte” text visible without adding a filter row. The search hint is
now a short, localized name/role/worker-ID cue, because the long hint was
visibly truncated in the available field width. English, German and Arabic RTL
were each inspected in the running Go/WASM app after the final rebuild. The
Arabic filter and table fit the normal desktop capture without a direction or
alignment defect. Temporary 390×844 and 320×720 Codex-browser overrides again
showed a large black capture gutter; the resulting image cannot establish the
actual app width, and the override was reset.

`TestTodo_UXAUDIT_008_Performance` now measures a verified 100-row People
component fixture at a 50 ms p95 budget (25 samples, three warmups); its
focused run reported p95 below 8 ms. This bounds component-side rendering,
not network or browser paint. The full coverage run reached 92.4% but exposed
an existing CSS snapshot that still expected the previous filter track values.
That assertion was updated and the named UXAUDIT-008 plus responsive-style
tests passed afterward. The full product UI package then passed with
`go test -count=1`, as did targeted `go vet`, `git diff --check` and the root
`npm test` workspace suite. Light theme, reliable narrow viewports and
end-to-end evidence remain before UXAUDIT-008 can be ticked.

## Narrow header collision discovered during People review

The Codex-browser narrow-width pass surfaced a shell regression outside the
People table itself: the compact global-search input retained a higher-
specificity desktop padding rule, so its collapsed field covered most of the
adjacent Start an action trigger. The mobile rule now has matching specificity
and hides the long placeholder only while collapsed; focus restores its
readable placeholder. Below 350px, the header gives search and actions their
own row so notification and profile controls do not compete for the same
horizontal space. The `WEB-047` CSS golden and responsive shell assertions were
updated to pin this arrangement.

After rebuilding the isolated Go server, the narrow Codex-browser capture
showed separate glyph targets. Start an action opened a bounded panel with
authorized results, Escape closed it and restored focus to the trigger, and
global search expanded then collapsed after focus left it. The browser tool
still adds a black gutter to temporary narrow-viewport captures, so this is
evidence of the observed collision and interaction fix, not a precise 320px or
390px layout certification. The viewport override was reset afterward.
The full product UI package and targeted Go vet pass after the shell change;
gofmt and diff hygiene are clean. UXAUDIT-008 remains open.

## Unavailable Experience Studio publication gate

UXAUDIT-011 is complete for the current release. Experience Studio remains a
registered direct route with an explicit unavailable explanation and return
path, but the page registry does not publish it in primary or Admin navigation.
The same registry decision keeps it out of global search and page utilities;
role permissions or a forged navigation projection cannot reintroduce it.
The live isolated Codex browser showed the unavailable state and no Studio
menu item or authoring control. The named primary, browser-rendered, security,
and localized conformance tests pass. No preview is enabled for this release.

## Promotion pay-refusal refinement (PROMOUX-007, in progress)

The live Jane promotion path from People showed a corrective-action gap: an
out-of-range base-pay refusal focused and linked the right field and kept the
entered reason, but said only “within the approved pay range” without an
amount. A pure domain calculation now derives inclusive cent bounds from the
current pay and published ladder percentages, ceiling the lower threshold and
flooring the upper. Server admission uses the same predicate. A cent-boundary
regression proves 105.03 is refused, 105.04 and 118.03 admitted, and 118.04
refused for a 100.03 baseline. The Go/WASM projection shows the formatted
range before submit and maps only the typed ladder-range refusal to it;
unrecognized reason references and unresolvable facts retain safe generic
copy. Native submissions now synchronize submitted values into the controlled
store before a refusal re-projects the form.

The rebuilt isolated Go frontend at port 18768 showed Jane's annual range as
USD 173,250.00–194,700.00. Submitting 100.00 produced that same field-linked
range, focused Proposed base pay, preserved the reason, and editing pay to
173250.00 cleared the stale error. German rendered localized money order and
separators; Arabic rendered RTL with the bounds. The named PROMOUX-007 tests,
full domain, journey-client, journey-renderer, intent-app and product-UI package
tests, `go vet`, `npm test` and `git diff --check` passed. Explicit coverage
reached 78.2% for promotion, 86.4% for journey-client and 96.2% for the journey
renderer, 47.5% for `internal/intent/app` (under its existing, unexpired
exact-package below-floor exception) and 92.4% for product UI.

PROMOUX-007 remains open: a copyable support reference behind authorized
diagnostics and a shared SSR/enhanced refusal projection still need proof.
The Codex browser's temporary 390px viewport override again produced a tiny
page with a black capture gutter, so this pass does not certify responsive
layout. The live locale popover's links also lacked accessible names in the
Codex AX tree, and the target-position control is still free text despite the
governed picker component; both need follow-up before UXAUDIT-002 can close.

## Promotion position regression exposed by the real-server gate

The broader `TestPromoUXRealServerPromotionContract` first returned BLOCKED
with `promotion.target_position_not_found`: its old `POS-HRBP-301` display code
was never a Position revision reference. Replacing that guess with a real
tenant-scoped `job_position` and a reader-issued revision reference exposed two
more mismatches instead of hiding them. The intent subject had been set to the
opaque token (which is not canonical subject-id text), and the simulation's
domain-input resolver lacked the real Position reader that the workspace
preview already used. The intent now records the canonical position entity id
as its subject while retaining the exact revision reference in the proposal;
the resolver shares the composed Position reader, pins a position subject only
after a real read, and passes that reader to promotion preflight. The workflow
fixture seeds an actual legal entity, org unit, job and vacant position, and
uses its issued reference. An older full-lifecycle test was also updated to
use that fixture rather than its own guessed display code.

`TestTodo_PROMOUX_015_PositionRevisionHasCanonicalSubject`, the real-server
promotion contract and `TestTodo_PROMO_009_Integration` pass. The full
`internal/intent/app` package passed with 47.5% coverage under its recorded
exception, and the full `test/workflow` package passed after the fixture
repair. The targeted bootstrap tests for corpus proposal, created-worker
promotion completion and typed promotion integration also passed.
`go vet ./internal/intent/app ./test/workflow`, `npm test` and
`git diff --check` pass. The isolated server was rebuilt and restarted; in
the Codex browser Jane's invalid annual pay again focused the pay field,
showed the exact inclusive range, preserved her entered business reason, and
cleared only that error when pay was corrected. This does not make the live
target-position picker or its demo vacancy catalog complete: the browser form
still labels that input optional and accepts free text. PROMOUX-007,
PROMOUX-015 and UXAUDIT-002 therefore remain open.

## Promotion refusal support disclosure refinement (PROMOUX-007, still open)

The live Go/WASM rejection now carries only a validated, opaque server-issued
request reference into the shared journey notice. The reference is selectable
in a closed, keyboard-operable Support details disclosure; ordinary title and
detail copy remain localized and omit transport descriptions, field-rule names
and the reference. The notice layout uses the existing theme tokens and a
bounded input width. The form's error summary now says “Fields to correct”
instead of “Fields to complete,” because an entered-but-invalid pay amount is
not an empty field. English, German and Arabic labels were updated together.

The isolated server and WASM bundle were rebuilt on port 18768. In the Codex
browser, Jane's invalid USD 100.00 proposal focused and scrolled to Proposed
base pay, retained the entered reason, displayed the inclusive USD 173,250.00
to USD 194,700.00 bound and rendered the revised summary. Expanding Support
details exposed a read-only, selectable `req:` token with a matching accessible
label; the default closed notice did not show it. Named PROMOUX-007 tests now
exercise valid and hostile token shapes, localized disclosures, SSR-capable and
enhanced renderer projections, preservation of another field's refusal and
recovery after editing. The journey-client and journey-renderer targeted suites,
`npm test`, scoped `go vet`, `gofmt -l` and `git diff --check` passed. The
full package coverage run passed at 86.5% for journey-client, 96.1% for
journey renderer, 92.4% for product UI and 78.2% for promotion domain.
`go test -count=1 -cover ./internal/humanwork/workspace ./tools/uxqual/cmd/journeywasm`
passed at 65.3% (workspace's existing below-floor exception) and 84.1%.
The real PostgreSQL `TestPromoUXRealServerPromotionContract` and both of its
subtests passed; Go then returned a Windows `unlinkat ... Access is denied`
temp-executable cleanup error, which the repository's gate policy explicitly
does not classify as a test failure when the test result lines pass.

The item is not ticked: the production journey route intentionally serves a
WASM shell rather than a no-JavaScript SSR proposal form. Both native and live
renderers consume the same projected Page, but an actual server-side proposal
submission using this mapper has not been demonstrated. Fresh manual 390px,
320px and light-theme inspection also remain outstanding; the browser API's
prior viewport override produced an unreliable capture.

## People directory density and isolated-refresh review (UXAUDIT-008, in progress)

The Go People composition now uses a tighter heading/summary/filter rhythm and
denser table cells without shrinking the 44px row-action targets. The wide
filter reserves enough space for both Filter and Clear; a live test first
exposed horizontal overflow when Clear appeared, and the revised one-row
layout removed it. The search input retains its accessible name when its
redundant visible label is hidden at desktop width. The shared DataTable,
local sort projection and server table-preference contracts remain in place.
The WASM route classifier now includes the eligible-only filter in the same
directory-scoped refresh as search, team, location, sort and page size; a named
regression refuses to classify page, locale or navigation changes that way.

The isolated Go server and WASM bundle on port 18768 were rebuilt. In the
Codex browser at the default 1280px desktop viewport, a 64-person page showed
six complete rows before scrolling; a sticky header stayed aligned while the
main content scrolled independently of the sidebar. Sorting Person while
scrolled changed only row ordering and kept the viewport in place. Searching
for Jane produced one row; Clear restored 64 without overflow. Changing page
size from 100 to 20 produced four pages, and a clean Home-to-People navigation
reloaded the saved 20-row preference from the server without a page-size URL
parameter. The eligible-only filter produced 37 of 64 people without a shell
skeleton. At 390px and 320px in dark mode, the menu became a compact header,
filters stacked, the sort strip scrolled horizontally and person rows became
readable cards with no clipped row controls. The browser's first small capture
had a black gutter because the emitted image included the surrounding browser
surface; re-emitting the original capture showed the actual narrow viewport.

`go test -count=1 ./internal/humanwork/productui -run '^TestTodo_UXAUDIT_008'`
passed the PRIMARY, BROWSER, ACCESSIBILITY, PERFORMANCE and REGRESSION tests;
the 100-row renderer measured about 1 ms p95 under its 50 ms budget. The new
WASM `TestTodo_UXAUDIT_008_Regression_PeopleRegionRoutes` passed. Full
`go test -count=1 -cover ./internal/humanwork/productui` passed at 92.4%; the
full native journeywasm and productclient suites, `npm test`, scoped `go vet`,
gofmt and `git diff --check` passed. The staged-coverage gate passed two
packages using a private temporary Git index, without staging into the shared
branch index. The WASM generator produced the bundle but returned the known
Windows temp-executable unlink error after output; the subsequent server
build and live browser test verified the generated asset.

The final client bundle was rebuilt with the eligible-filter routing fix and
retested live. A temporary SSR page served the production Go components and
stylesheet with a synthetic light-mode People projection at desktop, 390px
and 320px, including scrolled person cards; its first post-resize capture also
contained a browser gutter, but the settled original captures showed the
whole viewport without overlap, truncation or unreadable controls. The SSR
fixture and its process were removed afterward. No organization-wide
appearance setting was changed merely to make this test pass; its saved
selection remains Use system setting. The final
`go test -count=1 ./internal/humanwork/workspace ./tools/uxqual/cmd/journeywasm`
run passed after regenerating the embedded asset. UXAUDIT-008 is ticked with
that evidence. No commit was made from the shared index; the mega-PR branch
still contains concurrent work that requires integration review before a
grouped commit.

## Localized promotion refusal recovery (PROMOUX-007, still open)

The live German proposal reproduced a recovery defect: after an out-of-range
pay refusal, changing Proposed base pay removed the field error and summary
link but left the page-level refusal visible. The client compared translated
notice text to an English literal. Correction now uses stable notice catalog
keys, including required-field notices. Refusal state retains only allowlisted
field correction keys and exact Money so changing locale after a refusal, or
while an RPC is in flight, re-localizes the field message without retaining
raw transport descriptions. Regression tests exercise German and Arabic
correction, post-refusal switching and a delayed-response locale race.

The revised Go/WASM bundle was built and served through the isolated frontend
on port 18768. In the Codex browser, Omar's rejected 100.00 USD proposal
showed a localized 97,650.00–106,950.00 USD bound, a summary link and focus
on the pay control; changing it to 97,650.00 removed the stale notice and
error while preserving the business reason. The corrected dark page was
inspected at desktop, 390px and 320px; the viewport override was reset.

`go test -count=1 -cover ./tools/uxqual/journeyclient
./tools/uxqual/render/journey ./internal/humanwork/workspace` passed at
86.5%, 96.1% and 65.3% respectively (workspace retains its recorded
below-floor exception); scoped `go vet`, gofmt, `git diff --check` and
`npm test` passed. An independent adversarial review confirmed the locale
fixes but refused a completion tick: the purported SSR test renders a copy
of the enhanced Page, while the actual Journey HTTP route still serves a
WASM shell and has no server-side proposal POST/recovery path using the
shared refusal mapper. The client also computes displayed bounds from cached
worker and path data captured before an asynchronous refusal; a changed
authoritative baseline could make those exact cents stale. These are the
next integration issues for PROMOUX-007; no TODO tick or commit is claimed
from this partial checkpoint.

The stale-bound issue is now addressed on the typed proposal-refusal path.
The application attaches the exact inclusive `Money` bounds calculated by
ladder admission to `JourneyInputError`; the journey transport permits them
through an additive Protobuf `MoneyRange` field only for proposal operations,
and the shared envelope preserves them across the gRPC error detail. The
client parses only coherent, exact server-supplied bounds and gives generic
guidance when the field is absent or malformed. It no longer passes its cached
worker/path calculation into the refusal mapper. The previously separate
form-help calculation remains guidance, not a source for a server correction.

`TestTodo_PROMOUX_007_ServerMatchesDisplayedCentBounds`,
`TestTodo_PROMOUX_007_Conformance_ServerPayBoundsSurviveGRPC`,
`TestTodo_PROMOUX_007_Regression_MoneyRangeDetailRoundTrip`,
`TestTodo_PROMOUX_007_Regression_ServerBoundsOverrideStaleClientProjection`
and the non-proposal disclosure regression now pass. The owned envelope also
rejects reversed or malformed Money values on both construction and wire
recovery. `TestTodo_PROMOUX_007_Integration_RealLadderRangeParity` now runs
the actual application ladder through the composed cell, direct gRPC and
the browser tunnel, pinning 97650.00–106950.00 USD; it does not claim to be
a browser-rendered end-to-end test. An adversarial review
found that the first transport draft could publish a pay range on unrelated
journey operations; it was restricted to `propose` and `propose_promotion`
before the live retest. `buf lint`, scoped `go vet`, `npm test` and the
generation-drift tests pass; the latter reported the known Windows
test-executable unlink denial only after its `ok` line. In the isolated live
Go/WASM frontend on port 18768, Omar's intentionally rejected 100.00 USD
proposal displayed the server's German 97.650,00–106.950,00 USD correction,
linked the summary to Proposed base pay, focused the field and retained the
business reason. Desktop, 390px and 320px dark-mode screenshots were
inspected; the temporary viewport override was reset. The genuine HTTP SSR
proposal/recovery path remains absent, so PROMOUX-007 is still unticked and
this partial work is not yet a commit or PR.

The private-index staged-coverage gate passed for six changed packages, with
the pre-existing `internal/intent/app` and `internal/humanwork/workspace`
below-floor exceptions applied at 47.5% and 65.3%. The new real-cell parity
test passed separately in `go test -count=1 ./test/bootstrap -run
'^TestTodo_PROMOUX_007_Integration_RealLadderRangeParity$'`; scoped `go vet`,
gofmt, `git diff --check` and Markdown formatting were clean after the
envelope hardening. The final isolated server build was reopened and the
German typed refusal retested in the Codex browser with the correct field
focus, range and retained answer.

### Promotion confirmation review (PROMOUX-010, in progress)

Manually exercised a complete local-development confirmation chain in the
isolated Codex browser on port 18768: Rafael proposed and started Omar's
promotion; Thomas opened and approved its finance review; Dominic opened
the shared Reject and Approve reviews, cancelled one with Escape, then
approved the manager step. The modal consistently named the employee,
placement, pay change, effective date and consequence, with the final action
visible. Escape returned focus to the trigger. At 320px the original German
Cancel label broke into an awkward three-line word; the shared component now
keeps its localized accessible name and shows a compact glyph below the
mobile-first breakpoint. The final bundle was visually rechecked at 320px,
390px and desktop in dark mode, with the viewport reset afterward.

The live Start action exposed a larger regression the existing component
tests missed: the product route's same-journey loading state briefly
replaced its detail with a full-page proxy. The route now keeps the existing
journey store mounted only when the destination still names that same
journey; changing subjects still gets a destination-shaped loading state.
Another live approval exposed focused success feedback above the scroll
viewport. The confirmation controller now scrolls the resolved notice into
view without animated motion; the subsequent manager approval showed the
success notice in the 320px viewport. The `go test -count=1 -cover`
packages `./tools/uxqual/cmd/journeywasm`,
`./tools/uxqual/render/journey` and `./tools/uxqual/journeyclient` pass at
84.6%, 96.1% and 86.5%; the private-index staged coverage gate passes
both edited packages. `npm test`, scoped `go vet`, code style, gofmt and
`git diff --check` pass. `TestPromoUXRealServerPromotionContract` reached
the real workflow store and printed `ok`; Windows then denied unlinking its
completed test executable, which AGENTS.md classifies as a passed test.

The light-mode pass now covers the same approval modal at desktop, 390px and
320px; its action stayed visible, the narrow Cancel glyph retained the full
localized accessible name, and Escape returned focus to the trigger. The
organization's original “follow system” appearance was restored afterward.
The first live approval after rebuilding exposed a second pending-state gap:
the generic journey network wrapper could still replace an action review
with a whole-page proxy. Start, Approve and Reject now retain the detail
body during their typed busy notices, and the live GWC store skips the
pending revision so it cannot reconcile away the browser-managed open
dialog. An actual js/wasm test using the Node runner confirms the review
tree remains unchanged until the result arrives; a native renderer test
confirms the full-page proxy remains for ordinary detail loading. Scoped
native package tests, js/wasm regression and cross-target vet pass; renderer
coverage is 96.0%. `npm test`, `npm run check:code-style`,
`npm run format:check` and `git diff --check` also pass. The
client bundle and isolated server were rebuilt, and the German dark dialog
was inspected again. Samuel's manager approval completed and the resulting
waiting-for-effective-date state appeared without an application error.

PROMOUX-010 remains unticked until a deliberately delayed live action shows
the open modal's in-place progress and disabled actions through the network
wait; the fast live RPC made the AX pending snapshot and resolved screenshot
race. A fresh attempt to exercise that path with Isaac surfaced a separate
validation defect: the proposal form advertised USD 71,400.00–102,000.00 as
the allowed increase for WRK-MGR, but the server blocked USD 75,000.00 as
below the job-band floor. The blocked request then redirected a second
attempt to the existing journey, so it could not be reused for this dialog
check. The local demo now contains that blocked Isaac request; no employee
record changed. This needs the PROMOUX-007 correction flow and consistent
band intersection, not a false PROMOUX-010 tick. The German review also
exposed an English simulation finding
on a German page; that is recorded for the PROMOUX-009 finding projection,
not silently counted as fixed here. No commit or PR is claimed for this
partial todo.

### Promotion confirmation closure (PROMOUX-010)

The previously missing latency proof is now visible in the Codex browser.
On the isolated 18768 gateway, Rafael created Dominic's proposal at
USD 108,000.00 to 120,000.00 and opened its Start confirmation. A temporary
local TCP proxy delayed only `/workspace/grpc` WebSocket responses. While
the real Start RPC was in flight, the native modal stayed open with the
employee, placement, pay, date and consequence unchanged; its action bar
showed `Processing your request…`, and both Start and Cancel were disabled.
The next browser state showed an approval-process success notice and finance
review. The temporary proxy file and process were removed; the isolated
gateway was restored to the direct backend upstream. The earlier Omar flow
covered Approve, Reject, Escape/focus return and desktop/390px/320px dark
and light inspection, including the compact German Cancel glyph. None of
these local test proposals changed an employee ledger fact.

The delayed-service regression now covers Start, Approve and Reject, asserting
the exact busy key, retained detail/action surface, one RPC despite a duplicate
Submit, and the resolved outcome. The five-action renderer has an explicit
50 ms p95 budget, which passed ten consecutive repetitions. The review
listener is installed only after successful product-shell hydration, so a
fallback mount cannot leave a listener bound to a stale store. An adversarial
review initially flagged a no-script POST regression, but inspecting the
production journey shell showed that it never serves these WASM-rendered
action forms without JavaScript: it explicitly links readers to the separate
server-rendered Promotion workspace. The misleading `formProps` comment was
corrected. The reviewer found no remaining P0 production defect. Its
remaining browser-test concern is recorded rather than hidden: the named
Go Browser test pins native dialog markup, while native focus, Escape,
disabled pending controls and exact viewport geometry were verified manually
in the Codex browser, per the user's no-Playwright direction; there is not
a checked-in automated browser interaction spec for this todo.

`go test -count=1 -cover ./tools/uxqual/render/journey
./tools/uxqual/journeyclient ./tools/uxqual/cmd/journeywasm` passed at 96.0%,
86.5% and 84.6% coverage; the real js/wasm Node runner passed its mounted
pending-review regression; `go test -count=1 ./test/workflow -run
'^TestPromoUXRealServerPromotionContract$'` passed against the real store.
The private-index staged coverage gate passed all three affected packages;
`npm test`, `npm run check:code-style`, `npm run format:check`, scoped `go vet`,
gofmt and `git diff --check` passed. PROMOUX-010 is ticked; PROMOUX-011's
stale `Updated` timestamp was separately reproduced during Aya's Start and
remains open. This remains uncommitted work on the combined UI/UX branch;
there is no PR yet.

### Promotion chronology progress (PROMOUX-011 remains open)

The stale-time defect was traced to the application projection: the journey
list used durable time only for terminal records, while detail always exposed
the earlier intent simulation time. Both now take the maximum business instant
from the intent, instance, node executions, WorkItems, WorkItem transitions and
terminal ledger. Detail constructs its timeline first so the simulation event
does not get relabeled with an approval time. An adversarial review also found
that a CREATED instance without `started_at` was falsely presented as an
approval-process start; the timeline now omits that event until it really
starts. The reviewer also flagged equal-time node/decision ordering and the
distinction between transition `at` and DB `recorded_at`; the displayed time
intentionally uses the business `at`, while stream sequencing still needs a
separate durable source.

The isolated Go server on 18080 and Go frontend on 18768 were rebuilt. In the
Codex browser, Aya's request header and list card both showed Updated 08:59
UTC, matching the latest finance review transition; previously the header
showed 08:57 UTC. The dark desktop view had no observed overlap. Targeted
chronology tests, full `go test -count=1 -cover ./internal/intent/app`, scoped
`go vet`, gofmt and `git diff --check` passed. The app package reported 47.6%
coverage under its existing exact-path `below_floor` exception. No new
390/320-width or light-theme screenshot is claimed for this partial step.
The product-wide authority-filtered invalidation endpoint, sequence/catch-up,
affected-region refresh and concurrency/performance matrix are still absent,
so PROMOUX-011 is intentionally unticked and the mega PR remains open work.

### Promotion refusal recovery closure (PROMOUX-007)

The earlier refusal mapper and field-linked form were already in the shared
worktree when this pass began. An adversarial read-only review found that a
generic violation arriving before the exact pay-range violation silently won
the one-error-per-field slot. The mapper now ranks actionable typed
corrections above generic ones regardless of wire order, with a regression
for both orders. A new integration test puts a typed `JourneyInputError`
through the real gRPC admission/transport boundary, the production client
adapter, the application refusal projection and the SSR-capable GWC renderer.
It verifies exact server bounds, preserved input, an actual generated
`req:` support reference, summary-to-field association and redaction of the
port's private diagnostic. The engine in this narrow transport-composition
test is a refusal fake; the full PostgreSQL/domain/browser journey belongs
to PROMOUX-015, not a claim made by this test.

Rafael then used the isolated 18768 Go/WASM site to submit Julian's
out-of-range USD 190,000 proposal against a current USD 184,000 base and
published Director of Engineering range of USD 193,200–276,000. The real
server refused it without creating a promotion or changing the worker.
The Codex browser showed the exact range at Proposed base pay, a linked
error summary, focus and scroll to the field on first and repeat attempts,
retained business reason and input, edit-clears-error behavior, and a
copyable support reference behind a closed disclosure. Settled desktop,
390px and 320px dark and light views had no observed clipping or overlap.
The light-mode check temporarily saved an organization-wide appearance
setting in the local demo database; it was restored to the prior Use system
setting and verified after a cold reload.

The Go/WASM asset and integrity manifest were rebuilt. The first workspace
test failed because the generated raw WASM was incorrectly removed: this
repository embeds both raw and gzip representations. Rebuilding and keeping
the raw asset restored `go test -count=1 ./internal/humanwork/workspace` to
green. Full client, renderer and transport package tests passed at 86.5%,
96.0% and 81.8% coverage. The journeywasm package, scoped native/js-wasm
vet, `npm test`, the private-index staged coverage gate, the real-server
promotion workflow test and the bootstrap incomplete-form test passed.
A final adversarial review found no remaining P0/P1 objection, and
PROMOUX-007 is ticked. This is not a closure claim for PROMOUX-011 or the
combined UI/UX PR; both remain open.

### Task-language copy closure (UXAUDIT-006)

The all-page copy sweep found an unpublished-route return action that said
"live workspace", service-named Headcount and Position unavailable titles,
and Appearance guidance that exposed internal asset paths and an organization
allowlist. Central locale messages now replace those with task and recovery
language in English, German and Arabic. A component review also found that
Appearance advertised Upload, Preview and Undo even without a working asset
handler; the published page now hides every unwired logo lifecycle action.
That does not complete the upload/proxy/revision workflow in UXAUDIT-022.

The named UXAUDIT-006 primary, golden, browser-facing render, i18n and
regression tests pass across ready, empty, loading and error renders for every
registered page and all three locales. The error matrix also asserts a visible
recovery step. Full `go test -count=1 -cover ./internal/humanwork/productui`
passed at 92.6%, and workspace asset-integrity, i18n, vet, `npm test`,
format, code-style, private-index staged coverage and diff checks passed.
The generated WASM and integrity manifest were rebuilt. The first `go run`
produced complete assets but exited on a Windows temporary-binary unlink;
running the built generator directly exited successfully and workspace tests
passed. The isolated Go server and gateway on 18080/18768 were refreshed; the
user's 8768 server was not touched. In the Codex browser, the settled logo
section was inspected at dark desktop, 390px and 320px, light desktop, and
German/Arabic RTL. Light was a client-side preview only; a reload verified
the organization still uses its saved system setting. The RTL screenshot also
showed older Appearance option headings still in English. That is a remaining
broader localization/product-voice defect, not an assertion that this
technical-vocabulary todo has made the whole page multilingual. The mega PR
is still in progress and no commit was made in this shared-index window.

### Development persona capability alignment (UXAUDIT-014)

Local-dev login cards now derive their task promises from the same verified
principal and effective durable page grants used by the product shell. A
failed policy read produces neutral copy and refuses sign-in; a configured
empty policy never falls back to broader token roles. Each of the four
HarborCare personas is bound to its active seeded worker key, and sign-in
chooses a published page that the resolved role can actually view. The
development-only bootstrap narrows untouched payroll grants so finance
review remains available without advertising promotion initiation or
organization browsing. It covers the main Organization route and its
explorer, outline and responsive routes; a versioned administrator edit
survives bootstrap replay. Production default grants are unchanged.

The isolated Go server at 18080 and gateway at 18768 were rebuilt without
touching the user's 8768 server. In the Codex browser, Rafael signed in to
Home with Admin navigation; Dominic signed in to People, could find
"Promote an employee" in the action launcher, and was denied a direct Admin
route; Thomas signed in to My Work with two finance-review items, had no
promotion-start or Organization action, and was denied the direct main and
explorer Organization routes; Samuel signed in to his actual HC-21022 worker
profile with no available promotion and was denied People. A forged Thomas
promotion-start URL showed unavailable employee details and disabled form
controls. The login was visually checked at desktop, 390px and 320px, with
no observed clipping or overlap; the dark-theme admin landing was also
inspected. Browser warning/error logs were empty. The 320px Rafael button
wraps its label to two lines, but remains legible and usable.

The named `_Browser` Go regression tests the browser-facing HTTP/session/route
contract through loopback, not a real browser process; the real-server browser
matrix above was exercised manually in Codex browser because this project
session forbids Playwright. Focused and full tests passed for application
(81.3% coverage), role-access store (81.5%), and workspace (66.3%, covered
by its existing exact-path below-floor exception). Scoped vet,
`npm run lint`, `npm test`, format, code-style and a private-index
`npm run check:coverage:staged` passed. The final adversarial re-review found no
remaining P0/P1 code objection. No claim is made that the broader UI/UX
backlog or combined PR is complete.

### Home attention and continuity checkpoint (UXAUDIT-015, not yet closed)

Home now composes due-first assigned work, resumable drafts, tracked requests,
recent people, recent terminal activity and role-appropriate quick links from
the existing floorplan and shared components. The displayed collections are
bounded while preserving their full authorized totals. Work, History, People
and profile links are independently grant-gated; recent completion rows only
show a person name when the matching person record is admitted. An adversarial
review found and prompted fixes for the History/Work grant split, a denied
person name in the completion rail, and a status incorrectly marked up as a
`<time>` element. Home copy, empty states and the shared tracked-completion
suffix are localized for en-US, de-DE and RTL Arabic.

The named UXAUDIT-015 unit, regression, accessibility, i18n, browser-facing
markup and performance tests pass. The mixed 100-record Home render measured
13.0 ms p95 on the last local run, below its 50 ms gate. `go test -count=1
-cover ./internal/humanwork/productui ./internal/humanwork/workspace
./tools/uxqual/pagedef ./tools/uxqual/render/journey
./tools/uxqual/cmd/journeywasm` passed with 92.8%, 66.3% (existing exact-path
exception), 87.3%, 96.0% and 84.6% coverage respectively. `go test -count=1
./test/workspace`, scoped `go vet`, `npm test`, lint, format and code-style
checks passed. The Go/WASM bundle and isolated local-dev cell were rebuilt on
18080/18443 behind the 18768 gateway. A second adversarial code pass found
no remaining P0/P1 objection.

This is deliberately **not** a completion claim. The Codex browser and
desktop automation runtimes currently fail before initialization with
`failed to write kernel assets: The system cannot find the path specified`
(OS error 3). Their prior browser screenshots predate the final privacy and
History-grant fixes, so desktop/390px/320px/light/dark visual inspection and
manual interaction still have to be repeated before ticking UXAUDIT-015.
No commit or PR is claimed for this checkpoint.

### UIPOLISH live-design checkpoint (001–012, in progress)

The production typography roles now use one fluid semantic scale. A Codex-browser
review of the isolated local-dev cell at 18768/18080 found a concrete Settings
composition defect: the sole Account & security group occupied one track of a
two-track grid, wasting half the workspace and squeezing its content. The
shared settings layout now spans the available measure and places Session &
security and Sign out in responsive, token-spaced regions. Desktop, 390px and
320px Settings were visually rechecked; scrolling the main region left the
header and navigation fixed. No scrollbar/overflow claim is made for other
pages yet.

Theme review found that dark-mode qualification blended an adjusted brand
color that the runtime CSS never rendered. The qualifier and actual dark CSS
now use the same 40% primary/32% hover blends against white, with a regression
pinning the CSS and effective values. Focused product UI tests and vet passed.
Six affected `tools/uxqual` packages passed unit tests and their coverage
floors: tokens 80.3%, page render 93.0%, journey render 95.7%, WCAG 85.1%,
i18n 84.4%, and latency gate 75.0%. A Windows test-binary cleanup warning
followed successful package result lines.

Luna lanes added layout, shape, color, icon, control, density, content and
motion contracts. Integration review returned the icon registry for a mutable
global, density for DOM/visual order divergence, and motion for permanent
compositing hints; those were refined before production wiring. These
contracts are not evidence of completed production pages. UIPOLISH-001–012
remain unticked pending the full live-page, locale, theme, accessibility,
latency and responsive matrix. No commit or PR is claimed for this checkpoint.

The next adversarial pass caught two false-confidence cases before a TODO
could be closed. The typography layer's broad global selectors and its
zero-specificity metadata reset were narrowed to the product shells and
tested against the existing uppercase rule. A new late drawer transition
had accidentally replaced the desktop sidebar width transition; it now
applies only to the mobile logical inline axis, with explicit reduced-motion
overrides. A proposed generic-card shadow and a second async-region component
were removed instead of adding dead or competing visual systems. The small
scroll layer is wired to production without changing People’s page-owned
vertical scroll; it only contains gestures and keeps focused controls clear.

The production Go/WASM bundle was rebuilt behind the isolated 18768 gateway.
In the Codex browser, the settled dark Settings page now exposes proper H1/H2/H3
subsection hierarchy and a full-width account group; the People page was
checked at desktop, 390px and 320px. A mobile drawer opened, closed, and
navigated to People without remaining over the destination. Loading skeletons
appeared during the route change, then resolved to the authorized list; the
main page scrolled while the top bar stayed put. This is a targeted sample,
not the full UIPOLISH-012 browser matrix. The focused UIPOLISH tests, six
foundation-package coverage runs, and scoped vet passed. A new gate test
renders all registered PageDefinitions through production component paths,
but viewport/zoom/theme/keyboard baselines still need live verification.

Final verification for this checkpoint rebuilt the Go/WASM bundle and ran it
through the isolated local-dev cell, then visually inspected People and
Settings in dark desktop and 390px/320px layouts, including German and Arabic
RTL Settings. The navigation drawer opened and closed at mobile width,
software navigation to People dismissed it, the page-owned People list
scrolled beneath the fixed header, and browser error/warning logs were empty.
The updated dark-theme qualifier now also checks production focus, action,
hover, selected-surface and four status pairs in default and tenant themes.
The journey's status/severity icon adapters route through the governed icon
registry; other icon call sites remain to be migrated. Five high-confidence
product-voice keys were localized across en-US, de-DE and Arabic.

`go test -count=1 ./internal/humanwork/productui -run
'^TestTodo_UIPOLISH_'` passed. The full product-UI suite passed at 92.9%
coverage when only the concurrently edited `TestTodo_WEB_173` was excluded;
the unfiltered run failed that WEB-173 test, so an unconditional green claim
would be wrong. The six affected `tools/uxqual` packages passed with coverage
from 75.0% to 95.9%; scoped `go vet`, `npm test`, `npm run check:code-style`,
`npm run format:check`, and `git diff --check` passed. UIPOLISH-001–012 remain
unticked because the requested full production-page and adversarial browser
matrix, and several component integrations, are not yet complete. No commit
or PR is claimed.

The next UIPOLISH refinement pass put the shape contract into the live
stylesheet: standard surfaces now use a boundary without resting elevation,
focused surfaces use the shared focus ring, the global-search panel uses the
same raised token as other popovers, and buttons/selects use the semantic
control border. A production-stylesheet test pins those behaviors. The
isolated server was rebuilt and the dark RTL Settings page and global-search
overlay were checked in the Codex browser at desktop and 320px; the overlay
opened and closed with Escape, and console warnings/errors were empty.

The Journey renderer's remaining direct glyph calls now resolve through its
semantic icon registry, guarded by an AST test over non-test production files.
This does **not** close UIPOLISH-010: the adversarial pass found that customer
substitution currently has only self-mappings, all production calls supply no
pack, and the product-shell icon path is separate. UIPOLISH-003 also stays
open: other surface/hover shadows remain in legacy stylesheet layers and the
full computed-cascade review is unfinished.

For UIPOLISH-008, a proposed second density component was removed because it
was not connected to production and conflicted with the persisted customer
preference. The shared DataTable now consumes that root density in its row
spacing, and narrow-screen sort controls retain a 44px minimum hit target;
the production density test and desktop/390px RTL People checks passed. The
remaining field-priority and object-page density contracts still need
integration. All twelve UIPOLISH checkboxes remain open pending their full
acceptance matrix; this is an implementation checkpoint, not a PR claim.

A further color audit found that the earlier generic high-contrast `:root`
declarations lost the CSS cascade to explicit and system dark-mode selectors.
Dark high-contrast declarations now follow those dark selectors and use
contrast-checked text, surface, border, action and focus pairs. Dark hover now
uses the tenant's admitted hover token in both the runtime CSS and theme
qualifier, rather than silently deriving it from primary. Focused dark/theme
tests pass, including a regression for cascade ordering. High-contrast has
not yet been exercised with an actual forced user preference in the browser.

The product-voice pass changed the awkward English confirmation labels to
"Confirm approval" and "Confirm rejection" and made the Journey action
component select its semantic locale keys for approve/reject/execute. The
standalone workspace presentation adapter now recognizes Arabic and round-
trips its named-month date, number and explicit-currency formats; its
promotion field/action corpus has Arabic translations. Scoped workspace
UX-004 and Journey renderer tests pass. Hardcoded English remains in the
legacy standalone Journey People panel and some diagnostic sections, so
UIPOLISH-007 is not closed.

The async lifecycle contract gained optional operation correlation that
rejects stale load/mutation results after a newer start; scoped tests and
vet pass. Production callers still need to pass operation IDs before this
protects real UI requests, so UIPOLISH-009 remains open. The product-UI
suite passed with `TestTodo_WEB_173` excluded (92.9% coverage), and focused
UIPOLISH, Journey, workspace-locale, latency-gate and vet runs passed. One
prior unfiltered product-UI run failed the concurrently edited WEB-173 test;
that separate lane is not claimed fixed here. No UIPOLISH checkbox, commit or
PR is claimed by this checkpoint.

The app's saved “More contrast” preference exposed a second cascade path:
the prior dark override only listened to the operating-system media query.
Explicit dark and system-dark modes now reassert the dark high-contrast
palette when `data-hcm-contrast="more"` is selected in Settings. The isolated
18768 server was rebuilt, and in the Codex browser the setting was saved,
reloaded, and visually verified as black surfaces with white text and
stronger borders. The demo account was returned to “Use system setting” and
that restoration was verified after another reload. This validates the live
app preference path, not the full UIPOLISH-005 theme matrix.

This checkpoint's scoped product-UI suite passed at 92.9% coverage with the
concurrently edited WEB-173 test excluded. Workspace passed with the
separately failing UX-002 golden excluded; Journey renderer, page renderer,
and latency-gate suites passed without exclusions. Scoped `go vet`, Prettier,
code-style, and `git diff --check` passed. The worktree still contains extensive
parallel WEB and promotion edits, so no mixed commit, checkbox closure, or
PR is claimed.

An adversarial review of the saved-contrast fix found that its stronger
selector could beat the later print and forced-colors palette. Matching-
specificity print/forced-color overrides now come last, with a regression
test for both branches. Ordinary Journey notices, cards, panels and buttons
were flattened to boundary-based surfaces; hover no longer lifts journey
cards, while focus retains the shared ring. A Journey stylesheet regression
test pins those semantics. The async-region model now rejects a completion
with no operation ID when a correlated operation is active. Journey,
latency-gate and focused theme tests pass; the rebuilt isolated v11 Settings
page was visually checked in the Codex browser. `npm test` passed. Forced-
colors media itself and the newly generated Journey WASM have not yet had
browser-level visual verification, so these items remain open.

A further ordinary-surface pass removed the preview mini-page shadow and the
search field's inset decoration, leaving the raised token on the search
overlay itself. The focused shape test passes, and the isolated v12 server's
settled dark Journeys page was inspected in the Codex browser with no browser
warnings or errors. The displayed Journey WASM still predates the latest
Journey-source elevation changes, so that screenshot is not evidence for
those rules. The async retry event now accepts a new operation identity and
rejects a late completion from the failed attempt; its fault regression
passes. UIPOLISH-003 and -009 remain open.

During parallel suite execution the Journey 10,000-worker preview latency
gate spiked to 21.8 ms p95 against its 16 ms budget. Rerunning the gate alone
produced 3.0 ms p95 and the full Journey package then passed in isolation.
That discrepancy is recorded as contention-sensitive gate evidence, not
silently counted as an unconditional parallel-matrix pass.

A follow-up responsive check used the Codex browser's temporary viewport
override at 390 and 320 pixels. Its captures showed a page surface narrower
than the requested image width, while resetting the override returned a
full-width settled responsive page. That discrepancy makes these override
captures unsuitable as conclusive 390/320 acceptance evidence; the viewport
was reset. The broader UIPOLISH-012 browser matrix remains required.

UIPOLISH-006 cleanup removed a draft Journey `Control*` helper and its tests:
no production caller used it, it had no CSS integration, and its compact
target advertised 32px despite the declared accessibility minimum. The
production compact control height is now 44px; compact density continues to
affect spacing without shrinking pointer targets. Focused product-UI control
and visual-token tests pass. The full control/state matrix remains open.

The adversarial pass found that default-density links and icon actions still
fell below 44px. Production history sort, Studio back link, page-size select
and apply button now have 44px minimum height; narrow-header search, action
launcher and utility drawer triggers now reserve 44px in both dimensions.
The WEB-047 mobile stylesheet golden was intentionally updated. Focused
UIPOLISH-006, WEB-047 and visual-token tests pass. An isolated v13 build was
visually checked on the mobile Journeys layout and reported no console
warning/error. The browser viewport override still produced a narrower
capture than requested, so that inspection cannot certify exact target
measurements or close UIPOLISH-006.

The next UIPOLISH-006 adversarial pass found more undersized interactive
targets. History navigation, desktop and mobile navigation toggles,
permission checkboxes, organization view options, menu search/apply,
favorites, ownership disclosure summaries, and subnavigation links now
reserve at least 44px in their interactive dimension(s). The menu filter
field grew with its submit target rather than letting the button overlap
entered text. Focused control, mobile-shell, UXAUDIT-003, WEB-047 and visual
token tests pass. The full product UI suite (skipping the separately owned
WEB-173 test) passed at 92.9% coverage before the final menu-size patch; the
focused suite passed after it. An isolated v14 server was checked in the
Codex browser on desktop and with a phone viewport override: the drawer
opened and exposed its controls, but the override still crops/scales the
capture, so it is not pixel-size acceptance evidence. The signed-in persona
has no organization-page access; direct navigation correctly showed the
access-denied page, and organization visual acceptance is still pending.
No UIPOLISH checkbox is marked complete and no partial TODO is committed.

A second cascade audit then found later 36–40px overrides on the People sort
links, subnavigation, locale choices and pager buttons, plus a text-sized
menu-filter clear link. Those production affordances now hold the 44px
minimum; focused UIPOLISH-006, People, WEB-047, mobile-shell, UXAUDIT-003 and
visual tests pass after the change. The isolated browser viewport was reset
and its desktop layout rechecked. A later full product UI run and direct
per-element computed-size/browser matrix are still needed before closure.
The post-change product UI suite subsequently passed in 103 seconds at
92.9% coverage with the separately owned `TestTodo_WEB_173` excluded;
scoped `go vet`, the Journey/latency/page/i18n/WCAG/token package suites,
`gofmt -l`, and `git diff --check` also passed.

The last target-size review found `.button.compact` at 34px on real
Organization, role and support actions and the embedded Journey technical
summary at 40px on mobile. Both now use 44px; the compact button text also
uses a readable .8125rem rather than .72rem. Focused UIPOLISH-006 and related
mobile/People regressions pass after rerunning with the repository-local Go
temporary directory (the first test process reported success but Windows
returned an access-denied cleanup error on its system-temp executable).
`npm test`, `npm run format:check` and `npm run check:code-style` pass. The
isolated v15 server's settled People page was visually inspected in the
Codex browser with its 3 authorized rows, filter controls and table; no
claim is made for denied Organization content or the stale Journey WASM.

UIPOLISH-012's production-page gate now runs every registered page through
de-DE and Arabic RTL rendering, rejecting unresolved locale markers. A new
per-page state matrix checks the loading fragment's main landmark and stable
geometry contract, then renders a backend-error path and rejects leaked raw
error text. Both focused gates pass. This extends the automated matrix but
does not satisfy its required 1440/390/320, zoom, theme, keyboard, and
screen-reader browser review; UIPOLISH-012 remains open.

The UIPOLISH-006 target regression now checks every generated rule for each
audited interactive selector, rejecting later sub-44px `min-height`
declarations rather than accepting an earlier 44px base rule. Its focused
suite passes. This still does not substitute for browser-computed target
measurement across the full component inventory.

The final full product UI `go test` run printed `ok` with 92.9% coverage but
the Go wrapper exited nonzero when Windows denied deletion of its temporary
test executable. To separate that cleanup issue from test assertions, the
instrumented test binary was compiled to a unique repository artifact and
run directly from `internal/humanwork/productui` (needed by one relative-file
audit). It exited 0 with `PASS` and 92.9% coverage, skipping only the
separately owned failing `TestTodo_WEB_173`. Browser console warning/error
log remained empty on the inspected People page.

The next People-row accessibility pass found that an unavailable workflow's
specific server-resolved reason was visible in its popover but absent from the
Codex browser accessibility tree while closed. Two attempted description
mechanisms (`aria-description` and a reference into the closed disclosure)
were rejected by live browser checks. The production Go component now puts
the reason in the summary's localized accessible name, while retaining the
compact visible `Unavailable` trigger. A separate localized generic name
avoids claiming that a fallback supplies a specific reason. The en-US,
de-DE and Arabic SSR tests, real browser AX check, Enter activation, Escape
dismissal and visual popover check pass. Rendering 100 unavailable rows
measured 4.17 ms/op in the local Go benchmark. A read-only adversarial
review found no new privacy exposure. The Codex browser's temporary 320/390
viewport override again produced a narrower capture than requested, so it
is not accepted as a breakpoint-size gate. No UIPOLISH checkbox is closed on
this one control fix.

After the fallback-copy adjustment, the full product UI test binary again
exited 0 with `PASS` (excluding only separately owned `WEB-173`), scoped
`go vet` and `git diff --check` passed, and the Go/WASM bundle plus integrity
manifest were rebuilt with a dedicated build executable to avoid the Windows
`go run` temporary-file cleanup race. The isolated preview server is running
the new bundle on port 18768. A fresh admin sign-in displayed 64 authorized
people and a 20-row page; the dense desktop table, separate navigation scroll
and an ineligible worker's accessible explanation were checked directly in
the Codex browser. The screenshot is a visual spot check, not the remaining
full UIPOLISH-012 viewport/theme/zoom/keyboard matrix.
Running `TestTodo_WEB_173` alone still fails in the concurrently edited
performance-review page (`performance-review workspace invents review data:
"section:"`). That unrelated test is explicitly excluded above; the branch
cannot claim an unqualified product-UI suite pass until its owner resolves it.
The admin People directory was additionally inspected in German and Arabic
RTL through the real locale switcher. Both retained the title/filter/table
hierarchy, and the unavailable-action accessible name carried the localized
specific reason. English was restored afterward. A browser zoom keystroke
attempt did not provide a verifiable zoom-level signal, so 200% zoom remains
unqualified rather than inferred from an unchanged screenshot.
The desktop People scroll-owner check used the real 20-row admin table:
scrolling the content kept the column headers pinned to the table top and
left the nav position unchanged; scrolling the navigation separately changed
only its menu contents while the table stayed at the same rows. This is
positive direct-browser evidence for part of UIPOLISH-004, not a keyboard,
touch, RTL-horizontal, overlay or full-page acceptance pass.
On the admin Home page, the `Recent people` helper text visibly touched the
first row's top divider. A regression first failed against the actual panel
markup; the panel now gives that intro its own margin-free, token-padded slot.
The focused test passed, the Go/WASM bundle and isolated server were rebuilt,
and a direct before/after Codex-browser screenshot showed a clean gap and
natural two-line wrap. The rest of Home's density and empty-state hierarchy
still needs the page-wide UIPOLISH review.
A read-only adversarial follow-up found no concrete regression in that
narrow Home fix; it specifically checked logical RTL padding, bounded density
tokens, localized description copy and selector scoping. It did not promote
the SSR selector assertion into a geometry guarantee.

UIPOLISH header-language accessibility check: the isolated live Go/WASM UI
visually showed English, Deutsch and Arabic in its header popover, but the
Codex-browser accessibility tree exposed those three links as URLs without
names. The Settings language panel exposed the same choices with names.
A red regression across en-US, de-DE and Arabic now requires explicit localized
names and language metadata on the header links. The renderer passes after
adding `aria-label`, `lang`, `dir`, and a visible language span; the Go/WASM
bundle and isolated server were rebuilt. However, the browser tree **still**
reported unnamed header links after opening and settling the popover. The
read-only adversarial review confirmed that the Go render/helper/popup path
preserves the props and could not establish whether this is a runtime DOM or
accessibility-tree issue without live DOM inspection. This is not counted as a
completed UIPOLISH-006 or -007 accessibility gate; do not mark those TODOs
until the live tree names the choices. The same browser visually showed the
Settings page retaining its hierarchy and named in-page language choices.
A second live trial wrapped the header choices in the same named list pattern
as Settings. The browser recognized the list but still gave its links URL-only
names, so that extra structure was reverted; it did not address the defect.
The current product UI binary passes the full package suite with only
`TestTodo_WEB_173` excluded. That independently owned concurrent test still
fails (`performance-review workspace invents review data: "section:"`).
`go vet ./internal/humanwork/productui`, gofmt and `git diff --check` pass.
No UIPOLISH checkbox was advanced by these partial checks.

UIPOLISH follow-up on the isolated 18768 preview: the product shell's browser
history and global-search controls already used governed decorative SVGs, but
the sidebar menu-filter submit button still embedded a Unicode search mark.
It now requests the semantic `search` icon from the closed product registry,
with an 18px optical size and its existing localized button name intact.
The dark desktop Codex-browser screenshot and accessibility tree showed a
recognizable glyph, a named filter field, and a named Apply button. A direct
trial removing the nested span from the header language links did **not**
resolve their URL-only names in the browser tree, so that trial was reverted.
The header-language accessibility finding remains open rather than being
papered over by the passing server-rendered test.

The Settings account group no longer stretches the Sign out utility into a
mostly empty card beside the taller session context. Its two-column and
single-column tokenized layout was preserved, and the settled dark desktop
page was checked in the Codex browser. Focused UIPOLISH tests passed in all
seven relevant Go packages when run sequentially; the first parallel run
exhausted local Go linker space and provided no assertion result. The three
shell/search goldens were intentionally repinned for the semantic SVG markup,
and their focused regressions passed. Scoped `go vet`, gofmt and
`git diff --check` passed. A read-only adversarial review found no concrete
regression in the icon or spacing changes. This is partial UIPOLISH-002/010
progress, not completion of their full viewport/theme/keyboard matrices.
The full product UI package subsequently passed in 115.6 seconds with only
the separately owned `TestTodo_WEB_173` skipped. `npm test`,
`npm run format:check`, and `npm run check:code-style` also passed. No file
was staged or committed in this shared-index pass.

The next UIPOLISH-010 pass found two more raw navigation glyphs: Unicode
favorite stars and the submenu chevron. Both now use closed semantic SVG
paths from the product icon registry, with decorative accessibility semantics.
The favorite link still changes its localized action name between add and
remove, while a class selects the filled visual state; the native disclosure
retains its keyboard behavior. The open-state chevron also gets an explicit
RTL transform instead of losing the mirror to the stronger open selector.
The rebuilt Go/WASM preview was checked in the Codex browser with outlined
and filled stars on Home/People and collapsed/expanded groups in Arabic RTL;
the preview locale was restored to English. A second read-only adversarial
review found no concrete regression. The intended shell/navigation goldens
were repinned and their focused tests passed. The full product UI package
passed in 116.1 seconds with only the separately owned `TestTodo_WEB_173`
skipped; `go vet`, gofmt and `git diff --check` passed after final formatting.
UIPOLISH-010 remains open because customer icon-pack substitution is not yet
connected to production render paths, and the full cross-page glyph audit is
not complete.

UIPOLISH-001/007 follow-up: a live German Settings review exposed clipped
sidebar group names ("Meine Aufgaben" and "Administration"). The shared
group-summary label now wraps at word boundaries rather than ellipsizing;
the rebuilt dark desktop page visibly shows both complete names without
displacing their disclosure chevrons. A focused typography regression pins
the scoped wrapping rule. The eight Settings account/session messages now
come from semantic locale catalog keys in en-US, de-DE and Arabic, and the
three settled browser views showed their respective page copy. The first
broader product UI run caught an unsafe identity fallback introduced during
this pass: it would have printed a raw principal when the authorized viewer
name was missing. That fallback was removed, and the focused privacy and
Settings identity regressions pass. A read-only adversarial review found no
remaining concrete blocker in these changes. Full locale/zoom and cross-page
voice coverage remain open; no UIPOLISH checkbox was advanced.
After removing the unsafe principal fallback, the full product UI package
passed in 112 seconds with only the separately owned concurrent
`TestTodo_WEB_173` excluded. Focused `TestTodo_UXAUDIT_006_Regression`,
`TestTodo_UIPOLISH_007_SettingsIdentityFallback` and typography tests passed;
`go vet`, gofmt and `git diff --check` were clean. The isolated 18768 server
was rebuilt for the live German verification and remains available.

Another UIPOLISH-010 pass replaced the Organization team disclosure and Work
row Unicode arrows with decorative semantic SVGs. Organization now uses the
same governed `expand` path as sidebar groups; both directions mirror in RTL.
The Organization flat view was manually opened in the isolated Codex browser:
the new chevron remained aligned, rotated on expansion, and exposed the
authorized member list. A read-only adversarial review caught a mobile grid
regression from making the Work row's final child an SVG. The refinement gives
ordinary and status-projection rows explicit three-track placement at 421–760
px and two-track placement at 420 px and below; the latter uses the same
layout in My Work and person-level active workflows. Focused semantic-icon,
responsive-selector and organization tests pass, as do `go vet` and
`git diff --check`; the reviewer found no remaining cascade defect. The live
Work queue currently has zero rows, so a populated Work-row visual check is
still due and UIPOLISH-010 remains open.

The next icon audit found raw dropdown/disclosure glyphs in the workspace
context switcher, delegation selector, sensitive-profile details and People
workflow menus. Those controls now request the governed `expand` SVG, keep
their existing text/ARIA names, and rotate for closed/open states; the
sensitive-profile arrow also mirrors in RTL. The People workflow option now
falls back to its visible label if the server projection leaves the explicit
accessible name empty. Directly rendering the reusable `PeopleRow` surfaced
a missing `Cells` prop that caused a server-render panic; the component now
passes its aligned cells to the shared data-table row renderer. Golden and
focused icon tests were updated and pass. After rebuilding both the Go/WASM
asset and the isolated backend, the Codex browser showed the new People
workflow chevrons in their closed/open states. Its accessibility view still
displayed the open option as a URL-only link despite the rendered component
test's nonempty `aria-label`; this needs an independent assistive-technology
check before the accessibility gate can be considered green. Read-only
adversarial review found no high-severity CSS or semantic regression. The
earlier full product UI suite passed with only `WEB-173` and the isolated
latency gate skipped; a new broad run after the latest icon changes is in
progress. No UIPOLISH checkbox was advanced.

UIPOLISH-008 profile-density follow-up: the shared ProfileFact component no
longer renders a redundant "Available" badge beside every present value.
Its exact upstream state remains in `data-fact-status`, and missing, unknown
and withheld values keep their differentiated, localized explanations. This
removes the visually noisy repetition on the live Marisol profile without
changing the authorization projection. A four-state component regression
passes, the rebuilt Go/WASM page was visually checked in the Codex browser,
and a read-only adversarial review found no concrete privacy or semantic
regression. The larger UIPOLISH-008 page-density matrix remains open.
The same shared WorkflowLauncher now omits the redundant "0 available"
badge when the localized empty state already says no workflows are
available. Positive result counts and the filter remain unchanged; focused
zero/positive tests pass and adversarial review found no concrete regression.
The final rebuilt Codex-browser profile showed the zero-count label gone
while retaining the specific ineligibility reason. The full product UI
package passed with `WEB-173` and the interaction-latency gate excluded;
the latency gate passed separately under an unloaded run. The workspace
package passed with its separately owned `TestTodo_UX_002_Golden` excluded;
that legacy promotion-document golden currently differs in the shared dirty
worktree and was not repinned as part of UIPOLISH. The product UI rerun after
the zero-count adjustment passed. `go vet` and
`git diff --check` are clean. The browser's URL-only accessibility-tree
presentation for the People popover link and the full viewport/theme/locale
matrices remain explicit blockers to closing UIPOLISH-006/008/010/012.

A further UIPOLISH-010 refinement replaced the raw privacy-circle bullet on
the person profile with a direction-neutral decorative lock from the governed
icon registry. The profile golden was repinned for that intentional markup
change; focused registry and golden tests passed. After rebuilding Go/WASM
and the isolated server, the Codex browser showed the lock aligned inside its
existing circle in the closed Personal information disclosure. A read-only
adversarial review found no accessibility, RTL, safety or geometry issue.

On the same live profile, missing facts repeated "Not reported" as a value
and "Not supplied" as a badge. UIPOLISH-008 now omits that badge only when
the value exactly equals the active locale's not-reported stand-in; alternate
missing values, unknown verdicts and withheld values keep explicit badges.
The upstream projection and `data-fact-status` remain unchanged. Unit tests
cover English, German, Arabic and an alternate missing value. The full product
UI suite passed in 88.6 seconds with the separately owned `WEB-173` and
unloaded latency gate excluded; after another Go/WASM and server rebuild, the
Codex browser showed the de-duplicated facts and lock together. A read-only
adversarial review found no concrete status or privacy regression. Other
UIPOLISH acceptance matrices remain open; no checkbox was advanced.

The follow-up UIPOLISH-008 density pass now keeps two or more genuinely
unreported fields in a native, closed-by-default disclosure within the
shared EmploymentDetails component. Present, unknown and withheld facts lead
the section; a single missing fact remains inline and retains source order.
The disclosure only groups values that exactly match the localized
not-reported stand-in, so an alternate missing-value message is not
mischaracterized. Its count is localized in en-US, de-DE and Arabic, the
chevron is governed and mirrors in RTL, and native summary focus has a visible
ring. Focused component and localization tests pass. The Go/WASM asset and
isolated server were rebuilt; the Codex browser showed Marisol's employment
and organization sections shorter and let the user expand the preserved
fields by clicking the summary. A read-only adversarial review found no
authorization, accessibility or RTL blocker and surfaced the alternate-value
case, which was tightened before the final rebuild. The broad product UI
suite passed in 117.4 seconds with the independently failing `WEB-173` and
unloaded latency gate excluded; UIPOLISH-008 remains open for its full
table/form/object, responsive, persistence and performance matrix.
The workspace suite passed with its existing `UX-002` golden excluded; the
isolated interaction-latency gate, product UI vet and `git diff --check` also
passed after this change.

Another UIPOLISH-010 pass found the workflow launcher card still using a
literal northeast arrow. The shared card now requests a decorative `launch`
SVG from the same product icon registry; the card's localized Start action
remains its accessible name. The governed profile golden was updated for the
intended markup change, and focused icon and golden tests passed. The rebuilt
Codex-browser Isaac profile shows the mark aligned in its existing icon slot
beside Promotion; the accessibility tree still exposes the Start Promotion
link by name. A read-only adversarial review found no geometry, RTL or
semantics regression. The wider ad-hoc glyph audit and full UIPOLISH-010
matrix remain open.
After this icon change the product UI suite passed in 85.7 seconds with
the separately owned `WEB-173` and unloaded latency gate excluded; the
workspace suite passed with the existing `UX-002` golden excluded. The
interaction-latency gate passed separately, as did product UI vet and
`git diff --check`.

The remaining People row-option AX concern was retested on the live rebuilt
directory. The visual option reads “Promotion · Frequently used,” and the
component render includes both visible text and `aria-label`, but the Codex
browser accessibility tree still reports a URL-only link even when it has
keyboard focus. Wrapping the text in a child span did not change that tree;
the experiment was reverted and the Go/WASM asset/server rebuilt back to
the tested markup. This is not claimed fixed or a passing assistive-technology
gate; it needs a grounded browser/AT diagnosis before UIPOLISH-010/012 close.
All six focused `TestTodo_UIPOLISH_*` tool-package suites (tokens, journey
render, page render, i18n, WCAG and latencygate) pass; `git diff --check`
remains clean. The UIPOLISH TODO checkboxes are still open because the
production browser/viewport/theme/accessibility matrices are not complete.

The next UIPOLISH-004 pass named the production action launcher, utility
drawer, and People workflow options as contained overlay scroll surfaces;
the shared rule already covered semantic dialogs but missed the workflow
popover. The focused scroll tests pass. In the rebuilt Codex browser, wheel
scrolling the dark-mode launcher moved its result list while the People page
and sidebar retained position. This is direct desktop evidence, not the full
wheel/touch/RTL/mobile/performance scroll matrix.

That check exposed an unrelated UIPOLISH-005 failure: hovering a launcher
result turned its white label dark navy on a dark surface. The embedded
Journey WASM stylesheet applied an unscoped global `a:hover` rule after the
product sheet. The Journey base rules for links, focus, headings, resets,
SVG/media, controls, print, and reduced motion are now scoped to `.jn-page`
or `.jn-embedded`; only the standalone document body receives its zero-margin
rule. The stylesheet regression test rejects these unscoped selectors, and
the Journey package suite passes. After rebuilding Go/WASM and the isolated
server, the Codex browser showed the previously failing My Work hover text
readable in the same launcher position. An adversarial review found the
broader selector leakage after the first link fix and that feedback drove
the second scoping pass. The full UIPOLISH-005 theme/contrast matrix remains
open; this is a verified dark-mode defect fix, not a completion claim.

For UIPOLISH-009, a read-only audit found that the standalone Journey WASM
entrypoint did not install the finite-work scheduler used by the product
shell. Its fallback `Async` path also ignored `KeepExisting`, so two rapid
submissions could issue the same non-idempotent RPC twice. Standalone now
uses the bounded four-running/64-queued task lane, and scheduler-less native
embeddings retain a per-key in-flight mutation fence until work completes.
The new native tests cover proposal, employee creation, execute, decision,
independent keys and retry after completion; the Journey client/taskmux suites
pass and the WASM asset builds. The broader async-region OperationID wiring
and same-route response-order coverage remain open. A full Journey client
run first exposed legacy assertions for the old refusal titles; those now
expect the current localized copy and the full suite passes.

The UIPOLISH-004 scrollbar follow-up moved the shell's standard and narrow
navigation rail widths from inline pixel values into named CSS size tokens.
An adversarial pass identified that tables and overlays were missing from
the common treatment; they now share the theme's scrollbar size, thumb and
track tokens, with a focused regression test over all named owners. A second
adversarial pass caught the generic rule overriding the dedicated narrow nav
rail, so navigation was excluded from that generic rule. A third pass caught
forced-colors inheritance failing for direct overlay scrollbar colors; the
shared owners now receive direct `ButtonText`/`Canvas` overrides in that mode.
The final adversarial check found no regression introduced by this diff.
The existing token/forced-colors navigation regression passes, and the
rebuilt isolated product server at 18768 was visually checked on the settled
dark People page. At a short desktop viewport, a wheel gesture over the
sidebar moved its navigation list while the People title stayed put; a wheel
gesture over the page then moved People to its mobile-style row region while
the sidebar stayed put. The normal viewport was restored. No RTL, touch, or
scroll-performance completion is claimed from that appearance check; the
UIPOLISH-004 checkbox stays open.

The next UIPOLISH-009 pass found that a same-route watch response could
arrive after a newer mutation response and replace the detail and its success
notice. The Journey client now serializes detail/notice publication and
rejects older detail by durable instance version, then by timestamp when
versions tie. A first executed instance wins over a pre-execution proposal
even if clocks disagree; a different nonempty instance ID fails closed.
Native regression tests cover out-of-order versions, timestamps, the first
execution transition, and interleaved publication. The focused Journey
client, task scheduler, latency-gate and render suites and vet pass; a
separate Go test invocation reported an access-denied unlink during temporary
binary cleanup after all six UIPOLISH package results were `ok`. The rebuilt
Go/WASM bundle was opened in the Codex browser at 18768: the server-backed
Journeys list and Aya request detail resolved from their loading regions
without losing shell context. This visual check does not inject response
reordering or establish the full latency, empty/error, or reduced-motion
matrix, so UIPOLISH-009 remains open.

The UIPOLISH-003 follow-up removed one bespoke search focus shadow and three
fixed status/count radii. Search focus now uses the platform focus-ring token;
the chips derive from a status-radius alias of the validated customer control
radius. The first attempt referenced an undefined cross-package token; an
adversarial read-only review caught that before handoff, and the refined test
requires the production alias and all three consumers. The second review had
no concrete objection. The focused shape and theme-golden tests, Journey
render and token suites, and vet passed. The whole productui package run
failed in the separately known `TestTodo_WEB_173` fixture (`section:`), not
in this shape test. The isolated v54 Go server at 18768 was visually checked
on dark Home and Journeys, including status chips, settled cards, loading
transition and keyboard-style search focus. The full shape/theme/viewport
matrix remains open, so UIPOLISH-003 is not yet checked.

The next UIPOLISH-010 pass removed literal checkmarks from the shared
activity-row and Myself read-only components. Both now use the registered
decorative check glyph at measured optical sizes, while their adjacent text
remains the accessible meaning. A native regression checks the registered
path in both rendered components and rejects a text checkmark; the focused
test passes. An adversarial read-only review found no concrete visible or
AX regression. The first isolated v55 browser view was later found to use a
stale embedded WASM bundle, so it is not visual evidence for the change.
After rebuilding the Journey WASM assets and v58 server, the populated dark
Myself page showed the registered icon aligned in its tile, and the AX notice
read the task copy without announcing the glyph. The
larger product icon and customer substitution matrix remains open, so
UIPOLISH-010 is not checked.

The 320px Codex-browser pass exposed a UIPOLISH-008 density problem in the
same read-only notice: a decorative icon consumed the first column while
the essential explanation wrapped into a very tall, narrow strip. At 420px
and below, the shared notice now uses one column, smaller safe padding, and
hides only that decorative icon. The badge stays after the copy in DOM and
visual order. A focused CSS regression passes; adversarial review found no
reading-order or cascade objection. The first v56 browser view also used the
stale embedded bundle; the actual fix was checked after the v58 WASM rebuild
at 320px and 390px. The copy regained full card width and no text or badge
was clipped. The temporary viewport override was reset. This is one verified
responsive density fix, not the full UIPOLISH-008 table/form/object matrix.

The same rebuilt v58 bundle carries a UIPOLISH-007 copy refinement for the
Myself notice: “Request changes through a workflow” explains the view-only
boundary and names the next action. The complete message and badge are
localized in en-US, de-DE and RTL Arabic, with exact-string regressions that
reject accidental English fallback. The Codex browser showed the new copy at
320px in all three locales and at desktop in dark mode; no clipping was seen
and the Arabic reading direction was correct. The broader navigation, status,
date, Money and recovery voice matrix remains open. For future visual checks,
rebuilding only the Go server is insufficient: the `journeywasm` builder must
refresh `internal/humanwork/workspace/assets/journey.wasm.gz` and its manifest
before the server binary is rebuilt.

Another live 320px UIPOLISH-008 check caught the embedded Journeys header
action overflowing its narrow measure: the global Journey button rule forced
`white-space:nowrap` on the long “Choose an employee to promote” link.
The shared page-header action container and button now cap their width and
allow centered label wrapping without changing the desktop button. A focused
production-markup/CSS regression and read-only adversarial cascade review
passed. The v60 Go/WASM bundle was rebuilt and checked in the Codex browser:
the complete action is visible and operable at 320px in English, with the
longer German label and RTL Arabic also contained at the same viewport.
The viewport override and English Home route were restored. This is one
fixed overflow, not completion of the full UIPOLISH-008 density matrix.

The next UIPOLISH-006 pass found a 42px context-switcher trigger and a generic
popover summary with no target-size floor. Both now use a 44px minimum; the
existing locale, notification and People-specific triggers remain compatible.
Focused production-selector and rendered-generic-popover tests pass. An
adversarial read-only review checked the People action-column width, CSS
specificity and existing focus-visible path and found no concrete regression.
The wider control-state and device target-size matrix is still open.

For UIPOLISH-011, the explicit Limited preference still allowed mobile drawer
travel because its final motion layer shortened, rather than removed, the
transition. The final shared layer now uses `transition:none!important` at
the mobile breakpoint. It wins over the WASM controller's ordinary inline
transform transition while retaining the logical open/closed inset state,
including RTL. Focused motion regressions pass. The OS reduced-motion path
remains separate; overlay/async and full browser latency checks are still due.

The v61 mobile drawer pass exposed a keyboard edge case: when the drawer was
opened from its visible trigger, focus remained on that trigger and an
immediate Escape did not close it. The visible `nav-drawer-trigger` now handles
Escape through the shared drawer predicate and the same state toggle as its
click. This is distinct from the legacy mobile controller's hidden
`header-nav-toggle` listener. A focused wiring regression passes. After
rebuilding the embedded WASM and v62 server, the exact click-then-Escape
sequence was retested in the Codex browser at 390px: the menu went from
expanded to collapsed and focus stayed on the trigger. The drawer was also
visually inspected while open. This fixes that keyboard path, not the entire
UIPOLISH-011 motion and overlay matrix.

The v62 focused UIPOLISH test matrix passes across product UI, Journey
renderer/client, latency gate, tokens and WCAG packages; vet and `git diff
--check` are clean. The full product UI package run exposed a stale Myself
assertion from the older read-only copy, which is now bound to the locale key
and passes with the localized copy regression. One broader package failure
remains in the independently developed `TestTodo_WEB_173`: its generic
`section:` invented-data sentinel matches current rendered output. This
WEB-173 failure is not counted as a UIPOLISH pass and needs its owning
review-workspace lane to reconcile the fixture or actual page data.

The restored desktop Home view still shows an avoidable empty band beneath
the attention card while the activity and quick-start rail determines the
height of the first two-column row. It remains an open UIPOLISH-002/008
composition finding; no speculative negative-margin or DOM reorder was
applied in this pass because responsive reading order and customer floorplan
configuration need to be preserved.

The v63 Go/WASM build resolves that Home finding with two independent
semantic rails. Actionable work, start, drafts, tracked requests and recent
activity stay in the primary rail; overview and recent people stay in the
supporting rail. The old full-width secondary grid and its CSS margin chain
were removed. In the Codex browser, the rebuilt Home view no longer has the
empty first-row band on desktop, and the cards form a readable single column
at 320px. Navigation remains fixed while the page content scrolls. This is a
verified Home refinement, not a pass for every UIPOLISH-002 page and mode.

The shared target-size pass raised utility-drawer controls, session recovery
links, delegation summary, step-up/break-glass/policy links and dismissal
controls, and the mobile profile link to the 44px floor. Focused UIPOLISH-006
selectors and product UI tests pass. The wider interaction-state and browser
target matrix remains open.

An adversarial UIPOLISH-009 check found that an equal-version Journey watch
answer with an absent or tied presentation timestamp could rewind a different
stage and replace a confirmation notice. The detail publication fence now
rejects that unorderable stage change while allowing a same-stage capability
refresh. Response-order regressions and the full native Journey client suite
pass. The existing test fixture gives distinct stage snapshots distinct
timestamps; it does not claim a wall clock can substitute for durable version
ordering. The remaining async-region and latency matrix is still open.

The 320px German Home inspection then exposed English backend presentation
labels in Recent activity: “Promotion journey” and “Recorded” appeared inside
the translated page. The Journey adapter now carries semantic title and
terminal-stage keys alongside its original labels, and the Home and shared
Work projections resolve the keys at render time. A focused product UI test
covers en-US, de-DE and Arabic, and the adapter test verifies the recorded
keys. After the v64 Go/WASM rebuild, the Codex browser showed
“Beförderungsantrag · Erfasst” and “طلب الترقية · مسجل” in the real 320px
activity cards without clipping. The viewport and English Home route were
restored. Other service-supplied nonterminal labels and unrelated pages still
need the broader UIPOLISH-007/I18N audit. The browser's initial loading title
briefly used the unrefined account display name before the final “Rafael”
title resolved. The v65 refinement now holds the locale-specific generic Home
title during cold loading, then replaces it with the admitted worker's
preferred-name greeting. A focused adapter regression passes for en-US,
de-DE and Arabic; the Codex browser observed “Home” during load followed by
“Good morning, Rafael.” after the response. The full async-state gate remains
open.

The v67–v73 History pass began with a live German browser finding: the table
still showed English column names and “Recorded” outcomes, while the
Completed filter excluded both recorded promotions. The product projection now
uses semantic status labels in History and maps Recorded to the Completed
filter category. A browser click through the actual German outcome selector
and Apply filters preserved both rows. Effective dates are formatted as civil
dates, independently of viewer time zone; closed timestamps are localized
instants. Search now includes the localized title, status and dates visible in
the row, while admission, sorting, outcome and year filtering still use
canonical service fields. The adversarial review found and verified fixes for
the non-UTC civil-date shift and visible-text search mismatch.

RTL Arabic History initially exposed several English fallback labels in the
page, filters and record actions. The v70 browser build now shows Arabic
History headings, explanations, actions, column labels and dates. The live
two-record count showed the generic “2 سجل,” revealing that the shared
localizer only implemented one/other plural cases. Exact integer Arabic
zero/one/two/few/many rules and Arabic decimal digits/separators now pass
focused localizer and UI regressions; the v71 browser shows “سجلان.” Arabic
clock digits are kept in an isolated LTR time-zone run for RTL legibility.

At 390px RTL, the v71 History form showed 120px-plus select controls. The
mobile layout was still a flex column inheriting desktop 150px/280px child
flex-bases. It is now a one-column grid with intrinsic control heights; the
v72/v73 Codex-browser screenshots show compact 44px controls and readable
record cards. A focused CSS regression and adversarial cascade review pass.
UIPOLISH-007 and -008 stay open because their full page/state/theme,
accessibility and performance matrices are not yet qualified. Focused
UIPOLISH, History, product adapter, Journey client, renderer, latency,
token, WCAG and localizer tests plus vet and `git diff --check` pass. The full
`internal/humanwork/productui` package remains red on the separately owned
`TestTodo_WEB_173` invented-data sentinel (`web173_perf_review_test.go:42`);
no WEB-173 fixture was changed here.

The v74 responsive History pass found that narrow rows relied on desktop
column headers that were not in view. The shared History row now repeats a
visible localized Change label on narrow screens and gives its Change and
Outcome cells contextual accessible names without duplicating spoken text.
The RTL Arabic page was manually inspected at 390px in the Codex browser;
the effective date, changed job code and recorded outcome remained legible.
A rendered-row regression and focused UIPOLISH-008 test pass. The adversarial
review found no new accessibility or cascade issue. This closes the specific
row-context finding, not the whole UIPOLISH-008 acceptance matrix.

The broader Journey-client run then exposed two promotion-validation tests
that still expected Western numerals in Arabic refusal copy. Their expected
display bounds now use Arabic digits and separators, while the submitted
decimal value stays canonical and exact. The focused UIPOLISH, History and
PROMOUX-007 suites, Journey client, localizer and product UI tests, targeted
vet and `git diff --check` pass. An adversarial review found no new
formatting, range or canonical-value regression in the shared Arabic path.

The next parallel UIPOLISH pass repaired several production contracts:
mobile navigation now has a visible, localized 44px close button inside its
drawer and returns focus to the trigger; the narrow navigation rail restores
its own horizontal scroll owner; generic popover/context controls meet the
44px target and focus contract; History consumes the persisted density choice;
and both Promotion renderers include the shared page-layout stylesheet.
Typography now wraps the desktop fallback wordmark instead of clipping it.
The Journey focus radius and system reduced-motion rules use semantic shape
and instant spatial-state contracts. A dark-theme color qualifier now requires
3:1 for essential boundaries and distinguishes decorative dividers; governed
navigation icons have an actual substitutable variant. The i18n renderer no
longer expands placeholder-looking user values, and the standalone duplicate
Execute test deterministically verifies the client-side fence.

Read-only adversarial reviews also found several tests named Browser, Golden
or Performance that only inspected strings or server-rendered HTML. Those
claims were renamed to their actual CSS/markup/locale-contract scope rather
than counted as browser or visual-baseline evidence. The refreshed Go/WASM
build was manually inspected in the Codex browser at 320px (Home, People and
drawer), 390px (History), and 1440px (People and History). The drawer closes
visibly with focus restored, People sort controls wrap at 320px, and History
stays readable at 390px. The desktop fallback wordmark no longer truncates.
All ten directly affected product UI/renderer/token/WCAG/i18n/latency/Journey
client package tests returned `ok`, as did targeted vet and `git diff --check`;
Windows emitted only its known post-test `unlinkat` cleanup warning. The
full 1440/390/320 × zoom × theme × locale/RTL × state/browser/performance
matrix remains incomplete, so UIPOLISH-001..012 stay open.

The integration rerun also exposed repository-wide policy debt rather than a
UI test failure: importgraph and boundarytests pass, but libfirewall reports
23 third-party import violations across application, domains, telemetry,
product UI and uxqual packages, plus its separate protobuf and OTel direct-
import checks. The scoped UI package suites above are green; this policy
gate is explicitly not claimed as passing.

The next UXAUDIT/PROMOUX wave separated My Work from the lifecycle tracker:
the former now scopes every assignment to the bound viewer, including the
empty-view path, and links to Journeys for tracked promotion requests; the
latter groups authorized promotion requests by review, waiting, follow-up and
closed state without treating every visible request as the viewer's task.
The Codex browser showed a genuine zero-action My Work page and nine grouped
Journeys requests on the rebuilt local server. The browser also exposed dead
Organization-wide density/expand controls: this server-rendered route never
mounted their WASM enhancer. Those unbound controls were removed, retaining
native per-team disclosures; a fresh build/browser retest is still required.

Settings now groups account safety and personal preferences and moves the
account identity before the long preference form. A desktop browser pass
found an awkward oversized profile action; a compact identity row and
proportional action followed, pending a rebuilt visual check. The same pass
confirmed improved Organization summary spacing. Help action copy, Home
tracked-request status, worker-profile missing-value treatment, Insights
empty/security coverage, admin form grouping and navigation search projection
received scoped refinements and tests. Brand-asset validation/picker support
is only a partial slice: durable preference storage and browser-byte upload
are not wired. History remains in-memory paginated despite tests previously
named integration; a separate API/schema lane is addressing the 200-row cap.

An adversarial review found that a warm same-route refresh could show person
A's prior projection while person B's URL loaded. The product WASM path now
guards person/work/history selector changes, with a regression; the canonical
invalidation stream and catch-up endpoint are still absent. Promotion
diagnostics are gated in the renderer, simulation findings are canonicalized,
and a typed effective-date wait explanation exists, but live transport does
not yet populate the wait projection or expose an authorized local-dev
advance action. The UIPOLISH-012 gate now explicitly distinguishes static
route/persona/i18n/accessibility checks from browser, visual baseline, CLS
and interaction-latency evidence. None of the broad UXAUDIT/PROMOUX or
UIPOLISH families is marked complete merely for these partial slices.

The attempted UXAUDIT-019 API expansion did not pass adversarial security or
scale review. The first implementation allowed an unbound worker filter and
unadmitted/non-terminal rows into History, paired a replayable offset cursor
with an unstable comparator, and scanned/resimulated the full intent set per
page. The transport/product client/UI integration was therefore disabled
again; existing admitted, terminal-only in-memory History remains the live
behavior. Query-bound cursor/comparator helpers and generated request fields
remain isolated for future work, but are not acceptance evidence. A real
authorized, bounded durable projection and >200-row PostgreSQL regression
are still required. The v82 rebuilt Go/WASM page confirmed that dead
Organization toolbar controls are absent and native team disclosure expands
to its employee links; Settings now presents a compact identity row and an
`h1`→`h2` profile heading. The same browser pass found Insights counting two
visible reviews as “Needs attention” while Rafael's My Work had zero; the
Insights assignment count was aligned to the My Work projection, pending a
fresh build/browser recheck.

The v85 Codex-browser refinement loop confirmed the Insights attention count
is now zero for Rafael, matching My Work. Its first composition put the
attention panel into a letter-wide column; a second pass left the evidence
labels split mid-word. The final full-width metric/attention/evidence stack
is readable at the live desktop viewport, with scope caveats under evidence
and localized zero-assignment guidance in the action panel. The full product
UI package passed before the last stylesheet-owned Insights layout tweak;
its focused UXAUDIT-018 tests pass, and the final full-package rerun is in
progress. Seven related promotion/client/transport/wait packages passed.
The dev server remains on port 18768, served by the v85 Go/WASM bundle.

The final full product UI rerun is not green: its interaction-latency gate
missed the 10,000-person query p95 100ms budget (103.7ms on the third attempt)
and stable-shell p95 16ms budget (39.8ms on the third attempt) while the intent
application suite ran concurrently. All other product UI tests passed in that
run; the intent application package reported `ok` and only a Windows
post-test executable cleanup warning. A dedicated isolated latency/profile
lane is investigating rather than relaxing the budgets. The v85 browser
visually confirmed the final Insights metric, attention and evidence rows
are readable with no letter-wide labels.

A fresh, isolated full `go test -count=1 ./internal/humanwork/productui`
passed (74.7s) after the v85 refinements. The latency investigation found
repeated page-registry construction in the shell hot path; its initial cache
cut the measured stable-shell p95 to about 5.6–8.5ms (16ms budget) and the
10,000-person interaction to about 17ms (100ms budget), without relaxing
either threshold. An adversarial review found no demonstrated runtime race,
staleness or authority defect, but correctly flagged that package-level
`sync.Once` registry state conflicts with the composition-root rule in
`AGENTS.md`. A value-owned optimization is being worked through before
switching the live server or accepting that change. The Codex browser at its
default narrow viewport showed the full-width Insights stack without the
prior letter-wide labels. An explicit 320px viewport override produced an
unusable scaled capture, so no 320px browser acceptance is claimed. The
duplicate `Needs attention` panel heading/count label was observed; the
localized panel title is now distinct (`Your action queue`, with German and
Arabic counterparts), pending a rebuilt-server visual recheck.

The i18n qualifier now substitutes placeholders only in the original
template, preventing a parameter containing `{date}` from being expanded a
second time; both declaration-order regressions pass. A standalone journey
double-click regression now waits for the second submission to return and
the first RPC to complete before asserting one mutation. The brand-logo
pipeline audit confirms the picker/store scaffolding is not yet a browser
upload: the callback receives only a filename, appearance has no bound
adapter, there is no tenant asset byte transport or durable asset revision,
and the existing static asset route cannot serve tenant proxies. UXAUDIT-022
therefore remains open pending a governed byte upload, tenant-scoped storage,
safe proxy serving, and real browser reload verification.

The first attempt to remove the package-level cache replaced it with an
ephemeral `PageRegistry` rebuilt on every lookup. The strict latency gate and
full package happened to pass in isolation, but an adversarial follow-up
correctly rejected it as a false ownership/performance optimization: no
composition owner retains that value. Integration is now revisiting the
registry owner and hot-path calls before the server is switched from v85;
neither the initial cache nor its ephemeral replacement is accepted as the
final implementation.

The final registry integration is fresh-construction only: it removes
redundant whole-catalog and return-value copies, with cross-API mutation
isolation tests and no package-level state. The isolated strict latency gate
and plain full product-UI package pass; the full coverage-instrumented run
does not pass its wall-clock checks (10,000-person p95 141–156ms against
100ms, stable shell p95 21–42ms against 16ms), despite 93.2% coverage.
Coverage and latency therefore need separate mandatory, non-overlapping
quality executions with unchanged budgets; this gate work is in flight.
No retained-cache speedup is claimed. A promotion simulation validation
regression also now rejects unknown/unspecified severities; durable
finding-replay proof remains open.

The v88 server initially showed stale copy because its embedded WASM had
not been rebuilt after the Go UI change. The bundle and then server were
rebuilt in that order for v89. The Codex browser now shows `Your action
queue` with `Needs attention 0` and the full-width Insights evidence stack.
The Help page was navigated live: it remains task links plus promotion
guidance for Rafael. The new authorized knowledge-search destination cannot
appear for a persona without that published capability, and actual article
queries/support-request submission have no governed backend yet. This
confirms UXAUDIT-013 is still partial, rather than treating its SSR tests as
browser or service acceptance.

The authoritative coverage runner now runs product-UI functional coverage
with a `covergate` build tag and separately requires all four named
wall-clock tests to pass uninstrumented. Budgets, samples and retry limits
are unchanged. The first real `-pkg ./internal/humanwork/productui` run
exposed a tagged-test helper dependency and failed; the fix excludes the
dependent Home performance test only from the instrumented invocation and
includes it in the mandatory uninstrumented run. Two subsequent real
product-UI covergate invocations passed with one package measured above
the 70% floor. Adversarial review additionally hardened broad-pattern
package resolution, exact result reconciliation, JSON pass evidence for
each expected performance test and the narrow Windows test-binary cleanup
exception. Focused covergate tests, product-UI and covergate vet, and
`git diff --check` pass. The repository-wide `-all` gate is not claimed:
an unrelated shared-worktree deletion of `gen/go/hcmnext/model` currently
breaks `go list ./...`. No TODO was marked complete merely from this
single-package gate; the full browser/assistive-technology matrix remains.

The next parallel wave tackled role administration (UXAUDIT-009),
organization policy editing (UXAUDIT-010), same-page subject reuse
(UXAUDIT-012), and a bounded promotion integration regression
(PROMOUX-015). Adversarial review caught a dead role pagination link:
the UI initially emitted `role_page`, but the product route parser discarded
it and the component read a plain search term as if it were a URL query.
The integration now carries a separate role page through route parsing,
canonicalization, admitted-view clamping and focused rendering. The live
Go/WASM page on port 18768 advanced from workers 1–20 to 21–40 and a
Rafael filter reset to one result. The first split layout cramped the
table; the directory now owns the full content width, with the catalog
below it and its create form collapsed. Full `go test -count=1
./internal/humanwork/productui` passed, as did productclient tests,
focused UXAUDIT-009/010 and vet. One focused rerun printed a passing test
result followed only by the known Windows `unlinkat` cleanup warning.

The org-policy lane added a role selector, current/proposed diff, validation
and sticky save, but explicitly shows preview unavailable: the authorized
policy-simulation response is not yet connected. Role administration still
lacks a governed bulk assignment and effective-access preview; no cosmetic
bulk control remains. Neither UXAUDIT-009 nor UXAUDIT-010 is ticked.
The layout-shift lane fenced stale organization-person reuse, with focused
journeywasm tests and vet passing; measured browser CLS remains open.

`TestTodo_PROMOUX_015_Integration` now exercises a real PostgreSQL-backed
composed engine across proposer, refused non-routed finance principal,
assigned approver and denied employee, ending in one ledger event; it passed
after adding a before/after database fingerprint for the refused decision.
Adversarial review kept the scope honest: it calls the engine in process,
uses seeded fixture data and a past effective date, and does not cover the
required browser journey, second approval, future WAIT, stale/duplicate
recovery or multi-viewport/locale matrix. PROMOUX-015 remains open.
The promotion invalidation and effective-date-wait audits likewise found
missing server contracts/durable wait projection, so their renderer/client
slices are not presented as live completion.

The next live iteration of Roles exposed an edge-clipped status line. The
directory now gives guidance, table and pagination their own insets. In
Codex browser on the real server, page 2 displayed `21–40 / 64`, filtering
`Rafael` reset to `1–1 / 1`, and the final spacing was visually inspected.
The org-policy intro initially squeezed four flex children into narrow
columns, making role links unreadable; it was changed to a stacked
explanation, actions and wrapped role chips, then visually rechecked on
the rebuilt server. A temporary 390-pixel browser viewport override
produced a scaled-down canvas rather than a usable layout capture and was
reset; mobile acceptance is not claimed from that attempt. No permissions
or tenant appearance were changed during browser review.

The next integration pass stopped the Luna implementation waves and used one
Sol agent for the Roles & access UX. Before that review, the live Codex browser
exposed a projected-navigation bug: clicking People from a collapsed sidebar
reopened the sidebar. `statefulHrefAtRoute` now carries the collapsed state (and
removes stale collapse when expanded); a regression test and a rebuilt
Go/WASM browser click confirmed the sidebar stays collapsed. The WASM route
loader also now passes a sanitized warm baseline to `LoadWithBaseline`; same-
page person, work, history, organization and journey subject switches clear
route-owned selections before the new authorized projection resolves. The
focused UXAUDIT-012 test passes, and a live global-search Person switch from
Sofia to Rosa resolved to the correct profile. Measured CLS and delayed-
network intermediate-state evidence remain open.

Adding the shared page-layout stylesheet to both fallback renderers exposed
a real workspace CSP drift: the frozen hash still covered only
`tokens.WorkspaceCSS`, while GWC emitted that CSS plus `page.LayoutCSS`.
`stylesheetHash` now hashes `gwc.Stylesheet()` exactly, the CSP test pins the
same bytes, and the workspace document golden was mechanically regenerated.
`go test -count=1 ./internal/humanwork/workspace`, renderer-package tests,
focused WASM refresh tests, scoped vet and `npm run check:code-style` pass.

The single-Sol Roles refinement was rebuilt twice and reviewed in the live
Codex browser. The first pass put the role catalog above the 20-row directory,
expanded cards to full width, and split published grants from 109 preview-only
routes. Visual review found an oversized duplicated authorization hero and
redundant role-detail toggles. The second pass removed that hero, widened
desktop cards to two columns, showed a role definition in one expansion and
kept the grants matrix as the only nested disclosure. Desktop screenshots now
show the create action and first definitions above the fold. At a temporary
390px viewport the role table showed a localized horizontal-scroll cue; two
rightward scroll actions reached its Save controls. The viewport screenshot
still truncates the canvas, so narrow-layout visual acceptance is qualified.
Focused UXAUDIT-009 tests and productui vet pass. UXAUDIT-009 remains open for
governed bulk assignment, per-worker result feedback, and a server-projected
effective-access preview. No authorization settings were changed during QA.

The same single Sol agent then reviewed the actual live Journeys/My Work
split. Journeys already groups promotion requests by lifecycle status; the
concrete defect was My Work's subtitle describing all live journeys while
Rafael's assigned queue was empty. My Work now says it contains decisions and
tasks assigned to the viewer, and Review/Blocked filters have distinct
viewer-scoped empty copy and localized due labels. The v98 Go/WASM page was
manually checked in the Codex browser: both empty filters were distinct,
Track promotion requests opened Journeys, and browser Back restored the
Blocked filter. Focused UXAUDIT-017 tests, vet and the full productui suite
pass. A nonempty assigned multi-persona queue and its urgency/order contract
remain unverified in the browser, so UXAUDIT-017 is still open.

The full productui rerun also surfaced three changed chrome digests after
the navigation-state and composition refinements. The WEB-037/039/040
semantic suites passed; their deterministic digests were repinned against
the current rendered shell, including projected collapse state. A final
`go test -count=1 ./internal/humanwork/productui` passed without the Windows
test-binary cleanup warning on this run.

The profile-identity review used the same Sol agent. The first implementation
correctly refused to elevate legal name but failed live acceptance: Sofia's
worker number was visible in Employment overview while the H1/breadcrumb
showed only her preferred name and the WASM browser title remained generic.
Tracing the production client showed that `ListWorkers` supplies an admitted
whole-record summary with no per-field verdict map. The final helper now
uses that same admitted silent path for the worker number, while a nonempty
partial verdict map requires explicit PRESENT name/number dispositions.
`ResolveDocumentPageTitle` drives SSR and both live WASM title updates, with
a generic loading guard on cross-subject transitions. In the rebuilt Codex
browser, the browser title, breadcrumb and H1 all showed `Sofia · HC-21011`;
the sensitive legal-name section remained closed. A real `productclient.Load`
fixture, focused security/locale tests, scoped vet, a WASM build and the full
productui suite pass after stale generic-title fixture assertions were
updated. UXAUDIT-016 stays open for sparse/withheld live personas and the
narrow/RTL visual matrix.

The next sequential Sol pass addressed the Home hierarchy. In the live
Codex browser, a full-width pale promotion block dominated an otherwise
empty personal-work page, and `Current activity` made Rafael's zero assigned
waits appear to contradict the visible waits on Journeys. The Home patch
keeps the underlying viewer-scoped counts, labels their scope explicitly,
reuses the My Work empty explanation, and makes promotion a secondary,
auto-width action with a 44px minimum target. The v102 Go/WASM build was
reloaded and visually checked at dark desktop; the revised action is
proportional to the card and the status copy no longer conflates personal
assignments with generally visible records. A temporary 390px viewport
exposed a truncated browser screenshot, so narrow visual acceptance is not
claimed. Focused Home tests, productui vet and the full productui suite pass.
UXAUDIT-015 remains open for the complete responsive/continuity matrix.

A click-through of that Home shortcut found a continuity trap: the People
directory silently restored Rafael's saved Care Coordination/Chicago filters,
showing only 2 of 64 people even though the shortcut promised a promotion
candidate choice. The task-specific route now explicitly clears search, team
and location, opens page one, and selects eligible-only. The live Codex
browser showed 37 of 64 eligible workers, with All teams/All locations and
the eligibility filter selected. A distinct shortcut URL initially revealed
a duplicate generic `Find an employee` action; the resolver now emits only
one People shortcut for the Home task. A focused failure-capable regression
passes; the v104 Codex-browser Home recheck showed exactly one People
shortcut, with the eligible-only route retained.

The next Settings pass began from live Codex-browser inspection rather than
stylesheet guesswork. Account & security had one padded card next to a
nearly flush Appearance card; Sign out occupied a lonely half-width cell;
three locale choices used one narrow column; two-option accessibility
groups left a blank third column; and Notifications/Navigation touched card
edges. The single Sol agent made the card geometry consistent, arranged
locale and preference choices according to their actual counts, and kept
narrow fallbacks. Browser screenshots of the rebuilt Go/WASM page show the
desktop composition is materially more balanced. A second review removed
the disabled, full-width Notifications pseudo-action, added a visible and
accessible Current state for Compact navigation, and fixed the Profile and
Appearance links to retain locale, favorites and collapsed navigation. A
live Profile click reached Myself with that shell state intact. Focused
Settings/UIPOLISH-002/WEB-043 tests and productui vet pass; the full
cross-persona, narrow/RTL and persisted-preference matrix remains open.

Final combined verification for the Home shortcut and Settings passes:
`go test -count=1 ./internal/humanwork/productui` passed in 81.3s;
`go vet ./internal/humanwork/productui`, `npm run format:check`,
`npm run check:code-style`, and `git diff --check` passed. No TODO was
marked complete based on these focused/browser checks alone.

### Organization visibility editor refinement (UXAUDIT-010, open)

The single Sol pass took the live admin page from a repeated explanation
and nine-role chip wall to a concise progressive editor. The role list is
one exclusive disclosure, Own-unit and All-unit modes do not show an
irrelevant disabled unit grid, selected-unit modes use the available width,
and current/proposed wording distinguishes viewer-relative Own-unit scope
from enumerated units. Missing data-domain information is not presented as
an asserted denial, and the absent worker preview is not faked.

The first rebuilt Codex-browser review found the Own-unit form still left
a large blank right half, so the mode-only layout was widened. A more
important manual role-switch test then found that Employee self-service's
unsaved ALLOWLIST plus Care Coordination draft appeared on Compensation
administrator. Stable role component keys now isolate those local drafts.
The v106 browser retest confirmed Compensation administrator stayed at its
unchanged Own-unit state with Save disabled, while returning to Employee
self-service restored only its own unsaved draft. Nothing was saved.

Focused organization-policy tests, `go test -count=1
./internal/humanwork/productui` (73.7s), `go vet
./internal/humanwork/productui`, and `git diff --check` pass on this final
version. UXAUDIT-010 remains open: authorized representative-worker preview
is not wired, role switching can still scroll the newly opened header out
of view, and the full responsive/persona accessibility matrix is pending.

### Final promotion-flow pass (PROMOUX-015 and UXAUDIT-002, open)

The live Codex-browser pass started in People, opened Isaac's profile and
existing blocked promotion, then opened recorded Jane and Omar requests from
Journeys. A canonical worker entity reference now links to the admitted
directory worker only within the admitted tenant, so People and Person offer
`Open active promotion` instead of a second `Start` action for Isaac. The
profile no longer calls that active request an available new workflow.
Recorded requests inspect their durable execution evidence instead of
re-simulating against a later worker state, subject to current read
authorization; both recorded detail pages opened with their stages and
history. Organization labels use readable names in the proposal and detail.

The blocked detail now shows its first actual blocking finding immediately
below the hero, omits the misleading disabled action, and keeps exact engine
evidence in Checks and timing. Known pay-band finding codes receive localized
business copy in the prominent banner; unknown codes retain their original
message. The first banner draft linked to a document fragment and scrolled
the entire shell, so the link was removed after live testing. The route back
to the worker profile is labeled as a profile destination. The fresh Amara
proposal form and required-field validation were visually checked without
submitting an employment or compensation decision through the browser.

After the final localized-copy edit, the full productui, productclient,
journeyclient and journey-render package suites passed. The localized catalog
check, scoped vet and `git diff --check` also passed. The Go/WASM bundle and server
were rebuilt in that order; the live v112 Codex-browser retest showed the
localized blocker, retained exact lower-page checks, and a profile-specific
Back label with no document-fragment scroll. This is a bounded UX
improvement, not the PROMOUX-015 completion gate: the legacy direct
`mode=new` bookmark for an already-blocked worker can loop browser Back
through its redirect, one recorded fixture has an effective-date/recorded-time
chronology mismatch, and a browser-driven multi-persona terminal promotion
was not executed in this pass.

### Promotion mechanical recovery pass (PROMOUX-015 remains open)

The real gRPC/embedded-PostgreSQL promotion contract passed the proposer,
finance, manager, effective-date timer and one-outcome path, plus employee
denial, stale request revision, duplicate request key, duplicate timer fire,
duplicate resume and reconnect coverage. A separate one-approval journey
regression initially failed because historical Inspect no longer emitted a
SIMULATED timeline event after switching to durable evidence; it now emits
that event from the pinned proposal identity without re-simulating a recorded
promotion. The two earlier chronology unit tests were preserved.

Adversarial review then found a WorkItem/driver commit gap: the approval
decision can commit before Resume, and replay previously skipped Resume,
stranding a parked instance after a transient failure or process crash.
Both the journey page and direct proposal-decision service now retry Resume
only while the decided node is still on the durable frontier. An injected
first-Resume failure against embedded PostgreSQL proves authorized replay
finishes the promotion and records exactly one terminal outcome.

Promotion admission guards were also never released automatically. The
terminal writer now closes the guard in the same transaction as the ledger,
checkpoint and outbox outcome, for both approved and rejected terminals.
Release reconciles an unconfirmed reservation via the intent's durable
idempotency key, covering a lost post-CreateIntent confirmation. Embedded
PostgreSQL tests prove normal completion, rejection and lost-confirmation
closure. Created workers now advertise and validate their actual durable
revision coordinate rather than a manufactured fixed-corpus coordinate;
direct gRPC tests reject a mismatch and accept the published coordinate.
The admission replay now returns the existing guard's actual ID, so a retry
can confirm that row after a lost confirmation instead of updating a fresh,
nonexistent ID. `Confirm` now refuses an unknown guard or conflicting intent
binding instead of treating an affected-row count of zero as success. A
direct gRPC/PostgreSQL recovery test covers that replay.

This is not a claim of complete Phase 1 promotion execution. The terminal
fact is an outcome event; no consumer that applies its placement and pay
to authoritative worker records was found in this repository. The created
worker table is append-only, so a real intervening-state mutation test
cannot be faked by updating its row. The browser multi-persona completion,
full PROMOUX-015 matrix and downstream observation/reconciliation remain
open. The local focused test and vet evidence is recorded in the turn's
handoff; no live employment decision was submitted through the browser.
Admission and intent creation are still separate commits; a crash between
them or a changed payload using an existing idempotency key and another
effective date can leave an orphan ACTIVE window. PROMOUX-017 tracks the
atomicity/reconciliation gate; this pass does not claim to have closed it.

Verification: `go test -count=1 ./internal/intent/app` passed, as did
`go test -count=1 ./internal/data/promotionguard`, targeted real-server
promotion tests in `./test/workflow`, `go test -count=1 ./test/bootstrap`
(298.948s in its own `.artifacts` temp directory), scoped `go vet`, and
`git diff --check`. The final focused bootstrap rerun after the confirmation
changes passed in 129.551s. The TODO registry was regenerated from 1730
entries and `go test -count=1 ./tools/planning/todoregistry` passed. The
broader drift gate could not run: another lane's current worktree deletion
of `gen/go/hcmnext/model/model_generated.go` makes its Go import unavailable;
that file was not restored or altered in this pass.

### Mainline reconciliation (2026-09-13)

The UX snapshot was committed at `1dc66159` before integration. Repeated
commit-by-commit rebase conflicts made that route unsafe for concurrent work,
so a separate worktree was created at main `09d52f13` and the snapshot was
reconciled as one final-tree commit. The original UX branch and a pre-rebase
backup ref remain intact; the main checkout, including another agent's dirty
files, was not edited. The integrated branch preserves main's verified
approval separation and durable role-policy behavior while retaining the UX
components, copy, and tests from the snapshot.

The full application and bootstrap suites passed against embedded Postgres,
including promotion proposal, independent finance and manager approval,
effective-date execution, and recovery cases. Workspace, product-client,
journey-client, and journey-render suites passed after reconciling stale test
fixtures with the server's current authorization and landing-route behavior.
The Go/WASM bundle and asset manifest are rebuilt from the reconciled source
after the UI package settles. The planning boundary suite still reports a
`domains -> transport` import in main's existing `promoux011.go`; it is not
silently golden-updated as part of this UI change. The broad traceability
suite also reports legacy evidence-contract debt and is not claimed green.

The staged coverage run exposed one integration defect: a stored promotion
summary reused the demo corpus tenant even for a sandbox intent, so the
historical-read policy correctly refused the post-execution detail. The
summary now takes its tenant from the stored intent and rejects a missing
tenant; the cross-tenant unit regression and sandbox proof/recovery tests
pass. The first coverage run also hit parallel Windows Postgres-cache access
and test-deadline contention. Isolated covered runs of role-access storage,
promotion execution, sandbox, and product UI passed; the full staged gate is
retried serially without changing its test set or thresholds.

The tagged coverage build also found two latency tests referring to a sample
constant compiled out under `covergate`. The Context Switcher timing test is
now in a dedicated uninstrumented/race-excluded file; its other semantic
tests remain in the tagged build. Global-search timing uses the same tag
boundary. Both timing tests passed uninstrumented, and the covered product UI
package now compiles with the gate tag.

### Paused promotion work and UX merge (2026-09-14)

The clean UX reconciliation commit `655e12d6` and the paused promotion work
preservation commit `c23b9c0c` were merged in an isolated worktree, leaving
both source commits and the original UX snapshot intact. The paused work's
standalone staged coverage passed twice (15 packages), and its remaining
policy, TypeScript, nested Go and build checks passed independently. The
user explicitly requested bypassing the third redundant hook pass after a
browser-test lint fix and architecture-inventory regeneration; the commit
records that exception rather than claiming a completed hook run.

The combined tree required semantic reconciliation, not a wholesale side
choice. It keeps the UX manager-approver setting alongside the finance-partner
route, uses the worker relationship graph first, and applies the configured
manager fallback only outside that graph. It also removed a duplicate
published-path append, fenced demo pay bands by tenant, classified rejected
promotion decisions before writes, and fixed test helpers that had issued
reviewer credentials for the proposer. The Go/WASM bundle, asset integrity
manifest and architecture inventory were regenerated from the merged source.

Verification on the combined tree: `npm run test:all`; full Go package suites
for `internal/application`, `internal/intent/app`, `internal/platform/execution`,
`internal/platform/sandbox`, `internal/trust/authz`, `internal/humanwork/workspace`,
`test/bootstrap`, `test/workflow`, and `test/workspace`; and the product UI,
journey WASM, and product-client suites. The product UI suite reported `ok`
before a Windows temporary-binary unlink denial. A combined staged-coverage
pass, Linux race CI, and an interactive browser pass were not run in this
reconciliation; they are not represented as green.
