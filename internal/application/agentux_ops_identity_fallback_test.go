package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXOpsPartialIdentities struct {
	known map[string]AgentControlIdentity
}

func (s agentUXOpsPartialIdentities) ResolveAgentControlIdentity(_ context.Context, _ values.TenantId, agentID, _ string) (AgentControlIdentity, error) {
	if identity, ok := s.known[agentID]; ok {
		return identity, nil
	}
	return AgentControlIdentity{}, agentpersonastore.ErrNotFound
}

// One run whose agent the catalog cannot name used to fail the whole
// "Running agents" region with "Couldn't load running agents." The owner
// must still see every run they are authorized for; the unnamed one carries
// the content-free fallback label.
func TestAgentOwnerControlsListRunsWhoseAgentTheCatalogCannotName(t *testing.T) {
	principal := portableApplicationPrincipal(t)
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	runs := &agentUXOps2RunSource{rows: []AgentOwnerRunProjection{
		{View: ownerops.TaskView{TaskID: "task-1", AgentID: "policy-helper", Version: "4", State: "COMPLETED", Revision: 3}, StartedAt: at, UpdatedAt: at.Add(time.Second)},
		{View: ownerops.TaskView{TaskID: "task-2", AgentID: "general-agent", Version: "1", State: "COMPLETED", Revision: 2}, StartedAt: at, UpdatedAt: at.Add(time.Second)},
	}}
	identities := agentUXOpsPartialIdentities{known: map[string]AgentControlIdentity{"policy-helper": {Name: "Policy Helper", OwnerName: "Walt Brennan"}}}
	reply, err := (&AgentOwnerControls{Runs: runs, Identities: identities}).Snapshot(trust.WithPrincipal(context.Background(), principal))
	if err != nil {
		t.Fatalf("one unnamed agent failed the region: %v", err)
	}
	if len(reply.Snapshot.Runs) != 2 {
		t.Fatalf("runs = %+v", reply.Snapshot.Runs)
	}
	if reply.Snapshot.Runs[0].Name != "Policy Helper" || reply.Snapshot.OwnerName != "Walt Brennan" {
		t.Fatalf("named run = %+v owner=%q", reply.Snapshot.Runs[0], reply.Snapshot.OwnerName)
	}
	if name := reply.Snapshot.Runs[1].Name; name != "General agent" {
		t.Fatalf("unnamed run label = %q, want the platform general-agent label", name)
	}
}

func TestAgentUXR5Ops_FallbackGeneralAgentIdentity(t *testing.T) {
	identity, err := (AgentFallbackControlIdentities{}).ResolveAgentControlIdentity(context.Background(), "tenant-a", "general-agent", "1")
	if err != nil || identity.Name != "General agent" {
		t.Fatalf("general agent fallback = %+v, %v", identity, err)
	}
}
