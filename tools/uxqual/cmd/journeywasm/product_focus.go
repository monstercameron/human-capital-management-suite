package main

import "net/url"

const (
	productPageFocusSelector = "#page-title, #main-content h1"
	menuFilterFocusSelector  = "#menu-filter"
	peoplePath               = "/workspace/app/people"
)

// productRouteFocusTarget keeps focus in the menu filter when the debounce
// changes only menu_q. Other software-navigation changes retain the normal
// SPA behavior of announcing and focusing the new page heading.
func productRouteFocusTarget(previous, current string) (selector string, caretAtEnd bool) {
	if menuFilterOnlyRouteChange(previous, current) {
		return menuFilterFocusSelector, true
	}
	if !productRouteDestinationChanged(previous, current) {
		// Same-destination query changes belong to the control that initiated
		// them. Keep focus and scroll where they are for sorting, filtering,
		// paging, locale, favorites, and other presentation state.
		return "", false
	}
	return productPageFocusSelector, false
}

// productRouteShouldResetMainScroll distinguishes destination navigation from
// presentation changes. The product shell owns an independently scrolling main
// region, so the browser cannot provide its usual new-document scroll reset.
func productRouteShouldResetMainScroll(previous, current string) bool {
	return productRouteDestinationChanged(previous, current)
}

// productRouteDestinationChanged reports whether the address names a new page
// or a new record/workflow within the same page. All other same-path query
// changes are presentation state and must not disorient the user by moving the
// viewport or page-heading focus.
func productRouteDestinationChanged(previous, current string) bool {
	before, beforeErr := url.Parse(previous)
	after, afterErr := url.Parse(current)
	if beforeErr != nil || afterErr != nil {
		return true
	}
	if before.Path != after.Path {
		return true
	}
	for _, key := range productResourceRouteKeys(before.Path) {
		if before.Query().Get(key) != after.Query().Get(key) {
			return true
		}
	}
	return false
}

func productResourceRouteKeys(path string) []string {
	switch path {
	case "/workspace/app/person":
		return []string{"person"}
	case "/workspace/app/journeys":
		return []string{"journey", "mode", "worker"}
	default:
		return nil
	}
}

func navigationOnlyRouteChange(previous, current string) bool {
	return queryOnlyRouteChange(previous, current, "", map[string]bool{"nav": true})
}

func navigationCollapsedRouteChange(previous, current string) bool {
	if !navigationOnlyRouteChange(previous, current) {
		return false
	}
	parsed, err := url.Parse(current)
	return err == nil && parsed.Query().Get("nav") == "collapsed"
}

func peopleDirectoryOnlyRouteChange(previous, current string) bool {
	return queryOnlyRouteChange(previous, current, peoplePath, map[string]bool{
		"q": true, "team": true, "location": true, "eligible": true,
		"sort": true, "dir": true, "page": true, "page_size": true,
	})
}

func menuFilterOnlyRouteChange(previous, current string) bool {
	return queryOnlyRouteChange(previous, current, "", map[string]bool{"menu_q": true})
}

func queryOnlyRouteChange(previous, current, requiredPath string, mutable map[string]bool) bool {
	before, beforeErr := url.Parse(previous)
	after, afterErr := url.Parse(current)
	if beforeErr != nil || afterErr != nil || before.Path != after.Path || requiredPath != "" && before.Path != requiredPath {
		return false
	}
	beforeQuery, afterQuery := before.Query(), after.Query()
	changed := false
	for key := range mutable {
		if beforeQuery.Get(key) != afterQuery.Get(key) {
			changed = true
		}
		beforeQuery.Del(key)
		afterQuery.Del(key)
	}
	if !changed {
		return false
	}
	return beforeQuery.Encode() == afterQuery.Encode()
}
