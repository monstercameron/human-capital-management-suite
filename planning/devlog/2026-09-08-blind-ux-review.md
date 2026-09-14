# Devlog 2026-09-08 — blind workspace UX review

## Scope and evidence

Reviewed the running site at `http://127.0.0.1:8768` through the Codex
in-app browser using visible controls, screenshots, and accessibility-tree
observations. No application source was inspected to diagnose these findings.
This is a usability observation log, not proof of a backend defect or a completed
implementation. Issue identifiers below are local review references, not TODO
registry IDs.

The first pass stopped at login. On the second pass the browser was already
authenticated as Rafael and the workspace was accessible; fresh sign-in was not
retested, so the original authentication failure is not declared fixed.

The second pass covered Home, People, the employee workflow menu, Adrian's
promotion form, Journeys, Myself, global employee search, Adrian's profile,
Settings, My Work, and global workflow history. Screenshots were taken at the
current in-app viewport, approximately 876 by 878 pixels during the second pass.
No mobile-width, dark-mode, alternate-role, or comprehensive accessibility audit
was performed. No proposal was submitted, employee created, or preference changed.
No code fixes, automated tests, or commits are part of this review.

## First pass — login findings

### UXBLIND-001 — sample sign-in fails (blocker; needs fresh retest)

- Reproduction: open `/workspace/login`; click each of Continue as Rafael Torres,
  Dominic Collins, Thomas Baker, and Samuel Rivera. Reload and retry Rafael.
- Observed: each attempt remained at login with “That credential was not accepted.”
- Impact: no sample persona could enter the workspace during the first pass.
- Follow-up: verify fresh sign-in for all four personas and confirm the expected
  identity and permissions after entry. The later authenticated session does not
  establish that these buttons now work.

### UXBLIND-002 — sign-in error offers no useful recovery (high; open)

- Observed after a persona-button click: the error blames a credential the user
  never entered. It does not distinguish account availability from a service
  problem and supplies no actionable recovery.
- Expected: plain-language explanation and a safe next step, without exposing
  sensitive authentication internals.

### UXBLIND-003 — login error shifts the layout (medium; open)

- Reproduction: compare initial login with the failed sign-in state.
- Observed: insertion of the error banner moves all persona cards downward.
- Expected: stable action placement while showing and announcing the error.

### UXBLIND-004 — credential disclosure expands below the fold (medium; open)

- Reproduction: expand “Use a bearer credential” after the login error.
- Observed: the actual credential field and Sign in button are below the visible
  viewport; the disclosure initially appears to have done little.
- Expected: make the newly expanded content discoverable and keyboard-reachable
  without an unexplained visual dead end.

### UXBLIND-005 — persona descriptions use implementation language (low; open)

- Observed: phrases such as “governed workflow workspaces” do not explain the
  useful tasks available to that persona.
- Expected: task-oriented descriptions such as reviewing requests or payroll.

## Second pass — authenticated workspace findings

### UXBLIND-006 — promotion has empty required choices (high; open)

- Reproduction: People → Adrian → Workflows → Promotion → Valid next role.
- Observed: the employee-specific page correctly says “Promote Adrian,” but
  Valid next role and Target grade offer only their placeholders. No explanation
  distinguishes an absent ladder, ineligibility, missing setup, or loading failure.
  The general Journeys form also showed empty target-job and grade choices.
- Impact: the attempted promotion cannot progress through the required fields.
- Expected: explicit eligibility/setup/loading state, a recovery route, and no
  misleading suggestion that an unusable form is ready to complete.

### UXBLIND-007 — Journeys combines unrelated tasks (high; open)

- Reproduction: navigate to Journeys and scroll through the page.
- Observed: journey history, an inline promotion form, another employee directory,
  and a New employee form share one long page. The latter is unrelated to reviewing
  a past promotion and duplicates the directory responsibility of People.
- Expected: a focused journey list/detail experience, with deliberate entry points
  for starting a workflow; employee creation belongs in its own clear context.

### UXBLIND-008 — context dropdown appearance promises the wrong interaction (medium; open)

- Reproduction: click “Compensation Review ⌄” on the employee profile.
- Observed: instead of opening a dropdown, it navigates to Settings. Settings
  displays Purpose / scope but offers no corresponding context-switch control.
- Expected: either a genuine context menu or a clearly labeled link to access
  information; do not imply an unavailable switching action.

### UXBLIND-009 — employee names are insufficiently identifying (high; open)

- Observed: People, global search, profiles, and promotion headings primarily use
  first names such as Adrian and Rafael. Worker numbers exist in some detailed
  views, but full names are not prominent at the decision points.
- Impact: a larger workforce makes mistaken employee selection more likely.
- Expected: full display name plus useful secondary identity/context before any
  employment action. Retest with duplicate first names.

### UXBLIND-010 — promotion requires insider knowledge (medium; open)

- Observed: Target position is a raw ID input with an example such as
  `POS-HRBP-301`, without a discoverable position chooser. Supporting text refers
  to a “ladder edge,” ledger evidence, and a gate admitting execution.
- Expected: searchable valid positions and business-language explanations of
  validation, approval, simulation, and the point at which a change takes effect.

### UXBLIND-011 — employee layouts are inconsistent and hard to scan (medium; open)

- Reproduction: inspect People at the current viewport, then the employee table
  embedded in Journeys.
- Observed: People switches to tall cards with labels and values separated across
  broad spaces; few employees fit onscreen. Journeys instead presents a dense
  horizontally scrolling table whose right-hand columns are offscreen.
- Expected: a consistent responsive directory treatment with readable identity,
  predictable actions, and efficient scanning. Verify narrower and wider widths
  before selecting the final breakpoint or layout.

### UXBLIND-012 — My Work empty state contains circular actions (medium; open)

- Reproduction: open My Work with zero items and the default Open work view.
- Observed: “View My Work” links to the current page. Another panel says “Nothing
  selected” and offers “Show all work” despite there being nothing to select.
- Expected: one coherent no-work state that distinguishes an empty inbox from
  filters hiding existing work; avoid self-links and redundant panels.

### UXBLIND-013 — self-service foregrounds system metadata over useful tasks (medium; open)

- Reproduction: open Myself and inspect employment, organization, compensation,
  and available-workflow sections; compare the employee profile.
- Observed: Source / CREATED, record timestamps, and numerous “Not reported” values
  occupy prominent space. Payroll statements and related payroll detail are
  explicitly unavailable. Promotion is the sole offered change workflow.
- Expected: prioritize useful employment and pay information, explain unavailable
  capabilities in employee language, and move technical provenance to a secondary
  view. Future workflow availability must not be implied by the generic change
  instructions until those workflows exist.

