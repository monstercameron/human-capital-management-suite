package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OrganizationVisibilityPageProps struct {
	I18nProps
	Roles          []AccessRole
	Policies       []OrganizationVisibilityPolicy
	AvailableUnits []string
	// AvailableDomains bounds which data-domain names the editors may
	// render; anything outside it resolves to the honest empty state.
	AvailableDomains []string
	Back             ActionLinkProps
	RolesLink        ActionLinkProps
	Editable         bool
	OnSave           func(OrganizationVisibilityPolicy)
}

type organizationVisibilityMode struct{ Value, Label, Detail string }

type roleVisibilityEditorProps struct {
	Page     OrganizationVisibilityPageProps
	Role     AccessRole
	Policy   OrganizationVisibilityPolicy
	Expanded bool
}

func RoleVisibilityEditor(props roleVisibilityEditorProps) ui.Node {
	return roleVisibilityEditor(props.Page, props.Role, props.Policy, props.Expanded)
}

type organizationVisibilityDraftState struct {
	Source OrganizationVisibilityPolicy
	Draft  OrganizationVisibilityPolicy
}

func organizationVisibilityPolicyValidation(policy OrganizationVisibilityPolicy, available []string) []string {
	mode := strings.ToUpper(strings.TrimSpace(policy.Mode))
	validModes := map[string]bool{"ALL": true, "OWN_UNIT": true, "ALLOWLIST": true, "DENYLIST": true}
	if !validModes[mode] {
		return []string{"organization_visibility.validation_mode"}
	}
	if mode == "ALLOWLIST" && len(ResolveOrganizationScope(mode, policy.OrganizationUnits, available)) == 0 {
		return []string{"organization_visibility.validation_units"}
	}
	known := make(map[string]bool, len(available))
	for _, unit := range available {
		if key := strings.ToLower(strings.TrimSpace(unit)); key != "" {
			known[key] = true
		}
	}
	for _, unit := range policy.OrganizationUnits {
		if key := strings.ToLower(strings.TrimSpace(unit)); key != "" && !known[key] {
			return []string{"organization_visibility.validation_unknown_unit"}
		}
	}
	return nil
}

func organizationVisibilityPolicySnapshot(policy OrganizationVisibilityPolicy) OrganizationVisibilityPolicy {
	return OrganizationVisibilityPolicy{
		Version: policy.Version, RoleID: policy.RoleID, Mode: policy.Mode,
		OrganizationUnits: append([]string(nil), policy.OrganizationUnits...),
		DataDomains:       append([]string(nil), policy.DataDomains...),
	}
}

func organizationVisibilityPolicyEqual(left, right OrganizationVisibilityPolicy) bool {
	left = organizationVisibilityPolicySnapshot(left)
	right = organizationVisibilityPolicySnapshot(right)
	if !strings.EqualFold(strings.TrimSpace(left.Mode), strings.TrimSpace(right.Mode)) || left.Version != right.Version || left.RoleID != right.RoleID {
		return false
	}
	return equalFoldStringSet(left.OrganizationUnits, right.OrganizationUnits) && equalFoldStringSet(left.DataDomains, right.DataDomains)
}

