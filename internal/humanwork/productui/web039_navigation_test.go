package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_WEB_039(t *testing.T) {
	view := ApplyNavigationProjection(testView(PageHome), web039NavigationProjection())
	view.MenuQuery = ""
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	navigation := findElementByID(root, "workspace-navigation")
	if navigation == nil || linkForRoute(navigation, "/workspace/app/people") == nil {
		t.Fatal("authorized People destination was not rendered")
	}
	for _, denied := range []string{"/workspace/app/admin", "/workspace/app/person", "/workspace/app/studio"} {
		if linkForRoute(navigation, denied) != nil || strings.Contains(navigationText(navigation), denied) {
			t.Fatalf("denied destination %q was disclosed", denied)
		}
	}
	if strings.Contains(navigationText(navigation), "Add Admin") {
		t.Fatal("favorite state minted an unauthorized Admin destination")
	}
	favoriteView := ApplyNavigationProjection(testView(PageHome), web039NavigationProjection())
	favoriteView.FavoritePages = []PageID{PageAdmin}
	favoriteDoc, err := Render(favoriteView)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(favoriteDoc, "Admin") || strings.Contains(favoriteDoc, "/workspace/app/admin") {
		t.Fatal("favorite state minted an unauthorized Admin destination")
	}

	// A supplied answer is copied and remains authoritative after its source is
	// mutated; no URL or browser preference can expand it.
	projection := web039NavigationProjection()
	view = ApplyNavigationProjection(NewView(PageHome, "tenant", "principal", "scope"), projection)
	projection.Items[0].Page = PageAdmin
	if view.Navigation[0].Page != PageHome {
		t.Fatal("navigation projection escaped through caller-owned slices")
	}
	malformed := NewView(PageHome, "tenant", "principal", "scope")
	malformed.NavigationProjection = &AuthorizedNavigationProjection{Items: []AuthorizedNavigationItem{{Page: PageAdmin, Label: "Admin", Icon: "admin", Href: "https://evil.example/admin", Authorized: true}}}
	malformed = ApplyLocale(malformed, ResolveProductLocale("en-US"))
	malformedDoc, err := Render(malformed)
	if err != nil {
		t.Fatal(err)
	}
	malformedRoot, err := xhtml.Parse(strings.NewReader(malformedDoc))
	if err != nil {
		t.Fatal(err)
	}
	malformedNavigation := findElementByID(malformedRoot, "workspace-navigation")
	if linkForRoute(malformedNavigation, "/workspace/app/admin") != nil || strings.Contains(navigationText(malformedNavigation), "Admin") || strings.Contains(malformedDoc, "evil.example") {
		t.Fatalf("malformed authoritative navigation fell back or rendered an unsafe destination: nav=%q evil=%t", navigationText(malformedNavigation), strings.Contains(malformedDoc, "evil.example"))
	}
}

func TestTodo_WEB_039_Golden(t *testing.T) {
	view := ApplyNavigationProjection(NewView(PageHome, "tenant-web039", "Taylor", "manager"), web039NavigationProjection())
	doc, err := ui.RenderToString(BuildShell(view, html.Section(html.Props{ID: "web039-outlet"}, ui.Text("Route content")), true))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(doc))
	got := hex.EncodeToString(digest[:])

	// Closed launcher omits active-option references and does not claim modality.
	// UXAUDIT-007 removed the page-identity header's unconditional
	// "Acting as yourself" span; re-pinned for the same reason as
	// TestTodo_WEB_037_Golden. UXAUDIT-006 reworded the header's
	// "Authenticated scope" fallback to "Workspace access"; re-pinned again
	// for the same reason as TestTodo_WEB_037_Golden's second pin.
	// UXAUDIT-003 re-pinned again for the same reason as
	// TestTodo_WEB_037_Golden's third pin: the closed launcher no longer
	// embeds its results list, and this fixture's registry-free view
	// (no PersonWorkflows/People) makes the launcher label itself "Go to"
	// rather than "Start an action". Verified against the rendered
	// markup before re-pinning.
	// UIPOLISH-004 re-pins again for the same reason as
	// TestTodo_WEB_037_Golden's latest pin: ".main-scroll", ".primary-nav"
	// and ".sidebar" now render through the shared ScrollRegion component
	// (each gains tabIndex="0", ".primary-nav" also gains
	// id="primary-nav") and the inlined stylesheet gains ScrollRegion's
	// shared rules plus ".main-scroll"'s scrollbar tokens.
	// PROMOUX-012 re-pins: the notification summary now counts only work
	// the viewer must act on and says so ("N promotion items need your
	// action.") instead of "N promotion journeys are visible in this
	// scope.", and the My Work subtitle no longer says every journey needs
	// attention. Verified before re-pinning by substituting exactly those
	// two old strings back into the new document, which reproduced the
	// previous digest byte for byte.
	// NAAS-001 adds the localized empty inbox state before Open My Work.
	const want = "4c2c2532c182c91851f329ad6eeb5ff683c2c93766c77e8ecd484fe081c4b8bf"
	if got != want {
		t.Fatalf("authorization-resolved navigation golden digest = %s, want %s", got, want)
	}
}

