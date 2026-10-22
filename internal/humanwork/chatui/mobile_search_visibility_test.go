package chatui

import (
	"strings"
	"testing"
)

func TestMobileSearchResultsVisibleWithDrawerOpen(t *testing.T) {
	model := Model{
		State:       StateReady,
		Locale:      "en-US",
		SidebarOpen: true,
		SelectedID:  "general",
		Search:      "meeting",
		Conversations: []Conversation{{
			ID: "general", Name: "general", Kind: PublicChannel, Joined: true,
		}},
		SearchMessages: []SearchMessage{{
			ConversationID:   "general",
			ConversationName: "general",
			Message:          Message{ID: "post-1", Author: "Theodore Wright", Body: "Meeting notes"},
		}},
		Callbacks: Callbacks{
			Search:            func(string) {},
			OpenSearchMessage: func(string, string, uint64) {},
		},
	}
	markup := renderAgentUXChat3Node(t, Build(model), 390)
	for _, want := range []string{
		`data-sidebar-open="true"`,
		`data-has-selection="true"`,
		`class="chat-main"`,
		`class="chat-search-results"`,
		`search-results-back`,
		`Open message in #general by Theodore Wright`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("mobile search render is missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(AgentUXChat3Styles, `@media(max-width:767px){.chat-workspace[data-sidebar-open="true"] .chat-main:has(.chat-search-results),.chat-workspace[data-has-selection="false"] .chat-main:has(.chat-search-results){display:flex}`) {
		t.Fatal("mobile drawer CSS still hides the main column while search results are mounted")
	}
	for _, preserved := range []string{
		`.chat-workspace[data-sidebar-open="true"] .chat-layout`,
		`.chat-workspace[data-sidebar-open="true"] .chat-main,.chat-workspace[data-sidebar-open="true"] .chat-side`,
		`.chat-side.thread-pane{position:fixed`,
		`@media(hover:none){.chat-rail-row .rail-row-more{position:absolute;opacity:0}`,
	} {
		if !strings.Contains(AgentUXChat3Styles, preserved) {
			t.Fatalf("existing mobile drawer contract %q was removed", preserved)
		}
	}

	noSelection := model
	noSelection.SelectedID = ""
	noSelection.Conversations = nil
	noSelectionMarkup := renderAgentUXChat3Node(t, Build(noSelection), 390)
	for _, want := range []string{`data-has-selection="false"`, `class="chat-search-results"`, `search-results-back`} {
		if !strings.Contains(noSelectionMarkup, want) {
			t.Fatalf("search without an open conversation is missing %q: %s", want, noSelectionMarkup)
		}
	}

	closed := model
	closed.Search = ""
	closed.SearchMessages = nil
	closedMarkup := renderAgentUXChat3Node(t, Build(closed), 390)
	if strings.Contains(closedMarkup, `class="chat-search-results"`) {
		t.Fatal("closed search retains a results panel that could intercept the mobile conversation drawer")
	}
}
