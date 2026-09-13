package productui

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	xhtml "golang.org/x/net/html"
)

// ---------------------------------------------------------------------------
// UXAUDIT-024: clarify navigation identity, hierarchy and search
// responsibilities.
//
// The live audit against the running server found every RED clause already
// fixed and every checkable GREEN clause already holding:
//   - .brand-cluster/.tenant compute white-space:normal (long names wrap,
//     they do not truncate);
//   - .nav-group is a native <details>/<summary> disclosure with zero inner
//     scrolling descendants -- all scrolling happens on the single
//     .primary-nav landmark;
//   - #global-search-input (role=combobox, "Search Human Capital Management
//     Suite") and #menu-filter (no role, "Filter navigation menu") are
//     structurally and semantically distinct, and global search is fuzzy,
//     ranked, multi-typed and authorization-scoped.
//
// This file pins that behavior as invariants so it cannot silently regress,
// and drives the one thing the live audit could not check with a spot query:
// that global search can never surface a page/person/workflow/setting the
// viewer is not authorized to reach, as a property over roles rather than a
// sample of two queries (TestTodo_UXAUDIT_024_Security, below).
//
// What a Go unit test cannot prove (computed CSS -- white-space, scrollbar
// width/color, actual scrollWidth/clientWidth) is pinned instead by the
// written-but-not-executed Playwright spec
// tools/uxqual/browser/uxaudit024_navigation_identity.spec.mjs
// (TestTodo_UXAUDIT_024_Browser), matching the precedent set by
// PROMOUX-001's browser evidence.
// ---------------------------------------------------------------------------

// uxaudit024RolePagePermissions converts roleaccess's own effective grants
// into productui's narrow RolePagePermission projection, exactly the way
// internal/humanwork/workspace/product_shell.go's productPagePermissions
// does for the live server. Duplicating the four-line conversion here (rather
// than importing the workspace package, which would be a dependency
// inversion -- productui must not depend on the transport package that
// depends on it) keeps this test on the same shape without touching the
// excluded product_shell.go.
func uxaudit024RolePagePermissions(permissions []roleaccess.PagePermission) []RolePagePermission {
	result := make([]RolePagePermission, 0, len(permissions))
	for _, permission := range permissions {
		result = append(result, RolePagePermission{
			Version: permission.Version, RoleID: permission.RoleID, Page: PageID(permission.PageID),
			View: permission.View, Create: permission.Create, Update: permission.Update, Delete: permission.Delete,
		})
	}
	return result
}

// uxaudit024EffectivePermissions resolves one role bundle against the exact
// default grants roleaccessstore.Store.Bootstrap seeds every tenant with
// (roleaccess.DefaultRoles, roleaccess.DefaultPagePermissions) -- the
// authority product_shell.go's serveProduct actually enforces once page
// permissions are configured, which they always are in production. It
// returns both the roleaccess-native permissions (ground truth for
// roleaccess.CanPageAction) and the productui-shaped projection built from
// that exact same slice, so a caller can drive real production code
// (ApplyPagePermissions) and then check its output against the same
// authority the route itself is enforced by.
func uxaudit024EffectivePermissions(roles []string) ([]roleaccess.PagePermission, []RolePagePermission) {
	snapshot := roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}
	effective := roleaccess.EffectivePagePermissions(snapshot, roles)
	return effective, uxaudit024RolePagePermissions(effective)
}

// uxaudit024ViewForRoles builds a View the way productShellDocumentForRouteQuery
// does for an authenticated request carrying page permissions: it starts from
// the shared people/work/workflow fixture (testView) and then applies the
// real ApplyPagePermissions production path, so view.NavigationProjection is
// the same authorized projection the live server would compute for this role
// bundle -- not the direct-slice-truncation stand-in
// (`view.Navigation = view.Navigation[:2]`) the older WEB-041 tests use.
func uxaudit024ViewForRoles(roles []string) (View, []roleaccess.PagePermission) {
	ground, projected := uxaudit024EffectivePermissions(roles)
	view := ApplyPagePermissions(testView(PageHome), projected)
	return view, ground
}

