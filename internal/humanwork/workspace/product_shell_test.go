package workspace

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestProductLoadingShellSeedsMenuQueryForHydration(t *testing.T) {
	for _, query := range []string{"language", `a"<b>`, "  people  "} {
		doc, err := productShellDocumentForRouteQuery(JourneyConfig{Roles: []string{"comp_admin"}}, true, productui.ResolveProductLocale("en-US"), productui.PageHome, query)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(doc, `id="menu-filter"`)
		if start < 0 {
			t.Fatal("missing loading-shell filter")
		}
		end := strings.Index(doc[start:], ">")
		if end < 0 || !strings.Contains(doc[start:start+end], `value="`+html.EscapeString(strings.TrimSpace(query))+`"`) {
			t.Fatalf("loading-shell input lost or failed to escape query %q", query)
		}
	}
}

func TestTodo_UXAUDIT_024_SSRHonorsExplicitCollapsedNavigation(t *testing.T) {
	config := JourneyConfig{Tenant: "harborcare-demo", Roles: []string{"comp_admin"}}
	for _, test := range []struct {
		nav       string
		collapsed bool
	}{
		{nav: "collapsed", collapsed: true},
		{nav: "expanded", collapsed: false},
		{nav: "invalid", collapsed: false},
		{nav: "", collapsed: false},
	} {
		doc, err := productShellDocumentForRouteState(config, true, productui.ResolveProductLocale("en-US"), productui.PageHome, "promote", test.nav)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(doc, `class="app-shell`)
		if start < 0 {
			t.Fatal("loading shell is missing")
		}
		end := strings.IndexByte(doc[start:], '>')
		if end < 0 {
			t.Fatal("loading shell opening tag is malformed")
		}
		got := strings.Contains(doc[start:start+end], "nav-collapsed")
		if got != test.collapsed {
			t.Fatalf("nav=%q collapsed=%v, want %v", test.nav, got, test.collapsed)
		}
	}
}

func TestProductShellCarriesAuthenticatedLiveClientConfiguration(t *testing.T) {
	h, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome+"?nav=collapsed", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	config := island(t, recorder.Body.String())
	if config.TunnelURL != "ws://cell.test"+PathTunnel || config.Bearer != token || config.Tenant != shellTenant || config.Subject != shellSubject {
		t.Fatalf("product config = %+v", config)
	}
	if config.GiphyAPIKey != "" || strings.Contains(recorder.Header().Get("Content-Security-Policy"), "giphy.com") {
		t.Fatal("unconfigured product shell enabled GIPHY")
	}
	if !strings.Contains(recorder.Body.String(), `class="app-shell nav-collapsed`) {
		t.Fatal("explicit collapsed route did not shape the initial server-rendered shell")
	}
	for _, forbidden := range []string{"Maya Chen", "Northstar Group", "Validation passed", "Manager change completed"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("product shell contains fixture content %q", forbidden)
		}
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != ProductContentSecurityPolicy("cell.test") {
		t.Fatalf("product CSP = %q", got)
	}
}

func TestProductShellGiphyConfigAndCSPRequireExplicitPublicKey(t *testing.T) {
	h, token := newShellHandlerWithGiphyKey(t, "  giphy-public-client-key  ")
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := island(t, recorder.Body.String()).GiphyAPIKey; got != "giphy-public-client-key" {
		t.Fatalf("product GIPHY key = %q", got)
	}
	policy := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "https://api.giphy.com") || !strings.Contains(policy, "img-src 'self' blob: https://*.giphy.com") {
		t.Fatalf("configured GIPHY CSP missing provider sources: %q", policy)
	}

	journey := getJourney(t, h, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	if journey.Code != http.StatusOK || island(t, journey.Body.String()).GiphyAPIKey != "" {
		t.Fatalf("journey shell unexpectedly carried the product GIPHY key: status=%d", journey.Code)
	}
}

func TestGiphyAPIKeyCanBeConfiguredByEnvironment(t *testing.T) {
	t.Setenv(EnvGiphyAPIKey, "  operator-public-key  ")
	h, _ := newShellHandler(t, false)
	if h.giphyAPIKey != "operator-public-key" {
		t.Fatalf("environment GIPHY key = %q", h.giphyAPIKey)
	}
}

