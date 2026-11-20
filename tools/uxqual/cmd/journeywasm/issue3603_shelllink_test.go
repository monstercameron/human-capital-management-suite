package main

import "testing"

// Issue 3603: the shell's own Chat link rewrites the address with no hashchange
// or popstate, so the Chat page never heard that a refused "#channel=" link was
// gone. The click listener decides from the link's address alone.
func TestIssue3603_ShellChatLinkLeavesTheChannelAddress(t *testing.T) {
	const chat = "/workspace/app/chat"
	cases := []struct {
		href string
		want bool
	}{
		{"http://127.0.0.1:8080/workspace/app/chat?locale=en-US", true},
		{"/workspace/app/chat", true},
		{"/workspace/app/chat?locale=de-DE#channel=general", false},
		{"/workspace/app/chat/agents?locale=en-US", false},
		{"/workspace/app/home?locale=en-US", false},
		{"", false},
		{"://bad", false},
	}
	for _, tc := range cases {
		if got := chatShellLinkClearsChannel(tc.href, chat); got != tc.want {
			t.Errorf("chatShellLinkClearsChannel(%q) = %v, want %v", tc.href, got, tc.want)
		}
	}
}
