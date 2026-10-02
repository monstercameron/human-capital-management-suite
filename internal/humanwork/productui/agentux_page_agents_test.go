package productui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderAgentUXPage(t *testing.T, locale string, projection AgentsAvailabilityProjection) string {
	t.Helper()
	view := uxblind122View(t, locale, &projection)
	markup, err := ui.RenderToString(BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func agentUXSnapshot() AgentSnapshot {
	tasks := []AgentTask{
		{ID: "active", Goal: "Review the open policy", State: AgentTaskRunning, LiveStep: "Reading the policy"},
		{ID: "done", Goal: "Summarise the handbook", State: AgentTaskCompleted, AnswerText: "The handbook is current."},
		{ID: "failed", Goal: "Prepare the unavailable report", State: AgentTaskFailed},
	}
	for index := 0; index < 21; index++ {
		tasks = append(tasks, AgentTask{ID: fmt.Sprintf("done-%d", index), Goal: fmt.Sprintf("Completed request %d", index), State: AgentTaskCompleted, AnswerText: "Finished."})
	}
	return AgentSnapshot{
		Availability:   AgentsAvailable,
		StartAvailable: true,
		Agents:         []AgentSummary{{ID: "coach", Name: "People Coach", Description: "Helps with people questions."}},
		Tasks:          tasks,
	}
}

func TestTodo_AGENTUX_001(t *testing.T) {
	projection := AgentsAvailabilityProjection{Enabled: true, Snapshot: agentUXSnapshot()}
	markup := renderAgentUXPage(t, "en-US", projection)
	composer := strings.Index(markup, `class="agents-composer"`)
	tasks := strings.Index(markup, `id="agents-tasks"`)
	if composer < 0 || tasks < 0 || composer >= tasks {
		t.Fatalf("composer is not the first page region: %s", markup)
	}
	for _, want := range []string{
		"People Coach", "Who should answer?", ">Ask<", "Plan a longer task", "usually within a minute",
		`class="agents-task-filter-label">Active</span>`, `aria-label="Task count" class="agents-task-filter-count">1</span>`, `class="agents-task-filter-label">Completed</span>`, `class="agents-task-filter-label">Failed</span>`, `data-task-category="active"`,
		`aria-label="Open task: Review the open policy"`, "Reading the policy", "The handbook is current.",
		"The agent stopped before it could answer. Nothing was changed.", "Show more",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Agents page missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, "Review the open policy") != 2 { // visible request plus the link's accessible name
		t.Fatalf("task request was duplicated in visible row content: %s", markup)
	}
	for _, forbidden := range []string{"Agent owner controls", "Export portable definition", "Import as draft", "/api/agents/rollouts", "/api/agent-controls", "/api/agents/portable/catalog", ">Open task<"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("user page still contains %q: %s", forbidden, markup)
		}
	}
	if strings.Contains(markup, "Agent operations") {
		t.Fatalf("regular viewer received owner link: %s", markup)
	}
	projection.ViewerIsAdmin = true
	admin := renderAgentUXPage(t, "en-US", projection)
	if strings.Count(admin, ">Operations<") != 1 || !strings.Contains(admin, `href="/workspace/app/admin/agents`) || !strings.Contains(admin, `aria-current="page"`) {
		t.Fatalf("administrator manage link = %s", admin)
	}
}

func TestTodo_AGENTUX_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		projection := AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}}
		markup := renderAgentUXPage(t, locale, projection)
		for _, want := range []string{
			agentUXR7Text(ResolveProductLocale(locale), "other_agents"),
			ResolveProductLocale(locale).Text("agents.no_tasks"),
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s page missing %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "agents.") || strings.Contains(markup, "âŸ¦") || strings.Contains(markup, "agents-empty-thread") {
			t.Fatalf("%s page has unresolved or redundant empty content: %s", locale, markup)
		}
		if strings.Contains(markup, `role="tablist"`) || strings.Contains(markup, `data-agent-task-more`) {
			t.Fatalf("%s empty page rendered task tabs or pagination: %s", locale, markup)
		}
	}

	task := AgentTask{ID: "detail", Title: "Handbook answer", Goal: "Summarise the handbook", AnswerText: "The handbook is current.", State: AgentTaskCompleted, Steps: []AgentTaskStep{{Name: "agent.read_handbook", State: "completed", Tier: "T0", Detail: "Read the approved version"}}}
	snapshot := AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{task}, SelectedTask: &task}
	detail := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	for _, want := range []string{"Back to tasks", `#agents-tasks"`, "Handbook answer", "Answer", "The handbook is current.", "Ask a follow-up", "Copy answer"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("task detail missing %q: %s", want, detail)
		}
	}
	if !strings.Contains(detail, `id="agents-composer-input"`) || !strings.Contains(detail, `id="agents-tasks"`) || !strings.Contains(detail, `class="agents-task-row is-expanded"`) || !strings.Contains(detail, `class="agents-task-detail-pane"`) {
		t.Fatalf("task detail did not stay with its list row: %s", detail)
	}
}

