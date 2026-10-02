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

// Rewrite returns only the text of a rewrite whose model verdict kept the
// meaning; a rewrite the model itself disowns is a preservation failure.
func (m ChattoneGatewayModel) Rewrite(ctx context.Context, prompt chatrewrite.Prompt) (string, error) {
	checked, err := m.RewriteChecked(ctx, prompt)
	if err != nil {
		return "", err
	}
	if !checked.MeaningPreserved {
		return "", chatrewrite.ErrPreservation
	}
	return checked.Text, nil
}

// RewriteChecked is the one structured tone-rewrite call: the model returns the
// rewritten text and its verdict on whether the meaning survived, in the typed
// result agentmodel.ChattoneRewrite. The service reads the verdict first.
func (m ChattoneGatewayModel) RewriteChecked(ctx context.Context, prompt chatrewrite.Prompt) (chatrewrite.Checked, error) {
	if m.Gateway == nil || m.Binding == nil || !prompt.Identity.Valid() || prompt.TaskProfile != chatrewrite.TaskProfileID {
		return chatrewrite.Checked{}, chatrewrite.ErrUnavailable
	}
	req, err := m.Binding.BindWritingStyle(ctx, prompt.Identity, ChattoneTaskProfile())
	if err != nil {
		return chatrewrite.Checked{}, err
	}
	if req.TenantID != prompt.Identity.Tenant || req.Dispatch.Outbound.Tenant != prompt.Identity.Tenant || req.Dispatch.Outbound.Principal != prompt.Identity.Person || req.Route.Task.ID != chatrewrite.TaskProfileID || req.Route.Pin.TaskProfileID != chatrewrite.TaskProfileID {
		return chatrewrite.Checked{}, chatrewrite.ErrUnavailable
	}
	// The typed result is the call's contract whatever the binding built.
	output, err := agentmodel.ChattoneRewriteOutput()
	if err != nil {
		return chatrewrite.Checked{}, chatrewrite.ErrUnavailable
	}
	req.Dispatch.Model.Output = output
	req.Dispatch.Model.RequiredFeatures = []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}
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
			return chatrewrite.Checked{}, chatrewrite.ErrLimit
		}
		return chatrewrite.Checked{}, err
	}
	rewrite, err := agentmodel.DecodeChattoneRewrite(result.Dispatch.Model)
	if err != nil {
		return chatrewrite.Checked{}, chatrewrite.ErrUnavailable
	}
	return chatrewrite.Checked{Text: rewrite.Text, MeaningPreserved: rewrite.MeaningPreserved}, nil
}

var _ chatrewrite.CheckedModel = ChattoneGatewayModel{}
