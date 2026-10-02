package agentclient

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// PlanHistoryReader is the optional read of the plan a task had before its
// current revision. *agentsystem.Runner satisfies it. It is asked only for a
// task whose new revision waits for the owner's confirmation.
type PlanHistoryReader interface {
	PreviousPlan(ctx context.Context, taskID, userID string) (agentrun.HistoricalPlanSnapshot, bool, error)
}

// projectTaskDetail fills the parts of the task view that come from the
// task's own durable record: where its progress was saved, what it produced,
// what each pending approval rests on, what it has submitted, and what a new
// plan revision changes.
//
// It sends kinds, labels and codes only. A ledger reference, a source
// identifier, a destination and the content of any step never leave the
// server: a source is named by the label its owner already sees for the
// document, or by its kind; an artifact gets a link only when it is a
// document the task's owner attached.
func projectTaskDetail(ctx context.Context, task agentrun.AgentTask, reader TaskReader, result *productui.AgentTask) {
	result.Checkpoints = taskCheckpoints(task)
	result.Artifacts = taskArtifacts(task)
	result.SubmittedIntents = taskSubmittedIntents(task)
	for index := range result.Approvals {
		step, ok := approvalStep(task, result.Approvals[index].ID)
		if !ok {
			continue
		}
		result.Approvals[index].Sources, result.Approvals[index].Taint = approvalEvidence(task, step)
	}
	if task.State == agentrun.StateAwaitingPlanConfirmation && task.Plan.Revision > 1 {
		if history, ok := reader.(PlanHistoryReader); ok {
			if previous, found, err := history.PreviousPlan(ctx, task.ID, task.UserID); err == nil && found {
				result.PlanChanges = planChanges(previous, task.Plan)
			}
		}
	}
}

func instant(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

// taskCheckpoints lists, oldest first, the points at which the runtime saved
// the task and could pick it up again: the confirmed plan, each finished
// step, the wake that resumed it, and the wait or pause it is in now.
func taskCheckpoints(task agentrun.AgentTask) []productui.AgentCheckpoint {
	type point struct {
		at    time.Time
		order int
		value productui.AgentCheckpoint
	}
	var points []point
	add := func(at time.Time, value productui.AgentCheckpoint) {
		if at.IsZero() {
			return
		}
		value.At = instant(at)
		points = append(points, point{at: at, order: len(points), value: value})
	}
	if task.Plan.Confirmed {
		add(task.Plan.ConfirmedAt, productui.AgentCheckpoint{Kind: productui.AgentCheckpointPlanConfirmed})
	}
	for _, step := range task.Plan.Steps {
		if step.State == agentrun.StepCompleted {
			add(step.FinishedAt, productui.AgentCheckpoint{Kind: productui.AgentCheckpointStepFinished, Step: stepName(step)})
		}
	}
	if task.LastWake != nil {
		add(task.LastWake.OccurredAt, productui.AgentCheckpoint{Kind: productui.AgentCheckpointResumed})
	}
	switch {
	case task.State == agentrun.StatePaused:
		add(task.UpdatedAt, productui.AgentCheckpoint{Kind: productui.AgentCheckpointPaused})
	case task.Wake != nil && (task.State == agentrun.StateWaiting || task.State == agentrun.StateAwaitingApproval):
		add(task.UpdatedAt, productui.AgentCheckpoint{Kind: waitingCheckpoint(task.Wake.Kind)})
	}
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].at.Equal(points[j].at) {
			return points[i].order < points[j].order
		}
		return points[i].at.Before(points[j].at)
	})
	out := make([]productui.AgentCheckpoint, 0, len(points))
	for _, item := range points {
		out = append(out, item.value)
	}
	return out
}

func waitingCheckpoint(kind agentrun.WakeKind) string {
	switch kind {
	case agentrun.WakeApproval:
		return productui.AgentCheckpointWaitingApproval
	case agentrun.WakeUserReply:
		return productui.AgentCheckpointWaitingReply
	case agentrun.WakeTimer:
		return productui.AgentCheckpointWaitingTimer
	default:
		return productui.AgentCheckpointWaitingUpdate
	}
}

