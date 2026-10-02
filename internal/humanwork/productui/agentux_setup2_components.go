package productui

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	personaAdminInlineSeparator  = " · "
	personaAdminCompactSeparator = " · "
	personaAdminSpace            = " "
	personaAdminSentenceJoin     = ". "
	personaAdminSentenceEnd      = "."
	personaAdminPathSeparator    = "/"
)

func personaAdminPageLinks(locale LocaleContext) ui.Node {
	return AgentPageNavigation(View{Page: PagePersonaAdmin, Locale: locale}, AgentPageSetup)
}

func personaAdminBreadcrumb(locale LocaleContext) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-breadcrumb"},
		html.A(html.Props{Href: Path(PageAdmin)}, ui.Text(personaAdminText(locale, "breadcrumb_admin"))),
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text(personaAdminPathSeparator)),
		html.Span(html.Props{Class: "muted", Aria: map[string]string{"current": "page"}}, ui.Text(personaAdminText(locale, "breadcrumb_agents"))),
	)
}

func personaAdminHeaderActions(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	children := make([]ui.Node, 0, 3)
	if personaAdminVersionEditorAvailable(persona) {
		editorID := personaAdminVersionEditorID(persona)
		class := "button secondary"
		children = append(children, html.Button(html.Props{Class: class, Type: "button", Disabled: !personaAdminCommandAllowed(snapshot, "CREATE_VERSION"), Aria: map[string]string{"controls": editorID, "expanded": "false"}, Raw: map[string]any{"data-persona-editor-toggle": editorID}}, ui.Text(personaAdminVersionText(locale, "disclosure"))))
	}
	children = append(children, personaAdminPauseButton(locale, persona, snapshot))
	if client != nil && persona.Lifecycle != PersonaRetired {
		panelID := "persona-admin-more-" + safeAgentDOMToken(persona.ID)
		children = append(children, html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"popovertarget": panelID, "popovertargetaction": "toggle", "aria-haspopup": "menu", "style": "anchor-name:--agent-more-" + safeAgentDOMToken(persona.ID)}}, ui.Text(personaAdminText(locale, "more"))))
	}
	return html.Div(html.Props{Class: "persona-admin-card-controls"}, children...)
}

func personaAdminVersionStatus(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	live := personaAdminLiveVersionValue(persona)
	if live == "" || live == persona.Version || persona.Lifecycle == PersonaPublished {
		return nil
	}
	template := personaAdminText(locale, "live_and_draft")
	return html.P(html.Props{Class: "persona-admin-version-status", Role: "status"}, ui.Text(strings.NewReplacer("{live}", personaAdminLocalizedNumber(locale, live), "{draft}", personaAdminLocalizedNumber(locale, persona.Version)).Replace(template)))
}

func personaAdminLiveVersionValue(persona PersonaAdminPersona) string {
	live := strings.TrimSpace(persona.LiveVersion)
	if live != "" {
		return live
	}
	for _, installation := range persona.Installations {
		if strings.TrimSpace(installation.Version) != "" && installation.Version != persona.Version {
			return installation.Version
		}
	}
	return ""
}

