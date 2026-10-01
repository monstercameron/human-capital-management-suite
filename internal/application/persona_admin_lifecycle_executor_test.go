package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAdminLifecycleTenantFake struct {
	versions       []agentpersonastore.PersonaVersion
	states         map[int64]agentpersonastore.LifecycleState
	events         []agentpersonastore.LifecycleEvent
	published      agentpersonastore.LifecycleEvent
	evidence       agentpersonastore.PublicationEvidence
	publishErr     error
	createdVersion agentpersonastore.PersonaVersion
	createdActor   string
}

func (f *personaAdminLifecycleTenantFake) ListVersions(_ context.Context, personaID string) ([]agentpersonastore.PersonaVersion, error) {
	var out []agentpersonastore.PersonaVersion
	for _, version := range f.versions {
		if version.PersonaID == personaID {
			out = append(out, version)
		}
	}
	return out, nil
}
func (f *personaAdminLifecycleTenantFake) Lifecycle(_ context.Context, _ string, version int64) (agentpersonastore.LifecycleState, error) {
	state, ok := f.states[version]
	if !ok {
		return "", agentpersonastore.ErrNotFound
	}
	return state, nil
}
func (*personaAdminLifecycleTenantFake) ListLifecycle(context.Context, string, int64) ([]agentpersonastore.LifecycleEvent, error) {
	return nil, nil
}
func (f *personaAdminLifecycleTenantFake) AppendLifecycle(_ context.Context, event agentpersonastore.LifecycleEvent) error {
	state, ok := f.states[event.PersonaVersion]
	if !ok || state != event.From {
		return agentpersonastore.ErrConflict
	}
	f.states[event.PersonaVersion] = event.To
	f.events = append(f.events, event)
	return nil
}
func (f *personaAdminLifecycleTenantFake) Publish(_ context.Context, event agentpersonastore.LifecycleEvent, evidence agentpersonastore.PublicationEvidence) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	if f.states[event.PersonaVersion] != event.From || evidence.ReviewID == "" || evidence.EvaluationRunID == "" {
		return agentpersonastore.ErrPublicationEvidenceRequired
	}
	f.states[event.PersonaVersion] = event.To
	f.published, f.evidence = event, evidence
	return nil
}
func (*personaAdminLifecycleTenantFake) Install(context.Context, agentpersonastore.PersonaInstallation) error {
	return nil
}
func (f *personaAdminLifecycleTenantFake) CreateVersionDraft(_ context.Context, version agentpersonastore.PersonaVersion, actor string, _ time.Time) error {
	f.createdVersion, f.createdActor = version, actor
	f.versions = append(f.versions, version)
	f.states[version.Version] = agentpersonastore.StateDraft
	return nil
}

type personaAdminLifecycleStoreFake struct{ tenant PersonaAdminLifecycleTenant }

func (f personaAdminLifecycleStoreFake) ForTenant(_ context.Context, _ values.TenantId) (PersonaAdminLifecycleTenant, error) {
	return f.tenant, nil
}

type personaAdminReviewIssuerFake struct {
	review agentpersonastore.VerifiedReview
	err    error
}

type personaAdminReviewAuthorityFake struct{}

func (personaAdminReviewAuthorityFake) AuthorizePersonaReview(context.Context, values.TenantId, string) error {
	return nil
}

func (f personaAdminReviewIssuerFake) IssuePersonaReview(_ context.Context, tenant values.TenantId, personaID string, version int64, reviewer, decision string) (agentpersonastore.VerifiedReview, error) {
	if f.err != nil {
		return agentpersonastore.VerifiedReview{}, f.err
	}
	result := f.review
	result.TenantID, result.PersonaID, result.PersonaVersion = string(tenant), personaID, version
	result.ReviewerID, result.Permission, result.Decision = reviewer, "persona:review", decision
	result.GrantCurrent, result.ReviewID, result.ProfileDigest, result.ReviewDigest = true, "review-1", "sha256:profile", "sha256:review"
	return result, nil
}

func TestTodo_AGENTP_006_RecoveryReviewRetriesWithoutAnotherLifecycleEvent(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateDraft}}
	writeErr := errors.New("review evidence pool temporarily unavailable")
	reviews, _ := NewPersonaReviewIssuanceService(personaAdminReviewAuthorityFake{}, personaAdminReviewIssuerFake{err: writeErr})
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant}, personaAdminLifecycleAuthorizerFake{}, nil, nil, reviews, nil, nil, nil, func() time.Time { return time.Unix(200, 0) }, func() string { return "review-event" })
	command := PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: "persona-a", Decision: "APPROVE"}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); !errors.Is(err, writeErr) {
		t.Fatalf("first review error = %v", err)
	}
	if len(tenant.events) != 1 || tenant.states[1] != agentpersonastore.StateInReview {
		t.Fatalf("failed evidence changed wrong state: %+v", tenant)
	}
	executor.Reviews, _ = NewPersonaReviewIssuanceService(personaAdminReviewAuthorityFake{}, personaAdminReviewIssuerFake{})
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil {
		t.Fatal(err)
	}
	if len(tenant.events) != 1 {
		t.Fatalf("retry duplicated lifecycle event: %+v", tenant.events)
	}
}

