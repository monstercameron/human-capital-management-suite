package productui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// agentMatrixActivity is the Activity region as the server projects it for an
// agent's owner: one failed run, one running, one completed, and schedules in
// each state. Fields the page must never print carry the word "secret".
func agentMatrixActivity() AgentControlsSnapshot {
	return AgentControlsSnapshot{
		Available: true, OwnerName: "Walt Brennan", UpdatedAt: "2026-10-02T09:20:00Z", CanDraft: true,
		Runs: []AgentControlRun{
			{ID: "task-failed", AgentID: "policy-helper", Name: "Policy Helper", Revision: 5, Version: "4", Installation: "install-secret", State: "FAILED", Started: "2026-10-02T09:00:00Z", Since: "2026-10-02T09:00:42Z", Duration: "42s", RequestedBy: "Ana Flores", Location: "#general", ConversationID: "general", FailureGate: "MODEL_CALL", Failure: "provider_down_secret", QueueLag: "2.5s", Denials: []string{"tool_denied_secret", "scope_denied_secret"}, Incident: "incident-secret"},
			{ID: "task-running", AgentID: "policy-helper", Name: "Policy Helper", Revision: 3, Version: "4", State: "RUNNING", Started: "2026-10-02T09:19:00Z", Since: "2026-10-02T09:19:30Z", RequestedBy: "Walt Brennan", Location: "Walt Brennan", ConversationID: "direct-walt", ViewerDirect: true, Actions: []string{"pause", "delete"}},
			{ID: "task-done", AgentID: "assistant", Name: "Assistant", Revision: 2, Version: "2", State: "COMPLETED", Started: "2026-10-02T08:00:00Z", Since: "2026-10-02T08:00:09Z", Duration: "9s", RequestedBy: "Curtis Bell", Location: "#benefits", ConversationID: "benefits", Spend: "$0.02", Citations: []string{"doc-secret-1", "doc-secret-2", "doc-secret-3"}},
		},
		Schedules: []AgentControlSchedule{
			{ID: "sched-weekly", Name: "Weekly policy digest", Revision: 7, State: "ACTIVE", Version: "4", Zone: "America/New_York", Recurrence: "FREQ=WEEKLY;BYDAY=MO", DST: "LATER", Calendar: "business", Destination: "#general", Misfire: "SKIP", Overlap: "REFUSE", Budget: "100", Occurrences: []string{"2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"}, OccurrenceKeys: []string{"occ-1", "occ-2"}, Actions: []string{"pause", "skip", "dry_run", "run_now"}},
			{ID: "sched-paused", Name: "Onboarding reminder", Revision: 2, State: "PAUSED", Version: "3", Zone: "Europe/Berlin", Actions: []string{"resume", "retire"}},
			{ID: "sched-draft", Name: "Benefits reminder", Revision: 1, State: "DRAFT", Version: "4", Zone: "UTC", Actions: []string{"publish"}},
		},
	}
}

func agentMatrixRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return stdhtml.UnescapeString(markup)
}

// agentMatrixBlock is the element that carries the given attribute, up to its
// closing tag.
func agentMatrixBlock(t *testing.T, markup, attribute, tag string) string {
	t.Helper()
	at := strings.Index(markup, attribute)
	if at < 0 {
		t.Fatalf("no element with %s", attribute)
	}
	start := strings.LastIndex(markup[:at+len(attribute)], "<"+tag)
	if start < 0 {
		t.Fatalf("%s is not inside a <%s>", attribute, tag)
	}
	end := strings.Index(markup[start:], "</"+tag+">")
	if end < 0 {
		t.Fatalf("unterminated <%s> for %s", tag, attribute)
	}
	return markup[start : start+end]
}

