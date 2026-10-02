package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentAccessPage renders the user's own linked accounts and effective agent
// reach. It has no route or chat integration: callers mount it from the
// authenticated agents area, which keeps provider linking anchored here.
func AgentAccessPage(props AgentAccessPageProps) ui.Node {
	locale := props.Locale
	if props.State == "" {
		props.State = AgentAccessStateUnavailable
	}
	if props.State == AgentAccessStateLoading {
		return agentAccessStatus(locale, AgentAccessStateLoading, "", props.Embedded)
	}
	if props.State == AgentAccessStateUnavailable {
		reason := strings.TrimSpace(props.UnavailableReason)
		if reason == "" {
			reason = agentAccessText(locale, "unavailable_detail")
		}
		return agentAccessStatus(locale, AgentAccessStateUnavailable, reason, props.Embedded)
	}

	children := []ui.Node{
		html.Div(html.Props{Class: "agent-access-hero"},
			html.P(html.Props{Class: "eyebrow"}, ui.Text(agentAccessText(locale, "eyebrow"))),
			agentAccessHeading(props.Embedded, html.Props{ID: "agent-access-title"}, agentAccessText(locale, "title")),
			html.P(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "description"))),
		),
		agentAccessMessage(locale, props.Message),
		agentAccessConnections(props),
		agentDelegationGrants(props),
	}
	return agentAccessContainer(props.Embedded, html.Props{Class: "agent-access-page", Dir: string(locale.Direction), Raw: map[string]any{"aria-labelledby": "agent-access-title", "data-agent-access-state": string(props.State)}}, children...)
}

func agentAccessStatus(locale LocaleContext, state AgentAccessLoadState, detail string, embedded bool) ui.Node {
	if state == AgentAccessStateLoading {
		return agentAccessContainer(embedded, html.Props{Class: "agent-access-page", Raw: map[string]any{"data-agent-access-state": string(state), "role": "status", "aria-live": "polite"}},
			agentAccessHeading(embedded, html.Props{}, agentAccessText(locale, "title")),
			AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingRows, Rows: 3, Status: agentAccessText(locale, "loading")}),
		)
	}
	return agentAccessContainer(embedded, html.Props{Class: "agent-access-page", Raw: map[string]any{"data-agent-access-state": string(state)}},
		ui.CreateElement(EmptyState, EmptyStateProps{
			Title: agentAccessText(locale, "unavailable_title"), Description: detail, Role: "status",
		}),
		html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button primary", Type: "button", Raw: map[string]any{"data-agent-access-action": "retry"}}, ui.Text(agentAccessText(locale, "try_again")))),
	)
}

func agentAccessConnections(props AgentAccessPageProps) ui.Node {
	locale := props.Locale
	items := make([]ui.Node, 0, len(props.Snapshot.Connections))
	for _, connection := range props.Snapshot.Connections {
		items = append(items, agentConnectionCard(props, connection))
	}
	if len(items) == 0 {
		return html.Section(html.Props{Class: "surface agent-access-connections", Raw: map[string]any{"aria-labelledby": "agent-access-connections-title"}},
			html.H2(html.Props{ID: "agent-access-connections-title"}, ui.Text(agentAccessText(locale, "connections_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "connections_empty"))),
		)
	}
	return html.Section(html.Props{Class: "agent-access-connections", Raw: map[string]any{"aria-labelledby": "agent-access-connections-title"}},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "agent-access-connections-title"}, ui.Text(agentAccessText(locale, "connections_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "connections_description")))),
		html.Div(html.Props{Class: "agent-access-connection-list"}, items...),
	)
}

func agentConnectionCard(props AgentAccessPageProps, connection AgentAccessConnection) ui.Node {
	locale := props.Locale
	cardID := "agent-connection-" + safeAgentDOMToken(connection.ID)
	status := agentLinkStateLabel(locale, connection.LinkState)
	meta := []ui.Node{
		html.H3(html.Props{ID: cardID + "-title"}, ui.Text(connection.Provider)),
		html.P(html.Props{Class: "muted"}, ui.Text(connection.AccountLabel)),
		html.Span(html.Props{Class: "status agent-link-state", Raw: map[string]any{"data-link-state": strings.ToLower(strings.TrimSpace(connection.LinkState))}}, ui.Text(status)),
	}
	if mode := strings.TrimSpace(connection.CredentialMode); mode != "" {
		meta = append(meta, html.Small(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "credential_mode")+": "+mode)))
	}
	actions := make([]ui.Node, 0, 2)
	if connection.CanLink && !strings.EqualFold(connection.LinkState, "linked") {
		actions = append(actions, agentLinkButton(props, connection))
	}
	if connection.CanUnlink && strings.EqualFold(connection.LinkState, "linked") {
		actions = append(actions, agentUnlinkButton(props, connection))
	}
	if len(actions) == 0 && props.Client == nil {
		actions = append(actions, html.Small(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "actions_unavailable"))))
	}
	return html.Article(html.Props{Class: "surface agent-access-connection", Raw: map[string]any{"aria-labelledby": cardID + "-title", "data-connection-id": connection.ID}},
		html.Div(html.Props{Class: "agent-access-connection-heading"}, meta...),
		agentSkillsList(locale, connection.Skills),
		html.Div(html.Props{Class: "agent-access-actions"}, actions...),
	)
}

