package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

type versionRolloutFake struct {
	row           agentpersonastore.PersonaVersion
	installations map[string]agentpersonastore.PersonaInstallation
	plan          agentrollout.VersionPlan
	progress      agentpersonastore.VersionRolloutProgress
	evidence      agentpersonastore.PublicationEvidence
	failure       error
	writes        int
}

func (f *versionRolloutFake) ForRolloutTenant(context.Context, values.TenantId) (AgentVersionRolloutTenant, error) {
	return f, nil
}
func (f *versionRolloutFake) GetInstallation(_ context.Context, id string) (agentpersonastore.PersonaInstallation, error) {
	i, ok := f.installations[id]
	if !ok {
		return i, agentpersonastore.ErrNotFound
	}
	return i, nil
}
func (f *versionRolloutFake) GetVersion(context.Context, string, int64) (agentpersonastore.PersonaVersion, error) {
	return f.row, nil
}
func (f *versionRolloutFake) Lifecycle(context.Context, string, int64) (agentpersonastore.LifecycleState, error) {
	return agentpersonastore.StatePublished, nil
}
func (f *versionRolloutFake) ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error) {
	return []agentpersonastore.PersonaVersion{f.row}, nil
}
func (f *versionRolloutFake) ListVersionRolloutInstallations(_ context.Context, id string) ([]agentpersonastore.PersonaInstallation, error) {
	out := []agentpersonastore.PersonaInstallation{}
	for _, in := range f.installations {
		if in.PersonaID == id && in.State == agentpersonastore.InstallationActive {
			out = append(out, in)
		}
	}
	return out, nil
}
func (f *versionRolloutFake) ResolvePublicationEvidence(context.Context, string, int64) (agentpersonastore.PublicationEvidence, error) {
	return f.evidence, f.failure
}
func (f *versionRolloutFake) SaveVersionRollout(_ context.Context, p agentrollout.VersionPlan) (agentpersonastore.VersionRolloutProgress, error) {
	f.plan = p
	f.progress = agentpersonastore.VersionRolloutProgress{Revision: 1, Stage: "PREVIEWED"}
	f.writes++
	return f.progress, nil
}
func (f *versionRolloutFake) GetVersionRollout(context.Context, string) (agentrollout.VersionPlan, agentpersonastore.VersionRolloutProgress, error) {
	return f.plan, f.progress, nil
}
func (f *versionRolloutFake) ApproveVersionRollout(_ context.Context, _, _, actor string, _ int64) (agentpersonastore.VersionRolloutProgress, error) {
	f.progress.ApproverID = actor
	f.progress.Stage = "APPROVED"
	f.progress.Revision++
	f.writes++
	return f.progress, nil
}
func (f *versionRolloutFake) PromoteVersionRollout(ctx context.Context, id, digest, actor string, rev int64) (agentpersonastore.VersionRolloutProgress, error) {
	return f.ApproveVersionRollout(ctx, id, digest, actor, rev)
}
func (f *versionRolloutFake) ApplyVersionRollout(_ context.Context, _, _, _ string, _ int64, c agentrollout.VersionCandidate, target agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, agentpersonastore.VersionRolloutProgress, error) {
	target.Revision++
	target.RevocationEpoch++
	f.installations[c.InstallationID] = target
	f.progress.Cursor++
	f.progress.Revision++
	f.writes++
	if f.progress.Cursor == len(f.plan.Candidates) {
		f.progress.Stage = "COMPLETE"
	} else if f.progress.Cursor == f.plan.CanaryCount {
		f.progress.Stage = "CANARY_COMPLETE"
	}
	return target, f.progress, nil
}

type versionRolloutPlacement struct {
	actor        string
	revision     uint64
	failure      error
	fenceFailure error
	policy       agentpersonastore.ChannelPolicy
}

func (f *versionRolloutPlacement) ResolvePersonaAdminPlacement(_ context.Context, a PersonaAdminCommandActor, room string) (PersonaAdminPlacementFacts, error) {
	return PersonaAdminPlacementFacts{Tenant: a.Tenant, ConversationID: room, ManagerID: f.actor, Revision: f.revision, Class: agentpersonastore.ConversationPrivate, Policy: f.policy}, f.failure
}
func (f *versionRolloutPlacement) WithPersonaAdminPlacementFence(_ context.Context, _ PersonaAdminPlacementFacts, fn func() error) error {
	if f.fenceFailure != nil {
		return f.fenceFailure
	}
	return fn()
}

