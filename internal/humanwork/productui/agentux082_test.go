package productui

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_AGENTUX_082_Browser pins the route-validity surfaces in every
// locale: the Agent setup card shows "Answers until" while the evidence is
// current and "Stopped: evidence expired on ... - Renew" after it lapses; the
// Activity list states the same per agent instead of leaving an expired agent
// on Answering or Ready.
func TestTodo_AGENTUX_082_Browser(t *testing.T) {
	futureRaw := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	pastRaw := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		t.Run(localeID, func(t *testing.T) {
			locale := ResolveProductLocale(localeID)
			futureLabel, ok := personaAdminFormattedDate(locale, futureRaw)
			if !ok {
				t.Fatalf("no date label for %q", futureRaw)
			}
			pastLabel, ok := personaAdminFormattedDate(locale, pastRaw)
			if !ok {
				t.Fatalf("no date label for %q", pastRaw)
			}
			var until, stopped, expired, renew, ask, stoppedPill string
			switch localeID {
			case "de-DE":
				until, stopped, expired = "Antwortet bis "+futureLabel, "Angehalten: die Nachweise sind am "+pastLabel+" abgelaufen.", "Nachweise abgelaufen am "+pastLabel
				renew, ask, stoppedPill = "Erneuern", "Bitten Sie Walt Brennan, ihn zu erneuern.", "Angehalten"
			case "ar":
				until, stopped, expired = "يجيب حتى "+futureLabel, "متوقف: انتهت صلاحية الأدلة في "+pastLabel+".", "انتهت صلاحية الأدلة في "+pastLabel
				renew, ask, stoppedPill = "تجديد", "اطلب من Walt Brennan تجديده.", "متوقف"
			default:
				until, stopped, expired = "Answers until "+futureLabel, "Stopped: evidence expired on "+pastLabel+".", "Evidence expired on "+pastLabel
				renew, ask, stoppedPill = "Renew", "Ask Walt Brennan to renew it.", "Stopped"
			}

			// The setup card while the evidence is current.
			valid := agentUXSetup2Persona()
			valid.Lifecycle, valid.EvidenceExpiresAt, valid.EvidenceExpired = PersonaPublished, futureRaw, false
			card := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, valid, agentUXSetup2Snapshot(valid)))
			if !strings.Contains(card, until) {
				t.Fatalf("current card does not show validity %q: %s", until, card)
			}
			if strings.Contains(card, stopped) || strings.Contains(card, ">"+renew+"<") {
				t.Fatalf("current card claims expiry: %s", card)
			}

			// The setup card after the evidence lapsed: the stopped sentence
			// and a Renew that opens the version editor.
			lapsed := agentUXSetup2Persona()
			lapsed.Lifecycle, lapsed.EvidenceExpiresAt, lapsed.EvidenceExpired = PersonaPublished, pastRaw, true
			editorID := personaAdminVersionEditorID(lapsed)
			lapsedCard := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, lapsed, agentUXSetup2Snapshot(lapsed)))
			for _, want := range []string{stopped, `data-persona-editor-toggle="` + editorID + `"`, ">" + renew + "<"} {
				if !strings.Contains(lapsedCard, want) {
					t.Fatalf("lapsed card is missing %q: %s", want, lapsedCard)
				}
			}
			if strings.Contains(lapsedCard, until) {
				t.Fatalf("lapsed card still promises answers: %s", lapsedCard)
			}

			// Without the version permission Renew names who can renew.
			denied := agentUXSetup2Snapshot(lapsed)
			denied.AllowedCommands = []string{"INSTALL"}
			deniedCard := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, lapsed, denied))
			if !strings.Contains(deniedCard, ask) {
				t.Fatalf("denied lapsed card does not name the owner: %s", deniedCard)
			}
			// The header's Edit keeps its editor toggle; only the banner's
			// Renew button must be gone.
			if strings.Contains(deniedCard, ">"+renew+"<") {
				t.Fatalf("denied lapsed card offers Renew: %s", deniedCard)
			}

			// A version without retained evidence shows no validity either way.
			bare := agentUXSetup2Persona()
			bare.Lifecycle = PersonaPublished
			bareCard := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, bare, agentUXSetup2Snapshot(bare)))
			if strings.Contains(bareCard, until) || strings.Contains(bareCard, stopped) {
				t.Fatalf("card without evidence shows validity: %s", bareCard)
			}

			// Activity: the expired agent is Stopped with its expiry and a
			// Renew link to its setup card; the rest keep their states.
			placed := []PersonaAdminInstallation{{InstallationID: "i1", ConversationID: "general", Conversation: "general", Kind: "channel"}}
			activity := agentUXR7Render(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, AllowedCommands: []string{"SUSPEND", "PUBLISH"}, Agents: []PersonaAdminPersona{
				{ID: "valid-agent", Name: "Valid", Version: "1", Lifecycle: PersonaPublished, Installations: placed, EvidenceExpiresAt: futureRaw},
				{ID: "expired-agent", Name: "Expired", Version: "1", Lifecycle: PersonaPublished, Installations: placed, EvidenceExpiresAt: pastRaw, EvidenceExpired: true},
				{ID: "working", Name: "Working", Version: "1", Lifecycle: PersonaPublished, Installations: placed, RecentRuns: []AgentControlRun{{ID: "r1", State: "running"}}},
				{ID: "quiet", Name: "Quiet", Version: "1", Lifecycle: PersonaPublished, Installations: placed},
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
			stale := row("expired-agent")
			for _, want := range []string{">" + stoppedPill + "<", expired, `href="/workspace/app/admin/personas#persona-admin-expired-agent"`, ">" + renew + "<"} {
				if !strings.Contains(stale, want) {
					t.Fatalf("expired Activity row is missing %q: %s", want, stale)
				}
			}
			if fresh := row("valid-agent"); !strings.Contains(fresh, until) {
				t.Fatalf("current Activity row does not show validity %q: %s", until, fresh)
			}
		})
	}
}