// uxaudit024SearchItems reproduces, item for item, the two lines
// shell.go's globalSearch(view) runs once a NavigationProjection exists. It
// exists here (rather than calling globalSearch, which returns a ui.Node) so
// the test can inspect the exact item slice the live header renders.
func uxaudit024SearchItems(view View) []GlobalSearchItem {
	items := globalSearchItems(view)
	if view.NavigationProjection != nil {
		items = authorizedGlobalSearchItems(view, items)
	}
	return items
}

// uxaudit024LongAlias picks the first registry alias with at least three
// runes. navigationSearchScore deliberately only matches label/page-id
// fields (never keywords) below that length -- "me" is a real
// PageMyself.SearchTerms entry but is too short to test keyword matching
// with, independent of any sharing question this file checks -- so tests
// that want to exercise the shared-keyword path need an alias long enough
// for both consumers to actually consider it.
func uxaudit024LongAlias(terms []string) (string, bool) {
	for _, term := range terms {
		if len([]rune(term)) >= 3 {
			return term, true
		}
	}
	return "", false
}

// uxaudit024ItemPage resolves a GlobalSearchItem back to the PageDefinition
// its Href actually opens, the same way authorizedGlobalSearchItems itself
// does (url.Parse + LookupRoute on the path, ignoring the query string that
// carries the record id).
func uxaudit024ItemPage(t *testing.T, item GlobalSearchItem) PageDefinition {
	t.Helper()
	parsed, err := url.Parse(item.Href)
	if err != nil {
		t.Fatalf("search item %q has an unparseable href %q: %v", item.ID, item.Href, err)
	}
	definition, ok := LookupRoute(parsed.Path)
	if !ok {
		t.Fatalf("search item %q resolves to unregistered route %q", item.ID, parsed.Path)
	}
	return definition
}