## What worked and what remains unverified

- Global search for Adrian returned his profile and a promotion action; Enter
  opened the selected profile. The employee-specific promotion route preserved
  Adrian's context rather than dropping the user into a list of other people.
- The navigation shell remained in place during the observed page transitions.
- Personal identifiers were collapsed by default. Their authorization behavior
  was not tested by this review.
- Journeys initially displayed a loading skeleton and later rendered its content.
  No loading-time benchmark or permanent-loading defect is claimed.
- History navigation was reachable, but zero records meant past-workflow details,
  populated sorting/filtering, and approval/completion behavior could not be assessed.
- No conclusion is made about all pages, all components, backend root causes,
  production readiness, or regression-test coverage.

## Recommended next verification order

1. Retest all sample sign-ins from a fresh session.
2. Resolve or clearly explain missing promotion choices, then exercise a safe
   end-to-end sample workflow with an explicit submission scope.
3. Separate journey review, workflow creation, and employee creation responsibilities.
4. Refine employee identity, position selection, context navigation, and empty states.
5. Retest the resulting screens across responsive widths and populated/empty/error
   states, then add automated regression coverage for the confirmed behavior.

These are review recommendations only. No issue is marked implemented or closed,
and no existing TODO completion status was changed.

## Continued exploration — admin, insights, help, and organization

This pass used the same authenticated Rafael session at an approximately
1153 by 878 pixel viewport. No access settings or business records were changed.
Role editors were expanded for inspection only; no Save or Create action was used.

### UXBLIND-014 — role-assignment employee filter does not narrow results (high; open)

- Reproduction: Admin → Manage roles → Find an employee; enter Rafael and click
  Filter, then observe again.
- Observed: the query remains Rafael but the list still contains Isaac, Marisol,
  Wesley, and the rest of the workforce. Rafael had to be located farther down.
- Expected: matching results, a result count, and a clear no-match state.

### UXBLIND-015 — assigned versus effective access is unclear (high; open)

- Reproduction: expand Rafael in Employee role assignments.
- Observed: only Employee self-service is checked; HCM administrator is unchecked,
  despite the active session accessing administrative screens. The page mentions
  credential fallback but does not explain this employee's effective access.
- Expected: distinguish explicit assignments, inherited/session roles, and actual
  effective permissions, including how a save would change them. This is a UX
  ambiguity, not a demonstrated authorization bypass; no permission was saved.

### UXBLIND-016 — help link promises explanation but opens an empty inbox (medium; open)

- Reproduction: Help → Understand journey stages.
- Observed: navigation opens My Work with zero items, not stage guidance. Help
  otherwise offers navigation links and says support tickets are unavailable.
- Expected: an actual stage explanation and troubleshooting for common blockers,
  such as an empty promotion role selector, with a usable support alternative.

### UXBLIND-017 — Insights does not support the apparent reporting task (medium; open)

- Reproduction: open Insights to inspect workforce/workflow reporting.
- Observed: three zero journey counters and an Operational attention panel; no
  reporting, trends, or exploration controls. The page explicitly says analytics
  capability is unpublished. The prominent label Terminal is technical language.
- Expected: clearly label the currently limited workflow-summary capability and
  describe unavailable reporting in business language. No missing capability is
  claimed to be a broken implemented feature.

### UXBLIND-018 — employee profile return link ignores organization entry (medium; open)

- Reproduction: Organization → expand Finance → Peter.
- Observed: the correct Peter profile opens, but its return link says Back to
  People instead of returning to the Finance bucket just visited.
- Expected: contextual return navigation preserving the source view, or label
  the directory link as a directory destination rather than Back.

### Additional successful task and visual observations

- Finance expanded to three employees; choosing Peter opened the correct Senior
  Accountant profile and displayed Thomas as manager.
- Organization unit expansion was visually clear. Expanding a three-column grid
  card left substantial blank space in its neighboring columns; consider the
  scanning cost with larger teams, without treating this alone as a functional bug.
- Role cards and employee assignments have clear section headings, but some
  employee descriptions truncate in the two-column layout and role badges expose
  internal IDs such as worker_self rather than their readable catalog names.
- Roles loaded after a skeleton state. No timing benchmark was conducted.
- This pass did not create roles, edit visibility, save appearance, generate a
  report, or execute a workflow. Findings remain observations requiring refinement
  and regression verification, not completed fixes.

## Remaining discoverable pages — coverage completion pass

Expanded the sidebar and inspected Worker IDs, Organization visibility,
Brand & appearance, and Experience Studio at approximately 1153 by 878 pixels.
Together with the earlier passes, every page destination exposed by this
session's expanded sidebar has now been visited: Home, Myself, Journeys,
Work queue, Work History, People, Organization, Insights, Admin overview,
Worker IDs, Roles & access, Organization visibility, Brand & appearance,
Experience Studio, Help, and Settings. Employee profiles and employee-specific
promotion entry were also visited; login was inspected in the initial pass.

This is coverage of discoverable page destinations, not every possible route,
employee record, role, modal, form state, or workflow instance detail. No populated
journey existed for detail review. No configuration save or business submission
was made. Navigation was expanded for discovery.

### UXBLIND-019 — sidebar wraps inside words (medium; open)

- Observed in the expanded sidebar: Organization visibility breaks Organization
  across lines as “Organizatio” / “n visibility”; several other entries wrap into
  narrow two-line labels while substantial screen width remains available.
- Expected: enough label width or deliberate word-boundary wrapping. Verify the
  interaction between indentation, glyphs, favorite space, and sidebar width.

### UXBLIND-020 — appearance persistence scope is contradictory (high; open)

- Observed: Color mode describes Light/Dark as applying “on this device,” while
  the page identifies Tenant appearance and says appearance is stored for the
  organization and follows every signed-in user.
- Expected: explicitly distinguish personal/device preferences from organization
  branding before the administrator saves. Actual persistence was not tested.

### UXBLIND-021 — company logo requires an internal asset path (medium; open)

- Observed: Company logo asset is a text field requiring an allowlisted image
  under `/workspace/assets/`, without a visible upload or asset-picker control.
- Expected: a discoverable asset selection/upload workflow or actionable
  instructions for obtaining an approved asset. No upload was attempted.

### UXBLIND-022 — Studio availability copy contradicts adjacent settings (medium; open)

- Observed: Experience Studio says the cell exposes no governed page, brand,
  navigation, access-policy, or publication service. Brand & appearance and Roles
  & access are meanwhile exposed as available configuration pages.
