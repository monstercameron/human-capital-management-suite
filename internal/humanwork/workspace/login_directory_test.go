package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// stubDevDirectory is the whole reason DevDirectory is a port rather than a
// read through Cell: the sign-in page's directory is exercised here without a
// database, a seed package or a credential, and newShellHandler's
// unreachableCell (whose ReadPromotion calls t.Fatal) proves the page never
// reaches the workforce read chain while rendering any of it.
type stubDevDirectory struct{ snapshot DevDirectorySnapshot }

func (stub stubDevDirectory) DevDirectorySnapshot() DevDirectorySnapshot { return stub.snapshot }

// directoryFixture is a three-level company: a root, one division under it
// and one department under that, so the default disclosure policy (open to
// level two) has something on each side of the line.
func directoryFixture() DevDirectorySnapshot {
	return DevDirectorySnapshot{
		Units: []DevDirectoryUnit{
			{Code: "harborcare", Name: "HarborCare Health Services"},
			{Code: "care-operations", Name: "Care Operations", ParentCode: "harborcare"},
			{Code: "clinical-operations", Name: "Clinical Operations", ParentCode: "care-operations"},
		},
		Employees: []DevDirectoryEmployee{
			{PersonaID: DevEmployeePersonaID("hc-001-amina-rahman"), WorkerKey: "hc-001-amina-rahman", Name: "Amina Rahman", WorkerNumber: "HC-21001", JobTitle: "Chief Executive Officer", UnitCode: "harborcare", ManagerKey: "board:harborcare"},
			{PersonaID: DevEmployeePersonaID("hc-002-mateo-alvarez"), WorkerKey: "hc-002-mateo-alvarez", Name: "Mateo Alvarez", WorkerNumber: "HC-21002", JobTitle: "Director of Clinical Operations", UnitCode: "clinical-operations", ManagerKey: "hc-001-amina-rahman"},
			{PersonaID: DevEmployeePersonaID("hc-003-evelyn-morgan"), WorkerKey: "hc-003-evelyn-morgan", Name: "Evelyn Morgan", WorkerNumber: "HC-21003", JobTitle: "Registered Nurse", UnitCode: "clinical-operations", ManagerKey: "hc-002-mateo-alvarez"},
		},
	}
}

// newDirectoryHandler wires the fixture plus one server-held credential per
// fixture employee, the way the composition root does.
func newDirectoryHandler(t *testing.T) *Handler {
	t.Helper()
	h, _ := newShellHandler(t, true)
	snapshot := directoryFixture()
	h.directory = stubDevDirectory{snapshot: snapshot}
	h.devPersonas = map[string]DevPersona{}
	for _, employee := range snapshot.Employees {
		// One employee per bundle shape, so the role filter has something to
		// discriminate between: an executive, a people manager and a plain
		// employee.
		bundle := DevEmployeeBundleSelf
		switch {
		case strings.HasPrefix(employee.JobTitle, "Chief"):
			bundle = DevEmployeeBundleExecutive
		case strings.HasPrefix(employee.JobTitle, "Director"):
			bundle = DevEmployeeBundleManager
		}
		access, ok := DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("the %q bundle did not resolve", bundle)
		}
		h.devPersonas[employee.PersonaID] = DevPersona{
			ID: employee.PersonaID, Name: employee.Name, Access: access.Label,
			Roles: access.Roles, WorkerRef: employee.WorkerKey,
			Token: frontendE2EToken(t, employee.WorkerKey, access.Roles),
		}
	}
	return h
}

func getLoginPage(t *testing.T, h *Handler, query string) string {
	t.Helper()
	return getFilteredLoginPage(t, h, query, "")
}

func getFilteredLoginPage(t *testing.T, h *Handler, query, role string) string {
	t.Helper()
	values := url.Values{}
	if query != "" {
		values.Set(paramDirectoryQuery, query)
	}
	if role != "" {
		values.Set(paramDirectoryRole, role)
	}
	target := "http://cell.test" + PathLogin
	if len(values) > 0 {
		target += "?" + values.Encode()
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("login page status = %d", recorder.Code)
	}
	return recorder.Body.String()
}

