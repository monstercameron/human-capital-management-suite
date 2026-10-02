package application

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type proactiveOutputFloor struct {
	floor        *PersonaRuntimeAudienceFloor
	conversation chat.Conversation
	body         string
}

func (f proactiveOutputFloor) AuthorizePersonaOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	return f.floor.authorizeDocumentOutput(ctx, output, f.conversation, f.body, dlp.ClassInternal)
}

type proactivePublicCommit struct {
	writes int
	body   string
}

func (c *proactivePublicCommit) CommitPersonaReply(_ context.Context, request chat.PersonaReplyCommitRequest) (chat.Post, error) {
	c.writes++
	c.body = request.Body
	return chat.Post{ID: "public-1", Body: request.Body}, nil
}

func TestAgentUXProactive_SealedPublicSources_Security(t *testing.T) {
	request := AgentAnnouncementRunRequest{TenantID: "tenant-a", ConversationID: "general", PersonaID: "policy-helper", InstallationID: "install", OwnerID: "owner", OccurrenceID: "occurrence", Documents: []AgentAnnouncementResolvedDocument{{DocumentID: "holiday-guide", Version: "1", Title: "2026 holiday guide", Content: "Thanksgiving", Digest: personaRunT0ToolOutputDigest([]byte("Thanksgiving"))}}}
	result := proactiveSealedResult(t, request, "Thanksgiving is coming up.")
	conversation := chat.Conversation{TenantID: "tenant-a", ID: "general", Kind: chat.PublicChannel}
	members := []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "owner"}, {TenantID: "tenant-a", SubjectID: "employee"}}
	snapshot := chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: "general", Revision: 4, CurrentMembers: members, EligibilityPopulation: members, Complete: true, EligibilityComplete: true, GuestAndExternalComplete: true}
	for _, kind := range []chat.ConversationKind{chat.PublicChannel, chat.PrivateChannel} {
		conversation.Kind = kind
		for _, allowed := range []bool{true, false} {
			documentAuthority := proactiveDocumentAuthority{denied: map[string]bool{"employee": !allowed}}
			floor := &PersonaRuntimeAudienceFloor{Authority: proactiveAudience{snapshot: snapshot}, Documents: documentAuthority}
			service := &agentUXPrivateDeliveryChat{}
			commit := &proactivePublicCommit{}
			delivery, err := NewPersonaReplyDelivery(service, commit, proactiveOutputFloor{floor: floor, conversation: conversation, body: "Thanksgiving is coming up."})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := delivery.Deliver(context.Background(), PersonaReplyDeliveryRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, Output: result.Output, IdempotencyKey: "announcement-test", Documents: []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "holiday-guide"}, Title: "2026 holiday guide"}, {Reference: agentdocref.Reference{DocumentID: "private-payroll"}, Title: "Secret payroll"}}})
			if err != nil {
				t.Fatal(err)
			}
			if allowed {
				if !receipt.Public || commit.writes != 1 || !strings.Contains(commit.body, "2026 holiday guide") || strings.Contains(commit.body, "Secret payroll") || service.ephemeralWrites != 0 {
					t.Fatalf("public source projection leaked or lost a cited source: %+v %s", receipt, commit.body)
				}
			} else if !receipt.Private || commit.writes != 0 || commit.body != "" || service.ephemeralWrites != 1 {
				t.Fatalf("unreadable document entered public message or Sources: %+v %s", receipt, commit.body)
			}
		}
	}
	for _, kind := range []chat.ConversationKind{chat.Direct, chat.Group, "UNKNOWN"} {
		if agentAnnouncementChannel(kind) {
			t.Fatalf("non-channel %s accepted for a public announcement", kind)
		}
	}
	if _, _, err := outputAnnouncementDocuments(agentsecurity.FinalOutputPersistence{}); err != nil {
		t.Fatal(err)
	}
	if err := (&PersonaRuntimeAudienceFloor{}).WithPersonaOutputFence(context.Background(), result.Output, func() error { t.Fatal("unfenced source reached a write"); return nil }); err == nil {
		t.Fatal("document output bypassed the hub fence")
	}
	if _, err := (&PersonaRuntimeAudienceFloor{}).AuthorizePersonaOutput(context.Background(), result.Output); err == nil {
		t.Fatal("missing current installation authority accepted")
	}
}

func TestAgentUXProactive_RequesterAndTenantBinding_Security(t *testing.T) {
	request := AgentAnnouncementRunRequest{TenantID: "tenant-a", ConversationID: "general", PersonaID: "policy-helper", InstallationID: "install", OwnerID: "owner", OccurrenceID: "occurrence", Documents: []AgentAnnouncementResolvedDocument{{DocumentID: "holiday-guide", Version: "1", Title: "2026 holiday guide", Content: "Thanksgiving", Digest: personaRunT0ToolOutputDigest([]byte("Thanksgiving"))}}}
	result := proactiveSealedResult(t, request, "Thanksgiving is coming up.")
	if err := validateAnnouncementOutput(request, &result); err != nil {
		t.Fatal(err)
	}
	if result.Text != "Thanksgiving is coming up." || len(result.CitedDocumentIDs) != 1 || len(result.Sources) != 1 {
		t.Fatalf("sealed text and sources not projected: %+v", result)
	}
	for _, changed := range []AgentAnnouncementRunRequest{
		{TenantID: "other", ConversationID: "general", PersonaID: "policy-helper", InstallationID: "install", OwnerID: "owner", OccurrenceID: "occurrence", Documents: request.Documents},
		{TenantID: "tenant-a", ConversationID: "general", PersonaID: "policy-helper", InstallationID: "install", OwnerID: "other", OccurrenceID: "occurrence", Documents: request.Documents},
	} {
		if err := validateAnnouncementOutput(changed, &result); err == nil {
			t.Fatal("cross-tenant or requester substitution accepted")
		}
	}
	if err := validateAnnouncementOutput(request, &AgentAnnouncementRunResult{Text: "Unsealed output", CitedDocumentIDs: []string{"holiday-guide"}}); err == nil {
		t.Fatal("unsealed model output reached delivery")
	}
}