func TestTodo_AGENTUX_011(t *testing.T) {
	task := AgentTask{
		ID: "failed-task", Title: strings.Repeat("Explain the failed benefits comparison clearly. ", 4), Goal: "Explain the failed benefits comparison clearly.", State: AgentTaskFailed,
		Retryable: true, AnsweringAgentDisplayName: "Policy Helper",
		Steps: []AgentTaskStep{
			{Name: "Read own worker state", State: "completed", Tier: "T0"},
			{Name: "Summarize request", State: "failed", Tier: "T1"},
		},
	}
	snapshot := AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: snapshot})
	for _, want := range []string{
		`id="agents-task-title"`, "Failed", "The task did not finish", "The agent could not finish this task. You can try again.",
		`data-agent-ask-again="true"`, "data-agent-retry-prompt=\"Explain the failed benefits comparison clearly.", "Ask again",
		"What the agent did", "Looked up your own record", "Wrote a draft only you can see", "The agent could not finish this step. You can try again.",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("failed task detail missing %q: %s", want, markup)
		}
	}
	detailStart := strings.Index(markup, `class="agents-task-detail-pane"`)
	if detailStart < 0 {
		t.Fatalf("failed task detail is not reachable: %s", markup)
	}
	detailMarkup := markup[detailStart:]
	if failure, next := strings.Index(detailMarkup, "The task did not finish"), strings.Index(detailMarkup, "Ask again"); failure < 0 || next < failure {
		t.Fatalf("failure and recovery order = %d, %d: %s", failure, next, markup)
	}
	if strings.Contains(markup, "Your agents") {
		t.Fatalf("detail retained list/admin copy: %s", markup)
	}
}

func TestTodo_AGENTUX_009(t *testing.T) {
	snapshot := AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Agents: []AgentSummary{{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions."}}, Tasks: []AgentTask{{ID: "named", Title: "Policy answer", State: AgentTaskCompleted, AnsweringAgentDisplayName: "Policy Helper"}, {ID: "general", Title: "General answer", State: AgentTaskCompleted}}}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	for _, want := range []string{"Who should answer?", "General agent", "Answers everyday questions.", "Policy Helper", "Answers policy questions.", `name="agent"`, `value="policy-helper"`, "Answered by Policy Helper", "Answered by General agent"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("real agent choice missing %q: %s", want, markup)
		}
	}
	generalOnly := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}})
	if !strings.Contains(generalOnly, agentUXR7Text(ResolveProductLocale("en-US"), "other_agents")) || !strings.Contains(generalOnly, `name="agent"`) {
		t.Fatalf("general-only composer rendered a misleading choice: %s", generalOnly)
	}
}