// taskArtifacts lists what the task produced: the artifacts its ledger
// records and the drafts its finished DRAFT steps left. A revoked ledger
// entry is not listed.
func taskArtifacts(task agentrun.AgentTask) []productui.AgentArtifact {
	var out []productui.AgentArtifact
	seen := map[string]bool{}
	for _, entry := range task.Ledger.Entries {
		if entry.Kind != "ARTIFACT" || entry.Revoked || seen[entry.Ref] {
			continue
		}
		seen[entry.Ref] = true
		out = append(out, artifactFor(task, entry.Ref))
	}
	for _, step := range task.Plan.Steps {
		if step.Type != agentrun.StepDraft || step.State != agentrun.StepCompleted || strings.TrimSpace(step.ResultRef) == "" || seen[step.ResultRef] {
			continue
		}
		seen[step.ResultRef] = true
		artifact := artifactFor(task, step.ResultRef)
		if artifact.Kind == productui.AgentArtifactResult {
			artifact.Kind = productui.AgentArtifactDraft
		}
		out = append(out, artifact)
	}
	return out
}

// artifactFor names one produced reference. Only a document the owner
// attached to this task gets a name and a link; the Documents page decides
// again whether the reader may open it.
func artifactFor(task agentrun.AgentTask, ref string) productui.AgentArtifact {
	ref = strings.TrimSpace(ref)
	if id, ok := documentID(ref); ok {
		for _, document := range task.Plan.DocumentReferences {
			if document.DocumentID == id {
				return productui.AgentArtifact{Kind: productui.AgentArtifactDocument, Name: strings.TrimSpace(document.Label), Href: "/workspace/app/docs?document=" + url.QueryEscape(id)}
			}
		}
		return productui.AgentArtifact{Kind: productui.AgentArtifactDocument}
	}
	switch {
	case strings.HasPrefix(ref, "draft:"):
		return productui.AgentArtifact{Kind: productui.AgentArtifactDraft}
	case strings.HasPrefix(ref, "report:"):
		return productui.AgentArtifact{Kind: productui.AgentArtifactReport}
	default:
		return productui.AgentArtifact{Kind: productui.AgentArtifactResult}
	}
}

func documentID(ref string) (string, bool) {
	id, ok := strings.CutPrefix(strings.TrimSpace(ref), "document:")
	if !ok {
		return "", false
	}
	for _, separator := range []string{"/version:", "#"} {
		if before, _, found := strings.Cut(id, separator); found {
			id = before
		}
	}
	id = strings.TrimSpace(id)
	return id, id != ""
}

// taskSubmittedIntents reports each governed submission of the plan (a SUBMIT
// step) with the status the task's own record supports. "Done and checked" is
// claimed only when a later VERIFY step of the plan has completed.
func taskSubmittedIntents(task agentrun.AgentTask) []productui.AgentIntentStatus {
	var out []productui.AgentIntentStatus
	for index, step := range task.Plan.Steps {
		if step.Type != agentrun.StepSubmit {
			continue
		}
		status := productui.AgentIntentDraft
		switch step.State {
		case agentrun.StepAwaitingApproval:
			status = productui.AgentIntentAwaitingApproval
		case agentrun.StepRunning, agentrun.StepWaiting:
			status = productui.AgentIntentExecuting
		case agentrun.StepFailed:
			status = productui.AgentIntentFailed
		case agentrun.StepCompleted:
			status = productui.AgentIntentExecuting
			for _, later := range task.Plan.Steps[index+1:] {
				if later.Type == agentrun.StepVerify && later.State == agentrun.StepCompleted {
					status = productui.AgentIntentObserved
					break
				}
			}
		}
		// An effect the runtime could not confirm either way must be looked
		// at before anything continues, whatever the step state says.
		if task.FailureCode == "AMBIGUOUS_EFFECT" && index == task.CurrentStep {
			status = productui.AgentIntentNeedsRepair
		}
		out = append(out, productui.AgentIntentStatus{Name: stepName(step), Status: status})
	}
	return out
}

func approvalStep(task agentrun.AgentTask, approvalID string) (agentrun.PlanStep, bool) {
	for _, step := range task.Plan.Steps {
		if task.ID+"/"+step.ID == approvalID {
			return step, true
		}
	}
	return agentrun.PlanStep{}, false
}

