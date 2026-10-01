package projectstore

import (
	"context"
	"errors"
	"sync"
	"testing"

	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

func TestTodo_AGENT_049_Integration(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	p := ProjectRecord{ID: "agent049-project", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Operations", Timezone: "UTC", Revision: 1}
	if err := s.CreateProject(ctx, p, "owner-a", "HUMAN", "agent049-project"); err != nil {
		t.Fatal(err)
	}
	proposal := projectdomain.AgentTaskProposal{ID: "proposal-a", TenantID: "tenant-a", ProjectID: projectdomain.ProjectID(p.ID), TaskID: "task-a", AgentID: "agent-v1", BaseTaskRevision: 1, Revision: 1, State: projectdomain.AgentProposalProposed}
	if err := s.SaveAgentTaskProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgentTaskProposal(ctx, "tenant-a", p.ID, proposal.ID)
	if err != nil || got.Revision != 1 || got.ProjectID != proposal.ProjectID {
		t.Fatalf("proposal = %+v err=%v", got, err)
	}
	proposal.Revision = 2
	proposal.State = projectdomain.AgentProposalApproved
	if err := s.UpdateAgentTaskProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetAgentTaskProposal(ctx, "tenant-a", p.ID, proposal.ID)
	if err != nil || got.State != projectdomain.AgentProposalApproved {
		t.Fatalf("updated proposal = %+v err=%v", got, err)
	}
}

func TestTodo_AGENT_049_Integration_ConcurrentRevisionCAS(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	p := ProjectRecord{ID: "agent049-cas", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Operations", Timezone: "UTC", Revision: 1}
	if err := s.CreateProject(ctx, p, "owner-a", "HUMAN", "agent049-cas-create"); err != nil {
		t.Fatal(err)
	}
	base := projectdomain.AgentTaskProposal{ID: "proposal-cas", TenantID: "tenant-a", ProjectID: projectdomain.ProjectID(p.ID), TaskID: "task-a", AgentID: "agent-v1", BaseTaskRevision: 1, Revision: 1, State: projectdomain.AgentProposalProposed}
	if err := s.SaveAgentTaskProposal(ctx, base); err != nil {
		t.Fatal(err)
	}
	next := base
	next.Revision = 2
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() { defer wg.Done(); results <- s.UpdateAgentTaskProposal(ctx, next) }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("CAS error = %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("CAS winners = %d, want 1", winners)
	}
}
