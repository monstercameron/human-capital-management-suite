package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type streamReaderStub struct{}

func (streamReaderStub) Read(context.Context, chatstream.ReadRequest) (chatstream.Page, error) {
	return chatstream.Page{Complete: true}, nil
}

type streamAuthStub struct{}

func (streamAuthStub) Authorize(context.Context, chatstream.Access) error { return nil }

type membershipResolverStub struct{ member chatcore.Membership }

func (r membershipResolverStub) GetMembership(context.Context, string, string, string, string) (chatcore.Membership, error) {
	return r.member, nil
}

func runtimeConfig(key string) ChatStreamRuntimeConfig {
	return ChatStreamRuntimeConfig{CursorKey: key, Reader: streamReaderStub{}, Authorizer: streamAuthStub{}, PollInterval: time.Hour, PageLimit: 8, QueueSize: 4, ReplayLimit: 8, CursorTTL: time.Minute, Budgets: chatAdmissionConfig()}
}
func chatAdmissionConfig() chatadmission.Config {
	return chatadmission.Config{TenantConcurrent: 2, ConversationConcurrent: 2, SendConcurrent: 1, WatchConcurrent: 1}
}

func TestTodo_CHAT_018_RuntimeDisabledWithoutCursorKey(t *testing.T) {
	if _, err := NewChatStreamRuntime(runtimeConfig("")); !errors.Is(err, ErrChatStreamingDisabled) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_CHAT_046_RuntimeAdmissionWrapsWatch(t *testing.T) {
	r, err := NewChatStreamRuntime(runtimeConfig("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	sub, lease, err := r.Watch(context.Background(), chatstream.WatchRequest{TenantID: "t", SubjectID: "s", ConversationID: "c", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	sub.Close()
}

func TestTodo_CHAT_020_CurrentMembershipResolverBindsEpoch(t *testing.T) {
	got, err := chatMembershipEpoch(context.Background(), nil, membershipResolverStub{member: chatcore.Membership{TenantID: "host", ConversationID: "c", HomeTenantID: "home", SubjectID: "subject", Revision: 77}}, "host", "c", chatcore.Principal{TenantID: "home", SubjectID: "subject"})
	if err != nil || got != 77 {
		t.Fatalf("epoch=%d err=%v", got, err)
	}
}
