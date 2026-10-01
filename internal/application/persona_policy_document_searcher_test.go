package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"strings"
	"testing"
	"time"
)

type personaPolicyScopeTest struct {
	scope  string
	denied bool
}

func (s *personaPolicyScopeTest) ResolvePersonaDocumentSearchScope(context.Context, PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error) {
	if s.denied {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	return PersonaDocumentSearchScope{ScopeID: s.scope}, nil
}

func TestTodo_AGENTP_011_PersonaPolicySearcherReturnsRealPlacedSource(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "persona-policy-source"
	doc, err := svc.store.CreateDocument(ctx, tenant, "author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: "author", Title: "Approval policy", Markdown: "Expense approval requires a written manager decision.", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: v.ID, ScopeKind: "placement", ScopeID: "room-a", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{documenthubstore.ActionManage, documenthubstore.ActionDeploy} {
		if _, err = svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: "deployer", Action: action, Effect: documenthubstore.EffectAllow, Issuer: "author"}); err != nil {
			t.Fatal(err)
		}
	}
	placement, err := svc.store.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: doc, VersionID: v.ID, ScopeKind: "placement", ScopeID: "room-a", ActorID: "deployer", CustodianID: "deployer", ReviewDueAt: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.store.ShareDocument(ctx, tenant, doc, "author", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err = svc.store.IndexDeployedVersion(ctx, tenant, doc, v.ID); err != nil {
		t.Fatal(err)
	}
	searcher, err := NewPersonaPolicyDocumentSearcher(svc, svc.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := &personaPolicyScopeTest{scope: "room-a"}
	identity := PersonaRunT0ToolInvocation{TenantID: tenant, ConversationID: "room-a", InvokerID: "reader", AgentID: "agent"}
	ctx = context.WithValue(ctx, personaDocumentSearchContextKey{}, personaDocumentSearchContext{identity: identity, source: scope})
	call := personaDocumentSearchCall{TenantID: values.TenantId(tenant), ConversationID: "room-a", InvokerID: "reader", AgentID: "agent", Query: "approval"}
	result, err := searcher.SearchPersonaPolicyDocuments(ctx, call)
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("source missing %+v %v", result, err)
	}
	hit := result.Hits[0]
	if hit.Markdown != v.Markdown || hit.ContentDigest != personaRunT0ToolOutputDigest([]byte(v.Markdown)) || hit.PlacementID != placement.ID || hit.ScopeID != "room-a" || hit.VersionID != v.ID || !strings.Contains(hit.Markdown, "written manager decision") {
		t.Fatalf("source invented %+v", hit)
	}
	scope.denied = true
	if _, err = searcher.SearchPersonaPolicyDocuments(ctx, call); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("current revoked installation allowed %v", err)
	}
	scope.denied = false
	scope.scope = "room-b"
	if _, err = searcher.SearchPersonaPolicyDocuments(ctx, call); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("widened owner scope allowed %v", err)
	}
	if _, err = searcher.SearchPersonaPolicyDocuments(context.Background(), call); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("unproven direct invocation allowed %v", err)
	}
}

func TestTodo_AGENTP_011_RuntimeToolNormalizesActualAdmissionDigest(t *testing.T) {
	raw := strings.Repeat("a", 64)
	if personaRuntimeToolAdmissionDigest(raw) != "sha256:"+raw || personaRuntimeToolAdmissionDigest("sha256:"+raw) != "sha256:"+raw {
		t.Fatal("admission digest encoding changed identity")
	}
}

func TestTodo_AGENTP_011_PersonaDocumentScopeRechecksProductionAuthority(t *testing.T) {
	ctx, tools, grant, personas, _, _, record, run := runtimeToolFixture(t)
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	identity, err := personaRunT0ToolInvocation(record, run)
	if err != nil {
		t.Fatal(err)
	}
	personas.reader.install.ChannelPolicy.ConversationSearchAllowed = true
	scope, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity)
	if err != nil || scope.ScopeID != identity.ConversationID {
		t.Fatalf("current exact placement denied %+v %v", scope, err)
	}
	grant.epoch++
	if _, err = tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("revoked delegation admitted %v", err)
	}
	grant.epoch--
	personas.reader.install.PersonaVersion++
	if _, err = tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("changed immutable persona admitted %v", err)
	}
	personas.reader.install.PersonaVersion--
	personas.reader.install.ChannelPolicy.ConversationSearchAllowed = false
	if _, err = tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("channel denied search admitted %v", err)
	}
	personas.reader.install.ChannelPolicy.ConversationSearchAllowed = true
	identity.InvokerID = "other-user"
	if _, err = tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("different invoker admitted %v", err)
	}
}
