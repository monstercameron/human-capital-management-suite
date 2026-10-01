package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaAdminCompositionStore struct {
	entries     []agentpersonastore.CatalogEntry
	err         error
	called      values.TenantId
	evidence    agentpersonastore.PublicationEvidence
	evidenceErr error
	review      *agentpersonastore.VerifiedReview
}

func (s *personaAdminCompositionStore) ForTenant(_ context.Context, tenant values.TenantId) (PersonaAdminCatalogTenant, error) {
	s.called = tenant
	if s.err != nil {
		return nil, s.err
	}
	return personaAdminCompositionTenant{entries: s.entries, evidence: s.evidence, evidenceErr: s.evidenceErr, review: s.review}, nil
}

type personaAdminCompositionTenant struct {
	entries     []agentpersonastore.CatalogEntry
	evidence    agentpersonastore.PublicationEvidence
	evidenceErr error
	review      *agentpersonastore.VerifiedReview
}

func (s personaAdminCompositionTenant) ResolveCurrentReview(context.Context, string, int64) (agentpersonastore.VerifiedReview, error) {
	if s.review == nil {
		return agentpersonastore.VerifiedReview{}, agentpersonastore.ErrPublicationEvidenceRequired
	}
	return *s.review, nil
}

func (s personaAdminCompositionTenant) ResolvePublicationEvidence(context.Context, string, int64) (agentpersonastore.PublicationEvidence, error) {
	return s.evidence, s.evidenceErr
}

func (s personaAdminCompositionTenant) ListCatalog(context.Context) ([]agentpersonastore.CatalogEntry, error) {
	return s.entries, nil
}

func TestTodo_AGENTP_018_CompositionRequiresTrustedDependencies(t *testing.T) {
	if _, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("missing dependencies error = %v, want denied", err)
	}
}

func TestTodo_AGENTP_018_CompositionReadsScopedCatalogAndRefusesLifecycle(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile := compositionPersonaProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	store := &personaAdminCompositionStore{entries: []agentpersonastore.CatalogEntry{{
		Version:   agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: encoded, ContentDigest: sealed.Digest},
		Lifecycle: agentpersonastore.StatePublished, BusinessOwner: profile.Owner, Steward: profile.Steward,
		Installations: []agentpersonastore.CatalogInstallation{{ID: "installation-1", PersonaID: profile.PersonaID, PersonaVersion: int64(profile.Version), ConversationID: "conversation-1", ConversationClass: agentpersonastore.ConversationPrivate, ChannelPolicy: agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}, Revision: 1, RevocationEpoch: 1, State: string(agentpersonastore.InstallationActive)}},
	}}}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: store, Skills: catalogSkills{}, Targets: catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin"}}, conversations: []productui.PersonaAdminTarget{{ID: "conversation-1", Label: "Room"}}}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Available || len(snapshot.Personas) != 1 || len(snapshot.Personas[0].Installations) != 1 {
		t.Fatalf("snapshot = %+v, want one scoped persona and placement", snapshot)
	}
	if store.called != principal.Tenant() {
		t.Fatalf("store scoped to %q, want %q", store.called, principal.Tenant())
	}
	if len(snapshot.Preview.EffectiveSkills) != 1 || snapshot.Preview.Conversation != "conversation-1" {
		t.Fatalf("preview from durable placement policy = %+v", snapshot.Preview)
	}
	for _, action := range []func(string) error{client.RequestReview, client.PublishPersona, client.RollbackPersona, client.SuspendPersona, client.RetirePersona} {
		if err := action(profile.PersonaID); !errors.Is(err, ErrPersonaCatalogLifecycleUnavailable) {
			t.Fatalf("lifecycle action error = %v, want unavailable", err)
		}
	}
}

func TestTodo_AGENTP_018_CompositionRejectsTamperedProfile(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile := compositionPersonaProfile(t)
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	store := &personaAdminCompositionStore{entries: []agentpersonastore.CatalogEntry{{Version: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: encoded, ContentDigest: "sha256:tampered"}, Lifecycle: agentpersonastore.StatePublished, BusinessOwner: profile.Owner, Steward: profile.Steward}}}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: store, Skills: catalogSkills{}, Targets: catalogTargets{}, Grants: &catalogGrants{}, Authorizer: &catalogAuth{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("tampered profile error = %v, want denied", err)
	}
}

func TestTodo_AGENTP_006_CatalogPublicationEligibilityRequiresCurrentAuthorities(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile := compositionPersonaProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(profile)
	store := &personaAdminCompositionStore{entries: []agentpersonastore.CatalogEntry{{Version: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: int64(profile.Version), Profile: encoded, ContentDigest: sealed.Digest}, Lifecycle: agentpersonastore.StateInReview, BusinessOwner: profile.Owner, Steward: profile.Steward}}}
	reader := personaAdminCatalogVersions{store: store}
	versions, err := reader.ListPersonaCatalogVersions(ctx, principal.Tenant())
	if err != nil || len(versions) != 1 || !versions[0].ReviewRequired || versions[0].ReviewApproved {
		t.Fatalf("missing authority evidence authorized publication: %+v, %v", versions, err)
	}
	store.evidence = agentpersonastore.PublicationEvidence{ReviewID: "verified-current-review", EvaluationRunID: "verified-current-evaluation"}
	versions, err = reader.ListPersonaCatalogVersions(ctx, principal.Tenant())
	if err != nil || !versions[0].ReviewApproved || versions[0].EvaluationRef != store.evidence.EvaluationRunID {
		t.Fatalf("verified exact evidence not projected: %+v, %v", versions, err)
	}
	store.evidenceErr = agentpersonastore.ErrPublicationEvidenceRequired
	versions, err = reader.ListPersonaCatalogVersions(ctx, principal.Tenant())
	if err != nil || versions[0].ReviewApproved || versions[0].EvaluationRef != "" {
		t.Fatalf("revoked or stale evidence retained publication eligibility: %+v, %v", versions, err)
	}
}

func TestTodo_AGENTP_018_IndependentReviewIsVisibleBeforeEvaluation(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile := compositionPersonaProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(profile)
	store := &personaAdminCompositionStore{entries: []agentpersonastore.CatalogEntry{{Version: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: int64(profile.Version), Profile: encoded, ContentDigest: sealed.Digest}, Lifecycle: agentpersonastore.StateInReview, BusinessOwner: profile.Owner, Steward: profile.Steward}}, evidenceErr: agentpersonastore.ErrPublicationEvidenceRequired, review: &agentpersonastore.VerifiedReview{ReviewerID: "independent-reviewer", Decision: "APPROVE", GrantCurrent: true}}
	versions, err := (personaAdminCatalogVersions{store: store}).ListPersonaCatalogVersions(ctx, principal.Tenant())
	if err != nil || len(versions) != 1 || !versions[0].ReviewApproved || versions[0].Reviewer != "independent-reviewer" || versions[0].EvaluationRef != "" {
		t.Fatalf("independent review projection: %+v, %v", versions, err)
	}
	store.review = nil
	versions, err = (personaAdminCatalogVersions{store: store}).ListPersonaCatalogVersions(ctx, principal.Tenant())
	if err != nil || versions[0].ReviewApproved || versions[0].Reviewer != "" {
		t.Fatalf("stale review retained: %+v, %v", versions, err)
	}
}

func compositionPersonaProfile(t *testing.T) agentpersona.PersonaProfile {
	t.Helper()
	return agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-composed", Version: 1, Handle: "composed", DisplayName: "Composed Persona", AvatarRef: "avatar", Purpose: "Read approved records", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer with approved records.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}}
}
