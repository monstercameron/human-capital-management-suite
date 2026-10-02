package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

func TestAgentUXProactive_DocumentAuthority_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "announcement-documents"
	doc, err := svc.store.CreateDocument(ctx, tenant, "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: "owner", Title: "2026 holiday guide", Markdown: "Thanksgiving is November 26, 2026.", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.store.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", CustodianID: "owner", ReviewDueAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	authority := AgentAnnouncementHubAuthority{Store: svc.store}
	digest := personaRunT0ToolOutputDigest([]byte(version.Markdown))
	if placed, err := authority.OfficialConversationPlacement(ctx, tenant, "general", doc, version.ID, digest, ""); err != nil || placed {
		t.Fatalf("placement invented a membership read grant: %t %v", placed, err)
	}
	if err := authority.AuthorizeDocumentRead(ctx, tenant, doc, version.ID, digest, "", tenant, "employee"); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("unreadable member accepted: %v", err)
	}
	grant, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: "employee", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{version.ID, "1"} {
		if err := authority.AuthorizeDocumentRead(ctx, tenant, doc, pin, digest, "", tenant, "employee"); err != nil {
			t.Fatalf("authorized version %s: %v", pin, err)
		}
	}
	if err := authority.AuthorizeDocumentRead(ctx, "other-tenant", doc, version.ID, digest, "", "other-tenant", "employee"); err == nil {
		t.Fatal("cross-tenant document became public")
	}
	if err := authority.AuthorizeDocumentRead(ctx, tenant, doc, version.ID, "sha256:"+strings.Repeat("a", 64), "", tenant, "employee"); err == nil {
		t.Fatal("changed source digest accepted")
	}
	called := false
	if err := authority.WithDocumentReadFence(ctx, tenant, []string{doc}, func() error {
		called = true
		return authority.AuthorizeDocumentRead(ctx, tenant, doc, version.ID, digest, "", tenant, "employee")
	}); err != nil || !called {
		t.Fatalf("fenced read: %t %v", called, err)
	}
	principal := &proactiveServicePrincipal{}
	resolver := AgentAnnouncementReferenceResolver{Hub: svc.store, Principals: principal}
	ref := agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}
	if _, err := resolver.ResolveAnnouncementDocuments(ctx, tenant, "install", []agentdocref.Reference{ref}); err == nil {
		t.Fatal("service borrowed the owner's document access")
	}
	if err := svc.store.SetAnnouncementDocuments(ctx, tenant, "owner", "workload:policy", "ann", []string{doc}, "general"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveAnnouncementDocuments(ctx, tenant, "install", []agentdocref.Reference{ref}); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("unscoped service read accepted: %v", err)
	}
	ctx = context.WithValue(ctx, announcementDraftKey{}, agentstore.Announcement{ID: "ann", TenantKey: tenant, InstallationID: "install", Documents: []agentdocref.Reference{ref}})
	resolved, err := resolver.ResolveAnnouncementDocuments(ctx, tenant, "install", []agentdocref.Reference{ref})
	if err != nil || len(resolved) != 1 || resolved[0].Content != version.Markdown || resolved[0].Version != "1" {
		t.Fatalf("scoped service document resolution: %+v %v", resolved, err)
	}
	principal.denied = true
	if _, err := resolver.ResolveAnnouncementDocuments(ctx, tenant, "install", []agentdocref.Reference{ref}); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("revoked installation resolved document: %v", err)
	}
	if _, err := (AgentAnnouncementServicePrincipal{}).CurrentAnnouncementServicePrincipal(ctx, tenant, "install"); !errors.Is(err, ErrAgentAnnouncementUnavailable) {
		t.Fatalf("missing service authority: %v", err)
	}
	if err := svc.store.RevokeGrant(ctx, tenant, grant.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizeDocumentRead(ctx, tenant, doc, version.ID, digest, "", tenant, "employee"); err == nil {
		t.Fatal("revoked grant accepted")
	}
	if _, err := svc.store.Withdraw(ctx, tenant, documenthubstore.WithdrawInput{DocumentID: doc, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", ExpectedLive: version.ID, Reason: "Policy withdrawn"}); err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizeDocumentRead(ctx, tenant, doc, version.ID, digest, "", tenant, "owner"); err == nil {
		t.Fatal("withdrawn version accepted")
	}
}

type proactiveServicePrincipal struct{ denied bool }

func (p *proactiveServicePrincipal) CurrentAnnouncementServicePrincipal(context.Context, string, string) (AgentAnnouncementInstallationIdentity, error) {
	if p.denied {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementDenied
	}
	return AgentAnnouncementInstallationIdentity{SubjectID: "workload:policy", PrincipalID: "service", ConversationID: "general", PersonaID: "policy-helper"}, nil
}

func TestAgentUXProactive_CitationBinding_Security(t *testing.T) {
	for _, source := range []string{"chat:post", "document:holiday/version:1/foreign", "document:holiday/version:", "document:/version:1", "document:holiday/version:1/section:private"} {
		if _, err := announcementDocumentCitation(agentsecurity.Citation{SourceID: source, Location: source, Digest: "sha256:" + strings.Repeat("a", 64)}); err == nil {
			t.Fatalf("invalid citation accepted: %s", source)
		}
	}
	source := "document:holiday/version:1"
	doc, err := announcementDocumentCitation(agentsecurity.Citation{SourceID: source, Location: source, Digest: "sha256:" + strings.Repeat("a", 64)})
	if err != nil || doc.DocumentID != "holiday" || doc.Version != "1" {
		t.Fatalf("valid citation: %+v %v", doc, err)
	}
}