// TestTodo_AGENT_041_Browser renders the owner's Agent operations page and its
// Activity region with the product's catalog. The owner can tell which run
// failed and why, what it cost when the server says, how long it waited and
// how many tool requests were refused, and can pause a running task with a
// reason and an incident reference. Nothing private to the run is printed.
func TestTodo_AGENT_041_Browser(t *testing.T) {
	english := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "owner-1", ""), english)
	view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}
	page := agentMatrixRender(t, BuildAgentOperationsPage(view))
	for _, want := range []string{`data-agent-operations-state="ready"`, `id="agent-operations-title"`, `id="agent-controls"`, `role="tablist"`, `data-agent-operations-tab="running"`, `aria-selected="true"`} {
		if !strings.Contains(page, want) {
			t.Errorf("owner page missing %q", want)
		}
	}
	activity := agentMatrixRender(t, RenderAgentControls(english, agentMatrixActivity(), "done"))

	failed := agentMatrixBlock(t, activity, `<tr data-control-run="task-failed"`, "tr")
	for _, want := range []string{
		"Policy Helper, version 4", "Ana Flores", `href="/workspace/app/chat#channel=general"`, "#general", "Oct 2, 9:00 AM", "Failed after 42 seconds",
		"The agent could not answer because the service it needed was unavailable.",
		"Try again; if it keeps failing, ask the technical contact to check the service connection.",
		"Waited to start: </strong>3 seconds", "Tool requests refused: </strong>2",
	} {
		if !strings.Contains(failed, want) {
			t.Errorf("failed run does not show %q: %s", want, failed)
		}
	}
	// Spend is the server's figure when it sends one, and an honest "not
	// available" with a way to ask when it does not.
	if !strings.Contains(failed, "Cost information is not available for this run.") || !strings.Contains(failed, ">Open Agent setup</a>") {
		t.Errorf("a run with no projected cost: %s", failed)
	}
	done := agentMatrixBlock(t, activity, `<tr data-control-run="task-done"`, "tr")
	if !strings.Contains(done, "Cost: </strong>$0.02") || strings.Contains(done, "Cost information is not available") || !strings.Contains(done, "Sources cited: </strong>3") || !strings.Contains(done, "Assistant, version 2") {
		t.Errorf("completed run: %s", done)
	}

	// Pausing: one button bound to this task and its revision, asking first,
	// with a labelled reason and incident reference for the audit record.
	running := agentMatrixBlock(t, activity, `<article class="card persona-admin-card agent-running-card" data-control-run="task-running"`, "article")
	if !strings.Contains(running, `data-owner-action="pause" data-owner-confirm="Pause task Policy Helper, version 4 for everyone?" data-owner-id="task-running" data-owner-kind="run" data-owner-revision="3" type="button">Pause task Policy Helper, version 4</button>`) {
		t.Errorf("running task has no bound pause control: %s", running)
	}
	for _, want := range []string{`<label for="agent-controls-reason">Reason for the action</label>`, `id="agent-controls-reason"`, `<label for="agent-controls-incident">Incident reference</label>`, `id="agent-controls-incident"`, "Running now (1)", "Recent runs (2)"} {
		if !strings.Contains(activity, want) {
			t.Errorf("activity missing %q", want)
		}
	}
	// An action the page has no meaning for is not offered, whatever was sent.
	if strings.Contains(activity, `data-owner-action="delete"`) || strings.Contains(activity, `data-owner-action="run_now"`) {
		t.Error("the page offers an action outside the owner's set")
	}
	// Schedule health sits on the same page.
	weekly := agentMatrixBlock(t, activity, `data-control-schedule="sched-weekly"`, "article")
	for _, want := range []string{"Weekly policy digest", "Status: </strong>Active", "Pinned agent version: </strong>4", "America/New_York", `<time datetime="2026-11-01T01:30:00-04:00">`} {
		if !strings.Contains(weekly, want) {
			t.Errorf("schedule missing %q: %s", want, weekly)
		}
	}
	// Private or internal values never reach the page: provider error text,
	// refusal codes, source ids, installation ids, incident ids.
	if strings.Contains(activity, "secret") {
		at := strings.Index(activity, "secret")
		t.Errorf("the page prints an internal value: …%s…", activity[max(0, at-80):min(len(activity), at+40)])
	}

	// Someone who is not the owner: the reason, and no controls.
	notOwner := agentMatrixRender(t, RenderAgentControls(english, agentMatrixActivity(), "denied"))
	if strings.Contains(notOwner, "data-owner-action") || strings.Contains(notOwner, "Policy Helper") || !strings.Contains(notOwner, "Only the agent's owner or steward can pause or stop it. Owner: Walt Brennan.") {
		t.Errorf("a person who is not the owner sees: %s", notOwner)
	}
	// Someone who may not open the page at all gets no mount to load it into.
	employee := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "employee-1", ""), english)
	employee.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true}
	refused := agentMatrixRender(t, BuildAgentOperationsPage(employee))
	if !strings.Contains(refused, `data-agent-operations-state="denied"`) || strings.Contains(refused, `id="agent-controls"`) || strings.Contains(refused, `role="tablist"`) {
		t.Errorf("a refused viewer received the page: %s", refused)
	}
}