func TestProductRouteAuthorizationRejectsHiddenPageButAllowsVisiblePage(t *testing.T) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{Issuer: shellIssuer, Audience: shellAudience, Subject: "omar-reyes", SubjectKind: "human", Tenant: shellTenant, OrganizationScopeID: "org:acme:people", Roles: []string{"worker_self"}, Purposes: []string{"self_service_view"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-ic", IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{Cell: unreachableCell{t: t}, Config: transport.Config{Verifier: verifier, Audience: shellAudience, Now: func() time.Time { return shellNow }}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://cell.test"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, req)
		return recorder
	}
	if got := request(PathProductHome).Code; got != http.StatusOK {
		t.Fatalf("visible home = %d", got)
	}
	if got := request("/workspace/app/admin").Code; got != http.StatusForbidden {
		t.Fatalf("hidden admin = %d, want 403", got)
	}
}

func TestProductShellDeclaresInitialLocaleBeforeWASMBoot(t *testing.T) {
	h, token := newShellHandler(t, false)
	tests := []struct {
		locale string
		want   []string
	}{
		{locale: "ar", want: []string{`lang="ar"`, `dir="rtl"`, `data-hcm-locale="ar"`, `data-hcm-catalog="product-ui.v1"`, `data-hcm-message-fallback="en-US"`}},
		{locale: "zz", want: []string{`lang="en-US"`, `dir="ltr"`, `data-hcm-locale="en-US"`, `data-hcm-locale-fallback="unsupported_locale"`}},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome+"?locale="+test.locale, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
			}
			for _, want := range test.want {
				if !strings.Contains(recorder.Body.String(), want) {
					t.Errorf("product shell missing %q", want)
				}
			}
		})
	}
}

func TestProductShellDeclaresColorModeBeforeWASMBoot(t *testing.T) {
	doc, err := productShellDocument(JourneyConfig{TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-color-mode="system"`, `<meta name="color-scheme" content="light dark">`} {
		if !strings.Contains(doc, want) {
			t.Errorf("product shell missing initial color-mode contract %q", want)
		}
	}
}

func TestProductShellRefusesUnknownAndUnauthenticatedRoutes(t *testing.T) {
	h, token := newShellHandler(t, false)
	unknown := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductPrefix+"unknown", nil)
	unknown.Header.Set("Authorization", "Bearer "+token)
	unknownRecorder := httptest.NewRecorder()
	h.ServeHTTP(unknownRecorder, unknown)
	if unknownRecorder.Code != http.StatusNotFound {
		t.Fatalf("unknown product page = %d", unknownRecorder.Code)
	}

	unauthenticated := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
	unauthenticatedRecorder := httptest.NewRecorder()
	h.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated product page = %d", unauthenticatedRecorder.Code)
	}
}

func TestProductShellServesNestedRegisteredRoutesOnColdReload(t *testing.T) {
	handler, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test/workspace/app/admin/organization-visibility", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET nested product route = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Organization visibility") {
		t.Fatalf("nested product route rendered the wrong shell: %s", recorder.Body.String())
	}
}

func TestProductShellUsesOnlyPinnedStylesAndSharedWASMClient(t *testing.T) {
	doc, err := productShellDocument(JourneyConfig{TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<style>`, PathWasmExec, PathJourneyWasm, `id="` + JourneyRootElementID + `"`, `id="` + JourneyConfigElementID + `"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("product shell missing %q", want)
		}
	}
	for _, want := range []string{"--jn-accent", ".jn-embedded", ".app-shell", "--accent:#006b57"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("product shell does not carry integrated journey style %q", want)
		}
	}
	policy := ProductContentSecurityPolicy("cell.test")
	if strings.Contains(policy, "'unsafe-inline'") || !strings.Contains(policy, productStylesheetHash) || !strings.Contains(policy, journeyLoaderHash) || !strings.Contains(policy, "img-src 'self'") {
		t.Fatalf("product CSP is not pinned: %s", policy)
	}
}

func TestProductShellUsesTheRouteShapedLoadingProxyBeforeWASMStarts(t *testing.T) {
	config := JourneyConfig{
		TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
		Tenant: "harborcare-demo", Subject: "local-developer", Purpose: "compensation_review",
	}
	doc, err := productShellDocumentForRoute(config, true, productui.ResolveProductLocale("en-US"), productui.PageHistory)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell is-loading"`, `class="loading-proxy loading-proxy-history"`,
		`aria-busy="true"`, "Loading your workspace data", "Harborcare Demo",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("initial product shell missing %q", want)
		}
	}
	if strings.Contains(doc, "0 promotion journeys are visible") {
		t.Fatal("initial product shell exposed an unresolved count as zero")
	}
	// UXAUDIT-007: config.Purpose ("compensation_review" -> "Compensation
	// Review") is the admitted principal's authorized data-processing
	// purpose, not the shell's authorized scope. This test previously
	// asserted the opposite -- that "Compensation Review" appears in this
	// route-agnostic loading shell, shown for every product page -- which
	// was the exact pattern the live audit flagged as RED. The purpose
	// still reaches the Promotion journey's own masthead; it never reaches
	// this shared shell again.
	if strings.Contains(doc, "Compensation Review") {
		t.Fatal("initial product shell still leaks the task-specific purpose into its route-agnostic scope label")
	}
}

