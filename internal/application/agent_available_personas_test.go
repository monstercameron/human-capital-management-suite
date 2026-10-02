package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type availableAudienceFake struct {
	err   error
	calls int
}

func (f *availableAudienceFake) ResolveAvailablePersonaInstallations(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	f.calls++
	return nil, f.err
}

type availableBackendFake struct {
	items []agentpersonastore.PersonaVersion
	ids   []agentpersonastore.AvailableInstallation
}

func (f *availableBackendFake) ListAvailable(_ context.Context, _ values.TenantId, ids []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error) {
	f.ids = append([]agentpersonastore.AvailableInstallation(nil), ids...)
	return f.items, nil
}

func availablePersonaPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecodeAvailableProfileRejectsIdentityMismatch(t *testing.T) {
	profile := agentpersona.PersonaProfile{PersonaID: "persona-a", Version: 1}
	b, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decodeAvailableProfile(agentpersonastore.PersonaVersion{PersonaID: "persona-b", Version: 1, Profile: b})
	if !errors.Is(err, errAgentAvailablePersonas) {
		t.Fatalf("error = %v, want unavailable", err)
	}
}

func TestTenantAvailablePersonaReaderRequiresVerifiedContext(t *testing.T) {
	principal := availablePersonaPrincipal(t)
	audience := &availableAudienceFake{}
	backend := &availableBackendFake{}
	reader := &TenantAvailablePersonaReader{Backend: backend, Audience: audience}
	if _, err := reader.ListAvailable(context.Background(), principal); !errors.Is(err, errAgentAvailablePersonas) {
		t.Fatalf("detached principal error = %v, want unavailable", err)
	}
	if audience.calls != 0 || len(backend.ids) != 0 {
		t.Fatalf("unverified request reached resolver/backend: calls=%d ids=%v", audience.calls, backend.ids)
	}
}

func TestTenantAvailablePersonaReaderFailsClosedOnAudienceError(t *testing.T) {
	principal := availablePersonaPrincipal(t)
	audience := &availableAudienceFake{err: errors.New("membership unavailable")}
	backend := &availableBackendFake{}
	reader := &TenantAvailablePersonaReader{Backend: backend, Audience: audience}
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := reader.ListAvailable(ctx, principal); err == nil {
		t.Fatal("audience failure was rendered as an available catalog")
	}
	if len(backend.ids) != 0 {
		t.Fatalf("backend was called after audience failure with ids=%v", backend.ids)
	}
}

func availablePersonaCandidate(t *testing.T, persona agentpersona.PersonaVersion) agentpersonastore.PersonaVersion {
	t.Helper()
	profile, err := json.Marshal(persona.Profile)
	if err != nil {
		t.Fatal(err)
	}
	return agentpersonastore.PersonaVersion{PersonaID: persona.Profile.PersonaID, Version: int64(persona.Profile.Version), Profile: profile, ContentDigest: persona.Digest}
}

func TestTenantAvailablePersonaReaderFiltersByCurrentPinnedSkillIntersection(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	principal := availablePersonaPrincipal(t)
	audience := &availableAudienceFake{}
	backend := &availableBackendFake{items: []agentpersonastore.PersonaVersion{availablePersonaCandidate(t, persona)}}
	reader := &TenantAvailablePersonaReader{Backend: backend, Audience: audience, Skills: &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		personaChatReplyPurpose: {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: pin.Digest, Status: agentskills.StatusActive}},
	}}}
	got, err := reader.ListAvailable(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil || len(got) != 1 || got[0].Profile.PersonaID != persona.Profile.PersonaID {
		t.Fatalf("ListAvailable = %v, %v; want one invocable persona", got, err)
	}
}

func TestTenantAvailablePersonaReaderOmitsRevokedOrUndiscoverablePinnedSkill(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	principal := availablePersonaPrincipal(t)
	for name, discovered := range map[string][]agentskills.SkillRecord{
		"revoked":       {},
		"wrong version": {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: 1}, Digest: pin.Digest, Status: agentskills.StatusActive}},
		"wrong digest":  {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: "revoked-digest", Status: agentskills.StatusActive}},
	} {
		t.Run(name, func(t *testing.T) {
			reader := &TenantAvailablePersonaReader{
				Backend:  &availableBackendFake{items: []agentpersonastore.PersonaVersion{availablePersonaCandidate(t, persona)}},
				Audience: &availableAudienceFake{},
				Skills:   &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{personaChatReplyPurpose: discovered}},
			}
			got, err := reader.ListAvailable(trust.WithPrincipal(context.Background(), principal), principal)
			if err != nil || len(got) != 0 {
				t.Fatalf("ListAvailable = %v, %v; want omitted persona", got, err)
			}
		})
	}
}
