package workspace

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentSettings is the tenant agents setting. Agents are off until an
// administrator turns them on; a tenant with no stored value is off.
type AgentSettings interface {
	AgentsEnabled(ctx context.Context, tenant values.TenantId) (bool, error)
	SetAgentsEnabled(ctx context.Context, tenant values.TenantId, enabled bool, actor string) error
}

// Agent service states carried by the island when agents are on.
const (
	agentServiceAvailable   = "available"
	agentServiceUnavailable = "unavailable"
)

// AgentsConfig is the JSON-island form of productui.AgentsAvailabilityProjection.
// Tasks are the signed-in user's own tasks and are present only when agents
// are on; they never carry ledger entries, tainted content or unsanitized
// failure detail.
type AgentsConfig struct {
	StartAvailable         bool                 `json:"start_available"`
	StartUnavailableReason string               `json:"start_unavailable_reason,omitempty"`
	Enabled                bool                 `json:"enabled"`
	ViewerIsAdmin          bool                 `json:"viewer_is_admin"`
	Reason                 string               `json:"reason,omitempty"`
	SettingsHref           string               `json:"settings_href,omitempty"`
	Service                string               `json:"service,omitempty"`
	Agents                 []AgentSummaryConfig `json:"agents,omitempty"`
	Tasks                  []AgentTaskConfig    `json:"tasks,omitempty"`
}

// AgentSummaryConfig is one named agent choice that the viewer may invoke.
type AgentSummaryConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

// AgentTaskConfig is one owner-scoped task row.
type AgentTaskConfig struct {
	ID                        string                             `json:"id"`
	Version                   uint64                             `json:"version"`
	Title                     string                             `json:"title"`
	Goal                      string                             `json:"goal,omitempty"`
	AnswerText                string                             `json:"answer_text,omitempty"`
	State                     string                             `json:"state"`
	LiveStep                  string                             `json:"live_step,omitempty"`
	BudgetUsed                string                             `json:"budget_used,omitempty"`
	BudgetLimit               string                             `json:"budget_limit,omitempty"`
	PlanRevision              string                             `json:"plan_revision,omitempty"`
	Steps                     []AgentStepConfig                  `json:"steps,omitempty"`
	Approvals                 []AgentApprovalConfig              `json:"approvals,omitempty"`
	Actions                   AgentTaskActionPolicy              `json:"actions"`
	CreatedAt                 string                             `json:"created_at,omitempty"`
	UpdatedAt                 string                             `json:"updated_at,omitempty"`
	ResultPreview             string                             `json:"result_preview,omitempty"`
	FailureReason             string                             `json:"failure_reason,omitempty"`
	Retryable                 bool                               `json:"retryable,omitempty"`
	AnsweringAgentID          string                             `json:"answering_agent_id,omitempty"`
	AnsweringAgentDisplayName string                             `json:"answering_agent_display_name,omitempty"`
	AnsweringAgentVersion     string                             `json:"answering_agent_version,omitempty"`
	Documents                 []AgentTaskDocumentReferenceConfig `json:"document_references,omitempty"`
	UsedDocuments             []AgentTaskDocumentReferenceConfig `json:"used_document_references,omitempty"`
	DocumentUsageState        string                             `json:"document_usage_state,omitempty"`
	DocumentOmissions         []AgentTaskDocumentOmissionConfig  `json:"document_omissions,omitempty"`
}

type AgentTaskDocumentReferenceConfig struct {
	DocumentID    string `json:"document_id"`
	Label         string `json:"label,omitempty"`
	SectionAnchor string `json:"section_anchor,omitempty"`
}

