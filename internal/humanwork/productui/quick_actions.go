package productui

import "strings"

// ResolveQuickActions resolves a viewer's chosen quick-action
// IDs against the available launcher items for the
// quick-actions slot. Chosen order is kept, repeats collapse
// to first mention, and unknown IDs drop fail-closed so stale
// or forged choices never leak through or break the slot.
// Items pass through untouched; the catalog owns the truth.
func ResolveQuickActions(available []ActionLauncherItem, chosen []string) []ActionLauncherItem {
	byID := make(map[string]ActionLauncherItem, len(available))
	for _, item := range available {
		byID[item.ID] = item
	}
	resolved := make([]ActionLauncherItem, 0, len(chosen))
	seen := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		if seen[id] {
			continue
		}
		seen[id] = true
		if item, ok := byID[id]; ok {
			resolved = append(resolved, item)
		}
	}
	return resolved
}

// ResolveHomeQuickActions builds outcome-oriented Home shortcuts from the
// already authorized page projection. Role hints only choose which useful
// starts to prefer; Allows and the page registry remain the source of
// discoverability, so a role string can never grant an action.
func ResolveHomeQuickActions(view View) []ActionLinkProps {
	actions := make([]ActionLinkProps, 0, 5)
	peopleShortcutAdded := false
	if view.Allows(PageJourneys, "create") && view.Allows(PagePeople, "view") {
		// This is a task start, not a return to the viewer's last directory
		// search. Explicit values override server-persisted filters so a stale
		// team or location cannot silently hide eligible employees.
		href := statefulHref(view, PagePeople, "eligible", "1", "page", "1")
		href = withExplicitEmptyQuery(href, "q", "team", "location")
		actions = append(actions, ActionLinkProps{Label: view.Locale.Text("home.action_promote"), Href: href, Class: "button secondary", Navigate: view.Navigate})
		peopleShortcutAdded = true
	} else if view.Allows(PagePeople, "view") {
		actions = append(actions, ActionLinkProps{Label: view.Locale.Text("home.action_find"), Href: statefulHref(view, PagePeople), Class: "button secondary", Navigate: view.Navigate})
		peopleShortcutAdded = true
	}
	role := strings.ToLower(strings.TrimSpace(view.Viewer.Role + " " + strings.Join(view.Roles, " ")))
	managerial := strings.Contains(role, "manager") || strings.Contains(role, "lead") || strings.Contains(role, "admin") || strings.Contains(role, "hr")
	// A manager/specialist benefits from a direct directory start; employees
	// get self-service destinations when the authorized registry publishes
	// them. These are additive only when their page is actually registered.
	if managerial {
		if !peopleShortcutAdded {
			appendHomePageActionInPlace(&actions, view, PagePeople, view.Locale.Text("home.action_find"), "button secondary")
		}
	} else {
		appendHomePageActionInPlace(&actions, view, PageMyself, view.Locale.Text("home.action_profile"), "button secondary")
		appendHomePageActionInPlace(&actions, view, PageTimeOffRequest, view.Locale.Text("home.action_time_off"), "button secondary")
	}
	appendHomePageActionInPlace(&actions, view, PagePayStatements, view.Locale.Text("home.action_pay_statement"), "button secondary")
	return dedupeHomeActions(actions)
}

func appendHomePageActionInPlace(actions *[]ActionLinkProps, view View, page PageID, label, class string) {
	if !view.Allows(page, "view") {
		return
	}
	if page == PageTimeOffRequest && !view.Allows(page, "create") {
		return
	}
	if _, ok := LookupPage(page); !ok {
		return
	}
	*actions = append(*actions, ActionLinkProps{Label: label, Href: statefulHref(view, page), Class: class, Navigate: view.Navigate})
}

func dedupeHomeActions(actions []ActionLinkProps) []ActionLinkProps {
	seen := make(map[string]bool, len(actions))
	result := make([]ActionLinkProps, 0, len(actions))
	for _, action := range actions {
		key := action.Href
		if action.Href == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, action)
	}
	return result
}