func equalFoldStringSet(left, right []string) bool {
	leftSet := make(map[string]bool, len(left))
	rightSet := make(map[string]bool, len(right))
	for _, value := range left {
		if key := strings.ToLower(strings.TrimSpace(value)); key != "" {
			leftSet[key] = true
		}
	}
	for _, value := range right {
		if key := strings.ToLower(strings.TrimSpace(value)); key != "" {
			rightSet[key] = true
		}
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for key := range leftSet {
		if !rightSet[key] {
			return false
		}
	}
	return true
}

func organizationVisibilityDiff(current, proposed OrganizationVisibilityPolicy, available []string) (added, removed []string, modeChanged bool) {
	modeChanged = !strings.EqualFold(strings.TrimSpace(current.Mode), strings.TrimSpace(proposed.Mode))
	// OWN_UNIT is relative to each viewer; the available-unit inventory is
	// not an authorized preview of that population. Report only the mode
	// change rather than fabricating added or removed effective units.
	if strings.EqualFold(strings.TrimSpace(current.Mode), "OWN_UNIT") || strings.EqualFold(strings.TrimSpace(proposed.Mode), "OWN_UNIT") {
		return nil, nil, modeChanged
	}
	currentScope := ResolveOrganizationScope(current.Mode, current.OrganizationUnits, available)
	proposedScope := ResolveOrganizationScope(proposed.Mode, proposed.OrganizationUnits, available)
	currentSet := make(map[string]string, len(currentScope))
	proposedSet := make(map[string]string, len(proposedScope))
	for _, unit := range currentScope {
		currentSet[strings.ToLower(strings.TrimSpace(unit))] = unit
	}
	for _, unit := range proposedScope {
		proposedSet[strings.ToLower(strings.TrimSpace(unit))] = unit
	}
	for key, unit := range proposedSet {
		if _, ok := currentSet[key]; !ok {
			added = append(added, unit)
		}
	}
	for key, unit := range currentSet {
		if _, ok := proposedSet[key]; !ok {
			removed = append(removed, unit)
		}
	}
	sortStringsFold(added)
	sortStringsFold(removed)
	return added, removed, modeChanged
}

func sortStringsFold(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && strings.ToLower(values[j]) < strings.ToLower(values[j-1]); j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func OrganizationVisibilityPage(props OrganizationVisibilityPageProps) ui.Node {
	policies := make(map[string]OrganizationVisibilityPolicy, len(props.Policies))
	for _, policy := range props.Policies {
		policies[policy.RoleID] = policy
	}
	editors := make([]ui.Node, 0, len(props.Roles))
	for _, role := range props.Roles {
		if !role.Active {
			continue
		}
		policy, ok := policies[role.ID]
		if !ok {
			policy = OrganizationVisibilityPolicy{RoleID: role.ID, Mode: "OWN_UNIT"}
		}
		// Reconciliation must follow the role, never a sibling slot: an unsaved
		// draft for one role must not appear in another role's editor.
		editors = append(editors, html.WithKey(ui.CreateElement(RoleVisibilityEditor, roleVisibilityEditorProps{Page: props, Role: role, Policy: policy, Expanded: len(editors) == 0}), role.ID))
	}
	if len(editors) == 0 {
		editors = append(editors, ui.CreateElement(EmptyState, EmptyStateProps{
			Title: props.Text("organization_visibility.no_roles"), Description: props.Text("organization_visibility.no_roles_detail"),
			Action: &props.RolesLink,
		}))
	}
	return html.Div(html.Props{Class: "organization-visibility-page"},
		html.Section(html.Props{Class: "surface organization-visibility-intro"},
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.description"))),
			html.Div(html.Props{Class: "organization-visibility-nav"}, ui.CreateElement(ActionLink, props.RolesLink), ui.CreateElement(ActionLink, props.Back)),
		),
		html.Section(html.Props{Class: "role-visibility-list", Raw: map[string]any{"aria-label": props.Text("organization_visibility.configured_roles")}}, editors...),
	)
}

func localizedVisibilityModeLabel(props OrganizationVisibilityPageProps, mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "ALL":
		return props.Text("organization_visibility.mode_all")
	case "ALLOWLIST":
		return props.Text("organization_visibility.mode_allow")
	case "DENYLIST":
		return props.Text("organization_visibility.mode_deny")
	default:
		return props.Text("organization_visibility.mode_own")
	}
}

func roleVisibilityEditor(props OrganizationVisibilityPageProps, role AccessRole, policy OrganizationVisibilityPolicy, expanded bool) ui.Node {
	seed := organizationVisibilityDraftState{Source: organizationVisibilityPolicySnapshot(policy), Draft: organizationVisibilityPolicySnapshot(policy)}
	state := ui.UseState(seed)
	current := state.Get()
	if !organizationVisibilityPolicyEqual(current.Source, policy) {
		current = organizationVisibilityDraftState{Source: organizationVisibilityPolicySnapshot(policy), Draft: organizationVisibilityPolicySnapshot(policy)}
		state.Set(current)
	}
	draft := current.Draft
	validation := organizationVisibilityPolicyValidation(draft, props.AvailableUnits)
	setDraft := func(update func(*OrganizationVisibilityPolicy)) {
		next := state.Get()
		update(&next.Draft)
		state.Set(next)
	}
	selected := map[string]bool{}
	for _, unit := range draft.OrganizationUnits {
		selected[strings.ToLower(strings.TrimSpace(unit))] = true
	}
	modes := []organizationVisibilityMode{
		{"ALL", props.Text("organization_visibility.mode_all"), props.Text("organization_visibility.mode_all_detail")},
		{"OWN_UNIT", props.Text("organization_visibility.mode_own"), props.Text("organization_visibility.mode_own_detail")},
		{"ALLOWLIST", props.Text("organization_visibility.mode_allow"), props.Text("organization_visibility.mode_allow_detail")},
		{"DENYLIST", props.Text("organization_visibility.mode_deny"), props.Text("organization_visibility.mode_deny_detail")},
	}
	modeChoices := make([]ui.Node, 0, len(modes))
	for _, mode := range modes {
		value := mode.Value
		input := html.Props{Type: "radio", Name: "organization-visibility-mode-" + role.ID, Value: value, Checked: strings.EqualFold(draft.Mode, value), Disabled: !props.Editable, Aria: map[string]string{"label": mode.Label}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { setDraft(func(next *OrganizationVisibilityPolicy) { next.Mode = value }) })
		modeChoices = append(modeChoices, html.Label(html.Props{Class: "organization-visibility-mode"}, html.Input(input), html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(mode.Label)), html.Small(html.Props{}, ui.Text(mode.Detail)))))
	}
	unitChoices := make([]ui.Node, 0, len(props.AvailableUnits))
	for _, unit := range props.AvailableUnits {
		value := unit
		input := html.Props{Type: "checkbox", Name: "organization-visibility-unit-" + role.ID, Value: value, Checked: selected[strings.ToLower(value)], Disabled: !props.Editable, Aria: map[string]string{"label": value}}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) {
			key := strings.ToLower(value)
			selected[key] = !selected[key]
			setDraft(func(next *OrganizationVisibilityPolicy) {
				next.OrganizationUnits = next.OrganizationUnits[:0]
				for _, candidate := range props.AvailableUnits {
					if selected[strings.ToLower(candidate)] {
						next.OrganizationUnits = append(next.OrganizationUnits, candidate)
					}
				}
			})
		})
		unitChoices = append(unitChoices, html.Label(html.Props{Class: "organization-visibility-unit"}, html.Input(input), html.Span(html.Props{}, ui.Text(unit))))
	}
	modeFieldset := append([]ui.Node{html.Legend(html.Props{}, ui.Text(props.Text("organization_visibility.scope_title")))}, modeChoices...)
	unitSelectionActive := strings.EqualFold(draft.Mode, "ALLOWLIST") || strings.EqualFold(draft.Mode, "DENYLIST")
	formClass := "organization-visibility-form"
	if !unitSelectionActive {
		formClass += " mode-only"
	}
	unitFieldsetProps := html.Props{Class: "organization-visibility-units", Disabled: !props.Editable}
	unitFieldset := append([]ui.Node{html.Legend(html.Props{}, ui.Text(props.Text("organization_visibility.units_title"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.units_help")))}, html.Div(html.Props{Class: "organization-visibility-unit-grid"}, unitChoices...))
	modeLabels := make(map[string]string, len(modes))
	for _, mode := range modes {
		modeLabels[mode.Value] = mode.Label
	}
	scopeBlock := html.Div(html.Props{Class: "organization-visibility-scope"},
		organizationScopeResolution(props, "organization_visibility.current_scope", "organization-scope-current", policy.Mode, policy.OrganizationUnits, props.AvailableUnits, modeLabels),
		organizationScopeResolution(props, "organization_visibility.proposed_scope", "organization-scope-proposed", draft.Mode, draft.OrganizationUnits, props.AvailableUnits, modeLabels),
	)
	added, removed, modeChanged := organizationVisibilityDiff(policy, draft, props.AvailableUnits)
	diffChanged := modeChanged || len(added) > 0 || len(removed) > 0
	diffBlock := organizationVisibilityDiffNode(props, added, removed, modeChanged, diffChanged)
	detailsProps := html.Props{Class: "surface role-visibility-editor", ID: "organization-visibility-role-" + role.ID, Raw: map[string]any{"data-role-id": role.ID}}
	detailsProps.Raw["name"] = "organization-visibility-editors"
	if expanded {
		detailsProps.Raw["open"] = true
	}
	formChildren := []ui.Node{html.Fieldset(html.Props{Class: "organization-visibility-modes"}, modeFieldset...)}
	if unitSelectionActive {
		formChildren = append(formChildren, html.Fieldset(unitFieldsetProps, unitFieldset...))
	}
	formChildren = append(formChildren, scopeBlock)
	if diffChanged {
		formChildren = append(formChildren, diffBlock)
	}
	formChildren = append(formChildren, organizationVisibilityPreviewUnavailable(props))
	if diffChanged || len(validation) > 0 {
		formChildren = append(formChildren, organizationVisibilityValidation(props, validation))
	}
	if len(policy.DataDomains) > 0 && len(ResolveDataDomainScope(policy.DataDomains, props.AvailableDomains)) > 0 {
		formChildren = append(formChildren, dataDomainScope(props, policy.DataDomains, props.AvailableDomains))
	}
	formChildren = append(formChildren, saveActions(props, role, diffChanged, len(validation) > 0))
	return html.Tag("details", detailsProps,
		html.Tag("summary", html.Props{}, html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(role.Name)), html.Code(html.Props{}, ui.Text(role.ID))), html.Span(html.Props{Class: "status"}, ui.Text(localizedVisibilityModeLabel(props, policy.Mode)))),
		html.Form(html.Props{Class: formClass, OnSubmit: saveOrganizationVisibility(props.OnSave, &draft)}, formChildren...),
	)
}

// saveActions renders the editor's save row in its semantic state: an
// editable policy gets the live save button; without the update grant
// the button stays disabled with the reason and the recovery link, so
// the viewer can resolve the condition instead of guessing.
func organizationVisibilityDiffNode(props OrganizationVisibilityPageProps, added, removed []string, modeChanged, changed bool) ui.Node {
	items := make([]ui.Node, 0, 2)
	if modeChanged {
		items = append(items, html.Li(html.Props{}, ui.Text(props.Text("organization_visibility.diff_mode"))))
	}
	if len(added) > 0 {
		items = append(items, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(props.Text("organization_visibility.diff_added"))), ui.Text(strings.Join(added, ", "))))
	}
	if len(removed) > 0 {
		items = append(items, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(props.Text("organization_visibility.diff_removed"))), ui.Text(strings.Join(removed, ", "))))
	}
	if !changed {
		items = append(items, html.Li(html.Props{}, ui.Text(props.Text("organization_visibility.diff_none"))))
	}
	return html.Div(html.Props{Class: "organization-visibility-diff organization-visibility-boundary", DataAttr: html.DataAttribute{Name: "diff-state", Value: map[bool]string{true: "changed", false: "unchanged"}[changed]}, Raw: map[string]any{"role": "status", "aria-live": "polite"}},
		html.H3(html.Props{}, ui.Text(props.Text("organization_visibility.diff_title"))), html.Ul(html.Props{}, items...))
}

