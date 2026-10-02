package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ChattoneGatewayBinding resolves a fresh task-bound route, pin, source evidence and
// single-use lease. The prompt is supplied by the rewrite service, not by an HTTP client.
type ChattoneGatewayBinding interface {
	BindWritingStyle(context.Context, chatrewrite.Identity, agentmodel.TaskProfile) (AgentModelGatewayRequest, error)
}
type ChattoneGateway interface {
	Dispatch(context.Context, AgentModelGatewayRequest) (AgentModelGatewayResult, error)
}
type ChattoneGatewayModel struct {
	Gateway ChattoneGateway
	Binding ChattoneGatewayBinding
}

func ChattoneTaskProfile() agentmodel.TaskProfile {
	return agentmodel.TaskProfile{ID: chatrewrite.TaskProfileID, MaxLatency: 3 * time.Second, MaxCostMicros: 20000, DataClasses: []string{string(dlp.ClassPublic), string(dlp.ClassInternal)}, SemanticsDigest: "chat-writing-style-preservation-v1"}
}
func (m ChattoneGatewayModel) Rewrite(ctx context.Context, prompt chatrewrite.Prompt) (string, error) {
	if m.Gateway == nil || m.Binding == nil || !prompt.Identity.Valid() || prompt.TaskProfile != chatrewrite.TaskProfileID {
		return "", chatrewrite.ErrUnavailable
	}
	req, err := m.Binding.BindWritingStyle(ctx, prompt.Identity, ChattoneTaskProfile())
	if err != nil {
		return "", err
	}
	if req.TenantID != prompt.Identity.Tenant || req.Dispatch.Outbound.Tenant != prompt.Identity.Tenant || req.Dispatch.Outbound.Principal != prompt.Identity.Person || req.Route.Task.ID != chatrewrite.TaskProfileID || req.Route.Pin.TaskProfileID != chatrewrite.TaskProfileID {
		return "", chatrewrite.ErrUnavailable
	}
	req.Dispatch.Model.TaskProfile = chatrewrite.TaskProfileID
	req.Dispatch.Model.Messages = []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: prompt.Instruction}, {Role: agentmodel.RoleUser, Content: prompt.Data}}
	req.Dispatch.Model.Tools = nil
	// Every message must be present verbatim in the classified outbound field
	// set, with a source class the provider terms approve, or egress refuses it.
	provenance := []string{"chat-writing-style:" + req.Route.TraceID}
	// Both fields are human-written (the instruction may carry an administrator's
	// house style); neither is canonical fact.
	taint := []string{string(agentsecurity.TaintHuman)}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.0", "model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{
		{Name: "model.message.0", Value: prompt.Instruction, Class: dlp.ClassInternal, Taint: taint, Provenance: provenance},
		{Name: "model.message.1", Value: prompt.Data, Class: dlp.ClassInternal, Taint: taint, Provenance: provenance},
	}
	req.Dispatch.FieldSources = map[string]string{"model.message.0": chattoneSourceInstruction, "model.message.1": chattoneSourceDraft}
	result, err := m.Gateway.Dispatch(context.WithValue(ctx, chattoneEvidenceKey{}, req), req)
	if err != nil {
		// A spent per-person, per-tenant or per-platform budget is the writer's
		// plain "limit", not an outage.
		if errors.Is(err, agentbudget.ErrPaused) {
			return "", chatrewrite.ErrLimit
		}
		return "", err
	}
	if result.Dispatch.Model.Refusal != nil || result.Dispatch.Model.Failure != nil || result.Dispatch.Model.Finish != agentmodel.FinishComplete || len(result.Dispatch.Model.ToolProposals) != 0 || len(result.Dispatch.Model.RequestedActions) != 0 {
		return "", chatrewrite.ErrUnavailable
	}
	return result.Dispatch.Model.Text, nil
}
