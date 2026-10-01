package productui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENT2_022(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tasks := []AgentOpsTask{
		{
			TenantID:       "harborcare-demo",
			TaskID:         "task-stalled-looping",
			UserID:         "user-private",
			OwnerID:        "owner-agent",
			AgentVersion:   "agent-v7",
			Status:         AgentOpsTaskRunning,
			LastActivityAt: now.Add(-20 * time.Minute),
			LoopCount:      4,
			Goal:           "PRIVATE GOAL: prepare a compensation draft",
			Documents:      []string{"PRIVATE-DOCUMENT-ID"},
			Drafts:         []string{"PRIVATE-DRAFT-BODY"},
			BudgetCents:    1000,
			SpendCents:     1200,
			Steps:          []AgentOpsStep{{StepID: "s1", Name: "read", LatencyMillis: 31, SpendCents: 25}},
		},
		{
			TenantID:       "ironridge-demo",
			TaskID:         "task-healthy",
			UserID:         "other-user",
			OwnerID:        "other-owner",
			AgentVersion:   "agent-v7",
			Status:         AgentOpsTaskRunning,
			LastActivityAt: now.Add(-30 * time.Second),
			BudgetCents:    1000,
			SpendCents:     20,
		},
	}

	projection := ProjectAgentOps(tasks, AgentOpsViewer{Audience: AgentOpsAudienceOperator, ID: "operator-1"}, now)
	if len(projection.Tasks) != 1 {
		t.Fatalf("operator task count = %d, want 1 flagged task", len(projection.Tasks))
	}
	view := projection.Tasks[0]
	if !view.Stalled || !view.Looping || !view.OverBudget {
		t.Fatalf("operator flags = stalled:%v looping:%v over-budget:%v", view.Stalled, view.Looping, view.OverBudget)
	}
	if view.UserID != "" || len(view.Trace) != 0 {
		t.Fatalf("operator projection exposed private content: %+v", view)
	}
	if strings.Contains(projection.String(), "PRIVATE-") || strings.Contains(projection.String(), "compensation draft") {
		t.Fatalf("operator projection serialized private content: %s", projection.String())
	}
	events := AgentOpsTelemetryEvents(tasks[0])
	if len(events) != 1 || events[0].TenantID != tasks[0].TenantID || events[0].TaskID != tasks[0].TaskID || events[0].StepID != "s1" || events[0].LatencyMillis != 31 {
		t.Fatalf("structured telemetry lost task-scoped step metrics: %+v", events)
	}
	doc, err := ui.RenderToString(AgentOpsDashboard(AgentOpsDashboardProps{
		Projection: projection,
		Copy:       AgentOpsCopy{Title: "Operations", Pause: "Pause"},
		OnPause:    func(string) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-agent-ops-action="pause"`) || !strings.Contains(doc, "task-stalled-looping") || !strings.Contains(doc, "Latency") || !strings.Contains(doc, "Wake lag") || strings.Contains(doc, "PRIVATE-") {
		t.Fatalf("dashboard did not render safe operator controls: %s", doc)
	}
}

func TestTodo_AGENT2_022_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	task := AgentOpsTask{
		TenantID:       "harborcare-demo",
		TaskID:         "task-owner-view",
		UserID:         "user-private",
		OwnerID:        "owner-1",
		AgentVersion:   "agent-v2",
		Status:         AgentOpsTaskWaiting,
		LastActivityAt: now.Add(-time.Minute),
		Goal:           "PRIVATE GOAL",
		Documents:      []string{"PRIVATE DOC"},
		Drafts:         []string{"PRIVATE DRAFT"},
		Steps: []AgentOpsStep{
			{StepID: "s1", Name: "private step name", DenialReason: "private denial detail", SpendCents: 10},
		},
	}

	owner := ProjectAgentOps([]AgentOpsTask{task}, AgentOpsViewer{Audience: AgentOpsAudienceOwner, ID: "owner-1"}, now)
	if len(owner.Tasks) != 1 {
		t.Fatalf("owner task count = %d, want 1", len(owner.Tasks))
	}
	view := owner.Tasks[0]
	if view.UserID != "" || len(view.Trace) != 0 {
		t.Fatalf("owner projection exposed private content: %+v", view)
	}
	if view.StepCount != 1 || view.SpendCents != 10 {
		t.Fatalf("owner aggregate lost safe metrics: %+v", view)
	}

	user := ProjectAgentOps([]AgentOpsTask{task}, AgentOpsViewer{Audience: AgentOpsAudienceUser, ID: "user-private"}, now)
	if len(user.Tasks) != 1 || len(user.Tasks[0].Trace) != 1 {
		t.Fatalf("user did not receive own full metadata trace: %+v", user.Tasks)
	}
	if strings.Contains(user.String(), "PRIVATE GOAL") || strings.Contains(user.String(), "PRIVATE DOC") || strings.Contains(user.String(), "PRIVATE DRAFT") {
		t.Fatalf("user projection copied raw task content: %s", user.String())
	}
	doc, err := ui.RenderToString(AgentOpsDashboard(AgentOpsDashboardProps{Projection: user, Copy: AgentOpsCopy{}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Step trace") || !strings.Contains(doc, "private step name") || strings.Contains(doc, "PRIVATE GOAL") || strings.Contains(doc, "PRIVATE DOC") {
		t.Fatalf("user dashboard did not render only the user's safe trace: %s", doc)
	}
	wrongTenant := ProjectAgentOps([]AgentOpsTask{task}, AgentOpsViewer{Audience: AgentOpsAudienceOwner, ID: "owner-1", TenantID: "ironridge-demo"}, now)
	if len(wrongTenant.Tasks) != 0 {
		t.Fatalf("owner projection crossed tenant boundary: %+v", wrongTenant.Tasks)
	}
}

func TestTodo_AGENT2_022_Integration(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	task := AgentOpsTask{
		TenantID:       "harborcare-demo",
		TaskID:         "task-pause",
		UserID:         "user-1",
		Status:         AgentOpsTaskRunning,
		LastActivityAt: now.Add(-15 * time.Minute),
		LoopCount:      4,
		BudgetCents:    100,
		SpendCents:     101,
	}
	controller := &recordingAgentOpsController{}
	viewer := AgentOpsViewer{Audience: AgentOpsAudienceOperator, ID: "operator-1"}
	if err := PauseAgentOpsTask(context.Background(), viewer, task, "loop detected", now, controller); err != nil {
		t.Fatal(err)
	}
	if len(controller.commands) != 1 {
		t.Fatalf("pause commands = %d, want 1", len(controller.commands))
	}
	command := controller.commands[0]
	if command.TaskID != task.TaskID || command.Audit.EventType != "agent.task.paused" || command.Audit.ActorID != viewer.ID || command.Audit.Reason != "loop detected" {
		t.Fatalf("pause command lost audit provenance: %+v", command)
	}
	if command.Audit.TenantID != task.TenantID || command.Audit.At.Before(now) {
		t.Fatalf("pause audit is not tenant-scoped and timestamped: %+v", command.Audit)
	}

	unauthorized := &recordingAgentOpsController{}
	err := PauseAgentOpsTask(context.Background(), AgentOpsViewer{Audience: AgentOpsAudienceOwner, ID: "owner-1"}, task, "not allowed", now, unauthorized)
	if !errors.Is(err, ErrAgentOpsUnauthorized) || len(unauthorized.commands) != 0 {
		t.Fatalf("unauthorized pause = %v, commands = %d", err, len(unauthorized.commands))
	}
}

type recordingAgentOpsController struct {
	commands []AgentOpsPauseCommand
}

func (c *recordingAgentOpsController) Pause(_ context.Context, command AgentOpsPauseCommand) error {
	c.commands = append(c.commands, command)
	return nil
}
