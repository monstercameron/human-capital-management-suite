// Package agents is the thin gRPC boundary for the browser Agents page
// (UXBLIND-122): it turns the admin's tenant toggle and a user's task prompt
// into port calls. Tenant, actor and user come only from the verified
// transport principal; a request never names them.
package agents

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
)

// MaxPromptBytes bounds a task prompt at the boundary, before any port runs.
const MaxPromptBytes = 8 << 10

// Settings is the tenant agents setting port.
type Settings interface {
	AgentsEnabled(ctx context.Context, tenant values.TenantId) (bool, error)
	SetAgentsEnabled(ctx context.Context, tenant values.TenantId, enabled bool, actor string) error
}

// Admin answers whether the principal may change the tenant agents setting.
// A non-nil error means the decision could not be made and refuses the call.
type Admin func(ctx context.Context, principal *trust.Principal) (bool, error)

// Dependencies are the ports the service calls. Settings and Admin are
// required for SetAgentsEnabled; Starter for StartAgentTask.
type Dependencies struct {
	Settings   Settings
	Admin      Admin
	Starter    agentclient.Starter
	Controller agentclient.Controller
}

// Server implements hcmnext.agent.v1.AgentService.
type Server struct {
	agentv1.UnimplementedAgentServiceServer
	deps Dependencies
}

// NewServer constructs the service.
func NewServer(deps Dependencies) *Server { return &Server{deps: deps} }

// Register installs the service on a gRPC server. It registers nothing when
// there is no settings port, so a cell without agents exposes no method.
func Register(server *grpc.Server, deps Dependencies) {
	if server != nil && deps.Settings != nil {
		agentv1.RegisterAgentServiceServer(server, NewServer(deps))
	}
}

var _ agentv1.AgentServiceServer = (*Server)(nil)

func human(ctx context.Context) (*trust.Principal, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, refuse(envelope.CodeUnauthenticated, "agents.unauthenticated", "sign in to continue")
	}
	if principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, refuse(envelope.CodePermissionDenied, "agents.human_only", "this action is for signed-in people")
	}
	return principal, nil
}