func personaAdminMoreActionsPanel(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	if client == nil || persona.Lifecycle == PersonaRetired {
		return nil
	}
	actions := make([]ui.Node, 0, 4)
	if persona.Lifecycle == PersonaInReview && !persona.ReviewApproved {
		if _, canReview := client.(PersonaAdminReviewClient); canReview {
			reject := personaAdminAction(locale, persona, "REVIEW", "reject", "review_unavailable", personaAdminCommandAllowed(snapshot, "REVIEW"), false, false)
			actions = append(actions, html.Div(html.Props{Class: "persona-admin-review-decision", Raw: map[string]any{"data-review-decision-wrapper": "REJECT", "data-review-reason-prompt": personaAdminR5SetupText(locale, "rejection_reason")}}, reject))
		}
	}
	if version := personaAdminRollbackVersion(persona); version != "" {
		actions = append(actions, html.Div(html.Props{Class: "persona-admin-action-row"}, html.A(html.Props{Class: "button secondary", Href: personaAdminRollbackHref(persona.ID, version)}, ui.Text(agentUXR7Text(locale, "rollback", "{version}", personaAdminLocalizedNumber(locale, version)))), html.Small(html.Props{Class: "muted"}, ui.Text(agentUXR7Text(locale, "rollback_help")))))
	}
	canRetire := personaAdminCommandAllowed(snapshot, "RETIRE")
	if canRetire {
		question := personaAdminText(locale, "retire_confirm") + personaAdminSpace + persona.Name + "?"
		actions = append(actions, html.Details(html.Props{Class: "persona-admin-confirm"},
			html.Summary(html.Props{}, ui.Text(personaAdminR5SetupText(locale, "retire_agent"))),
			html.P(html.Props{Class: "muted"}, ui.Text(agentUXR7Text(locale, "retire_help"))),
			html.P(html.Props{}, ui.Text(question)),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-command": "RETIRE", "data-persona-confirm": question}}, ui.Text(personaAdminText(locale, "retire_confirm_action"))),
		))
	} else {
		administrator := personaAdminRoleContact(snapshot.SubjectOptions, "agent_administrator")
		reason := personaAdminR5SetupText(locale, "retire_role_reason")
		if administrator.Label != "" {
			reason = strings.ReplaceAll(personaAdminR5SetupText(locale, "retire_role_reason_named"), "{person}", administrator.Label)
		}
		actions = append(actions, html.Div(html.Props{Class: "persona-admin-action-row"},
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: true}, ui.Text(personaAdminR5SetupText(locale, "retire_agent"))),
			html.Small(html.Props{Class: "muted"}, ui.Text(reason)),
		))
	}
	return html.Div(html.Props{ID: "persona-admin-more-" + safeAgentDOMToken(persona.ID), Class: "persona-admin-secondary-actions", Role: "menu", Raw: map[string]any{"popover": "auto", "style": "position-anchor:--agent-more-" + safeAgentDOMToken(persona.ID)}}, actions...)
}

func personaAdminVersionEditorPanel(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	if !personaAdminVersionEditorAvailable(persona) {
		return nil
	}
	if !personaAdminCommandAllowed(snapshot, "CREATE_VERSION") {
		client = nil
	}
	return personaAdminVersionEditor(locale, client, persona, snapshot.DocumentServiceAvailable)
}

func personaAdminRoleContact(options []PersonaAdminTarget, role string) PersonaAdminTarget {
	wanted := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(role)))
	for _, option := range options {
		candidate := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(option.Role)))
		if candidate == wanted || strings.Contains(candidate, wanted) {
			return option
		}
	}
	return PersonaAdminTarget{}
}

func personaAdminSkillTierLabel(locale LocaleContext, skill PersonaAdminSkill) string {
	if strings.EqualFold(lastPersonaIdentifierSegment(skill.ID), "chat_reply") {
		return personaAdminText(locale, "posts_reply")
	}
	if locale.Resolved == "de-DE" && (strings.EqualFold(skill.Tier, "T0") || strings.TrimSpace(skill.Tier) == "0") {
		return "Nur lesen"
	}
	return PersonaTierLabel(locale, skill.Tier)
}

func personaAdminPersonDefinition(locale LocaleContext, key, reference, name, initials, avatarURL string) ui.Node {
	value := ui.Node(ui.Text(personaAdminText(locale, "none_set")))
	if strings.TrimSpace(reference) != "" || strings.TrimSpace(name) != "" {
		value = personaAdminPersonChip(reference, name, initials, avatarURL, personaAdminText(locale, "unknown_person"))
	}
	return html.Div(html.Props{Class: "persona-admin-fact"},
		html.Tag("dt", html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, key))),
		html.Tag("dd", html.Props{}, value),
	)
}

