package main

import "net/url"

// chatShellLinkClearsChannel reports whether a link points at the ordinary Chat
// page with no conversation fragment: following it removes a "#channel=" address
// without a hashchange or popstate (issue 3603).
func chatShellLinkClearsChannel(href, chatPath string) bool {
	if href == "" {
		return false
	}
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	return u.Path == chatPath && u.Fragment == ""
}