// approvalEvidence names what a step awaiting approval rests on: the sources
// of its inputs and the trust labels of that content. The inputs are matched
// to the ledger entries they reference, so a step that takes an earlier
// step's result inherits that result's sources and labels.
func approvalEvidence(task agentrun.AgentTask, step agentrun.PlanStep) ([]string, string) {
	var sources, taint []string
	addSource := func(source string) {
		if source != "" && !contains(sources, source) {
			sources = append(sources, source)
		}
	}
	addTaint := func(labels []string) {
		for _, label := range labels {
			if label = strings.ToUpper(strings.TrimSpace(label)); label != "" && !contains(taint, label) {
				taint = append(taint, label)
			}
		}
	}
	for _, input := range step.Inputs {
		addTaint(input.Taint)
		addSource(sourceLabel(task, input.SourceID))
		matched := false
		for _, entry := range task.Ledger.Entries {
			if entry.Revoked || entry.Ref != input.Ref {
				continue
			}
			matched = true
			addTaint(entry.Taint)
			switch entry.Kind {
			case "USER_GOAL", "USER_CONSTRAINT":
				addSource(productui.AgentApprovalSourceRequest)
			default:
				named := false
				for _, source := range append(append([]string(nil), entry.SourceIDs...), entry.SourceID) {
					if label := sourceLabel(task, source); label != "" {
						addSource(label)
						named = true
					}
				}
				if !named {
					addSource(productui.AgentApprovalSourceEarlierStep)
				}
			}
		}
		if !matched && strings.TrimSpace(input.SourceID) == "" {
			addSource(productui.AgentApprovalSourceEarlierStep)
		}
	}
	return sources, strings.Join(taint, ",")
}

// sourceLabel turns a source identifier into what the page may say about it.
// The identifier itself is never returned.
func sourceLabel(task agentrun.AgentTask, source string) string {
	source = strings.TrimSpace(source)
	switch {
	case source == "", source == "agent-document-usage:v1":
		return ""
	case strings.HasPrefix(source, "task:"):
		return productui.AgentApprovalSourceRequest
	}
	if id, ok := documentID(source); ok {
		for _, document := range task.Plan.DocumentReferences {
			if document.DocumentID == id {
				return productui.AgentApprovalSourceDocument + strings.TrimSpace(document.Label)
			}
		}
		return productui.AgentApprovalSourceDocument
	}
	return productui.AgentApprovalSourceRecords
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// planChanges compares the plan the owner last had with the revision waiting
// for confirmation: steps that are new, then steps that are gone. A step is
// the same step when its identity (id, skill and version, tier) is unchanged.
func planChanges(previous agentrun.HistoricalPlanSnapshot, current agentrun.AgentPlan) []productui.AgentPlanChange {
	type identity struct {
		id, skill string
		version   uint32
		tier      agentrun.Tier
	}
	before := map[identity]bool{}
	for _, step := range previous.Steps {
		before[identity{step.ID, step.SkillID, step.SkillVersion, step.Tier}] = true
	}
	after := map[identity]bool{}
	var out []productui.AgentPlanChange
	for _, step := range current.Steps {
		key := identity{step.ID, step.SkillID, step.SkillVersion, step.Tier}
		after[key] = true
		if !before[key] {
			out = append(out, productui.AgentPlanChange{Change: productui.AgentPlanChangeAdded, Step: productui.AgentTaskStep{Name: stepName(step), Tier: tierName(step.Tier)}})
		}
	}
	for _, step := range previous.Steps {
		if !after[identity{step.ID, step.SkillID, step.SkillVersion, step.Tier}] {
			name := strings.TrimSpace(step.SkillID)
			if name == "" {
				name = step.ID
			}
			out = append(out, productui.AgentPlanChange{Change: productui.AgentPlanChangeRemoved, Step: productui.AgentTaskStep{Name: name, Tier: tierName(step.Tier)}})
		}
	}
	return out
}

func tierName(tier agentrun.Tier) string {
	return "T" + string(rune('0'+int(tier)))
}
