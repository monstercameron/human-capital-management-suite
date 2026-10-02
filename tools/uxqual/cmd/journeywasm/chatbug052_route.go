package main

import (
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-052. Moderation, a search and the Saved panel are part of the
// address, so a reload, Back, Forward and a copied link bring them back:
//
//	#moderation        the Moderation page
//	#search=<words>    the search results for the words
//	#saved             the Saved panel
//
// The conversation keeps its own "#channel=" form (chat_history_wasm.go). Only
// one of them is the address at a time; when more than one is showing, the
// Moderation page wins over the search, and the search over the Saved panel.

// chatPageFragment is the fragment that names a page, or "" for the
// conversation itself.
func chatPageFragment(kind, query string) string {
	switch kind {
	case chatui.ChatPageModeration:
		return "#moderation"
	case chatui.ChatPageSaved:
		return "#saved"
	case chatui.ChatPageSearch:
		if query = strings.TrimSpace(query); query != "" {
			return "#search=" + url.QueryEscape(query)
		}
	}
	return ""
}

// parseChatPageFragment is the page a fragment names, with the words of a
// search; the kind is "" for any other fragment.
func parseChatPageFragment(hash string) (kind, query string) {
	switch {
	case hash == "#moderation":
		return chatui.ChatPageModeration, ""
	case hash == "#saved":
		return chatui.ChatPageSaved, ""
	case strings.HasPrefix(hash, "#search="):
		words, err := url.QueryUnescape(strings.TrimPrefix(hash, "#search="))
		if words = strings.TrimSpace(words); err == nil && words != "" && len(words) <= 512 {
			return chatui.ChatPageSearch, words
		}
	}
	return "", ""
}

// chatPageShowing is the page the address should name, given what is on the
// screen: the Moderation page, a search whose results are showing (not one
// whose result is open), or the Saved panel.
func chatPageShowing(moderation, saved bool, search string, searchOpened bool) (kind, query string) {
	switch search = strings.TrimSpace(search); {
	case moderation:
		return chatui.ChatPageModeration, ""
	case search != "" && !searchOpened:
		return chatui.ChatPageSearch, search
	case saved:
		return chatui.ChatPageSaved, ""
	}
	return "", ""
}

// chatTabTitleBase is the page's own title, which the chat's title is put in
// front of. It is the title the document has now unless the document still
// holds the one this code set last: then nothing else has changed it and the
// base kept so far is still the base.
func chatTabTitleBase(current, last, base string) string {
	if base == "" || current != last {
		return current
	}
	return base
}

// chatTabTitleText is the tab's title: what the page shows, then the page's
// own title ("#random · Chat").
func chatTabTitleText(base, shown string) string {
	if strings.TrimSpace(shown) == "" {
		return base
	}
	return strings.TrimSpace(shown) + " · " + base
}

// chatChannelAddress splits a "#channel=" address into the part that names the
// conversation and the tab the address asks for. "#channel=general&tab=docs"
// opens #general with Conversation details shown; an address with no extra
// parts, or with ones this page does not know, is its channel part alone, so a
// link written by a newer page still opens the conversation.
func chatChannelAddress(hash string) (channelHash, tab string) {
	if !strings.HasPrefix(hash, "#channel=") {
		return hash, ""
	}
	value, extra, found := strings.Cut(hash[1:], "&")
	if !found {
		return hash, ""
	}
	params, err := url.ParseQuery(extra)
	if err != nil {
		return hash, ""
	}
	switch requested := params.Get("tab"); requested {
	case "docs", "details", "members":
		tab = requested
	}
	return "#" + value, tab
}
