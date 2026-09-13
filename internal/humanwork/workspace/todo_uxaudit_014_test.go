package workspace

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ---------------------------------------------------------------------------
// UXAUDIT-014: keep login persona promises consistent with usable
// capabilities.
//
// RED (measured live): a sample persona description promised payroll or
// hiring work that was reachable by no persona at all, and hiring-manager
// and payroll-manager -- holding disjoint role sets
// ({hiring_manager,manager,intent_author} vs {payroll_manager}) -- rendered
// byte-identical destination sets, because productui.PageVisible and
// roleaccess.DefaultPagePermissions both bucket "hiring_manager" and
// "payroll_manager" identically and no payroll-specific page is admitted
// anywhere in the registry.
//
// GREEN (this file plus dev_persona_roles.go and the rewritten
// loginPersonaDescription in handler.go): payroll-manager's fixture now
// carries "worker_self" instead of the do-nothing "payroll_manager" role
// (option (b) from the todo: the smaller, safer fix, since no real payroll
// surface exists yet to admit); persona copy is derived, not hand-written,
// from the same PageVisible+Admitted projection that decides the menu; and
// the tests below prove both the visible and the denied differences a real
// route request produces.
//
// REFACTOR (third pass): the GREEN above was itself measured against two
// projections that disagree. productui.PageVisible's workforce bucket for
// PageInsights omits "worker_self", so it denies; roleaccess.
// DefaultPagePermissions explicitly grants worker_self View on "insights"
// (roleaccess.go's worker_self grant loop). serveProduct (product_shell.go)
// always prefers the roleaccess-derived allowance once any page permission
// is configured, and every real deployment bootstraps a roleaccessstore.Store
// that seeds exactly those defaults on first use (internal/data/
// roleaccessstore.Store.Bootstrap) -- so the live server's menu and route both
// grant worker_self "insights", while loginPersonaDescription's old
// productui.PageVisible-derived copy silently omitted it. This package's own
// test fixtures (newShellHandler) never wired a roleaccess.Store, so this
// suite never caught the drift; uxaudit014Server now wires
// uxaudit014RoleAccess, a fixed-snapshot Store seeded exactly the way
// Store.Bootstrap seeds a fresh tenant, so this in-package suite exercises
// the same admission path the real server does (see
// test/workspace/todo_uxaudit_014_test.go's doc comment, which named this
// exact gap first at the Postgres-backed integration layer).
// loginPersonaDescription and admittedDestinationLabels (handler.go) now
// derive from roleaccess.EffectivePagePermissions/CanPageAction instead of
// productui.PageVisible, and TestTodo_UXAUDIT_014_CopyMatchesMenu below
// asserts the two can never disagree again, generically, without pinning a
// per-persona expected string.
// ---------------------------------------------------------------------------

// uxaudit014RoleAccess is a fixed-snapshot roleaccess.Store standing in for
// roleaccessstore.Store's Postgres-backed one. Load always returns the exact
// snapshot Store.Bootstrap seeds a fresh tenant with
// (roleaccess.DefaultRoles, roleaccess.DefaultPagePermissions), which is
// what every dev persona here resolves against: none of them carry a durable
// roleaccess.Assignment or per-tenant PagePermission override, so
// roleaccess.AssignedRoles falls back to the credential's admitted roles and
// DefaultPagePermissions is the whole story. Save* are never exercised by
// this suite; they exist only to satisfy roleaccess.Store.
type uxaudit014RoleAccess struct{}

func (uxaudit014RoleAccess) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (uxaudit014RoleAccess) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{Roles: roleaccess.DefaultRoles(), PagePermissions: roleaccess.DefaultPagePermissions()}, nil
}

func (uxaudit014RoleAccess) SaveRole(context.Context, values.TenantId, string, roleaccess.Role) (roleaccess.Role, error) {
	return roleaccess.Role{}, nil
}

func (uxaudit014RoleAccess) SaveAssignment(context.Context, values.TenantId, string, roleaccess.Assignment) (roleaccess.Assignment, error) {
	return roleaccess.Assignment{}, nil
}

func (uxaudit014RoleAccess) SaveVisibility(context.Context, values.TenantId, string, string, roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return roleaccess.VisibilityPolicy{}, nil
}

func (uxaudit014RoleAccess) SavePagePermission(context.Context, values.TenantId, string, roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return roleaccess.PagePermission{}, nil
}

