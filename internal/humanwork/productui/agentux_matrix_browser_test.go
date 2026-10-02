package productui

import (
	"regexp"
	"strings"
	"testing"
)

// agentUXMatrixClient is the server as the page sees it: a snapshot the test
// replaces between reloads, and the reviewer's command.
type agentUXMatrixClient struct {
	personaAdminTestClient
}

func (*agentUXMatrixClient) ReviewPersona(string, string) error { return nil }

// agentUXMatrixPage loads Agent setup the way a browser does on a reload: a
// new page built from nothing but what the server returns, with the product's
// own catalog for the locale.
func agentUXMatrixPage(t *testing.T, locale string, client *agentUXMatrixClient) string {
	t.Helper()
	view := ApplyLocale(NewView(PagePersonaAdmin, "ironridge-demo", "ir-001-walt-brennan", ""), ResolveProductLocale(locale))
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}}
	before := client.snapshotCalls
	markup := personaAdminRender(t, BuildPersonaAdminPage(view, client))
	if client.snapshotCalls != before+1 {
		t.Fatalf("loading the page read the server %d times, want once", client.snapshotCalls-before)
	}
	return markup
}

var agentUXMatrixOption = regexp.MustCompile(`<option[^>]*value="([^"]*)"`)

// TestTodo_AGENTUX_037_Browser renders the whole Agent setup page for an agent
// placed in two conversations. Every placement has its own Remove, "Add to a
// conversation" offers only conversations the agent is not in, and a person
// without those permissions gets neither as a working control.
func TestTodo_AGENTUX_037_Browser(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Installations = []PersonaAdminInstallation{
		{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4"},
		{InstallationID: "install-direct", ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "4"},
	}
	words := map[string]struct{ remove, add string }{
		"en-US": {"Remove", "Add to a conversation"},
		"de-DE": {"Entfernen", "Zu einer Unterhaltung hinzufügen"},
		"ar":    {"إزالة", "إضافة إلى محادثة"},
	}
	for locale, want := range words {
		t.Run(locale, func(t *testing.T) {
			markup := agentUXMatrixPage(t, locale, &agentUXMatrixClient{personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}})
			for _, conversation := range []string{"general", "direct-walt"} {
				row := agentUXMatrixRow(t, markup, conversation)
				// One submit button, in a form that names this agent and this
				// conversation and nothing else: removing here cannot remove there.
				if strings.Count(row, `data-persona-admin-command-form="UNINSTALL"`) != 1 || !strings.Contains(row, `name="conversation_id" type="hidden" value="`+conversation+`"`) ||
					!strings.Contains(row, `name="persona_id" type="hidden" value="policy-helper"`) || !strings.Contains(row, `<button class="button secondary" type="submit">`+want.remove+`</button>`) {
					t.Errorf("placement in %s has no working Remove: %s", conversation, row)
				}
				if strings.Contains(row, "hidden") && strings.Contains(row, `aria-hidden="true"`) {
					t.Errorf("Remove in %s is hidden from an administrator who may remove: %s", conversation, row)
				}
			}
			add := markup[strings.Index(markup, `data-persona-admin-command-form="INSTALL"`):]
			add = add[:strings.Index(add, "</form>")]
			var offered []string
			for _, option := range agentUXMatrixOption.FindAllStringSubmatch(add, -1) {
				offered = append(offered, option[1])
			}
			if len(offered) != 1 || offered[0] != "benefits" {
				t.Errorf("conversations offered by Add = %v, want only the one the agent is not in", offered)
			}
			if !strings.Contains(markup, ">"+want.add+"</button>") {
				t.Errorf("no %q control: %s", want.add, markup)
			}
		})
	}

	// Someone who may open the page but not place agents.
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.AllowedCommands = []string{"CREATE_VERSION", "REQUEST_REVIEW"}
	markup := agentUXMatrixPage(t, "en-US", &agentUXMatrixClient{personaAdminTestClient{snapshot: snapshot}})
	if strings.Contains(markup, `data-persona-admin-command-form="INSTALL"`) || strings.Contains(markup, ">Add to a conversation</button>") {
		t.Errorf("Add to a conversation is offered without the permission: %s", markup)
	}
	for _, conversation := range []string{"general", "direct-walt"} {
		row := agentUXMatrixRow(t, markup, conversation)
		if !strings.Contains(row, `aria-hidden="true"`) || !strings.Contains(row, `<button class="button secondary" disabled type="submit">Remove</button>`) {
			t.Errorf("Remove in %s is a working control without the permission: %s", conversation, row)
		}
	}
}

