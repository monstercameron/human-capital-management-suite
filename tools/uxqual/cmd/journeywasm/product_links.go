package main

import (
	"net/url"
	"strings"
)

// productLinkFallbackAnchor is what the link backstop reads from a clicked
// anchor and the current location.
type productLinkFallbackAnchor struct {
	Href     string // resolved absolute href
	Target   string
	Download bool
	Origin   string // location.origin
	Current  string // location.pathname + location.search
}

// productLinkFallbackTarget decides whether a clicked link should navigate
// in software, and to which in-app address. Only same-origin links under
// /workspace/app/ qualify; a new-tab target, a download, and a link that
// only changes the fragment of the current page (the browser handles those
// without reloading) are left to the browser.
func productLinkFallbackTarget(a productLinkFallbackAnchor) (string, bool) {
	if a.Download || (a.Target != "" && a.Target != "_self") {
		return "", false
	}
	parsed, err := url.Parse(a.Href)
	if err != nil || parsed.Scheme+"://"+parsed.Host != a.Origin {
		return "", false
	}
	if !strings.HasPrefix(parsed.Path, "/workspace/app/") {
		return "", false
	}
	pathAndQuery := parsed.Path
	if parsed.RawQuery != "" {
		pathAndQuery += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" && pathAndQuery == a.Current {
		return "", false
	}
	href := pathAndQuery
	if parsed.Fragment != "" {
		href += "#" + parsed.EscapedFragment()
	}
	return href, true
}