func versionRolloutFixture(t *testing.T) (context.Context, *AgentVersionRolloutService, *versionRolloutFake, *versionRolloutPlacement) {
	t.Helper()
	ctx, p := personaAdminCommandContext(t)
	profile := agentpersona.PersonaProfile{PersonaID: "persona", Version: 2, TierCeiling: agentskills.TierT0, Manifest: agentpersona.AgentManifestRef{ID: "manifest", Version: 2, Digest: "manifest", SchemaVersion: 1}, Handle: "assistant", DisplayName: "Assistant", AvatarRef: "avatar:assistant", Purpose: "Help", Owner: "owner", Steward: "steward", ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org"}}, EvalSuiteRef: "eval", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 2, MaxLatencyMS: 500}, SkillPins: []agentskills.SkillPin{{ID: "policy.read", Version: 1, Digest: "skill"}}}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(profile)
	policy := agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}, AlwaysPrivate: true}
	store := &versionRolloutFake{row: agentpersonastore.PersonaVersion{TenantID: p.Tenant(), PersonaID: "persona", Version: 2, Profile: body, ContentDigest: sealed.Digest}, evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}, installations: map[string]agentpersonastore.PersonaInstallation{}}
	for _, id := range []string{"install-a", "install-b"} {
		store.installations[id] = agentpersonastore.PersonaInstallation{TenantID: p.Tenant(), InstallationID: id, PersonaID: "persona", PersonaVersion: 1, ConversationID: "room", ConversationClass: agentpersonastore.ConversationPrivate, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1, ChannelPolicy: policy}
	}
	placement := &versionRolloutPlacement{actor: p.Subject(), revision: 4, policy: policy}
	s := &AgentVersionRolloutService{Store: store, Authorizer: &personaAdminCommandAuthFake{}, Placement: placement, Governed: GovernedPersonaAdminInstallation{Placement: placement, Versions: personaAdminVersionFake{store.row}, Profiles: personaProfileBuilderFake{}}, NewID: func() string { return "plan" }}
	return ctx, s, store, placement
}

func versionRolloutPreview() AgentVersionRolloutCommand {
	return AgentVersionRolloutCommand{Action: "PREVIEW", PersonaID: "persona", TargetVersion: 2, InstallationIDs: []string{"install-b", "install-a"}, CanaryIDs: []string{"install-a"}, BatchLimit: 100}
}
func versionRolloutAction(action string, r AgentVersionRolloutReceipt) AgentVersionRolloutCommand {
	return AgentVersionRolloutCommand{Action: action, RolloutID: r.Plan.ID, Digest: r.Plan.Digest, Revision: r.Progress.Revision}
}

func TestTodo_AGENT_044_Served(t *testing.T) {
	ctx, s, store, _ := versionRolloutFixture(t)
	r, err := s.Execute(ctx, versionRolloutPreview())
	if err != nil {
		t.Fatal(err)
	}
	if store.installations["install-a"].PersonaVersion != 1 {
		t.Fatal("preview changed actual installation")
	}
	r, err = s.Execute(ctx, versionRolloutAction("APPROVE", r))
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Execute(ctx, versionRolloutAction("ADVANCE", r))
	if err != nil || r.Progress.Stage != "CANARY_COMPLETE" || store.installations["install-b"].PersonaVersion != 1 {
		t.Fatalf("canary state %+v: %v", r.Progress, err)
	}
	if _, err = s.Execute(ctx, versionRolloutAction("ADVANCE", r)); !errors.Is(err, agentrollout.ErrApprovalRequired) {
		t.Fatalf("unpromoted remainder accepted %v", err)
	}
	r, err = s.Execute(ctx, versionRolloutAction("PROMOTE", r))
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Execute(ctx, versionRolloutAction("ADVANCE", r))
	if err != nil || r.Progress.Stage != "COMPLETE" || store.installations["install-b"].PersonaVersion != 2 {
		t.Fatalf("completion %+v %v", r.Progress, err)
	}
	if store.installations["install-b"].RevocationEpoch != 2 || store.installations["install-a"].ChannelPolicy.MaxTier != "T0" {
		t.Fatal("version upgrade lost epoch or grant")
	}
}