// TestTodo_UXAUDIT_024 is the PRIMARY test. It pins, at the component and
// metadata level, the four GREEN facts a Go unit test can check without a
// browser: the brand slot's two honest states, the menu-filter/global-search
// structural distinction, favorites discoverability, and fuzzy multi-typed
// ranked search. It also pins REFACTOR: menu filtering (navigationSearchScore)
// and global search (globalSearchScore) both score against item.Keywords,
// which navigationItemFromDefinition and globalSearchItems both populate
// verbatim from the one registry field, PageDefinition.SearchTerms.
//
// Mutation used: dropped the `Keywords: append([]string(nil),
// definition.SearchTerms...)` half of navigationItemFromDefinition's return
// (registry.go) so every NavItem carried no keywords. FAIL, in the
// REFACTOR subtest below: "home: NavItem.Keywords = [], want it to equal
// PageDefinition.SearchTerms verbatim ([dashboard overview landing
// start])". Restoring the line made it PASS again.
func TestTodo_UXAUDIT_024(t *testing.T) {
	t.Run("brand slot preserves a recognizable logo and accessible name in both states", func(t *testing.T) {
		fallback, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{Name: "Harborcare Health"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fallback, `data-hcm-brand-logo-state="fallback"`) {
			t.Fatalf("empty LogoURL must render the fallback state, got: %s", fallback)
		}
		if strings.Contains(fallback, `src="`) {
			t.Fatalf("fallback state must not carry a fabricated image src: %s", fallback)
		}
		if strings.Count(fallback, "Harborcare Health") < 2 {
			t.Fatalf("fallback state must show the brand name in both the wordmark and the sr-only accessible name: %s", fallback)
		}

		configured, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{Name: "Harborcare Health", LogoURL: "/workspace/assets/harborcare-logo.svg"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(configured, `data-hcm-brand-logo-state="configured"`) {
			t.Fatalf("a configured LogoURL must render the configured state, got: %s", configured)
		}
		if !strings.Contains(configured, `src="/workspace/assets/harborcare-logo.svg"`) {
			t.Fatalf("configured state must carry the customer-supplied src: %s", configured)
		}
		if !strings.Contains(configured, "Harborcare Health") {
			t.Fatalf("configured state must still carry the accessible name, got: %s", configured)
		}

		// DefaultCustomerTheme's BrandLogoURL is empty: a tenant that has
		// never configured a logo must render the fallback state, never a
		// fabricated default asset path. This is the exact "do not fix this
		// into loading harborcare-logo.svg" trap the live audit called out.
		if got := DefaultCustomerTheme().BrandLogoURL; got != "" {
			t.Fatalf("DefaultCustomerTheme().BrandLogoURL = %q, want empty so an unconfigured tenant renders the fallback state", got)
		}
	})

	t.Run("menu filter and global search are visually and semantically distinct", func(t *testing.T) {
		view := testView(PageHome)
		filterMarkup, err := ui.RenderToString(ui.CreateElement(MenuFilter, navigationSidebarPropsForQuery(view).Filter))
		if err != nil {
			t.Fatal(err)
		}
		searchMarkup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, globalSearchProps(view)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(filterMarkup, `id="menu-filter"`) {
			t.Fatalf("menu filter must carry its own id, got: %s", filterMarkup)
		}
		if strings.Contains(filterMarkup, `role="combobox"`) {
			t.Fatal("menu filter must not claim the combobox role global search owns")
		}
		if !strings.Contains(searchMarkup, `id="global-search-input"`) || !strings.Contains(searchMarkup, `role="combobox"`) {
			t.Fatalf("global search must be a labeled combobox, got: %s", searchMarkup)
		}
		filterLabel, searchLabel := view.Locale.Text("nav.filter"), view.Locale.Text("global_search.label")
		if filterLabel == searchLabel {
			t.Fatal("menu filter and global search must not share one accessible name")
		}
		if !strings.Contains(filterMarkup, `aria-label="`+filterLabel+`"`) {
			t.Fatalf("menu filter aria-label = %q not found in %s", filterLabel, filterMarkup)
		}
		if !strings.Contains(searchMarkup, `aria-label="`+searchLabel+`"`) {
			t.Fatalf("global search aria-label = %q not found in %s", searchLabel, searchMarkup)
		}
		if !strings.Contains(filterMarkup, `placeholder="`+view.Locale.Text("nav.filter_placeholder")+`"`) {
			t.Fatal("menu filter must scope its placeholder to menu destinations")
		}
		if !strings.Contains(searchMarkup, `placeholder="`+view.Locale.Text("global_search.placeholder")+`"`) {
			t.Fatal("global search must scope its placeholder to people/pages/workflows/settings")
		}
	})

	t.Run("favorites remain discoverable", func(t *testing.T) {
		props := NavigationItemProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale("")}, Page: PagePeople, Label: "People",
			FavoriteHref: "/workspace/app/people?favorites=people", Favorite: false,
		}
		markup, err := ui.RenderToString(ui.CreateElement(NavigationItem, props))
		if err != nil {
			t.Fatal(err)
		}
		wantLabel := props.Text("nav.favorite_add", map[string]string{"label": "People"})
		if !strings.Contains(markup, `class="nav-favorite"`) || !strings.Contains(markup, `aria-label="`+wantLabel+`"`) {
			t.Fatalf("favorite control missing or mislabeled, want aria-label %q, got: %s", wantLabel, markup)
		}
	})

	t.Run("global search is fuzzy, ranked and multi-typed for one query", func(t *testing.T) {
		items := globalSearchItems(testView(PageHome))
		results := SearchGlobalItems(items, "avery", globalSearchLimit)
		if len(results) < 2 {
			t.Fatalf("query %q should surface more than one kind of destination, got %#v", "avery", searchResultIDs(results))
		}
		kinds := map[string]bool{}
		for _, result := range results {
			kinds[result.Kind] = true
		}
		if len(kinds) < 2 {
			t.Fatalf("query %q should rank across multiple result types, got kinds %v", "avery", kinds)
		}
		typo := SearchGlobalItems(items, "insigths", globalSearchLimit)
		if !hasSearchResult(typo, "page:insights") {
			t.Fatalf("typo-tolerant query %q should still find Insights, got %#v", "insigths", searchResultIDs(typo))
		}
	})

	t.Run("REFACTOR: menu filtering and global search read one shared metadata source", func(t *testing.T) {
		for _, definition := range PageDefinitions() {
			if len(definition.SearchTerms) == 0 {
				continue
			}
			item := navigationItemFromDefinition(definition, ResolveProductLocale(""))
			if strings.Join(item.Keywords, ",") != strings.Join(definition.SearchTerms, ",") {
				t.Fatalf("%s: NavItem.Keywords = %v, want it to equal PageDefinition.SearchTerms verbatim (%v) -- menu filtering must read the registry's own aliases, not a second hand-copied list", definition.ID, item.Keywords, definition.SearchTerms)
			}
			view := testView(PageHome)
			view.Navigation = []NavItem{item}
			searchItems := globalSearchItems(view)
			found := false
			for _, searchItem := range searchItems {
				if searchItem.ID != "page:"+string(definition.ID) {
					continue
				}
				found = true
				for _, term := range definition.SearchTerms {
					if !containsString(searchItem.Keywords, term) {
						t.Fatalf("%s: global search keywords %v are missing registry alias %q that menu filtering already has", definition.ID, searchItem.Keywords, term)
					}
				}
			}
			if !found {
				t.Fatalf("%s: global search dropped a page menu filtering still admits", definition.ID)
			}
			// The two consumers must also share the fuzzy-matching primitive,
			// not merely the data: an alias only navigationSearchScore
			// recognizes (or only globalSearchScore recognizes) is the same
			// class of fork REFACTOR forbids.
			alias, ok := uxaudit024LongAlias(definition.SearchTerms)
			if !ok {
				continue
			}
			if navigationSearchScore(item, alias) == 0 {
				t.Fatalf("%s: menu filter does not recognize its own registry alias %q", definition.ID, alias)
			}
			if globalSearchScore(GlobalSearchItem{Label: item.Label, Keywords: item.Keywords}, strings.Fields(normalizeNavigationSearch(alias))) == 0 {
				t.Fatalf("%s: global search does not recognize the same registry alias %q menu filtering does", definition.ID, alias)
			}
		}
	})
}