func TestLoginDirectoryOffersEverySeededEmployeeAsASignInControl(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getLoginPage(t, h, "")
	for _, employee := range directoryFixture().Employees {
		control := `<button type="submit" name="` + paramLoginPersona + `" value="` + employee.PersonaID + `">Sign in as ` + employee.Name + `</button>`
		if !strings.Contains(body, control) {
			t.Errorf("no sign-in control for %s (%s)", employee.Name, employee.PersonaID)
		}
		for _, fact := range []string{employee.WorkerNumber, employee.JobTitle} {
			if !strings.Contains(body, fact) {
				t.Errorf("%s is rendered without %q, so a tester cannot tell them apart", employee.Name, fact)
			}
		}
	}
	// The manager is named, not just referenced by key, and the person with
	// no seeded manager says so rather than rendering a dangling key.
	if !strings.Contains(body, "reports to Amina Rahman") {
		t.Error("the directory does not name the manager a tester would use to disambiguate")
	}
	if !strings.Contains(body, "no seeded manager") {
		t.Error("an employee whose manager is not a seeded worker rendered a dangling reference")
	}
	// The per-employee forms must not reuse the quick-pick card's class:
	// personaCards splits the document on it and counts the result.
	if strings.Count(body, `<form class="persona"`) != 0 {
		t.Fatal("the directory rendered forms the quick-pick persona-card parser would count")
	}
}

func TestLoginDirectoryNamesTheRolesEachEmployeeWouldSignInWith(t *testing.T) {
	h := newDirectoryHandler(t)
	card := directorySearchResults(t, getLoginPage(t, h, "Amina"))
	executive, _ := DevEmployeeBundleAccess(DevEmployeeBundleExecutive)
	for _, label := range devRoleLabels(executive.Roles) {
		if !strings.Contains(card, label) {
			t.Errorf("the chief executive's card omits the role %q its credential carries", label)
		}
	}
	// The promise is read off the server-held persona, not off the directory
	// row, so it cannot outrun the bundle actually signed into the token.
	h.devPersonas[DevEmployeePersonaID("hc-001-amina-rahman")] = DevPersona{
		ID: DevEmployeePersonaID("hc-001-amina-rahman"), Name: "Amina Rahman",
		Roles: []string{"worker_self"}, Token: frontendE2EToken(t, "hc-001-amina-rahman", []string{"worker_self"}),
	}
	narrowed := directorySearchResults(t, getLoginPage(t, h, "Amina"))
	if strings.Contains(narrowed, "Compensation administrator") {
		t.Fatal("the card kept an administrative promise after the credential behind it narrowed")
	}
	if !strings.Contains(narrowed, "Employee self-service") {
		t.Fatal("the card does not name the role its narrowed credential actually carries")
	}
}

func TestLoginDirectorySearchFiltersOnEveryOfferedFact(t *testing.T) {
	h := newDirectoryHandler(t)
	for name, query := range map[string]string{
		"name":          "evelyn",
		"worker number": "HC-21003",
		"job title":     "registered nurse",
		"org unit":      "Clinical Operations",
	} {
		results := directorySearchResults(t, getLoginPage(t, h, query))
		if !strings.Contains(results, "Evelyn Morgan") {
			t.Errorf("searching by %s (%q) did not find the matching employee", name, query)
		}
		// "Amina Rahman" also appears as somebody else's manager, so the
		// unrelated-result check has to look at the sign-in control.
		if strings.Contains(results, "Sign in as Amina Rahman") {
			t.Errorf("searching by %s (%q) returned an unrelated employee", name, query)
		}
	}
	empty := getLoginPage(t, h, "nobody-by-that-name")
	if !strings.Contains(empty, "0 of 3 employees match") {
		t.Fatalf("an unmatched search did not report its result count: %s", directorySummary(t, empty))
	}
	if strings.Contains(directorySearchResults(t, empty), "Sign in as ") {
		t.Fatal("an unmatched search still listed sign-in controls")
	}
	if !strings.Contains(empty, `<a class="directory-clear" href="`+PathLogin+`">Clear filters</a>`) {
		t.Fatal("an active search offers no way back to the whole directory")
	}
	// The query is addressable state on a native GET form, so it must come
	// back in the control that produced it.
	if !strings.Contains(empty, `name="`+paramDirectoryQuery+`" value="nobody-by-that-name"`) {
		t.Fatal("the search field did not retain the submitted query")
	}
}