// agentUXMatrixRow is the placement row of one conversation.
func agentUXMatrixRow(t *testing.T, markup, conversation string) string {
	t.Helper()
	start := strings.Index(markup, `data-conversation-id="`+conversation+`"`)
	if start < 0 {
		t.Fatalf("no placement row for %s: %s", conversation, markup)
	}
	start = strings.LastIndex(markup[:start], "<li")
	end := strings.Index(markup[start:], "</li>")
	// The row holds a nested list of document titles only when documents are
	// projected; these fixtures project none.
	return markup[start : start+end]
}

// TestTodo_AGENTUX_045_Browser walks one version from draft to roll-out and
// reloads the page after every step. What the page offers next comes only from
// what the server returns, so each step must still hold after the reload, and
// the other tenant's administrator is never offered the evaluation.
func TestTodo_AGENTUX_045_Browser(t *testing.T) {
	persona := agentUXSetup2Persona()
	steps := []struct {
		name   string
		server func(*PersonaAdminSnapshot)
		wants  []string
		absent []string
	}{
		{"draft", func(s *PersonaAdminSnapshot) { s.Personas[0].Lifecycle = PersonaDraft },
			[]string{"Step 1 of 4: Draft", `data-persona-command="REQUEST_REVIEW"`, ">Request review</button>"},
			[]string{">Run evaluation</button>", ">Publish version 4</button>"}},
		{"in review, as the reviewer", func(s *PersonaAdminSnapshot) { s.Personas[0].Lifecycle = PersonaInReview },
			[]string{"Step 2 of 4: Waiting for review", `data-persona-review-decision="APPROVE"`, ">Approve version</button>"},
			[]string{">Request review</button>", ">Run evaluation</button>", ">Publish version 4</button>"}},
		{"reviewed", func(s *PersonaAdminSnapshot) {
			s.Personas[0].Lifecycle, s.Personas[0].ReviewApproved = PersonaInReview, true
		},
			[]string{`data-persona-admin-command-form="RUN_EVALUATION"`, `data-evaluation-version="4"`, ">Run evaluation</button>"},
			[]string{">Approve version</button>", ">Publish version 4</button>"}},
		{"evaluated", func(s *PersonaAdminSnapshot) {
			p := &s.Personas[0]
			p.Lifecycle, p.ReviewApproved, p.EvaluationRef, p.EvaluationStatus, p.EvaluationPassed = PersonaInReview, true, "eval-4", "PASSED", 8
		},
			[]string{"Step 4 of 4: Ready to publish", "8 passed, 0 failed", `data-persona-command="PUBLISH"`, ">Publish version 4</button>"},
			// The defect: after a reload the page offered the evaluation again.
			[]string{">Run evaluation</button>", `data-persona-admin-command-form="RUN_EVALUATION"`}},
		{"published", func(s *PersonaAdminSnapshot) {
			p := &s.Personas[0]
			p.Lifecycle, p.ReviewApproved, p.EvaluationRef, p.PublishedAt = PersonaPublished, true, "eval-4", "2026-10-01"
		},
			[]string{"Step 4 of 4: Published", ">Add to a conversation</button>", `data-persona-admin-command-form="INSTALL"`},
			[]string{">Run evaluation</button>", ">Publish version 4</button>"}},
		{"rolled out", func(s *PersonaAdminSnapshot) {
			p := &s.Personas[0]
			p.Lifecycle, p.ReviewApproved, p.EvaluationRef, p.PublishedAt = PersonaPublished, true, "eval-4", "2026-10-01"
			p.Installations = []PersonaAdminInstallation{{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4"}}
		},
			[]string{"#general", "running version 4", `data-persona-admin-command-form="UNINSTALL"`},
			[]string{">Run evaluation</button>", ">Publish version 4</button>"}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			snapshot := agentUXSetup2Snapshot(persona)
			snapshot.Personas = []PersonaAdminPersona{persona}
			step.server(&snapshot)
			client := &agentUXMatrixClient{personaAdminTestClient{snapshot: snapshot}}
			first := agentUXMatrixPage(t, "en-US", client)
			for _, want := range step.wants {
				if !strings.Contains(first, want) {
					t.Errorf("%s: page does not offer %q: %s", step.name, want, first)
				}
			}
			for _, absent := range step.absent {
				if strings.Contains(first, absent) {
					t.Errorf("%s: page still offers %q: %s", step.name, absent, first)
				}
			}
			if reloaded := agentUXMatrixPage(t, "en-US", client); reloaded != first {
				t.Errorf("%s: the page after a reload differs from the page before it", step.name)
			}
		})
	}

	// The other tenant's administrator: the server has no evaluation for that
	// tenant, so the reviewed version shows who runs it and no button.
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.Personas = []PersonaAdminPersona{persona}
	snapshot.Personas[0].ReviewApproved = true
	snapshot.AllowedCommands = []string{"CREATE_VERSION", "REQUEST_REVIEW", "REVIEW", "PUBLISH", "INSTALL", "UNINSTALL"}
	snapshot.EvaluationRuntimeUnavailable = true
	other := agentUXMatrixPage(t, "en-US", &agentUXMatrixClient{personaAdminTestClient{snapshot: snapshot}})
	if strings.Contains(other, `data-persona-admin-command-form="RUN_EVALUATION"`) || strings.Contains(other, ">Run evaluation</button>") {
		t.Errorf("an administrator whose tenant has no evaluation is offered it: %s", other)
	}
	if !strings.Contains(other, `data-evaluation-fallback="policy-helper"`) || !strings.Contains(other, "Loretta Haynes") {
		t.Errorf("the page does not say who runs the evaluation: %s", other)
	}
}

// TestTodo_AGENTUX_047_Browser renders what an administrator sees when a
// placement was stopped: the reason in words, "Start again" once the cause is
// cured, one row per conversation, and the sentence a refused publish shows.
func TestTodo_AGENTUX_047_Browser(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Installations = []PersonaAdminInstallation{
		{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "6", Stopped: true, StoppedReason: PersonaPlacementStoppedNoIdentity},
		{InstallationID: "install-direct", ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "6", Stopped: true, StoppedReason: PersonaPlacementStoppedNotPublished, StartAgain: true},
		{InstallationID: "install-benefits", ConversationID: "benefits", Conversation: "benefits", Kind: "CHANNEL", Version: "6"},
	}
	words := map[string]struct{ notSetUp, notPublished, startAgain, later string }{
		"en-US": {"Stopped: this version is not set up to run.", "Stopped: this version is no longer published.", "Start again", "It can be started again once this is fixed."},
		"de-DE": {"Angehalten: diese Version ist nicht für die Ausführung eingerichtet.", "Angehalten: diese Version ist nicht mehr veröffentlicht.", "Erneut starten", "Er kann erneut gestartet werden, sobald dies behoben ist."},
		"ar":    {"متوقف: هذا الإصدار غير مُعدّ للتشغيل.", "متوقف: هذا الإصدار لم يعد منشورًا.", "بدء التشغيل من جديد", "يمكن بدء تشغيله من جديد بعد إصلاح ذلك."},
	}
	for locale, want := range words {
		t.Run(locale, func(t *testing.T) {
			snapshot := agentUXSetup2Snapshot(persona)
			snapshot.AllowedCommands = append(snapshot.AllowedCommands, "REINSTALL")
			markup := agentUXMatrixPage(t, locale, &agentUXMatrixClient{personaAdminTestClient{snapshot: snapshot}})
			// Not yet cured: the reason and what happens next, and no button.
			general := agentUXMatrixRow(t, markup, "general")
			if !strings.Contains(general, want.notSetUp) || !strings.Contains(general, want.later) || strings.Contains(general, `data-persona-admin-command-form="REINSTALL"`) {
				t.Errorf("stopped placement that cannot start yet: %s", general)
			}
			// Cured: one "Start again" that names this agent and conversation.
			direct := agentUXMatrixRow(t, markup, "direct-walt")
			if !strings.Contains(direct, want.notPublished) || strings.Count(direct, `data-persona-admin-command-form="REINSTALL"`) != 1 ||
				!strings.Contains(direct, `name="conversation_id" type="hidden" value="direct-walt"`) || !strings.Contains(direct, `type="submit">`+want.startAgain+`</button>`) {
				t.Errorf("stopped placement that can start again: %s", direct)
			}
			// A stopped placement is announced and is not described as running.
			for _, row := range []string{general, direct} {
				if !strings.Contains(row, `role="status"`) || !strings.Contains(row, `data-placement-stopped=`) || strings.Contains(row, "running version") || strings.Contains(row, "NO_RUNTIME_IDENTITY</") {
					t.Errorf("stopped placement row: %s", row)
				}
			}
			// A running placement says nothing about stopping.
			if benefits := agentUXMatrixRow(t, markup, "benefits"); strings.Contains(benefits, `data-placement-stopped`) || strings.Contains(benefits, want.startAgain) {
				t.Errorf("running placement carries a stopped notice: %s", benefits)
			}
			// One conversation, one row.
			for _, conversation := range []string{"general", "direct-walt", "benefits"} {
				if got := strings.Count(markup, `data-conversation-id="`+conversation+`"`); got != 1 {
					t.Errorf("%s is listed %d times", conversation, got)
				}
			}
			for _, code := range []string{"AGENT_PRINCIPAL_MISSING_AFTER_RESTORE", PersonaPlacementStoppedNoIdentity + ".", "reason_"} {
				if strings.Contains(markup, code) {
					t.Errorf("the page shows the code %q", code)
				}
			}
		})
	}

	// "Start again" needs the permission that adding an agent needs.
	snapshot := agentUXSetup2Snapshot(persona)
	withoutPermission := agentUXMatrixPage(t, "en-US", &agentUXMatrixClient{personaAdminTestClient{snapshot: snapshot}})
	if direct := agentUXMatrixRow(t, withoutPermission, "direct-walt"); strings.Contains(direct, `data-persona-admin-command-form="REINSTALL"`) || !strings.Contains(direct, "Someone who may add this agent to conversations can start it again.") {
		t.Errorf("Start again without the permission: %s", direct)
	}
	// A reason the page has no sentence for is never shown as it arrived.
	persona.Installations[0].StoppedReason = "pg: relation \"persona_installations\" is locked"
	odd := agentUXMatrixPage(t, "en-US", &agentUXMatrixClient{personaAdminTestClient{snapshot: agentUXSetup2Snapshot(persona)}})
	if strings.Contains(odd, "persona_installations") || !strings.Contains(odd, "Stopped: it needs attention from the person who manages this agent.") {
		t.Errorf("an unknown stop reason reached the page: %s", agentUXMatrixRow(t, odd, "general"))
	}

	// A publish the server refuses because the version cannot run is explained
	// in words in each language, not with the general failure sentence.
	for locale, want := range map[string]string{
		"en-US": "This version cannot be published yet: it is not set up to run in this workspace.",
		"de-DE": "Diese Version kann noch nicht veröffentlicht werden",
		"ar":    "لا يمكن نشر هذا الإصدار بعد",
	} {
		ctx := ResolveProductLocale(locale)
		got := PersonaAdminCommandStatusText(ctx, "runtime_unavailable")
		if !strings.Contains(got, want) || got == PersonaAdminCommandStatusText(ctx, "unavailable") {
			t.Errorf("%s publish refusal sentence = %q", locale, got)
		}
	}
}
