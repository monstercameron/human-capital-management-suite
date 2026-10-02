package productui

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

type AgentAnnouncementRow struct {
	ID, AgentName, ConversationName, Instruction, NextRun, LastResult, MessageHref, State string
	LastOccurrence                                                                        string
	LastRun, LastRunAt, OwnerName                                                         string
	Revision                                                                              uint64
	ResultCode, Reason, NextRunAt                                                         string
	Editor                                                                                AgentAnnouncementEditorValue
	Attempts                                                                              []AgentAnnouncementAttempt
}

type AgentAnnouncementAttempt struct {
	At, TimeLabel, ResultCode, Reason, MessageHref string
}

type AgentAnnouncementEditorValue struct {
	InstallationID, PersonaID, ConversationID, Instruction, Cadence, Time, Zone string
	Weekdays                                                                    []int
	MonthDay                                                                    int
	Documents                                                                   []agentdocref.Reference
}

type AgentAnnouncementsSnapshot struct {
	Available bool
	Loading   bool
	CanCreate bool
	Agents    []AgentAnnouncementAgent
	Rows      []AgentAnnouncementRow
	Status    string
}

type AgentAnnouncementAgent struct {
	InstallationID string
	PersonaID      string
	Name           string
	Conversations  []AgentAnnouncementConversation
}

type AgentAnnouncementConversation struct {
	ID, Name, InstallationID string
}

func AgentAnnouncementsMount(locale LocaleContext) ui.Node {
	return AgentAnnouncementsMountForTab(locale, true)
}

func AgentAnnouncementsMountForTab(locale LocaleContext, selected bool) ui.Node {
	return html.Div(html.Props{ID: "agent-announcements", Class: "agent-announcements", Hidden: !selected, Aria: map[string]string{"labelledby": "agent-operations-tab-announcements"}, Raw: map[string]any{"role": "tabpanel", "data-agent-operations-panel": "announcements", "data-locale": locale.Resolved, "data-agent-announcements-mount": "true", "data-agent-announcements-endpoint": "/api/agent-controls/announcements"}},
		html.Tag("style", html.Props{}, ui.Text(AgentAnnouncementsStyles)),
		RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Loading: true}),
	)
}