func agentLinkButton(props AgentAccessPageProps, connection AgentAccessConnection) ui.Node {
	button := html.Props{Class: "button secondary agent-link-button", Type: "button", Aria: map[string]string{"label": agentAccessText(props.Locale, "link_account")}, Raw: map[string]any{"data-provider-id": connection.ID, "data-agent-access-action": "link", "data-connection-id": connection.ID}}
	if props.Client == nil {
		button.Disabled = true
		button.Raw["aria-disabled"] = "true"
	} else {
		button.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			start, err := props.Client.StartProviderAuthorization(connection.ID)
			if err != nil || !start.ValidPKCE() || props.OnAuthorization == nil {
				return
			}
			props.OnAuthorization(start)
		})
	}
	return html.Button(button, ui.Text(agentAccessText(props.Locale, "link_account")))
}

func agentUnlinkButton(props AgentAccessPageProps, connection AgentAccessConnection) ui.Node {
	button := html.Props{Class: "button secondary agent-unlink-button", Type: "button", Aria: map[string]string{"label": agentAccessText(props.Locale, "unlink_account")}, Raw: map[string]any{"data-agent-access-action": "unlink", "data-connection-id": connection.ID}}
	if props.Client == nil {
		button.Disabled = true
	} else {
		button.OnClick = ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.UnlinkConnection(connection.ID) })
	}
	return html.Button(button, ui.Text(agentAccessText(props.Locale, "unlink_account")))
}

func agentSkillsList(locale LocaleContext, skills []AgentAccessSkill) ui.Node {
	children := []ui.Node{html.H4(html.Props{}, ui.Text(agentAccessText(locale, "skills_title")))}
	if len(skills) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "skills_empty"))))
		return html.Div(html.Props{Class: "agent-access-skills"}, children...)
	}
	rows := make([]ui.Node, 0, len(skills))
	for _, skill := range skills {
		detail := strings.TrimSpace(skill.Description)
		if detail == "" {
			detail = skill.Scope
		}
		rows = append(rows, html.Li(html.Props{Class: "agent-access-skill", Raw: map[string]any{"data-skill-tier": skill.Tier}},
			html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(skill.Name)), html.Small(html.Props{Class: "muted"}, ui.Text(detail))),
			html.Span(html.Props{Class: "status agent-skill-tier"}, ui.Text(agentTierLabel(locale, skill.Tier))),
		))
	}
	return html.Div(html.Props{Class: "agent-access-skills"}, html.Div(html.Props{Class: "agent-access-subheading"}, children...), html.Ul(html.Props{}, rows...))
}

