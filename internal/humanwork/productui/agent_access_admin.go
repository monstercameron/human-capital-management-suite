package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentAdminAccessPage renders revisioned connection and skill grants. It
// shows the effective result beside the draft so a publisher can see scope,
// tier and approval consequences before changing live access.
func AgentAdminAccessPage(props AgentAdminAccessPageProps) ui.Node {
	locale := props.Locale
	if props.State == "" {
		props.State = AgentAccessStateUnavailable
	}
	if props.State == AgentAccessStateLoading {
		return agentAdminStatus(locale, AgentAccessStateLoading, "")
	}
	if props.State == AgentAccessStateUnavailable {
		reason := strings.TrimSpace(props.UnavailableReason)
		if reason == "" {
			reason = agentAdminText(locale, "unavailable_detail")
		}
		return agentAdminStatus(locale, AgentAccessStateUnavailable, reason)
	}

	revisions := make([]ui.Node, 0, len(props.Snapshot.Revisions))
	for _, revision := range props.Snapshot.Revisions {
		revisions = append(revisions, agentConnectionRevisionCard(props, revision))
	}
	if len(revisions) == 0 {
		revisions = append(revisions, html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "revisions_empty"))))
	}
	return html.Main(html.Props{Class: "agent-admin-access-page", Dir: string(locale.Direction), Raw: map[string]any{"aria-labelledby": "agent-admin-access-title", "data-agent-access-state": string(props.State)}},
		html.Div(html.Props{Class: "agent-access-hero"}, html.P(html.Props{Class: "eyebrow"}, ui.Text(agentAdminText(locale, "eyebrow"))), html.H1(html.Props{ID: "agent-admin-access-title"}, ui.Text(agentAdminText(locale, "title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "description")))),
		html.Section(html.Props{Class: "agent-admin-revisions", Raw: map[string]any{"aria-labelledby": "agent-admin-revisions-title"}}, html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "agent-admin-revisions-title"}, ui.Text(agentAdminText(locale, "revisions_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "revisions_description")))), html.Div(html.Props{Class: "agent-admin-revision-list"}, revisions...)),
		AgentEffectiveAccessPreviewPanel(AgentEffectiveAccessPreviewProps{I18nProps: props.I18nProps, Ready: props.Snapshot.PreviewReady, Subject: props.Snapshot.PreviewSubject, Preview: props.Snapshot.Preview}),
	)
}

func agentAdminStatus(locale LocaleContext, state AgentAccessLoadState, detail string) ui.Node {
	if state == AgentAccessStateLoading {
		return html.Main(html.Props{Class: "agent-admin-access-page", Raw: map[string]any{"data-agent-access-state": string(state), "role": "status", "aria-live": "polite"}}, html.H1(html.Props{}, ui.Text(agentAdminText(locale, "title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "loading"))))
	}
	return html.Main(html.Props{Class: "agent-admin-access-page", Raw: map[string]any{"data-agent-access-state": string(state)}}, ui.CreateElement(EmptyState, EmptyStateProps{Title: agentAdminText(locale, "unavailable_title"), Description: detail, Role: "status"}))
}

func agentConnectionRevisionCard(props AgentAdminAccessPageProps, revision AgentConnectionRevision) ui.Node {
	locale := props.Locale
	revisionID := "agent-revision-" + safeAgentDOMToken(revision.ID)
	children := []ui.Node{
		html.Div(html.Props{Class: "agent-admin-revision-heading"}, html.Div(html.Props{}, html.H3(html.Props{ID: revisionID + "-title"}, ui.Text(revision.Provider)), html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "revision")+" "+revision.Revision))), html.Span(html.Props{Class: "status"}, ui.Text(revision.Status))),
	}
	if mode := strings.TrimSpace(revision.CredentialMode); mode != "" {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "credential_mode")+": "+mode)))
	}
	if revision.MCPSnapshotID != "" {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "mcp_snapshot")+": "+revision.MCPSnapshotID)))
	}
	grantNodes := make([]ui.Node, 0, len(revision.Grants))
	for _, grant := range revision.Grants {
		grantNodes = append(grantNodes, agentAdminGrantNode(locale, grant))
	}
	if len(grantNodes) == 0 {
		grantNodes = append(grantNodes, html.Li(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "grants_empty"))))
	}
	children = append(children, html.H4(html.Props{}, ui.Text(agentAdminText(locale, "grants_title"))), html.Ul(html.Props{Class: "agent-admin-grants"}, grantNodes...))
	needsSecondAdmin := agentRevisionNeedsSecondAdmin(revision)
	if needsSecondAdmin && !revision.SecondAdminApproved {
		children = append(children, html.Div(html.Props{Class: "agent-admin-warning", Raw: map[string]any{"role": "alert", "data-approval-required": "true"}}, html.Strong(html.Props{}, ui.Text(agentAdminText(locale, "second_admin_required"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "second_admin_detail")))))
	}
	actions := make([]ui.Node, 0, 3)
	if props.Client != nil {
		actions = append(actions,
			html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.CreateConnectionRevision(revision) })}, ui.Text(agentAdminText(locale, "create_revision"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: revision.MCPSnapshotID == "", OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.ImportMCPSnapshot(revision.ID, revision.MCPSnapshotID) })}, ui.Text(agentAdminText(locale, "import_snapshot"))),
		)
		if needsSecondAdmin && !revision.SecondAdminApproved {
			actions = append(actions, html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.RequestSecondAdminApproval(revision.ID) })}, ui.Text(agentAdminText(locale, "request_approval"))))
		} else {
			actions = append(actions, html.Button(html.Props{Class: "button primary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { _ = props.Client.PublishConnectionRevision(revision.ID) })}, ui.Text(agentAdminText(locale, "publish"))))
		}
	} else {
		actions = append(actions, html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "actions_unavailable"))))
	}
	children = append(children, html.Div(html.Props{Class: "agent-admin-actions"}, actions...))
	return html.Article(html.Props{Class: "surface agent-admin-revision", Raw: map[string]any{"aria-labelledby": revisionID + "-title", "data-revision-id": revision.ID}}, children...)
}

