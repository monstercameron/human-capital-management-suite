package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var (
	errPersonaReferenceInvalid    = errors.New("application: invalid persona reference")
	errPersonaReferenceNotPersona = errors.New("application: chat agent reference is not a persona")
	errPersonaReferenceInactive   = errors.New("application: persona installation is not current and active")
	errPersonaReferenceDuplicate  = errors.New("application: duplicate persona reference")
)

type personaReferenceInstallationState string
type personaReferenceLifecycle string

const (
	personaReferenceActive    personaReferenceInstallationState = "ACTIVE"
	personaReferencePublished personaReferenceLifecycle         = "PUBLISHED"
)

// personaReferenceFacts is the typed projection required from the server-side
// persona registry and conversation installation store. Display and post body
// text are deliberately absent.
type personaReferenceFacts struct {
	ReferenceID         string
	TenantID            string
	ConversationID      string
	PersonaID           string
	InstallationID      string
	PersonaVersion      uint64
	CurrentVersion      uint64
	InstallationState   personaReferenceInstallationState
	PersonaLifecycle    personaReferenceLifecycle
	InstallationExpires time.Time
}

// personaReferenceLookup resolves a chat agent identity only when it belongs
// to a persona installation in the exact tenant and conversation. Generic
// chat app identities must return errPersonaReferenceNotPersona.
type personaReferenceLookup interface {
	LookupPersonaReference(context.Context, string, string, string) (personaReferenceFacts, error)
}

type personaChatReferenceResolver struct {
	lookup personaReferenceLookup
	now    func() time.Time
}

func newPersonaChatReferenceResolver(lookup personaReferenceLookup, now func() time.Time) (*personaChatReferenceResolver, error) {
	if lookup == nil {
		return nil, fmt.Errorf("%w: lookup is required", errPersonaReferenceInvalid)
	}
	if now == nil {
		now = time.Now
	}
	return &personaChatReferenceResolver{lookup: lookup, now: now}, nil
}

// ResolvePersonaMentions converts stored AgentMention references into canonical
// persona mentions after resolving their current installation. Non-agent
// references are ignored. Any malformed, foreign, generic-app, duplicate, or
// inactive AgentMention rejects the resolution instead of starting partial work.
func (r *personaChatReferenceResolver) ResolvePersonaMentions(ctx context.Context, tenant, conversation string, references []chatcore.Reference) ([]agentinvoke.Mention, error) {
	if r == nil || r.lookup == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" {
		return nil, fmt.Errorf("%w: tenant, conversation and lookup are required", errPersonaReferenceInvalid)
	}
	seenReferences := make(map[string]struct{})
	seenPersonas := make(map[string]struct{})
	mentions := make([]agentinvoke.Mention, 0, len(references))
	for _, reference := range references {
		if reference.Kind != chatcore.AgentMention {
			continue
		}
		if reference.TenantID != tenant || !validPersonaReferenceID(reference.ID) {
			return nil, fmt.Errorf("%w: tenant or id does not match the committed reference", errPersonaReferenceInvalid)
		}
		if _, duplicate := seenReferences[reference.ID]; duplicate {
			return nil, fmt.Errorf("%w: reference %q", errPersonaReferenceDuplicate, reference.ID)
		}
		seenReferences[reference.ID] = struct{}{}
		facts, err := r.lookup.LookupPersonaReference(ctx, tenant, conversation, reference.ID)
		if err != nil {
			if errors.Is(err, errPersonaReferenceNotPersona) {
				return nil, fmt.Errorf("%w: %q", errPersonaReferenceNotPersona, reference.ID)
			}
			return nil, fmt.Errorf("application: resolve persona reference: %w", err)
		}
		if !currentPersonaReference(facts, tenant, conversation, reference.ID, r.now().UTC()) {
			return nil, fmt.Errorf("%w: %q", errPersonaReferenceInactive, reference.ID)
		}
		if _, duplicate := seenPersonas[facts.PersonaID]; duplicate {
			return nil, fmt.Errorf("%w: persona %q", errPersonaReferenceDuplicate, facts.PersonaID)
		}
		seenPersonas[facts.PersonaID] = struct{}{}
		mentions = append(mentions, agentinvoke.Mention{Kind: agentinvoke.PersonaMention, PersonaID: facts.PersonaID, Canonical: true})
	}
	return mentions, nil
}

func currentPersonaReference(facts personaReferenceFacts, tenant, conversation, referenceID string, at time.Time) bool {
	return facts.ReferenceID == referenceID && facts.TenantID == tenant && facts.ConversationID == conversation &&
		validPersonaReferenceID(facts.PersonaID) && validPersonaReferenceID(facts.InstallationID) &&
		facts.PersonaVersion > 0 && facts.PersonaVersion == facts.CurrentVersion &&
		facts.InstallationState == personaReferenceActive && facts.PersonaLifecycle == personaReferencePublished &&
		(facts.InstallationExpires.IsZero() || facts.InstallationExpires.After(at))
}

func validPersonaReferenceID(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		alphaNum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if i == 0 {
			if !alphaNum {
				return false
			}
			continue
		}
		if !alphaNum && c != '.' && c != '_' && c != ':' && c != '-' {
			return false
		}
	}
	return true
}
