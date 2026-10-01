package project

import (
	"errors"
	"strings"
)

type AgentProposalState string

const (
	AgentProposalProposed  AgentProposalState = "PROPOSED"
	AgentProposalApproved  AgentProposalState = "APPROVED"
	AgentProposalRejected  AgentProposalState = "REJECTED"
	AgentProposalCommitted AgentProposalState = "COMMITTED"
)

var (
	ErrAgentScope     = errors.New("project: agent is outside project task scope")
	ErrAgentProposal  = errors.New("project: invalid agent task proposal")
	ErrAgentReview    = errors.New("project: agent task proposal requires human review")
	ErrAgentRevision  = errors.New("project: agent task proposal revision is stale")
	ErrAgentInput     = errors.New("project: agent summary cites an unauthorized input")
	ErrAgentHCMAction = errors.New("project: agent cannot perform HCM actions through a project task")
	ErrAgentSummary   = errors.New("project: invalid agent task summary")
)

// AgentTaskScope is issued by the agent installation boundary. It carries
// only ordinary project authority; AllowHCMActions is intentionally not an
// escape hatch and must remain false for every project installation.
type AgentTaskScope struct {
	TenantID           string
	ProjectID          ProjectID
	AgentID            string
	AllowedTaskIDs     map[TaskID]bool
	AuthorizedInputIDs map[string]bool
	CanPropose         bool
	CanSummarize       bool
	RequireHumanReview bool
	AllowHCMActions    bool
}

func (s AgentTaskScope) allowsTask(tenant string, project ProjectID, task TaskID) bool {
	return s.TenantID != "" && tenant == s.TenantID && s.ProjectID == project && s.AgentID != "" && s.CanPropose && s.AllowedTaskIDs[task] && !s.AllowHCMActions
}

type AgentTaskEdit struct {
	Patch          TaskPatch
	TargetStatusID string
	FieldEdits     []TaskFieldEdit
	ConfigRevision uint64
}

type AgentTaskProposal struct {
	ID               string
	TenantID         string
	ProjectID        ProjectID
	TaskID           TaskID
	AgentID          string
	BaseTaskRevision uint64
	Edit             AgentTaskEdit
	CitedInputIDs    []string
	State            AgentProposalState
	Revision         uint64
	HumanReviewerID  string
	HumanDecision    string
}

// ProposeAgentTaskEdit creates a reviewable record only. It never mutates the
// live task and pins the exact task revision that the eventual commit must
// use.
func ProposeAgentTaskEdit(scope AgentTaskScope, task Task, proposalID string, edit AgentTaskEdit, citedInputIDs []string) (AgentTaskProposal, error) {
	if !scope.allowsTask(task.TenantID, task.ProjectID, task.ID) || strings.TrimSpace(proposalID) == "" || task.Revision == 0 || (edit.TargetStatusID == "" && emptyTaskPatch(edit.Patch)) || (edit.TargetStatusID != "" && edit.ConfigRevision == 0) {
		return AgentTaskProposal{}, ErrAgentProposal
	}
	if len(citedInputIDs) == 0 {
		return AgentTaskProposal{}, ErrAgentInput
	}
	for _, inputID := range citedInputIDs {
		if strings.TrimSpace(inputID) == "" || !scope.AuthorizedInputIDs[inputID] {
			return AgentTaskProposal{}, ErrAgentInput
		}
	}
	return AgentTaskProposal{ID: proposalID, TenantID: task.TenantID, ProjectID: task.ProjectID, TaskID: task.ID, AgentID: scope.AgentID, BaseTaskRevision: task.Revision, Edit: AgentTaskEdit{Patch: edit.Patch, TargetStatusID: edit.TargetStatusID, FieldEdits: append([]TaskFieldEdit(nil), edit.FieldEdits...), ConfigRevision: edit.ConfigRevision}, CitedInputIDs: append([]string(nil), citedInputIDs...), State: AgentProposalProposed, Revision: 1}, nil
}

// ReviewAgentTaskEdit advances a proposal without applying it. The reviewer
// must be a human distinct from the proposing agent whenever policy requires
// review.
func (p AgentTaskProposal) ReviewAgentTaskEdit(reviewerID, decision string, expectedProposalRevision uint64, scope AgentTaskScope) (AgentTaskProposal, error) {
	if p.State != AgentProposalProposed || expectedProposalRevision != p.Revision || reviewerID == "" || reviewerID == p.AgentID || !scope.RequireHumanReview {
		return AgentTaskProposal{}, ErrAgentReview
	}
	if decision != "APPROVE" && decision != "REJECT" {
		return AgentTaskProposal{}, ErrAgentReview
	}
	p.HumanReviewerID, p.HumanDecision, p.Revision = reviewerID, decision, p.Revision+1
	if decision == "APPROVE" {
		p.State = AgentProposalApproved
	} else {
		p.State = AgentProposalRejected
	}
	return p, nil
}

