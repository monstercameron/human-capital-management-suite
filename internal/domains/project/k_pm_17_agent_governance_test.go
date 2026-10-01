package project

import (
	"errors"
	"testing"
)

func pm17AgentFixture(t *testing.T, review bool) (AgentTaskScope, Project, Task) {
	t.Helper()
	project, err := NewProject("project-1", "tenant-1", "owner-1", "Operations", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTask("task-1", project, "Prepare plan", "planned")
	if err != nil {
		t.Fatal(err)
	}
	return AgentTaskScope{TenantID: "tenant-1", ProjectID: project.ID, AgentID: "agent-1", AllowedTaskIDs: map[TaskID]bool{"task-1": true}, AuthorizedInputIDs: map[string]bool{"input-1": true}, CanPropose: true, CanSummarize: true, RequireHumanReview: review}, project, task
}

func TestTodo_PM_067(t *testing.T) {
	scope, project, task := pm17AgentFixture(t, true)
	title := "Prepare reviewed plan"
	proposal, err := ProposeAgentTaskEdit(scope, task, "proposal-1", AgentTaskEdit{Patch: TaskPatch{Title: &title}}, []string{"input-1"})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.State != AgentProposalProposed || proposal.BaseTaskRevision != task.Revision || task.Title == title {
		t.Fatalf("agent proposal mutated live task or lost revision fence: proposal=%+v task=%+v", proposal, task)
	}
	proposal, err = proposal.ReviewAgentTaskEdit("human-1", "APPROVE", proposal.Revision, scope)
	if err != nil {
		t.Fatal(err)
	}
	proposal, updated, err := proposal.CommitAgentTaskEdit(scope, task, project, "human-1", task.Revision, nil)
	if err != nil || proposal.State != AgentProposalCommitted || updated.Title != title || updated.Revision != task.Revision+1 {
		t.Fatalf("reviewed project edit = proposal=%+v task=%+v err=%v", proposal, updated, err)
	}
}

func TestTodo_PM_067_Security(t *testing.T) {
	scope, _, task := pm17AgentFixture(t, true)
	title := "Unauthorized"
	outside := scope
	outside.AllowedTaskIDs = map[TaskID]bool{}
	if _, err := ProposeAgentTaskEdit(outside, task, "proposal-1", AgentTaskEdit{Patch: TaskPatch{Title: &title}}, []string{"input-1"}); !errors.Is(err, ErrAgentProposal) {
		t.Fatalf("out-of-scope agent proposal error = %v", err)
	}
	private := scope
	private.AuthorizedInputIDs = map[string]bool{}
	if _, err := ProposeAgentTaskEdit(private, task, "proposal-2", AgentTaskEdit{Patch: TaskPatch{Title: &title}}, []string{"private-input"}); !errors.Is(err, ErrAgentInput) {
		t.Fatalf("unauthorized proposal source error = %v", err)
	}
	if err := RejectAgentHCMAction(scope, "COMPLETE_HCM_WORK_ITEM"); !errors.Is(err, ErrAgentHCMAction) {
		t.Fatalf("agent HCM action was not rejected: %v", err)
	}
}

func TestTodo_PM_067_Conformance(t *testing.T) {
	scope, project, task := pm17AgentFixture(t, true)
	title := "Reviewed title"
	proposal, err := ProposeAgentTaskEdit(scope, task, "proposal-1", AgentTaskEdit{Patch: TaskPatch{Title: &title}}, []string{"input-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := proposal.CommitAgentTaskEdit(scope, task, project, "agent-1", task.Revision, nil); !errors.Is(err, ErrAgentReview) {
		t.Fatalf("unreviewed proposal committed: %v", err)
	}
	if _, err := proposal.ReviewAgentTaskEdit("agent-1", "APPROVE", proposal.Revision, scope); !errors.Is(err, ErrAgentReview) {
		t.Fatalf("proposer self-approved proposal: %v", err)
	}
	if _, err := proposal.ReviewAgentTaskEdit("human-1", "APPROVE", proposal.Revision+1, scope); !errors.Is(err, ErrAgentReview) {
		t.Fatalf("stale proposal review accepted: %v", err)
	}
}

func TestTodo_PM_067_Golden(t *testing.T) {
	scope, _, _ := pm17AgentFixture(t, false)
	summary, err := PublishAgentTaskSummary(scope, AgentTaskSummary{ID: "summary-1", TenantID: "tenant-1", ProjectID: "project-1", TaskID: "task-1", AgentID: "agent-1", Text: "The plan is ready.", Citations: []SummaryCitation{{InputID: "input-1", Revision: "rev-4"}}}, map[string]AuthorizedAgentInput{"input-1": {InputID: "input-1", Revision: "rev-4"}})
	if err != nil || !summary.Published || len(summary.Citations) != 1 || summary.Citations[0].Revision != "rev-4" {
		t.Fatalf("authorized cited summary = %+v, err=%v", summary, err)
	}
	private := map[string]AuthorizedAgentInput{"input-1": {InputID: "input-1", Revision: "rev-4", Private: true}}
	if _, err := PublishAgentTaskSummary(scope, AgentTaskSummary{ID: "summary-2", TenantID: "tenant-1", ProjectID: "project-1", TaskID: "task-1", AgentID: "agent-1", Text: "Private context", Citations: []SummaryCitation{{InputID: "input-1", Revision: "rev-4"}}}, private); !errors.Is(err, ErrAgentInput) {
		t.Fatalf("private cited input was published: %v", err)
	}
}
