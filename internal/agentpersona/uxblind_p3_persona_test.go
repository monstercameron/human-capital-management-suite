package agentpersona

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

func TestTodo_AGENTP_003_StarterPlacementCeilingsAreSealedAndDetached(t *testing.T) {
	profile, _ := personaFixture(t)
	profile.AllowedPlacementClasses = []string{"PRIVATE", "MANAGER"}
	profile.AlwaysPrivate = true
	profile.ConversationTierCeilings = map[ConversationKind]agentskills.SideEffectTier{ConversationDirect: agentskills.TierPrivateDraft, ConversationThread: agentskills.TierRead}
	profile.Template = &TemplateProvenance{ID: "starter.comp", Version: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	sealed, err := Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Profile.TierForConversation(ConversationThread) != agentskills.TierRead || sealed.Profile.TierForConversation(ConversationDirect) != agentskills.TierPrivateDraft {
		t.Fatal("narrower conversation ceiling ignored")
	}
	profile.AllowedPlacementClasses[0] = "EXTERNAL"
	profile.Template.Digest = "forged"
	profile.ConversationTierCeilings[ConversationThread] = agentskills.TierPrivateDraft
	if err := sealed.Verify(); err != nil {
		t.Fatalf("caller mutation changed sealed metadata: %v", err)
	}
	sealed.Profile.AlwaysPrivate = false
	if err := sealed.Verify(); err == nil {
		t.Fatal("privacy widening kept the immutable profile seal")
	}
}

func TestTodo_AGENTP_003_SecurityConversationCeilingCannotWiden(t *testing.T) {
	profile, _ := personaFixture(t)
	profile.ConversationTierCeilings = map[ConversationKind]agentskills.SideEffectTier{ConversationThread: agentskills.TierT3}
	if _, err := Seal(profile); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("widened tier seal=%v", err)
	}
	profile.ConversationTierCeilings = map[ConversationKind]agentskills.SideEffectTier{ConversationGroup: agentskills.TierRead}
	if _, err := Seal(profile); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("undeclared conversation seal=%v", err)
	}
}

type personaSkillCatalog struct {
	records map[agentskills.SkillKey]agentskills.SkillRecord
	grants  map[agentskills.SkillKey]SkillAudienceGrant
	errors  map[agentskills.SkillKey]error
}

func (c *personaSkillCatalog) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if err := c.errors[pin.Key()]; err != nil {
		return agentskills.SkillRecord{}, err
	}
	record, ok := c.records[pin.Key()]
	if !ok {
		return agentskills.SkillRecord{}, agentskills.ErrUnknownSkill
	}
	if record.Digest != pin.Digest {
		return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
	}
	if record.Status == agentskills.StatusRetired {
		return agentskills.SkillRecord{}, agentskills.ErrRetiredSkill
	}
	return record, nil
}

func (c *personaSkillCatalog) AudienceGrant(pin agentskills.SkillPin) (SkillAudienceGrant, error) {
	grant, ok := c.grants[pin.Key()]
	if !ok {
		return SkillAudienceGrant{}, errors.New("missing admin grant")
	}
	return grant, nil
}

type personaReviewAuthority struct{ denied bool }

func (a personaReviewAuthority) CanReview(reviewer string, _ PersonaProfile) bool {
	return !a.denied && reviewer == "steward-1"
}

type personaEvaluationStore struct {
	mu      sync.Mutex
	record  EvaluationRecord
	failure error
}

func (s *personaEvaluationStore) LookupEvaluation(digest string) (EvaluationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return EvaluationRecord{}, s.failure
	}
	if digest != s.record.RunDigest {
		return EvaluationRecord{}, errors.New("evaluation not found")
	}
	return s.record, nil
}

type personaManifestCompatibility struct{ failure error }

func (m personaManifestCompatibility) Compatible(AgentManifestRef) error { return m.failure }

type personaManifestResolver struct {
	manifest agentmanifest.Manifest
	failure  error
}

func (r personaManifestResolver) ResolveAgentManifest(ref AgentManifestRef) (agentmanifest.Manifest, error) {
	if r.failure != nil {
		return agentmanifest.Manifest{}, r.failure
	}
	if ref.ID != r.manifest.ID || uint64(ref.Version) != r.manifest.Version || ref.SchemaVersion != r.manifest.SchemaVersion {
		return agentmanifest.Manifest{}, errors.New("manifest identity not found")
	}
	return r.manifest, nil
}