func organizationVisibilityPreviewUnavailable(props OrganizationVisibilityPageProps) ui.Node {
	title := organizationVisibilityText(props, "organization_visibility.preview_title", "Worker preview unavailable")
	detail := organizationVisibilityText(props, "organization_visibility.preview_unavailable", "The server does not provide a policy preview; no worker records are shown.")
	return html.Div(html.Props{Class: "organization-visibility-preview-unavailable", Raw: map[string]any{"role": "status"}},
		html.Strong(html.Props{}, ui.Text(title)), html.P(html.Props{Class: "muted"}, ui.Text(detail)))
}

func organizationVisibilityValidation(props OrganizationVisibilityPageProps, issues []string) ui.Node {
	children := []ui.Node{html.H3(html.Props{}, ui.Text(organizationVisibilityText(props, "organization_visibility.validation_title", "Validation")))}
	if len(issues) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(organizationVisibilityText(props, "organization_visibility.validation_ready", "No local issues found. The server validates the policy on Save."))))
	} else {
		items := make([]ui.Node, 0, len(issues))
		for _, issue := range issues {
			fallback := map[string]string{"organization_visibility.validation_mode": "Choose a supported visibility mode.", "organization_visibility.validation_units": "Select at least one available unit for this mode."}[issue]
			items = append(items, html.Li(html.Props{}, ui.Text(organizationVisibilityText(props, issue, fallback))))
		}
		children = append(children, html.Ul(html.Props{}, items...))
	}
	return html.Div(html.Props{Class: "organization-visibility-validation", DataAttr: html.DataAttribute{Name: "validation-state", Value: map[bool]string{true: "invalid", false: "ready"}[len(issues) > 0]}, Raw: map[string]any{"role": "status", "aria-live": "polite"}}, children...)
}