- Expected: describe the specific unavailable page-builder capability without
  making broad claims that conflict with the visible application. The Studio
  return links provide a useful escape route.

### UXBLIND-023 — irrelevant visibility selections appear editable (medium; open)

- Observed: with Own unit selected, the complete Qualified organization units
  checkbox panel remains enabled. Supporting copy says selections only apply to
  allowlist/denylist modes and remain saved in other modes.
- Expected: make inactive selections visually secondary or disabled with a clear
  explanation; provide a plain-language effective-access summary, particularly
  when multiple additive roles are involved. No policy was modified.

Worker ID configuration had useful concrete next-number examples, an explicit
new-workers-only statement, and an explanation of intentional sequence gaps.
The controls and preview were inspected without saving or claiming allocation
correctness. Appearance exposed palette, shape, density, glyph, typography,
navigation, and motion choices; alternate selections were not applied.

## Touchpoint and visual-design pass

### UXBLIND-024 — Clear menu filter does not clear the visible filter (high; open)

- Reproduction: on Studio with expanded navigation, type peple in menu search,
  replace it with zzzznotfound, then click Clear menu filter. Repeat the click.
- Observed: the URL loses the query parameter, but the input still reads
  zzzznotfound and the sidebar still says No menus match. Deleting the input
  text directly restores the navigation.
- Expected: reset input, results, and URL together, preserving usable focus.

### UXBLIND-025 — filtering removes support and preference escape routes (medium; open)

- Observed: both the matching peple query and the unmatched query removed Help
  and Settings from the sidebar, not just the primary navigation destinations.
- Impact: the broken clear action leaves even support navigation undiscoverable.
- Expected: retain a stable support region or an equally clear recovery path.

### UXBLIND-026 — global-search glyph overlaps its placeholder (medium; open)

- Observed across the screenshots at this viewport: the leading search glyph
  sits over the opening letters of “Search people, pages, workflows, and settings.”
- Expected: reserve separate icon space and consistent text inset in the shared
  search component; verify focus, typed text, and narrow-width states.

### Visual refinement notes (design judgments, not measured compliance failures)

- Shape consistency is broadly good: restrained rounded rectangles and green
  accents establish a recognizable family. The larger problem is hierarchy:
  instructional banners, ordinary content, empty states, and forms all receive
  large bordered cards, competing for emphasis and adding scroll distance.
- The sidebar is visually cramped while many main pages have generous unused
  space. Rebalance label width and indentation before shrinking type.
- Technical secondary text is both plentiful and small. Remove implementation
  explanations from the primary task flow rather than merely styling them lighter.
- Replace “No configuration projection is published” with a capability-specific
  customer explanation such as “Page customization is not available in this
  workspace.” Replace Terminal with Finished where that accurately covers the
  outcomes. Preserve precise technical details in a secondary diagnostic view.
- “The UI will not simulate one” explains a development decision, not a user
  next step. Unavailable states should explain what is unavailable and where to go.
- Use consistent action semantics: links for destinations, disclosure indicators
  for actual expansion, and clearly distinguished primary save/submit actions.
  The previously documented Compensation Review control violates that expectation.

### Verified touchpoints and remaining limits

- Typing peple matched People without pressing Enter; exact 250 ms timing was
  not measured. The no-match message appeared for zzzznotfound.
- Work overview opened from the bell and Escape closed it with focus on the
  trigger. Pointer-gap resilience and mouseout were not tested in this pass.
- Menu filter was restored to empty by directly clearing the input. No favorites,
  appearance settings, or business records were changed.
- These observations are not a claim that every touchpoint, alternate theme,
  keyboard sequence, responsive width, or assistive technology has been tested.

### Direct integration repair and browser retest (September 8, late evening)

Stopped the remaining UX hydration Luna process at the user's request and
implemented the repairs directly. Hydration now waits for the framework commit
notification before the router can write to the root. The shell receives a view
snapshot, and loader completion publishes resolved shell state. This removes the
duplicate loading shell and stale account placeholders observed in the browser.

Removed the menu's stale-prop override and ensured clearing schedules the empty
query. Corrected invalid logical search padding and its CSS specificity, kept
header tools on one row, and raised an open workflow action cell above adjacent
sticky cells. An employee without a published promotion path now has a disabled
form with job-ladder recovery guidance; no business rules or access checks were
relaxed.

Manually verified in the Codex browser at 1153 by 878: a single resolved promotion
page after reload, loaded Rafael identity, fuzzy `peple` matching People without
Enter, Help/Settings retained, Clear restoring the menu and clearing the URL,
nonoverlapping search glyph, one-row header, and an unobscured employee workflow
popover. No promotion was executed: Adrian has no published Sales ladder path.

Passing checks: full productui package (43.151s), targeted new CSS/navigation
regressions, full journeyclient and journey renderer packages, productui and
journeyclient go vet, and the js/wasm WEB-040 shell hydration/outlet regression.
The WASM fixture initializes the UI facade before installing its controlled
adapter. No PR, complete responsive/theme/accessibility sweep, or 8/10 claim is
made by this pass. Remaining review issues still require integration.

### Follow-up direct fixes and verification

- UXBLIND-001: manually clicked each of the four sample login buttons in the
  Codex browser. Dominic, Thomas, Samuel, and Rafael each reached Home with the
  matching identity. Samuel's menu excluded People, Journeys, My Work and Admin;
  Rafael's administrator menu returned on signing back in. This verifies entry
  and visible navigation, not a complete authorization penetration test.
- UXBLIND-005: the inspected login cards now describe business tasks instead of
  governed-workspace jargon.
- UXBLIND-007: the integrated Journeys list no longer embeds a second directory,
  employee creation form or inline promotion form. It has a People entry point
  and a workflow list. Browser inspection also caught and prompted correction
  of stale empty-state wording referring to a form below.
- UXBLIND-008: scope is now informational text without a dropdown chevron or
  misleading Settings navigation. Browser verified on My Work and Home. Reviewed
  shell/page-header golden changes and replaced the test requiring the fake link.
- UXBLIND-012: empty My Work suppresses its redundant selection-preview panel
  and self-link. Browser verified. Its generic filter advice remains a wording
  refinement rather than being treated as fully polished.
- UXBLIND-014: the initial lane change failed manual testing. Direct fixes moved
  filter hooks ahead of variable row hooks, isolated role and employee editors,
  and admitted the role query into the route parser/canonicalizer. Browser
  verified Rafael-only results, Filter producing q=Rafael, and reload preserving
  both query and matching result. No role assignments were saved.

