package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func agentux065Publish(t *testing.T, s *TenantStore, tenant values.TenantId, persona string, n int64, label string) {
	t.Helper()
	agentux065PublishProfile(t, s, tenant, persona, n, label, "")
}

func agentux065PublishProfile(t *testing.T, s *TenantStore, tenant values.TenantId, persona string, n int64, label string, profile string) {
	t.Helper()
	v := version(tenant, persona, n)
	if profile != "" {
		v.Profile = jsonBytes(profile)
	}
	if n == 1 {
		owners, stewards := draftOwners(v)
		if err := s.CreateDraft(context.Background(), v, owners, stewards, "admin", v.CreatedAt); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := s.PutVersion(context.Background(), v); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, persona, n, fmt.Sprintf("draft-%s-%d", persona, n), "", StateDraft)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, persona, n, fmt.Sprintf("review-%s-%d", persona, n), StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	event := lifecycle(tenant, persona, n, fmt.Sprintf("publish-%s-%d", persona, n), StateInReview, StatePublished)
	event.SemanticVersion = label
	if err := s.Publish(context.Background(), event, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); err != nil {
		t.Fatal(err)
	}
}

// A publication seals its semantic label on the publication event: the first
// publication is 1.0.0, later ones advance from the author's computed label,
// and a publish that carries no label gets a patch over the greatest
// published one (AGENTUX-065).
func TestAgentUX065_PublishSealsLabel(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	agentux065Publish(t, s, tenant, "assistant", 1, "1.0.0")
	agentux065Publish(t, s, tenant, "assistant", 2, "1.1.0")
	agentux065Publish(t, s, tenant, "assistant", 3, "")
	if got, err := s.PublishedLabel(context.Background(), "assistant", 1); err != nil || got != "1.0.0" {
		t.Fatalf("v1 label = %q, %v", got, err)
	}
	if got, err := s.PublishedLabel(context.Background(), "assistant", 2); err != nil || got != "1.1.0" {
		t.Fatalf("v2 label = %q, %v", got, err)
	}
	// The label-less publish defaulted to a patch over the greatest published.
	if got, err := s.PublishedLabel(context.Background(), "assistant", 3); err != nil || got != "1.1.1" {
		t.Fatalf("v3 label = %q, %v", got, err)
	}
	if got, err := s.GreatestPublishedLabel(context.Background(), "assistant"); err != nil || got != "1.1.1" {
		t.Fatalf("greatest label = %q, %v", got, err)
	}
	// Republishing a sealed version keeps its label even when the event
	// carries none.
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 1, "suspend-assistant", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	event := lifecycle(tenant, "assistant", 1, "republish-assistant", StateSuspended, StatePublished)
	if err := s.Publish(context.Background(), event, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); err != nil {
		t.Fatalf("a rollback republish was refused: %v", err)
	}
	if got, err := s.PublishedLabel(context.Background(), "assistant", 1); err != nil || got != "1.0.0" {
		t.Fatalf("v1 label after republish = %q, %v", got, err)
	}
}

// A sealed label is never changed and a new one always advances: relabels,
// unparsable labels and backwards labels are refused (AGENTUX-065).
func TestAgentUX065_PublishedLabelImmutable(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	agentux065Publish(t, s, tenant, "assistant", 1, "1.0.0")
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 1, "suspend-assistant", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	relabel := lifecycle(tenant, "assistant", 1, "relabel-assistant", StateSuspended, StatePublished)
	relabel.SemanticVersion = "2.0.0"
	if err := s.Publish(context.Background(), relabel, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); !errors.Is(err, ErrVersionLabel) {
		t.Fatalf("a relabel was accepted: %v", err)
	}
	v := version(tenant, "assistant", 2)
	if err := s.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 2, "draft-assistant-2", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 2, "review-2", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	backwards := lifecycle(tenant, "assistant", 2, "backwards-assistant", StateInReview, StatePublished)
	backwards.SemanticVersion = "1.0.0"
	if err := s.Publish(context.Background(), backwards, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); !errors.Is(err, ErrVersionLabel) {
		t.Fatalf("a backwards label was accepted: %v", err)
	}
	bad := lifecycle(tenant, "assistant", 2, "badlabel-assistant", StateInReview, StatePublished)
	bad.SemanticVersion = "yesterday"
	if err := s.Publish(context.Background(), bad, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); !errors.Is(err, ErrVersionLabel) {
		t.Fatalf("an unparsable label was accepted: %v", err)
	}
}

// The server refuses a label smaller than the change requires: new skills
// need at least a minor, so a patch label is refused and an empty one
// computes the minor (AGENTUX-065).
func TestAgentUX065_PublishRefusesSmallerThanRequired(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	oneSkill := `{"owner":"user:business-owner","skill_pins":[{"id":"policy.search","version":1,"digest":"a"}]}`
	twoSkills := `{"owner":"user:business-owner","skill_pins":[{"id":"policy.search","version":1,"digest":"a"},{"id":"tasks.write","version":1,"digest":"b"}]}`
	agentux065PublishProfile(t, s, tenant, "assistant", 1, "1.0.0", oneSkill)
	v := version(tenant, "assistant", 2)
	v.Profile = jsonBytes(twoSkills)
	if err := s.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 2, "draft-assistant-2", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(tenant, "assistant", 2, "review-assistant-2", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	small := lifecycle(tenant, "assistant", 2, "small-assistant-2", StateInReview, StatePublished)
	small.SemanticVersion = "1.0.1"
	if err := s.Publish(context.Background(), small, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); !errors.Is(err, ErrVersionLabel) {
		t.Fatalf("a patch label for new skills was accepted: %v", err)
	}
	auto := lifecycle(tenant, "assistant", 2, "auto-assistant-2", StateInReview, StatePublished)
	if err := s.Publish(context.Background(), auto, PublicationEvidence{ReviewID: "test-review", EvaluationRunID: "test-run"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PublishedLabel(context.Background(), "assistant", 2); err != nil || got != "1.1.0" {
		t.Fatalf("PublishedLabel(assistant, 2) = %q, %v; want 1.1.0", got, err)
	}
}
