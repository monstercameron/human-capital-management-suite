package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

const (
	agentMaxPromptRunes = 2000
	agentStartWait      = 10 * time.Second
	agentDriveTimeout   = 3 * time.Minute
	agentVersion        = "hcm-agent-self-service/v1"
	agentReadStepID     = "read_worker_state"
	agentSummaryStepID  = "summarize_request"
)

// agentStarter implements agentclient.Starter. It turns the signed-in user's
// prompt into a deterministic minimum-data plan, starts the task with the
// user's current authority, confirms the plan as the user (they pressed
// start) and drives it on a context detached from the request.
type agentStarter struct {
	platform  *agentsystem.Platform
	settings  workspace.AgentSettings
	authority agentdelegation.AuthorityResolver
	inspector *trustdlp.Inspector
	model     agentModel
	now       func() time.Time
	wait      time.Duration
	driveFor  time.Duration
	newID     func() (string, error)
	documents agentdocref.Resolver
	personas  AgentTaskPersonaResolver
	planner   AgentTaskPlanner
}

var _ agentclient.Starter = (*agentStarter)(nil)

func randomAgentTaskID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "agt_" + hex.EncodeToString(raw[:]), nil
}

// StartTask starts and drives one self-service task for the principal.
func (s *agentStarter) StartTask(ctx context.Context, principal *trust.Principal, prompt string) (agentclient.StartedTask, error) {
	return s.startTaskMode(ctx, principal, prompt, agentclient.StartQuickAnswer, nil, "")
}

// StartTaskMode applies the explicit quick-answer versus long-task policy.
// Quick answers confirm the bounded read-only plan immediately; long tasks
// return awaiting confirmation and do not start a worker until the control
// RPC confirms the plan.
func (s *agentStarter) StartTaskMode(ctx context.Context, principal *trust.Principal, prompt string, mode agentclient.StartMode) (agentclient.StartedTask, error) {
	return s.startTaskMode(ctx, principal, prompt, mode, nil, "")
}

// StartTaskWithDocuments admits typed document references before creating the
// durable task. The resolver always receives the invoking human identity.
func (s *agentStarter) StartTaskWithDocuments(ctx context.Context, principal *trust.Principal, prompt string, mode agentclient.StartMode, refs []agentdocref.Reference) (agentclient.StartedTask, error) {
	return s.startTaskMode(ctx, principal, prompt, mode, refs, "")
}

// StartTaskWithSelection admits both typed document references and the
// optional persona choice before creating any durable task state.
func (s *agentStarter) StartTaskWithSelection(ctx context.Context, principal *trust.Principal, prompt string, mode agentclient.StartMode, refs []agentdocref.Reference, personaID string) (agentclient.StartedTask, error) {
	return s.startTaskMode(ctx, principal, prompt, mode, refs, personaID)
}

