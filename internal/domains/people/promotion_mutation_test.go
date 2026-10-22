//go:build !(js && wasm)

package people

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func promotionMutationFixture(t *testing.T) PromotionMutation {
	t.Helper()
	tenant := values.TenantId("11111111-1111-4111-8111-111111111111")
	key, err := values.NewResourceKey(tenant, values.Kind("assignment"), "worker-1", "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("people.assignment/worker-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	startTime, err := time.Parse(time.RFC3339, "2026-10-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	start := values.NewInstant(startTime)
	endTime, err := time.Parse(time.RFC3339, "2027-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	end := values.NewInstant(endTime)
	interval, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return PromotionMutation{Tenant: tenant, ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal",
		ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:1", WorkerID: "worker-1", AssignmentID: "assignment-1",
		ResourceKey: key, Subject: intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "people"},
		Effective: interval, ExpectedRevision: revision, CurrentJobCode: "ENG-3", TargetJobCode: "ENG-MGR1", CurrentLevel: "P3", TargetLevel: "M1"}
}

func TestTodo_PEOPLE_004(t *testing.T) {
	m := promotionMutationFixture(t)
	writes, err := m.PlannedWrites()
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0].FieldPath != AssignmentJobField || writes[1].FieldPath != AssignmentLevelField {
		t.Fatalf("writes = %+v", writes)
	}
	if got := m.Explain(); got == "" {
		t.Fatal("missing explanation")
	}
}

func TestTodo_PEOPLE_004_Golden(t *testing.T) {
	m := promotionMutationFixture(t)
	writes, err := m.PlannedWrites()
	if err != nil {
		t.Fatal(err)
	}
	if writes[0].CurrentCanonicalText != "ENG-3" || writes[0].ProposedCanonicalText != "ENG-MGR1" || writes[1].ProposedCanonicalText != "M1" {
		t.Fatalf("write projection lost exact values: %+v", writes)
	}
}

func TestTodo_PEOPLE_004_Mutation(t *testing.T) {
	m := promotionMutationFixture(t)
	m.TargetJobCode, m.TargetLevel = m.CurrentJobCode, m.CurrentLevel
	if !errors.Is(m.Validate(), ErrPromotionNoChange) {
		t.Fatalf("Validate = %v", m.Validate())
	}
	m = promotionMutationFixture(t)
	m.Subject.Kind = "employment"
	if err := m.Validate(); err == nil {
		t.Fatal("accepted a non-worker subject")
	}
}
