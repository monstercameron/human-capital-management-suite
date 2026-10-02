package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentUXSearchMembers struct {
	members []string
	err     error
}

type agentUXSearchScope struct{}

func (agentUXSearchScope) ResolvePersonaDocumentSearchScope(context.Context, PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error) {
	return PersonaDocumentSearchScope{ScopeID: "general", WorkspaceSearchAllowed: true}, nil
}

func (m *agentUXSearchMembers) WorkspaceDocumentMembers(context.Context, string) ([]string, error) {
	return append([]string(nil), m.members...), m.err
}

type agentUXSearchModel struct{ fail bool }

func (*agentUXSearchModel) Model() string { return "agentux-local" }
func (*agentUXSearchModel) Dim() int      { return 256 }
func (*agentUXSearchModel) Local() bool   { return true }
func (m *agentUXSearchModel) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.fail {
		return nil, documenthubstore.ErrEmbeddingDim
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = make([]float32, m.Dim())
		out[i][0] = 1
		if strings.Contains(text, "Salary") {
			out[i][0], out[i][1] = 0, 1
		}
	}
	return out, nil
}

func agentUXSearchFixture(t *testing.T) (context.Context, *PersonaPolicyDocumentSearcher, personaDocumentSearchCall, *agentUXSearchMembers) {
	t.Helper()
	svc := documentServiceFixture(t)
	members := &agentUXSearchMembers{members: []string{"owner", "reader"}}
	searcher, err := NewPersonaPolicyDocumentSearcher(svc, svc.store, members)
	if err != nil {
		t.Fatal(err)
	}
	searcher.embedder = &agentUXSearchModel{}
	identity := PersonaRunT0ToolInvocation{TenantID: "workspace-search", ConversationID: "general", InvokerID: "reader", AgentID: "assistant"}
	ctx := context.WithValue(context.Background(), personaDocumentSearchContextKey{}, personaDocumentSearchContext{identity: identity, source: agentUXSearchScope{}})
	call := personaDocumentSearchCall{TenantID: values.TenantId(identity.TenantID), ConversationID: "general", InvokerID: "reader", AgentID: "assistant", Query: "time away from work", Scope: personaWorkspaceSearchScope}
	return ctx, searcher, call, members
}

func agentUXSearchDocument(t *testing.T, ctx context.Context, s *PersonaPolicyDocumentSearcher, tenant, title string, shared bool) (string, string) {
	t.Helper()
	id, v, err := s.documents.CreatePersonalDocument(ctx, tenant, "owner", title, "# "+title+"\nEmployees receive paid time off.\n")
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		if err := s.documents.SharePersonalDocumentRole(ctx, tenant, id, "owner", "reader", documenthubstore.RoleViewer); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.documents.IndexVersionVectors(ctx, tenant, id, v.ID, documentembed.StoreModel(s.embedder), s.embedder.Embed); err != nil {
		t.Fatal(err)
	}
	return id, v.ID
}

func TestAgentUXSearch_Workspace_Security_Integration(t *testing.T) {
	ctx, s, call, members := agentUXSearchFixture(t)
	id, version := agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Leave guide", true)
	agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Private Salary", false)
	agentUXSearchDocument(t, ctx, s, "foreign", "Foreign leave guide", true)
	result, err := s.SearchPersonaPolicyDocuments(ctx, call)
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("public search: %+v %v", result, err)
	}
	hit := result.Hits[0]
	if hit.DocumentID != id || hit.VersionID != version || hit.SectionAnchor != "leave-guide" || hit.ScopeID != personaWorkspaceSearchScope || hit.PlacementID != "" || hit.ContentDigest != personaRunT0ToolOutputDigest([]byte(hit.Markdown)) {
		t.Fatalf("invented source: %+v", hit)
	}
	conversation := call
	conversation.Scope = ""
	narrow, err := s.SearchPersonaPolicyDocuments(ctx, conversation)
	if err != nil || len(narrow.Hits) != 0 {
		t.Fatalf("Policy Helper scope widened: %+v %v", narrow, err)
	}
	status, err := s.WorkspaceDocumentSearchStatus(ctx, call.TenantID.String())
	if err != nil || status.WorkspaceDocuments != 1 || status.WorkspacePending != 0 {
		t.Fatalf("projection: %+v %v", status, err)
	}
	if _, err := s.documents.GrantAction(ctx, call.TenantID.String(), documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny, Issuer: "owner"}); err != nil {
		t.Fatal(err)
	}
	result, err = s.SearchPersonaPolicyDocuments(ctx, call)
	if err != nil || len(result.Hits) != 0 {
		t.Fatalf("revoked source reached model: %+v %v", result, err)
	}
	members.err = errors.New("directory unavailable")
	_, err = s.SearchPersonaPolicyDocuments(ctx, call)
	var unavailable *WorkspaceSearchUnavailable
	if !errors.As(err, &unavailable) || err.Error() != workspaceSearchUnavailableMessage {
		t.Fatalf("typed absence missing: %v", err)
	}
	members.err = nil
	s.embedder = nil
	result, err = s.SearchPersonaPolicyDocuments(ctx, call)
	if err != nil || len(result.Hits) != 0 {
		t.Fatalf("keyword fallback ignored the deny: %+v %v", result, err)
	}
	call.Scope = "all-tenants"
	_, err = s.SearchPersonaPolicyDocuments(ctx, call)
	if !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("invented scope accepted: %v", err)
	}
}

