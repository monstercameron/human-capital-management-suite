package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_UIPOLISH_006_Security is the SECURITY matrix entry: controls
// stay predictable and operable without changing their authorization.
// A denied action must never expose a navigation target to the refused
// destination -- even when the item arrives with a hostile Href -- while
// keeping its accessible name and explanatory reason; a hidden action
// must render nothing at all.
func TestTodo_UIPOLISH_006_Security(t *testing.T) {
	t.Run("launcher denied row exposes no link to the refused destination", func(t *testing.T) {
		items := []ActionLauncherItem{{
			ID: "action-unavailable:worker-x", Label: "Start promotion",
			Description: "No eligible promotion role is published for this employee.",
			// Hostile input: the contract says Href and Reason are never
			// both set, so the renderer must not trust that invariant.
			Href:         "/workspace/app/journeys?mode=new&worker=worker-x",
			Availability: ActionState{Availability: ActionUnavailable, Reason: "No eligible promotion role is published for this employee."},
		}}
		doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
			I18nProps:    I18nProps{Locale: ResolveProductLocale("en-US")},
			InitialQuery: "promotion", Items: items,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "/workspace/app/journeys?mode=new&amp;worker=worker-x") ||
			strings.Contains(doc, "/workspace/app/journeys?mode=new&worker=worker-x") {
			t.Fatalf("denied launcher row links to the refused destination: %s", doc)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		var row *xhtml.Node
		walkElements(root, func(node *xhtml.Node) {
			if row == nil && attr(node, "aria-disabled") == "true" {
				row = node
			}
		})
		if row == nil {
			t.Fatal("denied launcher row lost its disabled presentation")
		}
		if row.Data != "button" || !hasAttr(row, "disabled") {
			t.Fatalf("denied row is not a real disabled control: <%s>", row.Data)
		}
		if strings.TrimSpace(nodeText(row)) == "" {
			t.Fatal("denied row lost its accessible name")
		}
		descID := attr(row, "aria-describedby")
		if descID == "" || findElementByID(root, descID) == nil {
			t.Fatal("denied row's reason does not resolve to a real element")
		}
	})

	t.Run("capability card denied action exposes no link and keeps its reason", func(t *testing.T) {
		markup, err := ui.RenderToString(ui.CreateElement(CapabilityCard, CapabilityCardProps{
			Title: "Journeys", Description: "Start a journey.", State: "Unavailable", Tone: "warning",
			Action:       ActionLinkProps{Label: "Open journeys", Href: "/workspace/app/journeys"},
			Availability: ActionState{Availability: ActionUnavailable, Reason: "Journeys are disabled for this tenant.", Recovery: ActionLinkProps{Label: "Contact support", Href: "/workspace/app/help"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, `href="/workspace/app/journeys"`) {
			t.Fatalf("denied capability card links to the refused destination: %s", markup)
		}
		for _, want := range []string{"Journeys are disabled for this tenant.", `data-action-state="unavailable"`, `href="/workspace/app/help"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("denied capability card missing %q: %s", want, markup)
			}
		}
	})

	t.Run("hidden actions render nothing", func(t *testing.T) {
		if got := presentableActionLauncherItems([]ActionLauncherItem{{ID: "hidden", Label: "Hidden", Availability: ActionState{Availability: ActionHidden}}}); len(got) != 0 {
			t.Fatalf("hidden launcher item survived presentation: %+v", got)
		}
		markup, err := ui.RenderToString(ui.CreateElement(AdminPage, AdminPageProps{
			Hero:         AdminHeroProps{Eyebrow: "Admin", Title: "Admin", Description: "Admin."},
			Capabilities: []CapabilityCardProps{{Title: "Hidden capability", Availability: ActionState{Availability: ActionHidden}}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "Hidden capability") {
			t.Fatalf("hidden capability card rendered: %s", markup)
		}
	})
}
