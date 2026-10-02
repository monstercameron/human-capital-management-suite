package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Document and current installation authority are explicit deterministic
// fixtures. Posting, membership changes and each member's read use PostgreSQL.
func TestAgentUXProactive_MemberAddedAfterPublicReply_Security_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	service := chat.NewService(store, clock)
	service.SetAuthority(servedPersonaChatAuthority{})
	service.SetEphemeralStore(store)
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: owner, TenantID: "tenant-a", ConversationID: "general", Kind: chat.PublicChannel, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "general", SubjectID: "employee", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	root, err := service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: "tenant-a", ConversationID: "general", Body: "Which holidays are coming up?", IdempotencyKey: "question"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "owner", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "public-reply-fixture", CredentialDigest: "fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ctx = trust.WithPrincipal(ctx, principal)
	request := AgentAnnouncementRunRequest{TenantID: "tenant-a", ConversationID: "general", PersonaID: "policy-helper", InstallationID: "install", OwnerID: "owner", OccurrenceID: root.ID, Documents: []AgentAnnouncementResolvedDocument{{DocumentID: "holiday-guide", Version: "1", Title: "2026 holiday guide", Content: "Thanksgiving", Digest: personaRunT0ToolOutputDigest([]byte("Thanksgiving"))}}}
	result := proactiveSealedResult(t, request, "Thanksgiving is coming up.")
	revision, err := store.AudienceRevision(ctx, "tenant-a", "general")
	if err != nil {
		t.Fatal(err)
	}
	members := []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "owner"}, {TenantID: "tenant-a", SubjectID: "employee"}}
	floor := &PersonaRuntimeAudienceFloor{Authority: proactiveAudience{snapshot: chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: "general", Revision: revision, CurrentMembers: members, Complete: true, GuestAndExternalComplete: true}}, Documents: proactiveDocumentAuthority{}}
	committer, err := newPersonaPublicReplyCommitter(store.Store, &personaPipelineCurrentReplyAuthority{runtimeReplyCurrentAuthorityFake: &runtimeReplyCurrentAuthorityFake{}, store: store}, privateChatGatewayVerifiedWorker(t, now), clock)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := NewPersonaReplyDelivery(service, committer, proactiveOutputFloor{floor: floor, conversation: chat.Conversation{TenantID: "tenant-a", ID: "general", Kind: chat.PublicChannel}, body: "Thanksgiving is coming up."})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delivery.Deliver(ctx, PersonaReplyDeliveryRequest{Principal: owner, Output: result.Output, IdempotencyKey: result.Output.Identity().AdmissionID, Documents: []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "holiday-guide"}, Title: "2026 holiday guide"}}})
	if err != nil || !receipt.Public {
		t.Fatalf("public reply: %+v %v", receipt, err)
	}
	if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "general", SubjectID: "new-member", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	sharedBody := ""
	for _, subject := range []string{"owner", "employee", "new-member"} {
		posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
		if err != nil || len(posts.Posts) != 2 || posts.Posts[1].ID != receipt.PublicPostID || posts.Posts[1].AuthorID != "policy-helper" || !strings.Contains(posts.Posts[1].Body, "2026 holiday guide") {
			t.Fatalf("%s saw a different or private reply: %+v %v", subject, posts, err)
		}
		if sharedBody != "" && sharedBody != posts.Posts[1].Body {
			t.Fatal("new member received more source content than existing members")
		}
		sharedBody = posts.Posts[1].Body
	}
}