// With no embedding model the Assistant still answers from documents every
// member may read, found by the words of the question through the same
// grant-filtered searcher, and says nothing about models.
func TestAgentUXSearch_KeywordFallback_Integration(t *testing.T) {
	ctx, s, call, members := agentUXSearchFixture(t)
	id, version := agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Leave guide", true)
	secret, _ := agentUXSearchDocument(t, ctx, s, call.TenantID.String(), "Private paid time off notes", false)
	foreign, _ := agentUXSearchDocument(t, ctx, s, "foreign", "Foreign paid time off guide", true)
	for name, embedder := range map[string]documentembed.Embedder{"no model files": nil, "model that fails": &agentUXSearchModel{fail: true}} {
		s.embedder = embedder
		call.Query = "how much paid time off do I get"
		result, err := s.SearchPersonaPolicyDocuments(ctx, call)
		if err != nil || len(result.Hits) != 1 || result.Unavailable != "" {
			t.Fatalf("%s: keyword search: %+v %v", name, result, err)
		}
		hit := result.Hits[0]
		if hit.DocumentID != id || hit.VersionID != version || hit.DocumentID == secret || hit.DocumentID == foreign || hit.SectionAnchor != "leave-guide" || hit.ScopeID != personaWorkspaceSearchScope || hit.ContentDigest != personaRunT0ToolOutputDigest([]byte(hit.Markdown)) || !strings.Contains(hit.Markdown, "paid time off") {
			t.Fatalf("%s: keyword hit is not a bounded, sealed, readable section: %+v", name, hit)
		}
	}
	// A new member without a grant makes the document no longer workspace-wide.
	members.members = append(members.members, "new-member")
	if result, err := s.SearchPersonaPolicyDocuments(ctx, call); err != nil || len(result.Hits) != 0 {
		t.Fatalf("keyword search ignored a member without access: %+v %v", result, err)
	}
	members.members = []string{"owner", "reader"}
	// With the model present and the index built, meaning search answers the same.
	s.embedder = &agentUXSearchModel{}
	if result, err := s.SearchPersonaPolicyDocuments(ctx, call); err != nil || len(result.Hits) != 1 {
		t.Fatalf("empty index: %+v %v", result, err)
	}
}

func TestAgentUXSearch_Corpus_Performance_Integration(t *testing.T) {
	ctx, s, call, _ := agentUXSearchFixture(t)
	for i := 0; i < 663; i++ {
		agentUXSearchDocument(t, ctx, s, call.TenantID.String(), fmt.Sprintf("Policy %03d", i), true)
	}
	start := time.Now()
	result, err := s.SearchPersonaPolicyDocuments(ctx, call)
	elapsed := time.Since(start)
	if err != nil || len(result.Hits) != 5 {
		t.Fatalf("corpus search: %+v %v", result, err)
	}
	t.Logf("663 documents / 256 dimensions: %s", elapsed)
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("search exceeded 300 ms: %s", elapsed)
	}
}

func TestAgentUXSearch_BoundedSections(t *testing.T) {
	if got := boundedWorkspaceSection("short", 8); got != "short" {
		t.Fatal(got)
	}
	got := boundedWorkspaceSection("# Guide\n"+strings.Repeat("ع", 100), 31)
	if len(got) > 31 || got != "# Guide\n" {
		t.Fatalf("unsafe bounded text %q", got)
	}
	if got := boundedWorkspaceSection(strings.Repeat("ع", 100), 31); len(got) != 30 {
		t.Fatalf("broken unicode bound: %d", len(got))
	}
}

func TestAgentUXSearch_IndexReplay_Integration(t *testing.T) {
	ctx, s, call, members := agentUXSearchFixture(t)
	id, v, err := s.documents.CreatePersonalDocument(ctx, call.TenantID.String(), "owner", "Leave guide", "# Leave guide\nPaid vacation.\n## Request\nAsk your manager.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.documents.SharePersonalDocumentRole(ctx, call.TenantID.String(), id, "owner", "reader", documenthubstore.RoleViewer); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareWorkspaceDocumentIndex(ctx, s.documents, call.TenantID.String(), members, s.embedder)
	if err != nil || first.DocumentsIndexed != 1 || first.SectionsIndexed != 2 || first.Workspace.WorkspaceDocuments != 1 || first.Workspace.WorkspaceIndexedAt.IsZero() || first.Workspace.WorkspacePending != 0 {
		t.Fatalf("first receipt: %+v %v", first, err)
	}
	vectors, err := s.documents.SectionVectors(ctx, call.TenantID.String(), id, v.ID, s.embedder.Model())
	if err != nil || len(vectors) != 2 {
		t.Fatalf("indexer failed: %+v %v", vectors, err)
	}
	second, err := PrepareWorkspaceDocumentIndex(ctx, s.documents, call.TenantID.String(), members, s.embedder)
	if err != nil || second.DocumentsIndexed != 0 || second.SectionsIndexed != 0 || second.Workspace.WorkspaceIndexedAt != first.Workspace.WorkspaceIndexedAt {
		t.Fatalf("replay did work: %+v %v", second, err)
	}
	if _, err := s.documents.CreatePersonalDocumentVersion(ctx, call.TenantID.String(), id, "owner", v.ID, "Leave guide", "# Leave guide\nUpdated paid vacation.\n"); err != nil {
		t.Fatal(err)
	}
	updated, err := PrepareWorkspaceDocumentIndex(ctx, s.documents, call.TenantID.String(), members, s.embedder)
	if err != nil || updated.DocumentsIndexed != 1 || updated.SectionsIndexed != 1 {
		t.Fatalf("new version of existing document omitted from receipt: %+v %v", updated, err)
	}
	if model, err := LocalWorkspaceEmbeddingModel(func(string) string { return t.TempDir() }); model != nil || err == nil || !strings.Contains(err.Error(), "install tokenizer.json and model.safetensors") {
		t.Fatalf("missing model concealed: %v %v", model, err)
	}
}