func (s *agentStarter) startTaskMode(ctx context.Context, principal *trust.Principal, prompt string, mode agentclient.StartMode, refs []agentdocref.Reference, personaID string) (agentclient.StartedTask, error) {
	if s == nil || s.platform == nil || principal == nil {
		return agentclient.StartedTask{}, agentclient.ErrNotAuthorized
	}
	if mode != agentclient.StartQuickAnswer && mode != agentclient.StartLongTask {
		return agentclient.StartedTask{}, agentclient.ErrInvalidPrompt
	}
	if err := agentdocref.Validate(refs, agentdocref.MaxRequestReferences); err != nil {
		return agentclient.StartedTask{}, agentrun.ErrDocumentReferenceInvalid
	}
	goal, err := s.validatePrompt(prompt)
	if err != nil {
		return agentclient.StartedTask{}, err
	}
	tenant, subject := principal.Tenant(), principal.Subject()
	if tenant.Validate() != nil || strings.TrimSpace(subject) == "" ||
		principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) {
		return agentclient.StartedTask{}, agentclient.ErrNotAuthorized
	}
	if s.settings == nil {
		return agentclient.StartedTask{}, agentclient.ErrDisabled
	}
	enabled, err := s.settings.AgentsEnabled(ctx, tenant)
	if err != nil {
		return agentclient.StartedTask{}, fmt.Errorf("agent settings: %w", err)
	}
	if !enabled {
		return agentclient.StartedTask{}, agentclient.ErrDisabled
	}
	if !s.model.Available() {
		return agentclient.StartedTask{}, ErrAgentModelUnavailable
	}
	planOutput := AgentTaskPlanningOutput{}
	if s.planner != nil {
		planOutput, err = s.planner.PlanAgentTask(ctx, goal, mode)
		if err != nil {
			return agentclient.StartedTask{}, fmt.Errorf("plan agent task: %w", err)
		}
	}
	answeringAgent := agentrun.TaskAgentIdentity{ID: "general-agent", DisplayName: "General agent", Version: agentVersion}
	selectedPersona := strings.TrimSpace(personaID)
	if selectedPersona != personaID {
		return agentclient.StartedTask{}, agentrun.ErrTaskAgentDenied
	}
	if selectedPersona == answeringAgent.ID {
		// The general agent named by its own id is the general agent.
		selectedPersona, personaID = "", ""
	}
	if personaID = selectedPersona; personaID != "" {
		if s.personas == nil {
			return agentclient.StartedTask{}, agentrun.ErrTaskAgentUnavailable
		}
		answeringAgent, err = s.personas.ResolveAgentTaskPersona(ctx, principal, personaID)
		if err != nil {
			return agentclient.StartedTask{}, err
		}
	}
	now := s.now().UTC()
	// The authority is resolved from the durable role policy at this instant;
	// the principal's claims and the prompt contribute nothing to it.
	current, err := s.authority.Resolve(subject, tenant, agentPurpose, now)
	if err != nil {
		return agentclient.StartedTask{}, fmt.Errorf("resolve agent authority: %w", err)
	}
	if !current.Active || current.UserID != subject || current.Authority.Tenant != tenant ||
		!slices.Contains(current.Authority.Capabilities, agentReadCapabilityID) || !slices.Contains(current.Authority.Purposes, agentPurpose) {
		return agentclient.StartedTask{}, agentclient.ErrNotAuthorized
	}
	if len(refs) > 0 {
		if s.documents == nil {
			return agentclient.StartedTask{}, agentrun.ErrDocumentResolverUnavailable
		}
		resolved, omissions, resolveErr := s.documents.Resolve(ctx, agentdocref.Invoker{TenantID: tenant.String(), SubjectID: subject}, refs)
		if resolveErr != nil || len(omissions) != 0 || len(resolved) != len(refs) {
			return agentclient.StartedTask{}, agentrun.ErrDocumentReferenceUnreadable
		}
		for i := range refs {
			if resolved[i].Reference != refs[i] {
				return agentclient.StartedTask{}, agentrun.ErrDocumentReferenceUnreadable
			}
		}
	}

	// Everything after authorization outlives the request: the grant store
	// binds its context at ForTenant, and the task keeps running after the
	// page's request returns.
	driveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.driveFor)
	if len(refs) > 0 {
		driveCtx, err = agentrun.WithDocumentReferences(driveCtx, refs)
		if err != nil {
			cancel()
			return agentclient.StartedTask{}, err
		}
	}
	driveCtx, err = agentrun.WithTaskAgentIdentity(driveCtx, answeringAgent)
	if err != nil {
		cancel()
		return agentclient.StartedTask{}, err
	}
	runner, err := s.platform.ForTenant(driveCtx, tenant)
	if err != nil {
		cancel()
		return agentclient.StartedTask{}, err
	}
	id, err := s.newID()
	if err != nil {
		cancel()
		return agentclient.StartedTask{}, fmt.Errorf("agent task id: %w", err)
	}
	task, err := runner.StartTask(driveCtx, agentsystem.StartRequest{
		TaskID: id, UserID: subject, AgentVersion: agentVersion, InstallationID: "install:" + tenant.String(),
		Purpose: agentPurpose, OrganizationScopeID: agentOrgScope(tenant), Goal: goal,
		Constraints:   []string{"read-only: no worker records are changed; the request is processed by the configured model provider"},
		Steps:         agentTaskPlanSteps(planOutput),
		UserAuthority: current.Authority,
		Limit:         agentbudget.Limits{},
		Lifetime:      24 * time.Hour,
	})
	if err != nil {
		cancel()
		if errors.Is(err, agentdelegation.ErrScopeExpanded) || errors.Is(err, agentdelegation.ErrUserInactive) {
			return agentclient.StartedTask{}, agentclient.ErrNotAuthorized
		}
		return agentclient.StartedTask{}, err
	}
	if mode == agentclient.StartLongTask {
		cancel()
		return agentclient.StartedTask{ID: task.ID, State: string(task.State), Version: task.Version}, nil
	}
	// The quick-answer button is confirmation of this fixed read-only plan.
	if _, err := runner.Runtime.ConfirmPlan(driveCtx, task.ID, subject, task.Version, now); err != nil {
		cancel()
		return agentclient.StartedTask{}, err
	}

	type outcome struct {
		task agentrun.AgentTask
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		defer cancel()
		final, driveErr := runner.Drive(driveCtx, task.ID, agentsystem.ModeOnBehalfOf)
		done <- outcome{final, driveErr}
	}()
	timer := time.NewTimer(s.wait)
	defer timer.Stop()
	select {
	case result := <-done:
		if result.err != nil && result.task.ID == "" {
			return agentclient.StartedTask{ID: task.ID, State: string(agentrun.StateRunning), Version: task.Version}, nil
		}
		return agentclient.StartedTask{ID: task.ID, State: string(result.task.State), Version: result.task.Version}, nil
	case <-timer.C:
	case <-ctx.Done():
	}
	// Still running: report the durable state as it stands. The drive
	// continues on its own context.
	if latest, getErr := runner.Runtime.GetTask(context.WithoutCancel(ctx), task.ID); getErr == nil {
		return agentclient.StartedTask{ID: task.ID, State: string(latest.State), Version: latest.Version}, nil
	}
	return agentclient.StartedTask{ID: task.ID, State: string(agentrun.StateRunning), Version: task.Version}, nil
}

// validatePrompt bounds the prompt and refuses one that carries personal or
// financial identifiers, since its text becomes the model's goal.
func (s *agentStarter) validatePrompt(prompt string) (string, error) {
	goal := strings.TrimSpace(prompt)
	if goal == "" || utf8.RuneCountInString(goal) > agentMaxPromptRunes || !utf8.ValidString(goal) || strings.ContainsRune(goal, 0) {
		return "", agentclient.ErrInvalidPrompt
	}
	if s.inspector != nil {
		inspection, err := s.inspector.Inspect([]byte(goal))
		if err != nil {
			return "", fmt.Errorf("inspect agent prompt: %w", err)
		}
		if len(inspection.Findings) > 0 {
			return "", fmt.Errorf("%w: it appears to contain personal or financial identifiers", agentclient.ErrInvalidPrompt)
		}
	}
	return goal, nil
}

// wakeGate reports whether a tenant has agents enabled, for the wake tick.
func agentWakeGate(settings workspace.AgentSettings) func(context.Context, string) bool {
	return func(ctx context.Context, tenant string) bool {
		if settings == nil {
			return false
		}
		enabled, err := settings.AgentsEnabled(ctx, values.TenantId(tenant))
		return err == nil && enabled
	}
}