The product UI full suite passed again in 45.763s after role-component isolation;
targeted role tests, the full productclient suite, journey-renderer tests, and
vet also passed. Added regressions for focused
Journeys, informational scope, empty selection omission and role-query URL
round-trip. No claim is made that the remaining identity, effective-access,
position-picker, responsive, appearance or accessibility items are complete.

### September 9: direct refinement after the all-page review

Moved shared breadcrumbs onto their own header row; added missing padding to
organization appearance guidance. Corrected light/dark option descriptions in
English, German, and Arabic so they no longer claim device-only scope. Home now
opens the employee directory directly for promotion, avoids a duplicate directory
action, replaces Terminal with Completed or closed, and removes circular empty-work
advice. Reduced navigation favorite reservation so Work History fits on one line.
Studio now explains that custom page editing is not enabled in plain language.

The productui package passed its full suite before the final Studio/menu edits;
targeted UX regression tests and go vet passed afterward. Rebuilt Go/WASM and
restarted the local server. Codex browser screenshots verified desktop/light Home
and Appearance. This is a partial refinement: employee identity, eligibility before
launch, role-assignment clarity, deeper content changes, and mobile/dark validation
remain open. No completion ticks or commits were made for those unresolved items.

### Continued refinement: truthful launchers and role identity

Removed the fabricated worker_self assignment from the roles editor. Missing
assignments now show a localized No explicit assignment state; known assignments
use role names instead of IDs. Added worker numbers beside preferred names in the
directory without deriving or exposing legal surnames. Browser verified both.

Promotion launchers now reuse the workflow form's published-choice resolver from
the existing ListWorkers response. No additional per-employee RPCs are issued.
Known-unavailable promotions are omitted from directory, profile, and global-search
actions; profiles explain the missing job ladder/pay band. Unknown options preserve
the existing launch behavior, and server authorization remains authoritative.
Browser verified Adrian's directory and profile no longer offer a dead-end action.

Help now links to concrete, permission-filtered tasks with English, German, and
Arabic labels. Updated the Studio test's obsolete heading assertion without
removing its no-simulated-publication checks. Productui passed its full suite
(48.291s), productclient and journeyclient passed, and vet passed for all three.
Dark appearance preview was visually inspected without saving organization settings.
Mobile-width and full cross-page dark-mode coverage remain unverified; no 10/10
or complete accessibility claim is made.

### Action launcher focus dismissal

Connected the stateful Start an action launcher to a reusable focus-dismissal
hook: internal focus moves stay open, focus leaving closes after 180ms, outside
pointer clicks close immediately, and Escape closes from any launcher child.
Unmount/close removes listeners and cancels pending timers. The WASM regression
test TestActionLauncherFocusDismissal passed, as did WEB-040 native tests and vet.
Rebuilt and restarted the server. Codex browser manually verified Tab into the
search keeps the panel open, Tab past both results closes it with focus on Language,
and clicking global search closes it. No business records or preferences changed.

### Journeys loading race

Reproduced the stuck loading screen with a deterministic WASM regression that
publishes resolved data between the initial render snapshot and subscription.
The test failed before the fix with "Loading journeys". The live component now
subscribes before requesting a catch-up render and rebinds when its store changes.
The regression now passes, including later updates and unsubscribe on unmount.
The full native journey renderer suite and vet pass; git diff --check passes.
Rebuilt the embedded Go/WASM assets and restarted the backend. Visually verified
in the Codex browser after reload and Home-to-Journeys navigation: loading resolves
to the current empty result (0 journeys), rather than hanging. No workflow was
created or business data changed during this fix.

### Workforce by organization refinement

Visually inspected the original flat and reporting views in the Codex browser.
Expanding one narrow team card wasted adjacent space and truncated roles. Replaced
the outer three-column grid with full-width disclosures and responsive member
grids, wrapping metadata and exposing worker numbers. Workforce exploration now
precedes supporting metadata. Renamed the views By team and Reporting lines and
localized descriptions/counts in English, German and Arabic.

Three adversarial review passes identified and resolved false ARIA tree semantics,
non-collapsible reports, ambiguous display-name manager attachments, empty metadata
rows and a missing reusable disclosure-label fallback. Reporting branches now use
native details/summary and nested lists; employee links remain separate. Duplicate
manager names are treated as unresolved rather than arbitrarily attaching reports.
Stable manager IDs remain an upstream limitation; no data or permission changes.

Full productui tests passed (45.106s), including latency checks. A first run during
WASM compilation exceeded the 16ms navigation p95 budget (17.0ms); the uncontended
rerun passed without changing budgets. Targeted organization/Myself and cross-page
i18n/accessibility gate tests passed after final refinements (13.397s). Vet and
diff checks passed. Staged coverage command had no Go packages to gate because
these changes are not staged; it is not coverage evidence for this patch.

Rebuilt/restarted and visually tested desktop light at 1153x878: expanded team
cards, report expansion, Enter collapse/reopen and Anika's correct profile link
(HC-21013). Final reviewer found no blocking defects. Mobile 320/390px and dark
visual inspection remain unverified in this pass; no literal 10/10 claim is made.

### Follow-up navigation latency investigation

Subsequent heartbeat runs reproduced the stable-shell 16ms p95 gate failure;
the earlier passing full-suite run is not evidence of consistent performance.
A CPU sample attributed substantial work to repeated registeredPages construction
inside projection validation. Changed validation to build a call-local definition
lookup once and pass resolved definitions to URL validation. All admission, text,
structural, canonical-route and parent checks remain in place; no global cache or
permission result is retained. Targeted navigation/registry/WEB-037/039 tests pass
(3.572s), as do vet and diff checks. Median timing improved in the observed runs,
but p95 still exceeds 16ms. This follow-up optimization is not yet rebuilt into
the running server or browser-verified, and the performance issue remains open.

### Promotion: observe, diagnose, repair (2026-09-09)

Enabled local stdout OTel export and added bounded request-log error_type
classification (schema mismatch, database/conflict, cancellation and deadline).
No diagnostic message, SQL text, salary or request payload is logged. Tests
exercise wrapped causes and ensure secret diagnostic text cannot leak.

Reproduced ListJourneys failure as SCHEMA_MISMATCH while ListWorkers succeeded.
The existing development database was at migration 261; applied pending
migrations through 280 without resetting data. Journeys recovered. Completed
Omar's today-effective demo promotion using the Codex browser: proposal,
simulation, execution, finance and manager approvals, durable timer wake,
revalidation and terminal ledger record. Journey
01a08614-a15b-72ae-91f1-fd77aa4a06d5 remains Recorded/COMPLETED after restart.
This verifies a synthetic local transaction, not external payroll delivery.
A separate future-effective Omar journey correctly remains parked until Dec 1.

