package app

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_WFPAGE_002(t *testing.T) {
	candidates := []WorkflowStartCandidate{
		{WorkflowID: "workflow.new-hire", Version: 2, Name: "New hire", Status: "ACTIVE", Category: "People"},
		{WorkflowID: "workflow.payroll", Version: 1, Name: "Payroll run", Status: "ACTIVE", Category: "Payroll"},
		{WorkflowID: "workflow.hidden", Version: 1, Name: "Hidden workflow", Status: "ACTIVE", Hidden: true},
		{WorkflowID: "workflow.retired", Version: 1, Name: "Retired", Status: "RETIRED"},
	}
	got := BuildWorkflowStartCatalog(context.Background(), "tenant-a", candidates, func(_ context.Context, _ values.TenantId, candidate WorkflowStartCandidate) WorkflowStartDecision {
		if candidate.WorkflowID == "workflow.payroll" {
			return WorkflowStartDecision{Availability: WorkflowStartMissingPrerequisite}
		}
		return WorkflowStartDecision{Availability: WorkflowStartAvailable}
	})
	if len(got) != 2 || got[0].WorkflowID != "workflow.new-hire" || got[1].Availability != WorkflowStartMissingPrerequisite {
		t.Fatalf("catalog = %+v, want two visible active entries with safe availability", got)
	}
	for _, entry := range got {
		if entry.WorkflowID == "workflow.hidden" || entry.WorkflowID == "workflow.retired" {
			t.Fatalf("non-discoverable entry leaked: %+v", entry)
		}
	}
}

func TestTodo_WFPAGE_002_Integration(t *testing.T) {
	got := BuildWorkflowStartCatalog(context.Background(), "tenant-integration", []WorkflowStartCandidate{{WorkflowID: "quarantined", Version: 1, Name: "Quarantined", Status: "ACTIVE", Quarantined: true}}, nil)
	if len(got) != 1 || got[0].Availability != WorkflowStartQuarantined {
		t.Fatalf("quarantined projection = %+v, want safe quarantined reason", got)
	}
}

func TestTodo_WFPAGE_002_LatestActiveVersion(t *testing.T) {
	candidates := []WorkflowStartCandidate{
		{WorkflowID: "workflow.hire", Version: 1, SemanticVersion: "1.0.0", Name: "  Hire  ", Status: " active "},
		{WorkflowID: "workflow.hire", Version: 2, SemanticVersion: "2.0.0", Name: "Hire", Status: "ACTIVE"},
	}
	got := BuildWorkflowStartCatalog(context.Background(), "tenant-a", candidates, func(_ context.Context, _ values.TenantId, candidate WorkflowStartCandidate) WorkflowStartDecision {
		if candidate.Version != 2 || candidate.Name != "Hire" || candidate.Status != "ACTIVE" {
			t.Fatalf("authority received %+v, want normalized latest version", candidate)
		}
		return WorkflowStartDecision{Availability: WorkflowStartAvailable}
	})
	if len(got) != 1 || got[0].Version != 2 || got[0].SemanticVersion != "2.0.0" {
		t.Fatalf("catalog = %+v, want latest active version only", got)
	}
}

func TestTodo_WFPAGE_002_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	got := BuildWorkflowStartCatalog(ctx, "tenant-a", []WorkflowStartCandidate{{WorkflowID: "workflow.hire", Version: 1, Name: "Hire", Status: "ACTIVE"}}, func(context.Context, values.TenantId, WorkflowStartCandidate) WorkflowStartDecision {
		called = true
		return WorkflowStartDecision{Availability: WorkflowStartAvailable}
	})
	if got != nil || called {
		t.Fatalf("catalog = %+v, authority called = %t; want cancelled read to fail closed", got, called)
	}
}
