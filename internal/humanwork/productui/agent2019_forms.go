package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The console's editing controls (AGENT2-019): create a revision, assign a
// skill's tier, say who may use which skills, import a tool snapshot and preview
// what one person's agents could do. Each is a plain form or select carrying
// data attributes; the page's script posts it and redraws, so the same markup
// works when it is rendered on the server.

func agentAdminEditable(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "DRAFT", "AWAITING_APPROVAL", "APPROVED":
		return true
	}
	return false
}

func agentAdminField(locale LocaleContext, id, labelKey, helpKey, name, value string) ui.Node {
	return html.Div(html.Props{Class: "field"},
		html.Label(html.Props{For: id}, ui.Text(agentAdminFormText(locale, labelKey))),
		html.Input(html.Props{ID: id, Name: name, Type: "text", Value: value, Raw: map[string]any{"autocomplete": "off"}}),
		html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, helpKey))),
	)
}

// agentAdminNewRevisionForm starts a connection's revision: which connection,
// what to call it, whose account it uses, and its skills with their tiers.
func agentAdminNewRevisionForm(locale LocaleContext) ui.Node {
	modes := []ui.Node{
		html.Option(html.Props{Value: "USER_DELEGATED", Selected: true}, ui.Text(agentAdminFormText(locale, "mode_user"))),
		html.Option(html.Props{Value: "BROKERED"}, ui.Text(agentAdminFormText(locale, "mode_brokered"))),
	}
	return html.Form(html.Props{Class: "surface agent-admin-form", Raw: map[string]any{"data-agent-admin-form": "create"}, Aria: map[string]string{"labelledby": "agent-admin-new-title"}},
		html.H3(html.Props{ID: "agent-admin-new-title"}, ui.Text(agentAdminFormText(locale, "new_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, "new_detail"))),
		agentAdminField(locale, "agent-admin-new-connection", "connection", "connection_help", "connection", ""),
		agentAdminField(locale, "agent-admin-new-provider", "provider", "provider_help", "provider", ""),
		html.Div(html.Props{Class: "field"},
			html.Label(html.Props{For: "agent-admin-new-mode"}, ui.Text(agentAdminFormText(locale, "mode"))),
			html.Select(html.Props{ID: "agent-admin-new-mode", Name: "mode"}, modes...),
			html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, "mode_help"))),
		),
		html.Div(html.Props{Class: "field"},
			html.Label(html.Props{For: "agent-admin-new-skills"}, ui.Text(agentAdminFormText(locale, "skills"))),
			html.Textarea(html.Props{ID: "agent-admin-new-skills", Name: "skills", Rows: 4, Raw: map[string]any{"spellcheck": "false"}}),
			html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, "skills_help"))),
		),
		html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(agentAdminFormText(locale, "create")))),
	)
}

// agentAdminTierSelect lets an administrator assign a skill's tier on a revision
// that has not been published. Changing it resets approval, which the page says.
func agentAdminTierSelect(locale LocaleContext, revision AgentConnectionRevision, grant AgentAdminSkillGrant) ui.Node {
	current := strings.ToUpper(strings.TrimSpace(grant.Tier))
	if cut := strings.IndexByte(current, '_'); cut > 0 {
		current = current[:cut]
	}
	options := make([]ui.Node, 0, 5)
	for _, tier := range []string{"T0", "T1", "T2", "T3", "T4"} {
		options = append(options, html.Option(html.Props{Value: tier, Selected: tier == current}, ui.Text(agentTierLabel(locale, tier))))
	}
	id := "agent-tier-" + safeAgentDOMToken(revision.ID) + "-" + safeAgentDOMToken(grant.SkillID)
	return html.Div(html.Props{Class: "agent-admin-tier"},
		html.Label(html.Props{For: id, Class: "muted"}, ui.Text(agentAdminFormText(locale, "tier"))),
		html.Select(html.Props{ID: id, Name: "tier", Raw: map[string]any{"data-agent-admin-tier": "true", "data-revision-id": revision.ID, "data-skill-id": grant.SkillID}}, options...),
	)
}

func agentAdminGrantRows(locale LocaleContext, revision AgentConnectionRevision) ui.Node {
	rows := make([]ui.Node, 0, len(revision.GrantRows))
	for _, row := range revision.GrantRows {
		detail := agentAdminFormText(locale, "roles") + ": " + strings.Join(row.Roles, ", ") + " · " + agentAdminFormText(locale, "population") + ": " + row.Population + " · " + agentAdminFormText(locale, "scopes") + ": " + strings.Join(row.OrganizationScopes, ", ")
		remove := ui.Node(nil)
		if agentAdminEditable(revision.Status) {
			remove = html.Button(html.Props{Class: "button secondary compact", Type: "button", Raw: map[string]any{"data-agent-admin-action": "remove-grant", "data-revision-id": revision.ID, "data-grant-id": row.ID}}, ui.Text(agentAdminFormText(locale, "remove_grant")))
		}
		rows = append(rows, html.Li(html.Props{Class: "agent-admin-grant-row", Raw: map[string]any{"data-grant-id": row.ID}},
			html.Div(html.Props{}, html.Strong(html.Props{}, ui.Text(strings.Join(row.Skills, ", "))), html.Small(html.Props{Class: "muted"}, ui.Text(detail))), remove))
	}
	if len(rows) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, "no_grants")))
	}
	return html.Ul(html.Props{Class: "agent-admin-grant-rows"}, rows...)
}