func TestTodo_WEB_039_Conformance(t *testing.T) {
	if css := Stylesheet(); !strings.Contains(css, `@media (max-width:360px){.brand-logo-slot[data-hcm-brand-logo-state="fallback"] .wordmark-label{display:none;}`) {
		t.Fatal("narrow viewport does not replace a truncated fallback wordmark with its compact brand mark")
	}
	for _, page := range []PageID{PageHome, PagePeople, PageSettings} {
		view := ApplyNavigationProjection(NewView(page, "tenant-web039", "Taylor", "manager"), web039NavigationProjection())
		ssr, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		start, end := strings.Index(ssr, "<body>"), strings.LastIndex(ssr, "</body>")
		if start < 0 || end <= start {
			t.Fatalf("%s SSR document has no body boundary", page)
		}
		mounted, err := ui.RenderToString(Build(view))
		if err != nil {
			t.Fatal(err)
		}
		if got := ssr[start+len("<body>") : end]; got != mounted {
			t.Fatalf("%s SSR/WASM navigation tree differs", page)
		}
	}
}

func TestNavigationProjectionFailsClosedOnEmptyOrDeniedAnswer(t *testing.T) {
	base := NewView(PageHome, "tenant", "principal", "scope")
	base.Roles = []string{RoleHCMAdmin}
	base.FavoritePages = []PageID{PageAdmin, PageSettings}
	for name, projection := range map[string]AuthorizedNavigationProjection{
		"empty":           AuthorizedNavigationProjection{},
		"versioned empty": {Version: 4},
		"denied item":     {Version: 4, Items: []AuthorizedNavigationItem{{Page: PageAdmin, Label: "Admin", LabelKey: "page.admin.label", Icon: "admin", Href: Path(PageAdmin), Authorized: false}}},
		"wrong parent": AuthorizedNavigationProjection{
			Version: 4,
			Items: []AuthorizedNavigationItem{{
				Page: PageWork, Label: "My Work", LabelKey: "page.work.label", Icon: "work", Href: Path(PageWork), Authorized: true,
				Children: []AuthorizedNavigationItem{{Page: PagePeople, Label: "People", LabelKey: "page.people.label", Icon: "people", Href: Path(PagePeople), Authorized: true}},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			view := ApplyNavigationProjection(base, projection)
			view = ApplyLocale(view, ResolveProductLocale("en-US"))
			props := navigationSidebarProps(view)
			if len(props.Items) != 0 || len(props.Favorites) != 0 || len(props.Support) != 0 {
				t.Fatalf("fail-open navigation projection = items=%d favorites=%d support=%d", len(props.Items), len(props.Favorites), len(props.Support))
			}
			if len(authorizedGlobalSearchItems(view, globalSearchItems(view))) != 0 {
				t.Fatal("authoritative empty/invalid projection retained fuzzy-search destinations")
			}
			for _, page := range []PageID{PageHome, PageMyself, PageWork, PageHelp, PageSettings, PageAdmin} {
				if navigationDestinationAuthorized(view, page) {
					t.Fatalf("registry or role fallback reintroduced %s", page)
				}
			}
			doc, err := ui.RenderToString(BuildShell(view, html.Section(html.Props{ID: "projection-empty-outlet"}), true))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc, "No navigation is available in this context.") {
				t.Fatalf("%s projection did not explain the authoritative empty state", name)
			}
			if strings.Contains(doc, "No menus match") {
				t.Fatalf("%s projection presented an authorization-empty state as a search miss", name)
			}
			for _, route := range []string{Path(PageMyself), Path(PageWork), Path(PageHelp), Path(PageSettings), Path(PageAdmin)} {
				if strings.Contains(doc, `href="`+route) {
					t.Fatalf("shell hydration advertised %q after authoritative empty/invalid answer", route)
				}
			}
			if strings.Contains(doc, "favorites=admin") || strings.Contains(doc, "favorites=settings") {
				t.Fatal("unauthorized favorite identifiers leaked into shell address state")
			}
		})
	}

	filtered := base
	filtered.MenuQuery = "destination-that-does-not-exist"
	filteredDoc, err := ui.RenderToString(BuildShell(filtered, html.Section(html.Props{ID: "filter-empty-outlet"}), true))
	if err != nil {
		t.Fatalf("render filtered navigation: %v", err)
	}
	if strings.Contains(filteredDoc, "No navigation is available in this context.") ||
		!strings.Contains(filteredDoc, "/workspace/app/help") || !strings.Contains(filteredDoc, "/workspace/app/settings") {
		t.Fatal("fuzzy-search empty state did not retain the support recovery region")
	}
}

