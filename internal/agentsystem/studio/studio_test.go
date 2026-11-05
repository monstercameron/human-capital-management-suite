package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	agentcharter "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/charter"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/publication"
)

func testDigest(seed string) string {
	sum := sha256.Sum256([]byte("agent-studio-test/v1\x00" + seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type memoryDrafts struct {
	mu     sync.Mutex
	drafts map[string]Draft
}

func draftKey(tenantID, agentID string) string { return tenantID + "\x00" + agentID }

func (m *memoryDrafts) Load(_ context.Context, tenantID, agentID string) (Draft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	draft, ok := m.drafts[draftKey(tenantID, agentID)]
	if !ok {
		return Draft{}, ErrNotFound
	}
	return draft, nil
}

func (m *memoryDrafts) Save(_ context.Context, expectedRevision uint64, draft Draft) (Draft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.drafts == nil {
		m.drafts = map[string]Draft{}
	}
	key := draftKey(draft.TenantID, draft.AgentID)
	current, ok := m.drafts[key]
	if !ok {
		if expectedRevision != 0 {
			return Draft{}, ErrStaleDraft
		}
		m.drafts[key] = draft
		return draft, nil
	}
	if current.Revision != expectedRevision {
		return Draft{}, ErrStaleDraft
	}
	m.drafts[key] = draft
	return draft, nil
}

type stubCharters struct {
	input agentcharter.Input
	err   error
}

func (s stubCharters) ResolveLayers(_ context.Context, _ string, _ Draft) (agentcharter.Input, error) {
	if s.err != nil {
		return agentcharter.Input{}, s.err
	}
	return s.input, nil
}

type stubSandbox struct {
	mu    sync.Mutex
	evals map[string]publication.Evaluation
}

func (s *stubSandbox) ResolveEvaluation(_ context.Context, digest string) (publication.Evaluation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	eval, ok := s.evals[digest]
	if !ok {
		return publication.Evaluation{}, errors.New("unknown evaluation")
	}
	return eval, nil
}

func (s *stubSandbox) put(eval publication.Evaluation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evals == nil {
		s.evals = map[string]publication.Evaluation{}
	}
	s.evals[eval.Digest] = eval
}

type stubReviewers struct {
	allowed map[string]bool
}

func (s stubReviewers) AuthorizeAgentReviewer(_ context.Context, _, _, reviewerID string) error {
	if s.allowed[reviewerID] {
		return nil
	}
	return errors.New("reviewer lacks agents.publish")
}

type memoryManifests struct {
	mu        sync.Mutex
	manifests map[string]agentmanifest.Manifest
}

func (m *memoryManifests) Seal(_ context.Context, manifest agentmanifest.Manifest) error {
	if _, err := manifest.Digest(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.manifests == nil {
		m.manifests = map[string]agentmanifest.Manifest{}
	}
	m.manifests[manifest.ID] = manifest
	return nil
}

func (m *memoryManifests) ResolveManifest(_ context.Context, _, agentID string, version uint64) (agentmanifest.Manifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, ok := m.manifests[agentID]
	if !ok || manifest.Version != version {
		return agentmanifest.Manifest{}, errors.New("manifest not sealed")
	}
	return manifest, nil
}

type stubPublisher struct {
	mu         sync.Mutex
	reviews    map[string]publication.Review
	published  map[string]publication.Publication
	generation uint64
}

func (s *stubPublisher) Review(_ context.Context, tenantID, agentID string, _ uint64, reviewerID, expectedDigest string) (publication.Review, error) {
	review := publication.Review{TenantID: tenantID, AgentID: agentID, ManifestDigest: expectedDigest, ReviewerID: reviewerID, Permission: publication.AgentReviewPermission, Digest: testDigest("review:" + reviewerID + ":" + expectedDigest)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviews == nil {
		s.reviews = map[string]publication.Review{}
	}
	s.reviews[review.Digest] = review
	return review, nil
}

func (s *stubPublisher) Publish(_ context.Context, tenantID, agentID string, version, expectedGeneration uint64, expectedManifestDigest, evaluationDigest, reviewDigest string) (publication.Publication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedGeneration != s.generation {
		return publication.Publication{}, publication.ErrConflict
	}
	s.generation++
	out := publication.Publication{TenantID: tenantID, AgentID: agentID, ManifestVersion: version, ManifestDigest: expectedManifestDigest, EvaluationDigest: evaluationDigest, ReviewDigest: reviewDigest, Generation: s.generation}
	if s.published == nil {
		s.published = map[string]publication.Publication{}
	}
	s.published[expectedManifestDigest] = out
	return out, nil
}

func (s *stubPublisher) Admit(_ context.Context, tenantID, agentID, manifestDigest string) (publication.Publication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, ok := s.published[manifestDigest]
	if !ok || out.TenantID != tenantID || out.AgentID != agentID {
		return publication.Publication{}, publication.ErrNotFound
	}
	return out, nil
}

func studioTestLayers() agentcharter.Input {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	org := agentcharter.OrganizationRef{ID: "org-1", EffectiveFrom: at.Add(-time.Hour), EffectiveUntil: at.Add(24 * time.Hour)}
	layer := func(kind agentcharter.LayerKind, id string, settings agentcharter.Settings) agentcharter.Layer {
		return agentcharter.Layer{Kind: kind, ID: id, Version: 1, EffectiveFrom: at.Add(-time.Hour), EffectiveUntil: at.Add(24 * time.Hour), Settings: settings}
	}
	platform := layer("PLATFORM", "platform", agentcharter.Settings{
		Organizations: agentcharter.Constraint[agentcharter.OrganizationID]{Specified: true, Allow: []agentcharter.OrganizationID{"org-1"}},
		Tools:         agentcharter.Constraint[agentcharter.CapabilityID]{Specified: true, Allow: []agentcharter.CapabilityID{"tool.docsearch", "tool.lookup"}, Deny: []agentcharter.CapabilityID{"tool.rawsql"}},
		Sources:       agentcharter.Constraint[agentcharter.CapabilityID]{Specified: true, Allow: []agentcharter.CapabilityID{"src.handbook"}, Deny: []agentcharter.CapabilityID{"src.payroll"}},
	})
	return agentcharter.Input{
		Context: agentcharter.Context{TenantID: "tenant-1", Organizations: []agentcharter.OrganizationRef{org}, RunID: "studio-preview", EffectiveAt: at},
		Layers: []agentcharter.Layer{
			platform,
			layer("TENANT", "tenant-1", agentcharter.Settings{}),
			layer("ENTITY", "org-1", agentcharter.Settings{}),
			layer("AGENT", "agent-1", agentcharter.Settings{}),
			layer("INSTALLATION", "studio-preview", agentcharter.Settings{}),
			layer("RUN", "studio-preview", agentcharter.Settings{}),
		},
	}
}

type studioFixture struct {
	service *Service
	drafts  *memoryDrafts
	sandbox *stubSandbox
	publish *stubPublisher
	review  stubReviewers
}

func newStudioFixture() *studioFixture {
	f := &studioFixture{
		drafts:  &memoryDrafts{},
		sandbox: &stubSandbox{},
		publish: &stubPublisher{},
		review:  stubReviewers{allowed: map[string]bool{"reviewer-1": true}},
	}
	service, err := New(Dependencies{
		Drafts:    f.drafts,
		Charters:  stubCharters{input: studioTestLayers()},
		Sandbox:   f.sandbox,
		Reviewers: f.review,
		Manifests: &memoryManifests{},
		Publisher: f.publish,
	})
	if err != nil {
		panic(err)
	}
	f.service = service
	return f
}

func mustSaveStep(t *testing.T, f *studioFixture, step Step, update StepUpdate, revision uint64) Draft {
	t.Helper()
	draft, err := f.service.SaveStep(context.Background(), "tenant-1", "agent-1", revision, step, update)
	if err != nil {
		t.Fatalf("SaveStep(%s) revision %d: %v", step, revision, err)
	}
	return draft
}

func fillDraftSteps(t *testing.T, f *studioFixture, tools []string, sources []string) Draft {
	t.Helper()
	draft := mustSaveStep(t, f, StepJob, StepUpdate{Job: &JobSelection{DisplayName: "Policy Guide", Purpose: "Answer policy questions.", InstructionsDigest: testDigest("instructions")}}, 1)
	draft = mustSaveStep(t, f, StepCharter, StepUpdate{Charter: &CharterSelection{OrganizationIDs: []string{"org-1"}, Jurisdictions: []string{"us-ca"}, DataCategories: []string{"public"}, EscalationOwner: "owner-1", SuccessMeasures: "CITED_ANSWERS"}}, draft.Revision)
	draft = mustSaveStep(t, f, StepContext, StepUpdate{Context: &ContextSelection{SourceIDs: sources, ChatSpaceIDs: []string{"chat.general"}, Audience: "employees", Purpose: "policy-answers"}}, draft.Revision)
	draft = mustSaveStep(t, f, StepTools, StepUpdate{Tools: &ToolSelection{CapabilityIDs: tools}}, draft.Revision)
	draft = mustSaveStep(t, f, StepTriggers, StepUpdate{Triggers: &TriggerSelection{Modes: []string{"DM", "MENTION"}, ChannelIDs: []string{"chat.general"}, ReplyVisibility: "public"}}, draft.Revision)
	draft = mustSaveStep(t, f, StepBudgets, StepUpdate{Budgets: &BudgetSelection{MaxCostMicros: 1000, MaxInputTokens: 8000, MaxOutputTokens: 2000, MaxConcurrentRuns: 2, AutonomyCeiling: "ASSISTED", ModelPolicyID: "model.standard", ModelPolicyVersion: 3}}, draft.Revision)
	return draft
}

func bindSandboxPass(t *testing.T, f *studioFixture, digest string) string {
	t.Helper()
	evalDigest := testDigest("eval:" + digest)
	f.sandbox.put(publication.Evaluation{Digest: evalDigest, ManifestDigest: digest, Passed: true, Fresh: true})
	return evalDigest
}

// TestTodo_AGENT_012 walks the governed Studio flow: revision-fenced draft
// steps, a validation that shows the effective scope, reviewer separation, and
// a publication pinned to the exact reviewed digest.
func TestTodo_AGENT_012(t *testing.T) {
	ctx := context.Background()
	f := newStudioFixture()

	draft, err := f.service.CreateDraft(ctx, "tenant-1", "agent-1", "owner-1", "Policy Guide", "Answer policy questions.", testDigest("instructions"))
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if draft.Revision != 1 || draft.State != StateDraft {
		t.Fatalf("CreateDraft = revision %d state %s, want revision 1 state DRAFT", draft.Revision, draft.State)
	}

	draft = fillDraftSteps(t, f, []string{"tool.docsearch"}, []string{"src.handbook", "src.payroll"})

	// A stale writer must not overwrite the current draft.
	if _, err := f.service.SaveStep(ctx, "tenant-1", "agent-1", 2, StepJob, StepUpdate{Job: &JobSelection{DisplayName: "Stale", Purpose: "stale", InstructionsDigest: testDigest("stale")}}); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale SaveStep = %v, want ErrStaleDraft", err)
	}

	// Validation shows the effective scope, including the denied source.
	preview, err := f.service.Preview(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !previewHas(preview.Sources, "src.handbook", true) {
		t.Fatalf("preview hides allowed source src.handbook: %+v", preview.Sources)
	}
	denied := previewItem(preview.Sources, "src.payroll")
	if denied == nil || denied.Allowed || denied.Reason == "" {
		t.Fatalf("preview must explain denied src.payroll: %+v", preview.Sources)
	}

	// The manifest digest is deterministic, so the sandbox result binds exactly.
	manifest, err := f.service.BuildManifest(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	evalDigest := bindSandboxPass(t, f, digest)
	if _, err := f.service.ValidateDraft(ctx, "tenant-1", "agent-1", draft.Revision, evalDigest); err == nil {
		t.Fatal("ValidateDraft accepted a draft that requests denied src.payroll")
	} else if !strings.Contains(err.Error(), "src.payroll") {
		t.Fatalf("ValidateDraft denial does not name the item: %v", err)
	}

	// Remove the denied source and validate again.
	draft, err = f.service.SaveStep(ctx, "tenant-1", "agent-1", draft.Revision, StepContext, StepUpdate{Context: &ContextSelection{SourceIDs: []string{"src.handbook"}, ChatSpaceIDs: []string{"chat.general"}, Audience: "employees", Purpose: "policy-answers"}})
	if err != nil {
		t.Fatalf("SaveStep remove denied source: %v", err)
	}
	manifest, err = f.service.BuildManifest(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	digest, err = manifest.Digest()
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	evalDigest = bindSandboxPass(t, f, digest)
	validation, err := f.service.ValidateDraft(ctx, "tenant-1", "agent-1", draft.Revision, evalDigest)
	if err != nil {
		t.Fatalf("ValidateDraft: %v", err)
	}
	if validation.Draft.State != StateValidated || validation.Digest != digest {
		t.Fatalf("ValidateDraft = state %s digest %s, want VALIDATED %s", validation.Draft.State, validation.Digest, digest)
	}

	draft, err = f.service.SubmitForReview(ctx, "tenant-1", "agent-1", validation.Draft.Revision, digest)
	if err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if draft.State != StateReviewRequired {
		t.Fatalf("SubmitForReview state = %s, want REVIEW_REQUIRED", draft.State)
	}
	if _, err := f.service.SubmitForReview(ctx, "tenant-1", "agent-1", validation.Draft.Revision, digest); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale SubmitForReview = %v, want ErrStaleDraft", err)
	}

	// The owner cannot review their own draft.
	if _, err := f.service.RecordReview(ctx, "tenant-1", "agent-1", "owner-1", draft.Revision, digest); !errors.Is(err, ErrSeparation) {
		t.Fatalf("owner self-review = %v, want ErrSeparation", err)
	}
	review, err := f.service.RecordReview(ctx, "tenant-1", "agent-1", "reviewer-1", draft.Revision, digest)
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if review.ManifestDigest != digest || review.ReviewerID != "reviewer-1" {
		t.Fatalf("RecordReview = %+v, want digest %s reviewer reviewer-1", review, digest)
	}

	// Publish pins the exact reviewed digest and generation.
	staleDigest := testDigest("other-manifest")
	if _, err := f.service.Publish(ctx, "tenant-1", "agent-1", draft.Revision+1, staleDigest, 0); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("stale Publish = %v, want ErrStaleDraft", err)
	}
	publication, err := f.service.Publish(ctx, "tenant-1", "agent-1", draft.Revision+1, digest, 0)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publication.ManifestDigest != digest || publication.EvaluationDigest != evalDigest || publication.ReviewDigest != review.Digest || publication.Generation != 1 {
		t.Fatalf("Publish evidence = %+v, want pinned digests and generation 1", publication)
	}

	status, err := f.service.Status(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StatePublished || status.ManifestDigest != digest || status.ReviewDigest != review.Digest || !status.Publication.Present || status.Publication.Generation != 1 {
		t.Fatalf("Status = %+v, want published evidence with pinned digests", status)
	}
	if status.OwnerID != "owner-1" || len(status.Steps) != 9 {
		t.Fatalf("Status owner/steps = %s/%d, want owner-1/9", status.OwnerID, len(status.Steps))
	}

	// Owner transfer moves accountability and bumps the revision.
	moved, err := f.service.TransferOwner(ctx, "tenant-1", "agent-1", "owner-1", "owner-2", status.Revision)
	if err != nil {
		t.Fatalf("TransferOwner: %v", err)
	}
	if moved.OwnerID != "owner-2" || moved.Revision != status.Revision+1 {
		t.Fatalf("TransferOwner = owner %s revision %d", moved.OwnerID, moved.Revision)
	}
}

// TestTodo_AGENT_012_Security proves tenant isolation, reviewer authorization,
// evaluation binding, and owner-transfer authority fail closed.
func TestTodo_AGENT_012_Security(t *testing.T) {
	ctx := context.Background()
	f := newStudioFixture()

	if _, err := f.service.CreateDraft(ctx, "tenant-1", "agent-1", "owner-1", "Policy Guide", "Answer policy questions.", testDigest("instructions")); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}

	// Cross-tenant reads and writes look exactly like a missing draft.
	if _, err := f.service.Status(ctx, "tenant-2", "agent-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Status = %v, want ErrNotFound", err)
	}
	if _, err := f.service.SaveStep(ctx, "tenant-2", "agent-1", 1, StepJob, StepUpdate{Job: &JobSelection{DisplayName: "x", Purpose: "x", InstructionsDigest: testDigest("x")}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant SaveStep = %v, want ErrNotFound", err)
	}
	if status, err := f.service.Status(ctx, "tenant-1", "agent-1"); err != nil || status.OwnerID != "owner-1" {
		t.Fatalf("tenant-1 draft changed by cross-tenant write: %+v %v", status, err)
	}

	draft := fillDraftSteps(t, f, []string{"tool.docsearch"}, []string{"src.handbook"})
	manifest, err := f.service.BuildManifest(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}

	// Stale, failing, and foreign evaluations never validate.
	foreignDigest := testDigest("foreign-manifest")
	for name, eval := range map[string]publication.Evaluation{
		"stale":   {Digest: testDigest("eval-stale"), ManifestDigest: digest, Passed: true, Fresh: false},
		"failing": {Digest: testDigest("eval-failing"), ManifestDigest: digest, Passed: false, Fresh: true},
		"foreign": {Digest: testDigest("eval-foreign"), ManifestDigest: foreignDigest, Passed: true, Fresh: true},
	} {
		f.sandbox.put(eval)
		if _, err := f.service.ValidateDraft(ctx, "tenant-1", "agent-1", draft.Revision, eval.Digest); !errors.Is(err, ErrEvaluation) {
			t.Fatalf("%s evaluation ValidateDraft = %v, want ErrEvaluation", name, err)
		}
	}

	evalDigest := bindSandboxPass(t, f, digest)
	validation, err := f.service.ValidateDraft(ctx, "tenant-1", "agent-1", draft.Revision, evalDigest)
	if err != nil {
		t.Fatalf("ValidateDraft: %v", err)
	}
	draft, err = f.service.SubmitForReview(ctx, "tenant-1", "agent-1", validation.Draft.Revision, digest)
	if err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}

	// A reviewer without agents.publish cannot record a review.
	if _, err := f.service.RecordReview(ctx, "tenant-1", "agent-1", "intruder", draft.Revision, digest); !errors.Is(err, ErrDenied) {
		t.Fatalf("unauthorized review = %v, want ErrDenied", err)
	}
	review, err := f.service.RecordReview(ctx, "tenant-1", "agent-1", "reviewer-1", draft.Revision, digest)
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}

	// Revoking the reviewer before publish denies the publication.
	f.review.allowed["reviewer-1"] = false
	if _, err := f.service.Publish(ctx, "tenant-1", "agent-1", draft.Revision+1, digest, 0); !errors.Is(err, ErrDenied) {
		t.Fatalf("publish after reviewer revocation = %v, want ErrDenied", err)
	}
	f.review.allowed["reviewer-1"] = true

	// Only the current owner transfers ownership, and the new owner must exist.
	if _, err := f.service.TransferOwner(ctx, "tenant-1", "agent-1", "intruder", "owner-2", draft.Revision+1); !errors.Is(err, ErrDenied) {
		t.Fatalf("intruder TransferOwner = %v, want ErrDenied", err)
	}
	if _, err := f.service.TransferOwner(ctx, "tenant-1", "agent-1", "owner-1", "  ", draft.Revision+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank-owner TransferOwner = %v, want ErrInvalid", err)
	}

	// Transfer during review clears the pending review so the new owner's
	// accountability cannot publish on stale separation evidence.
	moved, err := f.service.TransferOwner(ctx, "tenant-1", "agent-1", "owner-1", "owner-2", draft.Revision+1)
	if err != nil {
		t.Fatalf("TransferOwner: %v", err)
	}
	if moved.State != StateValidated || moved.ReviewDigest != "" {
		t.Fatalf("TransferOwner during review = state %s review %q, want VALIDATED with cleared review", moved.State, moved.ReviewDigest)
	}
	_ = review
}

func previewHas(items []ScopeItem, id string, allowed bool) bool {
	for _, item := range items {
		if item.ID == id && item.Allowed == allowed && item.Reason != "" {
			return true
		}
	}
	return false
}

func previewItem(items []ScopeItem, id string) *ScopeItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
