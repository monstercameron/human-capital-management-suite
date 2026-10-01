package application

// This file is the application adapter for the ordinary project skill. The
// project domain owns proposal and revision semantics; this adapter owns the
// agent grant boundary and delegates durable task mutations to projectservice.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const AgentProjectSkillID = "project.tasks"

var (
	ErrAgentProjectSkill     = errors.New("application: agent project skill unavailable")
	ErrAgentProjectGrant     = errors.New("application: agent project grant is invalid")
	ErrAgentProjectProposal  = errors.New("application: agent project proposal is unavailable")
	ErrAgentProjectHCMAction = errors.New("application: project skill cannot create a BusinessIntent")
	ErrAgentProjectNotAgent  = errors.New("application: project skill requires an agent claim")
	ErrAgentProjectPrincipal = errors.New("application: project skill principal mismatch")
)

// AgentProjectProposalStore persists proposals between draft, review and
// commit. Implementations must preserve the proposal revision exactly.
type AgentProjectProposalStore interface {
	Save(context.Context, project.AgentTaskProposal) error
	Get(context.Context, string, string, string) (project.AgentTaskProposal, error)
	Update(context.Context, project.AgentTaskProposal) error
}

type AgentProjectMemoryProposalStore struct {
	mu sync.RWMutex
	m  map[string]project.AgentTaskProposal
}