func TestTodo_WEB_039_Security(t *testing.T) {
	t.Run("deep graph", func(t *testing.T) {
		item := authorizedNavigationTestItem(PageHistory)
		for depth := 0; depth < 10000; depth++ {
			item = AuthorizedNavigationItem{
				Page: PageWork, Label: "My Work", LabelKey: "page.work.label", Icon: "work", Href: Path(PageWork), Authorized: true,
				Children: []AuthorizedNavigationItem{item},
			}
		}
		assertNavigationProjectionFailsClosed(t, AuthorizedNavigationProjection{Version: 1, Items: []AuthorizedNavigationItem{item}})
	})

	t.Run("huge root", func(t *testing.T) {
		items := make([]AuthorizedNavigationItem, 100000)
		for index := range items {
			items[index] = authorizedNavigationTestItem(PageHome)
		}
		assertNavigationProjectionFailsClosed(t, AuthorizedNavigationProjection{Version: 1, Items: items})
	})

	t.Run("repeated overview", func(t *testing.T) {
		overview := AuthorizedNavigationItem{Page: PageWork, Label: "Work queue", LabelKey: "nav.work_queue", Icon: "work", Href: Path(PageWork), Authorized: true}
		group := authorizedNavigationTestItem(PageWork)
		group.Children = []AuthorizedNavigationItem{overview, overview}
		assertNavigationProjectionFailsClosed(t, AuthorizedNavigationProjection{Version: 1, Items: []AuthorizedNavigationItem{group}})
	})
}

func TestNavigationProjectionSecurityRequiresCanonicalPageRouteAndPresentation(t *testing.T) {
	for name, href := range map[string]string{
		"another page": Path(PageAdmin),
		"unknown page": "/workspace/app/not-registered",
		"query state":  Path(PageHome) + "?favorites=admin",
		"fragment":     Path(PageHome) + "#main-content",
		"network path": "//evil.example/workspace/app/home",
		"traversal":    Path(PageHome) + "/../admin",
		"whitespace":   " " + Path(PageHome),
	} {
		t.Run(name, func(t *testing.T) {
			projection := AuthorizedNavigationProjection{Version: 1, Items: []AuthorizedNavigationItem{authorizedNavigationTestItem(PageHome)}}
			projection.Items[0].Href = href
			assertNavigationProjectionFailsClosed(t, projection)
		})
	}

	for name, mutate := range map[string]func(*AuthorizedNavigationItem){
		"label control":       func(item *AuthorizedNavigationItem) { item.Label = "Home\nAdmin" },
		"description control": func(item *AuthorizedNavigationItem) { item.Description = "safe\tsecret" },
		"keyword bidi":        func(item *AuthorizedNavigationItem) { item.Keywords = []string{"safe\u202eadmin"} },
		"label key spoof":     func(item *AuthorizedNavigationItem) { item.LabelKey = "page.admin.label" },
		"icon spoof":          func(item *AuthorizedNavigationItem) { item.Icon = "admin" },
		"opaque page":         func(item *AuthorizedNavigationItem) { item.Page = PageID("tenant-secret") },
	} {
		t.Run(name, func(t *testing.T) {
			item := authorizedNavigationTestItem(PageHome)
			mutate(&item)
			assertNavigationProjectionFailsClosed(t, AuthorizedNavigationProjection{Version: 1, Items: []AuthorizedNavigationItem{item}})
		})
	}

	projection := AuthorizedNavigationProjection{Version: 9, Items: []AuthorizedNavigationItem{authorizedNavigationTestItem(PageHome)}}
	projection.Items[0].Label = "resolver-owned arbitrary copy"
	projection.Items[0].Description = "resolver-owned arbitrary description"
	projection.Items[0].Keywords = []string{"resolver-only-keyword"}
	view := ApplyNavigationProjection(NewView(PageHome, "tenant", "principal", "scope"), projection)
	view = ApplyLocale(view, ResolveProductLocale("de-DE"))
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{projection.Items[0].Label, projection.Items[0].Description, projection.Items[0].Keywords[0]} {
		if strings.Contains(doc, raw) {
			t.Fatalf("resolver-owned unlocalized copy leaked to markup: %q", raw)
		}
	}
	if !strings.Contains(doc, view.Locale.Text("page.home.label")) {
		t.Fatal("canonical localized navigation label was not rendered")
	}
}

