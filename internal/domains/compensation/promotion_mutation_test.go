//go:build !(js && wasm)

package compensation

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func compensationMutationFixture(t *testing.T) PromotionMutation {
	t.Helper()
	tenant := values.TenantId("11111111-1111-4111-8111-111111111111")
	key, err := values.NewResourceKey(tenant, values.Kind("compensation_component"), "worker-1", "base-pay-1")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("rewards.compensation/worker-1", 9)
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
	oldPay, err := values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	newPay, err := values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return PromotionMutation{Tenant: tenant, ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal", ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:1",
		WorkerID: "worker-1", PackageID: "package-1", ComponentID: "base-pay-1", ResourceKey: key,
		Subject: intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "compensation"}, Effective: interval,
		ExpectedRevision: revision, CurrentBasePay: oldPay, TargetBasePay: newPay}
}

func TestTodo_COMP_004(t *testing.T) {
	writes, err := compensationMutationFixture(t).PlannedWrites()
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0].FieldPath != BasePayField || writes[0].ProposedCanonicalText != "180000.00 USD" {
		t.Fatalf("writes = %+v", writes)
	}
}

func TestTodo_COMP_004_Property(t *testing.T) {
	m := compensationMutationFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := m.Explain(); got == "" {
		t.Fatal("missing explanation")
	}
}

func TestTodo_COMP_004_Mutation(t *testing.T) {
	m := compensationMutationFixture(t)
	other, err := values.NewMoney("180000.00", "EUR", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	m.TargetBasePay = other
	if !errors.Is(m.Validate(), ErrPromotionCompensationCurrency) {
		t.Fatalf("Validate = %v", m.Validate())
	}
	m = compensationMutationFixture(t)
	m.TargetBasePay = m.CurrentBasePay
	if !errors.Is(m.Validate(), ErrPromotionCompensationNoChange) {
		t.Fatalf("Validate = %v", m.Validate())
	}
}