func TestTodo_UXAUDIT_003_ServerProjection(t *testing.T) {
	permissions := []roleaccess.PagePermission{
		{PageID: string(productui.PagePeople), View: true},
		{PageID: string(productui.PageJourneys), View: true, Create: true},
	}
	actions := resolveProductLauncherActions(true, permissions)
	if len(actions) != 1 || actions[0].ID != productui.SemanticActionPromoteWorker || actions[0].Availability != string(productui.ActionAvailable) {
		t.Fatalf("authorized semantic action projection = %+v", actions)
	}
	for name, candidate := range map[string][]roleaccess.PagePermission{
		"unconfigured": permissions,
		"no people":    {{PageID: string(productui.PageJourneys), View: true, Create: true}},
		"no create":    {{PageID: string(productui.PagePeople), View: true}, {PageID: string(productui.PageJourneys), View: true}},
	} {
		configured := name != "unconfigured"
		if got := resolveProductLauncherActions(configured, candidate); len(got) != 0 {
			t.Fatalf("%s policy projected promotion: %+v", name, got)
		}
	}
	config := JourneyConfig{LauncherActions: actions}
	viewActions := productLauncherActions(config.LauncherActions)
	if len(viewActions) != 1 || viewActions[0].State.Availability != productui.ActionAvailable {
		t.Fatalf("SSR launcher projection = %+v", viewActions)
	}
}

func TestTodo_UXAUDIT_003_AuthenticatedShellProjection(t *testing.T) {
	h, token := newShellHandler(t, false)
	store := launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{
		{RoleID: "comp_admin", PageID: string(productui.PageHome), View: true},
		{RoleID: "comp_admin", PageID: string(productui.PagePeople), View: true},
		{RoleID: "comp_admin", PageID: string(productui.PageJourneys), View: true, Create: true},
	}}}
	h.roleAccess = store
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET product shell = %d: %s", response.Code, response.Body.String())
	}
	config := island(t, response.Body.String())
	if len(config.LauncherActions) != 1 || config.LauncherActions[0].ID != productui.SemanticActionPromoteWorker {
		t.Fatalf("authenticated shell launcher projection = %+v", config.LauncherActions)
	}

	store.snapshot.PagePermissions[2].Create = false
	h.roleAccess = store
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET read-only product shell = %d: %s", response.Code, response.Body.String())
	}
	if denied := island(t, response.Body.String()).LauncherActions; len(denied) != 0 {
		t.Fatalf("read-only shell disclosed semantic action: %+v", denied)
	}
}

type launcherRoleAccessStore struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
}

func (s launcherRoleAccessStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, nil
}