// agentAdminGrantForm adds one grant: which roles, population and organization
// scope may use which skills. A field left empty is refused, never read as
// "everyone"; the wildcard is typed as * so it is a decision somebody made.
func agentAdminGrantForm(locale LocaleContext, revision AgentConnectionRevision) ui.Node {
	base := "agent-grant-" + safeAgentDOMToken(revision.ID)
	skills := make([]ui.Node, 0, len(revision.Grants))
	for index, grant := range revision.Grants {
		id := base + "-skill-" + strconv.Itoa(index)
		skills = append(skills, html.Div(html.Props{Class: "agent-admin-skill-choice"},
			html.Input(html.Props{ID: id, Name: "skill", Type: "checkbox", Value: grant.SkillID}),
			html.Label(html.Props{For: id}, ui.Text(grant.SkillName+" · "+agentTierLabel(locale, grant.Tier)))))
	}
	return html.Form(html.Props{Class: "agent-admin-grant-form", Raw: map[string]any{"data-agent-admin-form": "grants", "data-revision-id": revision.ID}},
		html.H4(html.Props{}, ui.Text(agentAdminFormText(locale, "grant_title"))),
		html.Fieldset(html.Props{}, append([]ui.Node{html.Legend(html.Props{}, ui.Text(agentAdminFormText(locale, "grant_skills")))}, skills...)...),
		agentAdminField(locale, base+"-roles", "roles", "roles_help", "roles", ""),
		agentAdminField(locale, base+"-population", "population", "population_help", "population", ""),
		agentAdminField(locale, base+"-scopes", "scopes", "scopes_help", "scopes", ""),
		html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(agentAdminFormText(locale, "add_grant")))),
	)
}

func agentAdminImportForm(locale LocaleContext, revision AgentConnectionRevision) ui.Node {
	id := "agent-import-" + safeAgentDOMToken(revision.ID)
	return html.Form(html.Props{Class: "agent-admin-import-form", Raw: map[string]any{"data-agent-admin-form": "import", "data-connection-id": agentRevisionConnection(revision.ID)}},
		agentAdminField(locale, id, "snapshot", "snapshot_help", "snapshot", revision.MCPSnapshotID),
		html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(agentAdminFormText(locale, "import")))),
	)
}

func agentRevisionConnection(revisionID string) string {
	if cut := strings.IndexByte(revisionID, '#'); cut > 0 {
		return revisionID[:cut]
	}
	return revisionID
}

// agentAdminEditControls is what an editable revision shows below its skills:
// who may use them, the form to add a grant and the snapshot import.
func agentAdminEditControls(locale LocaleContext, revision AgentConnectionRevision) []ui.Node {
	if !agentAdminEditable(revision.Status) {
		return []ui.Node{html.H4(html.Props{}, ui.Text(agentAdminFormText(locale, "who_title"))), agentAdminGrantRows(locale, revision)}
	}
	return []ui.Node{
		html.H4(html.Props{}, ui.Text(agentAdminFormText(locale, "who_title"))),
		agentAdminGrantRows(locale, revision),
		agentAdminGrantForm(locale, revision),
		html.H4(html.Props{}, ui.Text(agentAdminFormText(locale, "import_title"))),
		agentAdminImportForm(locale, revision),
	}
}

// agentAdminPreviewForm chooses the person and the revision to preview. The
// answer is what that person's agents could do, filtered by the same grant
// matching the registry applies at call time; nothing is changed.
func agentAdminPreviewForm(locale LocaleContext, revisions []AgentConnectionRevision) ui.Node {
	options := make([]ui.Node, 0, len(revisions))
	for _, revision := range revisions {
		options = append(options, html.Option(html.Props{Value: revision.ID}, ui.Text(revision.Provider+" · "+agentAdminFormText(locale, "revision")+" "+revision.Revision)))
	}
	if len(options) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(agentAdminFormText(locale, "preview_none")))
	}
	return html.Form(html.Props{Class: "surface agent-admin-form", Raw: map[string]any{"data-agent-admin-form": "preview"}, Aria: map[string]string{"labelledby": "agent-admin-preview-title"}},
		html.H3(html.Props{ID: "agent-admin-preview-title"}, ui.Text(agentAdminFormText(locale, "preview_form_title"))),
		html.Div(html.Props{Class: "field"},
			html.Label(html.Props{For: "agent-admin-preview-revision"}, ui.Text(agentAdminFormText(locale, "preview_revision"))),
			html.Select(html.Props{ID: "agent-admin-preview-revision", Name: "revision"}, options...)),
		agentAdminField(locale, "agent-admin-preview-user", "preview_person", "preview_person_help", "user", ""),
		html.Div(html.Props{Class: "action-row"}, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(agentAdminFormText(locale, "preview")))),
	)
}

