package productui

import (
	"net/url"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// FederationEntry is one server-composed tenant-federation sign-in option.
// Entries arrive as data from the AUTHN-001 issuer registry projection;
// presentation renders what it is given and never authors issuers,
// protocols, assurance levels, or destinations.
type FederationEntry struct {
	Tenant    string
	Issuer    string
	Protocol  string
	Assurance string
	Href      string
}

// RecoveryOption is one server-composed sign-in recovery destination: a
// password-reset flow, a helpdesk contact, or an administrator address.
// Options arrive as data; presentation validates the destination scheme
// and drops anything else without inventing a fallback.
type RecoveryOption struct {
	Label       string
	Description string
	Href        string
}

// FederationEntryProps keeps the entry gate independently composable
// without the page-wide View.
type FederationEntryProps struct {
	I18nProps
	Entries  []FederationEntry
	Recovery []RecoveryOption
	Navigate func(string)
}

// credentialQueryKeys denies the query parameters that carry authenticator
// material. Rendered destinations end up in server logs and telemetry
// surfaces, so a destination smuggling a token, secret, or password fails
// closed even when its scheme is otherwise admissible. Matching is exact
// and case-insensitive on the parameter name: merely containing the
// substring (mytoken) is not a credential.
var credentialQueryKeys = map[string]struct{}{
	"token": {}, "access_token": {}, "id_token": {}, "refresh_token": {},
	"secret": {}, "client_secret": {}, "password": {},
	"api_key": {}, "apikey": {}, "auth_token": {}, "session_token": {},
}

// validRecoveryHref admits only destinations a sign-in recovery link may
// carry: workspace-relative starts, https flows, and mail contacts.
// Everything else — script, data, network-relative, plain http — fails
// closed so a compromised or misconfigured server answer cannot turn the
// gate into an attack surface. Admissible schemes carrying credential
// query parameters fail closed too: authenticator material must never
// reach the logged URL surfaces.
func validRecoveryHref(href string) bool {
	href = strings.TrimSpace(href)
	if href == "" {
		return false
	}
	relative := strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//")
	lower := strings.ToLower(href)
	secure := strings.HasPrefix(lower, "https://")
	if !relative && !secure && !strings.HasPrefix(lower, "mailto:") {
		return false
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return false
	}
	if secure && (parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil) {
		return false
	}
	if strings.HasPrefix(lower, "mailto:") && strings.TrimSpace(parsed.Opaque) == "" {
		return false
	}
	// Query strings can be present on mailto links too. Treat all admitted
	// destinations uniformly so a recovery address can never carry an
	// authenticator into browser history, logs, or telemetry.
	return !recoveryHrefCarriesCredentials(href)
}

// validFederationHref applies the same credential and scheme policy to an
// issuer start link, while intentionally excluding mail contacts (which are
// valid recovery links but cannot start a federation flow).
func validFederationHref(href string) bool {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(strings.ToLower(href), "mailto:") {
		return false
	}
	return validRecoveryHref(href)
}

// recoveryHrefCarriesCredentials reports whether a destination carries a
// denied credential query or fragment parameter. Malformed destinations fail
// closed: a recovery gate must not render a link whose credential content it
// cannot parse.
func recoveryHrefCarriesCredentials(href string) bool {
	parsed, err := url.Parse(href)
	if err != nil {
		return true
	}
	denied := func(values url.Values) bool {
		for key := range values {
			if _, denied := credentialQueryKeys[strings.ToLower(key)]; denied {
				return true
			}
		}
		return false
	}
	if denied(parsed.Query()) {
		return true
	}
	// Fragments are not sent to the server, but they remain visible in browser
	// history and may be captured by client telemetry. Treat query-shaped
	// fragments with the same policy.
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		return true
	}
	if denied(fragment) {
		return true
	}
	return false
}

// federationRecoverySection renders the labelled recovery options. No safe
// option means no section: the gate never shows a control that leads
// nowhere.
func federationRecoverySection(props FederationEntryProps) ui.Node {
	options := make([]RecoveryOption, 0, len(props.Recovery))
	for _, option := range props.Recovery {
		if validRecoveryHref(option.Href) {
			options = append(options, option)
		}
	}
	if len(options) == 0 {
		return ui.Text("")
	}
	items := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		link := html.A(html.Props{Href: option.Href},
			html.Span(html.Props{Class: "federation-entry-issuer"}, ui.Text(option.Label)),
			html.Span(html.Props{Class: "federation-entry-meta"}, ui.Text(option.Description)))
		items = append(items, html.Li(html.Props{Class: "federation-entry-item"}, link))
	}
	return html.Section(html.Props{ID: "federation-entry-recovery", Class: "federation-entry-recovery", Aria: map[string]string{"labelledby": "federation-entry-recovery-title"}},
		html.H2(html.Props{ID: "federation-entry-recovery-title", Class: "federation-entry-tenant"}, ui.Text(props.Text("federation_entry.recovery_title"))),
		html.Ul(html.Props{Class: "federation-entry-recovery-list"}, items...),
	)
}