func RenderAgentAnnouncements(locale LocaleContext, snapshot AgentAnnouncementsSnapshot) ui.Node {
	copy := func(key string) string { return agentAnnouncementText(locale, key) }
	children := []ui.Node{html.Tag("style", html.Props{}, ui.Text(AgentAnnouncementsStyles)),
		html.Header(html.Props{Class: "agent-announcements-header"}, html.Div(html.Props{}, html.H2(html.Props{}, ui.Text(copy("title"))), html.P(html.Props{Class: "muted"}, ui.Text(copy("help")))), html.Button(html.Props{Type: "button", Class: "button primary", Disabled: !snapshot.CanCreate, Raw: map[string]any{"data-announcement-new": "true"}}, ui.Text(copy("new")))),
	}
	if snapshot.Loading {
		children = append(children, html.P(html.Props{Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite"}}, ui.Text(copy("loading"))))
	} else if !snapshot.Available {
		children = append(children, html.P(html.Props{Class: "empty-state", Role: "alert"}, ui.Text(copy("failed"))), html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-announcement-retry": "true"}}, ui.Text(copy("retry"))))
	} else if len(snapshot.Rows) == 0 {
		children = append(children, html.P(html.Props{Class: "empty-state", Role: "status"}, ui.Text(copy("empty"))))
	} else {
		rows := make([]ui.Node, 0, len(snapshot.Rows))
		for _, row := range snapshot.Rows {
			rows = append(rows, renderAgentAnnouncementRow(locale, row))
		}
		children = append(children, html.Div(html.Props{Class: "agent-announcement-list"}, rows...))
	}
	children = append(children, renderAgentAnnouncementEditor(locale, snapshot.Agents), html.P(html.Props{ID: "announcement-status", Class: "agent-announcement-status", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true", "data-announcement-status": "true"}}, ui.Text(snapshot.Status)))
	return html.Section(html.Props{Class: "surface agent-announcements-panel", Aria: map[string]string{"label": copy("title")}}, children...)
}

func renderAgentAnnouncementRow(locale LocaleContext, row AgentAnnouncementRow) ui.Node {
	stateAction := "pause"
	stateLabel := agentAnnouncementText(locale, "pause")
	if strings.EqualFold(row.State, "PAUSED") {
		stateAction, stateLabel = "resume", agentAnnouncementText(locale, "resume")
	}
	resultText := row.LastResult
	if row.ResultCode != "" {
		resultText = AgentAnnouncementResultText(locale, row.ResultCode, row.Reason)
	}
	next := row.NextRun
	if row.NextRunAt != "" && next == "" {
		if at, err := time.Parse(time.RFC3339, row.NextRunAt); err == nil {
			next = at.Format("2006-01-02 15:04 MST")
		}
	}
	if next == "" {
		next = agentAnnouncementText(locale, "no_future")
	}
	if row.State == "PAUSED" {
		next = agentAnnouncementText(locale, "paused")
	}
	lastRun := row.LastRun
	if lastRun == "" && row.LastRunAt != "" {
		if at, err := time.Parse(time.RFC3339, row.LastRunAt); err == nil {
			lastRun = at.Format("2006-01-02 15:04 MST")
		}
	}
	result := ui.Node(html.Span(html.Props{Class: "muted", Text: resultText}))
	if row.MessageHref != "" {
		result = html.A(html.Props{Href: row.MessageHref, Text: resultText})
	}
	postAction, postLabel := "post", announcementRowPostLabel(locale, row.ResultCode)
	if row.Reason == "The preview expired or its documents or authority changed. Preview again before posting." {
		postAction, postLabel = "preview-again", agentAnnouncementText(locale, "preview_again")
	}
	return html.Article(html.Props{Class: "agent-announcement-row", Raw: map[string]any{"data-announcement-id": row.ID, "data-announcement-revision": row.Revision, "data-announcement-retry-occurrence": announcementRetryOccurrence(row)}},
		html.Div(html.Props{Class: "agent-announcement-row-main"},
			html.H3(html.Props{Dir: "auto"}, ui.Text(row.AgentName+" · "+row.ConversationName)),
			html.P(html.Props{Class: "muted", Dir: "auto"}, ui.Text(strings.ReplaceAll(agentAnnouncementText(locale, "set_by"), "{owner}", row.OwnerName))),
			html.P(html.Props{Class: "muted"}, ui.Text(lastRun)),
			html.P(html.Props{Class: "agent-announcement-instruction", Dir: "auto"}, ui.Text(firstAnnouncementLine(row.Instruction))),
			renderAnnouncementAttempts(locale, row.Attempts),
			html.Tag("dl", html.Props{}, html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(agentAnnouncementText(locale, "next"))), html.Tag("dd", html.Props{}, ui.Text(next))), html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(agentAnnouncementText(locale, "last"))), html.Tag("dd", html.Props{}, result))),
		),
		html.Div(html.Props{Class: "agent-announcement-row-actions"},
			html.Button(html.Props{Type: "button", Class: "button secondary compact", Raw: map[string]any{"data-announcement-action": stateAction}}, ui.Text(stateLabel)),
			html.Button(html.Props{Type: "button", Class: "button secondary compact", Raw: map[string]any{"data-announcement-action": postAction}}, ui.Text(postLabel)),
			html.Button(html.Props{Type: "button", Class: "button secondary compact", Raw: map[string]any{"data-announcement-action": "edit"}}, ui.Text(agentAnnouncementText(locale, "edit"))),
			html.Button(html.Props{Type: "button", Class: "button secondary compact", Raw: map[string]any{"data-announcement-action": "confirm-delete"}}, ui.Text(agentAnnouncementText(locale, "delete"))),
		),
		html.Div(html.Props{Class: "agent-announcement-delete", Hidden: true, Raw: map[string]any{"data-announcement-delete-confirmation": "true"}}, html.P(html.Props{}, ui.Text(agentAnnouncementText(locale, "delete_confirm"))), html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-announcement-action": "cancel-delete"}}, ui.Text(agentAnnouncementText(locale, "cancel"))), html.Button(html.Props{Type: "button", Class: "button danger", Raw: map[string]any{"data-announcement-action": "delete"}}, ui.Text(agentAnnouncementText(locale, "delete_yes")))),
	)
}

func renderAgentAnnouncementEditor(locale LocaleContext, agents []AgentAnnouncementAgent) ui.Node {
	copy := func(key string) string { return agentAnnouncementText(locale, key) }
	agentOptions := []ui.Node{html.Option(html.Props{Value: ""}, ui.Text(copy("choose_agent")))}
	conversationOptions := []ui.Node{html.Option(html.Props{Value: ""}, ui.Text(copy("choose_conversation")))}
	seenAgents := map[string]bool{}
	for _, agent := range agents {
		if seenAgents[agent.PersonaID] {
			continue
		}
		seenAgents[agent.PersonaID] = true
		conversations, _ := json.Marshal(AgentAnnouncementConversations(agents, agent.PersonaID))
		agentOptions = append(agentOptions, html.Option(html.Props{Value: agent.PersonaID, Raw: map[string]any{"data-persona-id": agent.PersonaID, "data-announcement-conversations": string(conversations)}}, ui.Text(agent.Name)))
	}
	return html.Form(html.Props{Class: "agent-announcement-editor", Hidden: true, Raw: map[string]any{"data-announcement-editor": "true", "novalidate": true}},
		html.H3(html.Props{Raw: map[string]any{"data-announcement-editor-title": "true"}}, ui.Text(copy("new"))),
		html.Div(html.Props{Class: "agent-announcement-grid"},
			html.Label(html.Props{For: "announcement-agent"}, ui.Text(copy("agent"))), html.Select(html.Props{ID: "announcement-agent", Required: true, Raw: map[string]any{"data-announcement-agent": "true", "aria-describedby": "announcement-agent-error"}}, agentOptions...), announcementFieldError("agent"),
			html.Label(html.Props{For: "announcement-conversation"}, ui.Text(copy("conversation"))), html.Select(html.Props{ID: "announcement-conversation", Required: true, Raw: map[string]any{"data-announcement-conversation": "true", "aria-describedby": "announcement-conversation-error"}}, conversationOptions...), announcementFieldError("conversation"),
		),
		html.Label(html.Props{For: "announcement-instruction"}, ui.Text(copy("instruction"))),
		html.Textarea(html.Props{ID: "announcement-instruction", Required: true, Rows: 4, Placeholder: copy("example"), Raw: map[string]any{"maxlength": "1000", "aria-describedby": "announcement-counter announcement-example announcement-instruction-error", "data-announcement-instruction": "true"}}),
		announcementFieldError("instruction"),
		html.Div(html.Props{Class: "agent-announcement-field-help"}, html.Small(html.Props{ID: "announcement-example", Class: "muted"}, ui.Text(copy("example"))), html.Tag("output", html.Props{ID: "announcement-counter", For: "announcement-instruction", Class: "muted", Raw: map[string]any{"data-announcement-counter": "true"}}, ui.Text("0 / 1000"))),
		AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "announcement-documents", Available: true, State: AgentDocumentPickerIdle, ReferenceLimit: 5, DefaultVersionMode: "LATEST_PUBLISHED", LockedVersionMode: false}), announcementFieldError("documents"),
		html.Fieldset(html.Props{Class: "agent-announcement-when", Raw: map[string]any{"data-announcement-when": "true", "tabindex": "-1", "aria-describedby": "announcement-when-error"}}, html.Legend(html.Props{}, ui.Text(copy("when"))),
			html.Label(html.Props{}, html.Input(html.Props{Type: "radio", Name: "announcement-when", Value: "NOW", Checked: true}), ui.Text(copy("now"))),
			html.Label(html.Props{}, html.Input(html.Props{Type: "radio", Name: "announcement-when", Value: "DAILY"}), ui.Text(copy("daily"))),
			html.Label(html.Props{}, html.Input(html.Props{Type: "radio", Name: "announcement-when", Value: "WEEKLY"}), ui.Text(copy("weekly"))),
			html.Label(html.Props{}, html.Input(html.Props{Type: "radio", Name: "announcement-when", Value: "MONTHLY"}), ui.Text(copy("monthly"))),
			html.Div(html.Props{Class: "agent-announcement-time"}, html.Label(html.Props{For: "announcement-time"}, ui.Text(copy("time"))), html.Input(html.Props{ID: "announcement-time", Type: "time", Raw: map[string]any{"data-announcement-time": "true", "aria-describedby": "announcement-time-error"}}), announcementFieldError("time"), html.Label(html.Props{For: "announcement-zone"}, ui.Text(copy("zone"))), html.Select(html.Props{ID: "announcement-zone", Raw: map[string]any{"data-announcement-zone": "true", "aria-describedby": "announcement-zone-error"}}), announcementFieldError("zone")),
			html.Fieldset(html.Props{Raw: map[string]any{"data-announcement-weekdays": "true", "tabindex": "-1", "aria-describedby": "announcement-weekdays-error"}}, append([]ui.Node{html.Legend(html.Props{}, ui.Text(copy("weekdays")))}, announcementWeekdayChoices(locale)...)...), announcementFieldError("weekdays"),
			html.Label(html.Props{For: "announcement-month-day"}, ui.Text(copy("month_day"))), html.Input(html.Props{ID: "announcement-month-day", Type: "number", Raw: map[string]any{"min": "1", "max": "31", "data-announcement-month-day": "true", "aria-describedby": "announcement-month-help announcement-month-day-error"}}),
			announcementFieldError("month-day"), html.Small(html.Props{ID: "announcement-month-help", Class: "muted"}, ui.Text(copy("month_help"))),
		),
		announcementFieldError("when"),
		html.Div(html.Props{Class: "agent-announcement-editor-actions"}, html.Button(html.Props{Type: "button", Class: "button secondary", Raw: map[string]any{"data-announcement-action": "preview"}}, ui.Text(copy("preview"))), html.Button(html.Props{Type: "button", Class: "button primary", Raw: map[string]any{"data-announcement-action": "post"}}, ui.Text(copy("post_now"))), html.Button(html.Props{Type: "submit", Class: "button primary", Hidden: true, Raw: map[string]any{"data-announcement-action": "save", "aria-describedby": "announcement-save-help"}}, ui.Text(copy("save")))),
		html.Small(html.Props{ID: "announcement-save-help", Class: "muted", Hidden: true}, ui.Text(copy("save_help"))),
		html.Section(html.Props{Class: "agent-announcement-preview", Hidden: true, Raw: map[string]any{"data-announcement-preview": "true", "aria-live": "polite"}}, html.H4(html.Props{}, ui.Text(copy("preview_title")))),
	)
}

func firstAnnouncementLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	return value
}

func agentAnnouncementText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"working":             {"Working on your announcement. Please wait.", "Ihre Ankündigung wird bearbeitet. Bitte warten Sie.", "جارٍ إعداد إعلانك. يرجى الانتظار."},
		"preview_again":       {"Preview again", "Neue Vorschau erstellen", "أنشئ معاينة جديدة"},
		"save_before_preview": {"Save your changes, then preview the saved announcement before posting.", "Speichern Sie Ihre Änderungen und erstellen Sie vor dem Veröffentlichen eine Vorschau der gespeicherten Ankündigung.", "احفظ تغييراتك ثم عاين الإعلان المحفوظ قبل النشر."},
		"request_failed":      {"The announcement could not be completed. Try again.", "Die Ankündigung konnte nicht abgeschlossen werden. Versuchen Sie es erneut.", "تعذر إكمال الإعلان. حاول مرة أخرى."},
		"set_by":              {"Set by {owner}", "Eingerichtet von {owner}", "أعدّه {owner}"},
		"sources":             {"Sources", "Quellen", "المصادر"},
		"error_agent":         {"Choose an agent.", "Wählen Sie einen Agenten.", "اختر وكيلاً."},
		"error_conversation":  {"Choose a conversation for this agent.", "Wählen Sie eine Unterhaltung für diesen Agenten.", "اختر محادثة لهذا الوكيل."},
		"error_instruction":   {"Describe what to post, using up to 1,000 characters.", "Beschreiben Sie mit höchstens 1.000 Zeichen, was veröffentlicht werden soll.", "صف ما يجب نشره في ١٬٠٠٠ حرف كحد أقصى."},
		"error_documents":     {"Add one to five published documents.", "Fügen Sie ein bis fünf veröffentlichte Dokumente hinzu.", "أضف من مستند واحد إلى خمسة مستندات منشورة."},
		"error_when":          {"Choose when to post.", "Wählen Sie, wann veröffentlicht werden soll.", "اختر موعد النشر."},
		"error_weekdays":      {"Choose at least one weekday.", "Wählen Sie mindestens einen Wochentag.", "اختر يوماً واحداً على الأقل من أيام الأسبوع."},
		"error_month-day":     {"Choose a day from 1 to 31.", "Wählen Sie einen Tag von 1 bis 31.", "اختر يوماً من ١ إلى ٣١."},
		"error_time":          {"Choose a valid time.", "Wählen Sie eine gültige Uhrzeit.", "اختر وقتاً صالحاً."},
		"error_zone":          {"Choose a time zone.", "Wählen Sie eine Zeitzone.", "اختر منطقة زمنية."},

		"schedule_changed": {"The announcement changed or was paused before this post could run. Save or resume it to schedule the next post.", "Die Ankündigung wurde geändert oder vor der Veröffentlichung pausiert. Speichern oder setzen Sie sie fort, um die nächste Veröffentlichung zu planen.", "تم تغيير الإعلان أو إيقافه قبل تشغيل هذا المنشور. احفظه أو استأنفه لجدولة المنشور التالي."},
		"preview_changed":  {"The preview expired or its documents or authority changed. Preview again before posting.", "Die Vorschau ist abgelaufen oder ihre Dokumente oder Berechtigungen haben sich geändert. Erstellen Sie vor dem Veröffentlichen eine neue Vorschau.", "انتهت صلاحية المعاينة أو تغيرت المستندات أو الصلاحيات. أنشئ معاينة جديدة قبل النشر."},
		"denied":           {"Your access has changed. Check access to the agent and documents, then try again.", "Ihre Zugriffsrechte haben sich geändert. Prüfen Sie den Zugriff auf den Agenten und die Dokumente und versuchen Sie es erneut.", "تغيرت صلاحياتك. تحقق من الوصول إلى الوكيل والمستندات، ثم حاول مرة أخرى."},
		"edit_title":       {"Edit announcement", "Ankündigung bearbeiten", "تعديل الإعلان"}, "save_changes": {"Save changes", "Änderungen speichern", "حفظ التغييرات"}, "save_help": {"Choose a recurrence to save a schedule.", "Wählen Sie eine Wiederholung, um einen Zeitplan zu speichern.", "اختر تكرارًا لحفظ جدول."},
		"retry": {"Try again", "Erneut versuchen", "حاول مرة أخرى"}, "weekdays": {"On these weekdays", "An diesen Wochentagen", "في أيام الأسبوع هذه"}, "month_day": {"Day of the month", "Tag des Monats", "يوم الشهر"}, "month_help": {"Months without that day are skipped.", "Monate ohne diesen Tag werden übersprungen.", "يتم تخطي الأشهر التي لا تحتوي على هذا اليوم."},
		"no_future": {"No future post", "Keine weitere Veröffentlichung", "لا يوجد نشر قادم"}, "paused": {"Paused", "Pausiert", "متوقف مؤقتًا"}, "posted": {"Posted. Open the message.", "Veröffentlicht. Nachricht öffnen.", "تم النشر. افتح الرسالة."}, "not_posted": {"Not posted yet", "Noch nicht veröffentlicht", "لم يتم النشر بعد"}, "refused": {"Not posted: {reason}", "Nicht veröffentlicht: {reason}", "لم يتم النشر: {reason}"}, "failure": {"Failed: {reason}", "Fehlgeschlagen: {reason}", "فشل: {reason}"},
		"read_failed":  {"The documents could not be read with the agent's current access. Choose readable documents and try again.", "Die Dokumente konnten mit den aktuellen Rechten des Agenten nicht gelesen werden. Wählen Sie lesbare Dokumente und versuchen Sie es erneut.", "تعذر قراءة المستندات بصلاحيات الوكيل الحالية. اختر مستندات يمكن قراءتها وحاول مرة أخرى."},
		"write_failed": {"The agent could not write the announcement. Try again.", "Der Agent konnte die Ankündigung nicht schreiben. Versuchen Sie es erneut.", "تعذر على الوكيل كتابة الإعلان. حاول مرة أخرى."}, "post_failed": {"The announcement could not be posted. Try again.", "Die Ankündigung konnte nicht veröffentlicht werden. Versuchen Sie es erneut.", "تعذر نشر الإعلان. حاول مرة أخرى."}, "validation_failed": {"The agent could not produce a validated announcement. Try again.", "Der Agent konnte keine geprüfte Ankündigung erstellen. Versuchen Sie es erneut.", "تعذر على الوكيل إنتاج إعلان معتمد. حاول مرة أخرى."},
		"not_public": {"{count} of the documents cannot be read by everyone in {conversation}. Share the documents with the channel or choose readable sources, then preview again.", "{count} der Dokumente können nicht von allen in {conversation} gelesen werden. Teilen Sie die Dokumente mit dem Kanal oder wählen Sie lesbare Quellen und erstellen Sie eine neue Vorschau.", "يتعذر على الجميع في {conversation} قراءة {count} من المستندات. شارك المستندات مع القناة أو اختر مصادر متاحة للجميع ثم أعد المعاينة."}, "public_preview": {"This message can be posted publicly.", "Diese Nachricht kann öffentlich veröffentlicht werden.", "يمكن نشر هذه الرسالة بشكل عام."}, "private_preview": {"This message will not be posted because not everyone can read its sources. Share the sources with the channel or choose readable documents, then preview again.", "Diese Nachricht wird nicht veröffentlicht, weil nicht alle ihre Quellen lesen können. Teilen Sie die Quellen mit dem Kanal oder wählen Sie lesbare Dokumente und erstellen Sie eine neue Vorschau.", "لن يتم نشر هذه الرسالة لأن مصادرها ليست قابلة للقراءة من الجميع. شارك المصادر مع القناة أو اختر مستندات متاحة للجميع ثم أعد المعاينة."}, "input_invalid": {"Check the highlighted field, then try again.", "Prüfen Sie das markierte Feld und versuchen Sie es erneut.", "تحقق من الحقل المحدد ثم حاول مرة أخرى."}, "conflict": {"This announcement changed. Reload it before editing.", "Diese Ankündigung wurde geändert. Laden Sie sie vor der Bearbeitung neu.", "تم تغيير هذا الإعلان. أعد تحميله قبل التعديل."},
		"title": {"Announcements", "Ankündigungen", "الإعلانات"}, "help": {"Let an agent post a document-grounded message now or on a schedule.", "Lassen Sie einen Agenten jetzt oder nach Zeitplan eine dokumentgestützte Nachricht veröffentlichen.", "اسمح للوكيل بنشر رسالة تستند إلى مستند الآن أو وفق جدول."}, "new": {"New announcement", "Neue Ankündigung", "إعلان جديد"},
		"loading": {"Loading announcements…", "Ankündigungen werden geladen…", "جارٍ تحميل الإعلانات…"}, "failed": {"Announcements could not be loaded. Try again.", "Ankündigungen konnten nicht geladen werden. Versuchen Sie es erneut.", "تعذر تحميل الإعلانات. حاول مرة أخرى."}, "empty": {"No announcements yet. Create one to let an agent share an update.", "Noch keine Ankündigungen. Erstellen Sie eine, damit ein Agent ein Update teilen kann.", "لا توجد إعلانات بعد. أنشئ إعلانًا ليشارك الوكيل تحديثًا."},
		"agent": {"Agent", "Agent", "الوكيل"}, "conversation": {"Conversation", "Unterhaltung", "المحادثة"}, "choose_agent": {"Choose an agent", "Agent auswählen", "اختر وكيلاً"}, "choose_conversation": {"Choose a conversation", "Unterhaltung auswählen", "اختر محادثة"}, "instruction": {"What should it post?", "Was soll veröffentlicht werden?", "ماذا ينبغي أن ينشر؟"}, "example": {"Example: Tell employees which company holidays are coming up, using the 2026 holiday guide.", "Beispiel: Informieren Sie Mitarbeitende anhand des Feiertagsleitfadens 2026 über kommende Betriebsfeiertage.", "مثال: أخبر الموظفين بالعطلات القادمة للشركة باستخدام دليل عطلات 2026."},
		"when": {"When", "Wann", "متى"}, "now": {"Now, once", "Jetzt, einmalig", "الآن، مرة واحدة"}, "daily": {"Every day", "Jeden Tag", "كل يوم"}, "weekly": {"Every week", "Jede Woche", "كل أسبوع"}, "monthly": {"Every month", "Jeden Monat", "كل شهر"}, "time": {"Time", "Uhrzeit", "الوقت"}, "zone": {"Time zone", "Zeitzone", "المنطقة الزمنية"},
		"preview": {"Preview", "Vorschau", "معاينة"}, "post_now": {"Post now", "Jetzt veröffentlichen", "انشر الآن"}, "save": {"Save schedule", "Zeitplan speichern", "حفظ الجدول"}, "preview_title": {"Exact message preview", "Genaue Nachrichtenvorschau", "معاينة الرسالة الدقيقة"}, "pause": {"Pause", "Pausieren", "إيقاف مؤقت"}, "resume": {"Resume", "Fortsetzen", "استئناف"}, "edit": {"Edit", "Bearbeiten", "تعديل"}, "delete": {"Delete", "Löschen", "حذف"}, "cancel": {"Cancel", "Abbrechen", "إلغاء"}, "delete_yes": {"Delete announcement", "Ankündigung löschen", "حذف الإعلان"}, "delete_confirm": {"Delete this announcement? It will not post again.", "Diese Ankündigung löschen? Sie wird nicht erneut veröffentlicht.", "هل تريد حذف هذا الإعلان؟ لن يُنشر مرة أخرى."}, "next": {"Next run", "Nächste Ausführung", "التشغيل التالي"}, "last": {"Last result", "Letztes Ergebnis", "النتيجة الأخيرة"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	return values[agentRPLocaleIndex(locale)]
}

func AgentAnnouncementText(locale LocaleContext, key string) string {
	return agentAnnouncementText(locale, key)
}

func AgentAnnouncementResultText(locale LocaleContext, code, reason string) string {
	reason = localizeAnnouncementFailure(locale, reason)
	key := "not_posted"
	switch code {
	case "POSTED":
		key = "posted"
	case "REFUSED":
		key = "refused"
	case "FAILED":
		key = "failure"
	}
	for sentence, replacement := range map[string]string{
		"The announcement changed or was paused before this post could run. Save or resume it to schedule the next post.": "schedule_changed",
		"The preview expired or its documents or authority changed. Preview again before posting.":                        "preview_changed",
		"The announcement documents could not be read with the agent's current access.":                                   "read_failed",
		"The agent could not write this announcement.":                                                                    "write_failed",
		"The announcement could not be posted.":                                                                           "post_failed",
		"The agent could not produce a validated announcement.":                                                           "validation_failed",
	} {
		if reason == sentence {
			reason = agentAnnouncementText(locale, replacement)
		}
	}
	if parts := strings.SplitN(reason, " of the documents cannot be read by everyone in ", 2); len(parts) == 2 {
		reason = strings.NewReplacer("{count}", parts[0], "{conversation}", parts[1]).Replace(agentAnnouncementText(locale, "not_public"))
	}
	return strings.ReplaceAll(agentAnnouncementText(locale, key), "{reason}", reason)
}

func announcementWeekdayChoices(locale LocaleContext) []ui.Node {
	names := [][3]string{{"Sunday", "Sonntag", "الأحد"}, {"Monday", "Montag", "الاثنين"}, {"Tuesday", "Dienstag", "الثلاثاء"}, {"Wednesday", "Mittwoch", "الأربعاء"}, {"Thursday", "Donnerstag", "الخميس"}, {"Friday", "Freitag", "الجمعة"}, {"Saturday", "Samstag", "السبت"}}
	var nodes []ui.Node
	for day, name := range names {
		nodes = append(nodes, html.Label(html.Props{}, html.Input(html.Props{Type: "checkbox", Name: "announcement-weekday", Value: strconv.Itoa(day)}), ui.Text(name[agentRPLocaleIndex(locale)])))
	}
	return nodes
}

const AgentAnnouncementsStyles = `.agent-announcement-history{min-width:0;overflow-wrap:anywhere}.agent-announcement-history ul{padding-inline-start:var(--hcm-space-4,1rem)}.agent-announcements [hidden]{display:none!important}.agent-announcements-panel{padding:var(--hcm-space-4,1rem);display:grid;gap:var(--hcm-space-4,1rem);min-width:0}.agent-announcements-header{display:flex;justify-content:space-between;align-items:flex-start;gap:var(--hcm-space-3,.75rem);flex-wrap:wrap}.agent-announcement-list{display:grid;gap:var(--hcm-space-3,.75rem)}.agent-announcement-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:var(--hcm-space-3,.75rem);padding:var(--hcm-space-4,1rem);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-resting)}.agent-announcement-row h3,.agent-announcement-row p{margin-block-start:0}.agent-announcement-row dl{display:flex;gap:var(--hcm-space-4,1rem);flex-wrap:wrap;margin:0}.agent-announcement-row dl div{display:flex;gap:.35rem}.agent-announcement-row dt{color:var(--hcm-color-text-muted)}.agent-announcement-row dd{margin:0}.agent-announcement-row-actions,.agent-announcement-editor-actions{display:flex;gap:var(--hcm-space-2,.5rem);flex-wrap:wrap;align-items:flex-start}.agent-announcement-delete{grid-column:1/-1;padding:var(--hcm-space-3,.75rem);background:var(--hcm-color-canvas);border-radius:var(--hcm-radius-control)}.agent-announcement-editor{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-4,1rem);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface)}.agent-announcement-grid,.agent-announcement-time{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--hcm-space-1);align-items:center}.agent-announcement-editor textarea,.agent-announcement-editor select,.agent-announcement-editor input{max-width:100%;min-width:0;box-sizing:border-box}.agent-announcement-row-main,.agent-announcement-field-help>*{min-width:0;overflow-wrap:anywhere}.agent-announcement-instruction{overflow-wrap:anywhere}.agent-announcement-when fieldset{min-width:0;border:0;display:flex;gap:.5rem;flex-wrap:wrap}.agent-announcement-field-help{display:flex;justify-content:space-between;gap:var(--hcm-space-2,.5rem)}.agent-announcement-when{display:block;min-width:0;gap:var(--hcm-space-3,.75rem);flex-wrap:wrap}.agent-announcement-when legend{width:100%}.agent-announcement-when>label{display:inline-flex;align-items:center;gap:var(--hcm-space-2,.5rem);margin-inline-end:var(--hcm-space-3,.75rem);margin-block-end:var(--hcm-space-3,.75rem)}.agent-announcement-time{width:100%}.agent-announcement-editor textarea,.agent-announcement-editor select,.agent-announcement-editor input:not([type=radio]):not([type=checkbox]){width:100%}.agent-announcement-editor textarea{min-height:8rem;resize:vertical}.agent-announcement-editor>label,.agent-announcement-grid>label,.agent-announcement-time>label{display:block;font-weight:600}.agent-announcement-preview{min-width:0;padding:var(--hcm-space-4,1rem);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);background:var(--hcm-color-surface);box-shadow:var(--hcm-shadow-resting);overflow-wrap:anywhere}.agent-announcement-preview p{white-space:pre-wrap}.agent-announcement-field-error{color:var(--hcm-color-danger);margin:0;overflow-wrap:anywhere}.agent-announcement-editor [aria-invalid=true]{outline:2px solid var(--hcm-color-border);outline-offset:2px}.agent-announcements .agentdoc-picker{display:grid;gap:var(--hcm-space-1);min-width:0}.agent-announcements .agentdoc-picker-combobox{display:grid;gap:var(--hcm-space-1);min-width:0}.agent-announcements .agentdoc-picker-results{list-style:none;margin:0;padding:var(--hcm-space-1);max-height:18rem;overflow:auto;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}.agent-announcements .agentdoc-picker-option{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-1);cursor:pointer;overflow-wrap:anywhere}.agent-announcements .agentdoc-picker-option[aria-selected=true]{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);background:var(--hcm-color-canvas)}.agent-announcements .agentdoc-picker-selected{display:grid;gap:var(--hcm-space-1);margin:0;padding:0;list-style:none}.agent-announcements .agentdoc-picker-row{display:flex;gap:var(--hcm-space-1);align-items:center;flex-wrap:wrap;min-width:0}.agent-announcements .agentdoc-picker-row>a{flex:1 1 10rem;overflow-wrap:anywhere}.agent-announcements .agentdoc-picker-row select{flex:1 1 10rem;min-width:0}.agent-announcements input:not([type=radio]):not([type=checkbox]),.agent-announcements select,.agent-announcements textarea{font:inherit;min-height:var(--hcm-control-height);padding:var(--hcm-space-1);border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);color:var(--hcm-color-text);background:var(--hcm-color-canvas)}@media(max-width:800px){.agent-announcements-panel,.agent-announcement-editor,.agent-announcement-row{padding:var(--hcm-space-1)}.agent-announcement-editor-actions .button,.agent-announcement-row-actions .button{white-space:normal;overflow-wrap:anywhere;max-width:100%}.agent-announcement-row{grid-template-columns:minmax(0,1fr)}.agent-announcement-grid,.agent-announcement-time{grid-template-columns:minmax(0,1fr)}.agent-announcement-row-actions .button,.agent-announcement-editor-actions .button{flex:1 1 8rem}.agent-announcement-field-help{display:grid}}`

func announcementFieldError(field string) ui.Node {
	return html.P(html.Props{ID: "announcement-" + field + "-error", Class: "agent-announcement-field-error", Hidden: true, Role: "alert", Raw: map[string]any{"data-announcement-error": field}})
}

func announcementRowPostLabel(locale LocaleContext, code string) string {
	if code == "FAILED" || code == "REFUSED" {
		return agentAnnouncementText(locale, "retry")
	}
	return agentAnnouncementText(locale, "post_now")
}

// Only the selected agent's installations become choices. Repeated catalog
// installations of one conversation never produce repeated labels.
func AgentAnnouncementConversations(agents []AgentAnnouncementAgent, persona string) []AgentAnnouncementConversation {
	var out []AgentAnnouncementConversation
	seen := map[string]bool{}
	labels := map[string]bool{}
	for _, agent := range agents {
		if agent.PersonaID != persona {
			continue
		}
		for _, conversation := range agent.Conversations {
			if conversation.ID == "" || seen[conversation.ID] {
				continue
			}
			seen[conversation.ID] = true
			if conversation.InstallationID == "" {
				conversation.InstallationID = agent.InstallationID
			}
			name := conversation.Name
			for suffix := 2; labels[conversation.Name]; suffix++ {
				conversation.Name = name + " · " + strconv.Itoa(suffix)
			}
			labels[conversation.Name] = true
			out = append(out, conversation)
		}
	}
	return out
}

func announcementRetryOccurrence(row AgentAnnouncementRow) string {
	if row.ResultCode == "FAILED" {
		return row.LastOccurrence
	}
	return ""
}
