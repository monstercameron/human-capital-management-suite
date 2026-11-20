package chatui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// The audit log (#3780): a workspace administrator's page of who did what in
// Chat, on the Chat settings page. It shows the server's body-free entries as
// plain sentences, filtered by person, kind of event, conversation and dates,
// and downloads the same list as CSV. The download is recorded in the log by
// the server before it is handed over.

// ChatAuditEntry is one row the server answers. It carries no message text.
type ChatAuditEntry struct {
	EventID    string `json:"event_id"`
	Sequence   uint64 `json:"sequence"`
	ActorID    string `json:"actor_id"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
}

// ChatAuditData is one page of entries and the cursor of the next, empty at the end.
type ChatAuditData struct {
	Entries    []ChatAuditEntry `json:"entries"`
	NextCursor string           `json:"next_cursor"`
}

// ChatAuditFilter is what the filter form holds. Dates are calendar days
// (YYYY-MM-DD) and the last day is included.
type ChatAuditFilter struct {
	Actor, Action, Conversation, FromDate, ToDate string
}

// Empty reports whether no filter is set.
func (f ChatAuditFilter) Empty() bool { return f == ChatAuditFilter{} }

// ChatAuditModel is everything the page needs to draw. A nil handler draws its
// control disabled.
type ChatAuditModel struct {
	Locale string
	Data   ChatAuditData
	// Filter is the filter the list on show was read with.
	Filter                         ChatAuditFilter
	Loaded                         bool
	Loading, LoadingMore           bool
	Failed, MoreFailed             bool
	Denied, Unavailable, SignedOut bool
	Exporting, Exported            bool
	ExportFailed                   bool
	// Apply reads the form and shows the matching events; Clear empties the
	// form; More adds the next page; Export downloads the list; Retry reads again.
	Apply, Clear, More, Export, Retry ui.Handler
}

// ChatAuditText returns one string of the page in the person's language.
func ChatAuditText(locale, key string) string {
	values, ok := chatAuditCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
}

var chatAuditCopy = map[string][3]string{
	"title":                      {"Audit log", "Prüfprotokoll", "سجل التدقيق"},
	"intro":                      {"Who did what in Chat: removed messages, unmasked originals that were viewed, channel changes and app changes. The log shows who, what and when. It never shows what a message said.", "Wer hat im Chat was getan: entfernte Nachrichten, angesehene unmaskierte Originale, Kanal- und App-Änderungen. Das Protokoll zeigt, wer, was und wann. Es zeigt nie, was in einer Nachricht stand.", "من فعل ماذا في الدردشة: الرسائل المحذوفة، والنسخ الأصلية غير المقنّعة التي عُرضت، وتغييرات القنوات والتطبيقات. يعرض السجل من فعل ماذا ومتى، ولا يعرض أبدًا ما قالته الرسالة."},
	"loading":                    {"Loading the audit log. Please wait.", "Das Prüfprotokoll wird geladen. Bitte warten.", "جارٍ تحميل سجل التدقيق. يرجى الانتظار."},
	"error":                      {"The audit log could not load. Try again.", "Das Prüfprotokoll konnte nicht geladen werden. Versuchen Sie es erneut.", "تعذر تحميل سجل التدقيق. حاول مجددًا."},
	"signed_out":                 {"You were signed out, so the audit log could not load. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnte das Prüfprotokoll nicht geladen werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذر تحميل سجل التدقيق. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"retry":                      {"Try again", "Erneut versuchen", "حاول مجددًا"},
	"denied":                     {"Only a workspace administrator can read the audit log.", "Nur Administratoren des Arbeitsbereichs können das Prüfprotokoll lesen.", "يمكن لمسؤول مساحة العمل فقط قراءة سجل التدقيق."},
	"unavailable":                {"The audit log is not set up on this server.", "Das Prüfprotokoll ist auf diesem Server nicht eingerichtet.", "سجل التدقيق غير مُعدّ على هذا الخادم."},
	"filters":                    {"Filter the log", "Protokoll filtern", "تصفية السجل"},
	"actor":                      {"Person", "Person", "الشخص"},
	"actor_hint":                 {"The sign-in name of the person who did it.", "Der Anmeldename der Person, die es getan hat.", "اسم تسجيل الدخول للشخص الذي قام بذلك."},
	"action":                     {"What happened", "Was ist passiert", "ما الذي حدث"},
	"conversation":               {"Conversation", "Unterhaltung", "المحادثة"},
	"conv_hint":                  {"The conversation's identifier, from its address.", "Die Kennung der Unterhaltung, aus ihrer Adresse.", "معرّف المحادثة، من عنوانها."},
	"from":                       {"From date", "Von Datum", "من تاريخ"},
	"to":                         {"To date", "Bis Datum", "إلى تاريخ"},
	"apply":                      {"Show events", "Ereignisse anzeigen", "عرض الأحداث"},
	"clear":                      {"Clear filters", "Filter zurücksetzen", "مسح عوامل التصفية"},
	"export":                     {"Download CSV", "CSV herunterladen", "تنزيل CSV"},
	"exporting":                  {"Preparing the download…", "Der Download wird vorbereitet…", "جارٍ تجهيز التنزيل…"},
	"exported":                   {"The download started. It has been recorded in the log.", "Der Download wurde gestartet. Er wurde im Protokoll festgehalten.", "بدأ التنزيل. وقد سُجّل في السجل."},
	"export_failed":              {"The download could not be prepared, so nothing was downloaded. Try again, or narrow the dates if the log is very large.", "Der Download konnte nicht vorbereitet werden, daher wurde nichts heruntergeladen. Versuchen Sie es erneut oder grenzen Sie den Zeitraum ein, falls das Protokoll sehr groß ist.", "تعذر تجهيز التنزيل، لذا لم يُنزَّل شيء. حاول مجددًا، أو ضيّق نطاق التواريخ إذا كان السجل كبيرًا جدًا."},
	"export_hint":                {"The download holds every event that matches the filters on show, up to 10,000.", "Der Download enthält alle Ereignisse, die den angezeigten Filtern entsprechen, bis zu 10.000.", "يتضمن التنزيل كل الأحداث المطابقة لعوامل التصفية المعروضة، حتى 10,000."},
	"caption":                    {"Audit log events, newest first", "Ereignisse im Prüfprotokoll, neueste zuerst", "أحداث سجل التدقيق، الأحدث أولًا"},
	"col_time":                   {"Time", "Zeit", "الوقت"},
	"col_actor":                  {"Person", "Person", "الشخص"},
	"col_action":                 {"What happened", "Was ist passiert", "ما الذي حدث"},
	"col_target":                 {"Item", "Objekt", "العنصر"},
	"col_reason":                 {"Reason", "Grund", "السبب"},
	"empty":                      {"No events match these filters.", "Keine Ereignisse entsprechen diesen Filtern.", "لا توجد أحداث مطابقة لعوامل التصفية هذه."},
	"empty_all":                  {"Nothing has been recorded yet.", "Es wurde noch nichts festgehalten.", "لم يُسجَّل شيء بعد."},
	"count":                      {"%d events shown.", "%d Ereignisse angezeigt.", "تم عرض %d حدثًا."},
	"count_more":                 {"%d events shown. More are available.", "%d Ereignisse angezeigt. Weitere sind verfügbar.", "تم عرض %d حدثًا. تتوفر أحداث أخرى."},
	"more":                       {"Show more", "Mehr anzeigen", "عرض المزيد"},
	"more_failed":                {"More events could not load. Try again.", "Weitere Ereignisse konnten nicht geladen werden. Versuchen Sie es erneut.", "تعذر تحميل المزيد من الأحداث. حاول مجددًا."},
	"a_all":                      {"All activity", "Alle Aktivitäten", "كل النشاط"},
	"a_posts":                    {"Messages", "Nachrichten", "الرسائل"},
	"a_members":                  {"People added or removed", "Hinzugefügte oder entfernte Personen", "الأشخاص المضافون أو المزالون"},
	"a_apps":                     {"Apps", "Apps", "التطبيقات"},
	"a_moderation":               {"Moderation", "Moderation", "الإشراف"},
	"a_conversation":             {"Channel and conversation changes", "Kanal- und Unterhaltungsänderungen", "تغييرات القنوات والمحادثات"},
	"a_audit":                    {"Log downloads", "Protokoll-Downloads", "تنزيلات السجل"},
	"w_chat.post.send":           {"Sent a message", "Hat eine Nachricht gesendet", "أرسل رسالة"},
	"w_chat.post.edit":           {"Edited a message", "Hat eine Nachricht bearbeitet", "عدّل رسالة"},
	"w_chat.post.delete":         {"Deleted a message", "Hat eine Nachricht gelöscht", "حذف رسالة"},
	"w_chat.post.forward":        {"Forwarded a message", "Hat eine Nachricht weitergeleitet", "أعاد توجيه رسالة"},
	"w_chat.persona.reply":       {"An agent replied", "Ein Agent hat geantwortet", "ردّ وكيل"},
	"w_chat.membership.add":      {"Added a person", "Hat eine Person hinzugefügt", "أضاف شخصًا"},
	"w_chat.membership.remove":   {"Removed a person", "Hat eine Person entfernt", "أزال شخصًا"},
	"w_chat.conversation.create": {"Created a conversation", "Hat eine Unterhaltung erstellt", "أنشأ محادثة"},
	"w_chat.conversation.update": {"Changed a conversation", "Hat eine Unterhaltung geändert", "غيّر محادثة"},
	"w_chat.barrier.add":         {"Added an information barrier", "Hat eine Informationsbarriere hinzugefügt", "أضاف حاجز معلومات"},
	"w_chat.barrier.remove":      {"Removed an information barrier", "Hat eine Informationsbarriere entfernt", "أزال حاجز معلومات"},
	"w_chat.share.create":        {"Created a share link", "Hat einen Freigabelink erstellt", "أنشأ رابط مشاركة"},
	"w_chat.app.install":         {"Installed an app", "Hat eine App installiert", "ثبّت تطبيقًا"},
	"w_chat.app.status":          {"Changed an app's status", "Hat den Status einer App geändert", "غيّر حالة تطبيق"},
	"w_moderation.remove":        {"Removed a message (moderation)", "Hat eine Nachricht entfernt (Moderation)", "أزال رسالة (إشراف)"},
	"w_moderation.mask":          {"Masked a message (moderation)", "Hat eine Nachricht maskiert (Moderation)", "قنّع رسالة (إشراف)"},
	"w_moderation.notify":        {"Sent a moderation notice", "Hat einen Moderationshinweis gesendet", "أرسل إشعار إشراف"},
	"w_moderation.original_view": {"Viewed the unmasked original", "Hat das unmaskierte Original angesehen", "اطّلع على النسخة الأصلية غير المقنّعة"},
	"w_audit.export":             {"Downloaded the audit log", "Hat das Prüfprotokoll heruntergeladen", "نزّل سجل التدقيق"},
}

// chatAuditActionLabels are the kinds of event the filter offers, in order.
var chatAuditActionFilters = []struct{ Value, Key string }{
	{"", "a_all"},
	{"chat.post", "a_posts"},
	{"chat.membership", "a_members"},
	{"chat.app", "a_apps"},
	{"moderation", "a_moderation"},
	{"chat.conversation", "a_conversation"},
	{"audit", "a_audit"},
}

// ChatAuditWhat names an action in plain words; an action the page has no words
// for is shown as the server named it, never hidden.
func ChatAuditWhat(locale, action string) string {
	if text := ChatAuditText(locale, "w_"+action); text != "" {
		return text
	}
	return action
}

// chatAuditTime shows an RFC 3339 instant as a date and a time in UTC.
func chatAuditTime(locale, at string) (shown, machine string) {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at, ""
	}
	t = t.UTC()
	return localize.FormatDay(locale, t, true) + ", " + localize.FormatClock(locale, t) + " UTC", t.Format(time.RFC3339)
}

func chatAuditShell(m ChatAuditModel, body ...ui.Node) ui.Node {
	nodes := append([]ui.Node{html.H2(html.Props{ID: "chataudit-title", Text: ChatAuditText(m.Locale, "title")})}, body...)
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chataudit", Dir: direction(m.Locale), Aria: map[string]string{"labelledby": "chataudit-title"}}, nodes...)
}

// ChatAuditLogSettings draws the audit log for the Chat settings page. It is the
// shell (loading, refusal, failure with Try again) until the log has been read.
func ChatAuditLogSettings(m ChatAuditModel) ui.Node {
	t := func(key string) string { return ChatAuditText(m.Locale, key) }
	retry := func() ui.Node {
		return html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("retry"), OnClick: m.Retry, Disabled: m.Loading || m.Retry.Value() == nil})
	}
	switch {
	case m.Denied:
		return chatAuditShell(m, html.P(html.Props{Role: "alert", Text: t("denied")}))
	case m.Unavailable:
		return chatAuditShell(m, html.P(html.Props{Role: "status", Text: t("unavailable")}))
	case !m.Loaded && !m.Failed:
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chataudit", Dir: direction(m.Locale), Role: "status", Aria: map[string]string{"busy": "true", "labelledby": "chataudit-title"}},
			html.H2(html.Props{ID: "chataudit-title", Text: t("title")}), html.P(html.Props{Text: t("loading")}))
	case m.Failed && !m.Loaded:
		key := "error"
		if m.SignedOut {
			key = "signed_out"
		}
		return chatAuditShell(m, html.P(html.Props{Role: "alert", Text: t(key)}), retry())
	}

	busy := m.Loading || m.LoadingMore
	options := make([]ui.Node, 0, len(chatAuditActionFilters))
	for _, o := range chatAuditActionFilters {
		// An option without a value would submit its own words, so the empty
		// choice states its empty value outright.
		options = append(options, html.Option(html.Props{Raw: map[string]any{"value": o.Value}, Selected: o.Value == m.Filter.Action, Text: t(o.Key)}))
	}
	field := func(id, name, label, hint, kind string, value string) []ui.Node {
		aria := map[string]string{}
		maxLength := 200
		if kind == "date" {
			maxLength = 0
		}
		if hint != "" {
			aria["describedby"] = id + "-hint"
		}
		nodes := []ui.Node{
			html.Label(html.Props{For: id, Text: label}),
			html.Input(html.Props{ID: id, Name: name, Type: kind, Class: "chat-input", MaxLength: maxLength, Dir: "auto", Disabled: busy, Aria: aria}),
		}
		if hint != "" {
			nodes = append(nodes, html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: hint}))
		}
		return nodes
	}
	form := []ui.Node{}
	form = append(form, field("chataudit-actor", "actor", t("actor"), t("actor_hint"), "text", m.Filter.Actor)...)
	form = append(form,
		html.Label(html.Props{For: "chataudit-action", Text: t("action")}),
		html.Select(html.Props{ID: "chataudit-action", Name: "action", Class: "chat-input", Disabled: busy}, options...))
	form = append(form, field("chataudit-conversation", "conversation", t("conversation"), t("conv_hint"), "text", m.Filter.Conversation)...)
	form = append(form, field("chataudit-from", "fromdate", t("from"), "", "date", m.Filter.FromDate)...)
	form = append(form, field("chataudit-to", "todate", t("to"), "", "date", m.Filter.ToDate)...)
	form = append(form, html.Div(html.Props{Class: "chatlangadmin-actions"},
		html.Button(html.Props{Type: "submit", Class: "button", Text: t("apply"), Disabled: busy || m.Apply.Value() == nil}),
		html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("clear"), OnClick: m.Clear, Disabled: busy || m.Clear.Value() == nil})))

	status := ""
	switch {
	case m.Loading:
		status = t("loading")
	case m.Failed:
		status = t("error")
	case len(m.Data.Entries) == 0:
		status = ""
	case m.Data.NextCursor != "":
		status = fmt.Sprintf(t("count_more"), len(m.Data.Entries))
	default:
		status = fmt.Sprintf(t("count"), len(m.Data.Entries))
	}

	var list ui.Node
	if len(m.Data.Entries) == 0 {
		empty := t("empty")
		if m.Filter.Empty() {
			empty = t("empty_all")
		}
		list = html.P(html.Props{Class: "chataudit-empty", Text: empty})
	} else {
		rows := make([]ui.Node, 0, len(m.Data.Entries))
		for _, e := range m.Data.Entries {
			shown, machine := chatAuditTime(m.Locale, e.At)
			when := ui.Node(html.Time(html.Props{Text: shown, Raw: map[string]any{"datetime": machine}}))
			if machine == "" {
				when = html.Span(html.Props{Text: shown})
			}
			rows = append(rows, html.Tr(html.Props{},
				html.Td(html.Props{Dir: "ltr"}, when),
				html.Td(html.Props{Dir: "auto", Text: e.ActorID}),
				html.Td(html.Props{Text: ChatAuditWhat(m.Locale, e.Action)}),
				html.Td(html.Props{Dir: "auto", Text: e.TargetID}),
				html.Td(html.Props{Dir: "auto", Text: e.Reason})))
		}
		head := html.Thead(html.Props{}, html.Tr(html.Props{},
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_time")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_actor")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_action")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_target")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_reason")})))
		list = html.Div(html.Props{Class: "md-table-wrap", Dir: "auto", Raw: map[string]any{"tabindex": "0"}, Role: "region", Aria: map[string]string{"label": t("caption")}},
			html.Table(html.Props{Class: "md-table chataudit-table"}, html.Caption(html.Props{Class: "sr-only", Text: t("caption")}), head, html.Tbody(html.Props{}, rows...)))
	}

	exportLine := ""
	switch {
	case m.Exporting:
		exportLine = t("exporting")
	case m.ExportFailed:
		exportLine = t("export_failed")
	case m.Exported:
		exportLine = t("exported")
	}
	exportRole := "status"
	if m.ExportFailed {
		exportRole = "alert"
	}
	nodes := []ui.Node{
		html.P(html.Props{Class: "field-hint", Text: t("intro")}),
		html.Form(html.Props{Class: "chatlangadmin-form chataudit-filters", Data: map[string]string{"chataudit": "filters"}, OnSubmit: m.Apply, Aria: map[string]string{"label": t("filters")}}, form...),
		html.P(html.Props{ID: "chataudit-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}),
	}
	if m.Failed {
		nodes = append(nodes, retry())
	}
	nodes = append(nodes, list)
	if m.Data.NextCursor != "" {
		more := []ui.Node{html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("more"), OnClick: m.More, Disabled: busy || m.More.Value() == nil})}
		if m.MoreFailed {
			more = append(more, html.Span(html.Props{Role: "alert", Text: t("more_failed")}))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatlangadmin-actions"}, more...))
	}
	nodes = append(nodes,
		html.Div(html.Props{Class: "chatlangadmin-actions"},
			html.Button(html.Props{Type: "button", Class: "button", Text: t("export"), OnClick: m.Export, Disabled: busy || m.Exporting || m.Export.Value() == nil, Aria: map[string]string{"describedby": "chataudit-export-hint"}}),
			html.Span(html.Props{ID: "chataudit-export-status", Role: exportRole, Aria: map[string]string{"live": "polite"}, Text: exportLine})),
		html.P(html.Props{ID: "chataudit-export-hint", Class: "field-hint", Text: t("export_hint")}))
	return chatAuditShell(m, nodes...)
}

// chatAuditFilterClean trims what the form read.
func chatAuditFilterClean(f ChatAuditFilter) ChatAuditFilter {
	f.Actor, f.Action, f.Conversation = strings.TrimSpace(f.Actor), strings.TrimSpace(f.Action), strings.TrimSpace(f.Conversation)
	f.FromDate, f.ToDate = strings.TrimSpace(f.FromDate), strings.TrimSpace(f.ToDate)
	return f
}
