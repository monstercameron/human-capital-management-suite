package main

import (
	"net/url"
	"strings"
)

const docsSharePath = "/workspace/app/docs"

// docShareCanonical keeps only the selector that names a document, so the link
// Send to chat posts is the canonical address chat unfurls (issue 3748). It
// returns "" for any other address.
func docShareCanonical(href string) string {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != docsSharePath {
		return ""
	}
	id := strings.TrimSpace(parsed.Query().Get("document"))
	if id == "" {
		return ""
	}
	return docsSharePath + "?document=" + url.QueryEscape(id)
}