Fixed live terminal updates retaining an obsolete success notice saying the
promotion is waiting; added a regression assertion. Transport, request logger,
journeyclient tests and vet pass. The database-backed
TestPromotionBackendCommitsTheWorkflowTransactionEndToEnd passes (86.454s).
Rebuilt journey WASM and restarted the backend with stdout tracing enabled.

Open findings, not claimed fixed: Jane's legacy demo baseline incorrectly uses
the shared scenario salary, making her target band incompatible with the raise
limit; switching employees exposed a submit no-op cleared by reload. Final
visual inspection after restart also exposed a Live updates stopped warning
above the persisted completed outcome. The watch/stream termination path needs
further diagnosis; successful durable completion does not prove feed robustness.

### Jane baseline correction (2026-09-09)

Reproduced Jane's persisted blocked proposal in the Codex browser. Request-level
success alone could not explain the business failure, so added bounded OTel
events distinguishing declared-reference, durable-worker and unavailable
baseline sources, without salary or worker identity attributes. Reused Jane's
existing reference simulation inputs (165000 USD, 15% bonus) through shared
fixture constants. Omar retains his own legacy baseline; created workers retain
their durable rows. Other corpus workers now fail explicitly when compensation
is unavailable instead of silently inheriting Omar's pay. No policy was loosened.

Correction to the earlier investigation: a compa ratio uses the band midpoint,
not its minimum. The existing simulation certifies Jane's 180000 USD target as
in-band. The new browser proposal 01a0862c-4fd5-748c-8cba-64c988410fc8 displays
165000 to 180000 (+9.1%) and is ready for execution review. The exported proposal
span d5df9ce29691ed4a9c7b71c3612dd4dd records
promotion.baseline.declared_reference with status Ok. Existing immutable blocked
proposals were not rewritten. New proposal remains unexecuted, effective Dec 1.

Full internal/intent/app tests PASS (34.980s), internal/workflow/simulate tests
PASS (0.326s), vet PASS. Regression covers Jane's exact baseline and valid ladder
increase, resolved-subject precedence and refusal to borrow pay for unknown
workers. Staged coverage gate reports no Go packages (not coverage proof).
Browser inspection: desktop dark, corrected summary and readiness verified.
No layout changes; mobile and other locales were not rechecked in this pass.

### Follow-up: live feed failure isolated

Jane's new request log shows WatchJourney ending at 30.0005s, 30.0004s and
30.0001s with DEADLINE_EXCEEDED. The stream interceptor uses the common admission
deadline cap (default 30 seconds), while journeyclient/watch.go assumes a
fifteen-minute ceiling and counts each no-message termination as a failed
attempt. Thus an unchanged journey exhausts three attempts and displays the
warning after roughly 94 seconds. The database result is not lost. This is a
diagnosed deadline/reconnection mismatch, not a fixed issue yet; a repair must
preserve bounded streams, cancellation and a bounded retry rate rather than
remove transport deadlines or mask arbitrary connection failures.

Implemented a bounded client-side distinction: an established quiet stream
(at least ten seconds) ending with EOF/deadline now resets the fruitless-retry
count and reconnects with its existing digest. The two-second reconnect delay
and cancellation remain; immediate closures and other errors retain the retry
budget. Regression tests cover deadline/EOF rotation versus immediate failures,
permission denial, unavailability and cancellation. Journeyclient package tests
PASS (0.750s), vet and diff checks PASS. This follow-up is not yet rebuilt or
browser-verified across multiple real 30-second rotations; runtime verification
remains required before calling the feed issue resolved.

Follow-up rebuilt the WASM bundle after rerunning journeyclient tests (PASS,
0.682s) and restarted the local backend with stdout telemetry. Codex browser
reload of Jane's new proposal renders the correct 165000-to-180000 summary
without an initial feed warning. Rotation-window verification is still pending;
the active log is .artifacts/tmp/promotion-watch-verify.log. No proposal executed.

### Promotion action hierarchy refinement

Moved the existing, single shared actions section directly below the promotion
summary so it precedes supporting details at narrow desktop widths. Simplified
stage and action descriptions; clarified that starting does not record the
promotion and that effective-date/final checks still apply. No action duplication,
permission or transaction changes. Added a render regression asserting actions
appear exactly once before proposal details. Renderer tests PASS (1.137s), client
tests PASS (0.557s), vet and diff checks PASS. Rebuilt/restarted and visually
inspected desktop dark at 994x878. Other widths/locales not verified this pass.
The prior watch-verification log shows reconnects beyond the old three-expiration
cutoff and the browser retained its correct summary without a stopped-feed
warning. Employee-switch submission remains a separate open investigation.

### Adversarial page-by-page refinement, target 9/10

User requested all pages and controls, with adversarial review after each change.
Reviewed three changes: collapsed technical IDs in the journey hero, plain
Start approval workflow action labeling, and complete productclient stage
mapping. The last corrects Recorded journeys being counted as active: browser
Home now shows five active and two completed/closed, instead of seven and zero.
Reviewer accepted the changes with minor consistency follow-ups. Two initial
review claims (missing blocked mapping and missing disclosure CSS) were checked
against source and withdrawn; existing CSS and blocked handling were preserved.

Renderer, journeyclient and productclient suites pass (1.210s, 0.544s, 0.328s),
vet and diff checks pass. Rebuilt/restarted; Codex browser desktop dark verified
Home counts and the journey summary/disclosure/action layout. Not 9/10 yet:
Home rows still display lifecycle unavailable, current/proposed details remain
technical, and action wording differs in the Home status adapter. Employee
switching and mobile/light/keyboard/locale coverage remain open. No claim that
all pages or controls have been reviewed.

Remaining review order: Home/work/history data clarity; People/person/Myself
navigation and forms; Organization flat/tree; Insights; admin overview, worker
IDs, roles and visibility; Appearance/Settings; Studio/Help; login and shared
search, navigation, popovers, loading/error states. Assess task clarity,
navigation, feedback, visual hierarchy and accessibility separately; do not
assign a passing score to untested roles, locales, themes or viewport widths.

### Shared Home/My Work status refinement

WorkRowProps previously discarded the authorized journey-stage label and only
rendered unavailable canonical dimensions. Added a separate JourneyStage prop
and shared workStatus rendering for row and preview. Canonical dimensions take
precedence when supplied; a known stage never synthesizes them. Neither source
present retains the unavailable message. Adversarial review caught and resolved
the preview inconsistency and the misleading dashed unavailable CSS on known
stages. Regression tests cover row, preview, missing data and canonical precedence.

