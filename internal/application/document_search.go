// Application-boundary wrapper for HUB-030's typed document search
// filters (internal/data/documenthubstore.SearchLexicalFiltered). Callers
// outside internal/data never import the store package's SQL directly;
// this file only translates between the transport-neutral filter/result
// shapes used here and the store's own types. Every filter, and every
// authorization check, run inside the store query itself, before any
// title, snippet or count is built, so nothing here widens or narrows
// that guarantee.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// DocumentSearchFilters mirrors documenthubstore.SearchFilters at the
// application boundary. An empty string or zero time skips that filter.
type DocumentSearchFilters struct {
	TeamID, ChannelID, Status, OwnerID, Locale string
	DateFrom, DateTo                           time.Time
}

// storeFilters converts to the store's filter shape.
func (f DocumentSearchFilters) storeFilters() documenthubstore.SearchFilters {
	return documenthubstore.SearchFilters{
		TeamID: f.TeamID, ChannelID: f.ChannelID, Status: f.Status,
		OwnerID: f.OwnerID, Locale: f.Locale, DateFrom: f.DateFrom, DateTo: f.DateTo,
	}
}

// DocumentSearchHit is one authorized, filtered, currently deployed
// search result with its citation: exact version, status and deployed
// time.
type DocumentSearchHit struct {
	DocumentID, VersionID, Title string
	Status, Locale, OwnerID      string
	DeployedAt                   time.Time
	Score, MatchedTerms          int
}

// DocumentSearchResult is one page of hits plus the authorized, filtered
// total.
type DocumentSearchResult struct {
	Hits  []DocumentSearchHit
	Total int
}

func transportSearchResult(res documenthubstore.FilteredSearchResult) DocumentSearchResult {
	hits := make([]DocumentSearchHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, DocumentSearchHit{
			DocumentID: h.DocumentID, VersionID: h.VersionID, Title: h.Title,
			Status: h.Status, Locale: h.Locale, OwnerID: h.OwnerID, DeployedAt: h.DeployedAt,
			Score: h.Score, MatchedTerms: h.MatchedTerms,
		})
	}
	return DocumentSearchResult{Hits: hits, Total: res.Total}
}

// documentFilteredSearcher is the store port both the human and agent
// search services need: HUB-030's typed, server-authorized filtered
// search. documenthubstore.Store satisfies it.
type documentFilteredSearcher interface {
	SearchLexicalFiltered(ctx context.Context, tenantID, query, readerKind, readerID string, filters documenthubstore.SearchFilters) (documenthubstore.FilteredSearchResult, error)
}

// documentSearchService exposes HUB-030's typed filters at the
// application boundary for a human reader acting as themselves.
type documentSearchService struct {
	store documentFilteredSearcher
}

func newDocumentSearchService(store documentFilteredSearcher) documentSearchService {
	return documentSearchService{store: store}
}

// SearchDocuments runs an authorized, filtered lexical search for actorID
// acting as themselves. It is a thin pass-through to
// documenthubstore.SearchLexicalFiltered, which applies every filter and
// every grant check in one statement before any snippet, count or
// citation is built.
func (s documentSearchService) SearchDocuments(ctx context.Context, tenantID, actorID, query string, filters DocumentSearchFilters) (DocumentSearchResult, error) {
	if strings.TrimSpace(actorID) == "" {
		return DocumentSearchResult{}, documenthubstore.ErrDenied
	}
	res, err := s.store.SearchLexicalFiltered(ctx, tenantID, query, "person", actorID, filters.storeFilters())
	if err != nil {
		return DocumentSearchResult{}, err
	}
	return transportSearchResult(res), nil
}

// SearchDocuments adapts the transport contract to the authorized human
// search implementation. All access filtering remains in the store query.
func (s documentService) SearchDocuments(ctx context.Context, tenant, actor, query string, filters transportdocument.SearchFilters) (transportdocument.SearchResult, error) {
	result, err := newDocumentSearchService(s.store).SearchDocuments(ctx, tenant, actor, query, DocumentSearchFilters{
		TeamID: filters.TeamID, ChannelID: filters.ChannelID, Status: filters.Status,
		OwnerID: filters.OwnerID, Locale: filters.Locale, DateFrom: filters.DateFrom, DateTo: filters.DateTo,
	})
	if err != nil {
		return transportdocument.SearchResult{}, err
	}
	return transportSearchResultForTransport(result), nil
}

// AgentSearchDocuments adapts the transport contract while binding the
// agent authority to the invocation's tenant and conversation installation.
func (s documentService) AgentSearchDocuments(ctx context.Context, tenant, conversationID, agentID, requesterID, query string, filters transportdocument.SearchFilters) (transportdocument.SearchResult, error) {
	now := s.agentNow
	if now == nil {
		now = time.Now
	}
	authority := newChatInstallationSearchAuthority(s.agentApps, conversationID, now)
	result, err := newDocumentAgentService(s.store, authority).AgentSearchDocuments(ctx, tenant, agentID, requesterID, query, DocumentSearchFilters{
		TeamID: filters.TeamID, ChannelID: filters.ChannelID, Status: filters.Status,
		OwnerID: filters.OwnerID, Locale: filters.Locale, DateFrom: filters.DateFrom, DateTo: filters.DateTo,
	})
	if err != nil {
		return transportdocument.SearchResult{}, err
	}
	return transportSearchResultForTransport(result), nil
}

func transportSearchResultForTransport(result DocumentSearchResult) transportdocument.SearchResult {
	hits := make([]transportdocument.SearchHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, transportdocument.SearchHit{
			DocumentID: hit.DocumentID, VersionID: hit.VersionID, Title: hit.Title,
			Status: hit.Status, Locale: hit.Locale, OwnerID: hit.OwnerID,
			DeployedAt: hit.DeployedAt, Score: hit.Score, MatchedTerms: hit.MatchedTerms,
		})
	}
	return transportdocument.SearchResult{Hits: hits, Total: result.Total}
}
