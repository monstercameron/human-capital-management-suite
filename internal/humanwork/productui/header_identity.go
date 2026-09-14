package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageIdentity is the governed header identity for one rendered page: a
// stable registry stamp plus localized copy. Resolving it in one place
// keeps page titles, subtitles, and the scope control from drifting into
// per-page ad-hoc fields while the registry stays the single authority.
//
// It deliberately carries no acting-context field of its own (UXAUDIT-007
// removed the header's unconditional "Acting as yourself" label): a
// per-page identity resolution is not the place a page decides for itself
// whether the acting authority is worth a notice. [ActingAuthorityBanner] is
// the shell's one acting-context component, fed by the server-resolved
// projection, shown or quiet the same way on every page.
type PageIdentity struct {
	Page       PageID
	Title      string
	Subtitle   string
	ScopeLabel string
	ScopeHref  string
}

// PageHeadingProps is the narrow shell-header render contract. The shell
// resolves authority, route identity, and breadcrumb visibility before render.
type PageHeadingProps struct {
	Identity PageIdentity
	Trail    ui.Node
}

// ResolvePageIdentity derives the header identity from the canonical page
// registry, the locale, and the authorized navigation projection. Unknown
// pages fall back to Home exactly like ApplyLocale; the settings
// destination is withheld when the identity cannot open it.
func ResolvePageIdentity(view View) PageIdentity {
	definition, ok := LookupPage(view.Page)
	if !ok {
		definition, _ = LookupPage(PageHome)
	}
	identity := PageIdentity{
		Page:     definition.ID,
		Title:    view.Locale.Text(definition.TitleKey),
		Subtitle: view.Locale.Text(definition.SubtitleKey),
	}
	if definition.ID == PageHome {
		if name := strings.TrimSpace(view.Viewer.Name); name != "" {
			identity.Title = view.Locale.Text("page.home.greeting", map[string]string{"name": name})
		}
	}
	if definition.ID == PagePerson {
		if label, ok := resolvedPersonPageLabel(view); ok {
			identity.Title = label
		}
	}
	return identity
}

// ResolveDocumentPageTitle keeps SSR and the live WASM title setter on the
// same governed object identity. Other routes retain their established view
// title until they adopt an object-specific identity of their own.
func ResolveDocumentPageTitle(view View) string {
	if view.Page == PagePerson {
		return ResolvePageIdentity(view).Title
	}
	return view.Title
}

// PageIdentityHeader renders the canonical page head: the breadcrumb trail
// and resolved title and subtitle. The stable page
// id is stamped on the header so tests, styles, and automation can address
// a page without parsing localized copy.
//
// UXAUDIT-007 removed this header's second, unconditional acting-context
// label ("Acting as yourself" on every page regardless of authority): the
// Acting-context notices now come from exactly one
// place, [ActingAuthorityBanner], shown in the shell above this header only
// when the server-resolved authority actually calls for one.
func PageIdentityHeader(view View) ui.Node {
	return ui.CreateElement(PageHeading, PageHeadingProps{
		Identity: ResolvePageIdentity(view), Trail: Breadcrumbs(view, ResolveBreadcrumbs(view)),
	})
}

// PageHeading renders a stable product page title from resolved narrow props.
func PageHeading(props PageHeadingProps) ui.Node {
	children := []ui.Node{
		props.Trail,
		html.Div(html.Props{}, html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Identity.Title)), html.P(html.Props{Class: "subtitle"}, ui.Text(props.Identity.Subtitle))),
	}
	return html.Div(html.Props{Class: "page-head", Data: map[string]string{"hcm-page": string(props.Identity.Page)}}, children...)
}
