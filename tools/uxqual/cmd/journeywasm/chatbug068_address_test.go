package main

import "testing"

// The addresses Chat knows each read back to what they name, and a channel
// address may carry the tab it asks for: "#channel=general&tab=docs" opens
// #general with Conversation details shown, and an extra part this page does
// not know leaves the conversation as the only thing the address asks for.
func TestTodo_CHATBUG_068_AddressNamesConversationAndTab(t *testing.T) {
	cases := []struct {
		hash, wantChannel, wantTab string
	}{
		{"#channel=general", "#channel=general", ""},
		{"#channel=general&tab=docs", "#channel=general", "docs"},
		{"#channel=general&tab=details", "#channel=general", "details"},
		{"#channel=general&tab=members", "#channel=general", "members"},
		{"#channel=General+Chat%21&tab=docs", "#channel=General+Chat%21", "docs"},
		{"#channel=general&tab=somethingnew", "#channel=general", ""},
		{"#channel=general&utm=1", "#channel=general", ""},
		{"#channel=general&tab=docs&x=1", "#channel=general", "docs"},
		{"#moderation", "#moderation", ""},
		{"#saved", "#saved", ""},
		{"#search=holiday+pay", "#search=holiday+pay", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		channel, tab := chatChannelAddress(tc.hash)
		if channel != tc.wantChannel || tab != tc.wantTab {
			t.Errorf("%q: channel %q tab %q, want %q %q", tc.hash, channel, tab, tc.wantChannel, tc.wantTab)
		}
	}
	// The other pages' addresses are untouched by the channel parser.
	for hash, want := range map[string]string{"#moderation": "moderation", "#saved": "saved", "#search=holiday+pay": "search"} {
		kind, _ := parseChatPageFragment(hash)
		if kind != want {
			t.Errorf("%q is page %q, want %q", hash, kind, want)
		}
	}
}

// An address typed into the tab is read once, and the addresses the page writes
// itself are never read as typing: the claim the page makes for its own write
// is what a later list read compares the address against.
func TestTodo_CHATBUG_068_TypedAddressesAreReadAndOwnWritesAreNot(t *testing.T) {
	state := newChatStateForTest(t)
	claim := func(hash string) bool { return state.claimChannelFragment("northwind\x00avery\x00" + hash) }

	if !claim("#channel=general") {
		t.Fatal("an address read for the first time was refused")
	}
	if claim("#channel=general") {
		t.Fatal("the same address was read twice")
	}
	// The page writes "#channel=random" itself when a conversation is clicked;
	// it claims that address, so a read of the list that follows finds nothing new.
	state.claimChannelFragment("northwind\x00avery\x00#channel=random")
	if claim("#channel=random") {
		t.Fatal("the page's own address was read as typing")
	}
	// Typing the earlier address again, after the page has moved on, is a request.
	if !claim("#channel=general") {
		t.Fatal("typing an address the tab had left was ignored")
	}
	// An address that names no channel resets the claim, so the same channel
	// address typed after a page address is read again.
	state.claimChannelFragment("")
	if !claim("#channel=general") {
		t.Fatal("an address typed after a page address was ignored")
	}
}