// agentAdminMessage says what the last action did, in a live region.
func agentAdminMessage(locale LocaleContext, message string) ui.Node {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return html.P(html.Props{Class: "agent-access-message", Role: "status", Raw: map[string]any{"data-agent-admin-message": message, "aria-live": "polite"}}, ui.Text(agentAdminFormText(locale, "msg_"+message)))
}

func agentAdminFormText(locale LocaleContext, key string) string {
	labels, ok := agentAdminFormCopy[key]
	if !ok {
		return key
	}
	language := strings.ToLower(locale.Resolved)
	switch {
	case strings.HasPrefix(language, "de"):
		return labels[1]
	case strings.HasPrefix(language, "ar"):
		return labels[2]
	}
	return labels[0]
}

var agentAdminFormCopy = map[string][3]string{
	"new_title":           {"New connection revision", "Neue Verbindungsrevision", "إصدار اتصال جديد"},
	"new_detail":          {"A revision is a draft until it is published. Nothing it grants takes effect before then.", "Eine Revision bleibt ein Entwurf, bis sie veröffentlicht wird. Bis dahin wird nichts davon wirksam.", "الإصدار مسودة إلى أن يُنشر. لا يسري أي شيء يمنحه قبل ذلك."},
	"connection":          {"Connection", "Verbindung", "الاتصال"},
	"connection_help":     {"The identifier of a connection set up for this workspace.", "Die Kennung einer für diesen Arbeitsbereich eingerichteten Verbindung.", "معرّف اتصال تم إعداده لمساحة العمل هذه."},
	"provider":            {"Name shown to people", "Angezeigter Name", "الاسم المعروض للأشخاص"},
	"provider_help":       {"For example the system's name.", "Zum Beispiel der Name des Systems.", "مثلاً اسم النظام."},
	"mode":                {"Whose account the agents use", "Welches Konto die Agenten verwenden", "الحساب الذي يستخدمه الوكلاء"},
	"mode_user":           {"Each person's own account", "Das eigene Konto jeder Person", "حساب كل شخص بنفسه"},
	"mode_brokered":       {"A shared administrator account (needs a second administrator)", "Ein gemeinsames Administratorkonto (erfordert eine zweite Administration)", "حساب مسؤول مشترك (يتطلب مسؤولاً ثانياً)"},
	"mode_help":           {"A shared account can only read, and always needs a second administrator to approve it.", "Ein gemeinsames Konto kann nur lesen und muss immer von einer zweiten Administration genehmigt werden.", "الحساب المشترك للقراءة فقط ويتطلب دائماً موافقة مسؤول ثانٍ."},
	"skills":              {"Skills and tiers", "Fähigkeiten und Stufen", "المهارات والمستويات"},
	"skills_help":         {"One per line: the skill name, then its tier T0 to T4 (for example workers.read T0). A skill with no tier is T4.", "Eine pro Zeile: der Name der Fähigkeit, dann ihre Stufe T0 bis T4 (zum Beispiel workers.read T0). Ohne Stufe gilt T4.", "واحدة في كل سطر: اسم المهارة ثم مستواها T0 إلى T4 (مثلاً workers.read T0). المهارة دون مستوى تُعد T4."},
	"create":              {"Create revision", "Revision erstellen", "إنشاء إصدار"},
	"tier":                {"What it may do", "Was erlaubt ist", "ما يُسمح به"},
	"roles":               {"Roles", "Rollen", "الأدوار"},
	"roles_help":          {"Separate with commas, or * for every role.", "Mit Kommas trennen, oder * für jede Rolle.", "افصل بفواصل، أو * لكل الأدوار."},
	"population":          {"Population", "Population", "الفئة"},
	"population_help":     {"For example employees, or * for everyone.", "Zum Beispiel employees, oder * für alle.", "مثلاً employees، أو * للجميع."},
	"scopes":              {"Organization scopes", "Organisationsbereiche", "نطاقات المنظمة"},
	"scopes_help":         {"Separate with commas, or * for every organization.", "Mit Kommas trennen, oder * für jede Organisation.", "افصل بفواصل، أو * لكل المنظمات."},
	"who_title":           {"Who may use these skills", "Wer diese Fähigkeiten verwenden darf", "من يمكنه استخدام هذه المهارات"},
	"no_grants":           {"Nobody can use this connection yet.", "Diese Verbindung kann noch niemand verwenden.", "لا أحد يستطيع استخدام هذا الاتصال بعد."},
	"grant_title":         {"Add a grant", "Berechtigung hinzufügen", "إضافة منح"},
	"grant_skills":        {"Skills it covers", "Enthaltene Fähigkeiten", "المهارات التي يشملها"},
	"add_grant":           {"Add grant", "Berechtigung hinzufügen", "إضافة المنح"},
	"remove_grant":        {"Remove", "Entfernen", "إزالة"},
	"import_title":        {"Import tools", "Werkzeuge importieren", "استيراد الأدوات"},
	"snapshot":            {"Tool snapshot", "Werkzeugsnapshot", "لقطة الأدوات"},
	"snapshot_help":       {"The identifier of a captured snapshot. Imported tools that are not read-only start at T4.", "Die Kennung eines erfassten Snapshots. Importierte Werkzeuge, die nicht schreibgeschützt sind, beginnen bei T4.", "معرّف لقطة تم التقاطها. الأدوات المستوردة التي ليست للقراءة فقط تبدأ بالمستوى T4."},
	"import":              {"Import tools", "Werkzeuge importieren", "استيراد الأدوات"},
	"revision":            {"Revision", "Revision", "الإصدار"},
	"preview_form_title":  {"Preview what a person's agents could do", "Vorschau, was die Agenten einer Person tun könnten", "معاينة ما يمكن لوكلاء شخص ما فعله"},
	"preview_revision":    {"Revision to preview", "Zu prüfende Revision", "الإصدار المراد معاينته"},
	"preview_person":      {"Person", "Person", "الشخص"},
	"preview_person_help": {"The person's sign-in name.", "Der Anmeldename der Person.", "اسم تسجيل دخول الشخص."},
	"preview":             {"Preview", "Vorschau", "معاينة"},
	"preview_none":        {"Create a revision to preview it.", "Erstellen Sie eine Revision, um sie in der Vorschau zu sehen.", "أنشئ إصداراً لمعاينته."},
	"msg_created":         {"Revision created as a draft.", "Revision als Entwurf erstellt.", "تم إنشاء الإصدار كمسودة."},
	"msg_updated":         {"Saved. Any earlier approval no longer applies.", "Gespeichert. Eine frühere Genehmigung gilt nicht mehr.", "تم الحفظ. لم تعد أي موافقة سابقة سارية."},
	"msg_requested":       {"Approval requested from a second administrator.", "Genehmigung bei einer zweiten Administration angefragt.", "تم طلب الموافقة من مسؤول ثانٍ."},
	"msg_approved":        {"Approved.", "Genehmigt.", "تمت الموافقة."},
	"msg_published":       {"Published. It is live now.", "Veröffentlicht. Es ist jetzt aktiv.", "تم النشر. وهو نشط الآن."},
	"msg_rolled_back":     {"Rolled back. The earlier revision is live again as a new revision.", "Zurückgesetzt. Die frühere Revision ist als neue Revision wieder aktiv.", "تم التراجع. الإصدار السابق نشط مجدداً كإصدار جديد."},
	"msg_previewed":       {"Preview ready.", "Vorschau bereit.", "المعاينة جاهزة."},
	"msg_denied":          {"You cannot do that. Approval needs a second administrator who passed step-up and who did not write or request the revision.", "Das ist nicht möglich. Die Genehmigung braucht eine zweite Administration mit Step-up, die die Revision weder verfasst noch angefragt hat.", "لا يمكنك فعل ذلك. تتطلب الموافقة مسؤولاً ثانياً اجتاز التحقق الإضافي ولم يكتب الإصدار ولم يطلبه."},
	"msg_conflict":        {"The revision is not in a state that allows that. Refresh and look again.", "Die Revision hat keinen Zustand, der das erlaubt. Aktualisieren Sie und prüfen Sie erneut.", "حالة الإصدار لا تسمح بذلك. حدّث الصفحة وانظر مجدداً."},
	"msg_invalid":         {"That was not accepted. Check the fields and try again.", "Das wurde nicht akzeptiert. Prüfen Sie die Felder und versuchen Sie es erneut.", "لم يتم قبول ذلك. تحقق من الحقول وحاول مرة أخرى."},
	"msg_failed":          {"That did not work. Nothing was changed. Try again.", "Das hat nicht funktioniert. Es wurde nichts geändert. Versuchen Sie es erneut.", "لم تنجح العملية. لم يتم تغيير أي شيء. حاول مرة أخرى."},
	"try_again":           {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
}
