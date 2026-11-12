package chatui

import "testing"

// TestIssue3603_ClearRefusedChannel: a denied private-channel address leaves
// the refusal on the page after the fragment is removed. Only the refusal is
// cleared; a listed room, a preview and an ordinary ready page are untouched.
func TestIssue3603_ClearRefusedChannel(t *testing.T) {
	listed := []Conversation{{ID: "general"}}
	for _, tc := range []struct {
		name        string
		model       Model
		wantChanged bool
		wantState   LoadState
		wantSelect  string
	}{
		{
			name:        "refused room is cleared",
			model:       Model{State: StateError, Error: "unavailable", SelectedID: "qa-private", Conversations: listed},
			wantChanged: true, wantState: StateReady, wantSelect: "",
		},
		{
			name:        "refused room with an empty list is cleared",
			model:       Model{State: StateError, Error: "unavailable", SelectedID: "qa-private"},
			wantChanged: true, wantState: StateReady, wantSelect: "",
		},
		{
			name:        "listed room keeps its own error",
			model:       Model{State: StateError, Error: "failed", SelectedID: "general", Conversations: listed},
			wantChanged: false, wantState: StateError, wantSelect: "general",
		},
		{
			name:        "preview keeps its state",
			model:       Model{State: StateError, Error: "unavailable", SelectedID: "public", PreviewConversation: &Conversation{ID: "public"}},
			wantChanged: false, wantState: StateError, wantSelect: "public",
		},
		{
			name:        "ready page is untouched",
			model:       Model{State: StateReady, SelectedID: "qa-private"},
			wantChanged: false, wantState: StateReady, wantSelect: "qa-private",
		},
		{
			name:        "error without a selection is a page-level failure",
			model:       Model{State: StateError, Error: "transport"},
			wantChanged: false, wantState: StateError, wantSelect: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			if changed := ClearRefusedChannel(&m); changed != tc.wantChanged {
				t.Fatalf("ClearRefusedChannel changed = %v, want %v", changed, tc.wantChanged)
			}
			if m.State != tc.wantState || m.SelectedID != tc.wantSelect {
				t.Fatalf("after clear state=%q selected=%q, want state=%q selected=%q", m.State, m.SelectedID, tc.wantState, tc.wantSelect)
			}
			if tc.wantChanged && m.Error != "" {
				t.Fatalf("a cleared refusal still carries error %q", m.Error)
			}
		})
	}
	if ClearRefusedChannel(nil) {
		t.Fatal("ClearRefusedChannel(nil) reported a change")
	}
}
