package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestLoadingProxyUsesPageShapedAccessibleShells(t *testing.T) {
	tests := map[PageID]string{
		PageHome:         "loading-work-layout",
		PageMyself:       "loading-profile-layout",
		PageJourneys:     "loading-work-layout",
		PageWork:         "loading-work-layout",
		PagePeople:       "loading-table-layout",
		PageHistory:      "loading-table-layout",
		PagePerson:       "loading-profile-layout",
		PageOrganization: "loading-table-layout",
		PageInsights:     "loading-analysis-layout",
		PageSettings:     "loading-settings-layout",
		PageAppearance:   "loading-settings-layout",
	}
	for page, shape := range tests {
		t.Run(string(page), func(t *testing.T) {
			view := NewView(page, "HarborCare Demo", "manager", "self")
			out, err := ui.RenderToString(BuildLoading(view))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`class="app-shell is-loading"`, `id="main-content"`, `aria-busy="true"`,
				`aria-live="polite"`, "Loading your workspace data", shape,
				`aria-hidden="true"`, "loading-progress",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("loading %s surface missing %q", page, want)
				}
			}
			if strings.Contains(out, "0 promotion journeys are visible") {
				t.Fatal("loading shell exposed an unresolved network count as zero")
			}
		})
	}
}

func TestLoadingProxyPublishesStableRegionAndGeometryContract(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PagePerson, PageOrganization, PageSettings} {
		geometry := LoadingProxyGeometry(page)
		if geometry.Layout == "" || geometry.Rows <= 0 || geometry.Columns <= 0 {
			t.Fatalf("page %q has incomplete loading geometry: %+v", page, geometry)
		}
		markup, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: page}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`data-async-region="page-content"`,
			`data-loading-contract="v1"`,
			`data-loading-layout="` + geometry.Layout + `"`,
			`data-preserve-scroll="true"`,
			`data-preserve-focus="true"`,
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("page %q loading proxy missing %q: %s", page, want, markup)
			}
		}
	}

	// A malformed route still gets a deterministic, non-interactive proxy;
	// it must not produce a blank outlet or a forged page class.
	geometry := LoadingProxyGeometry(PageID("not-a-page"))
	if geometry.Layout == "" || geometry.Rows <= 0 || geometry.Columns <= 0 {
		t.Fatalf("unknown page has no safe loading fallback: %+v", geometry)
	}
}

func TestUnknownFocusedRefreshFallsBackToPageBusyState(t *testing.T) {
	view := testView(PagePeople)
	view.RefreshingRegion = "unregistered-region"
	markup, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="app-shell is-refreshing"`, `data-network-state="refreshing"`, `class="loading-progress network-progress"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("unknown refresh region did not preserve page busy state %q", want)
		}
	}
}

func TestLoadingProxyMotionHonorsExplicitAndOperatingSystemPreferences(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .loading-block:after`,
		`@media (prefers-reduced-motion:no-preference)`,
		`animation-name:hcm-shimmer-`,
		`@media (forced-colors:active){.loading-block,.loading-progress`,
		`.app-shell.nav-collapsed .loading-progress{inset-inline-start:72px;}`,
		`@media (max-width:760px){.app-shell .loading-progress,.app-shell.nav-collapsed .loading-progress{inset-inline-start:0;top:0;}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("loading motion contract missing %q", want)
		}
	}
	if strings.Contains(css, `.network-slot-ready{opacity`) {
		t.Fatal("persistent shell slots replay an entrance animation during leaf-route navigation")
	}
}

func TestNetworkTransitionsResolveAsOneRegionWithoutNestedFlicker(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@starting-style{.network-stage-ready{opacity:0.94;`,
		`.network-stage-refreshing{opacity:1;transform:none;}`,
		`.network-stage :where(.work-row,.people-row,.history-row,.status,.count)`,
		`:root[data-hcm-motion-preference="limited"] .network-stage-refreshing`,
		`@media (prefers-reduced-motion:reduce){.network-stage,.network-slot`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("network transition contract missing %q", want)
		}
	}
}

func TestWarmRefreshKeepsAuthorizedContentMounted(t *testing.T) {
	view := testView(PagePeople)
	out, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell is-refreshing"`, `aria-busy="true"`,
		`data-network-state="refreshing"`, `class="loading-progress network-progress"`,
		"Avery Patel",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refreshing surface missing %q", want)
		}
	}
	if strings.Contains(out, "loading-table-layout") {
		t.Fatal("warm refresh replaced authorized rows with a cold-loading proxy")
	}
}

func TestContentLoadingKeepsResolvedShellAndScopesPendingStateToMain(t *testing.T) {
	view := testView(PageWork)
	out, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell is-content-loading"`, `id="main-content"`, `aria-busy="true"`,
		`data-network-state="pending"`, `loading-work-layout`, "Taylor",
		`viewer-profile-link network-slot network-slot-ready`, `notifications network-slot network-slot-ready`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("content loading surface missing %q", want)
		}
	}
	for _, unwanted := range []string{
		`viewer-profile-loading`, `notification-loading`, `class="app-shell is-loading"`,
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("content loading remounted global shell placeholder %q", unwanted)
		}
	}
}

func TestPeopleCollectionRefreshScopesBusyStateToDirectory(t *testing.T) {
	view := testView(PagePeople)
	view.RefreshingRegion = RefreshRegionPeopleDirectory
	out, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="surface people-directory is-refreshing"`, `aria-busy="true"`,
		`class="loading-progress people-directory-progress"`, "Avery Patel",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("people directory refresh missing %q", want)
		}
	}
	for _, unwanted := range []string{
		`class="app-shell is-refreshing"`, `data-network-state="refreshing"`,
		`class="loading-progress network-progress"`,
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("people directory refresh leaked page-wide state %q", unwanted)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.people-directory.is-refreshing{position:relative;}`,
		`.people-directory.is-refreshing .people-directory-progress:after`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("people directory refresh styling missing %q", want)
		}
	}
}

// TestWorkFamilyProxyIsOneColumn: Home, My Work and Journeys render one
// column, so their proxy reserves one. A second, detail-shaped panel beside
// the list collapsed away on every navigation once the data arrived.
func TestWorkFamilyProxyIsOneColumn(t *testing.T) {
	for _, page := range []PageID{PageHome, PageWork, PageJourneys} {
		if geometry := LoadingProxyGeometry(page); geometry.Columns != 1 {
			t.Errorf("%s: proxy reserves %d columns; the page renders one", page, geometry.Columns)
		}
		out, err := ui.RenderToString(LoadingProxy(LoadingProxyProps{Page: page}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "loading-two-column") {
			t.Errorf("%s: proxy still draws a two-column body", page)
		}
	}
}
