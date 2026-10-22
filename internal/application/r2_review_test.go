package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type r2Membership struct {
	member chatcore.Membership
	err    error
}

func (r r2Membership) GetMembership(context.Context, string, string, string, string) (chatcore.Membership, error) {
	return r.member, r.err
}

type r2Conversations struct {
	chatcore.ConversationService
	err error
}

func (r r2Conversations) GetConversation(context.Context, chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{}, r.err
}

var r2Left = time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

var r2Access = chatstream.Access{TenantID: "host", HomeTenantID: "home", ConversationID: "room", SubjectID: "subject", MembershipEpoch: 3}

// A database that did not answer says nothing about whether the person is still
// a member. Reporting it as permission denied told the page their access was
// gone, and the page stopped watching and blanked the conversation.
func TestR2_MembershipLookupThatFailsIsNotPermissionDenied(t *testing.T) {
	p := chatcore.Principal{TenantID: "home", SubjectID: "subject"}
	_, err := chatMembershipEpoch(context.Background(), nil, r2Membership{err: errors.New("connection reset by peer")}, "host", "room", p)
	if !errors.Is(err, chatstream.ErrAuthorityUnavailable) || errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("a failed lookup: %v, want ErrAuthorityUnavailable and not permission denied", err)
	}
	for name, resolver := range map[string]r2Membership{
		"no such member": {err: chatcore.ErrNotFound},
		"left":           {member: chatcore.Membership{LeftAt: &r2Left}},
	} {
		if _, err := chatMembershipEpoch(context.Background(), nil, resolver, "host", "room", p); !errors.Is(err, chatcore.ErrPermissionDenied) {
			t.Fatalf("%s: %v, want permission denied", name, err)
		}
	}
}

func TestR2_AuthorizerSeparatesRefusedFromUndecided(t *testing.T) {
	member := r2Membership{member: chatcore.Membership{Revision: 3}}
	auth := func(conversations chatcore.ConversationService, membership chatMembershipResolver) error {
		return chatServiceStreamAuthorizer{service: conversations, membership: membership}.Authorize(context.Background(), r2Access)
	}
	if err := auth(r2Conversations{}, member); err != nil {
		t.Fatalf("a current member: %v", err)
	}
	if err := auth(r2Conversations{err: errors.New("context deadline exceeded")}, member); !errors.Is(err, chatstream.ErrAuthorityUnavailable) {
		t.Fatalf("conversation read that failed: %v, want ErrAuthorityUnavailable", err)
	}
	if err := auth(r2Conversations{err: chatcore.ErrPermissionDenied}, member); !errors.Is(err, chatcore.ErrPermissionDenied) || errors.Is(err, chatstream.ErrAuthorityUnavailable) {
		t.Fatalf("conversation read that was refused: %v, want permission denied", err)
	}
	if err := auth(r2Conversations{}, r2Membership{err: errors.New("pool exhausted")}); !errors.Is(err, chatstream.ErrAuthorityUnavailable) {
		t.Fatalf("membership read that failed: %v, want ErrAuthorityUnavailable", err)
	}
	if err := auth(r2Conversations{}, r2Membership{member: chatcore.Membership{Revision: 4}}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("membership changed since the watch opened: %v, want permission denied", err)
	}
}

// The failure the page is handed for an authority that did not answer is the
// retryable one.
func TestR2_UndecidedAuthorityReachesThePageAsUnavailable(t *testing.T) {
	err := chatStreamError(chatAuthorityError(errors.New("connection reset by peer")))
	if !errors.Is(err, chatcore.ErrUnavailable) || errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("stream error for an undecided authority: %v, want unavailable", err)
	}
}