func TestLoginDirectoryTreeIsANestedCollapsibleOrganization(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getLoginPage(t, h, "")
	for _, want := range []string{
		`<ul class="org-tree" role="tree" aria-labelledby="directory-tree-heading">`,
		`<ul role="group">`,
		`<li class="org-unit" role="treeitem" aria-level="1" aria-expanded="true">`,
		`<li class="org-unit" role="treeitem" aria-level="2" aria-expanded="true">`,
		`<li class="org-unit" role="treeitem" aria-level="3" aria-expanded="false">`,
		`<li class="org-person" role="treeitem" aria-level="2">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the organization tree is missing %q", want)
		}
	}
	// Headcount rolls up: the root covers all three people, the department
	// two, and the singular form is used for one.
	for _, want := range []string{"HarborCare Health Services</span> <span class=\"org-count\">3 people", "Clinical Operations</span> <span class=\"org-count\">2 people"} {
		if !strings.Contains(body, want) {
			t.Errorf("the tree does not roll up headcount: missing %q", want)
		}
	}
	// Sixty people across twenty units is unreadable fully expanded, so
	// departments start closed - and a search must override that rather than
	// hiding its own answer behind a disclosure.
	searched := getLoginPage(t, h, "Evelyn")
	if !strings.Contains(searched, `<li class="org-unit" role="treeitem" aria-level="3" aria-expanded="true">`) {
		t.Fatal("a unit holding a search match stayed collapsed")
	}
	if strings.Contains(searched, `aria-level="3" aria-expanded="false"`) {
		t.Fatal("the matching department is rendered both open and closed")
	}
	unmatched := getLoginPage(t, h, "nobody-by-that-name")
	if strings.Contains(unmatched, `aria-expanded="true"`) {
		t.Fatal("a search with no matches still expanded units")
	}
	// No script may appear on this page at all: it is served under
	// default-src 'none' with one style hash and no script source.
	if strings.Contains(body, "<script") || strings.Contains(body, " onclick") || strings.Contains(body, ` style=`) {
		t.Fatal("the directory emitted script, an inline handler or an inline style the login CSP forbids")
	}
	if got := loginCSPOf(t, h); !strings.Contains(got, "script-src 'none'") || !strings.Contains(got, "style-src '"+sha256Source(loginStylesheet())+"'") {
		t.Fatalf("the directory changed the login policy: %q", got)
	}
}

func TestLoginDirectorySignInUsesTheServerOwnedCredential(t *testing.T) {
	h := newDirectoryHandler(t)
	// Resolve access through the durable policy the product shell enforces,
	// so this test pins the sign-in path rather than the page registry's
	// separate visibility floor.
	h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}
	id := DevEmployeePersonaID("hc-003-evelyn-morgan")
	token := h.devPersonas[id].Token
	form := url.Values{paramLoginPersona: {id}}
	request := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != PathProductPrefix+"myself" {
		t.Fatalf("employee sign-in = %d to %q, want 303 to the self-service landing", recorder.Code, recorder.Header().Get("Location"))
	}
	if strings.Contains(recorder.Body.String(), token) {
		t.Fatal("the employee credential entered the response body")
	}
	var hardened bool
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			hardened = cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode && cookie.Value == token
		}
	}
	if !hardened {
		t.Fatal("employee sign-in did not set the hardened session cookie")
	}
	// A worker binding that disagrees with the verified subject is refused
	// before any access is resolved, exactly as it is for a quick pick.
	h.devPersonas[id] = DevPersona{ID: id, Name: "Evelyn Morgan", WorkerRef: "somebody-else", Token: token}
	rejected := httptest.NewRecorder()
	mismatch := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	mismatch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rejected, mismatch)
	if rejected.Code != http.StatusUnauthorized || len(rejected.Result().Cookies()) != 0 {
		t.Fatalf("mismatched employee binding signed in: %d, cookies %d", rejected.Code, len(rejected.Result().Cookies()))
	}
}

func TestLoginDirectorySecurityExistsOnlyForTheDevBrowserLoginSurface(t *testing.T) {
	// A production-shaped composition: the workspace is served, the login
	// flag is off, and a directory is supplied anyway. Nothing of it may
	// exist - not the route, not the markup, not the reader on the handler.
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	production, err := NewHandler(Options{
		Cell:   unreachableCell{t: t},
		Config: transport.Config{Verifier: verifier, Audience: shellAudience, Now: func() time.Time { return shellNow }},
		Now:    func() time.Time { return shellNow },
		// DevBrowserLogin deliberately left off.
		DevDirectory: stubDevDirectory{snapshot: directoryFixture()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if production.directory != nil {
		t.Fatal("a production-shaped cell retained the development directory reader")
	}
	recorder := httptest.NewRecorder()
	production.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil))
	if recorder.Code == http.StatusOK {
		t.Fatalf("the sign-in route answered %d on a cell with no dev browser login", recorder.Code)
	}
	for _, leaked := range []string{"Amina Rahman", "HC-21001", `<ul class="org-tree"`, `<form class="directory-signin"`} {
		if strings.Contains(recorder.Body.String(), leaked) {
			t.Errorf("a production-shaped refusal disclosed %q", leaked)
		}
	}
	if production.loginDirectorySection("", "") != "" {
		t.Fatal("the directory renders even with no reader composed")
	}

	// With the flag on but no directory composed, the page is the page it
	// was before this change: quick picks and the credential fallback.
	noDirectory, _ := newShellHandler(t, true)
	body := getLoginPage(t, noDirectory, "")
	// The class names themselves appear in the hash-pinned inline
	// stylesheet, which is always emitted; the markup is what must be gone.
	for _, absent := range []string{`<form class="directory-search"`, `<ul class="org-tree"`, `<form class="directory-signin"`, "Sign in as a seeded employee"} {
		if strings.Contains(body, absent) {
			t.Errorf("a cell with no directory rendered %q", absent)
		}
	}
}

func TestLoginDirectoryNeverDisclosesACredentialOrOffersAnUnbackedOne(t *testing.T) {
	h := newDirectoryHandler(t)
	// One employee the directory knows about for whom no credential exists.
	snapshot := directoryFixture()
	snapshot.Employees = append(snapshot.Employees, DevDirectoryEmployee{
		PersonaID: DevEmployeePersonaID("hc-004-darius-bennett"), WorkerKey: "hc-004-darius-bennett",
		Name: "Darius Bennett", WorkerNumber: "HC-21004", JobTitle: "Chief People Officer", UnitCode: "harborcare",
	})
	h.directory = stubDevDirectory{snapshot: snapshot}
	body := getLoginPage(t, h, "")
	for _, persona := range h.devPersonas {
		if persona.Token == "" {
			continue
		}
		if strings.Contains(body, persona.Token) {
			t.Fatalf("the directory disclosed %s's credential", persona.ID)
		}
	}
	if strings.Contains(body, "Sign in as Darius Bennett") {
		t.Fatal("an employee with no server-held credential was offered a sign-in control")
	}
	if !strings.Contains(body, "No development credential is composed for this employee.") {
		t.Fatal("an employee with no credential rendered no explanation of why")
	}
	// A request naming that id is refused the same way any unknown persona
	// is, without disclosing which part of it was wrong.
	form := url.Values{paramLoginPersona: {DevEmployeePersonaID("hc-004-darius-bennett")}}
	request := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("an employee id with no credential signed in: %d, cookies %d", recorder.Code, len(recorder.Result().Cookies()))
	}
}

func TestLoginDirectoryEscapesDirectoryTextAndBoundsTheQuery(t *testing.T) {
	h := newDirectoryHandler(t)
	h.directory = stubDevDirectory{snapshot: DevDirectorySnapshot{
		Units: []DevDirectoryUnit{{Code: "unit", Name: `Care</summary><script>alert(1)</script>`}},
		Employees: []DevDirectoryEmployee{{
			PersonaID: "employee:x", WorkerKey: "x", Name: `"><img src=x onerror=alert(1)>`,
			WorkerNumber: "HC-1", JobTitle: "<b>Nurse</b>", UnitCode: "unit",
		}},
	}}
	h.devPersonas["employee:x"] = DevPersona{ID: "employee:x", Name: "x", Roles: []string{"worker_self"}, Token: frontendE2EToken(t, "x", []string{"worker_self"})}
	// Unfiltered, so the hostile row is actually rendered: a filtered page
	// hides non-matching people, which would prove nothing about escaping.
	body := getLoginPage(t, h, "")
	for _, forbidden := range []string{"<script>alert(1)</script>", "<img src=x", "<b>Nurse</b>", "</summary><script"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("hostile directory text survived unescaped: %q", forbidden)
		}
	}
	if !strings.Contains(body, "&lt;b&gt;Nurse&lt;/b&gt;") {
		t.Error("escaped job title is missing, so the text was dropped rather than escaped")
	}
	// The submitted filters are reflected into controls and prose, so they
	// are escaped on the way back out too.
	reflected := getFilteredLoginPage(t, h, `"><script>alert(1)</script>`, `"><script>alert(2)</script>`)
	for _, forbidden := range []string{"<script>alert(1)</script>", "<script>alert(2)</script>"} {
		if strings.Contains(reflected, forbidden) {
			t.Errorf("hostile filter text survived unescaped: %q", forbidden)
		}
	}

	long := strings.Repeat("n", maxDirectoryQueryBytes+40)
	if got := normalizeDirectoryQuery("  " + long + "  "); len(got) != maxDirectoryQueryBytes {
		t.Fatalf("query length = %d, want the %d-byte bound", len(got), maxDirectoryQueryBytes)
	}
	if got := normalizeDirectoryQuery("   "); got != "" {
		t.Fatalf("blank query = %q, want empty so the whole directory renders", got)
	}
	bounded := getLoginPage(t, h, long)
	if strings.Contains(bounded, long) {
		t.Fatal("an unbounded query was reflected into the document")
	}
}