func TestTodo_AGENTUX_009_Browser(t *testing.T) {
	for _, localeID := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup := renderAgentUXPage(t, localeID, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Agents: []AgentSummary{{ID: "policy", Name: "Policy Helper", Description: "Purpose"}}}})
		for _, want := range []string{locale.Text("agents.who_should_answer"), locale.Text("agents.general_agent"), locale.Text("agents.general_agent_purpose")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s composer missing %q: %s", localeID, want, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_011_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		task := AgentTask{ID: "failed", Goal: "Review my request", State: AgentTaskFailed, Retryable: true, Steps: []AgentTaskStep{{Name: "Summarize request", State: "failed", Tier: "T1"}}}
		markup := renderAgentUXPage(t, localeID, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
		locale := ResolveProductLocale(localeID)
		for _, want := range []string{locale.Text("agents.task_failed_title"), locale.Text("agents.ask_again")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s detail missing %q: %s", localeID, want, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_012(t *testing.T) {
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{{ID: "active", Goal: "A two-line request that keeps its status at the inline end", State: AgentTaskRunning}}}})
	for _, want := range []string{`role="tablist"`, `role="tab"`, `aria-selected="true"`, `tabindex="0"`, `aria-controls="agents-task-list"`, `role="tabpanel"`, agentUXR7Text(ResolveProductLocale("en-US"), "other_agents"), `class="agents-task-time"`, `dir="auto"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("aligned task list contract missing %q: %s", want, markup)
		}
	}
	if note, label := strings.Index(markup, agentUXR7Text(ResolveProductLocale("en-US"), "other_agents")), strings.Index(markup, `for="agents-composer-input"`); note < 0 || label < note {
		t.Fatalf("general-agent availability is not the first composer line: %s", markup)
	}
}

func TestTodo_AGENTUX_012_Browser(t *testing.T) {
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{`.agents-composer,.agents-tasks,.agents-task-view{box-sizing:border-box;width:100%`, `.agents-page [hidden]{display:none !important;}`, `.agents-task-filter[aria-selected="true"]`, `background:var(--surface-subtle,var(--canvas))`, `box-shadow:none`, `.agents-composer > :empty:not(textarea):not(input):not(select){display:none;}`} {
		if !strings.Contains(css, want) {
			t.Fatalf("Agents layout CSS missing %q: %s", want, css)
		}
	}
}

func TestTodo_AGENTUX_017(t *testing.T) {
	task := AgentTask{ID: "done", Title: "Use no company data and explain how to welcome a teammate", Goal: "Use no company data and explain how to welcome a teammate", AnswerText: "Say hello, introduce the team, and offer help.", State: AgentTaskCompleted, Steps: []AgentTaskStep{{Name: "Summarize request", State: "completed", Tier: "T1"}}}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
	for _, want := range []string{"Answer", task.AnswerText, task.Title, "Ask a follow-up", "Copy answer", `data-agent-follow-up-context=`, `data-agent-copy-answer="true"`, `dir="auto"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("task detail missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `class="agents-task-request-detail"`) || strings.Contains(markup, `class="agents-task-goal"`) {
		t.Fatalf("detail retained the duplicate request section: %s", markup)
	}
	for _, forbidden := range []string{">Read information<", ">Prepare a private draft<", `class="agents-step-tier"`} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("task detail retained internal or duplicated content %q: %s", forbidden, markup)
		}
	}
}

func TestTodo_AGENTUX_017_Browser(t *testing.T) {
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{`.agents-task-heading{align-items:center;display:flex`, `font-size:1.375rem`, `.agents-task-answer{`, `max-inline-size:68ch`, `-webkit-line-clamp:3`, `.agents-plan-step{align-items:center;grid-template-columns:auto minmax(0,1fr) auto`} {
		if !strings.Contains(css, want) {
			t.Fatalf("task detail CSS missing %q: %s", want, css)
		}
	}
	failed := AgentTask{ID: "failed", Goal: "Try this", State: AgentTaskFailed, Retryable: true}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, SelectedTask: &failed}})
	if !strings.Contains(markup, "Ask again") || !strings.Contains(markup, `data-agent-ask-again="true"`) {
		t.Fatalf("failed task did not offer a safe new request: %s", markup)
	}
}

