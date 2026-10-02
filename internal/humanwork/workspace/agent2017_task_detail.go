package workspace

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

// AgentTaskDetailConfig carries the task view sections the server derives
// from the task's own record: saved progress, what the task produced, what it
// submitted and what a new plan revision changes. Every value is a kind, a
// code or a label the task's owner already sees; no ledger reference, source
// identifier or step content is in it.
type AgentTaskDetailConfig struct {
	Checkpoints      []AgentCheckpointConfig `json:"checkpoints,omitempty"`
	Artifacts        []AgentArtifactConfig   `json:"artifacts,omitempty"`
	SubmittedIntents []AgentIntentConfig     `json:"submitted_intents,omitempty"`
	PlanChanges      []AgentPlanChangeConfig `json:"plan_changes,omitempty"`
}

// AgentCheckpointConfig is one point where the task's progress was saved.
type AgentCheckpointConfig struct {
	Kind  string `json:"kind,omitempty"`
	Step  string `json:"step,omitempty"`
	Label string `json:"label,omitempty"`
	At    string `json:"at,omitempty"`
}

// AgentArtifactConfig is one thing the task produced.
type AgentArtifactConfig struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	Href string `json:"href,omitempty"`
}

// AgentIntentConfig is one governed submission of the plan and its status.
type AgentIntentConfig struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// AgentPlanChangeConfig is one step a new plan revision adds or drops.
type AgentPlanChangeConfig struct {
	Change string `json:"change"`
	Step   string `json:"step"`
	Tier   string `json:"tier,omitempty"`
}

// agentTaskDetailConfig is nil for a task with none of these sections, so an
// ordinary quick answer adds nothing to the page document.
func agentTaskDetailConfig(task productui.AgentTask) *AgentTaskDetailConfig {
	if len(task.Checkpoints) == 0 && len(task.Artifacts) == 0 && len(task.SubmittedIntents) == 0 && len(task.PlanChanges) == 0 {
		return nil
	}
	detail := &AgentTaskDetailConfig{}
	for _, checkpoint := range task.Checkpoints {
		detail.Checkpoints = append(detail.Checkpoints, AgentCheckpointConfig{Kind: checkpoint.Kind, Step: checkpoint.Step, Label: checkpoint.Label, At: checkpoint.At})
	}
	for _, artifact := range task.Artifacts {
		detail.Artifacts = append(detail.Artifacts, AgentArtifactConfig{Kind: artifact.Kind, Name: artifact.Name, Href: artifact.Href})
	}
	for _, intent := range task.SubmittedIntents {
		detail.SubmittedIntents = append(detail.SubmittedIntents, AgentIntentConfig{Name: intent.Name, Status: intent.Status})
	}
	for _, change := range task.PlanChanges {
		detail.PlanChanges = append(detail.PlanChanges, AgentPlanChangeConfig{Change: change.Change, Step: change.Step.Name, Tier: change.Step.Tier})
	}
	return detail
}

// apply puts the detail back on the page contract. The browser client makes
// the same conversion from the same document.
func (d *AgentTaskDetailConfig) apply(task *productui.AgentTask) {
	if d == nil {
		return
	}
	for _, checkpoint := range d.Checkpoints {
		task.Checkpoints = append(task.Checkpoints, productui.AgentCheckpoint{Kind: checkpoint.Kind, Step: checkpoint.Step, Label: checkpoint.Label, At: checkpoint.At})
	}
	for _, artifact := range d.Artifacts {
		task.Artifacts = append(task.Artifacts, productui.AgentArtifact{Kind: artifact.Kind, Name: artifact.Name, Href: artifact.Href})
	}
	for _, intent := range d.SubmittedIntents {
		task.SubmittedIntents = append(task.SubmittedIntents, productui.AgentIntentStatus{Name: intent.Name, Status: intent.Status})
	}
	for _, change := range d.PlanChanges {
		task.PlanChanges = append(task.PlanChanges, productui.AgentPlanChange{Change: change.Change, Step: productui.AgentTaskStep{Name: change.Step, Tier: change.Tier}})
	}
}
