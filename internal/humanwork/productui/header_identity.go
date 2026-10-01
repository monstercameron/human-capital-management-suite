package productui

import (
	"strings"
	"time"
	"unicode"

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
		if name := preferredViewerFirstName(view); name != "" {
			identity.Title = homeGreeting(view.Locale, name)
		}
	}
	if definition.ID == PagePerson {
		if label, ok := resolvedPersonPageLabel(view); ok {
			identity.Title = label
		}
	}
	return identity
}

func preferredViewerFirstName(view View) string {
	identities := []string{view.Viewer.PersonID, view.Principal}
	for _, identity := range identities {
		for _, person := range view.People {
			if !personIdentityMatches(identity, person) {
				continue
			}
			if preferred := firstName(person.PreferredName); preferred != "" {
				return preferred
			}
			if name := firstName(person.Name); name != "" {
				return name
			}
		}
	}
	name := strings.TrimSpace(view.Viewer.Name)
	if name == "" {
		name = strings.TrimSpace(view.Principal)
	}
	// An unbound worker-number or subject is not a first name. Failing closed
	// here avoids greeting someone with a tenant/number fragment while the
	// authorized worker projection is unavailable.
	if strings.IndexFunc(name, unicode.IsDigit) == -1 {
		return firstName(name)
	}
	return ""
}

func firstName(name string) string {
	if fields := strings.Fields(strings.TrimSpace(name)); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func personIdentityMatches(identity string, person Person) bool {
	want := compactIdentity(identity)
	if want == "" {
		return false
	}
	for _, candidate := range []string{person.ID, person.WorkerID, person.SubjectID, person.WorkerNumber} {
		if got := compactIdentity(candidate); got != "" && got == want {
			return true
		}
	}
	return false
}

func compactIdentity(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func homeGreeting(locale LocaleContext, name string) string {
	key := "page.home.greeting"
	if hour, ok := browserLocalHour(); ok {
		switch {
		case hour < 12:
			key += ".morning"
		case hour < 18:
			key += ".afternoon"
		default:
			key += ".evening"
		}
	}
	return locale.Text(key, map[string]string{"name": name})
}

// homeGreetingAt is kept pure for the native test matrix and for callers that
// need deterministic copy selection without consulting the browser clock.
func homeGreetingAt(locale LocaleContext, name string, at time.Time) string {
	key := "page.home.greeting.morning"
	switch hour := at.Hour(); {
	case hour < 12:
	case hour < 18:
		key = "page.home.greeting.afternoon"
	default:
		key = "page.home.greeting.evening"
	}
	return locale.Text(key, map[string]string{"name": name})
}

// ResolveDocumentPageTitle keeps SSR and the live WASM title setter on the
// same governed object identity. Other routes retain their established view
// title until they adopt an object-specific identity of their own.
func ResolveDocumentPageTitle(view View) string {
	switch view.Page {
	case PageHome:
		// The tab names the page; the personal greeting belongs to the page
		// heading, not to a title read among other browser tabs (UXBLIND-087).
		if definition, ok := LookupPage(PageHome); ok {
			return view.Locale.Text(definition.TitleKey)
		}
	case PagePerson:
		return ResolvePageIdentity(view).Title
	}
	// During a route transition the router may retain the previous English
	// view title while the new page is still loading. The registry title is the
	// localized interim identity and is safe before any page data arrives.
	if definition, ok := LookupPage(view.Page); ok && (view.Loading || view.ContentLoading) {
		return view.Locale.Text(definition.TitleKey)
	}
	return view.Title
}

// DocumentTitle is the one browser-tab title format shared by the server
// shell, SSR and the WASM route renderer: "<page> · <company display name>".
// The company is the tenant's configured brand name, or its display name
// while the brand is still the product default.
func DocumentTitle(pageTitle string, appearance CustomerTheme, tenant string) string {
	brandName, _ := HeaderBrandIdentity(NormalizeCustomerTheme(appearance), tenant)
	pageTitle = strings.TrimSpace(pageTitle)
	if pageTitle == "" {
		return brandName
	}
	return pageTitle + " · " + brandName
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
	// Keyed: the breadcrumb trail is absent while a destination loads and
	// present once it resolves. Unkeyed, that shifted the title block's
	// position, so the reconciler replaced the <h1> the router had just
	// focused and focus fell to <body> (UXLIVE-028).
	children := []ui.Node{
		html.WithKey(props.Trail, "page-trail"),
		html.Div(html.Props{Key: "page-title-block"}, html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Identity.Title)), html.P(html.Props{Class: "subtitle"}, ui.Text(props.Identity.Subtitle))),
	}
	return html.Div(html.Props{Class: "page-head", Data: map[string]string{"hcm-page": string(props.Identity.Page)}}, children...)
}
