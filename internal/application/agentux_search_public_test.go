package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestAgentUXSearch_PublicAudience_Security_Integration(t *testing.T) {
	ctx, s, call, _ := agentUXSearchFixture(t)
	id, vid := agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Leave guide", true)
	v, err := s.documents.ReadVersion(ctx, call.TenantID.String(), id, vid, "person", call.InvokerID)
	if err != nil {
		t.Fatal(err)
	}
	request := AgentAnnouncementRunRequest{TenantID: call.TenantID.String(), ConversationID: call.ConversationID, OwnerID: call.InvokerID, PersonaID: "assistant", InstallationID: "installed", OccurrenceID: "public-question", Documents: []AgentAnnouncementResolvedDocument{{DocumentID: id, Version: vid, Title: v.Title, Content: v.Markdown, Digest: personaRunT0ToolOutputDigest([]byte(v.Markdown))}}}
	result := proactiveSealedResult(t, request, "Employees receive paid time off.")
	snapshot := chatrecipient.AudienceSnapshot{TenantID: request.TenantID, ConversationID: request.ConversationID, Revision: 1, Complete: true, GuestAndExternalComplete: true, CurrentMembers: []chatrecipient.AudiencePrincipal{{TenantID: request.TenantID, SubjectID: "owner"}, {TenantID: request.TenantID, SubjectID: "reader"}}}
	floor := &PersonaRuntimeAudienceFloor{Authority: proactiveAudience{snapshot: snapshot}, Documents: AgentAnnouncementHubAuthority{Store: s.documents}}
	conversation := chat.Conversation{TenantID: request.TenantID, ID: request.ConversationID, Kind: chat.PublicChannel}
	decision, err := floor.authorizeDocumentOutput(ctx, result.Output, conversation, "Employees receive paid time off.", dlp.ClassInternal)
	if err != nil || decision.Revision != 1 || decision.Body == "" {
		t.Fatalf("unplaced workspace source could not post through shared rule: %+v %v", decision, err)
	}
	if _, err := s.documents.GrantAction(ctx, request.TenantID, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny, Issuer: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := floor.authorizeDocumentOutput(ctx, result.Output, conversation, "Employees receive paid time off.", dlp.ClassInternal); err == nil {
		t.Fatal("indexed but restricted document could still post publicly")
	}
}

func TestAgentUXSearch_BoundedPublicSection_Security_Integration(t *testing.T) {
	ctx, s, call, _ := agentUXSearchFixture(t)
	id, v, err := s.documents.CreatePersonalDocument(ctx, call.TenantID.String(), "owner", "Long policy", "# Long policy\n"+strings.Repeat("Paid time off requires approval.\n", 400))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.documents.SharePersonalDocumentRole(ctx, call.TenantID.String(), id, "owner", "reader", documenthubstore.RoleViewer); err != nil {
		t.Fatal(err)
	}
	section := agentDocumentSection(v.Markdown, "long-policy")
	bounded := boundedWorkspaceSection(section, 8000)
	if len(bounded) > 8000 || len(bounded) >= len(section) {
		t.Fatal("section bound failed")
	}
	a := WorkspaceSectionDocumentAuthority{AgentAnnouncementHubAuthority: AgentAnnouncementHubAuthority{Store: s.documents}}
	digest := personaRunT0ToolOutputDigest([]byte(bounded))
	if err := a.AuthorizeDocumentRead(ctx, call.TenantID.String(), id, v.ID, digest, "long-policy", call.TenantID.String(), "reader"); err != nil {
		t.Fatalf("exact bounded bytes not readable: %v", err)
	}
	if err := a.AuthorizeDocumentRead(ctx, call.TenantID.String(), id, v.ID, personaRunT0ToolOutputDigest([]byte("forged")), "long-policy", call.TenantID.String(), "reader"); err == nil {
		t.Fatal("forged excerpt authorized")
	}
	if err := a.AuthorizeDocumentRead(ctx, call.TenantID.String(), id, v.ID, digest, "long-policy", call.TenantID.String(), "stranger"); err == nil {
		t.Fatal("bounded excerpt bypassed current grants")
	}
}

func TestAgentUXSearch_UnheadedPublicSection_Security_Integration(t *testing.T) {
	ctx, s, call, _ := agentUXSearchFixture(t)
	id, v, err := s.documents.CreatePersonalDocument(ctx, call.TenantID.String(), "owner", "Plain policy", strings.Repeat("Paid time off requires approval.\n", 400))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.documents.SharePersonalDocumentRole(ctx, call.TenantID.String(), id, "owner", "reader", documenthubstore.RoleViewer); err != nil {
		t.Fatal(err)
	}
	a := WorkspaceSectionDocumentAuthority{AgentAnnouncementHubAuthority: AgentAnnouncementHubAuthority{Store: s.documents}}
	digest := personaRunT0ToolOutputDigest([]byte(boundedWorkspaceSection(v.Markdown, 8000)))
	if err := a.AuthorizeDocumentRead(ctx, call.TenantID.String(), id, v.ID, digest, "", call.TenantID.String(), "reader"); err != nil {
		t.Fatalf("unheaded bounded bytes denied: %v", err)
	}
	if err := a.AuthorizeDocumentRead(ctx, call.TenantID.String(), id, v.ID, digest, "", call.TenantID.String(), "stranger"); err == nil {
		t.Fatal("unheaded section bypassed grants")
	}
}