func TestNavigationProjectionStateCannotExpandAuthorityAndKeepsCurrentContext(t *testing.T) {
	view := ApplyNavigationProjection(NewView(PagePerson, "tenant", "principal", "scope"), web039NavigationProjection())
	view.FavoritePages = []PageID{PageAdmin, PagePeople, PagePeople}
	view.NavigationGroupOpen = map[PageID]bool{PageWork: false}
	view.MenuQuery = "people"
	props := navigationSidebarProps(view)
	if len(props.Favorites) != 1 || props.Favorites[0].Page != PagePeople {
		t.Fatalf("favorites escaped authorized leaf set: %#v", props.Favorites)
	}
	if strings.Contains(props.Favorites[0].Href, "admin") || strings.Contains(props.Favorites[0].FavoriteHref, "admin") {
		t.Fatal("unauthorized favorite reached presentation address state")
	}
	if !props.Favorites[0].Active {
		t.Fatal("People navigation was not active for a Person route")
	}

	view.MenuQuery = ""
	view.Page = PageHistory
	props = navigationSidebarProps(view)
	work, ok := projectedNavigationItem(props.Items, PageWork)
	if !ok || !work.Active || work.Expanded {
		t.Fatalf("My Work active state or explicit disclosure preference was lost: %#v", work)
	}
}

func TestNavigationContractsRemainNarrowAndNonAuthoritative(t *testing.T) {
	viewType := reflect.TypeOf(View{})
	for _, value := range []any{AuthorizedNavigationProjection{}, AuthorizedNavigationItem{}, NavigationSidebarProps{}, NavigationItemProps{}, MenuFilterProps{}} {
		typeOf := reflect.TypeOf(value)
		if typeContains(typeOf, viewType, map[reflect.Type]bool{}) {
			t.Fatalf("%s transitively embeds page-wide View", typeOf.Name())
		}
		for index := 0; index < typeOf.NumField(); index++ {
			name := strings.ToLower(typeOf.Field(index).Name)
			if strings.Contains(name, "credential") || strings.Contains(name, "token") || strings.Contains(name, "permission") || strings.Contains(name, "role") {
				t.Fatalf("%s contains authority-bearing field %s", typeOf.Name(), typeOf.Field(index).Name)
			}
		}
	}
}

func assertNavigationProjectionFailsClosed(t *testing.T, projection AuthorizedNavigationProjection) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("hostile projection panicked: %v", recovered)
		}
	}()
	view := ApplyNavigationProjection(NewView(PageHome, "tenant", "principal", "scope"), projection)
	if len(view.Navigation) != 0 || len(view.NavigationSupport) != 0 {
		t.Fatalf("hostile projection did not fail closed: items=%d support=%d", len(view.Navigation), len(view.NavigationSupport))
	}
}

func authorizedNavigationTestItem(page PageID) AuthorizedNavigationItem {
	definition, ok := LookupPage(page)
	if !ok {
		panic(fmt.Sprintf("unknown fixture page %q", page))
	}
	return AuthorizedNavigationItem{
		Page: page, Label: definition.Label, LabelKey: definition.LabelKey, Description: definition.Subtitle,
		Keywords: append([]string(nil), definition.SearchTerms...), Icon: definition.Icon, Href: definition.Route, Authorized: true,
	}
}

func web039NavigationProjection() AuthorizedNavigationProjection {
	return AuthorizedNavigationProjection{
		Version: 7,
		Items: []AuthorizedNavigationItem{
			{Page: PageHome, Label: "Home", LabelKey: "page.home.label", Icon: "home", Href: Path(PageHome), Authorized: true},
			{Page: PageWork, Label: "My Work", LabelKey: "page.work.label", Icon: "work", Href: Path(PageWork), Authorized: true, Children: []AuthorizedNavigationItem{
				{Page: PageWork, Label: "Work queue", LabelKey: "nav.work_queue", Icon: "work", Href: Path(PageWork), Authorized: true},
				{Page: PageHistory, Label: "Work History", LabelKey: "page.history.label", Icon: "history", Href: Path(PageHistory), Authorized: true},
			}},
			{Page: PagePeople, Label: "People", LabelKey: "page.people.label", Icon: "people", Href: Path(PagePeople), Authorized: true},
		},
		Support: []AuthorizedNavigationItem{{Page: PageHelp, Label: "Help", LabelKey: "page.help.label", Icon: "help", Href: Path(PageHelp), Authorized: true}},
	}
}

func navigationText(root *xhtml.Node) string {
	var b strings.Builder
	walkElements(root, func(node *xhtml.Node) {
		if node.FirstChild != nil && node.FirstChild.Type == xhtml.TextNode {
			b.WriteString(node.FirstChild.Data)
		}
	})
	return b.String()
}
