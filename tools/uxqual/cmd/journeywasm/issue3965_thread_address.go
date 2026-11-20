package main

import (
	"net/url"
	"strings"
)

// #3965. The address of an open thread. "#channel=general&openthread=<root id>"
// names the conversation and the root message whose thread is open, so a reload
// (or a pasted address) brings the thread back. The root id is not a grant: the
// thread is opened only once the signed-in reader's own read of the conversation
// holds that message, and any other address falls back to the channel alone.
// ("thread=1" is already the search address's own flag, hence the other name.)
const chatOpenThreadParam = "openthread"

// chatAddressOpenThread is the thread root a "#channel=" address asks to open,
// or "" when it names none.
func chatAddressOpenThread(hash string) string {
	if !strings.HasPrefix(hash, "#channel=") {
		return ""
	}
	_, extra, found := strings.Cut(hash[1:], "&")
	if !found {
		return ""
	}
	params, err := url.ParseQuery(extra)
	if err != nil {
		return ""
	}
	parent := params.Get(chatOpenThreadParam)
	if parent == "" || len(parent) > 256 {
		return ""
	}
	return parent
}

// chatHrefWithThread adds the open thread to a conversation address. Addresses
// that are not a conversation's (Saved, Moderation, a search) stay as they are.
func chatHrefWithThread(href string, state chatNavigationState) string {
	if !state.ShowThread || state.ThreadParentID == "" || !strings.Contains(href, "#channel=") || strings.Contains(href, "&"+chatOpenThreadParam+"=") {
		return href
	}
	return href + "&" + chatOpenThreadParam + "=" + url.QueryEscape(state.ThreadParentID)
}
