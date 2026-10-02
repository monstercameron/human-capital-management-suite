package productui

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

type PersonaAdminVersionHistory struct {
	Version                                          string
	Lifecycle                                        PersonaAdminLifecycle
	PublishedAt, PublishedBy, Instructions, Guidance string
	ReviewApprovedAt, EvaluationPassedAt             string
	ConversationCount                                int
	Documents                                        []PersonaAdminDocumentReference
	DocumentReferences                               []agentdocref.Reference `json:"-"`
}

func personaAdminAgentSelect(locale LocaleContext, selected string, disabled, invalid bool, options []personaAdminComboboxOption) ui.Node {
	id := "persona-admin-preview-persona"
	items := []ui.Node{html.Option(html.Props{Value: "", Selected: selected == ""}, ui.Text(personaAdminText(locale, "choose_persona_placeholder")))}
	for _, option := range options {
		items = append(items, html.Option(html.Props{Value: option.Value, Selected: option.Value == selected}, ui.Text(option.Label)))
	}
	return html.Div(html.Props{Class: "persona-admin-preview-picker"}, html.Label(html.Props{For: id}, ui.Text(personaAdminText(locale, "choose_persona"))), html.Select(html.Props{ID: id, Disabled: disabled, Raw: map[string]any{"aria-invalid": fmt.Sprint(invalid), "aria-describedby": id + "-error", "data-persona-combobox": "persona"}}, items...), html.P(html.Props{ID: id + "-error", Class: "field-error", Hidden: !invalid, Role: "alert"}, ui.Text(personaAdminPreviewValidationText(locale, "persona"))))
}

func personaAdminPauseButton(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	if persona.Lifecycle != PersonaPublished && persona.Lifecycle != PersonaSuspended {
		return nil
	}
	command, key, confirm := "SUSPEND", "pause", "pause_confirm"
	if persona.Lifecycle == PersonaSuspended {
		command, key, confirm = "PUBLISH", "resume", "resume_confirm"
	}
	allowed := personaAdminCommandAllowed(snapshot, command)
	label := agentUXR7Text(locale, key)
	question := agentUXR7Text(locale, confirm, "{agent}", persona.Name, "{count}", locale.FormatNumber(strconv.Itoa(len(persona.Installations)), 0))
	button := html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: !allowed, Raw: map[string]any{"data-persona-command": command, "data-persona-confirm": question}}, ui.Text(label))
	if allowed {
		return button
	}
	person := personaAdminRoleContact(snapshot.SubjectOptions, "agent_administrator")
	if person.Label == "" {
		person.Label = agentUXR7Text(locale, "agent_administrator")
	}
	return html.Div(html.Props{Class: "persona-admin-pause-unavailable"}, button, html.Small(html.Props{Class: "muted"}, ui.Text(agentUXR7Text(locale, "pause_permission", "{person}", person.Label))))
}

func personaAdminRollbackHref(personaID, version string) string {
	return Path(PageAgentOperations) + "?tab=rollout&agent=" + url.QueryEscape(personaID) + "&version=" + url.QueryEscape(version)
}

func personaAdminRollbackVersion(persona PersonaAdminPersona) string {
	return personaAdminRollbackBefore(persona, persona.Version)
}

func personaAdminRollbackBefore(persona PersonaAdminPersona, failedVersion string) string {
	current, _ := strconv.Atoi(failedVersion)
	best := 0
	for _, row := range persona.VersionHistory {
		version, _ := strconv.Atoi(row.Version)
		if row.Lifecycle == PersonaPublished && version < current && version > best {
			best = version
		}
	}
	if best == 0 {
		return ""
	}
	return strconv.Itoa(best)
}

