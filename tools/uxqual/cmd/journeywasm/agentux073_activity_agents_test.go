package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func agentUX073ServedControls() productui.AgentControlsSnapshot {
	// What GET /api/agent-controls answers: runs and schedules, no agents.
	return productui.AgentControlsSnapshot{Available: true, UpdatedAt: "2026-10-02T09:07:00Z", Runs: []productui.AgentControlRun{
		{ID: "run-1", AgentID: "policy-helper", Name: "Policy Helper", Version: "6", State: "COMPLETED", Started: "2026-10-01T15:16:00Z", Duration: "5s", RequestedBy: "Walt Brennan", Location: "#general"},
	}}
}

func agentUX073SetupSnapshot() productui.PersonaAdminSnapshot {
	return productui.PersonaAdminSnapshot{Available: true, CommandPermissionsAvailable: true, AllowedCommands: []string{"SUSPEND", "PUBLISH"}, Personas: []productui.PersonaAdminPersona{
		{ID: "assistant", Name: "Assistant", Version: "2", Lifecycle: productui.PersonaPublished, Icon: agentIconFixture(agenticon.Input{Name: "Assistant"})},
		{ID: "policy-helper", Name: "Policy Helper", Version: "6", Lifecycle: productui.PersonaPublished, Icon: agentIconFixture(agenticon.Input{Name: "Policy Helper"})},
	}}
}