// CommitAgentTaskEdit delegates the actual mutation to the same project-owned
// revisioned task operations used by human callers. It cannot invoke HCM
// actions and it rejects a stale task even when the proposal itself is valid.
func (p AgentTaskProposal) CommitAgentTaskEdit(scope AgentTaskScope, task Task, project Project, actorID string, expectedTaskRevision uint64, workflow TransitionPolicy) (AgentTaskProposal, Task, error) {
	if p.State != AgentProposalApproved && (scope.RequireHumanReview || p.State != AgentProposalProposed) {
		return AgentTaskProposal{}, Task{}, ErrAgentReview
	}
	if !scope.allowsTask(task.TenantID, task.ProjectID, task.ID) || p.TenantID != task.TenantID || p.ProjectID != task.ProjectID || p.TaskID != task.ID || p.AgentID != scope.AgentID || expectedTaskRevision != task.Revision || p.BaseTaskRevision != task.Revision || actorID == "" || scope.AllowHCMActions {
		return AgentTaskProposal{}, Task{}, ErrAgentRevision
	}
	var updated Task
	var err error
	if p.Edit.TargetStatusID != "" {
		updated, err = task.TransitionTask(project, workflow, p.Edit.TargetStatusID, expectedTaskRevision, p.Edit.ConfigRevision, p.Edit.FieldEdits)
	} else {
		updated, err = task.PatchTask(project, p.Edit.Patch, expectedTaskRevision)
	}
	if err != nil {
		return AgentTaskProposal{}, Task{}, err
	}
	p.State, p.Revision = AgentProposalCommitted, p.Revision+1
	return p, updated, nil
}

type SummaryCitation struct {
	InputID  string
	Revision string
}

type AuthorizedAgentInput struct {
	InputID  string
	Revision string
	Private  bool
}

type AgentTaskSummary struct {
	ID        string
	TenantID  string
	ProjectID ProjectID
	TaskID    TaskID
	AgentID   string
	Text      string
	Citations []SummaryCitation
	Published bool
}

// PublishAgentTaskSummary allows only cited, current, non-private inputs. The
// summary is a project projection; raw private source text never becomes a
// broadly readable project record.
func PublishAgentTaskSummary(scope AgentTaskScope, summary AgentTaskSummary, inputs map[string]AuthorizedAgentInput) (AgentTaskSummary, error) {
	if !scope.CanSummarize || scope.AllowHCMActions || summary.ID == "" || summary.TenantID != scope.TenantID || summary.ProjectID != scope.ProjectID || summary.AgentID != scope.AgentID || summary.TaskID == "" || strings.TrimSpace(summary.Text) == "" || len(summary.Citations) == 0 || summary.Published {
		return AgentTaskSummary{}, ErrAgentSummary
	}
	seen := make(map[string]bool, len(summary.Citations))
	for _, citation := range summary.Citations {
		input, ok := inputs[citation.InputID]
		if !ok || input.InputID != citation.InputID || input.Revision != citation.Revision || input.Private || !scope.AuthorizedInputIDs[citation.InputID] || seen[citation.InputID] {
			return AgentTaskSummary{}, ErrAgentInput
		}
		seen[citation.InputID] = true
	}
	summary.Citations = append([]SummaryCitation(nil), summary.Citations...)
	summary.Published = true
	return summary, nil
}

// RejectAgentHCMAction makes the boundary explicit for callers that try to
// turn a project card into a BusinessIntent or Human Work command.
func RejectAgentHCMAction(scope AgentTaskScope, action string) error {
	if strings.TrimSpace(action) != "" || scope.AllowHCMActions {
		return ErrAgentHCMAction
	}
	return nil
}

func emptyTaskPatch(patch TaskPatch) bool {
	return patch.Title == nil && patch.Description == nil && patch.AssigneeID == nil && patch.DueDate == nil && patch.TypeID == nil && patch.Priority == nil && patch.StartDate == nil && patch.StoryPoints == nil && patch.Labels == nil
}