// TestTodo_AGENT_030_Browser renders the schedule controls of Agent
// operations in three languages: an active schedule can be paused, have one
// previewed occurrence skipped, or be dry-run; a paused one can be resumed or
// retired; a draft waits to be published; none can be run at once. Every
// control is a button bound to the schedule's revision.
func TestTodo_AGENT_030_Browser(t *testing.T) {
	words := map[string]struct{ pause, skip, dryRun, resume, retire, publish, occurrence string }{
		"en-US": {"Pause Weekly policy digest", "Skip next occurrence Weekly policy digest", "Dry run Weekly policy digest", "Resume Onboarding reminder", "Retire Onboarding reminder", "Publish Benefits reminder", "Occurrence to skip"},
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			activity := agentMatrixRender(t, RenderAgentControls(locale, agentMatrixActivity(), "done"))
			weekly := agentMatrixBlock(t, activity, `data-control-schedule="sched-weekly"`, "article")
			for _, action := range []string{"pause", "skip", "dry_run"} {
				if strings.Count(weekly, `data-owner-action="`+action+`"`) != 1 || !strings.Contains(weekly, `data-owner-action="`+action+`"`) {
					t.Errorf("active schedule has no %s control: %s", action, weekly)
				}
			}
			// Each control names the schedule and the revision it was drawn from,
			// so a change made meanwhile is refused instead of overwritten.
			if got := strings.Count(weekly, `data-owner-id="sched-weekly" data-owner-kind="schedule" data-owner-revision="7" type="button"`); got != 3 {
				t.Errorf("controls bound to the schedule's revision = %d, want 3: %s", got, weekly)
			}
			// A dry run changes nothing, so it does not ask first; pause and skip do.
			if strings.Count(weekly, "data-owner-confirm=") != 2 || strings.Contains(agentMatrixBlock(t, weekly, `data-owner-action="dry_run"`, "button"), "data-owner-confirm") {
				t.Errorf("confirmation on the schedule controls: %s", weekly)
			}
			// Skipping names which previewed occurrence, through a labelled choice.
			if !strings.Contains(weekly, `<label for="agent-occurrence-sched-weekly">`) || !strings.Contains(weekly, `<select data-owner-occurrence="sched-weekly" id="agent-occurrence-sched-weekly">`) ||
				!strings.Contains(weekly, `<option value="occ-1">`) || !strings.Contains(weekly, `<option value="occ-2">`) {
				t.Errorf("occurrence choice: %s", weekly)
			}
			// The two previewed occurrences either side of the clock change are
			// two different times on the page, not one repeated.
			times := regexp.MustCompile(`<time datetime="([^"]+)">([^<]+)</time>`).FindAllStringSubmatch(weekly, -1)
			if len(times) != 2 || times[0][1] == times[1][1] || times[0][2] == times[1][2] {
				t.Errorf("previewed occurrences across the clock change = %v", times)
			}
			// The pinned version and the policies that decide a firing are stated.
			for _, value := range []string{">4</p>", "America/New_York", "business"} {
				if !strings.Contains(weekly, value) {
					t.Errorf("schedule details missing %q: %s", value, weekly)
				}
			}
			paused := agentMatrixBlock(t, activity, `data-control-schedule="sched-paused"`, "article")
			if !strings.Contains(paused, `data-owner-action="resume"`) || !strings.Contains(paused, `data-owner-action="retire"`) || strings.Contains(paused, `data-owner-action="pause"`) || strings.Contains(paused, `data-owner-action="dry_run"`) {
				t.Errorf("paused schedule controls: %s", paused)
			}
			draft := agentMatrixBlock(t, activity, `data-control-schedule="sched-draft"`, "article")
			if strings.Count(draft, "data-owner-action=") != 1 || !strings.Contains(draft, `data-owner-action="publish"`) {
				t.Errorf("draft schedule controls: %s", draft)
			}
			// Nothing on the page fires a schedule at once, past a pause.
			if strings.Contains(activity, "run_now") {
				t.Error("the page offers to run a schedule now")
			}
			// Every schedule control is a real button, reachable by keyboard.
			for _, control := range regexp.MustCompile(`<(\w+)[^>]*data-owner-kind="schedule"[^>]*>`).FindAllStringSubmatch(activity, -1) {
				if control[1] != "button" || strings.Contains(control[0], "disabled") || !strings.Contains(control[0], `type="button"`) {
					t.Errorf("schedule control is not an enabled button: %s", control[0])
				}
			}
			if !strings.Contains(activity, `dir="`+string(locale.Direction)+`"`) || strings.Contains(activity, "⟦") {
				t.Errorf("%s activity lost its direction or is untranslated", localeID)
			}
			if want, ok := words[localeID]; ok {
				for _, text := range []string{want.pause, want.skip, want.dryRun, want.resume, want.retire, want.publish, want.occurrence} {
					if !strings.Contains(activity, ">"+text+"<") {
						t.Errorf("control text %q missing", text)
					}
				}
			}
		})
	}
	// Drafting and previewing start from the announcements tab, offered only to
	// someone who may draft.
	english := ResolveProductLocale("en-US")
	owner := agentMatrixRender(t, RenderAgentControls(english, agentMatrixActivity(), "done"))
	if !strings.Contains(owner, `href="/workspace/app/admin/agents?tab=announcements"`) {
		t.Error("an owner who may draft has no way to the schedule drafts")
	}
	reader := agentMatrixActivity()
	reader.CanDraft = false
	for index := range reader.Schedules {
		reader.Schedules[index].Actions = nil
	}
	readOnly := agentMatrixRender(t, RenderAgentControls(english, reader, "done"))
	if strings.Contains(readOnly, `tab=announcements`) || strings.Contains(readOnly, `data-owner-kind="schedule"`) || !strings.Contains(readOnly, "Weekly policy digest") {
		t.Errorf("a viewer with no schedule authority: %s", readOnly)
	}
}

