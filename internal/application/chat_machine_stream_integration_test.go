package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type machineStreamTestAuthority struct{ store *chatstore.Adapter }

func (a machineStreamTestAuthority) Authorize(ctx context.Context, p chatcore.Principal, c chatcore.Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	if action != chatpolicy.ActionRead {
		return chatpolicy.Input{}, chatcore.ErrPermissionDenied
	}
	epoch, err := a.store.MachineWatchEpoch(ctx, c.TenantID, c.ID, p)
	if err != nil {
		return chatpolicy.Input{}, err
	}
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: epoch},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: false, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: epoch},
		HasMembership: true,
		Now:           now,
	}, nil
}

func TestTodo_CHAT_043_Integration_MachineSignedCursorSkipsOtherConversation(t *testing.T) {
	store := streamIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := chatcore.Principal{TenantID: "host", SubjectID: "owner"}
	for _, id := range []string{"watched-machine", "noisy-machine"} {
		c := chatcore.Conversation{ID: id, TenantID: "host", Kind: chatcore.PublicChannel, Name: id, OwnerID: "owner", Revision: 1}
		m := chatcore.Membership{TenantID: "host", HomeTenantID: "host", ConversationID: id, SubjectID: "owner", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory}
		if _, err := store.CreateConversation(ctx, c, []chatcore.Membership{m}, "create-"+id); err != nil {
			t.Fatal(err)
		}
	}
	joinedAt := time.Now().UTC()
	if err := store.Store.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,1,$5,$6,'ACTIVE','owner',1,$7,$7)`, "host:watched-machine:agent", "host", "watched-machine", "agent", `{"app_id":"agent","version":1,"agent":{"display_name":"Agent"}}`, []string{"chat.posts.read"}, joinedAt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "signed-cursor", IssuedAt: joinedAt.Add(-time.Minute), ExpiresAt: joinedAt.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	machineCtx := trust.WithPrincipal(ctx, identity)
	service := chatcore.NewService(store, time.Now)
	service.SetAuthority(machineStreamTestAuthority{store: store})
	runtime, err := NewChatStreamRuntime(ChatStreamRuntimeConfig{CursorKey: "machine-stream-key", Reader: chatServiceReader{service: service, membership: store, events: store}, Authorizer: chatServiceStreamAuthorizer{service: service, membership: store}, PollInterval: 10 * time.Millisecond, PageLimit: 8, QueueSize: 32, ReplayLimit: 8, CursorTTL: time.Minute, Budgets: defaultChatAdmissionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &streamingChatService{ConversationService: service, runtime: runtime, membership: store}
	first, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: owner, TenantID: "host", ConversationID: "watched-machine", IdempotencyKey: "first"}, chatcore.Post{AuthorID: "owner", Body: "first"})
	if err != nil {
		t.Fatal(err)
	}
	request := chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "host", SubjectID: "agent"}, TenantID: "host", ConversationID: "watched-machine"}
	firstCtx, stopFirst := context.WithCancel(machineCtx)
	events, failures, err := wrapper.WatchConversationWithErrors(firstCtx, request)
	if err != nil {
		stopFirst()
		t.Fatal(err)
	}
	var cursor string
	select {
	case event := <-events:
		if event.Event.Post == nil || event.Event.Post.ID != first.ID || event.ResumeCursor == "" {
			t.Fatalf("first event=%+v", event)
		}
		cursor = event.ResumeCursor
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stopFirst()
	for range failures {
	}
	for i := 0; i < 12; i++ {
		if _, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: owner, TenantID: "host", ConversationID: "noisy-machine", IdempotencyKey: "noise-" + string(rune('a'+i))}, chatcore.Post{AuthorID: "owner", Body: "noise"}); err != nil {
			t.Fatal(err)
		}
	}
	second, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: owner, TenantID: "host", ConversationID: "watched-machine", IdempotencyKey: "second"}, chatcore.Post{AuthorID: "owner", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	request.ResumeCursor = cursor
	resumed, resumeFailures, err := wrapper.WatchConversationWithErrors(machineCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-resumed:
		if event.Event.Post == nil || event.Event.Post.ID != second.ID || event.ResumeCursor == cursor {
			t.Fatalf("resumed event=%+v", event)
		}
	case err := <-resumeFailures:
		t.Fatalf("resumed stream failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, _, err := wrapper.WatchConversationWithErrors(machineCtx, chatcore.WatchConversationRequest{Principal: request.Principal, TenantID: "host", ConversationID: "watched-machine", AfterSequence: first.Sequence}); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("plain event sequence accepted as durable offset: %v", err)
	}
}