type AgentTaskDocumentOmissionConfig struct {
	Label  string `json:"label,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// AgentTaskActionPolicy is server-derived affordance state. It is advisory
// projection only; every action is rechecked by the control RPC.
type AgentTaskActionPolicy struct {
	ConfirmPlan bool `json:"confirm_plan"`
	Pause       bool `json:"pause"`
	Resume      bool `json:"resume"`
	Cancel      bool `json:"cancel"`
}

// AgentStepConfig is one plan step.
type AgentStepConfig struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	Tier          string `json:"tier"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

// AgentApprovalConfig is one step awaiting the user's approval.
type AgentApprovalConfig struct {
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Summary string `json:"summary"`
}

// agentsAdmin is the viewer's authority over the tenant agents setting: the
// same authority as the Chat settings page that hosts it.
func agentsAdmin(access productAccess) bool {
	if access.configured {
		return access.can(productui.PageChatSettings, roleaccess.ActionUpdate)
	}
	return access.can(productui.PageChatSettings, roleaccess.ActionView)
}

// resolveAgents composes the viewer's availability projection. Every input
// is server-derived: the tenant and subject from the verified principal, the
// administrator bit from the durable role policy, and the setting from the
// tenant store. A failed setting read keeps agents off.
func (h *Handler) resolveAgents(ctx context.Context, principal *trust.Principal, access productAccess) *AgentsConfig {
	if principal == nil {
		return nil
	}
	config := &AgentsConfig{ViewerIsAdmin: agentsAdmin(access)}
	if h.agentSettings != nil {
		enabled, err := h.agentSettings.AgentsEnabled(ctx, principal.Tenant())
		config.Enabled = err == nil && enabled
	}
	if !config.Enabled {
		if config.ViewerIsAdmin {
			config.Reason = productui.AgentsReasonTenantDisabled
			config.SettingsHref = productui.AgentsSettingsHref()
		}
		return config
	}
	config.Service = agentServiceUnavailable
	if h.agents == nil {
		return config
	}
	snapshot, err := h.agents.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()})
	if err != nil || snapshot.Availability != productui.AgentsAvailable {
		return config
	}
	config.Service = agentServiceAvailable
	config.StartAvailable, config.StartUnavailableReason = snapshot.StartAvailable, snapshot.StartUnavailableReason
	config.Agents = make([]AgentSummaryConfig, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		config.Agents = append(config.Agents, AgentSummaryConfig{ID: agent.ID, Name: agent.Name, Description: agent.Description, Status: agent.Status})
	}
	config.Tasks = make([]AgentTaskConfig, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		row := AgentTaskConfig{
			ID: task.ID, Version: task.Version, Title: task.Title, Goal: task.Goal, AnswerText: task.AnswerText, State: string(task.State), LiveStep: task.LiveStep,
			BudgetUsed: task.BudgetUsed, BudgetLimit: task.BudgetLimit, PlanRevision: task.PlanRevision,
			Actions: AgentTaskActionPolicy{ConfirmPlan: task.Actions.ConfirmPlan, Pause: task.Actions.Pause, Resume: task.Actions.Resume, Cancel: task.Actions.Cancel},
		}
		if !task.CreatedAt.IsZero() {
			row.CreatedAt = task.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		if !task.UpdatedAt.IsZero() {
			row.UpdatedAt = task.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		row.ResultPreview, row.FailureReason, row.Retryable = task.ResultPreview, task.FailureReason, task.Retryable
		row.AnsweringAgentID, row.AnsweringAgentDisplayName, row.AnsweringAgentVersion = task.AnsweringAgentID, task.AnsweringAgentDisplayName, task.AnsweringAgentVersion
		row.DocumentUsageState = string(task.DocumentUsageState)
		for _, reference := range task.Documents {
			row.Documents = append(row.Documents, AgentTaskDocumentReferenceConfig{DocumentID: reference.DocumentID, Label: reference.Label, SectionAnchor: reference.SectionAnchor})
		}
		for _, reference := range task.UsedDocuments {
			row.UsedDocuments = append(row.UsedDocuments, AgentTaskDocumentReferenceConfig{DocumentID: reference.DocumentID, Label: reference.Label, SectionAnchor: reference.SectionAnchor})
		}
		for _, omission := range task.DocumentOmissions {
			row.DocumentOmissions = append(row.DocumentOmissions, AgentTaskDocumentOmissionConfig{Label: omission.Label, Reason: omission.Reason})
		}
		if policyClient, ok := h.agents.(agentclient.PolicyReader); ok {
			policy := policyClient.TaskPolicy(ctx, string(principal.Tenant()), principal.Subject(), task.ID)
			row.Actions = AgentTaskActionPolicy{ConfirmPlan: policy.ConfirmPlan, Pause: policy.Pause, Resume: policy.Resume, Cancel: policy.Cancel}
		}
		for _, step := range task.Steps {
			item := AgentStepConfig{Name: step.Name, State: step.State, Tier: step.Tier, FailureReason: step.FailureReason}
			if !step.StartedAt.IsZero() {
				item.StartedAt = step.StartedAt.UTC().Format(time.RFC3339Nano)
			}
			if !step.FinishedAt.IsZero() {
				item.FinishedAt = step.FinishedAt.UTC().Format(time.RFC3339Nano)
			}
			row.Steps = append(row.Steps, item)
		}
		for _, approval := range task.Approvals {
			row.Approvals = append(row.Approvals, AgentApprovalConfig{ID: approval.ID, Digest: approval.Digest, Summary: approval.Summary})
		}
		config.Tasks = append(config.Tasks, row)
	}
	return config
}

// ProductAgentsAvailability converts the island form into the page contract.
// It is the same conversion the browser client performs, so the server's
// loading shell and the hydrated page agree.
func ProductAgentsAvailability(config *AgentsConfig) productui.AgentsAvailabilityProjection {
	if config == nil {
		return productui.NormalizeAgentsAvailability(productui.AgentsAvailabilityProjection{})
	}
	projection := productui.AgentsAvailabilityProjection{
		Enabled: config.Enabled, ViewerIsAdmin: config.ViewerIsAdmin, ReasonKey: config.Reason, SettingsHref: config.SettingsHref,
	}
	if config.Enabled {
		projection.Snapshot.StartAvailable, projection.Snapshot.StartUnavailableReason = config.StartAvailable, config.StartUnavailableReason
		projection.Snapshot.Availability = productui.AgentsUnavailable
		if config.Service == agentServiceAvailable {
			projection.Snapshot.Availability = productui.AgentsAvailable
		}
		for _, agent := range config.Agents {
			projection.Snapshot.Agents = append(projection.Snapshot.Agents, productui.AgentSummary{ID: agent.ID, Name: agent.Name, Description: agent.Description, Status: agent.Status})
		}
		for _, task := range config.Tasks {
			item := productui.AgentTask{
				ID: task.ID, Version: task.Version, Title: task.Title, Goal: task.Goal, AnswerText: task.AnswerText, State: productui.AgentTaskState(task.State), LiveStep: task.LiveStep,
				BudgetUsed: task.BudgetUsed, BudgetLimit: task.BudgetLimit, PlanRevision: task.PlanRevision,
				Actions: productui.AgentTaskActionPolicy{ConfirmPlan: task.Actions.ConfirmPlan, Pause: task.Actions.Pause, Resume: task.Actions.Resume, Cancel: task.Actions.Cancel},
			}
			item.CreatedAt, _ = time.Parse(time.RFC3339Nano, task.CreatedAt)
			item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, task.UpdatedAt)
			item.ResultPreview, item.FailureReason, item.Retryable = task.ResultPreview, task.FailureReason, task.Retryable
			item.AnsweringAgentID, item.AnsweringAgentDisplayName, item.AnsweringAgentVersion = task.AnsweringAgentID, task.AnsweringAgentDisplayName, task.AnsweringAgentVersion
			item.DocumentUsageState = productui.AgentDocumentUsageState(task.DocumentUsageState)
			for _, reference := range task.Documents {
				item.Documents = append(item.Documents, productui.AgentTaskDocumentReference{DocumentID: reference.DocumentID, Label: reference.Label, SectionAnchor: reference.SectionAnchor})
			}
			for _, reference := range task.UsedDocuments {
				item.UsedDocuments = append(item.UsedDocuments, productui.AgentTaskDocumentReference{DocumentID: reference.DocumentID, Label: reference.Label, SectionAnchor: reference.SectionAnchor})
			}
			for _, omission := range task.DocumentOmissions {
				item.DocumentOmissions = append(item.DocumentOmissions, productui.AgentTaskDocumentOmission{Label: omission.Label, Reason: omission.Reason})
			}
			for _, step := range task.Steps {
				projected := productui.AgentTaskStep{Name: step.Name, State: step.State, Tier: step.Tier, FailureReason: step.FailureReason}
				projected.StartedAt, _ = time.Parse(time.RFC3339Nano, step.StartedAt)
				projected.FinishedAt, _ = time.Parse(time.RFC3339Nano, step.FinishedAt)
				item.Steps = append(item.Steps, projected)
			}
			for _, approval := range task.Approvals {
				item.Approvals = append(item.Approvals, productui.AgentApproval{ID: approval.ID, Digest: approval.Digest, Summary: approval.Summary})
			}
			projection.Snapshot.Tasks = append(projection.Snapshot.Tasks, item)
		}
	}
	return productui.NormalizeAgentsAvailability(projection)
}

// CanChangeAgentsSetting reports whether the principal may change the tenant
// agents setting: the same durable-role decision the workspace uses to show
// the Chat settings toggle. The setting is written over the workspace gRPC
// tunnel (hcmnext.agent.v1.AgentService), so the transport asks this instead
// of re-deriving the authority. A role store failure is returned so the
// caller can refuse without guessing.
func CanChangeAgentsSetting(ctx context.Context, store roleaccess.Store, principal *trust.Principal) (bool, error) {
	if principal == nil {
		return false, nil
	}
	access, err := (&Handler{roleAccess: store}).resolveProductAccess(ctx, principal)
	if err != nil {
		return false, err
	}
	return agentsAdmin(access), nil
}