func agentDelegationGrants(props AgentAccessPageProps) ui.Node {
	locale := props.Locale
	rows := make([]ui.Node, 0, len(props.Snapshot.Delegations))
	for _, grant := range props.Snapshot.Delegations {
		grantID := "agent-grant-" + safeAgentDOMToken(grant.TaskID)
		actions := ui.Node(html.Small(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "not_revocable"))))
		if grant.Revocable && props.Client != nil {
			actions = html.Button(html.Props{Class: "button secondary compact", Type: "button", Raw: map[string]any{"data-agent-access-action": "revoke", "data-task-id": grant.TaskID}, OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.RevokeDelegation(grant.TaskID) })}, ui.Text(agentAccessText(locale, "revoke")))
		}
		rows = append(rows, html.Li(html.Props{Class: "agent-delegation-grant", Raw: map[string]any{"data-grant-id": grant.ID, "aria-labelledby": grantID + "-label"}},
			html.Div(html.Props{}, html.Strong(html.Props{ID: grantID + "-label"}, ui.Text(grant.TaskLabel)), html.Small(html.Props{Class: "muted"}, ui.Text(grant.Scope))),
			html.Div(html.Props{Class: "agent-delegation-expiry"}, html.Small(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "expires")+": "+grant.ExpiresAt)), actions),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, html.Li(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "delegations_empty"))))
	}
	return html.Section(html.Props{Class: "surface agent-delegations", Raw: map[string]any{"aria-labelledby": "agent-delegations-title"}},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "agent-delegations-title"}, ui.Text(agentAccessText(locale, "delegations_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAccessText(locale, "delegations_description")))),
		html.Ul(html.Props{}, rows...),
	)
}

func agentLinkStateLabel(locale LocaleContext, state string) string {
	if strings.EqualFold(strings.TrimSpace(state), "linked") {
		return agentAccessText(locale, "linked")
	}
	return agentAccessText(locale, "not_linked")
}

func safeAgentDOMToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func agentAccessText(locale LocaleContext, key string) string {
	language := strings.ToLower(locale.Resolved)
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if copy, ok := agentAccessCopy[language]; ok {
		if value := strings.TrimSpace(copy[key]); value != "" {
			return value
		}
	}
	return agentAccessCopy["en"][key]
}