// TestTodo_UXAUDIT_024_Accessibility covers the disclosure semantics, the
// distinct combobox/plain-input roles, the favorites labels, and the brand
// slot's accessible name surviving both states -- everything the live audit
// measured that a Go-rendered document can still verify without a browser.
//
// Mutation used: changed NavigationItem's group branch to render
// html.Div instead of html.Details/html.Summary (removing native disclosure
// semantics). FAIL: "group must be a native <details>/<summary> disclosure".
// Restoring html.Details/html.Summary made it PASS again.
func TestTodo_UXAUDIT_024_Accessibility(t *testing.T) {
	t.Run("groups are native keyboard-operable disclosures", func(t *testing.T) {
		group := NavigationItemProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale("")}, Page: PageAdmin, Label: "Admin", Expanded: true,
			Children: []NavigationItemProps{{Page: PageRoles, Label: "Roles & access"}},
		}
		doc, err := ui.RenderToString(ui.CreateElement(NavigationItem, group))
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		var details, summary *xhtml.Node
		walkElements(root, func(node *xhtml.Node) {
			if node.Data == "details" {
				details = node
			}
			if node.Data == "summary" {
				summary = node
			}
		})
		if details == nil || summary == nil {
			t.Fatalf("group must be a native <details>/<summary> disclosure, got: %s", doc)
		}
		if !hasAttr(details, "open") {
			t.Fatal("an Expanded group must render the native open attribute")
		}
	})

	t.Run("search combobox and menu filter carry distinct accessible names and roles", func(t *testing.T) {
		view := testView(PageHome)
		searchDoc, err := ui.RenderToString(ui.CreateElement(GlobalSearch, globalSearchProps(view)))
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(searchDoc))
		if err != nil {
			t.Fatal(err)
		}
		var searchInput *xhtml.Node
		walkElements(root, func(node *xhtml.Node) {
			if node.Data == "input" && attr(node, "id") == "global-search-input" {
				searchInput = node
			}
		})
		if searchInput == nil || attr(searchInput, "role") != "combobox" {
			t.Fatalf("global-search-input must expose role=combobox, got %s", searchDoc)
		}
		if attr(searchInput, "aria-label") == "" {
			t.Fatal("global-search-input must have a non-empty accessible name")
		}

		filterDoc, err := ui.RenderToString(ui.CreateElement(MenuFilter, navigationSidebarPropsForQuery(view).Filter))
		if err != nil {
			t.Fatal(err)
		}
		filterRoot, err := xhtml.Parse(strings.NewReader(filterDoc))
		if err != nil {
			t.Fatal(err)
		}
		var filterInput *xhtml.Node
		walkElements(filterRoot, func(node *xhtml.Node) {
			if node.Data == "input" && attr(node, "id") == "menu-filter" {
				filterInput = node
			}
		})
		if filterInput == nil {
			t.Fatalf("menu-filter input missing, got %s", filterDoc)
		}
		if attr(searchInput, "aria-label") == attr(filterInput, "aria-label") {
			t.Fatal("global search and menu filter must not share one accessible name")
		}
	})

	t.Run("favorite controls carry an accessible add/remove label naming the destination", func(t *testing.T) {
		locale := ResolveProductLocale("")
		for _, favorite := range []bool{false, true} {
			props := NavigationItemProps{I18nProps: I18nProps{Locale: locale}, Page: PageWork, Label: "My Work", FavoriteHref: "/workspace/app/work", Favorite: favorite}
			doc, err := ui.RenderToString(ui.CreateElement(NavigationItem, props))
			if err != nil {
				t.Fatal(err)
			}
			wantKey := "nav.favorite_add"
			if favorite {
				wantKey = "nav.favorite_remove"
			}
			want := locale.Text(wantKey, map[string]string{"label": "My Work"})
			if !strings.Contains(doc, `aria-label="`+want+`"`) {
				t.Fatalf("favorite=%t: want aria-label %q, got %s", favorite, want, doc)
			}
		}
	})

	t.Run("brand slot keeps a real accessible name in both fallback and configured states", func(t *testing.T) {
		for _, logoURL := range []string{"", "/workspace/assets/logo.svg"} {
			doc, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{Name: "Harborcare Health", LogoURL: logoURL}))
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			named := false
			walkElements(root, func(node *xhtml.Node) {
				if node.Data == "span" && attr(node, "data-hcm-brand-name") == "" && hasAttr(node, "data-hcm-brand-name") && node.FirstChild != nil && node.FirstChild.Data == "Harborcare Health" {
					if attr(node, "class") == "sr-only" {
						named = true
					}
				}
			})
			if !named {
				t.Fatalf("logoURL=%q: no sr-only accessible name carrying the brand name was found in %s", logoURL, doc)
			}
		}
	})
}

