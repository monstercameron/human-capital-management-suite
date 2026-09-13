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
	if peopleDirectoryOnlyRouteChange(previous, current) {
		// Keep focus on the activated table header or collection control. The
		// browser then preserves the independently scrolling main viewport.
		return "", false
	}
	if navigationOnlyRouteChange(previous, current) {
		// The navigation toggle owns focus. Do not jump the main viewport back
		// to its heading for a shell-only state change.
		return "", false
	}
	return productPageFocusSelector, false
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
