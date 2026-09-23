package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func TestChannelPollUnavailableWithoutConfiguredStore(t *testing.T) {
	service := &ChatExtensions{}
	principal := chat.Principal{TenantID: "tenant", SubjectID: "member"}
	if _, err := service.ChannelPoll(context.Background(), principal, "", "channel"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("read without store = %v", err)
	}
	if _, err := service.MutateChannelPoll(context.Background(), principal, "", "channel", 1, chatstore.ChannelPollMutation{Operation: "CREATE"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("mutation without store = %v", err)
	}
}
