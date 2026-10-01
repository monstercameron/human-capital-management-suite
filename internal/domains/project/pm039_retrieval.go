package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type BoardSourceKind string

const (
	BoardSourceChatPost         BoardSourceKind = "CHAT_POST"
	BoardSourceDeployedDocument BoardSourceKind = "DEPLOYED_DOCUMENT"
)

type BoardGrantSubject string

const (
	BoardGrantRequester    BoardGrantSubject = "REQUESTER"
	BoardGrantInstallation BoardGrantSubject = "INSTALLATION"
)

var (
	ErrInvalidBoardRetrieval = errors.New("project: invalid board retrieval request")
	ErrUnscopedBoardSource   = errors.New("project: board source is not authorized for requester and installation")
)

// BoardSourceRef pins a source to the exact revision used during proposal
// generation. A later document deployment or chat edit is never silently
// substituted.
type BoardSourceRef struct {
	Kind           BoardSourceKind `json:"kind"`
	ID             string          `json:"id"`
	Revision       string          `json:"revision"`
	ConversationID string          `json:"conversation_id,omitempty"`
	ScopeID        string          `json:"scope_id,omitempty"`
}

func (r BoardSourceRef) Validate() error {
	if !validProposalID(r.ID) || !validProposalID(r.Revision) {
		return ErrInvalidBoardRetrieval
	}
	switch r.Kind {
	case BoardSourceChatPost:
		if !validProposalID(r.ConversationID) || r.ScopeID != "" {
			return ErrInvalidBoardRetrieval
		}
	case BoardSourceDeployedDocument:
		if !validProposalID(r.ScopeID) || r.ConversationID != "" {
			return ErrInvalidBoardRetrieval
		}
	default:
		return ErrInvalidBoardRetrieval
	}
	return nil
}

type BoardRetrievalRequest struct {
	TenantID       string
	RequesterID    string
	InstallationID string
}

type BoardRetrievalGrant struct {
	TenantID       string            `json:"tenant_id"`
	SubjectKind    BoardGrantSubject `json:"subject_kind"`
	SubjectID      string            `json:"subject_id"`
	InstallationID string            `json:"installation_id"`
	Source         BoardSourceRef    `json:"source"`
	Allowed        bool              `json:"allowed"`
}

type BoardRetrievalContent struct {
	Source  BoardSourceRef `json:"source"`
	Title   string         `json:"title"`
	Excerpt string         `json:"excerpt"`
}

// BoardRetrievedSource marks source text as untrusted data. Callers may cite
// it, but must not treat its contents as instructions or capability grants.
type BoardRetrievedSource struct {
	Source        BoardSourceRef `json:"source"`
	Title         string         `json:"title"`
	Excerpt       string         `json:"excerpt"`
	TextUntrusted bool           `json:"text_untrusted"`
}

// RetrieveBoardSources applies the intersection of requester and installation
// grants before projecting any title or excerpt. Denied or missing sources are
// omitted with no existence-revealing placeholder.
func RetrieveBoardSources(request BoardRetrievalRequest, grants []BoardRetrievalGrant, contents []BoardRetrievalContent) ([]BoardRetrievedSource, error) {
	if !validProposalID(request.TenantID) || !validProposalID(request.RequesterID) || !validProposalID(request.InstallationID) {
		return nil, ErrInvalidBoardRetrieval
	}
	const (
		requesterGrant = 1 << iota
		installationGrant
	)
	authorized := make(map[string]uint8, len(grants))
	for _, grant := range grants {
		if grant.TenantID != request.TenantID || grant.InstallationID != request.InstallationID || !grant.Allowed || grant.Source.Validate() != nil {
			continue
		}
		key := boardSourceKey(grant.Source)
		switch {
		case grant.SubjectKind == BoardGrantRequester && grant.SubjectID == request.RequesterID:
			authorized[key] |= requesterGrant
		case grant.SubjectKind == BoardGrantInstallation && grant.SubjectID == request.InstallationID:
			authorized[key] |= installationGrant
		}
	}

	seen := make(map[string]bool, len(contents))
	out := make([]BoardRetrievedSource, 0, len(contents))
	for _, content := range contents {
		if content.Source.Validate() != nil {
			continue
		}
		key := boardSourceKey(content.Source)
		if authorized[key] != requesterGrant|installationGrant || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, BoardRetrievedSource{Source: content.Source, Title: content.Title, Excerpt: content.Excerpt, TextUntrusted: true})
	}
	return out, nil
}

func boardSourceKey(source BoardSourceRef) string {
	encoded, _ := json.Marshal(source)
	return string(encoded)
}

func (r BoardRetrievalRequest) Validate() error {
	if !validProposalID(r.TenantID) || !validProposalID(r.RequesterID) || !validProposalID(r.InstallationID) {
		return fmt.Errorf("%w: principal scope", ErrInvalidBoardRetrieval)
	}
	return nil
}

func (g BoardRetrievalGrant) String() string {
	return strings.Join([]string{string(g.SubjectKind), g.SubjectID, boardSourceKey(g.Source)}, ":")
}
