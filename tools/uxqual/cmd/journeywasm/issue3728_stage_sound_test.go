package main

import (
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A workflow decision line is a record, not a message: it does not ding in a
// direct conversation the way a person writing would (#3728).
func TestIssue3728_DecisionLineDoesNotPlayAMessageSound(t *testing.T) {
	now := time.Date(2026, time.November, 20, 16, 0, 0, 0, time.UTC)
	model := chatui.Model{
		CurrentUser:   "ari",
		Conversations: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}},
		Preferences:   chatui.Preferences{Notifications: map[string]chatui.NotificationMode{"dm": chatui.NotifyAll}},
	}
	body, err := chat.JourneyStageBody(chat.JourneyStageLine{Outcome: chat.JourneyStageApproved, Decider: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	line := &chatv1.Post{Id: "stage", AuthorId: "sam", Body: body, Sequence: 1, CreatedAt: timestamppb.New(now)}
	if shouldPlayChatSound(model, "dm", line, now, false, false) {
		t.Fatal("a decision line played a message sound")
	}
	spoken := &chatv1.Post{Id: "hello", AuthorId: "sam", Body: "hello", Sequence: 2, CreatedAt: timestamppb.New(now)}
	if !shouldPlayChatSound(model, "dm", spoken, now, false, false) {
		t.Fatal("an ordinary direct message lost its sound")
	}
}
