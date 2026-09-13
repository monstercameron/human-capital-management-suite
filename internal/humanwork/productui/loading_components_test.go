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
		PageOrganization: "loading-analysis-layout",
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

func TestLoadingProxyMotionHonorsExplicitAndOperatingSystemPreferences(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .loading-block:after`,
		`@media (prefers-reduced-motion:no-preference)`,
		`animation-name:hcm-shimmer-`,
		`@media (forced-colors:active){.loading-block,.loading-progress`,
		`.app-shell.nav-collapsed .loading-progress{left:72px;}`,
		`@media (max-width:760px){.app-shell .loading-progress,.app-shell.nav-collapsed .loading-progress{left:0;top:0;}`,
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
		`.network-stage-refreshing{opacity:0.985;transform:translateY(1px);}`,
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
