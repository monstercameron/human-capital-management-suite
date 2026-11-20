package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3793Roster is a conversation whose members are known exactly: two people
// who are in, one who left, one who was invited but never joined, and one whose
// home is another tenant.
type issue3793Roster struct{ extensionConversations }

func (r issue3793Roster) ListMemberships(_ context.Context, req chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	at := time.Now().Add(-time.Hour)
	left := time.Now().Add(-time.Minute)
	home := req.TenantID
	m := func(subject, tenant string, joined, gone *time.Time, role chat.MembershipRole) chat.Membership {
		return chat.Membership{TenantID: req.TenantID, ConversationID: req.ConversationID, HomeTenantID: tenant, SubjectID: subject, Role: role, JoinedAt: joined, LeftAt: gone}
	}
	return chat.ListMembershipsResponse{Memberships: []chat.Membership{
		m("person", home, &at, nil, chat.Member), m("boss", home, &at, nil, chat.Member), m("owner", home, &at, nil, chat.Manager),
		m("ghost", home, &at, &left, chat.Member), m("invited", home, nil, nil, chat.Member), m("alien", "tenant-z", &at, nil, chat.Member),
	}}, nil
}

type issue3793Kernel struct{ proposals []chatapps.Proposal }

func (k *issue3793Kernel) Propose(_ context.Context, p chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	k.proposals = append(k.proposals, p)
	return chatapps.ProposalReceipt{IntentID: "intent-9", Status: "DRAFT"}, nil
}

type issue3793Authority struct{ err error }

func (a issue3793Authority) AuthorizeChatAppCommand(context.Context, chat.Principal, string, string, string) error {
	return a.err
}

// The use case over the real chat extensions: the person's own reads of the
// conversation decide who may see the form and who may be named in it.
func TestIssue3793_RequestsOverTheRealExtensions_Integration(t *testing.T) {
	ctx := context.Background()
	kernel := &issue3793Kernel{}
	roster := issue3793Roster{}
	ext := &ChatExtensions{
		Conversations: roster,
		Apps:          &chatapps.Service{Repo: chatapps.NewMemoryRepository(), Authority: ChatAppAuthority{Conversations: roster}, Intent: kernel},
		Records:       &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}},
	}
	human := issue3795Person(t, "person", trust.SubjectKindHuman)
	tenant := human.Tenant().String()
	manifest := chatapps.Manifest{AppID: "hr-assistant", Version: 1, Scopes: []string{"requests:leave"},
		Commands: []chatapps.Command{{Name: "leave", Scope: "requests:leave"}}, Agent: &chatapps.AgentManifest{DisplayName: "HR Assistant"}}
	installed, err := ext.Install(ctx, chat.Principal{TenantID: tenant, SubjectID: "owner"}, "room", manifest, []string{"requests:leave"})
	if err != nil {
		t.Fatal(err)
	}
	requests := NewChatIntentRequests(ext)
	if requests == nil {
		t.Fatal("the use case must compose over the extensions")
	}
	values := func(employee string) map[string]string {
		return map[string]string{"employee": employee, "starts": "2026-12-01", "ends": "2026-12-02", "reason": "Rest"}
	}

	// No command authority wired: the installed agent alone never lets anyone ask.
	got, err := requests.Availability(ctx, human, "room")
	if err != nil || got[0].Available || got[0].Reason != ChatIntentRequestNotAllowed {
		t.Fatalf("without the person's own authority nothing is available: %+v %v", got, err)
	}
	ext.AppCommandAuthorization = issue3793Authority{}
	if got, err = requests.Availability(ctx, human, "room"); err != nil || !got[0].Available {
		t.Fatalf("authorized member: %+v %v", got, err)
	}

	// Only people who are in the conversation now can be named.
	for _, who := range []string{"ghost", "invited", "alien", "stranger"} {
		_, err := requests.Submit(ctx, human, "room", "leave", values(who), "submit-0001-aaaa")
		var invalid *ChatIntentRequestInvalidError
		if !errors.As(err, &invalid) || invalid.Fields["employee"] != chat.FormErrUnknownUser {
			t.Fatalf("%s must not be nameable: %v", who, err)
		}
	}
	if len(kernel.proposals) != 0 {
		t.Fatal("a refused request reached the intent kernel")
	}

	// The conversation's own reader check decides who may even ask what is there.
	outsider := issue3795Person(t, "outsider", trust.SubjectKindHuman)
	if _, err := requests.Availability(ctx, outsider, "room"); !errors.Is(err, ErrChatIntentRequestNotFound) {
		t.Fatalf("a non-member must read as not found: %v", err)
	}
	if _, err := requests.Submit(ctx, outsider, "room", "leave", values("person"), "submit-0001-aaaa"); !errors.Is(err, ErrChatIntentRequestNotFound) {
		t.Fatalf("a non-member must not submit: %v", err)
	}

	receipt, err := requests.Submit(ctx, human, "room", "leave", values("boss"), "submit-0001-aaaa")
	if err != nil || receipt.IntentID != "intent-9" || len(kernel.proposals) != 1 {
		t.Fatalf("receipt = %+v %v proposals=%d", receipt, err, len(kernel.proposals))
	}
	sent := kernel.proposals[0]
	if sent.Tenant != tenant || sent.Principal != "person" || sent.Conversation != "room" || sent.AgentInstallation != installed.ID {
		t.Fatalf("the proposal must carry the asking person, tenant and conversation: %+v", sent)
	}

	// A paused installation stops submissions, not only the menu.
	if _, err := ext.ChangeStatus(ctx, chat.Principal{TenantID: tenant, SubjectID: "owner"}, "room", installed.ID, chatapps.Suspended); err != nil {
		t.Fatal(err)
	}
	var gone *ChatIntentRequestNotAvailableError
	if _, err := requests.Submit(ctx, human, "room", "leave", values("boss"), "submit-0002-bbbb"); !errors.As(err, &gone) || gone.Reason != ChatIntentRequestPaused {
		t.Fatalf("a suspended agent must not take a request: %v", err)
	}
	if len(kernel.proposals) != 1 {
		t.Fatal("a paused installation still reached the intent kernel")
	}

	// The person's authority is the person's: a refusal there ends it.
	ext.AppCommandAuthorization = issue3793Authority{err: chat.ErrPermissionDenied}
	if _, err := ext.ChangeStatus(ctx, chat.Principal{TenantID: tenant, SubjectID: "owner"}, "room", installed.ID, chatapps.Active); err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Submit(ctx, human, "room", "leave", values("boss"), "submit-0003-cccc"); !errors.As(err, &gone) || gone.Reason != ChatIntentRequestNotAllowed {
		t.Fatalf("a person without authority: %v", err)
	}
}
