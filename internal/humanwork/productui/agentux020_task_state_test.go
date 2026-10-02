package productui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every state a task can be stored or transported in belongs to exactly one tab.
func TestTodo_AGENTUX_020(t *testing.T) {
	want := map[string]string{
		// The task runtime's own states, as stored.
		"DRAFTING": "active", "AWAITING_PLAN_CONFIRMATION": "active", "RUNNING": "active", "WAITING": "active",
		"AWAITING_APPROVAL": "active", "PAUSED": "active", "COMPLETED": "completed", "FAILED": "failed",
		"CANCELLED": "failed", "EXPIRED": "failed",
		// The run journal's states for an agent run.
		"READY": "active", "RECONCILING": "active", "NEEDS_REPAIR": "failed",
		// Spellings older builds and transports have written.
		"completed": "completed", "Complete": "completed", "TASK_STATE_COMPLETED": "completed", "AGENT_TASK_STATE_SUCCEEDED": "completed",
		"done": "completed", "canceled": "failed", "timed out": "failed", "awaiting-input": "active", "in_progress": "active", "queued": "active",
		// A value with no meaning is never shown as progressing or as answered.
		"": "failed", "???": "failed",
	}
	for stored, group := range want {
		if got := AgentTaskGroup(AgentTask{State: AgentTaskState(stored)}); got != group {
			t.Errorf("stored state %q is filed under %q, want %q", stored, got, group)
		}
		state := AgentTaskStateFromStored(stored)
		if !knownAgentTaskState(state) {
			t.Errorf("stored state %q maps to %q, which the page does not know", stored, state)
		}
		groups := 0
		for _, candidate := range []string{"active", "completed", "failed"} {
			if agentTaskCategory(state) == candidate {
				groups++
			}
		}
		if groups != 1 {
			t.Errorf("stored state %q belongs to %d groups", stored, groups)
		}
	}

	// A task stored before the newer projection fields existed has an answer and
	// no state the page knows: it is a completed task, not a failed one. One
	// that carries a failure stays failed.
	legacyAnswered := AgentTask{ID: "legacy-answered", Title: "Old question", State: "", ResultPreview: "The old answer."}
	legacyFailed := AgentTask{ID: "legacy-failed", Title: "Old failure", State: "", ResultPreview: "partial", FailureReason: "It took too long."}
	if AgentTaskGroup(legacyAnswered) != "completed" || AgentTaskSettledState(legacyAnswered) != AgentTaskCompleted {
		t.Fatalf("an answered task with no stored state is filed under %q", AgentTaskGroup(legacyAnswered))
	}
	if AgentTaskGroup(legacyFailed) != "failed" {
		t.Fatalf("a failed legacy task is filed under %q", AgentTaskGroup(legacyFailed))
	}

	// On the page the tab counts equal the rows shown, each task is listed once,
	// and every listed row opens.
	tasks := []AgentTask{legacyAnswered, legacyFailed}
	index := 0
	for stored := range want {
		index++
		tasks = append(tasks, AgentTask{ID: fmt.Sprintf("task-%d", index), Title: "Request " + strconv.Itoa(index), State: AgentTaskState(stored)})
	}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: tasks}})
	total := 0
	for _, group := range []string{"active", "completed", "failed"} {
		rows := strings.Count(markup, `data-task-category="`+group+`"`)
		count := regexp.MustCompile(`data-agent-task-filter="` + group + `".*?class="agents-task-filter-count">(\d+)<`).FindStringSubmatch(markup)
		if count == nil {
			count = regexp.MustCompile(`data-agent-task-filter="` + group + `".*?agents-task-filter-count[^>]*>(\d+)<`).FindStringSubmatch(markup)
		}
		if count == nil || count[1] != strconv.Itoa(rows) {
			t.Fatalf("tab %s counts %v but shows %d rows", group, count, rows)
		}
		total += rows
	}
	if total != len(tasks) {
		t.Fatalf("%d tasks are listed as %d rows", len(tasks), total)
	}
	for _, task := range tasks {
		if strings.Count(markup, `data-task-id="`+task.ID+`"`) != 1 || !strings.Contains(markup, `data-agent-task-row-link="`+task.ID+`"`) || !strings.Contains(markup, `task=`+task.ID+`#agents-task-title`) {
			t.Fatalf("task %s is not listed exactly once with a way to open it", task.ID)
		}
	}
	answered := markup[strings.Index(markup, `data-task-id="legacy-answered"`):]
	answered = answered[:strings.Index(answered, "</li>")]
	if !regexp.MustCompile(`data-task-category="completed"[^>]*data-task-id="legacy-answered"`).MatchString(markup) || !strings.Contains(answered, "The old answer.") {
		t.Fatalf("the answered legacy task is not listed as completed with its answer: %s", answered)
	}

	// Opening such a task shows its answer and the completed state.
	detail := agentUXR7Render(t, RenderAgentTaskDetail(View{}, ResolveProductLocale("en-US"), legacyAnswered, true))
	if !strings.Contains(detail, "The old answer.") || !strings.Contains(detail, `data-tone="completed"`) || strings.Contains(detail, `data-tone="failed"`) {
		t.Fatalf("the detail of an answered legacy task does not read as completed: %s", detail)
	}
}