func agentUX073RenderActivity(t *testing.T, snapshot productui.AgentControlsSnapshot) string {
	t.Helper()
	markup, err := ui.RenderToString(productui.RenderAgentControls(productui.ResolveProductLocale("en-US"), snapshot, "done"))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// Agent operations opened directly drew no agent rows: the page was served
// without the Agent setup snapshot the rows come from, the client held none,
// and nothing read it. The fixture-driven page test passed because it handed
// the renderer agents the served page never received. This test stands at the
// seam where the two projections are joined.
func TestTodo_AGENTUX_073_ActivityAgentsOnDirectLoad(t *testing.T) {
	setup := agentUX073SetupSnapshot()

	// Which snapshot the rows come from, and when it has to be read.
	if source, needsRead := agentControlsPersonaSource(nil, nil); source != nil || !needsRead {
		t.Fatal("a direct load of Agent operations, holding no setup snapshot, must read one")
	}
	if source, needsRead := agentControlsPersonaSource(nil, &productui.PersonaAdminSnapshot{}); source != nil || !needsRead {
		t.Fatal("an unavailable served snapshot was taken as the list of agents")
	}
	if source, needsRead := agentControlsPersonaSource(nil, &setup); source != &setup || needsRead {
		t.Fatal("the served setup snapshot was not used")
	}
	held := setup
	held.Personas = held.Personas[:1]
	if source, needsRead := agentControlsPersonaSource(&held, &setup); source != &held || needsRead {
		t.Fatal("the snapshot the session holds must win over the one the page was served with")
	}

	// The served controls reply alone: no agents, so no rows, and the hint must
	// not point at rows that are not there.
	bare := agentUX073RenderActivity(t, agentControlsPageSnapshot(agentUX073ServedControls(), nil, false))
	if strings.Contains(bare, `class="agent-owner-pause-row"`) || strings.Contains(bare, "pause an agent above") {
		t.Fatalf("Activity with no agents listed still refers to pausing above: %s", bare)
	}

	// Joined with the setup snapshot read for the page: both live agents are
	// rows with icon, name and version, state and Pause.
	joined := agentControlsPageSnapshot(agentUX073ServedControls(), &setup, false)
	if len(joined.Agents) != 2 || len(joined.AllowedCommands) != 2 || joined.AgentsUnavailable || len(joined.Runs) != 1 {
		t.Fatalf("the page snapshot lost a projection: %+v", joined)
	}
	activity := agentUX073RenderActivity(t, joined)
	if strings.Count(activity, `class="agent-owner-pause-row"`) != 2 {
		t.Fatalf("Activity does not list both live agents: %s", activity)
	}
	for _, agent := range setup.Personas {
		row := regexp.MustCompile(`data-persona-id="` + agent.ID + `".*?</li>`).FindString(activity)
		icon, _ := ui.RenderToString(agenticon.Node(agent.Icon))
		for _, want := range []string{icon, ">" + agent.Name + "<", ">Version " + agent.Version + "<", ">Answering<", `data-persona-command="SUSPEND"`, ">Pause agent<"} {
			if !strings.Contains(row, want) {
				t.Fatalf("the row for %s is missing %q: %s", agent.Name, want, row)
			}
		}
		if strings.Contains(row, " disabled") {
			t.Fatalf("Pause is disabled for %s although the viewer may pause it: %s", agent.Name, row)
		}
	}
	if !strings.Contains(activity, "You can pause an agent above to prevent new answers.") {
		t.Fatal("with agent rows drawn, the hint no longer says where to pause")
	}
	// Joining must not alias the setup snapshot's slices.
	joined.Agents[0].Name = "changed"
	if setup.Personas[0].Name != "Assistant" {
		t.Fatal("the page snapshot shares memory with the setup snapshot")
	}

	// The snapshot could not be read: the page says so and gives the other way
	// to pause an agent.
	failed := agentControlsPageSnapshot(agentUX073ServedControls(), nil, true)
	if !failed.AgentsUnavailable {
		t.Fatal("a failed read of the agents is not reported to the page")
	}
	unavailable := agentUX073RenderActivity(t, failed)
	if !strings.Contains(unavailable, "The list of agents could not be loaded here.") || !strings.Contains(unavailable, `href="/workspace/app/admin/personas`) || strings.Contains(unavailable, "pause an agent above") {
		t.Fatalf("a failed read of the agents is not explained: %s", unavailable)
	}
}

// The task list is re-rendered in the browser from the task RPCs. It was built
// from the tasks alone, so a row could not find the stored icon of the agent
// that answered and drew a fallback: a rocket under a choice card with a book.
func TestTodo_AGENTUX_074_TaskRowsWearTheChoiceIcon(t *testing.T) {
	book := agentIconFixture(agenticon.Input{Name: "Policy Helper"})
	agents := &journeyclient.Agents{Enabled: true, Service: "available", StartAvailable: true, Agents: []journeyclient.AgentSummary{
		{ID: "assistant", Name: "Assistant", Icon: agentIconFixture(agenticon.Input{Name: "Assistant"}), IconRevision: 1},
		{ID: "policy-helper", Name: "Policy Helper", Icon: book, IconRevision: 1},
	}}
	tasks := []productui.AgentTask{
		{ID: "t1", Title: "How many PTO hours carry over?", State: productui.AgentTaskCompleted, AnswerText: "Up to 40.", AnsweringAgentID: "policy-helper", AnsweringAgentDisplayName: "Policy Helper"},
		{ID: "t2", Title: "Older task under another id", State: productui.AgentTaskCompleted, AnswerText: "Up to 40.", AnsweringAgentID: "agent.policy@6", AnsweringAgentDisplayName: "Policy Helper"},
	}
	snapshot := agentTasksRegionSnapshot(agents, tasks, false, false)
	if len(snapshot.Agents) != 2 || snapshot.Agents[1].Icon != book || len(snapshot.Tasks) != 2 {
		t.Fatalf("the task list is rendered without the page's agents: %+v", snapshot.Agents)
	}
	// It is the same list the page itself is drawn from.
	if page := projectAgents(agents); len(page.Snapshot.Agents) != 2 || page.Snapshot.Agents[1].ID != snapshot.Agents[1].ID || page.Snapshot.Agents[1].Icon != snapshot.Agents[1].Icon {
		t.Fatal("the page and its task list take their agents from different places")
	}
	locale := productui.ResolveProductLocale("en-US")
	view := productui.NewView(productui.PageAgents, "tenant", "walt", "")
	view.Locale = locale
	markup, err := ui.RenderToString(productui.RenderAgentTasksRegion(view, locale, snapshot))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := ui.RenderToString(agenticon.Node(book))
	icon := regexp.MustCompile(`<svg[^>]*class="agent-icon".*?</svg>`)
	for _, id := range []string{"t1", "t2"} {
		row := markup[strings.Index(markup, `data-task-id="`+id+`"`):]
		row = row[:strings.Index(row, "</li>")]
		if got := icon.FindString(row); got != want {
			t.Fatalf("task %s is not drawn with the agent's stored icon:\n got %s\nwant %s", id, got, want)
		}
	}
	// The defect: the same tasks drawn without the agents wear another picture.
	bare, _ := ui.RenderToString(productui.RenderAgentTasksRegion(view, locale, productui.AgentSnapshot{Tasks: tasks}))
	if icon.FindString(bare) == want {
		t.Fatal("the fixture does not reproduce the defect; the stored icon is found without the agents")
	}
	if got := agentTasksRegionSnapshot(nil, tasks, true, false); len(got.Agents) != 0 || !got.TasksLoading {
		t.Fatalf("a page with agents turned off invented agents: %+v", got)
	}
}