func organizationVisibilityText(props OrganizationVisibilityPageProps, key, fallback string) string {
	value := props.Text(key)
	if strings.Contains(value, "⟦") {
		switch props.Locale.Resolved {
		case "de-DE":
			if key == "organization_visibility.preview_title" {
				return "Mitarbeitervorschau nicht verfügbar"
			}
			if key == "organization_visibility.preview_unavailable" {
				return "Der Server stellt keine Richtlinienvorschau bereit; es werden keine Beschäftigtendaten angezeigt."
			}
		case "ar":
			if key == "organization_visibility.preview_title" {
				return "معاينة الموظفين غير متاحة"
			}
			if key == "organization_visibility.preview_unavailable" {
				return "لا يوفر الخادم معاينة للسياسة؛ ولا تُعرض سجلات الموظفين."
			}
		}
		return fallback
	}
	return value
}

func saveActions(props OrganizationVisibilityPageProps, role AccessRole, changed bool, invalid ...bool) ui.Node {
	blocked := len(invalid) > 0 && invalid[0]
	save := html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !props.Editable || !changed || blocked}, ui.Text(props.Text("organization_visibility.save")))
	status := html.P(html.Props{ID: "organization-visibility-status-" + role.ID, Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Text("organization_visibility.status")))
	if props.Editable {
		return html.Div(html.Props{Class: "organization-visibility-actions sticky-actions", Style: map[string]string{"position": "sticky", "bottom": "0", "z-index": "2"}, DataAttr: html.DataAttribute{Name: "unsaved", Value: map[bool]string{true: "true", false: "false"}[changed]}}, save, status)
	}
	return html.Div(html.Props{Class: "organization-visibility-actions", Raw: map[string]any{"data-action-state": "unavailable"}},
		save, status,
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("organization_visibility.save_unavailable"))),
		ui.CreateElement(ActionLink, props.RolesLink),
	)
}

func visibilityModeLabel(mode string) string {
	switch strings.ToUpper(mode) {
	case "ALL":
		return "Everyone"
	case "ALLOWLIST":
		return "Selected units"
	case "DENYLIST":
		return "Except selected units"
	default:
		return "Own unit"
	}
}

func saveOrganizationVisibility(save func(OrganizationVisibilityPolicy), draft *OrganizationVisibilityPolicy) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
