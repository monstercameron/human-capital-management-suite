package main

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHAT_035_StreamRevocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"permission denied", status.Error(codes.PermissionDenied, "revoked"), true},
		{"expired session", status.Error(codes.Unauthenticated, "expired"), true},
		{"transient outage", status.Error(codes.Unavailable, "offline"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := chatui.Model{
				State: chatui.StateReady, CurrentTenantID: "tenant-a", CurrentUser: "user-a", SelectedID: "room-a",
				Draft: "unsent private text", Preferences: chatui.Preferences{Drafts: map[string]string{"room-a": "unsent private text", "room-b": "another draft"}},
				Messages: []chatui.Message{{ID: "post-a", Body: "private post"}}, Members: []chatui.Member{{ID: "member-a"}},
				ShowThread: true, ThreadParentID: "post-a", ThreadParent: &chatui.Message{ID: "post-a"},
				ThreadMessages: []chatui.Message{{ID: "reply-a", Body: "private reply"}},
				SearchMessages: []chatui.SearchMessage{{ConversationID: "room-a", Message: chatui.Message{ID: "search-a", Body: "private search result"}}},
				Embeds:         map[string]chatui.LinkEmbed{"share": {Body: "private preview"}},
			}
			got := chatStreamApplyAccessLoss(&model, "room-a", "tenant-a", "user-a", tc.err)
			if got != tc.want {
				t.Fatalf("access-loss application = %v, want %v", got, tc.want)
			}
			if !tc.want {
				if model.State != chatui.StateReady || len(model.Messages) != 1 {
					t.Fatal("transient transport failure cleared an authorized projection")
				}
				return
			}
			if model.State != chatui.StateError || model.Error == "" || model.SelectedID != "room-a" {
				t.Fatalf("revoked conversation state = %+v", model)
			}
			if len(model.Messages)+len(model.Members)+len(model.ThreadMessages)+len(model.SearchMessages)+len(model.Embeds) != 0 || model.ThreadParent != nil || model.ThreadParentID != "" {
				t.Fatal("revoked conversation retained private projection data")
			}
			if model.Draft != "unsent private text" || model.Preferences.Drafts["room-b"] != "another draft" {
				t.Fatal("drafts were removed before chatui could persist their tombstones")
			}
		})
	}
}

func TestTodo_CHAT_035_StreamRevocationIgnoresStaleIdentity(t *testing.T) {
	model := chatui.Model{State: chatui.StateReady, CurrentTenantID: "tenant-b", CurrentUser: "user-b", SelectedID: "room-a", Messages: []chatui.Message{{ID: "post", Body: "current user data"}}}
	if chatStreamApplyAccessLoss(&model, "room-a", "tenant-a", "user-a", status.Error(codes.PermissionDenied, "revoked")) {
		t.Fatal("old identity's refusal changed the current identity's projection")
	}
	if model.State != chatui.StateReady || len(model.Messages) != 1 {
		t.Fatal("stale stream refusal cleared current identity data")
	}
}

func TestTodo_CHAT_035_SecurityStaleSendCannotCrossIdentity(t *testing.T) {
	if chatDraftIdentityMatches("tenant-a", "user-a", "tenant-b", "user-b") {
		t.Fatal("a callback from the previous identity could send using the new session")
	}
	if chatDraftIdentityMatches("tenant-a", "user-a", "tenant-a", "user-b") {
		t.Fatal("a callback from the previous principal could send using the new principal")
	}
	if chatDraftIdentityMatches("", "user-a", "", "user-a") || chatDraftIdentityMatches("tenant-a", "", "tenant-a", "") {
		t.Fatal("an incomplete identity was treated as authorized for draft submission")
	}
	if !chatDraftIdentityMatches("tenant-a", "user-a", "tenant-a", "user-a") {
		t.Fatal("the current identity could not submit its own draft")
	}
}
