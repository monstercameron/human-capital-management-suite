package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ResolveOrganizationScope maps one visibility mode plus qualified units
// to the concrete sorted unit list it admits, resolved against the
// available units. It is display resolution only: it never authorizes
// anything and never invents scope.
//
//   - ALL admits every available unit.
//   - ALLOWLIST admits the qualified units that exist; unknown names drop.
//   - DENYLIST admits every available unit except the qualified ones.
//   - OWN_UNIT is viewer-relative and resolves to no enumerable list.
//   - Unknown or blank modes resolve to nothing.
//
// Matching is case-insensitive, blank names are skipped, output carries
// available-casing, and results are sorted and de-duplicated. Inputs are
// never mutated.
func ResolveOrganizationScope(mode string, units, available []string) []string {
	known := make(map[string]string, len(available))
	order := make([]string, 0, len(available))
	for _, unit := range available {
		key := strings.ToLower(strings.TrimSpace(unit))
		if key == "" {
			continue
		}
		if _, ok := known[key]; !ok {
			known[key] = strings.TrimSpace(unit)
			order = append(order, key)
		}
	}
	qualified := make(map[string]bool, len(units))
	for _, unit := range units {
		if key := strings.ToLower(strings.TrimSpace(unit)); key != "" {
			qualified[key] = true
		}
	}
	var keys []string
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "ALL":
		keys = order
	case "ALLOWLIST":
		for _, key := range order {
			if qualified[key] {
				keys = append(keys, key)
			}
		}
	case "DENYLIST":
		for _, key := range order {
			if !qualified[key] {
				keys = append(keys, key)
			}
		}
	default:
		return nil
	}
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, known[key])
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// organizationScopeResolution renders one resolved scope block for a role
// visibility editor: a title, the mode label, and the resolved unit chips,
// or an honest empty state when the mode resolves to nothing. Callers pass
// the policy for the current scope and the draft for the proposed scope;
// both resolve through ResolveOrganizationScope, so the display can never
// drift from the resolution contract.
func organizationScopeResolution(props OrganizationVisibilityPageProps, titleKey, modifier, mode string, units, available []string, modeLabels map[string]string) ui.Node {
	label, ok := modeLabels[strings.ToUpper(strings.TrimSpace(mode))]
	if !ok {
		label = visibilityModeLabel(mode)
	}
	children := []ui.Node{
		html.H3(html.Props{Class: "organization-scope-title"}, ui.Text(props.Text(titleKey))),
		html.P(html.Props{Class: "organization-scope-mode"}, ui.Text(label)),
	}
	if resolved := ResolveOrganizationScope(mode, units, available); len(resolved) > 0 {
		chips := make([]ui.Node, 0, len(resolved))
		for _, unit := range resolved {
			chips = append(chips, html.Li(html.Props{Class: "organization-scope-unit"}, ui.Text(unit)))
		}
		children = append(children, html.Ul(html.Props{Class: "organization-scope-units"}, chips...))
	} else {
		emptyKey := "organization_visibility.scope_empty"
		if strings.EqualFold(strings.TrimSpace(mode), "OWN_UNIT") {
			emptyKey = "organization_visibility.scope_relative"
		}
		children = append(children, html.P(html.Props{Class: "organization-scope-empty"}, ui.Text(props.Text(emptyKey))))
	}
	return html.Div(html.Props{Class: "organization-scope " + modifier}, children...)
}
