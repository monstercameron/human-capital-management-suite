package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaAudienceUnavailable = errors.New("application: persona audience unavailable")

// PersonaAudienceMember is a server-derived current member and its audience
// facts. Request payloads must never be used to construct one.
type PersonaAudienceMember struct {
	SubjectID         string
	Roles             []string
	Populations       []string
	OrganizationScope string
}

// PersonaAudienceConversation is the current membership projection for one
// conversation. The source must obtain it from the authoritative chat store.
type PersonaAudienceConversation struct {
	TenantID       string
	ConversationID string
	Members        []PersonaAudienceMember
}

// PersonaAudienceInstallation is a server-derived persona placement and its
// immutable audience facts.
type PersonaAudienceInstallation struct {
	Tuple               agentpersonastore.AvailableInstallation
	Active              bool
	CurrentVersion      bool
	AudienceRoles       []string
	AudiencePopulations []string
	AudienceScopes      []string
}

// PersonaAudienceSource supplies current membership and installation facts.
// Implementations must read both projections from authoritative stores.
type PersonaAudienceSource interface {
	ListCurrentPersonaAudience(context.Context, string, string) ([]PersonaAudienceConversation, error)
	ListCurrentPersonaInstallations(context.Context, string, string) ([]PersonaAudienceInstallation, error)
}

// CurrentPersonaAudience resolves exact active installation tuples across
// conversations where the trusted principal is currently a member.
type CurrentPersonaAudience struct {
	Source PersonaAudienceSource
}

var _ CurrentPersonaAudienceResolver = (*CurrentPersonaAudience)(nil)

// ResolveAvailablePersonaInstallations implements CurrentPersonaAudienceResolver.
// It requires a verified principal in context and returns no partial result on
// malformed, cross-tenant, stale, duplicate, or unavailable source data.
func (r *CurrentPersonaAudience) ResolveAvailablePersonaInstallations(ctx context.Context, principal *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	if r == nil || r.Source == nil || ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, errPersonaAudienceUnavailable
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errPersonaAudienceUnavailable
	}
	tenant := string(verified.Tenant())
	conversations, err := r.Source.ListCurrentPersonaAudience(ctx, tenant, verified.Subject())
	if err != nil {
		return nil, fmt.Errorf("list current persona audience: %w", err)
	}
	seenConversations := make(map[string]struct{}, len(conversations))
	seen := make(map[agentpersonastore.AvailableInstallation]struct{})
	result := make([]agentpersonastore.AvailableInstallation, 0)
	for _, conversation := range conversations {
		if conversation.TenantID != tenant || strings.TrimSpace(conversation.ConversationID) == "" {
			return nil, errPersonaAudienceUnavailable
		}
		if _, duplicate := seenConversations[conversation.ConversationID]; duplicate {
			return nil, errPersonaAudienceUnavailable
		}
		seenConversations[conversation.ConversationID] = struct{}{}
		if !memberOf(conversation.Members, verified.Subject()) {
			return nil, errPersonaAudienceUnavailable
		}
		installations, err := r.Source.ListCurrentPersonaInstallations(ctx, tenant, conversation.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("list persona installations for %q: %w", conversation.ConversationID, err)
		}
		member := memberFor(conversation.Members, verified.Subject())
		for _, installation := range installations {
			if !validAudienceInstallation(installation, tenant, conversation.ConversationID) || !audienceMatches(installation, member) {
				continue
			}
			if _, duplicate := seen[installation.Tuple]; duplicate {
				return nil, errPersonaAudienceUnavailable
			}
			seen[installation.Tuple] = struct{}{}
			result = append(result, installation.Tuple)
		}
	}
	slices.SortFunc(result, func(left, right agentpersonastore.AvailableInstallation) int {
		return compareAudienceInstallation(left, right)
	})
	return result, nil
}

func memberOf(members []PersonaAudienceMember, subject string) bool {
	for _, member := range members {
		if member.SubjectID == subject {
			return true
		}
	}
	return false
}

func memberFor(members []PersonaAudienceMember, subject string) PersonaAudienceMember {
	for _, member := range members {
		if member.SubjectID == subject {
			return member
		}
	}
	return PersonaAudienceMember{}
}

func validAudienceInstallation(item PersonaAudienceInstallation, tenant, conversation string) bool {
	tuple := item.Tuple
	return item.Active && item.CurrentVersion && tuple.ConversationID == conversation && tuple.PersonaID != "" && tuple.PersonaVersion > 0 && tuple.InstallationID != "" && tuple.ConversationID != "" && len(append(append([]string{}, item.AudienceRoles...), append(item.AudiencePopulations, item.AudienceScopes...)...)) > 0 && tenant != ""
}

func audienceMatches(item PersonaAudienceInstallation, member PersonaAudienceMember) bool {
	return anyAudienceMatch(item.AudienceRoles, member.Roles) && anyAudienceMatch(item.AudiencePopulations, member.Populations) && anyAudienceMatch(item.AudienceScopes, []string{member.OrganizationScope})
}

func anyAudienceMatch(allowed, actual []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, want := range allowed {
		for _, got := range actual {
			if strings.TrimSpace(want) != "" && want == got {
				return true
			}
		}
	}
	return false
}

func compareAudienceInstallation(left, right agentpersonastore.AvailableInstallation) int {
	for _, pair := range [][2]string{{left.ConversationID, right.ConversationID}, {left.PersonaID, right.PersonaID}, {left.InstallationID, right.InstallationID}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if left.PersonaVersion < right.PersonaVersion {
		return -1
	}
	if left.PersonaVersion > right.PersonaVersion {
		return 1
	}
	return 0
}