var agentAccessCopy = map[string]map[string]string{
	"en": {
		"eyebrow": "Agents", "title": "My agent access", "description": "Review the accounts your agents may use for you, the skills each connection permits, and grants that are still active.",
		"connections_title": "Connected accounts", "connections_description": "Linking starts here and uses the provider's authorization flow. An account is never linked from a chat message.", "connections_empty": "No administrator-granted connections are available for you.",
		"linked": "Linked", "not_linked": "Not linked", "credential_mode": "Credential mode", "link_account": "Link account", "unlink_account": "Unlink account", "actions_unavailable": "Account actions are unavailable while the service is disconnected.",
		"skills_title": "What agents may do", "skills_empty": "No skills are currently granted for this connection.", "delegations_title": "Active task grants", "delegations_description": "These run-bound grants expire automatically. Revoking one blocks the next task step.", "delegations_empty": "No active task grants.", "expires": "Expires", "revoke": "Revoke", "not_revocable": "Managed by the task owner", "tab_ask": "Ask", "tab_access": "My agent access", "tabs_label": "Agents views", "try_again": "Try again", "msg_linked": "Account linked.", "msg_unlinked": "Account unlinked. Agents can no longer act through it.", "msg_revoked": "Grant revoked. The task will not use it for its next step.", "msg_failed": "That did not work. Nothing was changed. Try again.", "msg_state_rejected": "That link request is no longer valid. Start linking again from this page.", "msg_unavailable": "Linking is not available for this connection right now.", "loading": "Loading your agent access…", "unavailable_title": "Agent access is unavailable", "unavailable_detail": "We couldn't load your agent access. Try again when the service is available.",
	},
	"de": {
		"eyebrow": "Agenten", "title": "Mein Agentenzugriff", "description": "Prüfen Sie die Konten, die Ihre Agenten für Sie verwenden dürfen, die Fähigkeiten je Verbindung und noch aktive Berechtigungen.",
		"connections_title": "Verbundene Konten", "connections_description": "Die Verknüpfung beginnt hier und nutzt den Autorisierungsablauf des Anbieters. Ein Konto wird niemals über eine Chatnachricht verknüpft.", "connections_empty": "Für Sie sind keine von Administratoren gewährten Verbindungen verfügbar.",
		"linked": "Verknüpft", "not_linked": "Nicht verknüpft", "credential_mode": "Anmeldemodus", "link_account": "Konto verknüpfen", "unlink_account": "Verknüpfung lösen", "actions_unavailable": "Kontofunktionen sind bei einer getrennten Verbindung nicht verfügbar.",
		"skills_title": "Was Agenten tun dürfen", "skills_empty": "Für diese Verbindung sind derzeit keine Fähigkeiten gewährt.", "delegations_title": "Aktive Aufgabenberechtigungen", "delegations_description": "Diese auf den Lauf begrenzten Berechtigungen laufen automatisch ab. Ein Widerruf blockiert den nächsten Aufgabenschritt.", "delegations_empty": "Keine aktiven Aufgabenberechtigungen.", "expires": "Läuft ab", "revoke": "Widerrufen", "not_revocable": "Vom Aufgabenbesitzer verwaltet", "tab_ask": "Fragen", "tab_access": "Mein Agentenzugriff", "tabs_label": "Ansichten der Agenten", "try_again": "Erneut versuchen", "msg_linked": "Konto verknüpft.", "msg_unlinked": "Verknüpfung gelöst. Agenten können dieses Konto nicht mehr verwenden.", "msg_revoked": "Berechtigung widerrufen. Die Aufgabe nutzt sie im nächsten Schritt nicht mehr.", "msg_failed": "Das hat nicht funktioniert. Es wurde nichts geändert. Versuchen Sie es erneut.", "msg_state_rejected": "Diese Verknüpfungsanfrage ist nicht mehr gültig. Starten Sie die Verknüpfung erneut auf dieser Seite.", "msg_unavailable": "Die Verknüpfung ist für diese Verbindung derzeit nicht verfügbar.", "loading": "Ihr Agentenzugriff wird geladen…", "unavailable_title": "Agentenzugriff nicht verfügbar", "unavailable_detail": "Ihr Agentenzugriff konnte nicht geladen werden. Versuchen Sie es später erneut.",
	},
	"ar": {
		"eyebrow": "الوكلاء", "title": "الوصول الخاص بوكلائي", "description": "راجع الحسابات التي يمكن لوكلائك استخدامها نيابةً عنك، والمهارات المسموح بها لكل اتصال، والتفويضات النشطة.",
		"connections_title": "الحسابات المرتبطة", "connections_description": "يبدأ الربط من هذه الصفحة ويستخدم تدفق تفويض المزوّد. لا يتم ربط أي حساب من رسالة محادثة.", "connections_empty": "لا توجد اتصالات ممنوحة من المسؤولين ومتاحة لك.",
		"linked": "مرتبط", "not_linked": "غير مرتبط", "credential_mode": "طريقة بيانات الاعتماد", "link_account": "ربط الحساب", "unlink_account": "إلغاء ربط الحساب", "actions_unavailable": "إجراءات الحساب غير متاحة أثناء انقطاع الخدمة.",
		"skills_title": "ما يمكن للوكلاء فعله", "skills_empty": "لا توجد مهارات ممنوحة لهذا الاتصال حالياً.", "delegations_title": "تفويضات المهام النشطة", "delegations_description": "تنتهي هذه التفويضات المقيّدة بالمهمة تلقائياً. يؤدي إلغاؤها إلى حظر الخطوة التالية.", "delegations_empty": "لا توجد تفويضات مهام نشطة.", "expires": "ينتهي في", "revoke": "إلغاء", "not_revocable": "يديره مالك المهمة", "tab_ask": "اسأل", "tab_access": "الوصول الخاص بوكلائي", "tabs_label": "عروض الوكلاء", "try_again": "حاول مرة أخرى", "msg_linked": "تم ربط الحساب.", "msg_unlinked": "تم إلغاء ربط الحساب. لم يعد بإمكان الوكلاء العمل عبره.", "msg_revoked": "تم إلغاء التفويض. لن تستخدمه المهمة في خطوتها التالية.", "msg_failed": "لم تنجح العملية. لم يتم تغيير أي شيء. حاول مرة أخرى.", "msg_state_rejected": "طلب الربط هذا لم يعد صالحاً. ابدأ الربط من جديد من هذه الصفحة.", "msg_unavailable": "الربط غير متاح لهذا الاتصال حالياً.", "loading": "جارٍ تحميل وصول وكلائك…", "unavailable_title": "الوصول إلى الوكلاء غير متاح", "unavailable_detail": "تعذر تحميل وصول وكلائك. حاول مرة أخرى عند توفر الخدمة.",
	},
}

// agentAccessMessage says what the last action did, in a live region, so a
// person who unlinked an account or revoked a grant is told it took effect (or
// that it did not) without hunting for the change.
func agentAccessMessage(locale LocaleContext, message string) ui.Node {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return html.P(html.Props{Class: "agent-access-message", Role: "status", Raw: map[string]any{"data-agent-access-message": message, "aria-live": "polite"}}, ui.Text(agentAccessText(locale, "msg_"+message)))
}