// agentMatrixSurfaces are the agent pages a person meets, rendered whole for
// one locale: Agent setup, the Activity region of Agent operations, the Agents
// page with a task open, and the agent's profile as Chat shows it.
func agentMatrixSurfaces(t *testing.T, localeID string, allowed bool) map[string]string {
	t.Helper()
	locale := ResolveProductLocale(localeID)
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.ReviewApproved, persona.EvaluationRef = PersonaPublished, true, "eval-4"
	persona.Installations = []PersonaAdminInstallation{
		{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4"},
		{InstallationID: "install-direct", ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "4", Stopped: true, StoppedReason: PersonaPlacementStoppedNotPublished, StartAgain: true},
	}
	setup := agentUXSetup2Snapshot(persona)
	setup.AllowedCommands = append(setup.AllowedCommands, "REINSTALL")
	activity := agentMatrixActivity()
	if !allowed {
		setup.AllowedCommands = nil
		activity.CanDraft = false
		for index := range activity.Runs {
			activity.Runs[index].Actions = nil
		}
		for index := range activity.Schedules {
			activity.Schedules[index].Actions = nil
		}
	}
	agents := NewView(PageAgents, "tenant-1", "principal-1", "")
	agents.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, Snapshot: agentMatrixTasks()}
	agents = ApplyLocale(ApplyRequest(agents, PageRequest{Page: PageAgents, AgentTaskID: "task-run"}), locale)
	return map[string]string{
		"Agent setup":      agentMatrixRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: locale}, State: PersonaAdminReady, Snapshot: setup, Client: &agentUXMatrixClient{personaAdminTestClient{snapshot: setup}}})),
		"Agent operations": agentMatrixRender(t, RenderAgentControls(locale, activity, "done")),
		"Agents":           agentMatrixRender(t, BuildAgentsSurface(agents)),
	}
}

// agentMatrixTasks is one person's task list with a task in every group, some
// written by older builds: a state spelt another way, and no state at all.
func agentMatrixTasks() AgentSnapshot {
	return AgentSnapshot{
		Availability: AgentsAvailable, StartAvailable: true,
		Agents: []AgentSummary{
			{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions with citations.", Status: "Ready"},
			{ID: "assistant", Name: "Assistant", Description: "Answers everyday questions.", Status: "Ready"},
		},
		Tasks: []AgentTask{
			{ID: "task-run", Version: 3, AgentID: "policy-helper", Title: "How much leave do I have?", State: AgentTaskRunning, LiveStep: "Reading sources", Actions: AgentTaskActionPolicy{Pause: true, Cancel: true}},
			{ID: "task-wait", AgentID: "assistant", Title: "Draft a note", State: "QUEUED"},
			{ID: "task-done", AgentID: "policy-helper", Title: "What is the PTO policy?", State: "TASK_STATE_SUCCEEDED", AnswerText: "Twenty days."},
			{ID: "task-old", AgentID: "policy-helper", Title: "Older answer", ResultPreview: "Fifteen days."},
			{ID: "task-fail", AgentID: "assistant", Title: "Summarize the handbook", State: AgentTaskFailed, FailureReason: "MODEL_UNAVAILABLE"},
			{ID: "task-cancel", AgentID: "assistant", Title: "Cancelled request", State: AgentTaskCancelled},
		},
	}
}

var (
	agentMatrixControl = regexp.MustCompile(`<(input|select|textarea)\b([^>]*)>`)
	agentMatrixID      = regexp.MustCompile(`\bid="([^"]+)"`)
	agentMatrixFor     = regexp.MustCompile(`<label[^>]*\bfor="([^"]+)"`)
	// agentMatrixMachine matches what a control sends or selects, as opposed to
	// what it says: command and action names, field names and option values.
	agentMatrixMachine     = regexp.MustCompile(`\b(data-persona-admin-command-form|data-persona-command|data-persona-review-decision|data-owner-action|data-owner-kind|data-owner-revision|data-agent-action|data-agent-task-filter|data-task-action|data-agentdoc-default-mode|name)="([^"]*)"|(<option[^>]*>)|<input[^>]*type="(?:radio|checkbox|hidden)"[^>]*\svalue="([^"]*)"`)
	agentMatrixOptionSent  = regexp.MustCompile(`\sdata-value="([^"]*)"`)
	agentMatrixOptionValue = regexp.MustCompile(`\svalue="([^"]*)"`)
)

// agentMatrixUnlabelled lists the visible form controls no label names.
func agentMatrixUnlabelled(markup string) []string {
	labelled := map[string]bool{}
	for _, match := range agentMatrixFor.FindAllStringSubmatch(markup, -1) {
		labelled[match[1]] = true
	}
	var missing []string
	for _, control := range agentMatrixControl.FindAllStringSubmatch(markup, -1) {
		attributes := control[2]
		if strings.Contains(attributes, `type="hidden"`) || strings.Contains(attributes, "aria-label=") || strings.Contains(attributes, "aria-labelledby=") {
			continue
		}
		id := agentMatrixID.FindStringSubmatch(attributes)
		if id == nil || !labelled[id[1]] {
			missing = append(missing, control[0])
		}
	}
	return missing
}

// TestTodo_AGENT_043: on every agent page, in English, German and Arabic, the
// page reads in the locale's direction, every word comes from the catalog,
// every field has a label, and results are announced.
func TestTodo_AGENT_043(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		for name, markup := range agentMatrixSurfaces(t, localeID, true) {
			if !strings.Contains(markup, `dir="`+string(locale.Direction)+`"`) {
				t.Errorf("%s in %s does not read %s", name, localeID, locale.Direction)
			}
			for _, raw := range []string{"⟦", ">agents.", ">agent_setup.", ">persona_admin.", ">page.", ">chat."} {
				if strings.Contains(markup, raw) {
					at := strings.Index(markup, raw)
					t.Errorf("%s in %s prints an untranslated key: %s", name, localeID, markup[at:min(len(markup), at+60)])
				}
			}
			if missing := agentMatrixUnlabelled(markup); len(missing) > 0 {
				t.Errorf("%s in %s has fields with no label: %v", name, localeID, missing)
			}
			for _, label := range agentMatrixFor.FindAllStringSubmatch(markup, -1) {
				if !strings.Contains(markup, `id="`+label[1]+`"`) {
					t.Errorf("%s in %s has a label for a field that is not on the page: %s", name, localeID, label[1])
				}
			}
			if !strings.Contains(markup, `role="status"`) {
				t.Errorf("%s in %s announces nothing", name, localeID)
			}
		}
	}
	// The same page says different words in each language: the catalog is used,
	// not English with another direction.
	english, german, arabic := agentMatrixSurfaces(t, "en-US", true), agentMatrixSurfaces(t, "de-DE", true), agentMatrixSurfaces(t, "ar", true)
	for name := range english {
		if english[name] == german[name] || english[name] == arabic[name] || german[name] == arabic[name] {
			t.Errorf("%s reads the same in two languages", name)
		}
	}
	for text, in := range map[string]string{"Entfernen": german["Agent setup"], "إزالة": arabic["Agent setup"], "Aktivität": german["Agent operations"], "النشاط": arabic["Agent operations"]} {
		if !strings.Contains(in, text) {
			t.Errorf("translated text %q is not on its page", text)
		}
	}
}