// TestTodo_UXAUDIT_024_Security is the SECURITY test the todo calls the most
// important one: it proves, as a property over role bundles rather than a
// spot check on two queries, that global search can never surface a page,
// person, workflow or setting the viewer is not authorized to reach.
//
// It resolves each role bundle against roleaccess.DefaultPagePermissions --
// what roleaccessstore.Store.Bootstrap seeds every tenant with, and what
// product_shell.go's serveProduct actually enforces via
// roleaccess.CanPageAction whenever page permissions are configured (always,
// in production) -- builds a View through the real ApplyPagePermissions
// path, runs the real shell.go search-authorization chain, and requires
// every returned item's destination page to be one roleaccess.CanPageAction
// itself admits for that exact role bundle. This is "the authority that
// actually enforces," not productui.PageVisible.
//
// Mutation used: in shell.go's authorizedGlobalSearchItems, changed
// `if ok && allowed[definition.ID]` to `if ok` (dropping the authorization
// filter entirely). FAIL: the "no roles" and "no_such_role" subtests both
// caught it -- "search item \"page:help\" (kind=page) resolves to page
// \"help\", which roleaccess denies this role bundle View on" -- because
// globalSearchItems unconditionally injects Help/Settings as candidate
// items regardless of the caller's authorized navigation, relying entirely
// on this filter to strip them back out for a viewer with no page grants at
// all; every named-role subtest still passed under the mutation because
// DefaultPagePermissions happens to grant every real role bundle "help", so
// the vulnerable path was invisible there -- itself a real, if narrow,
// finding: two callers of the same registry (a not-yet-provisioned or future
// zero-permission role) would otherwise see an unauthorized Help/Settings
// leak. Restoring the filter made every subtest PASS again.
func TestTodo_UXAUDIT_024_Security(t *testing.T) {
	roleBundles := [][]string{
		{},
		{"hcm_admin"},
		{"comp_admin"},
		{"hiring_manager"},
		{"manager"},
		{"payroll_manager"},
		{"hr_partner"},
		{"intent_author"},
		{"promotion_operator"},
		{"worker_self"},
		{"hcm_admin", "comp_admin", "intent_author", "promotion_operator"}, // admin persona
		{"hiring_manager", "manager", "intent_author"},                     // hiring-manager persona
		{"worker_self"}, // payroll-manager / individual-contributor persona
		{"manager", "worker_self"},
		{"hr_partner", "promotion_operator"},
		{"no_such_role"},
	}
	for _, roles := range roleBundles {
		roles := roles
		t.Run(strings.Join(roles, "+"), func(t *testing.T) {
			if len(roles) == 0 {
				t.Run("no roles", func(t *testing.T) {}) // keep the empty-name case legible in -v output
			}
			view, ground := uxaudit024ViewForRoles(roles)
			items := uxaudit024SearchItems(view)
			seenKinds := map[string]int{}
			for _, item := range items {
				definition := uxaudit024ItemPage(t, item)
				if !roleaccess.CanPageAction(ground, string(definition.ID), roleaccess.ActionView) {
					t.Fatalf("roles=%v: search item %q (kind=%s) resolves to page %q, which roleaccess denies this role bundle View on", roles, item.ID, item.Kind, definition.ID)
				}
				seenKinds[item.Kind]++
			}
			// Negative half: a bundle that never gets "people" or "person"
			// must never surface a person-kind or promotion-action result,
			// even though testView seeds three People fixtures every case
			// shares. This is the exact query the live audit ran as
			// "adrian" (a worker outside the viewer's authority) and
			// "roles" (Admin > Roles & access) -- restated here as a
			// property instead of two fixed strings.
			if !roleaccess.CanPageAction(ground, "people", roleaccess.ActionView) {
				if seenKinds["person"] > 0 {
					t.Fatalf("roles=%v: %d person-kind results leaked with no people-page authorization", roles, seenKinds["person"])
				}
				for _, item := range items {
					if strings.HasPrefix(item.ID, "action:promotion:") {
						t.Fatalf("roles=%v: promotion action %q leaked with no people-page authorization", roles, item.ID)
					}
				}
			}
			if !roleaccess.CanPageAction(ground, "roles", roleaccess.ActionView) {
				for _, item := range items {
					if item.ID == "page:roles" {
						t.Fatal("roles page leaked into search for a role bundle without roles-page authorization")
					}
				}
			}
			// The typed ranking must inherit the same authorization: a
			// live search for an out-of-authority worker's unique number
			// must come back empty, exactly like the audit's "adrian"
			// query. (A plain first-name query is not used here: "avery"
			// also fuzzy-matches unrelated authorized settings copy --
			// "reading and interaction" contains "every", one edit from
			// "avery" -- which would make this assertion fail for a
			// reason that has nothing to do with authorization.)
			if !roleaccess.CanPageAction(ground, "people", roleaccess.ActionView) {
				if results := SearchGlobalItems(items, "NW-40118", globalSearchLimit); len(results) != 0 {
					t.Fatalf("roles=%v: query for an out-of-authority worker's number returned %v", roles, searchResultIDs(results))
				}
			}
		})
	}

	// The `samuel`-finds-nothing question from the live audit: a
	// worker_self-only viewer (individual-contributor / payroll-manager)
	// cannot search their own name, because Person results are gated on
	// people-page authorization as a whole, and worker_self never holds
	// it. This is deliberate, not an oversight: the same viewer cannot
	// browse any coworker either, so "no Person results at all" is
	// consistent, and their own record stays reachable through the
	// separately-authorized, separately-labeled Myself page (worker_self
	// is explicitly granted "myself" View in DefaultPagePermissions).
	// Conclusion: DEFENSIBLE as designed; not a RED. This subtest pins
	// that conclusion so a future change cannot silently start leaking
	// (or silently start hiding Myself) without failing here.
	t.Run("worker_self cannot search their own name but Myself stays reachable", func(t *testing.T) {
		view, ground := uxaudit024ViewForRoles([]string{"worker_self"})
		if !roleaccess.CanPageAction(ground, "myself", roleaccess.ActionView) {
			t.Fatal("worker_self must retain myself View access")
		}
		items := uxaudit024SearchItems(view)
		if results := SearchGlobalItems(items, "avery patel", globalSearchLimit); len(results) != 0 {
			t.Fatalf("worker_self should find no Person result by name (no people-page authorization), got %v", searchResultIDs(results))
		}
		if results := SearchGlobalItems(items, "my profile", globalSearchLimit); !hasSearchResult(results, "page:myself") {
			t.Fatalf("worker_self must still be able to find Myself by its own search terms, got %v", searchResultIDs(results))
		}
	})
}