func NewAgentProjectMemoryProposalStore() *AgentProjectMemoryProposalStore {
	return &AgentProjectMemoryProposalStore{m: make(map[string]project.AgentTaskProposal)}
}
func (s *AgentProjectMemoryProposalStore) Save(_ context.Context, p project.AgentTaskProposal) error {
	if s == nil || p.ID == "" {
		return ErrAgentProjectProposal
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[p.ID]; ok {
		return ErrAgentProjectProposal
	}
	s.m[p.ID] = cloneAgentProposal(p)
	return nil
}
func (s *AgentProjectMemoryProposalStore) Get(_ context.Context, tenant, projectID, id string) (project.AgentTaskProposal, error) {
	if s == nil {
		return project.AgentTaskProposal{}, ErrAgentProjectProposal
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.m[id]
	if !ok || p.TenantID != tenant || string(p.ProjectID) != projectID {
		return project.AgentTaskProposal{}, ErrAgentProjectProposal
	}
	return cloneAgentProposal(p), nil
}
func (s *AgentProjectMemoryProposalStore) Update(_ context.Context, p project.AgentTaskProposal) error {
	if s == nil || p.ID == "" {
		return ErrAgentProjectProposal
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[p.ID]; !ok {
		return ErrAgentProjectProposal
	}
	s.m[p.ID] = cloneAgentProposal(p)
	return nil
}

type AgentProjectSkill struct {
	Projects  projectservice.Service
	Grants    agentdelegation.GrantStore
	GrantFactory AgentProjectGrantStoreFactory
	Proposals AgentProjectProposalStore
	Now       func() time.Time
}

type AgentProjectGrantStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error)
}

func NewAgentProjectSkillForRequests(projects projectservice.Service, grants AgentProjectGrantStoreFactory, proposals AgentProjectProposalStore, now func() time.Time) (*AgentProjectSkill, error) {
	if grants == nil || proposals == nil { return nil, ErrAgentProjectSkill }
	if now == nil { now = time.Now }
	return &AgentProjectSkill{Projects: projects, GrantFactory: grants, Proposals: proposals, Now: now}, nil
}

// NewAgentProjectSkill constructs the runtime adapter with the project-owned
// service, current delegation-grant reader, and durable proposal store.
func NewAgentProjectSkill(projects projectservice.Service, grants agentdelegation.GrantStore, proposals AgentProjectProposalStore, now func() time.Time) (*AgentProjectSkill, error) {
	if grants == nil || proposals == nil {
		return nil, ErrAgentProjectSkill
	}
	if now == nil {
		now = time.Now
	}
	return &AgentProjectSkill{Projects: projects, Grants: grants, Proposals: proposals, Now: now}, nil
}

type AgentProjectTaskRequest struct {
	Principal *trust.Principal
	Claims    agentdelegation.Claims
	ProjectID string
	TaskID    string
}

type AgentProjectTaskScope struct {
	TenantID, ProjectID, TaskID, AgentID         string
	TaskRevision, WorkflowRevision               uint64
	CanPropose, CanSummarize, RequireHumanReview bool
}

type AgentProjectTaskResult struct {
	Task  projectservice.TaskRecord
	Scope AgentProjectTaskScope
}

type AgentProjectDraftRequest struct {
	AgentProjectTaskRequest
	ProposalID         string
	Edit               project.AgentTaskEdit
	CitedInputIDs      []string
	AuthorizedInputIDs map[string]bool
}

type AgentProjectDraftResult struct {
	Proposal        project.AgentTaskProposal
	Task            projectservice.TaskRecord
	CurrentRevision uint64
}

type AgentProjectCommitRequest struct {
	AgentProjectTaskRequest
	ProposalID               string
	ExpectedProposalRevision uint64
	ReviewerID               string
	Decision                 string
	ExpectedTaskRevision     uint64
	IdempotencyKey           string
}

type AgentProjectCommitResult struct {
	Proposal        project.AgentTaskProposal
	Task            projectservice.TaskRecord
	CurrentRevision uint64
}

func (s *AgentProjectSkill) CurrentTask(ctx context.Context, req AgentProjectTaskRequest) (AgentProjectTaskResult, error) {
	grant, err := s.validate(ctx, req)
	if err != nil {
		return AgentProjectTaskResult{}, err
	}
	if err := resourceAllowed(grant, req.Claims, req.ProjectID, req.TaskID); err != nil {
		return AgentProjectTaskResult{}, err
	}
	t, err := s.Projects.GetTask(ctx, req.Principal, req.ProjectID, req.TaskID)
	if err != nil {
		return AgentProjectTaskResult{}, err
	}
	return AgentProjectTaskResult{Task: t, Scope: AgentProjectTaskScope{TenantID: t.TenantID, ProjectID: t.ProjectID, TaskID: t.ID, AgentID: req.Claims.Actor.AgentVersion, TaskRevision: t.Revision, WorkflowRevision: t.WorkflowRevision, CanPropose: true, CanSummarize: true}}, nil
}

func (s *AgentProjectSkill) DraftTaskEdit(ctx context.Context, req AgentProjectDraftRequest) (AgentProjectDraftResult, error) {
	current, err := s.CurrentTask(ctx, req.AgentProjectTaskRequest)
	if err != nil {
		return AgentProjectDraftResult{}, err
	}
	authorizedInputs := make(map[string]bool)
	scope := project.AgentTaskScope{TenantID: current.Task.TenantID, ProjectID: project.ProjectID(current.Task.ProjectID), AgentID: req.Claims.Actor.AgentVersion, AllowedTaskIDs: map[project.TaskID]bool{project.TaskID(current.Task.ID): true}, AuthorizedInputIDs: authorizedInputs, CanPropose: true, RequireHumanReview: s.requiresReview(req.Claims)}
	for _, inputID := range req.CitedInputIDs {
		if !s.inputAllowed(ctx, req.AgentProjectTaskRequest, inputID) {
			return AgentProjectDraftResult{}, ErrAgentProjectGrant
		}
		scope.AuthorizedInputIDs[inputID] = true
	}
	proposal, err := project.ProposeAgentTaskEdit(scope, taskDomain(current.Task), req.ProposalID, req.Edit, req.CitedInputIDs)
	if err != nil {
		return AgentProjectDraftResult{}, err
	}
	if s.Proposals == nil {
		return AgentProjectDraftResult{}, ErrAgentProjectProposal
	}
	if err := s.Proposals.Save(ctx, proposal); err != nil {
		return AgentProjectDraftResult{}, err
	}
	return AgentProjectDraftResult{Proposal: proposal, Task: current.Task, CurrentRevision: current.Task.Revision}, nil
}

func (s *AgentProjectSkill) CommitTaskEdit(ctx context.Context, req AgentProjectCommitRequest) (AgentProjectCommitResult, error) {
	grant, err := s.validate(ctx, req.AgentProjectTaskRequest)
	if err != nil {
		return AgentProjectCommitResult{}, err
	}
	if err := resourceAllowed(grant, req.Claims, req.ProjectID, req.TaskID); err != nil {
		return AgentProjectCommitResult{}, err
	}
	if strings.TrimSpace(req.ProposalID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ExpectedTaskRevision == 0 {
		return AgentProjectCommitResult{}, projectservice.ErrInvalidRequest
	}
	if err := s.rejectHCM(req.Claims); err != nil {
		return AgentProjectCommitResult{}, err
	}
	if s.Proposals == nil {
		return AgentProjectCommitResult{}, ErrAgentProjectProposal
	}
	proposal, err := s.Proposals.Get(ctx, req.Principal.Tenant().String(), req.ProjectID, req.ProposalID)
	if err != nil {
		return AgentProjectCommitResult{}, err
	}
	current, err := s.Projects.GetTask(ctx, req.Principal, req.ProjectID, req.TaskID)
	if err != nil {
		return AgentProjectCommitResult{}, err
	}
	if proposal.ProjectID != project.ProjectID(req.ProjectID) || proposal.TaskID != project.TaskID(req.TaskID) || proposal.AgentID != req.Claims.Actor.AgentVersion {
		return AgentProjectCommitResult{}, ErrAgentProjectGrant
	}
	scope := project.AgentTaskScope{TenantID: current.TenantID, ProjectID: project.ProjectID(current.ProjectID), AgentID: proposal.AgentID, AllowedTaskIDs: map[project.TaskID]bool{project.TaskID(current.ID): true}, CanPropose: true, RequireHumanReview: s.requiresReview(req.Claims)}
	if scope.RequireHumanReview {
		if strings.TrimSpace(req.ReviewerID) == "" || req.ReviewerID != req.Principal.Subject() {
			return AgentProjectCommitResult{}, ErrAgentProjectPrincipal
		}
		proposal, err = proposal.ReviewAgentTaskEdit(req.ReviewerID, req.Decision, req.ExpectedProposalRevision, scope)
		if err != nil {
			return AgentProjectCommitResult{}, err
		}
		if proposal.State != project.AgentProposalApproved {
			return AgentProjectCommitResult{}, ErrAgentProjectProposal
		}
	}
	if proposal.BaseTaskRevision != req.ExpectedTaskRevision || current.Revision != req.ExpectedTaskRevision {
		return AgentProjectCommitResult{}, project.ErrRevisionConflict
	}
	if proposal.Edit.TargetStatusID != "" {
		updated, e := s.Projects.MoveTask(ctx, req.Principal, projectservice.MoveTaskRequest{ProjectID: req.ProjectID, TaskID: req.TaskID, TargetStatusID: proposal.Edit.TargetStatusID, ExpectedTaskRevision: req.ExpectedTaskRevision, ExpectedConfigRevision: proposal.Edit.ConfigRevision, FieldEdits: proposal.Edit.FieldEdits, IdempotencyKey: req.IdempotencyKey})
		err = e
		current = updated
	} else {
		updated, e := s.Projects.PatchTask(ctx, req.Principal, projectservice.PatchTaskRequest{ProjectID: req.ProjectID, TaskID: req.TaskID, ExpectedTaskRevision: req.ExpectedTaskRevision, ExpectedWorkflowRevision: current.WorkflowRevision, Patch: proposal.Edit.Patch, IdempotencyKey: req.IdempotencyKey})
		err = e
		current = updated
	}
	if err != nil {
		return AgentProjectCommitResult{}, err
	}
	proposal.State, proposal.Revision = project.AgentProposalCommitted, proposal.Revision+1
	if err := s.Proposals.Update(ctx, proposal); err != nil {
		return AgentProjectCommitResult{}, err
	}
	return AgentProjectCommitResult{Proposal: proposal, Task: current, CurrentRevision: current.Revision}, nil
}

func (s *AgentProjectSkill) RejectBusinessIntent(ctx context.Context, req AgentProjectTaskRequest, action string) error {
	if _, err := s.validate(ctx, req); err != nil {
		return err
	}
	return project.RejectAgentHCMAction(project.AgentTaskScope{AllowHCMActions: false}, action)
}

func (s *AgentProjectSkill) validate(ctx context.Context, req AgentProjectTaskRequest) (agentdelegation.Grant, error) {
	if s == nil || (s.Grants == nil && s.GrantFactory == nil) || s.Projects.Auth == nil || req.Principal == nil || req.Principal.SubjectKind() != trust.SubjectKindHuman || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TaskID) == "" {
		return agentdelegation.Grant{}, ErrAgentProjectSkill
	}
	if verified, ok := trust.FromContext(ctx); !ok || verified != req.Principal {
		return agentdelegation.Grant{}, ErrAgentProjectPrincipal
	}
	if strings.TrimSpace(req.Claims.Actor.AgentVersion) == "" || strings.TrimSpace(req.Claims.Actor.InstallationID) == "" || strings.TrimSpace(req.Claims.Actor.RunID) == "" || strings.TrimSpace(req.Claims.Actor.StepID) == "" {
		return agentdelegation.Grant{}, ErrAgentProjectNotAgent
	}
	if req.Claims.TokenType != agentdelegation.DelegatedAccessTokenType || req.Claims.Tenant != req.Principal.Tenant().String() || req.Claims.Skill != AgentProjectSkillID || req.Claims.Subject != req.Principal.Subject() || req.Claims.GrantID == "" {
		return agentdelegation.Grant{}, ErrAgentProjectGrant
	}
	grants := s.Grants
	if s.GrantFactory != nil {
		scoped, err := s.GrantFactory.ForTenant(ctx, req.Principal.Tenant())
		if err != nil { return agentdelegation.Grant{}, err }
		grants = scoped
	}
	g, err := grants.Get(req.Claims.GrantID)
	if err != nil {
		return agentdelegation.Grant{}, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if g.GrantID != req.Claims.GrantID || g.Revoked || g.Tenant.String() != req.Principal.Tenant().String() || g.UserID != req.Principal.Subject() || g.Purpose != req.Claims.Purpose || g.TaskID == "" || g.AgentVersion != req.Claims.Actor.AgentVersion || g.InstallationID != req.Claims.Actor.InstallationID || (g.RevocationEpoch != grants.CurrentRevocationEpoch(g.Tenant, g.UserID)) || (!g.NotBefore.IsZero() && now.Before(g.NotBefore)) || (!g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt)) {
		return agentdelegation.Grant{}, ErrAgentProjectGrant
	}
	if !skillScopeSubset(g, req.Claims.Skill, req.Claims.Scope) {
		return agentdelegation.Grant{}, ErrAgentProjectGrant
	}
	if err := resourceAllowed(g, req.Claims, req.ProjectID, req.TaskID); err != nil {
		return agentdelegation.Grant{}, err
	}
	return g, nil
}

func resourceAllowed(g agentdelegation.Grant, c agentdelegation.Claims, projectID, taskID string) error {
	want := []string{"project:" + projectID, "task:" + taskID}
	allowed := skillResources(g, c.Skill)
	for _, w := range want {
		if !allowed[w] {
			return ErrAgentProjectGrant
		}
	}
	return nil
}

func (s *AgentProjectSkill) inputAllowed(ctx context.Context, req AgentProjectTaskRequest, inputID string) bool {
	g, err := s.validate(ctx, req)
	if err != nil {
		return false
	}
	return skillResources(g, req.Claims.Skill)["input:"+inputID]
}

func skillScopeSubset(g agentdelegation.Grant, skill string, requested []string) bool {
	declared := make(map[string]bool)
	for _, scope := range g.SkillScopes[skill] {
		declared[scope] = true
	}
	if len(declared) == 0 {
		return false
	}
	for _, scope := range requested {
		if !declared[scope] {
			return false
		}
	}
	return true
}

func skillResources(g agentdelegation.Grant, skill string) map[string]bool {
	all := make(map[string]bool, len(g.Authority.Resources))
	for _, resource := range g.Authority.Resources {
		all[resource] = true
	}
	if authority, ok := g.SkillAuthorities[skill]; ok {
		perSkill := make(map[string]bool, len(authority.Resources))
		for _, resource := range authority.Resources {
			perSkill[resource] = true
		}
		for resource := range all {
			if !perSkill[resource] {
				delete(all, resource)
			}
		}
	} else {
		return nil
	}
	return all
}
func (s *AgentProjectSkill) requiresReview(c agentdelegation.Claims) bool {
	return true
}
func (s *AgentProjectSkill) rejectHCM(c agentdelegation.Claims) error {
	if c.Skill != AgentProjectSkillID {
		return ErrAgentProjectGrant
	}
	return nil
}

func taskDomain(t projectservice.TaskRecord) project.Task {
	return project.Task{ID: project.TaskID(t.ID), ProjectID: project.ProjectID(t.ProjectID), TenantID: t.TenantID, Title: t.Title, Description: t.Description, Status: t.StatusID, TypeID: project.TypeID(t.TypeID), Priority: project.Priority(t.Priority), AssigneeID: t.AssigneeID, DueDate: t.DueDate, Revision: t.Revision, Archived: t.Archived, Fields: maps.Clone(t.Fields)}
}
func cloneAgentProposal(p project.AgentTaskProposal) project.AgentTaskProposal {
	p.CitedInputIDs = slices.Clone(p.CitedInputIDs)
	p.Edit.FieldEdits = slices.Clone(p.Edit.FieldEdits)
	return p
}

var _ AgentProjectProposalStore = (*AgentProjectMemoryProposalStore)(nil)