Targeted productui status/composition suites PASS (0.305s); prior vet/diff checks
pass. Rebuilt/restarted and visually verified Home desktop dark: rows now compact
and readable, stage text visible, five active/two closed retained. Journey-stage
localization and explicit accessible context remain polish gaps; no overall
9/10 certification, mobile or all-page completion claim. Next: verify selected
work preview, workflow filters and history, then remaining page inventory above.

### My Work controls and History empty-state pass

Browser-tested My Work selected summary and Awaiting approval's empty queue:
no stale preview remains when filtering to zero items. At 994px the Past
workflows destination clipped behind a horizontal tab scrollbar; scoped Work
tabs now wrap, preserving other tab surfaces. Proposed-stage copy now says
Ready to start approval, matching the detail action. Adversarial review found
no blocker; 320px crowding remains to be visually checked.

History's unmatched search showed zero of two records but incorrectly implied
there were no recorded workflows yet. It now uses the existing localized
no-match explanation when the authorized universe is nonempty; genuinely empty
history keeps its onboarding explanation. Regression covers both cases and
review confirmed no unauthorized-universe leakage. Browser reload retained the
search and displayed the corrected explanation; Clear filters restored both
records. Productclient/work/responsive/history targeted tests PASS, vet and diff
checks PASS. Rebuilt/restarted. Desktop dark checked at 994/1251x878; no blanket
all-pages or 9/10 claim. Remaining locale fallback and mobile checks still open.

### Continuous goal: People row actions

Registered the user's new continuous all-page >=9/10 refinement goal. People
inspection found missing manager text and a clipped workflow menu in a one-row
filtered matrix at 1251x878. Missing directory role/team/manager/location now
use the existing localized Not reported fallback without inferring absence of
a relationship. Desktop workflow panels remain in table flow so the row expands
instead of clipping its action; mobile fixed sheet behavior unchanged. Shared
popover handlers and scroll container retained. Reviewer checked both changes
and identified row expansion/column width as visual tradeoffs, not completion.

People tests PASS (0.451s). Rebuilt/restarted and browser-verified menu is visible,
then clicked Start Promotion for Omar and reached Promote Omar with OPS-HRBP2/P2
context. Mobile, keyboard crossing/dismissal, long action lists and final layout
polish remain unverified. Goal remains active; no 9/10 claim and no new promotion
submitted in this pass.

### Software-navigation verification and Settings localization

Submitted an unexecuted local demo proposal after People software navigation:
Omar's role correctly selected P3 and the proposal persisted as
01a087a4-9b4c-744f-b8d3-2171b0bf1a24. ProposeJourney logged OK in 38.7101ms,
request req:5fe1270c3f5d7883. This attempt did not reproduce the historical
intermittent submit failure; it is not proof the intermittent bug is fixed.

Inspected Myself's read-only overview, hidden personal-info disclosure and
no-eligible-workflow state. Settings wording now describes account access and
personal preferences without transport jargon in en/de/ar. Reviewer caught and
resolved initially inconsistent translated subtitles. Settings/locale/i18n
tests PASS (0.371s). Rebuilt/restarted and browser-verified Arabic RTL, then
restored English. Arabic Work queue/Admin overview still fall back to English;
these and mobile/full keyboard checks remain open. No overall9/10 claim.

### Admin entry points and visibility disclosure

Removed the unrelated primary Journeys action from the Admin hero; capability
cards retain role filtering. AdminHero now omits an unconfigured action instead
of rendering empty navigation. Collapsed the nine-role visibility summary into
a native disclosure while keeping the additive-grant boundary explanation
visible. Added a dedicated en/de/ar disclosure label after adversarial review
rejected the misleading generic role-catalog label.

Targeted admin, visibility and locale tests PASS (latest 0.378s); productui vet
passed. Rebuilt the WASM bundle and restarted the local backend. Codex browser
verified the updated dark desktop layout, disclosure click/Enter toggle, and
Admin navigation: role editors now start within the first screen and capability
cards remain visible without the unrelated hero action. No permission changes
were saved. Remaining issues include technical admin copy, cramped visibility
hero links, German/Arabic policy-description drift, and unverified mobile/role
variants. No overall 9/10 claim.

### Worker ID live draft preview and bounded formatting

Browser reproduction: entering TEST in Prefix left HC examples unchanged.
WorkerIDPage now owns draft state across component renders and updates examples
from the same non-allocating formatter as the server. The preview describes
unsaved inputs, current-year/CARE context, and the requirement to Save. Invalid
numeric/range settings show an unavailable preview instead of plausible numbers.
The timestamp is captured for the component lifetime. Server snapshot changes
reset the draft using the existing guarded GWC reconciliation pattern.

Extracted workerids.Preview and replaced the transport's unbounded exclusion
scan. The shared implementation jumps excluded intervals on the increment grid,
returns at most four examples, and does not mutate or reserve numbers. Regression
tests cover trillion-number exclusions, exhaustion, invalid ranges, increment
alignment, deterministic formatting and unchanged allocation state. Domain tests
PASS; UI targeted tests PASS (0.419s), transport targeted tests PASS (0.507s), vet
PASS. Large-exclusion benchmark: 9125 ns/op over 100 iterations locally, not an
end-to-end latency measurement.

Adversarial reviewer checked allocation safety and raised raw normalization and
render reconciliation risks. Closed enum selects and explicit numeric validation
address the former for supported controls; affix normalization remains the same
as server behavior. Rebuilt/restarted and browser-tested TEST prefix, increment
0 (no examples), then 5 (TEST-001000/1005/1010/1015). Focus stayed on the edited
control and the prefix survived subsequent edits. Session redirected to login
on reload; signed back in as Rafael and confirmed persisted HC/increment 1.
No policy was saved or number issued. Blank numeric input, complete save/error
reconciliation, affix-normalization explanation, mobile and screen-reader live
preview announcements still require review. Overall goal remains active.

### Incomplete numeric input and disabled-control refinement

Confirmed in the browser that clearing Increment retained the last valid
examples. Numeric drafts now preserve raw text, represent incomplete input as
invalid rather than reusing a number, and prevent numeric-invalid submission.
Adversarial review drove two refinements: disabled Save has localized en/de/ar
guidance in its associated live status region, and the preview no longer shows
a green uniqueness badge when no examples are available. Browser testing also
found native disabled buttons looked active; shared theme-token disabled/hover
styling now removes the active fill, motion and shadow.

