package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaCandidateProfiles struct {
	profiles []agentpersona.PersonaVersion
	err      error
}

func (p personaCandidateProfiles) ListAvailable(context.Context, *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	return p.profiles, p.err
}

type personaCandidateIdentities struct {
	identities []personaRegisteredChatIdentity
	err        error
}

func (c personaCandidateIdentities) ListPersonaChatIdentities(context.Context, string) ([]personaRegisteredChatIdentity, error) {
	return c.identities, c.err
}

type personaCandidateLookup struct {
	facts personaReferenceFacts
	err   error
	calls []string
}

func (l *personaCandidateLookup) LookupPersonaReference(_ context.Context, tenant, conversation, reference string) (personaReferenceFacts, error) {
	l.calls = append(l.calls, tenant+"/"+conversation+"/"+reference)
	return l.facts, l.err
}

func candidateProfile(t *testing.T) agentpersona.PersonaVersion {
	t.Helper()
	return agentUserCatalogPersona(t, "coach", "People Coach", "agent.self_service", agentpersonaSkillPin())
}

func agentpersonaSkillPin() (pin agentskills.SkillPin) {
	return agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
}

func candidatePrincipal(t *testing.T) (*trust.Principal, chat.Principal) {
	t.Helper()
	p := availablePersonaPrincipal(t)
	return p, chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
}

func candidateSource(t *testing.T, profiles []agentpersona.PersonaVersion, identities []personaRegisteredChatIdentity, facts personaReferenceFacts) (*productionPersonaChatReferenceSource, *personaCandidateLookup) {
	t.Helper()
	lookup := &personaCandidateLookup{facts: facts}
	source, err := newPersonaChatReferenceSource(personaCandidateProfiles{profiles: profiles}, personaCandidateIdentities{identities: identities}, lookup, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	return source, lookup
}

func TestTodo_AGENTP_019_PersonaReferenceCandidatesUseCanonicalIdentityAndCurrentFacts(t *testing.T) {
	profile := candidateProfile(t)
	p, principal := candidatePrincipal(t)
	identity := personaRegisteredChatIdentity{TenantID: "tenant-a", ReferenceID: "agent:coach", PersonaID: profile.Profile.PersonaID, Active: true}
	facts := currentPersonaReferenceFacts(identity.ReferenceID)
	facts.PersonaID = profile.Profile.PersonaID
	facts.ConversationID = "room-a"
	facts.PersonaVersion = uint64(profile.Profile.Version)
	facts.CurrentVersion = facts.PersonaVersion
	source, lookup := candidateSource(t, []agentpersona.PersonaVersion{profile}, []personaRegisteredChatIdentity{identity}, facts)

	got, err := source.ListPersonaReferenceCandidates(trust.WithPrincipal(context.Background(), p), principal, "tenant-a", "room-a", "coach")
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(got) != 1 || got[0].Kind != chat.AgentMention || got[0].ID != "agent:coach" || got[0].Display != "People Coach" || got[0].ConversationID != "room-a" || !got[0].Eligible {
		t.Fatalf("candidates = %+v", got)
	}
	if len(lookup.calls) != 1 || lookup.calls[0] != "tenant-a/room-a/agent:coach" {
		t.Fatalf("exact lookup calls = %v", lookup.calls)
	}
}

func TestTodo_AGENTP_019_PersonaReferenceCandidatesFailClosedForUntrustedOrStaleSources(t *testing.T) {
	profile := candidateProfile(t)
	p, principal := candidatePrincipal(t)
	identity := personaRegisteredChatIdentity{TenantID: "tenant-a", ReferenceID: "agent:coach", PersonaID: profile.Profile.PersonaID, Active: true}
	facts := currentPersonaReferenceFacts(identity.ReferenceID)
	facts.PersonaID = profile.Profile.PersonaID
	facts.ConversationID = "room-a"
	facts.PersonaVersion = uint64(profile.Profile.Version)
	facts.CurrentVersion = facts.PersonaVersion
	source, _ := candidateSource(t, []agentpersona.PersonaVersion{profile}, []personaRegisteredChatIdentity{identity}, facts)

	if _, err := source.ListPersonaReferenceCandidates(context.Background(), principal, "tenant-a", "room-a", ""); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("detached context error = %v", err)
	}
	for name, mutate := range map[string]func(*personaReferenceFacts){
		"stale installation": func(f *personaReferenceFacts) { f.PersonaVersion = 1; f.CurrentVersion = 2 },
		"wrong persona":      func(f *personaReferenceFacts) { f.PersonaID = "persona.other" },
		"inactive identity":  func(f *personaReferenceFacts) { f.InstallationState = personaReferenceInstallationState("SUSPENDED") },
	} {
		t.Run(name, func(t *testing.T) {
			bad := facts
			mutate(&bad)
			candidate, _ := candidateSource(t, []agentpersona.PersonaVersion{profile}, []personaRegisteredChatIdentity{identity}, bad)
			got, err := candidate.ListPersonaReferenceCandidates(trust.WithPrincipal(context.Background(), p), principal, "tenant-a", "room-a", "")
			if err != nil {
				t.Fatalf("list candidates: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("stale source returned candidates: %+v", got)
			}
		})
	}
}

func TestTodo_AGENTP_019_PersonaReferenceCandidatesRequireAllTrustedContracts(t *testing.T) {
	if _, err := newPersonaChatReferenceSource(nil, personaCandidateIdentities{}, &personaCandidateLookup{}, nil); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("missing availability error = %v", err)
	}
	if _, err := newPersonaChatReferenceSource(personaCandidateProfiles{}, nil, &personaCandidateLookup{}, nil); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("missing identity catalog error = %v", err)
	}
	if _, err := newPersonaChatReferenceSource(personaCandidateProfiles{}, personaCandidateIdentities{}, nil, nil); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("missing exact lookup error = %v", err)
	}
}