// TestTodo_AGENT_043_Browser: every agent page can be worked without a mouse.
// Actions are buttons, destinations are links, tabs keep one tab stop, and the
// places a result lands can take focus. A keyboard user can pause a schedule.
func TestTodo_AGENT_043_Browser(t *testing.T) {
	interactive := regexp.MustCompile(`<(\w+)\b[^>]*\b(?:data-owner-action|data-persona-command|data-agent-action|data-task-action|data-agent-task-filter|data-persona-editor-toggle)="[^"]*"[^>]*>`)
	tablist := regexp.MustCompile(`(?s)role="tablist".*?</(?:nav|div)>`)
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		surfaces := agentMatrixSurfaces(t, localeID, true)
		view := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "owner-1", ""), ResolveProductLocale(localeID))
		view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: true}
		surfaces["Agent operations page"] = agentMatrixRender(t, BuildAgentOperationsPage(view))
		for name, markup := range surfaces {
			for _, control := range interactive.FindAllStringSubmatch(markup, -1) {
				if control[1] != "button" {
					t.Errorf("%s in %s: an action is a <%s>, which a keyboard cannot press: %s", name, localeID, control[1], control[0])
				}
			}
			for _, form := range regexp.MustCompile(`(?s)<form[^>]*data-persona-admin-command-form="[^"]*"[^>]*>.*?</form>`).FindAllString(markup, -1) {
				if !strings.Contains(form, `type="submit"`) {
					t.Errorf("%s in %s: a command form has no submit button: %s", name, localeID, form[:min(len(form), 200)])
				}
			}
			for _, anchor := range regexp.MustCompile(`<a\b[^>]*>`).FindAllString(markup, -1) {
				if !strings.Contains(anchor, "href=") {
					t.Errorf("%s in %s: a link goes nowhere: %s", name, localeID, anchor)
				}
			}
			for _, tabs := range tablist.FindAllString(markup, -1) {
				if stops, selected := strings.Count(tabs, `tabindex="0"`), strings.Count(tabs, `aria-selected="true"`); stops != 1 || selected != 1 {
					t.Errorf("%s in %s: a tab list has %d tab stops and %d selected tabs: %s", name, localeID, stops, selected, tabs[:min(len(tabs), 300)])
				}
			}
			if strings.Contains(markup, "tabindex=\"1\"") || strings.Contains(markup, "tabindex=\"2\"") {
				t.Errorf("%s in %s forces a tab order", name, localeID)
			}
		}
		// The schedule pause the RED line names.
		pause := agentMatrixBlock(t, agentMatrixBlock(t, surfaces["Agent operations"], `data-control-schedule="sched-weekly"`, "article"), `data-owner-action="pause"`, "button")
		if !strings.HasPrefix(pause, "<button") || strings.Contains(pause, "disabled") || !strings.Contains(pause, `type="button"`) {
			t.Errorf("%s: the schedule's pause is not an enabled button: %s", localeID, pause)
		}
		// Where a result is announced, focus can be sent.
		if !strings.Contains(surfaces["Agent operations"], `id="agent-controls-status" role="status" tabindex="-1"`) || !strings.Contains(surfaces["Agents"], `id="agents-task-title" tabindex="-1"`) {
			t.Errorf("%s: a result has nowhere to take focus", localeID)
		}
	}
}