func agentAdminGrantNode(locale LocaleContext, grant AgentAdminSkillGrant) ui.Node {
	scopes := make([]string, 0, len(grant.Scopes))
	for _, scope := range grant.Scopes {
		value := strings.TrimSpace(scope.Kind)
		if strings.TrimSpace(scope.Value) != "" {
			value += ": " + strings.TrimSpace(scope.Value)
		}
		scopes = append(scopes, value)
	}
	detail := agentAdminText(locale, "all_scopes")
	if len(scopes) > 0 {
		detail = strings.Join(scopes, ", ")
	}
	tier := strings.ToUpper(strings.TrimSpace(grant.Tier))
	badges := []ui.Node{html.Span(html.Props{Class: "status agent-skill-tier", Raw: map[string]any{"data-skill-tier": tier}}, ui.Text(tier))}
	if grant.RequiresSecondAdmin || agentTierNeedsApproval(grant.Tier) {
		badges = append(badges, html.Span(html.Props{Class: "status warning", Raw: map[string]any{"data-approval-required": "true"}}, ui.Text(agentAdminText(locale, "approval_required"))))
	}
	return html.Li(html.Props{Class: "agent-admin-grant", Raw: map[string]any{"data-skill-tier": tier}}, html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(grant.SkillName)), html.Small(html.Props{Class: "muted"}, ui.Text(detail))), html.Div(html.Props{Class: "agent-admin-grant-badges"}, badges...))
}

func agentTierNeedsApproval(tier string) bool {
	tier = strings.ToUpper(strings.TrimSpace(tier))
	return tier == "T3" || tier == "T4"
}

func agentRevisionNeedsSecondAdmin(revision AgentConnectionRevision) bool {
	if revision.RequiresSecondAdmin || strings.EqualFold(strings.TrimSpace(revision.CredentialMode), "BROKERED") {
		return true
	}
	for _, grant := range revision.Grants {
		if grant.RequiresSecondAdmin || agentTierNeedsApproval(grant.Tier) {
			return true
		}
	}
	return false
}

type AgentEffectiveAccessPreviewProps struct {
	I18nProps
	Ready   bool
	Subject string
	Preview AgentEffectiveAccessPreview
}

