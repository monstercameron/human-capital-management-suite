package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// personaChatIdentityCatalog enumerates canonical chat identities in one
// tenant. Implementations must read the permanent registered namespace; a
// persona handle or display name is never an identity source.
type personaChatIdentityCatalog interface {
	ListPersonaChatIdentities(context.Context, string) ([]personaRegisteredChatIdentity, error)
}

// productionPersonaChatReferenceSource composes current persona availability with the
// permanent chat identity namespace. Availability is supplied by the reader,
// which verifies the authenticated principal, audience, installation state,
// published version, and current pinned skill discovery.
type productionPersonaChatReferenceSource struct {
	personas   AvailablePersonaReader
	identities personaChatIdentityCatalog
	lookup     personaReferenceLookup
	now        func() time.Time
}

// newPersonaChatReferenceSource constructs the fail-closed candidate source.
// All three authorities are required: omitting identity enumeration would
// force callers to derive a chat reference from mutable presentation data.
func newPersonaChatReferenceSource(personas AvailablePersonaReader, identities personaChatIdentityCatalog, lookup personaReferenceLookup, now func() time.Time) (*productionPersonaChatReferenceSource, error) {
	if personas == nil || identities == nil || lookup == nil {
		return nil, fmt.Errorf("%w: persona availability, identity catalog, and exact lookup are required", errPersonaReferenceInvalid)
	}
	if now == nil {
		now = time.Now
	}
	return &productionPersonaChatReferenceSource{personas: personas, identities: identities, lookup: lookup, now: now}, nil
}

var _ personaChatReferenceSource = (*productionPersonaChatReferenceSource)(nil)

// ListPersonaReferenceCandidates returns only current, audience- and
// skill-authorized personas installed in conversation. The candidate ID is
// the canonical registered Agent.ID and its display name is the immutable
// published profile snapshot.
func (s *productionPersonaChatReferenceSource) ListPersonaReferenceCandidates(ctx context.Context, principal chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	if s == nil || s.personas == nil || s.identities == nil || s.lookup == nil || ctx == nil || strings.TrimSpace(tenant) != tenant || tenant == "" || strings.TrimSpace(conversation) != conversation || conversation == "" || principal.TenantID != tenant || strings.TrimSpace(principal.SubjectID) == "" {
		return nil, fmt.Errorf("%w: candidate source requires a tenant-bound conversation and principal", errPersonaReferenceInvalid)
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant().String() != tenant || verified.Subject() != principal.SubjectID {
		return nil, fmt.Errorf("%w: verified human principal is required", errPersonaReferenceInvalid)
	}

	identities, err := s.identities.ListPersonaChatIdentities(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("list canonical persona chat identities: %w", err)
	}
	byPersona := make(map[string]personaRegisteredChatIdentity, len(identities))
	for _, identity := range identities {
		if identity.TenantID != tenant || !validPersonaReferenceID(identity.ReferenceID) || !validPersonaReferenceID(identity.PersonaID) || !identity.Active {
			continue
		}
		if prior, exists := byPersona[identity.PersonaID]; exists && prior.ReferenceID != identity.ReferenceID {
			return nil, fmt.Errorf("%w: multiple canonical chat identities for persona %q", errPersonaReferenceInvalid, identity.PersonaID)
		}
		byPersona[identity.PersonaID] = identity
	}

	profiles, err := s.personas.ListAvailable(ctx, verified)
	if err != nil {
		return nil, fmt.Errorf("list available persona references: %w", err)
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]chat.ReferenceCandidate, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		candidate, include := s.candidateForProfile(ctx, tenant, conversation, needle, profile, byPersona, seen)
		if include {
			seen[candidate.ID] = struct{}{}
			out = append(out, chat.ReferenceCandidate{Reference: candidate, Eligible: true})
		}
	}
	return out, nil
}

func (s *productionPersonaChatReferenceSource) candidateForProfile(ctx context.Context, tenant, conversation, query string, version agentpersona.PersonaVersion, byPersona map[string]personaRegisteredChatIdentity, seen map[string]struct{}) (chat.Reference, bool) {
	profile := version.Profile
	if version.Verify() != nil || profile.PersonaID == "" || profile.Version == 0 || strings.TrimSpace(profile.DisplayName) == "" {
		return chat.Reference{}, false
	}
	identity, ok := byPersona[profile.PersonaID]
	if !ok || identity.TenantID != tenant || !identity.Active || identity.PersonaID != profile.PersonaID || identity.ReferenceID == "" {
		return chat.Reference{}, false
	}
	if _, duplicate := seen[identity.ReferenceID]; duplicate {
		return chat.Reference{}, false
	}
	if query != "" && !strings.Contains(strings.ToLower(profile.DisplayName), query) && !strings.Contains(strings.ToLower(identity.ReferenceID), query) {
		return chat.Reference{}, false
	}
	facts, err := s.lookup.LookupPersonaReference(ctx, tenant, conversation, identity.ReferenceID)
	if err != nil || !currentPersonaReference(facts, tenant, conversation, identity.ReferenceID, s.now().UTC()) || facts.PersonaID != profile.PersonaID || facts.PersonaVersion != uint64(profile.Version) {
		return chat.Reference{}, false
	}
	return chat.Reference{Kind: chat.AgentMention, TenantID: tenant, ID: identity.ReferenceID, Display: profile.DisplayName, ConversationID: conversation}, true
}

// LookupPersonaReference rechecks the exact canonical identity and current
// installation used by a selected candidate.
func (s *productionPersonaChatReferenceSource) LookupPersonaReference(ctx context.Context, tenant, conversation, referenceID string) (personaReferenceFacts, error) {
	if s == nil || s.lookup == nil {
		return personaReferenceFacts{}, fmt.Errorf("%w: exact persona lookup is required", errPersonaReferenceInvalid)
	}
	return s.lookup.LookupPersonaReference(ctx, tenant, conversation, referenceID)
}