// directorySearchResults returns just the match list, so a search assertion
// cannot accidentally pass on a name that appears in the always-rendered tree
// below it.
func directorySearchResults(t *testing.T, body string) string {
	t.Helper()
	const open = `<ul class="directory-results">`
	start := strings.Index(body, open)
	if start < 0 {
		return ""
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, `<h2 id="directory-tree-heading">`)
	if end < 0 {
		t.Fatal("the result list is not followed by the organization tree")
	}
	return rest[:end]
}

func directorySummary(t *testing.T, body string) string {
	t.Helper()
	const open = `<p class="directory-summary" role="status">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatal("the directory rendered no search summary")
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, "</p>")
	if end < 0 {
		t.Fatal("the search summary is unterminated")
	}
	return rest[:end]
}

func loginCSPOf(t *testing.T, h *Handler) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil))
	return recorder.Header().Get("Content-Security-Policy")
}

// bundleAccess resolves one role bundle against the registry's own default
// permissions - the snapshot a dev persona, which carries no per-tenant
// override, actually resolves to.
func bundleAccess(roles []string) productAccess {
	return productAccess{
		configured:  true,
		roles:       roles,
		permissions: roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, roles),
	}
}

// TestPersonaAccessTitleNamesEachBundleAsItself is the regression for a live
// defect: the finance-partner persona rendered the access label "Individual
// contributor" and the copy "View your employment profile" - byte-identical to
// the worker_self persona, whose credential is strictly smaller. The old
// ladder was purely capability-shaped (Admin, else People, else Myself) and a
// finance partner has none of the first two, so it fell through. An HR
// partner would have collided with "Hiring manager" the same way.
func TestPersonaAccessTitleNamesEachBundleAsItself(t *testing.T) {
	titles := map[DevEmployeeBundle]string{
		DevEmployeeBundleExecutive: "HCM administrator",
		DevEmployeeBundlePeopleOps: "HR partner",
		DevEmployeeBundleFinance:   "Finance partner",
		DevEmployeeBundleManager:   "Hiring manager",
		DevEmployeeBundleSelf:      "Individual contributor",
	}
	seen := map[string]DevEmployeeBundle{}
	for bundle, want := range titles {
		access, ok := DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("bundle %q did not resolve", bundle)
		}
		got := personaAccessTitle(bundleAccess(access.Roles))
		if got != want {
			t.Errorf("bundle %q titled %q, want %q", bundle, got, want)
		}
		if other, clash := seen[got]; clash {
			t.Errorf("bundles %q and %q both render the title %q", other, bundle, got)
		}
		seen[got] = bundle
	}
	// The label is a name for grants access already holds, never a claim it
	// does not: a credential whose tenant admits nothing keeps no title.
	for _, roles := range [][]string{{"finance_partner"}, {"hr_partner"}} {
		narrowed := productAccess{configured: true, roles: roles}
		if got := personaAccessTitle(narrowed); got != "Workspace member" {
			t.Errorf("roles %v kept the title %q after their pages were revoked", roles, got)
		}
	}
}

// TestLoginFinancePartnerCardIsNotLabelledSelfService drives the same defect
// through the real sign-in page rather than the helper alone.
func TestLoginFinancePartnerCardIsNotLabelledSelfService(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}
	h.devPersonas = map[string]DevPersona{}
	// personaCards parses the whole grid, so every canonical slot is filled.
	for _, set := range DevPersonaRoleSets() {
		h.devPersonas[set.ID] = DevPersona{
			ID: set.ID, Name: "Worker " + set.ID, Roles: set.Roles,
			Token: frontendE2EToken(t, set.ID+"-label", set.Roles),
		}
	}
	cards := personaCards(t, getLoginPage(t, h, ""), "finance-partner", "individual-contributor")
	finance, self := cards["finance-partner"], cards["individual-contributor"]
	if !strings.Contains(finance, "Finance partner") {
		t.Error("the finance persona card does not name what the credential is")
	}
	if strings.Contains(finance, "Individual contributor") || strings.Contains(finance, "View your employment profile") {
		t.Error("the finance persona card is still labelled as self-service")
	}
	if !strings.Contains(self, "Individual contributor") || !strings.Contains(self, "View your employment profile") {
		t.Error("the self-service persona card lost its own copy")
	}
	if finance == self {
		t.Fatal("two disjoint role bundles render identical persona copy")
	}
}

// roleFilterFixtureBundles mirrors newDirectoryHandler's assignment: Amina is
// an executive, Mateo a people manager, Evelyn a plain employee.
func roleFilterFixtureBundles() map[string]DevEmployeeBundle {
	return map[string]DevEmployeeBundle{
		"Amina Rahman":  DevEmployeeBundleExecutive,
		"Mateo Alvarez": DevEmployeeBundleManager,
		"Evelyn Morgan": DevEmployeeBundleSelf,
	}
}

func TestLoginDirectorySearchMatchesRoleLabels(t *testing.T) {
	h := newDirectoryHandler(t)
	// A role label on its own is enough: "somebody who can approve finance"
	// and "a plain employee" are searches a tester actually makes, and the
	// words they type are the ones the card already shows them.
	for query, want := range map[string]string{
		"employee self-service":    "Evelyn Morgan",
		"people manager":           "Mateo Alvarez",
		"compensation administrat": "Amina Rahman",
		"promotion operator":       "Amina Rahman",
	} {
		results := directorySearchResults(t, getLoginPage(t, h, query))
		if !strings.Contains(results, "Sign in as "+want) {
			t.Errorf("searching the role label %q did not find %s", query, want)
		}
		for _, other := range []string{"Amina Rahman", "Mateo Alvarez", "Evelyn Morgan"} {
			if other != want && strings.Contains(results, "Sign in as "+other) {
				t.Errorf("searching the role label %q also returned %s", query, other)
			}
		}
	}
	// Role labels join the other facts as one more alternative, so a query
	// that matches a name still matches by name.
	if results := directorySearchResults(t, getLoginPage(t, h, "alvarez")); !strings.Contains(results, "Sign in as Mateo Alvarez") {
		t.Error("adding role labels to the search broke matching by name")
	}
	// The labels searched are exactly the labels rendered.
	access, _ := DevEmployeeBundleAccess(roleFilterFixtureBundles()["Mateo Alvarez"])
	for _, label := range devRoleLabels(access.Roles) {
		if !strings.Contains(directorySearchResults(t, getLoginPage(t, h, strings.ToLower(label))), "Sign in as Mateo Alvarez") {
			t.Errorf("the rendered role label %q is not searchable", label)
		}
	}
}

func TestLoginDirectoryRoleFilterNarrowsResultsAndTree(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getFilteredLoginPage(t, h, "", "manager")
	if !strings.Contains(body, "1 of 3 employees hold the People manager role.") {
		t.Fatalf("role-only filter did not report its own question: %q", directorySummary(t, body))
	}
	if !strings.Contains(body, "Sign in as Mateo Alvarez") {
		t.Error("the role filter dropped the only employee holding the role")
	}
	// Non-holders are gone from the whole surface, results and tree alike.
	for _, absent := range []string{"Sign in as Amina Rahman", "Sign in as Evelyn Morgan"} {
		if strings.Contains(body, absent) {
			t.Errorf("a filtered page still offers %q", absent)
		}
	}
	// Exactly the units on the path to the match are open, and each says how
	// much of itself it is actually showing.
	if !strings.Contains(body, `<li class="org-unit" role="treeitem" aria-level="3" aria-expanded="true">`) {
		t.Error("the unit holding the match stayed collapsed")
	}
	if !strings.Contains(body, `<span class="org-count">1 match</span>`) {
		t.Error("a filtered unit does not say how many matches it is showing")
	}
	if strings.Contains(body, `<span class="org-count">3 people</span>`) {
		t.Error("a filtered unit still claims a headcount it is not showing")
	}
}

func TestLoginDirectoryFiltersCombine(t *testing.T) {
	h := newDirectoryHandler(t)
	both := getFilteredLoginPage(t, h, "alvarez", "manager")
	if !strings.Contains(both, "1 of 3 employees match “alvarez” and hold the People manager role.") {
		t.Fatalf("combined filter summary = %q", directorySummary(t, both))
	}
	if !strings.Contains(both, "Sign in as Mateo Alvarez") {
		t.Error("the combined filter dropped the employee that satisfies both halves")
	}
	// The halves are an AND: text that matches somebody the role excludes
	// returns nothing, which is how a combined filter must fail.
	none := getFilteredLoginPage(t, h, "morgan", "manager")
	if !strings.Contains(none, "0 of 3 employees match “morgan” and hold the People manager role.") {
		t.Fatalf("contradictory filter summary = %q", directorySummary(t, none))
	}
	if strings.Contains(none, "Sign in as Evelyn Morgan") || strings.Contains(none, "Sign in as Mateo Alvarez") {
		t.Error("a contradictory filter still offered somebody")
	}
	if strings.Contains(none, `aria-expanded="true"`) {
		t.Error("a filter matching nobody still expanded a unit")
	}
}

func TestLoginDirectoryRoleWithNoHoldersNamesTheFilter(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getFilteredLoginPage(t, h, "", "hr_partner")
	if !strings.Contains(body, "0 of 3 employees hold the HR partner role.") {
		t.Fatalf("an unheld role empty state does not name it: %q", directorySummary(t, body))
	}
	if strings.Contains(body, `<ul class="directory-results">`) {
		t.Error("an empty result set still rendered a result list")
	}
	// The control keeps the filter the URL asked for rather than silently
	// resetting to Any role while the page below answers the filtered
	// question.
	if !strings.Contains(body, `<option value="hr_partner" selected>HR partner</option>`) {
		t.Error("the role control dropped a filter nobody currently holds")
	}
	// A role id with no registry name is still named, not hidden.
	unknown := getFilteredLoginPage(t, h, "", "not_a_role")
	if !strings.Contains(unknown, "0 of 3 employees hold the not_a_role role.") {
		t.Errorf("an unknown role id is not named: %q", directorySummary(t, unknown))
	}
}

func TestLoginDirectoryFilterStateRoundTripsThroughTheURL(t *testing.T) {
	h := newDirectoryHandler(t)
	// The control offers exactly the roles a signable employee holds, in
	// label order, and never a role nobody has.
	held := map[string]bool{}
	for name, bundle := range roleFilterFixtureBundles() {
		access, ok := DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("the bundle for %s did not resolve", name)
		}
		for _, role := range access.Roles {
			held[role] = true
		}
	}
	body := getLoginPage(t, h, "")
	if !strings.Contains(body, `<select id="directory-role" name="`+paramDirectoryRole+`">`) ||
		!strings.Contains(body, `<option value="" selected>Any role</option>`) {
		t.Fatal("the role control is missing or does not default to Any role")
	}
	for role := range held {
		if !strings.Contains(body, `<option value="`+role+`"`) {
			t.Errorf("the role control omits %q, which a seeded employee holds", role)
		}
	}
	for _, absent := range []string{"hr_partner", "finance_partner", "payroll_manager"} {
		if strings.Contains(body, `<option value="`+absent+`"`) {
			t.Errorf("the role control offers %q, which nobody in the directory holds", absent)
		}
	}
	if got := strings.Index(body, `>Compensation administrator<`); got < 0 || got > strings.Index(body, `>Workflow author<`) {
		t.Error("role options are not in a stable label order")
	}
	// A submitted role comes back selected, and one link clears both filters.
	filtered := getFilteredLoginPage(t, h, "alvarez", "manager")
	if !strings.Contains(filtered, `<option value="manager" selected>People manager</option>`) {
		t.Error("the submitted role did not round-trip into the control")
	}
	if !strings.Contains(filtered, `name="`+paramDirectoryQuery+`" value="alvarez"`) {
		t.Error("the submitted query did not round-trip into the control")
	}
	if !strings.Contains(filtered, `<a class="directory-clear" href="`+PathLogin+`">Clear filters</a>`) {
		t.Error("a filtered page offers no single way back to the whole directory")
	}
	if strings.Contains(body, `class="directory-clear"`) {
		t.Error("an unfiltered page offers a clear link with nothing to clear")
	}
	// Both filters travel on the one GET form, so the URL is the whole state.
	if strings.Count(filtered, `<form class="directory-search"`) != 1 {
		t.Error("the two filters are not submitted by one form")
	}
}