// TestTodo_UXAUDIT_024_Regression pins the REFACTOR contract against the
// specific fork risk the live audit called out by name: menu filtering and
// global search must keep reading one shared registry of labels, aliases,
// keywords and grouping rather than drifting back into two independently
// maintained lists, and the brand slot's honest fallback state must not be
// "fixed" into fabricating a default logo asset.
//
// Mutation used: in navigationSearchScore (navigation_search.go), changed
// `for _, keyword := range item.Keywords` to `for _, keyword := range
// []string(nil)`, so menu filtering stopped scoring against keywords at
// all while global search kept doing so -- the fork this test exists to
// catch, in the opposite direction from PRIMARY's REFACTOR mutation (which
// starved both consumers by emptying the shared data instead of
// decoupling one consumer from it). FAIL: "home: alias \"dashboard\"
// resolves in menu filtering=false but in global search=true -- the two
// have forked onto different metadata". Restoring the range over
// item.Keywords made it PASS again.
func TestTodo_UXAUDIT_024_Regression(t *testing.T) {
	t.Run("global search never forks its aliases away from the registry menu filtering reads", func(t *testing.T) {
		for _, definition := range PageDefinitions() {
			if !definition.Admitted || !definition.PrimaryNav || len(definition.SearchTerms) == 0 {
				continue
			}
			locale := ResolveProductLocale("")
			item := navigationItemFromDefinition(definition, locale)
			view := testView(PageHome)
			view.Navigation = []NavItem{item}
			alias, ok := uxaudit024LongAlias(definition.SearchTerms)
			if !ok {
				continue
			}
			menuMatch := navigationSearchScore(item, alias) > 0
			searchResults := SearchGlobalItems(globalSearchItems(view), alias, globalSearchLimit)
			searchMatch := hasSearchResult(searchResults, "page:"+string(definition.ID))
			if menuMatch != searchMatch {
				t.Fatalf("%s: alias %q resolves in menu filtering=%t but in global search=%t -- the two have forked onto different metadata", definition.ID, alias, menuMatch, searchMatch)
			}
			if !menuMatch {
				t.Fatalf("%s: neither menu filtering nor global search recognizes its own registered alias %q", definition.ID, alias)
			}
		}
	})

	t.Run("an unconfigured brand logo never fabricates a default asset", func(t *testing.T) {
		theme := DefaultCustomerTheme()
		if theme.BrandLogoURL != "" {
			t.Fatalf("DefaultCustomerTheme().BrandLogoURL = %q, want empty", theme.BrandLogoURL)
		}
		doc, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{Name: theme.BrandName, LogoURL: theme.BrandLogoURL}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "<img") && strings.Contains(doc, `src="/`) {
			t.Fatalf("an unconfigured tenant must not render an <img> with a fabricated src, got: %s", doc)
		}
		if !strings.Contains(doc, `data-hcm-brand-logo-state="fallback"`) {
			t.Fatalf("an unconfigured tenant must render the fallback state, got: %s", doc)
		}
	})
}