func TestTodo_AGENT_044_Security_Served(t *testing.T) {
	for _, change := range []func(*versionRolloutFake, *versionRolloutPlacement){func(f *versionRolloutFake, _ *versionRolloutPlacement) {
		i := f.installations["install-a"]
		i.State = agentpersonastore.InstallationSuspended
		f.installations["install-a"] = i
	}, func(_ *versionRolloutFake, p *versionRolloutPlacement) { p.actor = "departed" }, func(_ *versionRolloutFake, p *versionRolloutPlacement) { p.revision++ }, func(f *versionRolloutFake, _ *versionRolloutPlacement) { f.evidence.EvaluationRunID = "new-evaluation" }, func(f *versionRolloutFake, _ *versionRolloutPlacement) {
		i := f.installations["install-a"]
		i.RevocationEpoch++
		f.installations["install-a"] = i
	}} {
		ctx, s, store, p := versionRolloutFixture(t)
		r, e := s.Execute(ctx, versionRolloutPreview())
		if e != nil {
			t.Fatal(e)
		}
		r, e = s.Execute(ctx, versionRolloutAction("APPROVE", r))
		if e != nil {
			t.Fatal(e)
		}
		before := store.writes
		change(store, p)
		if _, e = s.Execute(ctx, versionRolloutAction("ADVANCE", r)); e == nil || store.writes != before {
			t.Fatal("changed/revoked authority mutated installation")
		}
	}
}

// TestTodo_AGENT_044_Fault injects a failed membership fence and a corrupted
// progress row into the served rollout; neither may move a placement.
func TestTodo_AGENT_044_Fault(t *testing.T) {
	t.Run("membership fence unavailable", versionRolloutFenceFailure)
	t.Run("corrupted progress cursor", versionRolloutCorruptedProgress)
}

func versionRolloutFenceFailure(t *testing.T) {
	ctx, s, store, p := versionRolloutFixture(t)
	r, e := s.Execute(ctx, versionRolloutPreview())
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.Execute(ctx, versionRolloutAction("APPROVE", r))
	if e != nil {
		t.Fatal(e)
	}
	p.fenceFailure = errors.New("membership fence unavailable")
	before := store.writes
	if _, e = s.Execute(ctx, versionRolloutAction("ADVANCE", r)); !errors.Is(e, p.fenceFailure) || store.writes != before {
		t.Fatalf("failed fence changed state %v", e)
	}
}