func personaManifest() agentmanifest.Manifest {
	return agentmanifest.Manifest{
		SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: "agent.onboarding", Version: 4,
		OwnerID: "owner-1", Purpose: "support onboarding", InstructionsDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{},
		ModelPolicy:     agentmanifest.Reference{ID: "model.default", Version: 1, SchemaVersion: 1, Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 10000, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1},
		OutputSchema:   agentmanifest.Reference{ID: "output.answer", Version: 1, SchemaVersion: 1, Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		ContextGrants:  []agentmanifest.Reference{},
		EvaluationRefs: []agentmanifest.Reference{{ID: "eval.onboarding", Version: 1, SchemaVersion: 1, Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}
}

func personaFixture(t testing.TB) (PersonaProfile, *personaSkillCatalog) {
	t.Helper()
	readPin := agentskills.SkillPin{ID: "skill.people.read", Version: 1, Digest: "digest-read"}
	draftPin := agentskills.SkillPin{ID: "skill.workflow.draft", Version: 2, Digest: "digest-draft"}
	catalog := &personaSkillCatalog{
		records: map[agentskills.SkillKey]agentskills.SkillRecord{
			readPin.Key():  {Definition: agentskills.SkillDefinition{ID: readPin.ID, Version: readPin.Version, SideEffectTier: agentskills.TierRead, DataClassesRead: []string{"WORKFORCE"}}, Digest: readPin.Digest, Status: agentskills.StatusActive},
			draftPin.Key(): {Definition: agentskills.SkillDefinition{ID: draftPin.ID, Version: draftPin.Version, SideEffectTier: agentskills.TierPrivateDraft, DataClassesRead: []string{"WORKFORCE"}, DataClassesWritten: []string{"WORKFLOW"}}, Digest: draftPin.Digest, Status: agentskills.StatusActive},
		},
		grants: map[agentskills.SkillKey]SkillAudienceGrant{
			readPin.Key():  {Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}},
			draftPin.Key(): {Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}},
		},
		errors: make(map[agentskills.SkillKey]error),
	}
	manifest := personaManifest()
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatalf("persona manifest digest: %v", err)
	}
	profile := PersonaProfile{
		Manifest:  AgentManifestRef{ID: manifest.ID, Version: uint32(manifest.Version), Digest: manifestDigest, SchemaVersion: manifest.SchemaVersion},
		PersonaID: "persona.onboarding", Version: 1, Handle: "onboarding", DisplayName: "Onboarding Coordinator", AvatarRef: "avatar:onboarding",
		Purpose:   "Answer onboarding questions and prepare private drafts.",
		Audience:  Audience{Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}},
		SkillPins: []agentskills.SkillPin{readPin, draftPin}, TierCeiling: agentskills.TierPrivateDraft,
		ConversationKinds: []ConversationKind{ConversationDirect, ConversationThread}, ChannelClasses: []ChannelClass{ChannelPrivate},
		Instructions: "Answer from governed records and ask for clarification when needed.", Owner: "owner-1", Steward: "steward-1", EvalSuiteRef: "eval:persona-onboarding",
		EvalLimits: EvaluationLimits{MaxCost: 20, MaxSteps: 10, MaxLatencyMS: 2000},
	}
	return profile, catalog
}

func personaValidator(catalog *personaSkillCatalog) Validator {
	return Validator{Skills: catalog, Grants: catalog, Manifests: ManifestCompatibilityAdapter{Resolver: personaManifestResolver{manifest: personaManifest()}}}
}

func mustPersonaVersion(t *testing.T, profile PersonaProfile, validator Validator) PersonaVersion {
	t.Helper()
	version, err := validator.Build(profile)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return version
}

