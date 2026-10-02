package productui

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type green2AgentClient struct{ snapshot AgentSnapshot }

func (client green2AgentClient) Snapshot(context.Context, AgentSnapshotRequest) (AgentSnapshot, error) {
	return client.snapshot, nil
}

func TestGreen2_AgentPagesShareFrameAndNavigation(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "owner-1", ""), ResolveProductLocale("en-US"))
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}, {Page: PageAgentOperations, View: true}}

	pages := []ui.Node{
		BuildAgentsPage(view, green2AgentClient{snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}}),
		PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: view.Locale}, Navigation: AgentPageNavigation(view, AgentPageSetup), State: PersonaAdminReady, Snapshot: PersonaAdminSnapshot{Available: true}}),
		BuildAgentOperationsPage(view),
	}
	var navigationShape string
	for index, page := range pages {
		markup, err := ui.RenderToString(page)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(markup, `<section aria-labelledby="`) || !strings.Contains(markup[:min(len(markup), 300)], `class="product-page-frame agent-page-frame"`) {
			t.Fatalf("page %d does not use the shared frame element and class list: %s", index, markup)
		}
		start := strings.Index(markup, `<nav aria-label="Agent pages" class="agents-page-nav">`)
		end := strings.Index(markup[start:], `</nav>`)
		if start < 0 || end < 0 {
			t.Fatalf("page %d is missing shared agent navigation: %s", index, markup)
		}
		navigation := markup[start : start+end+len(`</nav>`)]
		navigation = strings.ReplaceAll(navigation, ` aria-current="page"`, "")
		navigation = strings.ReplaceAll(navigation, `<span class="agents-page-nav-item">`, `<a class="agents-page-nav-item">`)
		navigation = strings.ReplaceAll(navigation, `</span>`, `</a>`)
		for _, label := range []string{"Ask", "Setup", "Operations"} {
			if strings.Count(navigation, ">"+label+"<") != 1 {
				t.Fatalf("page %d navigation is missing one %s item: %s", index, label, navigation)
			}
		}
		if navigationShape == "" {
			navigationShape = navigation
		} else if strings.Count(navigation, `agents-page-nav-item`) != strings.Count(navigationShape, `agents-page-nav-item`) {
			t.Fatalf("page %d navigation shape differs: %s", index, navigation)
		}
	}
}

func TestGreen2_AgentPagesDoNotOwnOuterWidthOrGutter(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{".agents-page", ".persona-admin-page", ".agent-operations-page"} {
		declarations := declarationsFor(css, selector)
		for _, forbidden := range []string{"max-inline-size", "padding-inline"} {
			if strings.Contains(declarations, forbidden) {
				t.Fatalf("%s still owns outer %s: %s", selector, forbidden, declarations)
			}
		}
	}
	frame := declarationsFor(css, ".product-page-frame")
	if !strings.Contains(frame, "max-inline-size:1200px") || !strings.Contains(css, `@media (max-width:760px){.product-page-frame{padding-inline:var(--hcm-space-2)`) {
		t.Fatalf("shared frame does not own width and narrow gutter: %s", frame)
	}
}