func TestTodo_AGENT_044_HTTP(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := OverlayAgentVersionRolloutHTTP(next, nil, transport.Config{})
	for _, tc := range []struct {
		path, method string
		status       int
	}{{"/other", http.MethodGet, 204}, {AgentVersionRolloutPath, http.MethodGet, 405}, {AgentVersionRolloutPath, http.MethodPost, 503}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestTodo_AGENT_044_Catalog(t *testing.T) {
	ctx, s, _, p := versionRolloutFixture(t)
	r, err := s.Execute(ctx, AgentVersionRolloutCommand{Action: "CATALOG"})
	if err != nil || r.Catalog == nil || len(r.Catalog.Versions) != 1 || len(r.Catalog.Installations) != 2 || r.Catalog.Versions[0].ManifestID != "manifest" {
		t.Fatalf("catalog %+v %v", r.Catalog, err)
	}
	p.actor = "nonmanager"
	r, err = s.Execute(ctx, AgentVersionRolloutCommand{Action: "CATALOG"})
	if err != nil || len(r.Catalog.Installations) != 0 {
		t.Fatal("nonmanager sees rollout placements")
	}
	if _, err = s.Execute(ctx, AgentVersionRolloutCommand{Action: "CATALOG", InstallationIDs: []string{"unexpected"}}); !errors.Is(err, agentrollout.ErrInvalid) {
		t.Fatalf("catalog accepted selection %v", err)
	}
}

func TestTodo_AGENT_044_InvalidServedCommands(t *testing.T) {
	ctx, s, store, _ := versionRolloutFixture(t)
	if _, err := (*AgentVersionRolloutService)(nil).Execute(ctx, AgentVersionRolloutCommand{}); !errors.Is(err, ErrAgentVersionRolloutDenied) {
		t.Fatal("nil service admitted")
	}
	if _, err := s.Execute(context.Background(), versionRolloutPreview()); !errors.Is(err, ErrAgentVersionRolloutDenied) {
		t.Fatal("unauthenticated request admitted")
	}
	for _, command := range []AgentVersionRolloutCommand{{Action: "PREVIEW"}, {Action: "READ"}, {Action: "PREVIEW", PersonaID: "persona", TargetVersion: 2, InstallationIDs: []string{"install-a", "install-a"}, CanaryIDs: []string{"install-a"}, BatchLimit: 1}} {
		if _, err := s.Execute(ctx, command); err == nil {
			t.Fatalf("invalid command accepted %+v", command)
		}
	}
	if store.writes != 0 {
		t.Fatal("invalid request persisted")
	}
	r, err := s.Execute(ctx, versionRolloutPreview())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, AgentVersionRolloutCommand{Action: "READ", RolloutID: r.Plan.ID}); err != nil {
		t.Fatal(err)
	}
	command := versionRolloutAction("APPROVE", r)
	command.Digest = "forged"
	if _, err = s.Execute(ctx, command); !errors.Is(err, agentrollout.ErrPreviewStale) {
		t.Fatal("forged approval admitted")
	}
	command = versionRolloutAction("UNSUPPORTED", r)
	if _, err = s.Execute(ctx, command); !errors.Is(err, agentrollout.ErrInvalid) {
		t.Fatal("unsupported command admitted")
	}
}

func TestTodo_AGENT_044_Recovery_ExplicitRollback(t *testing.T) {
	ctx, s, store, _ := versionRolloutFixture(t)
	r, err := s.Execute(ctx, versionRolloutPreview())
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"APPROVE", "ADVANCE", "PROMOTE", "ADVANCE"} {
		r, err = s.Execute(ctx, versionRolloutAction(action, r))
		if err != nil {
			t.Fatal(err)
		}
	}
	var profile agentpersona.PersonaProfile
	if err = json.Unmarshal(store.row.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	profile.Version = 1
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	store.row.Version = 1
	store.row.Profile, _ = json.Marshal(profile)
	store.row.ContentDigest = sealed.Digest
	s.Governed.Versions = personaAdminVersionFake{store.row}
	request := versionRolloutPreview()
	request.TargetVersion = 1
	request.InstallationIDs = []string{"install-a"}
	request.CanaryIDs = []string{"install-a"}
	r, err = s.Execute(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if store.installations["install-a"].PersonaVersion != 2 {
		t.Fatal("rollback preview mutated actual version")
	}
	r, err = s.Execute(ctx, versionRolloutAction("APPROVE", r))
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Execute(ctx, versionRolloutAction("ADVANCE", r))
	if err != nil || r.Progress.Stage != "COMPLETE" {
		t.Fatalf("rollback progress %+v: %v", r.Progress, err)
	}
	if store.installations["install-a"].PersonaVersion != 1 || store.installations["install-a"].RevocationEpoch != 3 || store.installations["install-b"].PersonaVersion != 2 {
		t.Fatal("rollback lost exact scope or monotonic revocation fence")
	}
}

func versionRolloutCorruptedProgress(t *testing.T) {
	for _, cursor := range []int{-1, 3} {
		ctx, s, store, _ := versionRolloutFixture(t)
		r, err := s.Execute(ctx, versionRolloutPreview())
		if err != nil {
			t.Fatal(err)
		}
		store.progress.Cursor = cursor
		before := store.writes
		if _, err = s.Execute(ctx, versionRolloutAction("APPROVE", r)); !errors.Is(err, agentrollout.ErrPreviewStale) || before != store.writes {
			t.Fatalf("corrupted cursor %d admitted: %v", cursor, err)
		}
	}
}

func TestTodo_AGENT_044_Security_ConversationCeiling(t *testing.T) {
	profile := agentpersona.PersonaProfile{TierCeiling: agentskills.TierT2, ConversationTierCeilings: map[agentpersona.ConversationKind]agentskills.SideEffectTier{agentpersona.ConversationDirect: agentskills.TierT0}}
	policy := agentpersonastore.ChannelPolicy{MaxTier: "T0"}
	if !agentRolloutScopeFits(profile, policy, agentpersonastore.ConversationOneToOne) {
		t.Fatal("narrower DM ceiling was rejected")
	}
	if agentRolloutScopeFits(profile, policy, agentpersonastore.ConversationPrivate) {
		t.Fatal("channel expansion passed old grant")
	}
}