func TestTodo_AGENTP_006_RequestReviewQueuesWithoutIssuingApproval(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateDraft}}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant}, personaAdminLifecycleAuthorizerFake{}, nil, nil, nil, nil, nil, nil, func() time.Time { return time.Unix(200, 0) }, func() string { return "queue-event" })
	command := PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: "persona-a"}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil {
		t.Fatal(err)
	}
	if len(tenant.events) != 1 || tenant.states[1] != agentpersonastore.StateInReview {
		t.Fatalf("queue state/events = %v / %+v", tenant.states, tenant.events)
	}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil || len(tenant.events) != 1 {
		t.Fatalf("queue retry = %v, events=%+v", err, tenant.events)
	}
	command.Decision = "APPROVE"
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); !errors.Is(err, ErrPersonaAdminLifecycleUnavailable) {
		t.Fatalf("approval without separate review authority = %v", err)
	}
}

func TestTodo_AGENTP_006_IndependentDecisionRequiresAuthorQueuedLatestVersion(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateDraft}}
	reviews, err := NewPersonaReviewIssuanceService(personaAdminReviewAuthorityFake{}, personaAdminReviewIssuerFake{})
	if err != nil {
		t.Fatal(err)
	}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant}, personaAdminLifecycleAuthorizerFake{}, nil, nil, reviews, nil, nil, nil, nil, nil)
	command := PersonaAdminCommand{Action: PersonaAdminReview, PersonaID: "persona-a", Decision: "APPROVE"}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); !errors.Is(err, ErrPersonaAdminLifecycleUnavailable) || len(tenant.events) != 0 || tenant.states[1] != agentpersonastore.StateDraft {
		t.Fatalf("reviewer changed an unqueued draft: %v, %+v", err, tenant)
	}
	tenant.states[1] = agentpersonastore.StateInReview
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil || len(tenant.events) != 0 {
		t.Fatalf("independent decision changed lifecycle: %v, %+v", err, tenant)
	}
	tenant.versions = append(tenant.versions, agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 2})
	tenant.states[2] = agentpersonastore.StateDraft
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); !errors.Is(err, ErrPersonaAdminLifecycleUnavailable) {
		t.Fatalf("review decision skipped newer unqueued author version: %v", err)
	}
}

type personaAdminPublicationEvidenceFake struct {
	evidence agentpersonastore.PublicationEvidence
}

func (f personaAdminPublicationEvidenceFake) ResolvePersonaPublicationEvidence(context.Context, values.TenantId, string, int64) (agentpersonastore.PublicationEvidence, error) {
	return f.evidence, nil
}

type personaAdminLifecycleAuthorizerFake struct{ err error }

func (f personaAdminLifecycleAuthorizerFake) AuthorizePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommandAction, string) error {
	return f.err
}

func TestTodo_AGENTP_006_LifecycleExecutorMovesDraftThroughReviewAndEvidencePublication(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	profile, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1},
		PersonaID: "persona-a", Version: 1, Handle: "persona-a", DisplayName: "Persona A", AvatarRef: "avatar", Purpose: "Help staff",
		Audience:  agentpersona.Audience{Roles: []string{"staff"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Owner: "user-owner", Steward: "user-steward", EvalSuiteRef: "eval-v1", Instructions: "Answer approved questions.",
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, _ := json.Marshal(profile.Profile)
	version := agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1, ContentDigest: profile.Digest, Profile: profileJSON}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{version}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateDraft}}
	issuer, err := NewPersonaReviewIssuanceService(personaAdminReviewAuthorityFake{}, personaAdminReviewIssuerFake{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "eval-run-1"}}
	authorizer := personaAdminLifecycleAuthorizerFake{}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, authorizer, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, issuer, evidence, nil, nil, func() time.Time { return time.Unix(200, 0) }, func() string { return "event-1" })
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: "persona-a", Decision: "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if tenant.states[1] != agentpersonastore.StateInReview || len(tenant.events) != 1 || tenant.events[0].ActorID != principal.Subject() {
		t.Fatalf("review state/events = %v / %+v", tenant.states[1], tenant.events)
	}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: "persona-a"}); err != nil {
		t.Fatal(err)
	}
	if tenant.published.To != agentpersonastore.StatePublished || tenant.evidence != evidence.evidence || tenant.states[1] != agentpersonastore.StatePublished {
		t.Fatalf("publication = %+v evidence=%+v state=%s", tenant.published, tenant.evidence, tenant.states[1])
	}
}