func personaAdminPersonChip(reference, name, initials, avatarURL, unknown string) ui.Node {
	label := PersonaWorkerLabel(reference, name)
	if strings.TrimSpace(name) == "" && strings.TrimSpace(reference) != "" {
		label = strings.TrimSpace(unknown)
	}
	if strings.TrimSpace(label) == "" {
		label = strings.TrimSpace(unknown)
	}
	return html.Span(html.Props{Class: "persona-admin-person-chip"}, personAvatar(label, initials, avatarURL, "tiny"), html.A(html.Props{Href: Path(PagePerson) + "?person=" + url.QueryEscape(reference)}, ui.Text(label)))
}

func personaAdminReadDefinition(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	count := locale.FormatNumber(strconv.Itoa(len(persona.DocumentReferences)), 0)
	label := strings.ReplaceAll(personaAdminR5SetupText(locale, "what_can_read"), "{count}", count)
	linkLabel := agentDocumentPickerText(locale, "field_label") + " (" + count + ")"
	prefix, _, found := strings.Cut(label, agentDocumentPickerText(locale, "field_label"))
	if !found {
		prefix = agentUXR7Text(locale, "read_prefix")
	}
	value := []ui.Node{ui.Text(prefix), html.A(html.Props{Href: "#persona-admin-documents-" + safeAgentDOMToken(persona.ID)}, ui.Text(linkLabel))}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Tag("dt", html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "what_can_read"))), html.Tag("dd", html.Props{}, value...))
}

func personaAdminAudienceDefinition(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	labels := make([]string, 0, len(persona.AudienceRoles)+len(persona.Organizations))
	everyone := false
	roleCount := 0
	for _, raw := range strings.Split(persona.Audience, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		label := personaAdminAudience(locale, PersonaAdminPersona{Audience: raw, AudienceRoles: persona.AudienceRoles, Organizations: persona.Organizations})
		if strings.EqualFold(raw, "employees") {
			everyone = true
			labels = append([]string{label}, labels...)
		} else {
			labels = append(labels, label)
		}
		if !strings.HasPrefix(raw, "org:") {
			roleCount++
		}
	}
	if len(labels) == 0 {
		labels = append(labels, personaAdminText(locale, "none_set"))
	}
	var value ui.Node = ui.Text(strings.Join(labels, ", "))
	if everyone {
		items := make([]ui.Node, 0, len(labels))
		for _, label := range labels {
			items = append(items, html.Li(html.Props{}, ui.Text(label)))
		}
		summary := strings.ReplaceAll(personaAdminR5SetupText(locale, "audience_roles"), "{count}", locale.FormatNumber(strconv.Itoa(roleCount), 0))
		value = html.Details(html.Props{Class: "persona-admin-audience-details"}, html.Summary(html.Props{}, ui.Text(summary)), html.Ul(html.Props{}, items...))
	}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Tag("dt", html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "who_can_use"))), html.Tag("dd", html.Props{}, value))
}

func personaAdminLimitsDefinition(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	value := personaAdminLimits(locale, persona.Limits)
	children := []ui.Node{ui.Text(value)}
	if value == personaAdminText(locale, "no_limits") {
		contact := ui.Node(ui.Text(personaAdminText(locale, "steward")))
		if strings.TrimSpace(persona.Steward) != "" && strings.TrimSpace(persona.StewardName) != "" {
			contact = html.A(html.Props{Href: Path(PagePerson) + "?person=" + url.QueryEscape(persona.Steward)}, ui.Text(persona.StewardName))
		}
		children = append(children, ui.Text(personaAdminInlineSeparator), html.Span(html.Props{}, ui.Text(agentUXR7Text(locale, "limit_help")), ui.Text(personaAdminSpace), contact))
	}
	return html.Div(html.Props{Class: "persona-admin-fact"}, html.Tag("dt", html.Props{Class: "muted"}, ui.Text(personaAdminText(locale, "limits"))), html.Tag("dd", html.Props{}, children...))
}