// destinationHrefRE captures the top-level slug of every /workspace/app/...
// link rendered anywhere on a page (primary nav, quick actions, breadcrumbs).
// Requesting only the Home page for a freshly-authenticated persona (no
// breadcrumb trail yet) keeps this to the actual navigation menu.
var destinationHrefRE = regexp.MustCompile(`href="` + regexp.QuoteMeta(PathProductPrefix) + `([a-z0-9-]+)`)

// renderedDestinations GETs path as a logged-in persona's session and
// returns the set of top-level /workspace/app/<slug> destinations its
// rendered HTML links to.
func renderedDestinations(t *testing.T, client *http.Client, serverURL, path string) map[string]bool {
	t.Helper()
	res, err := client.Get(serverURL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, res.StatusCode)
	}
	body := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, readErr := res.Body.Read(buf)
		body.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	set := map[string]bool{}
	for _, m := range destinationHrefRE.FindAllStringSubmatch(body.String(), -1) {
		set[m[1]] = true
	}
	return set
}

// signedInClient logs in as the named persona over a real loopback server
// and returns an *http.Client carrying its session cookie, the way a
// browser would.
func signedInClient(t *testing.T, serverURL, personaID string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{paramLoginPersona: {personaID}}
	res, err := client.PostForm(serverURL+PathLogin, form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != PathProductHome {
		t.Fatalf("login as %s = %d location %q", personaID, res.StatusCode, res.Header.Get("Location"))
	}
	return client
}

// uxaudit014Server composes a real loopback server carrying the four
// canonical dev personas, using exactly the role bundles
// DevPersonaRoleSets declares -- the same fixture composeDevPersonas (in
// internal/application) issues real credentials from and
// loginPersonaDescription derives sign-in copy from. It wires
// uxaudit014RoleAccess so its admission and navigation take the same
// roleaccess-derived path serveProduct always takes on the real server
// (REFACTOR, above) rather than falling back to productui.PageVisible the
// way a handler with no RoleAccess configured silently does.
func uxaudit014Server(t *testing.T) (serverURL string) {
	t.Helper()
	handler, _ := newShellHandler(t, true)
	// The verifier clock stays pinned to shellNow for deterministic
	// credentials (see frontendE2EToken); the session cookie's own clock is
	// switched to wall time here because the real cookiejar evaluates
	// Set-Cookie's Expires against wall time, and shellNow is a fixed date
	// that may sit in the past relative to whenever this suite runs.
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.roleAccess = uxaudit014RoleAccess{}
	handler.devPersonas = make(map[string]DevPersona, 4)
	for _, set := range DevPersonaRoleSets() {
		handler.devPersonas[set.ID] = DevPersona{
			ID: set.ID, Name: "Worker " + set.ID, Access: set.ID, Roles: set.Roles,
			Token: frontendE2EToken(t, set.ID, set.Roles),
		}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// TestTodo_UXAUDIT_014 is the PRIMARY test: it proves, generically against
// the live productui registry rather than against a fixed phrase list, that
// no persona's derived sign-in copy can ever name a destination its roles do
// not admit, and that the two personas RED named (hiring-manager and
// payroll-manager) derive different copy now that their role bundles are
// disjoint in effect.
func TestTodo_UXAUDIT_014(t *testing.T) {
	sets := DevPersonaRoleSets()
	labelsByID := make(map[string][]string, len(sets))
	for _, set := range sets {
		persona := DevPersona{ID: set.ID, Roles: set.Roles}
		labels := admittedDestinationLabels(persona.Roles)
		labelsByID[set.ID] = labels

		// A description must never name a page this persona's own roles do
		// not admit as a top-level, Admitted destination. This is the
		// "compile-time or test-time failure" the todo asks for: it is
		// re-derived from the registry on every run, so a future page that
		// loses its admission -- or a persona whose roles no longer reach
		// it -- is caught here, not by a prose reviewer.
		//
		// REFACTOR: "admits" is checked here through
		// roleaccess.CanPageAction over roleaccess.DefaultPagePermissions,
		// not productui.PageVisible -- the same switch loginPersonaDescription
		// itself made, and for the same reason (see this file's REFACTOR
		// comment above): roleaccess, not productui.PageVisible, is what
		// serveProduct and the rendered menu actually enforce, so it is the
		// only boundary a description may not outrun.
		description := loginPersonaDescription(persona)
		permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, persona.Roles)
		for _, definition := range productui.PageDefinitions() {
			names := strings.Contains(description, definition.Label)
			admitted := definition.Admitted && definition.PrimaryNav && roleaccess.CanPageAction(permissions, string(definition.ID), roleaccess.ActionView)
			if names && !admitted {
				t.Errorf("%s: description %q names %q, which its roles do not admit", set.ID, description, definition.Label)
			}
		}
	}

	// RED clause 2 at the fixture layer: hiring-manager
	// ({hiring_manager,manager,intent_author}) and payroll-manager
	// ({worker_self}) hold disjoint role sets and must derive different
	// destination-label sets.
	hiring := labelsByID["hiring-manager"]
	payroll := labelsByID["payroll-manager"]
	if strings.Join(hiring, ",") == strings.Join(payroll, ",") {
		t.Fatalf("hiring-manager and payroll-manager derive identical destinations: %v", hiring)
	}
	if !containsLabel(hiring, "People") || !containsLabel(hiring, "Journeys") {
		t.Errorf("hiring-manager labels = %v, want People and Journeys (its manager role admits both)", hiring)
	}
	if containsLabel(payroll, "People") || containsLabel(payroll, "Journeys") {
		t.Errorf("payroll-manager labels = %v, want neither People nor Journeys (worker_self admits neither)", payroll)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

// TestTodo_UXAUDIT_014_Browser drives the real HTTP edge the way a browser
// would: sign in as each persona over a loopback server, keep its session
// cookie, and request product routes directly. It proves both halves GREEN
// requires -- a visible destination renders, a denied one is refused -- for
// the two personas RED named as indistinguishable.
func TestTodo_UXAUDIT_014_Browser(t *testing.T) {
	serverURL := uxaudit014Server(t)

	cases := []struct {
		persona string
		route   string
		want    int
	}{
		{"hiring-manager", PathProductPrefix + "people", http.StatusOK},
		{"hiring-manager", PathProductPrefix + "journeys", http.StatusOK},
		{"hiring-manager", PathProductPrefix + "myself", http.StatusOK},
		{"hiring-manager", PathProductPrefix + "admin", http.StatusForbidden},
		{"payroll-manager", PathProductPrefix + "people", http.StatusForbidden},
		{"payroll-manager", PathProductPrefix + "journeys", http.StatusForbidden},
		{"payroll-manager", PathProductPrefix + "myself", http.StatusOK},
		{"payroll-manager", PathProductPrefix + "organization", http.StatusOK},
		{"individual-contributor", PathProductPrefix + "people", http.StatusForbidden},
		{"admin", PathProductPrefix + "admin", http.StatusOK},
	}
	for _, c := range cases {
		c := c
		t.Run(c.persona+" "+c.route, func(t *testing.T) {
			client := signedInClient(t, serverURL, c.persona)
			res, err := client.Get(serverURL + c.route)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != c.want {
				t.Fatalf("GET %s as %s = %d, want %d", c.route, c.persona, res.StatusCode, c.want)
			}
		})
	}
}

// TestTodo_UXAUDIT_014_Security proves that a persona outside a page's
// admitted roles is denied at the route itself, by a direct GET with a
// valid, authenticated session -- not merely that the menu omits the link.
// The doc comment on productui.PageDefinition.Admitted is explicit that
// Admitted never gates route access; PageVisible (through serveProduct's
// admission check) is what must actually refuse the request, so this test
// drives the route directly rather than inspecting rendered navigation.
//
// Mutation-verified: temporarily forcing `allowed = true` unconditionally in
// serveProduct (internal/humanwork/workspace/product_shell.go), bypassing
// the authorization check entirely, made this test FAIL (payroll-manager
// reached /workspace/app/people with 200 instead of being refused);
// reverting that change made it PASS again. See the todo close-out report
// for the exact run transcript.
//
// REFACTOR: "insights" was removed from deniedRoutes. It pinned the very
// drift this todo's third pass closes: worker_self is legitimately granted
// View on "insights" by roleaccess.DefaultPagePermissions (the authority
// uxaudit014Server now wires and serveProduct actually enforces), so a
// worker_self persona reaching /workspace/app/insights with 200 is correct,
// not a security gap -- the live server has always returned 200 there. The
// remaining five routes stay denied under that same authority (worker_self's
// grant list is exactly {home, myself, organization, insights, help,
// settings}), so this test still proves real route-level denial, not menu
// omission, for every page worker_self does not hold.
func TestTodo_UXAUDIT_014_Security(t *testing.T) {
	serverURL := uxaudit014Server(t)

	deniedRoutes := []string{
		PathProductPrefix + "people",
		PathProductPrefix + "journeys",
		PathProductPrefix + "work",
		PathProductPrefix + "history",
		PathProductPrefix + "admin",
	}
	for _, persona := range []string{"payroll-manager", "individual-contributor"} {
		persona := persona
		t.Run(persona, func(t *testing.T) {
			client := signedInClient(t, serverURL, persona)
			for _, route := range deniedRoutes {
				route := route
				t.Run(route, func(t *testing.T) {
					res, err := client.Get(serverURL + route)
					if err != nil {
						t.Fatal(err)
					}
					defer res.Body.Close()
					body := new(strings.Builder)
					buf := make([]byte, 4096)
					for {
						n, readErr := res.Body.Read(buf)
						body.Write(buf[:n])
						if readErr != nil {
							break
						}
					}
					if res.StatusCode != http.StatusForbidden {
						t.Fatalf("GET %s as %s (a role with no admitted access to this page) = %d, want 403", route, persona, res.StatusCode)
					}
					// A denial must not leak the authenticated product
					// shell it refused to render.
					if strings.Contains(body.String(), `id="`+JourneyRootElementID+`"`) || strings.Contains(body.String(), `id="`+JourneyConfigElementID+`"`) {
						t.Error("a 403 response disclosed the authenticated product shell")
					}
				})
			}
		})
	}

	// hiring-manager's manager role admits these; a denial-only suite could
	// hide a bug that walls off everything. Prove the positive side too.
	hiringClient := signedInClient(t, serverURL, "hiring-manager")
	for _, route := range []string{PathProductPrefix + "people", PathProductPrefix + "journeys", PathProductPrefix + "work"} {
		res, err := hiringClient.Get(serverURL + route)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s as hiring-manager = %d, want 200 (its manager role admits this page)", route, res.StatusCode)
		}
	}
}

// TestTodo_UXAUDIT_014_Regression encodes RED clause 2 as a live invariant
// -- no two personas whose signed roles are disjoint may render identical
// rendered destination sets -- computed generically over every pair of the
// four canonical personas by actually parsing each one's rendered Home
// navigation, not by comparing against a hardcoded expectation list.
//
// Mutation-verified: temporarily reverting payroll-manager's role bundle in
// DevPersonaRoleSets back to the pre-fix ["payroll_manager"] made this test
// FAIL (hiring-manager and payroll-manager rendered identical destination
// sets); reverting the fixture back to ["worker_self"] made it PASS again.
// See the todo close-out report for the exact run transcript.
func TestTodo_UXAUDIT_014_Regression(t *testing.T) {
	serverURL := uxaudit014Server(t)
	sets := DevPersonaRoleSets()

	destinations := make(map[string]map[string]bool, len(sets))
	for _, set := range sets {
		client := signedInClient(t, serverURL, set.ID)
		destinations[set.ID] = renderedDestinations(t, client, serverURL, PathProductHome)
	}

	rolesOf := func(id string) map[string]bool {
		out := map[string]bool{}
		for _, set := range sets {
			if set.ID == id {
				for _, role := range set.Roles {
					out[role] = true
				}
			}
		}
		return out
	}
	disjoint := func(a, b map[string]bool) bool {
		for role := range a {
			if b[role] {
				return false
			}
		}
		return true
	}
	sameSet := func(a, b map[string]bool) bool {
		if len(a) != len(b) {
			return false
		}
		for k := range a {
			if !b[k] {
				return false
			}
		}
		return true
	}

	for i, a := range sets {
		for j, b := range sets {
			if j <= i {
				continue
			}
			if !disjoint(rolesOf(a.ID), rolesOf(b.ID)) {
				continue
			}
			if sameSet(destinations[a.ID], destinations[b.ID]) {
				t.Errorf("%s and %s hold disjoint role sets (%v vs %v) but render identical destinations %v",
					a.ID, b.ID, a.Roles, b.Roles, destinations[a.ID])
			}
		}
	}
}

// TestTodo_UXAUDIT_014_CopyMatchesMenu is the REFACTOR assertion: for every
// persona, the set of destinations its sign-in card claims must equal the
// set of destinations its own signed-in /workspace/app/home actually
// renders, once a rendered child page is folded to the top-level group
// label a viewer would recognize it under (destinationGroupLabel, below).
// Both sides are derived, not pinned:
//
//   - "claimed" scans the rendered card text against the live registry's own
//     Admitted+PrimaryNav labels (the same predicate admittedDestinationLabels
//     uses), so it reflects whatever the card actually says, not a copy of it.
//   - "actual" parses real hrefs off a real signed-in GET of Home and folds
//     each one through the registry's own ParentNav chain, so it reflects
//     whatever the live menu actually renders, not a copy of that either.
//
// so a future page, fold, or persona cannot drift the two apart without this
// test naming exactly which destination disagrees.
//
// Mutation-verified: temporarily reverting admittedDestinationLabels
// (handler.go) to gate on productui.PageVisible instead of
// roleaccess.CanPageAction reintroduced the exact defect this pass fixes --
// payroll-manager's and individual-contributor's cards stopped claiming
// "Insights" while the live menu (still wired to uxaudit014RoleAccess) kept
// rendering it -- and made this test FAIL, naming the mismatch:
//
//	todo_uxaudit_014_test.go:529: payroll-manager: card claims [Myself Organization], live menu renders [Insights Myself Organization] (folded) -- symmetric difference: [Insights]
//	todo_uxaudit_014_test.go:529: individual-contributor: card claims [Myself Organization], live menu renders [Insights Myself Organization] (folded) -- symmetric difference: [Insights]
//
// Reverting handler.go back made it PASS again. See this pass's report for
// the exact run transcript.
func TestTodo_UXAUDIT_014_CopyMatchesMenu(t *testing.T) {
	serverURL := uxaudit014Server(t)

	// Baseline destinations every signed-in identity reaches regardless of
	// role. admittedDestinationLabels deliberately never names them (they
	// would not distinguish one persona's promise from another's), so both
	// sides below exclude them the same way -- by asking the registry which
	// page IDs they are, not by special-casing their label text.
	baseline := map[productui.PageID]bool{productui.PageHome: true, productui.PageHelp: true, productui.PageSettings: true}

	loginRes, err := http.Get(serverURL + PathLogin)
	if err != nil {
		t.Fatal(err)
	}
	loginBody := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, readErr := loginRes.Body.Read(buf)
		loginBody.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	loginRes.Body.Close()
	cards := personaCards(t, loginBody.String(), "admin", "hiring-manager", "payroll-manager", "individual-contributor")

	for _, set := range DevPersonaRoleSets() {
		card := cards[set.ID]

		claimed := map[string]bool{}
		for _, definition := range productui.PageDefinitions() {
			if !definition.Admitted || !definition.PrimaryNav || baseline[definition.ID] {
				continue
			}
			if strings.Contains(card, definition.Label) {
				claimed[definition.Label] = true
			}
		}

		client := signedInClient(t, serverURL, set.ID)
		rendered := renderedDestinations(t, client, serverURL, PathProductHome)
		actual := map[string]bool{}
		for slug := range rendered {
			definition, ok := productui.LookupRoute(PathProductPrefix + slug)
			if !ok || baseline[definition.ID] {
				continue
			}
			label, ok := destinationGroupLabel(definition.ID)
			if !ok {
				continue
			}
			actual[label] = true
		}

		if !sameLabelSet(claimed, actual) {
			t.Errorf("%s: card claims %v, live menu renders %v (folded) -- symmetric difference: %v",
				set.ID, sortedLabelKeys(claimed), sortedLabelKeys(actual), symmetricLabelDifference(claimed, actual))
		}
	}
}

// destinationGroupLabel folds a page ID that can appear in rendered
// navigation into the top-level primary-nav label a viewer would recognize
// it under: its own Label when it is itself an Admitted, PrimaryNav
// destination, or its ParentNav ancestor's folded label when it is a nested
// child (e.g. Work History renders under "My Work"; Brand & appearance
// renders under "Admin"). This mirrors navigationPrimaryEligible's and
// navigationChildEligible's own primary/child split
// (internal/humanwork/productui/registry.go) instead of hand-naming which
// pages fold where, so a new child page picks up the right fold
// automatically and a page eligible for neither folds to nothing (ok ==
// false).
func destinationGroupLabel(id productui.PageID) (string, bool) {
	definition, ok := productui.LookupPage(id)
	if !ok || !definition.Admitted {
		return "", false
	}
	if definition.PrimaryNav {
		return definition.Label, true
	}
	if definition.ParentNav != "" {
		return destinationGroupLabel(definition.ParentNav)
	}
	return "", false
}

func sameLabelSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedLabelKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func symmetricLabelDifference(a, b map[string]bool) []string {
	diff := map[string]bool{}
	for k := range a {
		if !b[k] {
			diff[k] = true
		}
	}
	for k := range b {
		if !a[k] {
			diff[k] = true
		}
	}
	return sortedLabelKeys(diff)
}