func TestTodo_AGENTP_006_LifecycleExecutorRequiresReviewPermissionAndEvidence(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	version := agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1, ContentDigest: "sha256:profile"}
	for _, tc := range []struct {
		name       string
		authorizer error
		evidence   agentpersonastore.PublicationEvidence
		want       error
	}{
		{name: "denied before lifecycle write", authorizer: errors.New("review grant missing"), want: ErrPersonaAdminCommandUnavailable},
		{name: "missing evidence", evidence: agentpersonastore.PublicationEvidence{}, want: ErrPersonaAdminLifecycleUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{version}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateDraft}}
			evidence := personaAdminPublicationEvidenceFake{evidence: tc.evidence}
			executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, personaAdminLifecycleAuthorizerFake{err: tc.authorizer}, nil, nil, nil, evidence, nil, nil, func() time.Time { return time.Unix(200, 0) }, func() string { return "event-1" })
			if tc.name == "missing evidence" {
				tenant.states[1] = agentpersonastore.StateInReview
			}
			command := PersonaAdminCommand{Action: PersonaAdminRequestReview, PersonaID: "persona-a"}
			if tc.name == "missing evidence" {
				command.Action = PersonaAdminPublish
			}
			err := executor.ExecutePersonaAdminCommand(ctx, actor, command)
			if !errors.Is(err, tc.want) {
				t.Fatalf("command error = %v, want %v", err, tc.want)
			}
			if tc.name == "denied before lifecycle write" && (len(tenant.events) != 0 || tenant.states[1] != agentpersonastore.StateDraft) {
				t.Fatalf("denied request changed lifecycle: %+v %v", tenant.events, tenant.states)
			}
		})
	}
}

func TestTodo_AGENTP_018_LifecycleExecutorRoutesStarterDraftThroughBuilder(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	instructions := "Answer policy questions only from approved records."
	manifest := personaStarterManifest(instructions)
	store := &personaDraftStoreFake{}
	drafts := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: &personaStarterProfileBuilderSpy{}, Clock: personaDraftClockFake{now}}
	builder := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:creator", now))
	principal, ok := trust.FromContext(ctx)
	if !ok {
		t.Fatal("test principal missing")
	}
	request := validPersonaStarterRequest(manifest)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	executor := NewPersonaAdminLifecycleExecutor(nil, personaAdminLifecycleAuthorizerFake{}, drafts, builder, nil, nil, nil, nil, nil, nil)
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminCreateDraft, PersonaID: request.PersonaID, Starter: &request}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.actor != principal.Subject() || store.version.ContentDigest == "" || store.version.TenantID != principal.Tenant() {
		t.Fatalf("starter draft persistence = %+v, calls %d", store.version, store.calls)
	}
}

func TestTodo_AGENTP_018_LifecycleExecutorBuildsAndAtomicallyPersistsStarterVersion(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	instructions := "Answer policy questions only from approved records."
	manifest := personaStarterManifest(instructions)
	request := validPersonaStarterRequest(manifest)
	starter, ok := agenttemplate.PersonaStarterFor(request.StarterID, request.StarterVersion)
	if !ok {
		t.Fatal("test starter is missing")
	}
	profile := personaStarterProfile(starter, request, manifest, instructions)
	current, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(current.Profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: 1, Profile: encoded, ContentDigest: current.Digest}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{row}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StatePublished}}
	profiles := &personaStarterProfileBuilderSpy{}
	drafts := &PersonaAdminDraftService{Profiles: profiles}
	builder := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenant}, personaAdminLifecycleAuthorizerFake{}, drafts, builder, nil, nil, nil, nil, func() time.Time { return time.Unix(250, 0) }, func() string { return "version-event" })
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	command := PersonaAdminCommand{Action: PersonaAdminCreateVersion, PersonaID: profile.PersonaID, StarterVersionEdit: &PersonaStarterVersionRequest{StarterID: request.StarterID, StarterVersion: request.StarterVersion, Handle: "policy-helper-v2", DisplayName: "Policy Helper v2", Purpose: "Answer approved policy questions", ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, BusinessOwnerID: profile.Owner, TechnicalStewardID: profile.Steward}}
	if err := executor.ExecutePersonaAdminCommand(ctx, actor, command); err != nil {
		t.Fatal(err)
	}
	if tenant.createdVersion.Version != 2 || tenant.createdActor != principal.Subject() || tenant.states[2] != agentpersonastore.StateDraft {
		t.Fatalf("atomic version result = %+v actor=%q state=%q", tenant.createdVersion, tenant.createdActor, tenant.states[2])
	}
	var persisted agentpersona.PersonaProfile
	if err := json.Unmarshal(tenant.createdVersion.Profile, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != 2 || persisted.Handle != "policy-helper-v2" || persisted.ChannelClasses[0] != agentpersona.ChannelPrivate {
		t.Fatalf("persisted profile = %+v", persisted)
	}
}
