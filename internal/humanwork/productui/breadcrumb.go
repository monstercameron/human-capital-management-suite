package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BreadcrumbItem is one resolved trail segment. Ancestors carry the
// registry route; the current page carries its resolved label.
type BreadcrumbItem struct {
	Label   string
	Href    string
	Current bool
}

// ResolveBreadcrumbs walks the canonical PageDefinitions ParentNav chain for
// the rendered page. The registry stays the single hierarchy authority:
// ancestors the identity cannot open stay unlisted (never unlinked), the
// selected worker names a profile crumb, and unknown pages resolve to nil.
func ResolveBreadcrumbs(view View) []BreadcrumbItem {
	definition, ok := LookupPage(view.Page)
	if !ok {
		return nil
	}
	var ancestors []PageDefinition
	seen := map[PageID]bool{definition.ID: true}
	for parent := definition.ParentNav; parent != ""; {
		parentDefinition, ok := LookupPage(parent)
		if !ok || seen[parentDefinition.ID] {
			break
		}
		seen[parentDefinition.ID] = true
		ancestors = append([]PageDefinition{parentDefinition}, ancestors...)
		parent = parentDefinition.ParentNav
	}
	items := make([]BreadcrumbItem, 0, len(ancestors)+1)
	for _, ancestor := range ancestors {
		if !navigationDestinationAuthorized(view, ancestor.ID) {
			continue
		}
		items = append(items, BreadcrumbItem{Label: view.Locale.Text(ancestor.LabelKey), Href: statefulHref(view, ancestor.ID)})
	}
	current := BreadcrumbItem{Label: view.Locale.Text(definition.LabelKey), Href: statefulHref(view, definition.ID), Current: true}
	if view.Page == PagePerson {
		// An unadmitted record keeps the generic page label: naming the
		// worker in the chrome would leak what the page withholds.
		if label, ok := resolvedPersonPageLabel(view); ok {
			current.Label = label
		}
	}
	return append(items, current)
}

// showBreadcrumbTrail reports whether a resolved trail becomes a landmark.
// A single generic crumb would duplicate the page title, so only
// multi-segment trails and object-resolved singles (a profile naming its
// worker) render.
func showBreadcrumbTrail(view View, items []BreadcrumbItem) bool {
	if len(items) == 0 {
		return false
	}
	if len(items) > 1 {
		return true
	}
	if definition, ok := LookupPage(view.Page); !ok || items[0].Label == view.Locale.Text(definition.LabelKey) {
		return false
	}
	return true
}

// Breadcrumbs renders one trail as a localized landmark.
func Breadcrumbs(view View, items []BreadcrumbItem) ui.Node {
	if !showBreadcrumbTrail(view, items) {
		return ui.Text("")
	}
	entries := make([]ui.Node, 0, len(items))
	for index, item := range items {
		var entry ui.Node
		if item.Current {
			entry = html.Span(html.Props{Aria: map[string]string{"current": "page"}}, ui.Text(item.Label))
		} else {
			entry = appLink(view, html.Props{}, item.Href, ui.Text(item.Label))
		}
		children := []ui.Node{entry}
		if index < len(items)-1 {
			children = append(children, html.Span(html.Props{Class: "breadcrumb-separator", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("/")))
		}
		entries = append(entries, html.Li(html.Props{}, children...))
	}
	return html.Nav(html.Props{Class: "breadcrumbs", Aria: map[string]string{"label": view.Locale.Text("shell.breadcrumbs")}},
		html.Ol(html.Props{}, entries...),
	)
}