func federationEntryProps(view View) FederationEntryProps {
	return FederationEntryProps{
		I18nProps: I18nProps{Locale: view.Locale}, Entries: view.FederationEntries, Recovery: view.RecoveryOptions, Navigate: view.Navigate,
	}
}

// FederationEntryList renders the tenant-federation entry gate: issuer
// options grouped by tenant in deterministic order, each link carrying its
// protocol metadata. No configured issuer is an honest empty state, never
// an empty page or an invented destination.
func FederationEntryList(props FederationEntryProps) ui.Node {
	groups := federationEntryGroups(props.Entries)
	if len(groups) == 0 {
		return html.Section(html.Props{ID: "federation-entry", Class: "federation-entry", Aria: map[string]string{"labelledby": "page-title"}},
			html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Text("federation_entry.title"))),
			html.Div(html.Props{Class: "federation-entry-empty"},
				unavailablePanel(props.Text("federation_entry.empty_title"), props.Text("federation_entry.empty_description"))),
			federationRecoverySection(props),
		)
	}
	sections := make([]ui.Node, 0, len(groups))
	for _, tenant := range groups {
		items := make([]ui.Node, 0, len(tenant.entries))
		for _, entry := range tenant.entries {
			entry := entry
			meta := entry.Protocol
			if assurance := strings.TrimSpace(entry.Assurance); assurance != "" {
				meta += " · " + assurance
			}
			link := softwareLink(props.Navigate, html.Props{}, entry.Href,
				html.Span(html.Props{Class: "federation-entry-issuer"}, ui.Text(entry.Issuer)),
				html.Span(html.Props{Class: "federation-entry-meta"}, ui.Text(meta)))
			items = append(items, html.Li(html.Props{Class: "federation-entry-item"}, link))
		}
		sections = append(sections, html.Section(html.Props{Class: "federation-entry-group", Aria: map[string]string{"label": tenant.name}},
			html.H2(html.Props{Class: "federation-entry-tenant"}, ui.Text(tenant.name)),
			html.Ul(html.Props{Class: "federation-entry-list"}, items...)))
	}
	children := append([]ui.Node{
		html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(props.Text("federation_entry.title"))),
		html.P(html.Props{Class: "federation-entry-description"}, ui.Text(props.Text("federation_entry.description"))),
	}, sections...)
	children = append(children, federationRecoverySection(props))
	return html.Section(html.Props{ID: "federation-entry", Class: "federation-entry", Aria: map[string]string{"labelledby": "page-title"}}, children...)
}

type federationTenantGroup struct {
	name    string
	entries []FederationEntry
}

func federationEntryGroups(entries []FederationEntry) []federationTenantGroup {
	index := map[string]int{}
	groups := make([]federationTenantGroup, 0)
	for _, entry := range entries {
		entry.Tenant = strings.TrimSpace(entry.Tenant)
		entry.Issuer = strings.TrimSpace(entry.Issuer)
		entry.Protocol = strings.TrimSpace(entry.Protocol)
		entry.Assurance = strings.TrimSpace(entry.Assurance)
		entry.Href = strings.TrimSpace(entry.Href)
		if !validFederationHref(entry.Href) || entry.Tenant == "" || entry.Issuer == "" {
			continue
		}
		at, ok := index[strings.ToLower(entry.Tenant)]
		if !ok {
			at = len(groups)
			index[strings.ToLower(entry.Tenant)] = at
			groups = append(groups, federationTenantGroup{name: entry.Tenant})
		}
		groups[at].entries = append(groups[at].entries, entry)
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].name) < strings.ToLower(groups[j].name)
	})
	for i := range groups {
		sort.SliceStable(groups[i].entries, func(a, b int) bool {
			left, right := groups[i].entries[a], groups[i].entries[b]
			if strings.ToLower(left.Issuer) != strings.ToLower(right.Issuer) {
				return strings.ToLower(left.Issuer) < strings.ToLower(right.Issuer)
			}
			if left.Protocol != right.Protocol {
				return left.Protocol < right.Protocol
			}
			if left.Assurance != right.Assurance {
				return left.Assurance < right.Assurance
			}
			return left.Href < right.Href
		})
	}
	return groups
}