// TestTodo_UXAUDIT_007_Regression proves, from this package's own caller
// path (productShellDocumentForRouteQuery, the SSR loading shell every
// product route renders before the WASM client takes over), that a task-
// specific purpose can no longer reach the shell's rendered, persistent
// page-identity region -- for any registered page, not just the one case
// above. It asserts by value: a distinctive purpose that could not have
// arrived any other way must be entirely absent from the rendered <div
// id="app"> region, while the tenant and subject display facts the same
// call carries stay present, so this is not merely proof that the loading
// shell rendered nothing at all.
//
// The check is deliberately scoped to the rendered region and not the whole
// document: the JSON configuration island later in the same document (see
// JourneyConfigElementID) still carries the raw, unrendered Purpose field
// verbatim, by design -- that island is what lets the WASM client's own
// Promotion journey masthead (tools/uxqual/render/journey's principalChip)
// surface the purpose inside the affected workflow, which is exactly where
// GREEN says it belongs. Only the shell's own rendered chrome must stay
// quiet about it.
func TestTodo_UXAUDIT_007_Regression(t *testing.T) {
	const distinctivePurpose = "zzz_uxaudit007_distinctive_purpose_marker"
	for _, page := range []productui.PageID{productui.PageHome, productui.PageHistory, productui.PagePeople, productui.PageSettings} {
		config := JourneyConfig{
			TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
			Tenant: "harborcare-demo", Subject: "local-developer", Purpose: distinctivePurpose,
		}
		doc, err := productShellDocumentForRoute(config, true, productui.ResolveProductLocale("en-US"), page)
		if err != nil {
			t.Fatalf("page %s: %v", page, err)
		}
		rendered, configIsland := splitAtConfigIsland(t, doc)
		if strings.Contains(rendered, distinctivePurpose) {
			t.Fatalf("page %s: caller-supplied purpose %q reached the rendered shell", page, distinctivePurpose)
		}
		if strings.Contains(rendered, "Distinctive Purpose Marker") {
			t.Fatalf("page %s: humanized form of the purpose reached the rendered shell", page)
		}
		// The tenant display fact survives on every page. A raw subject is
		// not an authorized viewer profile and must not be humanized into a
		// personalized Home greeting before that profile resolves.
		if !strings.Contains(rendered, "Harborcare Demo") {
			t.Fatalf("page %s: fix over-suppressed the shell -- missing tenant %q", page, "Harborcare Demo")
		}
		if page == productui.PageHome && strings.Contains(rendered, "Local Developer") {
			t.Fatalf("page %s: raw subject was mistaken for a viewer name", page)
		}
		// The purpose still belongs in the configuration island the WASM
		// client reads to build the affected workflow's own masthead: this
		// is the deliberate exception, not a second leak.
		if !strings.Contains(configIsland, distinctivePurpose) {
			t.Fatalf("page %s: configuration island lost the purpose the affected-workflow masthead needs", page)
		}
	}
}

// splitAtConfigIsland separates the rendered app region from the JSON
// configuration island a governed page's shell writes after it (see
// JourneyConfigElementID), so a test can require different things of each.
func splitAtConfigIsland(t *testing.T, doc string) (rendered, configIsland string) {
	t.Helper()
	marker := `<script type="application/json" id="` + JourneyConfigElementID + `">`
	at := strings.Index(doc, marker)
	if at < 0 {
		t.Fatalf("document has no %s configuration island", JourneyConfigElementID)
	}
	return doc[:at], doc[at:]
}

// TestAssignedJourneyDetailAdmitsOnlyTheDetail: a finance approver has My
// Work but not Journeys, and "Open live journey" on their queue led to "Page
// unavailable". One journey's detail is admitted to a My Work viewer; the
// journeys list, and every viewer without My Work, still are not.
func TestAssignedJourneyDetailAdmitsOnlyTheDetail(t *testing.T) {
	finance := productAccess{roles: []string{"finance_partner"}}
	if !productui.PageVisible(productui.PageWork, finance.roles) || productui.PageVisible(productui.PageJourneys, finance.roles) {
		t.Skip("finance_partner no longer has My Work without Journeys")
	}
	detail := map[string][]string{"journey": {"01a0b867-e24b-75b6-961b-5a87e6127ac8"}}
	if !assignedJourneyDetail(productui.PageJourneys, detail, finance) {
		t.Fatal("a My Work viewer was refused the detail of a journey")
	}
	if assignedJourneyDetail(productui.PageJourneys, map[string][]string{}, finance) ||
		assignedJourneyDetail(productui.PageJourneys, map[string][]string{"journey": {" "}}, finance) {
		t.Fatal("the journeys list was admitted through the detail exception")
	}
	if assignedJourneyDetail(productui.PagePeople, detail, finance) {
		t.Fatal("the exception reached a page other than Journeys")
	}
	if assignedJourneyDetail(productui.PageJourneys, detail, productAccess{roles: []string{"worker_self"}}) &&
		!productui.PageVisible(productui.PageWork, []string{"worker_self"}) {
		t.Fatal("a viewer without My Work was admitted")
	}
}
