package chatui

import "testing"

// AGENTUX-057: with the "@" menu open and the members still loading there is
// no row to pick, so Enter must neither send the draft nor type a newline.
func TestS24_MentionLoadingEnterDoesNothing(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		state composerKeyState
		want  composerAction
	}{
		{"loading, no row", "Enter", composerKeyState{Draft: "@po", MentionOpen: true, MentionLoading: true}, composerHold},
		{"loading, a row exists", "Enter", composerKeyState{Draft: "@po", MentionOpen: true, MentionLoading: true, MentionHighlighted: true}, composerPickMention},
		{"loaded, nothing matches", "Enter", composerKeyState{Draft: "hi @zz", MentionOpen: true}, composerSend},
		{"menu closed while members load", "Enter", composerKeyState{Draft: "hello", MentionLoading: true}, composerSend},
		{"shift+enter still a newline", "Enter", composerKeyState{Draft: "@po", MentionOpen: true, MentionLoading: true, Shift: true}, composerNative},
		{"tab does not hold", "Tab", composerKeyState{Draft: "@po", MentionOpen: true, MentionLoading: true}, composerNative},
	}
	for _, c := range cases {
		if got := composerKeyAction(c.state, c.key); got != c.want {
			t.Errorf("%s: composerKeyAction = %q, want %q", c.name, got, c.want)
		}
	}
}
