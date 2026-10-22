//go:build !(js && wasm)

package org

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func managerMutationFixture(t *testing.T) ManagerMutation {
	t.Helper()
	tenant := values.TenantId("11111111-1111-4111-8111-111111111111")
	key, err := values.NewResourceKey(tenant, values.Kind("assignment"), "worker-1", "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("org.manager_relationship/worker-1", 4)
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
	return ManagerMutation{Tenant: tenant, ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal", ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:1",
		WorkerID: "worker-1", AssignmentID: "assignment-1", ResourceKey: key,
		Subject: intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "organization"}, Effective: interval,
		ExpectedRevision: revision, CurrentRelationshipID: "rel-old", TargetRelationshipID: "rel-new", CurrentManagerID: "manager-old", TargetManagerID: "manager-new"}
}

func TestTodo_ORG_003(t *testing.T) {
	m := managerMutationFixture(t)
	writes, err := m.PlannedWrites()
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0].FieldPath != ManagerRelationshipIDField || writes[1].FieldPath != ManagerWorkerIDField {
		t.Fatalf("writes = %+v", writes)
	}
}

func TestTodo_ORG_003_Property(t *testing.T) {
	m := managerMutationFixture(t)
	if got, err := m.PlannedWrites(); err != nil || len(got) != 2 {
		t.Fatalf("first writes = %v, %v", got, err)
	}
	if got, err := m.PlannedWrites(); err != nil || len(got) != 2 {
		t.Fatalf("second writes = %v, %v", got, err)
	}
}

func TestTodo_ORG_003_Golden(t *testing.T) {
	if got := managerMutationFixture(t).Explain(); got == "" || !strings.Contains(got, "rel-new") {
		t.Fatalf("Explain = %q", got)
	}
}

func TestTodo_ORG_003_Mutation(t *testing.T) {
	m := managerMutationFixture(t)
	m.ManagerAncestors = []string{"manager-1", "worker-1"}
	if !errors.Is(m.Validate(), ErrManagerMutationCycle) {
		t.Fatalf("Validate = %v", m.Validate())
	}
	m = managerMutationFixture(t)
	m.TargetManagerID = m.CurrentManagerID
	m.TargetRelationshipID = m.CurrentRelationshipID
	if !errors.Is(m.Validate(), ErrManagerMutationNoChange) {
		t.Fatalf("Validate = %v", m.Validate())
	}
}