// TestTodo_AGENT_043_Conformance: a translated label never changes what a
// control does. The commands, actions, field names and option values on every
// agent page are the same in every language, in the same order.
func TestTodo_AGENT_043_Conformance(t *testing.T) {
	machine := func(markup string) []string {
		var values []string
		for _, match := range agentMatrixMachine.FindAllStringSubmatch(markup, -1) {
			switch {
			case match[1] != "":
				values = append(values, match[1]+"="+match[2])
			case match[3] != "":
				// A suggestion list shows its label in value and keeps what
				// is sent in data-value; an ordinary option sends its value.
				sent := agentMatrixOptionSent.FindStringSubmatch(match[3])
				if sent == nil {
					sent = agentMatrixOptionValue.FindStringSubmatch(match[3])
				}
				if sent != nil {
					values = append(values, "option="+sent[1])
				}
			default:
				values = append(values, "value="+match[4])
			}
		}
		return values
	}
	english := agentMatrixSurfaces(t, "en-US", true)
	for _, localeID := range []string{"de-DE", "ar"} {
		for name, markup := range agentMatrixSurfaces(t, localeID, true) {
			want, got := machine(english[name]), machine(markup)
			if len(want) < 10 {
				t.Fatalf("%s carries only %d machine values; the comparison would prove nothing", name, len(want))
			}
			if len(got) != len(want) {
				t.Errorf("%s in %s carries %d machine values, English carries %d", name, localeID, len(got), len(want))
				continue
			}
			for index := range want {
				if got[index] != want[index] {
					t.Errorf("%s in %s: control %d sends %q, English sends %q", name, localeID, index, got[index], want[index])
					break
				}
			}
		}
	}
	// The policy choices themselves are the stored values, not their labels.
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		setup := agentMatrixSurfaces(t, localeID, true)["Agent setup"]
		for _, value := range []string{`name="allowed_channels" type="checkbox" value="PRIVATE"`, `name="allowed_channels" type="checkbox" value="PUBLIC"`, `data-agentdoc-default-mode="PINNED"`, `data-persona-admin-command-form="UNINSTALL"`, `data-persona-admin-command-form="REINSTALL"`} {
			if !strings.Contains(setup, value) {
				t.Errorf("Agent setup in %s does not carry the policy value %s", localeID, value)
			}
		}
	}
}

// TestTodo_AGENT_043_Security: language and width change how a page looks and
// never what it allows or what it discloses.
func TestTodo_AGENT_043_Security(t *testing.T) {
	controls := regexp.MustCompile(`data-owner-action="|data-persona-admin-command-form="(?:INSTALL|REINSTALL)"|<button[^>]*type="submit">`)
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		refused := agentMatrixSurfaces(t, localeID, false)
		// A person with no authority gets no working control in any language.
		if found := controls.FindAllString(refused["Agent operations"], -1); len(found) != 0 {
			t.Errorf("Agent operations in %s offers %v to a person with no authority", localeID, found)
		}
		setup := refused["Agent setup"]
		if strings.Contains(setup, `data-persona-admin-command-form="INSTALL"`) || strings.Contains(setup, `data-persona-admin-command-form="REINSTALL"`) || strings.Contains(setup, `data-persona-command="PUBLISH"`) {
			t.Errorf("Agent setup in %s offers a command to a person with no authority", localeID)
		}
		for _, form := range regexp.MustCompile(`(?s)<form[^>]*data-persona-admin-command-form="UNINSTALL"[^>]*>.*?</form>`).FindAllString(setup, -1) {
			if !strings.Contains(form, "hidden") || !strings.Contains(form, "disabled") {
				t.Errorf("Agent setup in %s leaves Remove working for a person with no authority: %s", localeID, form)
			}
		}
		// The disclosure that an agent acts with the asker's access, and the
		// agent's identity, are on the card in every language.
		card := agentMatrixRender(t, chatui.Build(agentMatrixChat(localeID)))
		if !strings.Contains(card, `class="agent-badge"`) && !strings.Contains(card, "agent-badge") {
			t.Errorf("Chat in %s does not mark the agent as an agent", localeID)
		}
	}
	// At narrow width nothing that identifies an agent, states what it may
	// reach, or reports a stopped placement is hidden.
	identity := []string{".agent-badge", ".mention-profile-access", ".mention-attribution", ".mention-agent-identity", ".persona-admin-placement-warning", ".persona-admin-placement-stopped", ".agents-agent-choice", ".agent-control-action", ".agent-operations-updated"}
	media := regexp.MustCompile(`@media\(max-width:[^)]*\)\{((?:[^{}]*\{[^{}]*\})*)\}`)
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	for name, css := range map[string]string{"Agent setup": personaAdminStylesheet(), "agent pages": agentUXR7Stylesheet(), "agent profile": chatui.PersonaProfileStyles} {
		blocks := media.FindAllStringSubmatch(css, -1)
		for _, block := range blocks {
			for _, declared := range rule.FindAllStringSubmatch(block[1], -1) {
				if !strings.Contains(declared[2], "display:none") && !strings.Contains(declared[2], "visibility:hidden") {
					continue
				}
				for _, selector := range identity {
					for _, part := range strings.Split(declared[1], ",") {
						if strings.HasSuffix(strings.TrimSpace(part), selector) {
							t.Errorf("%s stylesheet hides %s at narrow width: %s{%s}", name, selector, declared[1], declared[2])
						}
					}
				}
			}
		}
	}
}

