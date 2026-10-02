package main

import "net/url"

// shellfixNavigationExpanded identifies the one explicit route value that
// must override the responsive compact-rail default.
func shellfixNavigationExpanded(rawQuery string) bool {
	values, err := url.ParseQuery(rawQuery)
	return err == nil && values.Get("nav") == "expanded"
}