// SetAgentsEnabled changes the caller's own tenant setting when the durable
// role policy lets the caller change Chat settings.
func (s *Server) SetAgentsEnabled(ctx context.Context, in *agentv1.SetAgentsEnabledRequest) (*agentv1.SetAgentsEnabledResponse, error) {
	if in == nil {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.request_required", "request is required")
	}
	principal, err := human(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.deps.Settings == nil || s.deps.Admin == nil {
		return nil, refuse(envelope.CodeUnavailable, "agents.settings_unavailable", "agent settings are unavailable")
	}
	allowed, err := s.deps.Admin(ctx, principal)
	if err != nil {
		return nil, refuse(envelope.CodeUnavailable, "agents.authorization_unavailable", "authorization is unavailable")
	}
	if !allowed {
		return nil, refuse(envelope.CodePermissionDenied, "agents.setting_not_authorized", "changing the agents setting is not authorized")
	}
	if err := s.deps.Settings.SetAgentsEnabled(ctx, principal.Tenant(), in.GetEnabled(), principal.Subject()); err != nil {
		return nil, refuse(envelope.CodeUnavailable, "agents.setting_not_saved", "the agents setting could not be saved")
	}
	return &agentv1.SetAgentsEnabledResponse{Enabled: in.GetEnabled()}, nil
}

// StartAgentTask starts one read-only task for the signed-in user.
func (s *Server) StartAgentTask(ctx context.Context, in *agentv1.StartAgentTaskRequest) (*agentv1.StartAgentTaskResponse, error) {
	if in == nil {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.request_required", "request is required")
	}
	principal, err := human(ctx)
	if err != nil {
		return nil, err
	}
	prompt := in.GetPrompt()
	if len(prompt) > MaxPromptBytes || strings.TrimSpace(prompt) == "" || !utf8.ValidString(prompt) {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.prompt_invalid", "the task prompt is empty or too long")
	}
	if s == nil || s.deps.Starter == nil {
		return nil, refuse(envelope.CodeUnavailable, "agents.unavailable", "agents are unavailable")
	}
	mode := agentclient.StartQuickAnswer
	if in.GetMode() == agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK {
		mode = agentclient.StartLongTask
	} else if in.GetMode() != agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER && in.GetMode() != agentv1.AgentStartMode_AGENT_START_MODE_UNSPECIFIED {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.mode_invalid", "the task start mode is invalid")
	}
	var started agentclient.StartedTask
	if modeStarter, ok := s.deps.Starter.(agentclient.ModeStarter); ok {
		started, err = modeStarter.StartTaskMode(ctx, principal, prompt, mode)
	} else if mode == agentclient.StartLongTask {
		return nil, refuse(envelope.CodeUnavailable, "agents.mode_unavailable", "long task mode is unavailable")
	} else {
		started, err = s.deps.Starter.StartTask(ctx, principal, prompt)
	}
	if err != nil {
		return nil, startError(err)
	}
	return &agentv1.StartAgentTaskResponse{TaskId: started.ID, State: started.State, Version: started.Version}, nil
}

// ControlAgentTask applies one owner-scoped CAS action to a durable task.
func (s *Server) ControlAgentTask(ctx context.Context, in *agentv1.ControlAgentTaskRequest) (*agentv1.ControlAgentTaskResponse, error) {
	if in == nil || strings.TrimSpace(in.GetTaskId()) == "" || in.GetExpectedVersion() == 0 {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.control_invalid", "task and expected version are required")
	}
	principal, err := human(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.deps.Controller == nil {
		return nil, refuse(envelope.CodeUnavailable, "agents.control_unavailable", "agent controls are unavailable")
	}
	action, ok := controlAction(in.GetAction())
	if !ok {
		return nil, refuse(envelope.CodeInvalidArgument, "agents.control_action_invalid", "task action is required")
	}
	result, err := s.deps.Controller.ControlTask(ctx, principal, agentclient.TaskControl{TaskID: in.GetTaskId(), ExpectedVersion: in.GetExpectedVersion(), Action: action})
	if err != nil {
		return nil, controlError(err)
	}
	return &agentv1.ControlAgentTaskResponse{TaskId: result.ID, State: result.State, Version: result.Version}, nil
}

func controlAction(action agentv1.AgentTaskAction) (agentclient.TaskAction, bool) {
	switch action {
	case agentv1.AgentTaskAction_AGENT_TASK_ACTION_CONFIRM_PLAN:
		return agentclient.TaskConfirmPlan, true
	case agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE:
		return agentclient.TaskPause, true
	case agentv1.AgentTaskAction_AGENT_TASK_ACTION_RESUME:
		return agentclient.TaskResume, true
	case agentv1.AgentTaskAction_AGENT_TASK_ACTION_CANCEL:
		return agentclient.TaskCancel, true
	default:
		return "", false
	}
}

func controlError(err error) error {
	switch {
	case errors.Is(err, agentclient.ErrDisabled):
		return refuse(envelope.CodeFailedPrecondition, "agents.disabled", "agents are not turned on for this organization")
	case errors.Is(err, agentclient.ErrNotAuthorized), errors.Is(err, agentsystem.ErrDenied):
		return refuse(envelope.CodePermissionDenied, "agents.control_not_authorized", "you may not control this task")
	case errors.Is(err, agentrun.ErrConflict):
		return refuse(envelope.CodeAborted, "agents.task_conflict", "the task changed; refresh and try again")
	case errors.Is(err, agentrun.ErrTerminal):
		return refuse(envelope.CodeFailedPrecondition, "agents.task_terminal", "the task has already ended")
	default:
		return refuse(envelope.CodeUnavailable, "agents.control_failed", "the task action could not be applied")
	}
}

// startError maps a start failure to a status whose message carries no
// detail from the failure.
func startError(err error) error {
	switch {
	case errors.Is(err, agentclient.ErrDisabled):
		return refuse(envelope.CodeFailedPrecondition, "agents.disabled", "agents are not turned on for this organization")
	case errors.Is(err, agentclient.ErrInvalidPrompt):
		return refuse(envelope.CodeInvalidArgument, "agents.prompt_invalid", "the task prompt is empty or too long")
	case errors.Is(err, agentclient.ErrNotAuthorized):
		return refuse(envelope.CodePermissionDenied, "agents.start_not_authorized", "you may not start agent tasks")
	case errors.Is(err, context.Canceled):
		return refuse(envelope.CodeUnavailable, "agents.canceled", "the request was canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return refuse(envelope.CodeDeadlineExceeded, "agents.deadline", "the request took too long")
	default:
		return refuse(envelope.CodeUnavailable, "agents.start_failed", "the task could not be started")
	}
}

// refuse builds an owned error. The owned model is what the admission chain
// projects to the wire: a plain gRPC status would be flattened to a generic
// unavailable, so a refusal must be an owned condition to keep its code.
func refuse(code envelope.Code, reason, message string) error {
	return envelope.New(code, reason, message)
}
