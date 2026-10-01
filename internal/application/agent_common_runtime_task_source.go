package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const commonAgentTaskSourcePrefix = "agent-task:"

// CommonAgentTaskOccurrence pins a capability step the task owner reached.
// JSON encoding avoids ambiguities when native IDs contain punctuation.
type CommonAgentTaskOccurrence struct {
	TaskID     string               `json:"task_id"`
	StepID     string               `json:"step_id"`
	PlanDigest string               `json:"plan_digest"`
	Skill      agentskills.SkillPin `json:"skill"`
}

func CommonAgentTaskSourceRef(occurrence CommonAgentTaskOccurrence) (string, error) {
	if !runAuthorityCleanRequired(occurrence.TaskID, 256) || !runAuthorityCleanRequired(occurrence.StepID, 256) ||
		!personaRunAuthorityDigest(occurrence.PlanDigest) || !runAuthorityCleanRequired(occurrence.Skill.ID, 256) ||
		occurrence.Skill.Version == 0 || !personaRunAuthorityDigest("sha256:"+occurrence.Skill.Digest) ||
		strings.ContainsAny(occurrence.TaskID+occurrence.StepID+occurrence.Skill.ID, "\r\n\x00") {
		return "", agentrun.ErrInvalidRequest
	}
	raw, err := json.Marshal(occurrence)
	if err != nil {
		return "", err
	}
	ref := commonAgentTaskSourcePrefix + base64.RawURLEncoding.EncodeToString(raw)
	if len(ref) > 512 {
		return "", agentrun.ErrInvalidRequest
	}
	return ref, nil
}

// CommonAgentTaskSourceAuthority reads the real executing task, current
// confirmed step and freshly verified durable delegated credential.
type CommonAgentTaskSourceAuthority struct{ Platform *agentsystem.Platform }

func (a CommonAgentTaskSourceAuthority) CheckRequest(ctx context.Context, request agentrun.Request) error {
	if a.Platform == nil || ctx == nil || request.Source.Kind != agentrun.SourceAPI || request.Persona != nil ||
		request.Principal.Mode != agentrun.ModeOnBehalfOf || !strings.HasPrefix(request.Source.Ref, commonAgentTaskSourcePrefix) {
		return commonAgentRefusal("TASK_SOURCE_DENIED")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(request.Source.Ref, commonAgentTaskSourcePrefix))
	if err != nil || len(raw) > 4096 {
		return commonAgentRefusal("TASK_SOURCE_DENIED")
	}
	var occurrence CommonAgentTaskOccurrence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&occurrence) != nil || decoder.Decode(new(any)) != io.EOF {
		return commonAgentRefusal("TASK_SOURCE_DENIED")
	}
	canonical, err := CommonAgentTaskSourceRef(occurrence)
	if err != nil || canonical != request.Source.Ref || request.CauseID != occurrence.TaskID {
		return commonAgentRefusal("TASK_SOURCE_DENIED")
	}
	source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	if err != nil || source != request.Source {
		return commonAgentRefusal("TASK_SOURCE_DENIED")
	}
	runner, err := a.Platform.ForTenant(ctx, values.TenantId(request.Source.TenantID))
	if err != nil {
		return err
	}
	current, err := runner.ResolveTaskInvocation(ctx, occurrence.TaskID, occurrence.StepID, request.Principal.InvokerID, occurrence.Skill)
	if err != nil {
		return commonAgentRefusal("TASK_SOURCE_NOT_CURRENT")
	}
	if current.Task.Plan.Digest != occurrence.PlanDigest || current.Grant.TaskID != occurrence.TaskID ||
		current.Grant.GrantID != request.Principal.DelegatedCredentialRef || current.Grant.TargetAgentID != request.Agent.AgentID ||
		!commonAgentGrantVersionMatches(current.Grant.AgentVersion, request.Agent) || current.Grant.InstallationID != request.InstallationID ||
		current.Grant.Purpose != request.Purpose || request.Deadline.After(current.Task.ExpiresAt) {
		return commonAgentRefusal("TASK_SOURCE_CHANGED")
	}
	return nil
}
