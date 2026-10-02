package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTodo_CHATBUG_072_NormalizeFollowsTheRulePrintedUnderTheField(t *testing.T) {
	for _, tc := range []struct {
		typed, name string
		refused     bool
	}{
		{"design-reviews", "design-reviews", false},
		{"General Chat", "general-chat", false},
		{"General Chat!", "general-chat", true},
		{"Q4_Plan 2026", "q4_plan-2026", false},
		{"büro ünd Ärzte", "büro-ünd-ärzte", false},
		{"قناة عامة", "قناة-عامة", false},
		{"#sales/eu", "saleseu", true},
		{strings.Repeat("a", MaxChannelNameRunes+3), strings.Repeat("a", MaxChannelNameRunes), true},
	} {
		name, refused := NormalizeChannelName(tc.typed)
		if name != tc.name || refused != tc.refused {
			t.Errorf("NormalizeChannelName(%q) = %q, %v; want %q, %v", tc.typed, name, refused, tc.name, tc.refused)
		}
	}
}

func TestTodo_CHATBUG_072_ValidChannelName(t *testing.T) {
	for name, want := range map[string]bool{
		"general": true, "design-reviews": true, "q4_plan": true, "2026": true,
		"": false, "General": false, "general chat": false, "general!": false, "--": false, "__": false,
	} {
		if got := ValidChannelName(name); got != want {
			t.Errorf("ValidChannelName(%q) = %v, want %v", name, got, want)
		}
	}
}

// The service repeats the form's rule: a channel whose name breaks it is
// refused with the field error and nothing is stored, while a group keeps the
// free-text name it is given.
func TestTodo_CHATBUG_072_ServiceRefusesABrokenChannelName(t *testing.T) {
	for _, kind := range []ConversationKind{PublicChannel, PrivateChannel} {
		for _, name := range []string{"General Chat!", "General", "", "  ", "a b"} {
			f := &fakeStore{}
			s := newTestService(f, time.Now)
			_, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: kind, Name: name, IdempotencyKey: "k"})
			if !errors.Is(err, ErrChannelName) || !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("%s %q: err = %v, want the channel-name error", kind, name, err)
			}
			if f.mutations != 0 {
				t.Errorf("%s %q: %d store writes for a refused name", kind, name, f.mutations)
			}
		}
		f := &fakeStore{}
		s := newTestService(f, time.Now)
		got, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: kind, Name: " design-reviews ", IdempotencyKey: "k"})
		if err != nil || got.Name != "design-reviews" {
			t.Errorf("%s: a valid name is stored trimmed: %q, %v", kind, got.Name, err)
		}
	}
	f := &fakeStore{}
	s := newTestService(f, time.Now)
	got, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: Group, Name: "Walt, Sam & Loretta", IdempotencyKey: "k", Members: []MemberRef{{TenantID: "t1", SubjectID: "u2"}}})
	if err != nil || got.Name != "Walt, Sam & Loretta" {
		t.Errorf("a group keeps its free-text name: %q, %v", got.Name, err)
	}
}

// A rename follows the same rule as a new channel, and a channel whose stored
// name already breaks the rule can still be changed in other ways while the
// name is left as it is.
func TestTodo_CHATBUG_072_ServiceRefusesABrokenRename(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	stored := conversation()
	stored.Name = "General Chat!"
	f := &fakeStore{conversation: stored, membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: &now, Revision: 2}}
	s := newTestService(f, func() time.Time { return now })
	ctx := context.Background()

	renamed := stored
	renamed.Name = "Design Reviews"
	if _, err := s.UpdateConversation(ctx, UpdateConversationRequest{Principal: principal(), Conversation: renamed, ExpectedRevision: 1}); !errors.Is(err, ErrChannelName) {
		t.Fatalf("a broken new name: err = %v, want the channel-name error", err)
	}
	if f.mutations != 0 {
		t.Fatalf("%d store writes for a refused rename", f.mutations)
	}
	renamed.Name = "design-reviews"
	if got, err := s.UpdateConversation(ctx, UpdateConversationRequest{Principal: principal(), Conversation: renamed, ExpectedRevision: 1}); err != nil || got.Name != "design-reviews" {
		t.Fatalf("a valid new name: %q, %v", got.Name, err)
	}

	f.conversation = stored
	f.mutations = 0
	if _, err := s.UpdateConversation(ctx, UpdateConversationRequest{Principal: principal(), Conversation: stored, ExpectedRevision: 1}); err != nil {
		t.Fatalf("a channel keeping the name it already has: %v", err)
	}
}
