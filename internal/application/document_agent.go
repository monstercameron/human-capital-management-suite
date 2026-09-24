// Agent document search for HUB-031: an installed chat agent may retrieve
// only documents that are both within its own installation's granted
// search scope AND currently readable by the human it is acting for.
// Neither grant alone is sufficient; this file always intersects both,
// reusing documenthubstore.SearchLexicalFiltered (HUB-030) so the
// requester-grant half of the intersection is exactly the store's own
// authorization, never a separate, weaker check. An agent's installation
// scope is resolved at query time through AgentInstallationAuthority, so
// a revoked installation loses access on the next call.
//
// The chat installation an agent acts under supplies its scopes through
// chatInstallationSearchAuthority (document_agent_chat.go), which resolves the
// same tenant:conversation:app installation chat admits the agent with. Scope
// strings are manifest-declared, so an app opts into document search by
// declaring and being granted "documents.search" (or a team/channel-scoped
// form of it).
package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// DocumentSearchScope is the blanket installation scope an agent must
// hold to search documents without a team/channel restriction.
const DocumentSearchScope = "documents.search"

// documentSearchTeamScopePrefix and documentSearchChannelScopePrefix bound
// an agent's installation to one named team or channel, e.g.
// "documents.search:team:team-people-ops". An agent holding only scoped
// grants (no blanket DocumentSearchScope) may search only when the
// caller's filter names one of its granted teams or channels.
const (
	documentSearchTeamScopePrefix    = DocumentSearchScope + ":team:"
	documentSearchChannelScopePrefix = DocumentSearchScope + ":channel:"
)

var (
	// ErrAgentNotInstalled is returned when the agent holds no resolvable,
	// active installation at all.
	ErrAgentNotInstalled = errors.New("document agent search: agent has no active installation")
	// ErrAgentSearchEscalation is returned when the agent's installation
	// does not cover the requested search: no document-search scope at
	// all, or a team/channel filter outside its granted scopes, or no
	// filter at all when only a scoped (non-blanket) grant is held. It is
	// always a distinct, typed refusal, never a silently empty result.
	ErrAgentSearchEscalation = errors.New("document agent search: requested scope exceeds installation grant")
)

// AgentInstallationAuthority is the minimal port document agent search
// needs from the chat installed-agent system: the scopes an installed
// agent currently holds for one tenant, resolved fresh on every call so a
// revoked installation loses access on its next query rather than a
// cached one. chatInstallationSearchAuthority is the chat-backed adapter.
type AgentInstallationAuthority interface {
	// InstalledScopes returns the current granted scopes for one agent's
	// active installation. A non-nil error means the agent has no active
	// installation (never installed, revoked, or expired); callers must
	// treat that identically to "no scope", never as "unrestricted".
	InstalledScopes(ctx context.Context, tenantID, agentID string) ([]string, error)
}

// documentAgentService lets an installed chat agent search only the
// documents its installation was scoped for AND that the requesting user
// may themselves currently read.
type documentAgentService struct {
	store     documentFilteredSearcher
	authority AgentInstallationAuthority
}

func newDocumentAgentService(store documentFilteredSearcher, authority AgentInstallationAuthority) documentAgentService {
	return documentAgentService{store: store, authority: authority}
}

// scopeBoundValue reports the value bound by a "prefix<value>" scope
// string, such as "documents.search:team:" -> "team-x".
func scopeBoundValue(scope, prefix string) (string, bool) {
	if !strings.HasPrefix(scope, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(scope, prefix)
	if value == "" {
		return "", false
	}
	return value, true
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// authorizeAgentScope checks the agent's installation scopes against the
// requested filters before any search runs. A blanket DocumentSearchScope
// allows any filter combination (still bounded separately by the
// requester's own read grants once the search runs). Without a blanket
// scope, the agent may search only within its scoped team/channel
// grants: the caller must name exactly one granted team or channel, and
// naming none, or naming one outside the grant, refuses as an escalation
// attempt rather than silently narrowing or emptying the result.
func authorizeAgentScope(scopes []string, filters documenthubstore.SearchFilters) error {
	var teams, channels []string
	for _, sc := range scopes {
		if sc == DocumentSearchScope {
			return nil
		}
		if v, ok := scopeBoundValue(sc, documentSearchTeamScopePrefix); ok {
			teams = append(teams, v)
		} else if v, ok := scopeBoundValue(sc, documentSearchChannelScopePrefix); ok {
			channels = append(channels, v)
		}
	}
	if len(teams) == 0 && len(channels) == 0 {
		return ErrAgentSearchEscalation
	}
	if filters.TeamID != "" && containsString(teams, filters.TeamID) {
		return nil
	}
	if filters.ChannelID != "" && containsString(channels, filters.ChannelID) {
		return nil
	}
	return ErrAgentSearchEscalation
}

// AgentSearchDocuments runs a query on behalf of an installed agent
// acting for requesterID. It refuses with ErrAgentNotInstalled when the
// agent has no resolvable active installation, and with
// ErrAgentSearchEscalation when the requested filters exceed the
// installation's granted scope. On success it runs the same authorized,
// filtered search as a human search
// (documenthubstore.SearchLexicalFiltered) under the REQUESTER's own
// identity ("person", requesterID), so the intersection with the
// requester's grants is exactly HUB-030's existing authorization, never a
// broader agent-only view. Every returned hit is a currently deployed
// version (SearchLexicalFiltered only matches the live default-scope
// pointer, never a draft or superseded version) and carries its exact
// version ID, status and deployed time as citation.
func (s documentAgentService) AgentSearchDocuments(ctx context.Context, tenantID, agentID, requesterID, query string, filters DocumentSearchFilters) (DocumentSearchResult, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(requesterID) == "" {
		return DocumentSearchResult{}, documenthubstore.ErrDenied
	}
	if s.authority == nil {
		return DocumentSearchResult{}, ErrAgentNotInstalled
	}
	scopes, err := s.authority.InstalledScopes(ctx, tenantID, agentID)
	if err != nil {
		return DocumentSearchResult{}, ErrAgentNotInstalled
	}
	storeFilters := filters.storeFilters()
	if err := authorizeAgentScope(scopes, storeFilters); err != nil {
		return DocumentSearchResult{}, err
	}
	res, err := s.store.SearchLexicalFiltered(ctx, tenantID, query, "person", requesterID, storeFilters)
	if err != nil {
		return DocumentSearchResult{}, err
	}
	return transportSearchResult(res), nil
}