Targeted Worker ID/locale tests PASS (0.430s; final shared-style regression
0.387s). Reviewer accepted the final guidance and disabled styling. Browser
verified blank Increment remains blank after a prefix edit, unavailable examples,
visible disabled Save guidance, and neutral disabled styling at 1251x878 dark.
Restored draft increment to 1 without saving. Brief Appearance light-mode preview
also changed the shell correctly; reload restored the saved system mode, with no
organization-wide save. Mobile and other themes for disabled controls remain open.

Full productui suite is NOT green: TestSupportMenusRemainReachableDuringFuzzySearch
fails its rendered support metadata assertion. This is the next investigation,
not a waived failure. Vet completed. Staged coverage gate passed 5 packages, but
that is evidence for the shared index's staged packages, not this unstaged UI
change. No commit or staging performed. Overall goal remains active.

### Menu search result placement and full UI suite recovery

The failing support-search test expected obsolete Settings copy; updated it to
the current localized subtitle without weakening result/recovery assertions.
Browser inspection also found a real placement defect: language matched Settings
but placed it at the bottom of an otherwise blank sidebar. Matching support
pages now appear once in the main results, while unmatched support links remain
at the bottom. Unmatched queries show a localized empty-result message even
when recovery links are available.

Added rendered ordering/deduplication and no-match recovery regressions.
Targeted menu tests PASS (0.402s); full productui suite PASS (25.529s); vet PASS.
Adversarial review found no blocker in query synchronization or deduplication.
Rebuilt/restarted and browser verified Settings near the filter, no-match message
with Help/Settings retained, and Clear restoring the menu with focus on search.

New observation to investigate: after reload with menu_q=language, results were
filtered correctly but the input appeared empty. Typing and Clear worked. This
reload/hydration mismatch remains open despite the full native test suite passing.
Desktop dark only for this pass; no overall 9/10 claim.

### Cold-load menu query hydration corrected

Traced the reload mismatch beyond the isolated Render test: serveProduct built
its initial loading shell without menu_q. GWC preserves initial live input
values during hydration, so the client filtered results while the input retained
the empty loading-shell value. The HTTP entry now passes menu_q into the loading
document builder and seeds the trimmed query before rendering. No imperative DOM
write or per-keystroke remount was introduced.

Added loading-shell query tests for language, whitespace and escaped HTML input,
plus an isolated rendered-value assertion. Targeted workspace shell suite PASS
(1.033s), productui assertion PASS (0.411s), workspace vet PASS. Adversarial review
accepted the root-cause fix. Restarted the backend and browser-reloaded the exact
menu_q=language URL: the field retained language and Settings matched directly
below it. Clear still restored the catalog. The earlier claim that server markup
was correct applied only to the isolated full View, not the actual loading shell;
this test gap is now covered. Other page/role/mobile review remains active.

### Insights aggregate consistency; Studio and Help inspection

Inspected Insights, Studio and Help in the Codex browser. Fixed an Insights
aggregate inconsistency: Visible workflows used the raw list length, while other
metrics used admittedWork. All now use the same discoverable population. Added
visible/hidden/absent-verdict regression coverage (1 total, 1 active, 0 closed).
Targeted authorization tests PASS (0.392s), vet PASS, adversarial review accepted.
Rebuilt/restarted and verified admin Insights still renders the live 8/6/2 summary.
Restricted-record behavior is proven by tests, not a browser permission mutation.

Studio explicitly marks editing unavailable and gives recovery links; Help shows
promotion guidance and available destinations without a fake support submission.
Their technical wording and role/mobile variations still need refinement. An
attempt to increase browser zoom with keyboard shortcuts produced no observable
zoom change, so it is NOT counted as responsive/reflow verification. This pass
remains desktop dark at 1251x878. Overall 9/10 remains unproven.

### Shared action launcher keyboard and accessibility semantics

Browser-tested Escape (focus returns to trigger) and Tab-away (closes while
preserving focus on Language). Found visible options announced as a collapsed
combobox, and aria-modal claiming a modal focus boundary despite free Tab exit.
The launcher now declares a non-modal dialog, reflects visible list expansion,
and references options only while present and visible. Empty results are a status
message rather than a missing listbox. Adversarial review prompted explicit
empty-status semantics; browser screenshot then exposed misleading no-authorization
copy for unmatched searches. Added en/de/ar no-match recovery wording while
retaining the distinct empty-authorized-catalog message.

Targeted WEB-040 and launcher tests PASS (final 0.499s), vet PASS before the final
copy change; golden updated for intentional accessibility markup changes.
Adversarial review accepted each refinement. Rebuilt/restarted and browser
verified expanded empty-query list, unmatched recovery wording and Escape focus
restoration. Desktop dark only; real screen-reader speech and mobile remain open.

### Employee-role workflow guidance

Logged in as Samuel Rivera through the actual local login. Home navigation is
limited and Myself resolves his own worker/pay record. Found job-ladder advice
shown despite denied workflow creation. The shared profile launcher now defers
to the access explanation for denied requesters, retaining ladder advice when
creation is allowed. No authorization or employee data was changed.

`go test ./internal/humanwork/productui/ -run 'TestMyself|TestProfileWorkflowEmptyReason' -count=1`
PASS (0.430s); productui vet PASS. Adversarial review found no blocker.
Rebuilt WASM, restarted the backend, and verified the corrected explanation in
Codex browser AX and desktop dark screenshot (1251x878). Mobile, other locales,
and the remaining role/page matrix are not claimed complete.

### Role-aware Help walkthrough

Samuel's Help screen advertised promotion guidance but exposed only a Settings
link. Reused QuickActions and InformationalPanel to show permitted self-service
destinations under a task-oriented heading. Users without journey-create access
now receive access/change-request guidance; authorized requesters retain the
promotion setup explanation. Added en/de/ar messages and a six-case locale and
permission regression. No role grants or business data changed.

Targeted `go test ./internal/humanwork/productui/ -run 'TestHelpGuidance|TestUXBlind016' -count=1`
PASS (0.417s), vet PASS, adversarial review found no blocker. Rebuilt/restarted;
Codex browser desktop dark 1251x878 verified the revised screen and the profile
link reaching Samuel's own record with heading focus. Mobile and locale visual
checks remain open. Full productui run (25.506s) failed WEB-037 and WEB-039 shell
golden digests; expected hashes were not changed without attribution. All other
tests reported no failures in that run. The shared dirty checkout is preserved.

### Shell golden attribution and regression recovery

Proved the WEB-037/039 mismatches were solely the earlier launcher accessibility
fix: restoring only the old closed-input aria-controls/aria-activedescendant and
dialog aria-modal in the rendered strings reproduced the exact prior golden
hashes (e68df15d / 3c31212e). Production code was never reverted. Removed the
temporary diagnostic test and updated expectations with explanatory comments.
Adversarial review found no concealed change or weakening.

