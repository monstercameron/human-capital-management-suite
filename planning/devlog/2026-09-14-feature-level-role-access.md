# Feature-level role access

Completed `WEB-241` by extending the existing page authorization boundary down to stable, page-local features without making presentation labels, routes, or component placement part of the security identity.

## What landed

- Every registered page now receives `content` and `actions` feature boundaries automatically; pages may add specialized features and declare their supported CRUD ceiling in the same registry.
- Tenant-scoped role feature grants are durable, RLS-protected, CAS-versioned, seeded from the registry, and exposed through the canonical Journey service.
- Effective authorization is the intersection of the role's page action and matching feature action. Unknown features, missing configured grants, unsupported actions, and feature grants beyond a denied page all fail closed.
- Server handlers gate page content, page actions, promotion requests, journey inspection, approvals, worker directory reads, and worker creation at their feature boundary.
- The Go/WASM session preserves the distinction between a legacy absent projection and an authoritative empty projection.
- The roles administration page exposes feature CRUD controls only for pages the role can view. The editor groups large catalogs by page, uses stable feature IDs, and disables operations outside each feature's declared ceiling.

## Verification

- Named `WEB-241` primary, property, security, browser, conformance, regression, transport, and embedded-Postgres integration tests pass.
- `buf lint schema/proto` passes.
- `GOOS=js GOARCH=wasm go build ./tools/uxqual/cmd/journeywasm` passes.
- The production Go/WASM UI was rebuilt, the local server was restarted, and the HCM administrator's 65 feature grants were manually inspected in the Codex browser. Page grouping, descriptions, CRUD controls, disabled unsupported operations, and accessible page/feature/action labels were visible and correct.

The shared checkout contains unrelated concurrent work. No unrelated files were reset, staged, or rewritten as part of this slice.

## Scalable page-module extensions

Completed `WEB-242`, `WEB-243`, and `WEB-244` by replacing coordinated page-ID dispatch with one immutable page-module registry and reusable route/data profiles.

- Each module owns its stable definition, stateless renderer adapter, declarative access audience, snapshotted feature catalogue, route-state profile, and authorized data-requirement profile.
- Sorted immutable indexes provide deterministic binary lookup; public callers receive copies, while hot-path profile lookups allocate no memory.
- Server request normalization, persistent shell address state, canonical client URLs, client read planning, and WASM warm-route identity consume profiles instead of growing page-specific switches.
- Registry validation rejects duplicate or incomplete modules, invalid product routes, incomplete identity metadata, invalid parent navigation, mismatched feature ownership, unsupported CRUD ceilings, and missing profiles.
- The contributor gate renders and audits every registered page for authorization-filtered discovery, semantic main/heading/skip-link structure, theme and responsive contracts, lookup cost, and reintroduced page-ID branching.

### Verification

- `go test -work -count=1 -run "TestTodo_WEB_24[1-4]|TestPageRegistryOwnsCanonicalIdentityRouteAndRenderer|TestAddressState|TestPageVisibilityRoleMatrix" ./internal/humanwork/productui` passes.
- `go test -work -count=1 ./tools/uxqual/productclient ./tools/uxqual/cmd/journeywasm` passes.
- `go vet ./internal/humanwork/productui ./tools/uxqual/productclient ./tools/uxqual/cmd/journeywasm` passes.
- The production Go/WASM UI was rebuilt and manually exercised against the real local server in the Codex browser. Journeys and People loaded live data at desktop, 390 px, and 320 px; light and dark themes remained readable and responsive without horizontal overflow; no browser console warnings or errors were emitted. The pass caught and fixed a collapsed promotion-review disclosure that exposed both labels and redundantly rendered `Review and review and submit`; it now shows one `Review and submit` label and retains the alternate close label only for the open state. The organization appearance used for testing was restored to `Plum · Dark`.
- The full product UI package passed all functional tests; its existing 10,000-worker latency gate exceeded 100 ms only under concurrent shared-checkout load, then passed immediately in isolation.

## Product-wide keyboard contract

Completed `WEB-245` by making keyboard operability a registry-wide extension gate and correcting two shared focus defects found during live testing.

- Every registered page is parsed in English, German and Arabic and must retain the first-position skip link, named native controls, non-positive tab order, explicit safe button types, real link destinations, native disclosure summaries, resolvable `aria-controls` / `aria-activedescendant` references, named tree semantics, and named keyboard-scroll targets.
- The page-utility modal now uses the same focus trap and trigger restoration contract as the appearance preview and mobile navigation.
- The shared trap now removes hidden, inert and non-rendered responsive descendants from its focus boundary. Before the change, Shift+Tab in the 390 px mobile drawer attempted to focus a hidden control and left focus stranded on Close.
- `TestTodo_WEB_245` and its property, accessibility, browser, i18n, performance and regression matrix pass. The JS/WASM focus-trap tests pass under Go's Node harness, and native plus JS/WASM vet/build checks pass.
- The rebuilt production Go/WASM client was exercised in the Codex browser. Global search retained ArrowUp/ArrowDown/Enter/Escape behavior; the Promotion review modal retained Enter/Escape and trigger restoration; page utilities opened with Enter, wrapped in both directions and restored focus on Escape; and the 390x844 mobile drawer opened with Space, reverse-wrapped from Close to the final visible Admin link, and restored the drawer trigger on Escape.

The full product UI run completed with no functional or keyboard failures. Its pre-existing performance-only `TestInteractionLatencyGate` still exceeded the 10,000-worker query and 1000x12 data-table budgets, including when rerun alone; this keyboard slice does not change either query or table implementation.

## In-place remote table transitions

Completed `WEB-246` by moving sort, paging and page-size changes through one authoritative People-directory route transition and adding a reusable busy contract to the shared data table.

- The last authorized rows remain mounted and subtly dim while a sticky, themed spinner and localized `role=status` label acknowledge network work without replacing the page or shifting table geometry.
- The table viewport exposes `aria-busy`; reduced-motion, limited-animation, forced-colors and print modes have explicit behavior.
- Sort now uses the same cancellable loader and server-side preference persistence as pagination and page-size changes.
- The named primary, browser, accessibility, i18n, performance and regression matrix passes, as do focused route-state regressions, native vet and the JavaScript/WASM build.
- Manual Codex-browser testing covered a deliberately delayed sort transition, successful authoritative sort recovery, page-size change, paging, and the responsive People view at 390x844. The pending state kept the prior rows visible and announced `Updating table…`; the resolved state preserved the table, controls and page position.

## Restrained product motion

Completed `UIPOLISH-011` on 2026-09-15 by tightening the final shared motion cascade rather than adding page-local animation.

- Cold page entry now uses the normal duration; repeated table/work rows use the fast duration with no stagger, so usable content does not continue arriving in waves.
- Retained async pages now fade without translating the full region. Dense rows no longer shift horizontally on hover.
- Controls and surfaces animate only their actual paint/composite properties. Layout transitions remain restricted to the shell column, brand cluster and labels that participate in navigation collapse.
- The named primary, browser, accessibility, performance and regression matrix passes. Token and latency-gate packages pass, native vet passes, and the production Go/WASM asset was rebuilt.
- The Codex-browser pass exercised desktop navigation collapse/expand, grouped submenu collapse, action-launcher open/Escape/focus restoration, a real People sort, limited-motion preview, and the 390x844 and 320x740 breakpoints. No browser warnings or errors were emitted.
- The full `internal/humanwork/productui` package reached one unrelated existing failure: `TestTodo_WEB_046_Golden` expects an older Utility Drawer digest while concurrent shared-checkout work changes that component. No Utility Drawer source or golden was changed here.