// agentMatrixChat is a conversation with an agent placed in it, with the
// agent's profile open from the mention menu.
func agentMatrixChat(localeID string) chatui.Model {
	locale := ResolveProductLocale(localeID)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "walt", MemberCount: 2, Joined: true}
	reference := chatui.ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID}
	return chatui.Model{
		State: chatui.StateReady, Locale: locale.Resolved, Direction: string(locale.Direction), ShowDetails: true, SelectedID: room.ID, CurrentTenantID: "t", CurrentUser: "walt",
		Text:          func(key string) string { return locale.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{
			{Reference: reference, Handle: "policy-helper", Purpose: "Answer policy questions", DataClasses: []string{"POLICY_DOCUMENT"}},
		},
		PersonaLookup: chatui.PersonaLookupReady, PersonaLookupConversationID: room.ID,
		Callbacks: chatui.Callbacks{ToggleDetails: func(bool) {}, SendMessageWithReferences: func(string, string, []chatui.ChatReference) {}},
	}
}

// TestTodo_AGENTUX_020_Browser opens the first row of each group of the
// Agents page, as a click on it does: the row's own link is followed and the
// page that comes back shows that task. The tab counts are the rows drawn.
func TestTodo_AGENTUX_020_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	load := func(taskID string) string {
		t.Helper()
		view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", ""), locale)
		view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, Snapshot: agentMatrixTasks()}
		return agentMatrixRender(t, BuildAgentsSurface(ApplyRequest(view, PageRequest{Page: PageAgents, AgentTaskID: taskID})))
	}
	list := load("")
	if strings.Contains(list, "data-task-view=") {
		t.Fatal("the list opened a task nobody chose")
	}
	rows := regexp.MustCompile(`<li class="agents-task-row[^"]*" data-selected="[^"]*" data-task-category="(\w+)" data-task-id="([^"]+)" data-task-index="(\d+)"`).FindAllStringSubmatch(list, -1)
	groups := map[string][]string{}
	for _, row := range rows {
		groups[row[1]] = append(groups[row[1]], row[2])
	}
	// Every stored state is in exactly one group, and the count on each tab is
	// the number of rows in it.
	if len(rows) != 6 || len(groups["active"]) != 2 || len(groups["completed"]) != 2 || len(groups["failed"]) != 2 {
		t.Fatalf("groups = %v", groups)
	}
	for category, ids := range groups {
		tab := agentMatrixBlock(t, list, `data-agent-task-filter="`+category+`"`, "button")
		if !strings.Contains(tab, `class="agents-task-filter-count">2</span>`) {
			t.Errorf("%s tab count is not its %d rows: %s", category, len(ids), tab)
		}
	}
	// A task an older build stored with another spelling, or with an answer and
	// no state, is a completed task.
	if groups["completed"][0] != "task-done" || groups["completed"][1] != "task-old" {
		t.Fatalf("completed group = %v", groups["completed"])
	}
	titles := map[string]string{"task-run": "How much leave do I have?", "task-done": "What is the PTO policy?", "task-fail": "Summarize the handbook"}
	status := map[string]string{"active": "Running", "completed": "Completed", "failed": "Failed"}
	for _, category := range []string{"active", "completed", "failed"} {
		first := groups[category][0]
		row := agentMatrixBlock(t, list, `data-task-id="`+first+`"`, "li")
		href := regexp.MustCompile(`class="agents-task-link"[^>]*href="([^"]+)"`).FindStringSubmatch(row)
		if href == nil || href[1] != "/workspace/app/chat/agents?task="+first+"#agents-task-title" {
			t.Fatalf("first %s row link = %v", category, href)
		}
		// Following the link is a request for that task id.
		opened := load(first)
		detail := agentMatrixBlock(t, opened, `data-task-view="`+first+`"`, "section")
		if !strings.Contains(detail, `<h1 dir="auto" id="agents-task-title" tabindex="-1">`+titles[first]+`</h1>`) || !strings.Contains(detail, `data-tone="`+category+`">`+status[category]+`</span>`) {
			t.Errorf("opening the first %s task shows: %s", category, detail[:min(len(detail), 600)])
		}
		if strings.Count(opened, "data-task-view=") != 1 {
			t.Errorf("opening %s shows %d details", first, strings.Count(opened, "data-task-view="))
		}
		// The list is on that task's tab, with the row marked open.
		if !strings.Contains(opened, `data-selected-task-filter="`+category+`" data-selected-task-id="`+first+`"`) || !strings.Contains(agentMatrixBlock(t, opened, `data-task-id="`+first+`"`, "li"), `aria-expanded="true"`) {
			t.Errorf("after opening %s the list is not on its tab", first)
		}
	}
	// What each group's first task offers is what its state allows.
	if running := load("task-run"); !strings.Contains(running, `data-task-action="pause"`) || !strings.Contains(running, `data-task-action="cancel"`) {
		t.Error("an open running task has no pause or cancel")
	}
	if answered := load("task-done"); !strings.Contains(answered, "<p dir=\"auto\">Twenty days.</p>") || strings.Contains(answered, `data-task-action="pause"`) {
		t.Error("an open completed task does not show its answer, or offers to pause it")
	}
	if older := load("task-old"); !strings.Contains(older, "Fifteen days.") {
		t.Error("a task stored before the state field shows no answer")
	}
	// An id that is not in the list opens nothing, and says nothing about why.
	for _, id := range []string{"task-of-someone-else", "<null>"} {
		if stray := load(id); strings.Contains(stray, "data-task-view=") || strings.Contains(stray, id) {
			t.Errorf("the id %q opened or echoed something", id)
		}
	}
}