func mustReview(t *testing.T, manager *Manager, version PersonaVersion) ReviewRecord {
	t.Helper()
	review, err := manager.Review(version.Profile.PersonaID, version.Profile.Version, ReviewRequest{Reviewer: "steward-1", Permission: PermissionPersonaReview, Decision: "APPROVE"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	return review
}

func evaluationFor(version PersonaVersion) EvaluationRecord {
	return EvaluationRecord{ProfileDigest: version.Digest, SuiteRef: version.Profile.EvalSuiteRef, RunDigest: "eval-run-1", Passed: true, Fresh: true}
}

// TestTodo_AGENTP_003 proves that a profile is a sealed manifest extension,
// not a display name with run-time-selected skills.
func TestTodo_AGENTP_003(t *testing.T) {
	profile, catalog := personaFixture(t)
	version := mustPersonaVersion(t, profile, personaValidator(catalog))
	if version.Digest == "" || version.Profile.InstructionsDigest == "" {
		t.Fatal("profile was not sealed")
	}
	if got, want := version.DerivedDataClassesRead, []string{"WORKFORCE"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("derived read classes = %v, want %v", got, want)
	}
	if got, want := version.DerivedDataClassesWritten, []string{"WORKFLOW"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("derived write classes = %v, want %v", got, want)
	}
	if version.Profile.Manifest.ID != "agent.onboarding" || len(version.Profile.SkillPins) != 2 || version.Profile.TierCeiling != agentskills.TierPrivateDraft {
		t.Fatalf("manifest extension lost pins or ceiling: %+v", version.Profile)
	}
	version.Profile.SkillPins[0].Digest = "tampered"
	current := mustPersonaVersion(t, profile, personaValidator(catalog))
	if current.Digest != version.Digest {
		t.Fatal("caller mutation changed the separately sealed profile")
	}
}

// TestTodo_AGENTP_003_Golden pins the canonical wire shape consumed by the
// manifest and review stores.
func TestTodo_AGENTP_003_Golden(t *testing.T) {
	profile, catalog := personaFixture(t)
	version := mustPersonaVersion(t, profile, personaValidator(catalog))
	encoded, err := json.Marshal(version.Profile)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"manifest":{"id":"agent.onboarding","version":4,"digest":"` + profile.Manifest.Digest + `","schema_version":1},"persona_id":"persona.onboarding","version":1,"handle":"onboarding","display_name":"Onboarding Coordinator","avatar_ref":"avatar:onboarding","purpose":"Answer onboarding questions and prepare private drafts.","audience":{"roles":["manager"],"populations":["employees"],"organization_scopes":["org-west"]},"skill_pins":[{"id":"skill.people.read","version":1,"digest":"digest-read"},{"id":"skill.workflow.draft","version":2,"digest":"digest-draft"}],"tier_ceiling":1,"conversation_kinds":["DIRECT","THREAD"],"channel_classes":["PRIVATE"],"instructions":"Answer from governed records and ask for clarification when needed.","instructions_digest":"` + version.Profile.InstructionsDigest + `","owner":"owner-1","steward":"steward-1","eval_suite_ref":"eval:persona-onboarding","eval_limits":{"max_cost":20,"max_steps":10,"max_latency_ms":2000},"data_classes_read":["WORKFORCE"],"data_classes_written":["WORKFLOW"]}`
	if string(encoded) != want {
		t.Fatalf("profile JSON = %s, want %s", encoded, want)
	}
}

// TestTodo_AGENTP_003_Security proves that authority cannot be enlarged by a
// ceiling, a broad audience, stale pins, or instruction text.
func TestTodo_AGENTP_003_Security(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	tooHigh := profile
	tooHigh.TierCeiling = agentskills.TierT4
	if _, err := validator.Build(tooHigh); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("T4 ceiling error = %v", err)
	}
	wide := profile
	wide.Audience.Roles = []string{"manager", "administrator"}
	if _, err := validator.Build(wide); !errors.Is(err, ErrAudience) {
		t.Fatalf("wide audience error = %v", err)
	}
	badInstructions := profile
	badInstructions.Instructions = "Use the payroll tool and send results to payroll@example.com."
	if _, err := validator.Build(badInstructions); !errors.Is(err, ErrInstructionReference) {
		t.Fatalf("instruction error = %v", err)
	}
	declaredLower := profile
	declaredLower.DataClassesRead = []string{"WORKFORCE"}
	declaredLower.DataClassesWritten = []string{"OTHER"}
	if _, err := validator.Build(declaredLower); !errors.Is(err, ErrAudience) {
		t.Fatalf("lower data reach error = %v", err)
	}
	retired := profile
	catalog.records[retired.SkillPins[0].Key()] = agentskills.SkillRecord{Definition: catalog.records[retired.SkillPins[0].Key()].Definition, Digest: retired.SkillPins[0].Digest, Status: agentskills.StatusRetired}
	if _, err := validator.Build(retired); !errors.Is(err, ErrRetiredSkill) {
		t.Fatalf("retired skill error = %v", err)
	}
}

// TestTodo_AGENTP_003_Property proves effective data reach is the union of
// every pinned skill, independent of pin order.
func TestTodo_AGENTP_003_Property(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	first := mustPersonaVersion(t, profile, validator)
	profile.SkillPins[0], profile.SkillPins[1] = profile.SkillPins[1], profile.SkillPins[0]
	second := mustPersonaVersion(t, profile, validator)
	if fmt.Sprint(first.DerivedDataClassesRead) != "[WORKFORCE]" || fmt.Sprint(first.DerivedDataClassesWritten) != "[WORKFLOW]" {
		t.Fatalf("unexpected union: %+v", first)
	}
	if first.Digest != second.Digest {
		t.Fatalf("pin order changed canonical profile digest: %s != %s", first.Digest, second.Digest)
	}
}

func managerFixture(t *testing.T) (*Manager, PersonaVersion, *personaEvaluationStore, *personaSkillCatalog) {
	t.Helper()
	profile, catalog := personaFixture(t)
	version := mustPersonaVersion(t, profile, personaValidator(catalog))
	evaluations := &personaEvaluationStore{record: evaluationFor(version)}
	manager := NewManager(Dependencies{Validator: personaValidator(catalog), Evaluations: evaluations, Reviews: personaReviewAuthority{}})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	return manager, version, evaluations, catalog
}

// TestTodo_AGENTP_006 proves publication requires exact independent review
// and passing evaluation evidence, then pins both digests.
func TestTodo_AGENTP_006(t *testing.T) {
	manager, version, evaluations, _ := managerFixture(t)
	review := mustReview(t, manager, version)
	publication, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publication.ProfileDigest != version.Digest || publication.ReviewDigest != review.Digest || publication.EvaluationDigest != evaluations.record.RunDigest || publication.Reviewer != "steward-1" {
		t.Fatalf("publication did not pin evidence: %+v", publication)
	}
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest}); !errors.Is(err, ErrImmutable) {
		t.Fatalf("republish error = %v", err)
	}
}

// TestTodo_AGENTP_006_Golden pins the publication evidence shape.
func TestTodo_AGENTP_006_Golden(t *testing.T) {
	manager, version, evaluations, _ := managerFixture(t)
	review := mustReview(t, manager, version)
	publication, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest})
	if err != nil {
		t.Fatal(err)
	}
	if publication.Generation != 1 || publication.Reviewer != "steward-1" || publication.ProfileDigest != version.Digest {
		t.Fatalf("unexpected publication evidence: %+v", publication)
	}
}

// TestTodo_AGENTP_006_Security proves self-review, tampered review, failed
// evaluation, and retired rollback evidence are refused.
func TestTodo_AGENTP_006_Security(t *testing.T) {
	manager, version, evaluations, catalog := managerFixture(t)
	if _, err := manager.Review(version.Profile.PersonaID, version.Profile.Version, ReviewRequest{Reviewer: version.Profile.Owner, Permission: PermissionPersonaReview, Decision: "APPROVE"}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("self-review error = %v", err)
	}
	review := mustReview(t, manager, version)
	tampered := review
	tampered.Reviewer = "attacker"
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: tampered, EvaluationDigest: evaluations.record.RunDigest}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("tampered review error = %v", err)
	}
	evaluations.record.Passed = false
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest}); !errors.Is(err, ErrEvaluation) {
		t.Fatalf("failed evaluation error = %v", err)
	}
	evaluations.record.Passed = true
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest}); err != nil {
		t.Fatal(err)
	}
	catalog.records[version.Profile.SkillPins[0].Key()] = agentskills.SkillRecord{Definition: catalog.records[version.Profile.SkillPins[0].Key()].Definition, Digest: version.Profile.SkillPins[0].Digest, Status: agentskills.StatusRetired}
	if _, err := manager.Rollback(version.Profile.PersonaID, version.Profile.Version); !errors.Is(err, ErrRetiredSkill) {
		t.Fatalf("retired rollback error = %v", err)
	}
}

// TestTodo_AGENTP_006_Race proves the lifecycle pointer cannot be half
// published when suspension races publication.
func TestTodo_AGENTP_006_Race(t *testing.T) {
	manager, version, evaluations, _ := managerFixture(t)
	review := mustReview(t, manager, version)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _ = manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest})
	}()
	go func() { defer wg.Done(); <-start; _ = manager.Suspend(version.Profile.PersonaID) }()
	close(start)
	wg.Wait()
	_, state, err := manager.Current(version.Profile.PersonaID)
	if err != nil || (state != StatePublished && state != StateSuspended) {
		t.Fatalf("raced lifecycle state = %q err=%v", state, err)
	}
}

// TestTodo_AGENTP_006_Fault proves an evaluation-store failure leaves the
// version in review and creates no publication pointer.
func TestTodo_AGENTP_006_Fault(t *testing.T) {
	manager, version, evaluations, _ := managerFixture(t)
	review := mustReview(t, manager, version)
	evaluations.failure = errors.New("evaluation store unavailable")
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest}); !errors.Is(err, ErrEvaluation) {
		t.Fatalf("fault error = %v", err)
	}
	_, state, err := manager.Current(version.Profile.PersonaID)
	if err != nil || state != StateInReview {
		t.Fatalf("fault changed lifecycle state to %q err=%v", state, err)
	}
}

// TestTodo_AGENTP_006_Recovery proves a retry after the failed store is
// atomic: the same reviewed version can publish once evidence is available.
func TestTodo_AGENTP_006_Recovery(t *testing.T) {
	manager, version, evaluations, _ := managerFixture(t)
	review := mustReview(t, manager, version)
	evaluations.failure = errors.New("restart")
	if _, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest}); err == nil {
		t.Fatal("faulted publish unexpectedly succeeded")
	}
	evaluations.failure = nil
	publication, err := manager.Publish(version.Profile.PersonaID, version.Profile.Version, PublishRequest{Review: review, EvaluationDigest: evaluations.record.RunDigest})
	if err != nil || publication.Generation != 1 {
		t.Fatalf("recovered publication = %+v err=%v", publication, err)
	}
}
