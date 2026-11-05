package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// TestTodo_AGENTUX_079_Browser pins AGENTUX-079's GREEN UI beats in the house
// native-run pattern (no server, no browser): the composed Agents page lists
// the four ready-made agents with their skills in plain words, the roster
// renders per-agent skills and icons, and Renew/ask-owner affordances appear
// where a route has lapsed.
//
// Roster expectations mirror PRIMARY TestTodo_AGENTUX_079 exactly:
//   - CatalogListPage: the roster lists every kind while Ask answers only the
//     answering subset (here Policy Helper); Support Desk carries two distinct
//     plain-words skill lines.
//   - DemoSkillDescriptions: ambient lines name their task/reminder work,
//     support lines name their ticket/alert work, nothing says "source-bound".
//   - The fourth ready-made agent per the PRIMARY proofs is Birthday Buddy:
//     Task Catcher, Reminder, Support Desk (the three named in the todo) plus
//     Birthday Buddy, whose schedule the PRIMARY proofs file.
func TestTodo_AGENTUX_079_Browser(t *testing.T) {
	// Skill lines are the exact served-registry descriptions the PRIMARY
	// proofs pin; the roster must show them verbatim, in plain words.
	const taskSkill = "Propose a task from a message for human confirmation"
	const reminderSkill = "Propose a reminder from a message for human confirmation"
	const ticketSkill = "File one support ticket from a stored customer email"
	const alertSkill = "Post the incident alert for a filed ticket"
	const replySkill = "Prepare a bounded plain-text persona reply for the current private chat. Delivery requires a fresh private-chat authorization and a sealed output admission."
	readyMade := []string{"Task Catcher", "Reminder", "Support Desk", "Birthday Buddy"}
	readyMadeIDs := []string{"task-catcher", "reminder", "support-desk", "birthday-buddy"}

	futureRaw := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	pastRaw := time.Now().AddDate(0, 0, -20).Format("2006-01-02")

	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			taskIcon := agenticon.Generate(agenticon.Input{Name: "Task Catcher"})
			supportIcon := agenticon.Generate(agenticon.Input{Name: "Support Desk"})
			if taskIcon == supportIcon {
				t.Fatal("the fixture agents need two different stored icons")
			}
			// Ask offers only the answering subset (Policy Helper); the
			// roster lists every kind, mirroring PRIMARY CatalogListPage's
			// roster-of-two vs Ask-of-one split with the full four plus the
			// answering control.
			snapshot := AgentSnapshot{
				Availability:   AgentsAvailable,
				StartAvailable: true,
				Agents: []AgentSummary{
					{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions.", Status: "Ready", Skills: []string{replySkill}},
				},
				Roster: []AgentSummary{
					{ID: "task-catcher", Name: "Task Catcher", Description: "Notice commitments and requests; offer tasks for human confirmation", Status: "Ready", Skills: []string{taskSkill}, Icon: taskIcon},
					{ID: "reminder", Name: "Reminder", Description: "Notice dates and deadlines; offer reminders for human confirmation", Status: "Ready", Skills: []string{reminderSkill}},
					{ID: "support-desk", Name: "Support Desk", Description: "Turns customer emails into support tickets and tells the incident channel.", Status: "Ready", Skills: []string{ticketSkill, alertSkill}, Icon: supportIcon, IconRevision: 3, IconImageURL: "/api/agent-controls/icons/image?persona_id=support-desk&revision=3"},
					{ID: "birthday-buddy", Name: "Birthday Buddy", Description: "Wishes people a happy birthday in the channel on the day.", Status: "Ready", Skills: []string{replySkill}},
					{ID: "policy-helper", Name: "Policy Helper", Description: "Answers policy questions.", Status: "Ready", Skills: []string{replySkill}},
				},
			}

			// Ask keeps offering only the kind that answers questions.
			ask := agentUXR7Render(t, tagAgentComposer(locale, snapshot))
			if !strings.Contains(ask, "Policy Helper") {
				t.Fatalf("Ask lost Policy Helper: %s", ask)
			}
			for _, excluded := range readyMade {
				if strings.Contains(ask, excluded) {
					t.Fatalf("Ask offers %q, which never answers questions: %s", excluded, ask)
				}
			}

			// The roster helper lists every ready-made agent with its skills
			// and its icon: one uploaded image over its generated icon, one
			// stored icon, and distinct fallbacks for the rest.
			side := agentUXR7Render(t, tagAgentsSidebar(locale, snapshot))
			if !strings.Contains(side, `class="agents-agent-list"`) {
				t.Fatalf("the roster helper lost its agent list: %s", side)
			}
			for _, name := range append(append([]string(nil), readyMade...), "Policy Helper") {
				if !strings.Contains(side, name) {
					t.Fatalf("the roster helper does not list %q: %s", name, side)
				}
			}
			for _, skill := range []string{taskSkill, reminderSkill, ticketSkill, alertSkill, replySkill} {
				if !strings.Contains(side, skill) {
					t.Fatalf("the roster helper does not state the skill %q in plain words: %s", skill, side)
				}
			}
			if ticketSkill == alertSkill {
				t.Fatal("Support Desk's two skill lines are shared")
			}
			if strings.Contains(side, "source-bound") {
				t.Fatalf("the roster helper is not in plain words: %s", side)
			}
			for _, id := range append(append([]string(nil), readyMadeIDs...), "policy-helper") {
				if !strings.Contains(side, `data-agent-id="`+id+`"`) {
					t.Fatalf("the roster helper has no row for agent %q: %s", id, side)
				}
			}
			if !strings.Contains(side, `class="agents-skills"`) || !strings.Contains(side, `class="agents-skill"`) {
				t.Fatalf("the roster helper lost its per-agent skills: %s", side)
			}
			if !strings.Contains(side, `class="agent-icon-photo-img"`) || !strings.Contains(side, "persona_id=support-desk") {
				t.Fatalf("the roster helper does not draw Support Desk's uploaded image: %s", side)
			}
			if stored := agentUXR7Render(t, agenticon.Node(taskIcon)); !strings.Contains(side, stored) {
				t.Fatalf("the roster helper does not draw Task Catcher's stored icon: %s", side)
			}
			if got := strings.Count(side, `class="agent-icon`); got < len(snapshot.Roster) {
				t.Fatalf("the roster helper draws %d agent icons for %d agents: %s", got, len(snapshot.Roster), side)
			}

			// The composed Agents page carries that same roster: the four
			// ready-made agents with their skills and icons, while Ask above
			// it keeps offering only Policy Helper.
			page := renderAgentUXPage5(t, localeID, snapshot, false)
			if !strings.Contains(page, `class="agents-agent-list"`) {
				t.Fatalf("%s: the composed Agents page does not render its roster of the four ready-made agents: %s", localeID, page)
			}
			for _, name := range readyMade {
				if !strings.Contains(page, name) {
					t.Fatalf("%s: the composed Agents page does not list %q: %s", localeID, name, page)
				}
			}
			for _, skill := range []string{taskSkill, reminderSkill, ticketSkill, alertSkill} {
				if !strings.Contains(page, skill) {
					t.Fatalf("%s: the composed Agents page does not state the skill %q in plain words: %s", localeID, skill, page)
				}
			}
			for _, id := range readyMadeIDs {
				if !strings.Contains(page, `data-agent-id="`+id+`"`) {
					t.Fatalf("%s: the composed Agents page has no roster row for agent %q: %s", localeID, id, page)
				}
			}
			if !strings.Contains(page, `class="agent-icon-photo-img"`) || !strings.Contains(page, "persona_id=support-desk") {
				t.Fatalf("%s: the composed Agents page does not draw Support Desk's uploaded image: %s", localeID, page)
			}

			// Where a route has lapsed, the ready-made agent's Setup card and
			// Activity row carry the Renew/ask-owner affordances (AGENTUX-082
			// validity, pinned here for the 079 roster).
			futureLabel, ok := personaAdminFormattedDate(locale, futureRaw)
			if !ok {
				t.Fatalf("no date label for %q", futureRaw)
			}
			pastLabel, ok := personaAdminFormattedDate(locale, pastRaw)
			if !ok {
				t.Fatalf("no date label for %q", pastRaw)
			}
			var until, stopped, expired, renew, askOwner, stoppedPill string
			switch localeID {
			case "de-DE":
				until, stopped, expired = "Antwortet bis "+futureLabel, "Angehalten: die Nachweise sind am "+pastLabel+" abgelaufen.", "Nachweise abgelaufen am "+pastLabel
				renew, askOwner, stoppedPill = "Erneuern", "Bitten Sie Walt Brennan, ihn zu erneuern.", "Angehalten"
			case "ar":
				until, stopped, expired = "يجيب حتى "+futureLabel, "متوقف: انتهت صلاحية الأدلة في "+pastLabel+".", "انتهت صلاحية الأدلة في "+pastLabel
				renew, askOwner, stoppedPill = "تجديد", "اطلب من Walt Brennan تجديده.", "متوقف"
			default:
				until, stopped, expired = "Answers until "+futureLabel, "Stopped: evidence expired on "+pastLabel+".", "Evidence expired on "+pastLabel
				renew, askOwner, stoppedPill = "Renew", "Ask Walt Brennan to renew it.", "Stopped"
			}
			desk := func() PersonaAdminPersona {
				persona := agentUXSetup2Persona()
				persona.ID, persona.Handle, persona.Name = "support-desk", "support-desk", "Support Desk"
				persona.StarterID = "hcmnext.persona_template.support_desk"
				return persona
			}
			valid := desk()
			valid.Lifecycle, valid.EvidenceExpiresAt, valid.EvidenceExpired = PersonaPublished, futureRaw, false
			card := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, valid, agentUXSetup2Snapshot(valid)))
			if !strings.Contains(card, until) {
				t.Fatalf("current Support Desk card does not show validity %q: %s", until, card)
			}
			if strings.Contains(card, stopped) || strings.Contains(card, ">"+renew+"<") {
				t.Fatalf("current Support Desk card claims expiry: %s", card)
			}
			lapsed := desk()
			lapsed.Lifecycle, lapsed.EvidenceExpiresAt, lapsed.EvidenceExpired = PersonaPublished, pastRaw, true
			editorID := personaAdminVersionEditorID(lapsed)
			lapsedCard := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, lapsed, agentUXSetup2Snapshot(lapsed)))
			for _, want := range []string{stopped, `data-persona-editor-toggle="` + editorID + `"`, ">" + renew + "<"} {
				if !strings.Contains(lapsedCard, want) {
					t.Fatalf("lapsed Support Desk card is missing %q: %s", want, lapsedCard)
				}
			}
			if strings.Contains(lapsedCard, until) {
				t.Fatalf("lapsed Support Desk card still promises answers: %s", lapsedCard)
			}
			denied := agentUXSetup2Snapshot(lapsed)
			denied.AllowedCommands = []string{"INSTALL"}
			deniedCard := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, lapsed, denied))
			if !strings.Contains(deniedCard, askOwner) {
				t.Fatalf("denied lapsed Support Desk card does not name the owner: %s", deniedCard)
			}
			if strings.Contains(deniedCard, ">"+renew+"<") {
				t.Fatalf("denied lapsed Support Desk card offers Renew: %s", deniedCard)
			}
			placed := []PersonaAdminInstallation{{InstallationID: "i1", ConversationID: "general", Conversation: "general", Kind: "channel"}}
			activity := agentUXR7Render(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, AllowedCommands: []string{"SUSPEND", "PUBLISH"}, Agents: []PersonaAdminPersona{
				{ID: "task-catcher", Name: "Task Catcher", Version: "1", Lifecycle: PersonaPublished, Installations: placed, EvidenceExpiresAt: futureRaw},
				{ID: "support-desk", Name: "Support Desk", Version: "1", Lifecycle: PersonaPublished, Installations: placed, EvidenceExpiresAt: pastRaw, EvidenceExpired: true},
			}}, "done"))
			row := func(id string) string {
				start := strings.Index(activity, `data-persona-id="`+id+`"`)
				if start < 0 {
					t.Fatalf("no Activity row for agent %q", id)
				}
				rest := activity[start:]
				if next := strings.Index(rest[len(id)+18:], "data-persona-id="); next >= 0 {
					rest = rest[:len(id)+18+next]
				}
				return rest
			}
			stale := row("support-desk")
			for _, want := range []string{">" + stoppedPill + "<", expired, `href="/workspace/app/admin/personas#persona-admin-support-desk"`, ">" + renew + "<"} {
				if !strings.Contains(stale, want) {
					t.Fatalf("expired Support Desk Activity row is missing %q: %s", want, stale)
				}
			}
			if fresh := row("task-catcher"); !strings.Contains(fresh, until) {
				t.Fatalf("current Task Catcher Activity row does not show validity %q: %s", until, fresh)
			}
		})
	}
}
