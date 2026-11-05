package studio

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	agentcharter "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/charter"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/publication"
)

// TestTodo_AGENT_012_Integration wires the Studio service to the real
// publication service, the real charter resolver, and real manifest
// validation: the full draft-to-publish flow pins one exact manifest,
// evaluation, and review digest that admission honors.
func TestTodo_AGENT_012_Integration(t *testing.T) {
	ctx := context.Background()
	drafts := &memoryDrafts{}
	sandbox := &stubSandbox{}
	manifests := &memoryManifests{}
	reviewers := stubReviewers{allowed: map[string]bool{"reviewer-1": true}}
	repo := newIntegrationPubRepo()
	eligibility := studioEligibility{}
	publisher, err := publication.New(publication.Dependencies{
		Manifests:   manifests,
		Reviews:     reviewers,
		Evaluations: sandbox,
		Eligibility: eligibility,
		Quarantine:  studioQuarantine{},
		Repository:  repo,
	})
	if err != nil {
		t.Fatalf("publication.New: %v", err)
	}
	service, err := New(Dependencies{
		Drafts:    drafts,
		Charters:  studioCharterAdapter{input: studioTestLayers()},
		Sandbox:   sandbox,
		Reviewers: reviewers,
		Manifests: manifests,
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := service.CreateDraft(ctx, "tenant-1", "agent-1", "owner-1", "Policy Guide", "Answer policy questions.", testDigest("instructions")); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	steps := []struct {
		step   Step
		update StepUpdate
	}{
		{StepJob, StepUpdate{Job: &JobSelection{DisplayName: "Policy Guide", Purpose: "Answer policy questions.", InstructionsDigest: testDigest("instructions")}}},
		{StepCharter, StepUpdate{Charter: &CharterSelection{OrganizationIDs: []string{"org-1"}, Jurisdictions: []string{"us-ca"}, DataCategories: []string{"public"}, EscalationOwner: "owner-1", SuccessMeasures: "CITED_ANSWERS"}}},
		{StepContext, StepUpdate{Context: &ContextSelection{SourceIDs: []string{"src.handbook"}, ChatSpaceIDs: []string{"chat.general"}, Audience: "employees", Purpose: "policy-answers"}}},
		{StepTools, StepUpdate{Tools: &ToolSelection{CapabilityIDs: []string{"tool.docsearch", "tool.lookup"}}}},
		{StepTriggers, StepUpdate{Triggers: &TriggerSelection{Modes: []string{"DM", "MENTION"}, ChannelIDs: []string{"chat.general"}, ReplyVisibility: "public"}}},
		{StepBudgets, StepUpdate{Budgets: &BudgetSelection{MaxCostMicros: 1000, MaxInputTokens: 8000, MaxOutputTokens: 2000, MaxConcurrentRuns: 2, AutonomyCeiling: "ASSISTED", ModelPolicyID: "model.standard", ModelPolicyVersion: 3}}},
	}
	revision := uint64(1)
	for _, step := range steps {
		draft, err := service.SaveStep(ctx, "tenant-1", "agent-1", revision, step.step, step.update)
		if err != nil {
			t.Fatalf("SaveStep(%s): %v", step.step, err)
		}
		revision = draft.Revision
	}

	// The preview resolves through the real charter package: denies win and
	// every item carries its explanation.
	preview, err := service.Preview(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !previewHas(preview.Tools, "tool.docsearch", true) || !previewHas(preview.Tools, "tool.lookup", true) || !previewHas(preview.Sources, "src.handbook", true) {
		t.Fatalf("real-charter preview is missing an allowed item: %+v %+v", preview.Tools, preview.Sources)
	}

	manifest, err := service.BuildManifest(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	evalDigest := testDigest("eval:" + digest)
	sandbox.put(publication.Evaluation{Digest: evalDigest, ManifestDigest: digest, Passed: true, Fresh: true})

	validation, err := service.ValidateDraft(ctx, "tenant-1", "agent-1", revision, evalDigest)
	if err != nil {
		t.Fatalf("ValidateDraft: %v", err)
	}
	draft, err := service.SubmitForReview(ctx, "tenant-1", "agent-1", validation.Draft.Revision, digest)
	if err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	review, err := service.RecordReview(ctx, "tenant-1", "agent-1", "reviewer-1", draft.Revision, digest)
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	published, err := service.Publish(ctx, "tenant-1", "agent-1", draft.Revision+1, digest, 0)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	admitted, err := publisher.Admit(ctx, "tenant-1", "agent-1", digest)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if admitted.ManifestDigest != published.ManifestDigest || admitted.EvaluationDigest != evalDigest || admitted.ReviewDigest != review.Digest || admitted.Generation != 1 {
		t.Fatalf("Admit = %+v, want the exact pinned evidence", admitted)
	}
	status, err := service.Status(ctx, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StatePublished || !status.Publication.Present || status.Sandbox.EvaluationDigest != evalDigest || !status.Sandbox.Passed || !status.Sandbox.Fresh {
		t.Fatalf("Status = %+v, want published sandbox evidence", status)
	}
}

type studioCharterAdapter struct {
	input agentcharter.Input
}

func (s studioCharterAdapter) ResolveLayers(_ context.Context, _ string, _ Draft) (agentcharter.Input, error) {
	return s.input, nil
}

type studioEligibility struct{}

func (studioEligibility) ValidateAgentVersion(_ context.Context, manifest agentmanifest.Manifest) error {
	return manifest.Validate()
}

type studioQuarantine struct{}

func (studioQuarantine) FenceAgentWrites(_ context.Context, _, _, _ string) error { return nil }

type integrationPubRepo struct {
	reviews      map[string]publication.Review
	publications map[uint64]publication.Publication
	byDigest     map[string]publication.Publication
	quarantined  map[string]bool
	generation   uint64
}

func newIntegrationPubRepo() *integrationPubRepo {
	return &integrationPubRepo{
		reviews:      map[string]publication.Review{},
		publications: map[uint64]publication.Publication{},
		byDigest:     map[string]publication.Publication{},
		quarantined:  map[string]bool{},
	}
}

func (r *integrationPubRepo) StoreReview(_ context.Context, review publication.Review) error {
	r.reviews[review.Digest] = review
	return nil
}

func (r *integrationPubRepo) ResolveReview(_ context.Context, _, _, digest string) (publication.Review, error) {
	review, ok := r.reviews[digest]
	if !ok {
		return publication.Review{}, errors.New("review not found")
	}
	return review, nil
}

func (r *integrationPubRepo) ResolvePublication(_ context.Context, _, _ string, version uint64) (publication.Publication, error) {
	out, ok := r.publications[version]
	if !ok {
		return publication.Publication{}, errors.New("publication not found")
	}
	return out, nil
}

func (r *integrationPubRepo) Publish(_ context.Context, expectedGeneration uint64, in publication.Publication) (publication.Publication, error) {
	if expectedGeneration != r.generation {
		return publication.Publication{}, publication.ErrConflict
	}
	if r.quarantined[in.ManifestDigest] {
		return publication.Publication{}, publication.ErrQuarantined
	}
	r.generation++
	in.Generation = r.generation
	r.publications[in.ManifestVersion] = in
	r.byDigest[in.ManifestDigest] = in
	return in, nil
}

func (r *integrationPubRepo) Quarantine(_ context.Context, _, _, digest, _ string) error {
	if _, ok := r.byDigest[digest]; !ok {
		return errors.New("digest was never published")
	}
	r.quarantined[digest] = true
	return nil
}

func (r *integrationPubRepo) Admit(_ context.Context, tenantID, agentID, digest string) (publication.Publication, error) {
	if r.quarantined[digest] {
		return publication.Publication{}, publication.ErrQuarantined
	}
	out, ok := r.byDigest[digest]
	if !ok || out.TenantID != tenantID || out.AgentID != agentID {
		return publication.Publication{}, publication.ErrNotFound
	}
	return out, nil
}