// TestTodo_UXAUDIT_024_Browser is the BROWSER matrix entry. The live audit
// (planning/todos.md's UXAUDIT-024 entry) is the browser evidence itself,
// recorded against the real server at 1024x768 signed in as admin:
// .brand-cluster/.tenant wrap rather than clip, .nav-group carries zero
// inner scrolling descendants while .primary-nav owns the one thin
// scrollbar, #global-search-input and #menu-filter are visually and
// semantically distinct, and search is fuzzy/multi-typed/authorization-
// scoped. tools/uxqual/browser/uxaudit024_navigation_identity.spec.mjs
// automates that run (written but not executed here, per this todo's
// hard constraints against starting Playwright or the dev server).
//
// What this Go test adds is the part a spec cannot give the traceability
// gate: it pins the exact browser-observable handles that spec's
// page.locator/getByRole/getAttribute calls depend on, against the real
// composed document (Render(view), the same function serveProduct's SSR
// path calls) rather than an isolated component render -- so the spec
// cannot silently start asserting against selectors, roles, or attributes
// the renderer has quietly stopped emitting. It intentionally does not
// re-derive the presentation rules TestTodo_UXAUDIT_024 (component props)
// and TestTodo_UXAUDIT_024_Accessibility (isolated-component ARIA
// semantics) already prove; it exists only where those leave off, at the
// literal markup fragments a real browser selects on.
//
// Mutation used: dropped `ID: "global-search-input"` from GlobalSearch's
// input props (global_search.go), the same class of change ("someone
// dropped an id") the traceability concern names. FAIL: "composed page is
// missing global search fragment \"id=\\\"global-search-input\\\"\"".
// Restoring the ID field made it PASS again.
func TestTodo_UXAUDIT_024_Browser(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}

	// The spec selects #global-search-input, then reads its role and
	// aria-label. Both must survive on the composed page, not just an
	// isolated component render.
	for _, want := range []string{
		`id="global-search-input"`,
		`role="combobox"`,
		`aria-label="Search Human Capital Management Suite"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("composed page is missing global search fragment %q", want)
		}
	}

	// The spec selects #menu-filter and reads its own aria-label. It must
	// differ from global search's, and the element must never pick up the
	// combobox role global search owns. (The composed page legitimately
	// carries a second, unrelated combobox -- #action-launcher-input, the
	// live audit's third distinct control -- so the assertion below is
	// scoped to the menu-filter input's own tag, not a document-wide count.)
	for _, want := range []string{
		`id="menu-filter"`,
		`aria-label="Filter navigation menu"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("composed page is missing menu filter fragment %q", want)
		}
	}
	menuFilterInput := regexp.MustCompile(`<input[^>]*id="menu-filter"[^>]*>`).FindString(doc)
	if menuFilterInput == "" {
		t.Error("composed page has no <input id=\"menu-filter\"> element for the spec to select")
	} else if strings.Contains(menuFilterInput, "role=") {
		t.Errorf("menu filter must never claim a role -- it is a plain labeled input, not the combobox global search owns, got: %s", menuFilterInput)
	}

	// The spec expects native disclosure semantics for a nav group, not a
	// div the browser's accessibility tree would never expose as
	// expandable.
	if !strings.Contains(doc, `<details class="nav-group"`) || !strings.Contains(doc, `<summary class="nav-group-summary">`) {
		t.Error("composed page has no native <details>/<summary> nav group for the spec to open")
	}

	// The spec counts .nav-favorite controls and reads one aria-label to
	// assert it names its destination.
	if !strings.Contains(doc, `class="nav-favorite"`) {
		t.Error("composed page has no .nav-favorite control for the spec to count")
	}
	if matched, err := regexp.MatchString(`class="nav-favorite" href="[^"]*" title="Add [^"]+ to favorites"`, doc); err != nil {
		t.Fatal(err)
	} else if !matched {
		t.Error(`composed page has no favorite control whose accessible name matches "Add <destination> to favorites"`)
	}

	// The spec reads the brand slot's data-hcm-brand-logo-state and
	// expects "fallback" with no fabricated src, plus a .sr-only element
	// carrying the real accessible name.
	if !strings.Contains(doc, `data-hcm-brand-logo-state="fallback"`) {
		t.Error("composed page's brand slot is not in the fallback state an unconfigured tenant must render")
	}
	brandImage := regexp.MustCompile(`<img[^>]*class="brand-logo-image"[^>]*>`).FindString(doc)
	if brandImage == "" {
		t.Error("composed page has no brand-logo-image element for the spec to inspect")
	} else if strings.Contains(brandImage, "src=") {
		t.Errorf("fallback brand slot must render an <img> with no src attribute, got: %s", brandImage)
	}
	if !strings.Contains(doc, `class="sr-only" data-hcm-brand-name="">Human Capital Management Suite</span>`) {
		t.Error("composed page's brand slot has no .sr-only accessible name for the spec (and assistive tech) to read")
	}
}