func TestTodo_AGENTUX_011_RetryPolicyAndStepTiming(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	retryable := AgentTask{ID: "retry", Goal: "Retry this", State: AgentTaskFailed, Retryable: true, FailureReason: "The provider was temporarily unavailable.", Steps: []AgentTaskStep{{Name: "Prepare answer", State: "failed", StartedAt: at, FinishedAt: at.Add(75 * time.Second), FailureReason: "The answer step could not reach the provider."}}}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &retryable}})
	for _, want := range []string{"Ask again", "The agent service was unavailable."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("retryable detail missing %q: %s", want, markup)
		}
	}
	nonRetryable := retryable
	nonRetryable.ID, nonRetryable.Retryable, nonRetryable.FailureReason = "terminal", false, "Your access changed."
	markup = renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &nonRetryable}})
	if !strings.Contains(markup, `data-agent-ask-again="true"`) || !strings.Contains(markup, "Something went wrong on our side.") {
		t.Fatalf("non-retryable detail did not offer the safe recovery or cause: %s", markup)
	}
}

func TestTodo_AGENTUX_011_PlanConfirmation(t *testing.T) {
	task := AgentTask{ID: "plan", Version: 3, Goal: "Compare the policies", State: AgentTaskAwaitingPlanConfirmation, Actions: AgentTaskActionPolicy{ConfirmPlan: true}, Steps: []AgentTaskStep{{Name: "Read information", State: "pending"}, {Name: "Prepare answer", State: "pending"}}}
	markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, SelectedTask: &task}})
	for _, want := range []string{"Plan to review", "Read information", "Prepare answer", "Start this plan", "Change the request"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("plan confirmation missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `class="button primary"`) != 2 || strings.Contains(markup, ">Ask a follow-up<") {
		t.Fatalf("composer and plan regions did not each keep one primary action: %s", markup)
	}
	if strings.Index(markup, "Plan to review") > strings.Index(markup, "Start this plan") {
		t.Fatalf("plan action appeared before the plan: %s", markup)
	}
}

func TestTodo_AGENTUX_011_DefaultTaskTab(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tasks    []AgentTask
		selected string
	}{
		{"active wins", []AgentTask{{ID: "a", State: AgentTaskRunning}, {ID: "c", State: AgentTaskCompleted}, {ID: "f", State: AgentTaskFailed}}, "active"},
		{"completed fallback", []AgentTask{{ID: "c", State: AgentTaskCompleted}, {ID: "f", State: AgentTaskFailed}}, "completed"},
		{"failed fallback", []AgentTask{{ID: "f", State: AgentTaskFailed}}, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, Tasks: tc.tasks}})
			if !strings.Contains(markup, `data-selected-task-filter="`+tc.selected+`"`) {
				t.Fatalf("default tab = %s: %s", tc.selected, markup)
			}
			for _, label := range []string{">Active</span>", ">Completed</span>", ">Failed</span>"} {
				if !strings.Contains(markup, label) {
					t.Fatalf("task filter missing %q: %s", label, markup)
				}
			}
		})
	}
}

func TestTodo_AGENTUX_018(t *testing.T) {
	projection := AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}}
	markup := renderAgentUXPage(t, "en-US", projection)
	for _, want := range []string{agentUXR7Text(ResolveProductLocale("en-US"), "other_agents"), ">Ask</span>", ">Setup</a>", "/workspace/app/admin/personas", ">Operations</a>", "/workspace/app/admin/agents"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("agent vocabulary/action missing %q: %s", want, markup)
		}
	}
	for _, forbidden := range []string{">persona<", ">Persona<", "specialized", "Manage agents"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("Agents page exposed obsolete vocabulary %q: %s", forbidden, markup)
		}
	}
}

func TestTodo_AGENTUX_018_Golden(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		for _, key := range []string{"agents.page_title", "agents.only_general_agent", "agents.setup", "agents.manage", "agents.choose_agent"} {
			text := locale.Text(key)
			if text == "" || strings.Contains(strings.ToLower(text), "persona") || strings.Contains(strings.ToLower(text), "special") {
				t.Fatalf("%s %s vocabulary = %q", localeID, key, text)
			}
		}
	}
}

func TestTodo_AGENTUX_018_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		markup := renderAgentUXPage(t, localeID, AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}})
		lower := strings.ToLower(markup)
		if strings.Contains(lower, ">persona<") || strings.Contains(lower, "specialized") {
			t.Fatalf("%s Agents surface exposed mixed vocabulary: %s", localeID, markup)
		}
	}
}