// AgentEffectiveAccessPreviewPanel is the shared current-effective-access seam
// used by the runtime RBAC surface and this admin console. It is read-only:
// no grant is widened by previewing it.
func AgentEffectiveAccessPreviewPanel(props AgentEffectiveAccessPreviewProps) ui.Node {
	locale := props.Locale
	if !props.Ready {
		return html.Section(html.Props{Class: "surface agent-effective-access-preview", Raw: map[string]any{"data-preview-state": "unavailable", "role": "status"}}, html.H2(html.Props{}, ui.Text(agentAdminText(locale, "preview_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "preview_unavailable"))))
	}
	connections := make([]ui.Node, 0, len(props.Preview.Connections))
	for _, connection := range props.Preview.Connections {
		connections = append(connections, html.Li(html.Props{Class: "agent-effective-connection"}, html.Strong(html.Props{}, ui.Text(connection.Provider)), html.Small(html.Props{Class: "muted"}, ui.Text(connection.AccountLabel)), agentSkillsList(locale, connection.Skills)))
	}
	if len(connections) == 0 {
		connections = append(connections, html.Li(html.Props{Class: "muted"}, ui.Text(agentAdminText(locale, "preview_empty"))))
	}
	warnings := make([]ui.Node, 0, len(props.Preview.Warnings))
	for _, warning := range props.Preview.Warnings {
		warnings = append(warnings, html.Li(html.Props{Class: "agent-effective-warning"}, ui.Text(warning)))
	}
	subject := strings.TrimSpace(props.Subject)
	if subject == "" {
		subject = strings.TrimSpace(props.Preview.SubjectLabel)
	}
	previewDetail := agentAdminText(locale, "preview_subject") + ": " + subject
	if scope := strings.TrimSpace(props.Preview.ScopeLabel); scope != "" {
		previewDetail += " · " + scope
	}
	previewBody := []ui.Node{html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{ID: "agent-effective-access-preview-title"}, ui.Text(agentAdminText(locale, "preview_title"))), html.P(html.Props{Class: "muted"}, ui.Text(previewDetail))), html.Ul(html.Props{Class: "agent-effective-connections"}, connections...)}
	if len(warnings) > 0 {
		previewBody = append(previewBody, html.H3(html.Props{}, ui.Text(agentAdminText(locale, "warnings_title"))), html.Ul(html.Props{Class: "agent-effective-warnings"}, warnings...))
	}
	return html.Section(html.Props{Class: "surface agent-effective-access-preview", Raw: map[string]any{"data-preview-state": "ready", "aria-labelledby": "agent-effective-access-preview-title"}}, previewBody...)
}

func agentAdminText(locale LocaleContext, key string) string {
	language := strings.ToLower(locale.Resolved)
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if copy, ok := agentAdminCopy[language]; ok {
		if value := strings.TrimSpace(copy[key]); value != "" {
			return value
		}
	}
	return agentAdminCopy["en"][key]
}

var agentAdminCopy = map[string]map[string]string{
	"en": {"eyebrow": "Administration", "title": "Agent connections and grants", "description": "Create revisioned connections, review imported tools, grant skills by scope, and preview the effective access before publication.", "revisions_title": "Connection revisions", "revisions_description": "Every publication is revisioned and reversible. High-impact grants require separation of duties.", "revisions_empty": "No connection revisions are configured.", "revision": "Revision", "credential_mode": "Credential mode", "mcp_snapshot": "MCP tool snapshot", "grants_title": "Skill grants", "grants_empty": "No skills are granted in this revision.", "all_scopes": "All configured scopes", "approval_required": "Second-admin approval required", "second_admin_required": "Publication is waiting for a second administrator", "second_admin_detail": "T3/T4 skills and brokered credentials cannot take effect from one administrator's action.", "create_revision": "Create revision", "import_snapshot": "Import tool snapshot", "request_approval": "Request second-admin approval", "publish": "Publish revision", "actions_unavailable": "Admin actions are unavailable while the service is disconnected.", "preview_title": "Effective access preview", "preview_subject": "Preview subject", "preview_unavailable": "Choose a user or population to load an authorization-filtered preview.", "preview_empty": "The selected subject has no effective agent connections.", "warnings_title": "Review before publishing", "loading": "Loading the agent connection console…", "unavailable_title": "Agent administration is unavailable", "unavailable_detail": "We couldn't load the agent connection console. Try again when the service is available."},
	"de": {"eyebrow": "Administration", "title": "Agentenverbindungen und Berechtigungen", "description": "Erstellen Sie versionierte Verbindungen, prüfen Sie importierte Werkzeuge, vergeben Sie Fähigkeiten nach Geltungsbereich und sehen Sie den effektiven Zugriff vor der Veröffentlichung.", "revisions_title": "Verbindungsrevisionen", "revisions_description": "Jede Veröffentlichung erhält eine Version und kann zurückgenommen werden. Berechtigungen mit hoher Wirkung benötigen Funktionstrennung.", "revisions_empty": "Keine Verbindungsrevisionen konfiguriert.", "revision": "Revision", "credential_mode": "Anmeldemodus", "mcp_snapshot": "MCP-Werkzeugsnapshot", "grants_title": "Fähigkeitsberechtigungen", "grants_empty": "In dieser Revision sind keine Fähigkeiten gewährt.", "all_scopes": "Alle konfigurierten Bereiche", "approval_required": "Zustimmung eines zweiten Administrators erforderlich", "second_admin_required": "Die Veröffentlichung wartet auf einen zweiten Administrator", "second_admin_detail": "Fähigkeiten der Stufen T3/T4 und gebrokerte Anmeldedaten dürfen nicht durch eine einzelne Administratoraktion wirksam werden.", "create_revision": "Revision erstellen", "import_snapshot": "Werkzeugsnapshot importieren", "request_approval": "Zustimmung anfordern", "publish": "Revision veröffentlichen", "actions_unavailable": "Administrationsaktionen sind bei einer getrennten Verbindung nicht verfügbar.", "preview_title": "Vorschau des effektiven Zugriffs", "preview_subject": "Vorschau für", "preview_unavailable": "Wählen Sie eine Person oder Population, um eine autorisierungsgefilterte Vorschau zu laden.", "preview_empty": "Für die ausgewählte Person bestehen keine effektiven Agentenverbindungen.", "warnings_title": "Vor der Veröffentlichung prüfen", "loading": "Die Agentenverwaltung wird geladen…", "unavailable_title": "Agentenverwaltung nicht verfügbar", "unavailable_detail": "Die Agentenverwaltung konnte nicht geladen werden. Versuchen Sie es später erneut."},
	"ar": {"eyebrow": "الإدارة", "title": "اتصالات الوكلاء والمنح", "description": "أنشئ اتصالات بإصدارات، وراجع الأدوات المستوردة، وامنح المهارات حسب النطاق، واعرض الوصول الفعلي قبل النشر.", "revisions_title": "إصدارات الاتصالات", "revisions_description": "كل نشر يحمل إصداراً ويمكن التراجع عنه. تتطلب المنح عالية التأثير فصل المهام.", "revisions_empty": "لا توجد إصدارات اتصالات مهيأة.", "revision": "الإصدار", "credential_mode": "طريقة بيانات الاعتماد", "mcp_snapshot": "لقطة أدوات MCP", "grants_title": "منح المهارات", "grants_empty": "لا توجد مهارات ممنوحة في هذا الإصدار.", "all_scopes": "كل النطاقات المهيأة", "approval_required": "موافقة مسؤول ثانٍ مطلوبة", "second_admin_required": "النشر بانتظار مسؤول ثانٍ", "second_admin_detail": "لا يمكن تفعيل مهارات T3/T4 وبيانات الاعتماد المفوضة من إجراء مسؤول واحد.", "create_revision": "إنشاء إصدار", "import_snapshot": "استيراد لقطة الأدوات", "request_approval": "طلب موافقة مسؤول ثانٍ", "publish": "نشر الإصدار", "actions_unavailable": "إجراءات الإدارة غير متاحة أثناء انقطاع الخدمة.", "preview_title": "معاينة الوصول الفعلي", "preview_subject": "المعاينة لـ", "preview_unavailable": "اختر مستخدماً أو مجموعة لتحميل معاينة مفلترة حسب التفويض.", "preview_empty": "لا توجد اتصالات وكلاء فعالة للجهة المحددة.", "warnings_title": "راجع قبل النشر", "loading": "جارٍ تحميل وحدة إدارة الوكلاء…", "unavailable_title": "إدارة الوكلاء غير متاحة", "unavailable_detail": "تعذر تحميل وحدة إدارة اتصالات الوكلاء. حاول مرة أخرى عند توفر الخدمة."},
}