func personaAdminPlacements(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	rows := make([]ui.Node, 0, len(persona.Installations))
	installed := make(map[string]bool, len(persona.Installations))
	for _, installation := range persona.Installations {
		installed[installation.ConversationID] = true
		rows = append(rows, personaAdminPlacementRow(locale, persona, installation, snapshot))
	}
	content := ui.Node(html.P(html.Props{Class: "muted persona-admin-empty"}, ui.Text(personaAdminText(locale, "no_installations"))))
	if len(rows) > 0 {
		content = html.Ul(html.Props{Class: "persona-admin-installation-list"}, rows...)
	}
	add := ui.Node(nil)
	if personaAdminCommandAllowed(snapshot, "INSTALL") {
		add = personaAdminAddPlacement(locale, persona, snapshot, installed)
	}
	return html.Section(html.Props{Class: "persona-admin-placements", Aria: map[string]string{"label": personaAdminText(locale, "where_installed")}},
		html.Div(html.Props{Class: "persona-admin-subhead"}, html.H4(html.Props{}, ui.Text(personaAdminText(locale, "where_installed"))), add),
		content,
	)
}

func personaAdminPlacementRow(locale LocaleContext, persona PersonaAdminPersona, installation PersonaAdminInstallation, snapshot PersonaAdminSnapshot) ui.Node {
	conversationName := personaAdminPlacementName(locale, installation)
	detail := personaAdminPlacementKind(locale, installation.Kind)
	if installation.Version != "" {
		detail += personaAdminCompactSeparator + strings.ReplaceAll(personaAdminText(locale, "running_version"), "{version}", personaAdminLocalizedNumber(locale, installation.Version))
	}
	placementSummary := []ui.Node{
		html.A(html.Props{Class: "persona-admin-conversation-link", Href: personaAdminConversationHref(installation.ConversationID), Dir: "auto", Raw: map[string]any{"lang": "und"}}, html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(conversationName))),
		html.Span(html.Props{Class: "persona-admin-placement-detail"}, ui.Text(personaAdminCompactSeparator+detail)),
	}
	docsHref := personaAdminConversationHref(installation.ConversationID) + "&tab=docs"
	documentState := personaAdminPlacementDocuments(locale, installation, docsHref)
	remove := ui.Node(nil)
	if personaAdminCommandAllowed(snapshot, "UNINSTALL") {
		remove = html.Form(html.Props{Class: "persona-admin-placement-remove", Raw: map[string]any{"data-persona-admin-command-form": "UNINSTALL"}},
			html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
			html.Input(html.Props{Name: "conversation_id", Type: "hidden", Value: installation.ConversationID}),
			html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(personaAdminText(locale, "remove_placement"))),
		)
	} else if snapshot.CommandPermissionsAvailable {
		remove = html.Form(html.Props{Class: "persona-admin-placement-remove", Hidden: true, Raw: map[string]any{"data-persona-admin-command-form": "UNINSTALL", "aria-hidden": "true"}},
			html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
			html.Input(html.Props{Name: "conversation_id", Type: "hidden", Value: installation.ConversationID}),
			html.Button(html.Props{Class: "button secondary", Type: "submit", Disabled: true}, ui.Text(personaAdminText(locale, "remove_placement"))),
		)
	}
	return html.Li(html.Props{Class: "persona-admin-installation", Raw: map[string]any{"data-installation-id": installation.InstallationID, "data-conversation-id": installation.ConversationID, "data-persona-version": installation.Version}},
		html.Div(html.Props{Class: "persona-admin-placement-main"}, placementSummary...),
		documentState,
		remove,
	)
}

func personaAdminPlacementName(locale LocaleContext, installation PersonaAdminInstallation) string {
	name := strings.TrimSpace(installation.Conversation)
	kind := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(installation.Kind))
	if kind == "channel" || kind == "private_channel" || kind == "public_channel" {
		name = "#" + strings.TrimPrefix(name, "#")
	}
	if kind == "dm" || kind == "direct_message" {
		name = strings.ReplaceAll(personaAdminText(locale, "direct_conversation"), "{person}", strings.TrimPrefix(name, "@"))
	}
	return name
}

func personaAdminPlacementKind(locale LocaleContext, kind string) string {
	normalized := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(kind)))
	if normalized == "channel" {
		return personaAdminText(locale, "conversation_public_channel")
	}
	return personaConversationKind(locale, kind)
}

