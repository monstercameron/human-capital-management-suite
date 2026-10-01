package journeyclient

// Agents mirrors internal/humanwork/workspace.AgentsConfig: the server's
// agents availability projection for the signed-in viewer (UXBLIND-122).
// It is display state only; the settings write is authorized by the server.
type Agents struct {
	StartAvailable         bool        `json:"start_available"`
	StartUnavailableReason string      `json:"start_unavailable_reason,omitempty"`
	Enabled                bool        `json:"enabled"`
	ViewerIsAdmin          bool        `json:"viewer_is_admin"`
	Reason                 string      `json:"reason,omitempty"`
	SettingsHref           string      `json:"settings_href,omitempty"`
	Service                string      `json:"service,omitempty"`
	Tasks                  []AgentTask `json:"tasks,omitempty"`
}

// AgentTask mirrors workspace.AgentTaskConfig.
type AgentTask struct {
	ID           string                `json:"id"`
	Version      uint64                `json:"version"`
	Title        string                `json:"title"`
	Goal         string                `json:"goal,omitempty"`
	State        string                `json:"state"`
	AnswerText   string                `json:"answer_text,omitempty"`
	LiveStep     string                `json:"live_step,omitempty"`
	BudgetUsed   string                `json:"budget_used,omitempty"`
	BudgetLimit  string                `json:"budget_limit,omitempty"`
	PlanRevision string                `json:"plan_revision,omitempty"`
	Steps        []AgentStep           `json:"steps,omitempty"`
	Approvals    []AgentApproval       `json:"approvals,omitempty"`
	Actions      AgentTaskActionPolicy `json:"actions"`
}

// AgentTaskActionPolicy mirrors the server's fail-closed action projection.
type AgentTaskActionPolicy struct {
	ConfirmPlan bool `json:"confirm_plan"`
	Pause       bool `json:"pause"`
	Resume      bool `json:"resume"`
	Cancel      bool `json:"cancel"`
}

// AgentStep mirrors workspace.AgentStepConfig.
type AgentStep struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Tier  string `json:"tier"`
}

// AgentApproval mirrors workspace.AgentApprovalConfig.
type AgentApproval struct {
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Summary string `json:"summary"`
}