func personaAdminVersionHistory(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	history := append([]PersonaAdminVersionHistory(nil), persona.VersionHistory...)
	if len(history) == 0 {
		history = append(history, PersonaAdminVersionHistory{Version: persona.Version, Lifecycle: persona.Lifecycle, PublishedAt: persona.PublishedAt, Instructions: persona.Instructions, Guidance: persona.Guidance, Documents: persona.DocumentReferences, ConversationCount: len(persona.Installations)})
	}
	sort.SliceStable(history, func(i, j int) bool {
		a, _ := strconv.Atoi(history[i].Version)
		b, _ := strconv.Atoi(history[j].Version)
		return a > b
	})
	head := []ui.Node{}
	for _, key := range []string{"version", "status", "published", "by", "conversations"} {
		head = append(head, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(agentUXR7Text(locale, key))))
	}
	rows := []ui.Node{}
	for _, row := range history {
		id := "agent-version-" + safeAgentDOMToken(persona.ID) + "-" + row.Version
		at := agentOperationsFormatInstant(locale, row.PublishedAt)
		if at == "" {
			at = "—"
		}
		by := strings.TrimSpace(row.PublishedBy)
		if by == "" {
			by = "—"
		}
		detailPersona := persona
		detailPersona.Version = row.Version
		detailPersona.Guidance = row.Guidance
		detailPersona.Instructions = row.Instructions
		detailPersona.DocumentReferences = row.Documents
		details := html.Details(html.Props{ID: id, Class: "persona-admin-version-view"}, html.Summary(html.Props{}, ui.Text(agentUXR7Text(locale, "view"))), personaAdminInstructionsSection(locale, detailPersona))
		actions := []ui.Node{details}
		version, _ := strconv.Atoi(row.Version)
		current, _ := strconv.Atoi(persona.Version)
		if row.Lifecycle == PersonaPublished && version > 0 && version < current {
			actions = append(actions, html.A(html.Props{Href: personaAdminRollbackHref(persona.ID, row.Version)}, ui.Text(agentUXR7Text(locale, "rollback", "{version}", personaAdminLocalizedNumber(locale, row.Version)))))
		}
		rows = append(rows, html.Tr(html.Props{}, html.Td(html.Props{}, ui.Text(personaAdminLocalizedNumber(locale, row.Version)), html.Div(html.Props{Class: "persona-admin-version-actions"}, actions...)), html.Td(html.Props{}, ui.Text(personaAdminText(locale, "lifecycle_"+strings.ToLower(string(row.Lifecycle))))), html.Td(html.Props{}, html.Time(html.Props{Title: row.PublishedAt, Raw: map[string]any{"datetime": row.PublishedAt}}, ui.Text(at))), html.Td(html.Props{}, ui.Text(by)), html.Td(html.Props{}, ui.Text(locale.FormatNumber(strconv.Itoa(row.ConversationCount), 0)))))
	}
	return html.Details(html.Props{Class: "persona-admin-technical"}, html.Summary(html.Props{}, ui.Text(agentUXR7Text(locale, "history"))), html.Div(html.Props{Class: "agent-version-table-wrap"}, html.Table(html.Props{Class: "persona-admin-version-history"}, html.Thead(html.Props{}, html.Tr(html.Props{}, head...)), html.Tbody(html.Props{}, rows...))))
}

func personaAdminFailureWarning(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	if persona.RecentRunsUnavailable {
		return html.Div(html.Props{Class: "agent-run-warning", Role: "alert"}, html.P(html.Props{}, ui.Text(agentUXR7Text(locale, "history_unavailable"))), html.A(html.Props{Href: Path(PageAgentOperations) + "?tab=running"}, ui.Text(agentUXR7Text(locale, "activity"))))
	}
	if warning, ok := AgentFailureStreak(persona.RecentRuns, persona.ID, persona.Name); ok {
		version := personaAdminRollbackBefore(persona, warning.Version)
		return agentFailureWarning(locale, warning, version, personaAdminRollbackHref(persona.ID, version), agentSetupHref(locale)+"#persona-admin-"+safeAgentDOMToken(persona.ID))
	}
	return nil
}

func agentUXR7ContentLanguage(text string) string {
	for _, r := range text {
		if r >= 0x600 && r <= 0x6ff {
			return "ar"
		}
	}
	return "en"
}
func agentUXR7Purpose(locale LocaleContext, raw, language string) (string, string) {
	switch strings.TrimSpace(raw) {
	case "Answer Ironridge policy questions with citations to current policy documents.", "Answer tenant-wide policy questions with cited policy documents.", "Answer policy questions with citations.":
		return agentUXR7Text(locale, "policy_purpose"), locale.Resolved
	}
	return raw, language
}
func agentUXR7Description(locale LocaleContext, raw string) ui.Node {
	text, language := agentUXR7Purpose(locale, strings.TrimSpace(raw), agentUXR7ContentLanguage(raw))
	if raw == locale.Text("agents.general_agent_purpose") {
		language = locale.Resolved
	}
	return html.Small(html.Props{Class: "muted", Raw: map[string]any{"lang": language}}, html.Tag("bdi", html.Props{}, ui.Text(text)))
}

func agentUXR7Breadcrumb(locale LocaleContext) ui.Node {
	return html.Nav(html.Props{Class: "persona-admin-breadcrumb", Aria: map[string]string{"label": locale.Text("agents.page_title")}}, html.A(html.Props{Href: Path(PageChat)}, ui.Text(locale.Text("page.chat.label"))), html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text(personaAdminPathSeparator)), html.Span(html.Props{Aria: map[string]string{"current": "page"}}, ui.Text(locale.Text("agents.page_title"))))
}