func personaAdminPlacementDocuments(locale LocaleContext, installation PersonaAdminInstallation, href string) ui.Node {
	if installation.OfficialDocumentCount == nil {
		return html.P(html.Props{Class: "muted persona-admin-placement-documents"}, html.A(html.Props{Href: href}, ui.Text(personaAdminText(locale, "documents_count_unavailable"))))
	}
	count := *installation.OfficialDocumentCount
	if count == 0 {
		return html.P(html.Props{Class: "persona-admin-placement-warning", Role: "status"}, ui.Text(personaAdminText(locale, "no_placed_documents")+" "), html.A(html.Props{Href: href}, ui.Text(personaAdminText(locale, "place_document"))))
	}
	label := personaAdminDocumentsInConversation(locale, count)
	children := []ui.Node{html.A(html.Props{Href: href}, ui.Text(label))}
	if len(installation.OfficialDocumentTitles) > 0 {
		titles := append([]string(nil), installation.OfficialDocumentTitles...)
		sort.Strings(titles)
		items := make([]ui.Node, 0, len(titles))
		for _, title := range titles {
			items = append(items, html.Li(html.Props{}, html.A(html.Props{Href: href}, html.Tag("bdi", html.Props{}, ui.Text(title)))))
		}
		children = append(children, html.Ul(html.Props{Class: "persona-admin-placement-document-titles"}, items...))
	}
	return html.Div(html.Props{Class: "persona-admin-placement-documents"}, children...)
}

func personaAdminDocumentsInConversation(locale LocaleContext, count int) string {
	formatted := locale.FormatNumber(strconv.Itoa(count), 0)
	key := "documents_in_conversation_many"
	if count == 1 {
		key = "documents_in_conversation_one"
	} else if count == 2 && locale.Resolved == "ar" {
		key = "documents_in_conversation_two"
	}
	return strings.ReplaceAll(personaAdminText(locale, key), "{count}", formatted)
}

func personaAdminLocalizedNumber(locale LocaleContext, value string) string {
	if _, err := strconv.Atoi(strings.TrimSpace(value)); err != nil {
		return value
	}
	return locale.FormatNumber(strings.TrimSpace(value), 0)
}

func personaAdminAddPlacement(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot, installed map[string]bool) ui.Node {
	options := make([]ui.Node, 0, len(snapshot.Conversations))
	for _, conversation := range snapshot.Conversations {
		if installed[conversation.ID] {
			continue
		}
		options = append(options, html.Option(html.Props{Value: conversation.ID}, ui.Text(personaAdminConversationOption(locale, conversation))))
	}
	if len(options) == 0 {
		return nil
	}
	id := "persona-admin-add-" + safeAgentDOMToken(persona.ID)
	panelID := id + "-popover"
	return html.Div(html.Props{Class: "persona-admin-add-placement"},
		html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"popovertarget": panelID, "popovertargetaction": "toggle", "style": "anchor-name:--agent-add-" + safeAgentDOMToken(persona.ID)}}, ui.Text(personaAdminText(locale, "add_conversation"))),
		html.Form(html.Props{ID: panelID, Class: "persona-admin-add-placement-form", Raw: map[string]any{"data-persona-admin-command-form": "INSTALL", "popover": "auto", "style": "position-anchor:--agent-add-" + safeAgentDOMToken(persona.ID)}},
			html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
			html.Label(html.Props{For: id}, ui.Text(personaAdminText(locale, "choose_conversation"))),
			html.Select(html.Props{ID: id, Name: "conversation_id", Required: true}, options...),
			html.Div(html.Props{Class: "persona-admin-popover-actions"},
				html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(personaAdminR5SetupText(locale, "add"))),
				html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"popovertarget": panelID, "popovertargetaction": "hide"}}, ui.Text(personaAdminR5SetupText(locale, "cancel"))),
			),
		),
	)
}

