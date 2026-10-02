package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

func TestAgentUXQuality_SourceAccess_Integration(t *testing.T) {
	service := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "quality-links"
	id, err := service.store.CreateDocument(ctx, tenant, "owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: id, CreatorID: "owner", Title: "Paid time off policy", Markdown: "## Carryover\n40 hours", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.store.ShareDocument(ctx, tenant, id, "owner", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	access := AgentUXAnswerSourceAccess{Documents: service.store}
	source := chat.AgentDocumentSource{Title: "Paid time off policy", DocumentID: id, VersionID: version.ID, SectionAnchor: "carryover"}
	got, err := access.ResolveAgentDocumentSource(ctx, chat.Principal{TenantID: tenant, SubjectID: "reader"}, tenant, "general", source)
	if err != nil || !got.Readable || got.Title != "Paid time off policy · Carryover · v1.0.0" || !strings.Contains(got.Href, "version="+version.ID+"#carryover") {
		t.Fatalf("reader source: %+v %v", got, err)
	}
	if _, err = access.ResolveAgentDocumentSource(ctx, chat.Principal{TenantID: tenant, SubjectID: "outsider"}, tenant, "general", source); err == nil {
		t.Fatal("unreadable document linked")
	}
	if _, err = access.ResolveAgentDocumentSource(ctx, chat.Principal{TenantID: "foreign", SubjectID: "reader"}, tenant, "general", source); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("foreign reader: %v", err)
	}
	if _, err = service.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny, Issuer: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err = access.ResolveAgentDocumentSource(ctx, chat.Principal{TenantID: tenant, SubjectID: "reader"}, tenant, "general", source); err == nil {
		t.Fatal("revoked document linked")
	}
}

func TestAgentUXQuality_SourceAccess_Security(t *testing.T) {
	access := AgentUXAnswerSourceAccess{}
	if _, err := access.ResolveAgentDocumentSource(context.Background(), chat.Principal{TenantID: "t", SubjectID: "u"}, "t", "room", chat.AgentDocumentSource{Title: "PTO", Readable: true, Href: "/workspace/app/docs?document=pto"}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("missing authority trusted stored flag: %v", err)
	}
}

func TestAgentUXQuality_LegacySources_Integration(t *testing.T) {
	service := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "quality-legacy"
	place := func(title string) string {
		id, version := deployedSearchFixture(t, service, ctx, tenant, "owner", title, "## Carryover\n40 hours")
		for _, action := range []string{documenthubstore.ActionManage, documenthubstore.ActionRead} {
			if _, err := service.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "u-deployer", Action: action, Effect: documenthubstore.EffectAllow, Issuer: "owner"}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: id, VersionID: version, ScopeKind: "placement", ScopeID: "general", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: documenthubstore.ReviewApproved}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.store.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: id, VersionID: version, ScopeKind: "placement", ScopeID: "general", ActorID: "u-deployer", CustodianID: "owner", ReviewDueAt: time.Now().Add(24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if err := service.ShareDocument(ctx, tenant, "owner", id, "reader", ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := place("Paid time off policy")
	access := AgentUXAnswerSourceAccess{Documents: service.store}
	reader := chat.Principal{TenantID: tenant, SubjectID: "reader"}
	source := chat.AgentDocumentSource{Title: "Paid time off policy (version 1)", SectionTitle: "Carryover"}
	got, err := access.ResolveAgentDocumentSource(ctx, reader, tenant, "general", source)
	if err != nil || !got.Readable || got.DocumentID != id || got.VersionID == "" || !strings.Contains(got.Title, "v1.0.0") || got.SectionAnchor != "carryover" || !strings.HasSuffix(got.Href, "#carryover") {
		t.Fatalf("unique exact legacy title not upgraded: %+v %v", got, err)
	}
	// CHATBUG-020: placement in the conversation is not the only way to resolve a
	// title. Outside the placements the exact title still resolves, once, for a
	// reader the hub lets read it, and never for one it does not.
	if other, err := access.ResolveAgentDocumentSource(ctx, reader, tenant, "another-room", source); err != nil || !other.Readable || other.DocumentID != id {
		t.Fatalf("legacy source did not resolve by its exact title outside the placements: %+v %v", other, err)
	}
	if _, err := access.ResolveAgentDocumentSource(ctx, chat.Principal{TenantID: tenant, SubjectID: "outsider"}, tenant, "another-room", source); err == nil {
		t.Fatal("a reader the hub refuses was given the document by title")
	}
	if _, err := access.ResolveAgentDocumentSource(ctx, reader, tenant, "general", chat.AgentDocumentSource{Title: "paid time off policy"}); err == nil {
		t.Fatal("non-exact title upgraded")
	}
	place("Paid time off policy")
	if _, err := access.ResolveAgentDocumentSource(ctx, reader, tenant, "general", source); err == nil {
		t.Fatal("ambiguous title guessed a document")
	}
}
