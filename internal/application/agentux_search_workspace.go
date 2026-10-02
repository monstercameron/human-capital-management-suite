package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

const (
	personaConversationSearchScope    = "conversation"
	personaWorkspaceSearchScope       = "workspace-public"
	personaWorkspaceSearchTool        = "workspace_documents_search"
	personaWorkspaceSearchSkillID     = "hcmnext.skill.workspace_document_search"
	workspaceSearchUnavailableMessage = "Assistant cannot search workspace documents right now"
)

// WorkspaceDocumentMembers must return the current complete tenant directory,
// never just members of the invoking conversation or caller-supplied IDs.
type WorkspaceDocumentMembers interface {
	WorkspaceDocumentMembers(context.Context, string) ([]string, error)
}

type WorkspaceSearchUnavailable struct{ Cause error }

func (e *WorkspaceSearchUnavailable) Error() string { return workspaceSearchUnavailableMessage }
func (e *WorkspaceSearchUnavailable) Unwrap() error { return e.Cause }

func (s *PersonaPolicyDocumentSearcher) searchWorkspace(ctx context.Context, call personaDocumentSearchCall) (PersonaPolicyDocumentSearchResult, error) {
	refused := func(cause error) (PersonaPolicyDocumentSearchResult, error) {
		return PersonaPolicyDocumentSearchResult{}, &WorkspaceSearchUnavailable{Cause: cause}
	}
	if isNilPersonaOutputPort(s.members) {
		return refused(documenthubstore.ErrWorkspaceMeaningUnavailable)
	}
	members, err := s.members.WorkspaceDocumentMembers(ctx, call.TenantID.String())
	if err != nil || len(members) == 0 {
		return refused(err)
	}
	hits, searched, err := s.workspaceMeaningHits(ctx, call, members)
	if err != nil {
		return PersonaPolicyDocumentSearchResult{}, personaRuntimeToolDeniedHere()
	}
	if !searched {
		// No usable local embedding model, or nothing indexed yet: the same
		// grant-filtered candidates are matched by the words of the question,
		// and the person is never told about models.
		hits, err = s.documents.SearchWorkspaceKeyword(ctx, call.TenantID.String(), call.InvokerID, call.Query, members)
		if err != nil {
			return PersonaPolicyDocumentSearchResult{}, personaRuntimeToolDeniedHere()
		}
	}
	result := PersonaPolicyDocumentSearchResult{Hits: []PersonaPolicyDocumentSearchHit{}}
	for _, hit := range hits {
		// Membership and grants are live authority, not attributes of a vector.
		current, err := s.members.WorkspaceDocumentMembers(ctx, call.TenantID.String())
		if err != nil {
			return refused(err)
		}
		public, err := s.documents.WorkspaceDocumentReadable(ctx, call.TenantID.String(), hit.DocumentID, current)
		if err != nil {
			return refused(err)
		}
		if !public {
			continue
		}
		version, err := s.documents.ReadVersion(ctx, call.TenantID.String(), hit.DocumentID, hit.VersionID, "person", call.InvokerID)
		if errors.Is(err, documenthubstore.ErrDenied) {
			continue
		}
		if err != nil {
			return PersonaPolicyDocumentSearchResult{}, personaRuntimeToolDeniedHere()
		}
		text := boundedWorkspaceSection(hit.Text, 8000)
		if text == "" {
			continue
		}
		result.Hits = append(result.Hits, PersonaPolicyDocumentSearchHit{SearchHit: transportdocument.SearchHit{DocumentID: hit.DocumentID, VersionID: hit.VersionID, Title: hit.Title, Locale: version.Locale, OwnerID: hit.OwnerID, Status: "deployed", Score: int(hit.Score * 1000)}, Markdown: text, ContentDigest: personaRunT0ToolOutputDigest([]byte(text)), Classification: version.Classification, ScopeID: personaWorkspaceSearchScope, SectionAnchor: hit.SectionAnchor})
	}
	result.Total = len(result.Hits)
	return result, nil
}

// workspaceMeaningHits searches by meaning when a local embedding model is
// configured and the index holds vectors for the readable documents. It
// reports searched=false, not an error, when that is not possible, so the
// caller falls back to keywords. Workspace text and queries must stay in
// process, including on deployments whose ordinary hub search has an external
// embedding provider configured, so only a local model is ever used.
func (s *PersonaPolicyDocumentSearcher) workspaceMeaningHits(ctx context.Context, call personaDocumentSearchCall, members []string) ([]documenthubstore.WorkspaceSectionHit, bool, error) {
	if s.embedder == nil || !documentembed.IsLocal(s.embedder) {
		return nil, false, nil
	}
	if _, ok := s.embedder.(interface{ Dim() int }); !ok {
		return nil, false, nil
	}
	embedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	vecs, err := s.embedder.Embed(embedCtx, []string{call.Query})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, false, nil
	}
	hits, err := s.documents.SearchWorkspaceMeaning(ctx, call.TenantID.String(), call.InvokerID, call.Query, s.embedder.Model(), vecs[0], members)
	if errors.Is(err, documenthubstore.ErrWorkspaceMeaningUnavailable) {
		return nil, false, nil
	}
	return hits, err == nil, err
}

func boundedWorkspaceSection(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	// Pin complete lines so independent section verification can compare bytes.
	if line := strings.LastIndexByte(text[:end], '\n'); line > 0 {
		end = line + 1
	}
	return text[:end]
}

// WorkspaceDocumentSearchStatus is the three-field setup projection. A zero
// timestamp is an absent index, not a successful indexing time.
type WorkspaceDocumentSearchStatus struct {
	WorkspaceDocuments int
	WorkspaceIndexedAt time.Time
	WorkspacePending   int
}

func (s *PersonaPolicyDocumentSearcher) WorkspaceDocumentSearchStatus(ctx context.Context, tenant string) (WorkspaceDocumentSearchStatus, error) {
	var out WorkspaceDocumentSearchStatus
	if s == nil || s.documents == nil || isNilPersonaOutputPort(s.members) {
		return out, &WorkspaceSearchUnavailable{}
	}
	members, err := s.members.WorkspaceDocumentMembers(ctx, tenant)
	if err != nil {
		return out, err
	}
	model := ""
	if s.embedder != nil {
		model = s.embedder.Model()
	}
	status, err := s.documents.WorkspaceIndexStatus(ctx, tenant, model, members)
	return WorkspaceDocumentSearchStatus{WorkspaceDocuments: status.Documents, WorkspaceIndexedAt: status.IndexedAt, WorkspacePending: status.Pending}, err
}