func personaAdminConversationHref(conversationID string) string {
	return Path(PageChat) + "#channel=" + url.QueryEscape(conversationID)
}

func personaAdminReviewBlock(locale LocaleContext, persona PersonaAdminPersona, fallback string) ui.Node {
	if persona.Lifecycle == PersonaPublished {
		children := []ui.Node{ui.Text(agentUXR7Text(locale, "reviewed_by") + personaAdminSpace), html.A(html.Props{Href: Path(PagePerson) + "?person=" + url.QueryEscape(persona.Reviewer)}, ui.Text(persona.ReviewerName))}
		if value, ok := personaAdminFormattedDate(locale, persona.ReviewApprovedAt); ok {
			children = append(children, ui.Text(personaAdminInlineSeparator), html.Time(html.Props{Title: persona.ReviewApprovedAt, Raw: map[string]any{"datetime": persona.ReviewApprovedAt}}, ui.Text(value)))
		}
		return html.Div(html.Props{Class: "persona-admin-published-approval", Role: "status", Raw: map[string]any{"data-review-approved": "true"}}, html.P(html.Props{}, children...), personaAdminEvaluationEvidence(locale, persona))
	}
	return html.Div(html.Props{Class: "persona-admin-review", Raw: map[string]any{"role": "status", "data-review-approved": persona.ReviewApproved}}, html.Strong(html.Props{}, ui.Text(personaAdminText(locale, "review_step"))), html.Span(html.Props{Class: "muted"}, ui.Text(fallback)), personaAdminReviewDetail(locale, persona))
}

func personaAdminFormattedDate(locale LocaleContext, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if value, err := time.Parse("2006-01-02", raw); err == nil {
		return ChatDocDateLabel(locale.WithTimeZone("UTC"), value, time.Now()), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if value, err := time.Parse(layout, raw); err == nil {
			return agentTaskDateTimeLabel(locale, value, time.Now()), true
		}
	}
	return "", false
}

func personaAdminNextStep(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	key := "next_draft"
	switch {
	case persona.Lifecycle == PersonaPublished:
		key = "next_published"
	case persona.EvaluationRef != "":
		key = "next_evaluated"
	case strings.EqualFold(persona.EvaluationStatus, "RUNNING"):
		key = "next_evaluation_running"
	case strings.EqualFold(persona.EvaluationStatus, "FAILED"):
		key = "next_evaluation_failed"
	case persona.ReviewApproved:
		key = "next_reviewed"
	case persona.Lifecycle == PersonaInReview:
		key = "next_in_review"
	}
	reviewer := PersonaWorkerLabel(persona.Reviewer, persona.ReviewerName)
	if len(strings.Fields(strings.TrimSpace(reviewer))) < 2 && strings.TrimSpace(persona.Reviewer) != "" {
		reviewer = personaAdminText(locale, "unknown_person")
	}
	if strings.TrimSpace(reviewer) == "" {
		reviewer = personaAdminText(locale, "independent_reviewer")
	}
	text := strings.NewReplacer("{version}", personaAdminLocalizedNumber(locale, persona.Version), "{live}", personaAdminLocalizedNumber(locale, personaAdminLiveVersionValue(persona)), "{reviewer}", reviewer).Replace(personaAdminText(locale, key))
	return html.P(html.Props{Class: "persona-admin-next-step", Role: "status", Raw: map[string]any{"data-persona-next-step": key}}, ui.Text(text))
}

func personaAdminEvaluationFallback(locale LocaleContext, persona PersonaAdminPersona, hidden bool) ui.Node {
	return html.P(html.Props{Class: "persona-admin-evaluation-fallback", Hidden: hidden, Raw: map[string]any{"data-evaluation-fallback": persona.ID}},
		ui.Text(personaAdminText(locale, "evaluation_done_by")+" "),
		personaAdminPersonChip(persona.Steward, persona.StewardName, persona.StewardInitials, persona.StewardAvatarURL, personaAdminText(locale, "unknown_person")),
		ui.Text(personaAdminSentenceJoin+personaAdminText(locale, "message_them")),
	)
}