// TestTodo_AGENTUX_021_Browser: on the Agents page each agent the person may
// use is a choice by name; a person with no agent sees none; and Agent
// operations names where an agent ran as Chat names the conversation.
func TestTodo_AGENTUX_021_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", ""), locale)
		view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, Snapshot: agentMatrixTasks()}
		page := agentMatrixRender(t, BuildAgentsSurface(view))
		choices := agentMatrixBlock(t, page, `class="agents-agent-choices"`, "fieldset")
		for id, name := range map[string]string{"policy-helper": "Policy Helper", "assistant": "Assistant"} {
			if !strings.Contains(choices, `name="agent" type="radio" value="`+id+`"`) || !strings.Contains(choices, `<strong dir="auto">`+name+`</strong>`) {
				t.Errorf("%s: %s is not a named choice: %s", localeID, name, choices)
			}
		}
		if !strings.Contains(choices, "Answers policy questions with citations.") || strings.Contains(page, locale.Text("agents.no_agents")) {
			t.Errorf("%s: the choices carry no purpose, or the page says there are no agents", localeID)
		}
		// Exactly the agents the server listed, plus the general one.
		if got := strings.Count(choices, `name="agent" type="radio"`); got != 3 {
			t.Errorf("%s: %d choices, want the two agents and the general one", localeID, got)
		}
	}
	// A person in no agent's audience: the same page with no agent named.
	english := ResolveProductLocale("en-US")
	outsider := agentMatrixTasks()
	outsider.Agents, outsider.Tasks = nil, nil
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "outsider-1", ""), english)
	view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, Snapshot: outsider}
	page := agentMatrixRender(t, BuildAgentsSurface(view))
	if strings.Contains(page, "Policy Helper") || strings.Contains(page, `value="policy-helper"`) || strings.Contains(page, `value="assistant"`) {
		t.Errorf("a person outside every audience sees an agent: %s", page)
	}

	// Placements are named as Chat names the conversation: the channel with
	// its "#", the direct conversation by the person, never "Conversation 1".
	activity := agentMatrixRender(t, RenderAgentControls(english, agentMatrixActivity(), "done"))
	for _, want := range []string{`<a href="/workspace/app/chat#channel=general"><bdi dir="ltr">#general</bdi></a>`, `<a href="/workspace/app/chat#channel=direct-walt"><bdi>Walt Brennan</bdi></a>`, `<bdi dir="ltr">#benefits</bdi>`} {
		if !strings.Contains(activity, want) {
			t.Errorf("Agent operations does not name the conversation as Chat does: missing %s", want)
		}
	}
	if regexp.MustCompile(`Conversation \d`).MatchString(activity) {
		t.Error("Agent operations names a placement by a number")
	}
}