`go test ./internal/humanwork/productui/ -count=1` PASS (24.574s), productui vet
PASS. No new production rendering change required a rebuild. Continued browser
walkthrough on Samuel Settings: desktop dark account identity, language cards,
and restricted navigation render without overlap; heading focus follows software
navigation. Accessibility preference save behavior and narrow widths still need
interaction verification. No overall UI score is asserted.

### Settings persistence and translated Help verification

In Samuel's real local session, selected Limited motion and saved. Inline success
feedback appeared beside Save, focus stayed on the button, and reload retained
the selection. Restored his original System motion selection and saved again.
Switched via Settings to German, followed Help, then switched via the header to
Arabic. Both translated Help layouts fit desktop dark 1251x878, with RTL mirroring
in Arabic. Restored English and dismissed the language popover with Escape.

The header language options were reported as URL-only links by the browser AX
tool, although a screenshot shows all three language labels. This is provisional
accessibility evidence, not a confirmed missing-name defect; investigate before
changing production behavior. English scope wording and Arabic footer fallback
remain visible localization gaps. No production code changed in this pass.

### Promotion focus: Jane completed through the live UI

User redirected work to promotion. Logged in as Rafael and used People search
Jane, workflow menu, Promotion. Submitted ENG-MGR1/M1, test-fixture position
POS-ENG-MGR-1, USD180000, effective2026-09-09 with an explicit local-test reason.
Journey 01a088e3-d5cd-7184-b80d-c5b83951c7f8 resolved USD165000 baseline and +9.1%.
Started execution and completed finance then manager approvals with recorded
test reasons. The UI reported Promotion recorded. Reload confirmed Recorded,
COMPLETED instance55d73d66-f1e8-5798-8858-e9e9f5210273 and completed ledger stage.
Desktop screenshot confirms completed stage indicators and exact pay comparison.

The initial directory/form pay omission is intentional for corpus workers;
journeyBaseline resolves scenario compensation separately. Do not populate the
directory with invented baseline values. Remaining UX findings: dash does not
explain omitted pay; internal identifiers dominate detail sections; effective
date guidance promises no past dates but server accepted Sept9 at Sept10 UTC.
The latter requires checking evaluation/timezone semantics before declaring a
runtime violation. Adversarial promotion review requested. No production code
changed; this verification created and recorded a local demo promotion.

### Promotion effective-date wording corrected

Adversarial review confirmed domain policy permits bounded retroactive dates;
the universal today-or-later form claim was false. Replaced it with an explanation
of simulation policy checks and required approvals/final checks. Removed the
same unsupported assertion from DefaultEffectiveDate's comment. Policy and date
selection behavior are unchanged. Targeted TestProposalFormShape PASS (0.380s),
journeyclient vet PASS; reviewer found no blocker. Rebuilt and restarted, then
visually verified the new guidance in Jane's live form at desktop dark1251x878.
No second proposal was submitted. Mobile and translated journey forms remain
unverified; the successful recorded Jane transaction remains intact.

### Promotion confirmation cancellation

Native confirmation summary was hidden when open, removing the way to back out.
Kept it visible with state-specific Review/Cancel review labels. Live desktop
Codex browser verified open then Enter cancels, preserves focus, and leaves the
existing December Jane proposal unexecuted. Targeted renderer test and vet passed;
adversarial reviewer also reported full renderer package passing. The screenshot
showed Cancel inheriting primary emphasis, so the summary now requests secondary
styling; this final styling adjustment still needs rebuild/visual verification.
Journey labels remain English pending renderer-wide localization work.

The rebuilt secondary toggle exposed a CSS background-color/gradient conflict.
Fixed secondary normal/hover backgrounds with shorthand that clears the primary
image. Full renderer package PASS (1.276s); adversarial review accepted scope and
theme-token use. Rebuilt/restarted and visually verified readable outlined Cancel
review in desktop dark1251x878; Enter closes it without starting the proposal.
Primary confirmation styling remains intact. Narrow/mobile and light theme are
not claimed verified in this pass.

### Promotion review reading order

Moved Current and proposed above Request in the shared proposal detail panel;
no facts removed. Added TestPromotionComparisonPrecedesRequestMetadata (PASS
0.945s); full renderer package passed before the assertion addition (1.284s).
Adversarial review accepted. Rebuilt/restarted and browser AX/screenshot confirm
comparison is first, desktop dark1251x878. The screenshot exposes a next issue:
the side-by-side History panel leaves the pay-delta column horizontally clipped
inside the table scroller. Reading order is improved, but width/layout still
needs refinement and is not rated complete.

Moved the existing proposal panel above the two-column supporting-detail area.
No duplicate rendering or removed facts. Full renderer tests PASS (1.240s),
adversarial review found no blocker. Rebuilt/restarted; desktop dark1251x878
screenshot now shows the complete +USD15000 (+9.1%) column without horizontal
scrolling. Mobile layout remains unverified. No workflow was submitted or changed.

### Promotion navigation regression reproduced

From the older Jane proposal, View all promotion journeys correctly clears the
detail selector. Opening recorded Jane from that list then remained on Reading
this journey from the engine for over30s. Reloading the identical detail URL
immediately showed Recorded. This isolates a software-navigation publication or
loading-state defect, not missing durable promotion data. Backend logs continue
serving requests and watch rollovers. Requested adversarial root-cause review of
journeyclient loadDetail/applyDetail/watch and embedded routing. No fix claimed.

### Grouped promotion request navigation resolved

Reproduced a separate deterministic list-to-detail stall in the integrated
Journeys page: the visible subject-group cards changed the URL to a standalone
`#/journeys/<id>` fragment while leaving the product history router on the
list. `journey.Wire` bound callbacks on the flat `Journeys` slice but not the
`Groups` copies that the overview actually rendered. Both representations now
receive their own live `OnOpen` callbacks. Copied legacy fragment links are
normalized to the product query route on a cold load; an attempted hot hash
fallback was removed after adversarial review found a Back-button trap.

The grouped-card regression test, product route tests, and the four related Go
packages passed. Rebuilt the WASM and restarted the local backend. In a clean
Codex browser tab, clicking Jane's recorded request opened her detail at the
canonical `?journey=` URL; Back returned directly to the grouped list and
Forward returned to the recorded detail. A copied fragment URL also resolved
to detail on reload. The adversarial re-review found no remaining P1/P2 in
this narrow change. This verifies navigation, not a new promotion submission
or every other detail-loading race described above.
