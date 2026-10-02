package chatui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AgentUXAmbientCard struct {
	AgentID                                                                                                     string
	ID, Conversation, Source, AgentName, Kind, Scope, Person, Reason, Title, State, SourceHref, Zone, Ask, Lead string
	Revision                                                                                                    uint64
	Due                                                                                                         time.Time
	Fire                                                                                                        []time.Time
	CanManage, Busy, Editing, Error                                                                             bool
}

const AgentUXAmbientCardStyles = `.agentux-ambient-card{min-width:0;max-width:100%;padding:12px;border-radius:var(--hcm-radius-control);overflow-wrap:anywhere}.agentux-ambient-card>*{min-width:0;max-width:100%}.agentux-ambient-actions{display:flex;flex-wrap:wrap;gap:8px;margin-block-start:10px}.agentux-ambient-card input,.agentux-ambient-card textarea{width:100%;min-width:0;max-width:100%;box-sizing:border-box}.agentux-ambient-card button{white-space:normal;overflow-wrap:anywhere;min-height:40px}.agentux-ambient-card label{display:block;margin-block:8px 4px}.agentux-ambient-card p{margin-block:6px}.agentux-ambient-card a{overflow-wrap:anywhere}`

// RenderAgentUXAmbientCard takes only the server's recipient-filtered cards.
// The local check is a second fence, not a replacement for server filtering.
func RenderAgentUXAmbientCard(m Model, c AgentUXAmbientCard) ui.Node {
	if c.Scope == "PRIVATE" && c.Person != m.CurrentUser {
		return nil
	}
	if c.State == "DISMISSED" || c.State == "NOT_TASK" {
		return nil
	}
	if c.State == "CANCELLED" || c.State == "SOURCE_UNAVAILABLE" {
		c.Title = ""
		c.Due = time.Time{}
		c.Fire = nil
		c.Ask = ""
		c.Lead = ""
		c.Editing = false
	}
	key := "channel_task"
	if c.Kind == "REMINDER" {
		key = "channel_reminder"
	}
	if c.Scope == "PRIVATE" {
		if c.Kind == "TASK" {
			key = "my_task"
		} else {
			key = "my_reminder"
		}
	}
	children := []ui.Node{renderAgentReplyIdentity(m, c.AgentName, storedAgentIcon(m, []string{c.AgentID}, c.AgentName)), html.H3(html.Props{Text: agentUXAmbientText(m.Locale, key)}), html.P(html.Props{Dir: "auto", Text: c.Title})}
	if c.Scope == "PRIVATE" {
		children = append(children, html.P(html.Props{Class: "muted", Text: agentUXAmbientText(m.Locale, "private")}))
	}
	reason := c.Reason
	if reason == "channel_task" {
		reason = "channel_reason"
	}
	if c.State != "CANCELLED" && c.State != "SOURCE_UNAVAILABLE" {
		children = append(children, html.P(html.Props{Class: "muted", Text: agentUXAmbientText(m.Locale, reason)}))
	}
	loc, err := time.LoadLocation(c.Zone)
	if err != nil {
		loc = time.UTC
	}
	if !c.Due.IsZero() {
		children = append(children, html.P(html.Props{Text: agentUXAmbientText(m.Locale, "due") + ": " + c.Due.In(loc).Format("2006-01-02 15:04 MST")}))
	}
	if c.Kind == "REMINDER" {
		if c.Lead != "" {
			children = append(children, html.P(html.Props{Text: agentUXAmbientText(m.Locale, c.Lead)}))
		}
		for _, at := range c.Fire {
			children = append(children, html.P(html.Props{Text: agentUXAmbientText(m.Locale, "remind_at") + ": " + at.In(loc).Format("2006-01-02 15:04 MST")}))
		}
	}
	if c.Ask != "" {
		children = append(children, html.P(html.Props{ID: c.ID + "-ask", Role: "status", Text: agentUXAmbientText(m.Locale, c.Ask)}))
	}
	if c.State == "SOURCE_CHANGED" {
		changed := "source_changed"
		if c.Kind == "TASK" {
			changed = "source_changed_task"
		}
		children = append(children, html.P(html.Props{Role: "status", Text: agentUXAmbientText(m.Locale, changed)}))
	}
	if c.Error {
		children = append(children, html.P(html.Props{ID: c.ID + "-error", Role: "alert", Text: agentUXAmbientText(m.Locale, "error")}))
	}
	if c.Busy {
		children = append(children, html.P(html.Props{Role: "status", Text: agentUXAmbientText(m.Locale, "saving")}))
	}
	if parsed, err := url.Parse(c.SourceHref); err == nil && !parsed.IsAbs() && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(c.SourceHref, "//") && !strings.ContainsAny(c.SourceHref, "\\\r\n") {
		children = append(children, html.A(html.Props{Href: c.SourceHref, Text: agentUXAmbientText(m.Locale, "source")}))
	}
	button := func(action, key string, primary bool) ui.Node {
		class := "button secondary small"
		if primary {
			class = "button primary small"
		}
		return html.Button(html.Props{Type: "button", Class: class, Text: agentUXAmbientText(m.Locale, key), Disabled: c.Busy || !c.CanManage, Raw: map[string]any{"data-ambient-action": action, "data-ambient-id": c.ID, "data-ambient-revision": strconv.FormatUint(c.Revision, 10), "data-ambient-conversation": c.Conversation}})
	}
	if c.Editing || c.Ask != "" || c.State == "NEEDS_TIME" || c.State == "SOURCE_CHANGED" {
		fields := []ui.Node{}
		aria := map[string]string{}
		if c.Error {
			aria["describedby"] = c.ID + "-error"
		}
		if c.Ask != "" {
			aria["describedby"] = strings.TrimSpace(aria["describedby"] + " " + c.ID + "-ask")
		}
		if c.Kind == "TASK" {
			fields = append(fields, html.Label(html.Props{For: c.ID + "-title", Text: agentUXAmbientText(m.Locale, "task_text")}), html.Textarea(html.Props{Key: c.ID + "-title", ID: c.ID + "-title", Name: "title", Class: "chat-input", Rows: 2, MaxLength: 500, Dir: "auto", Aria: aria}, ui.Text(c.Title)))
		}
		fields = append(fields, html.Label(html.Props{For: c.ID + "-date", Text: agentUXAmbientText(m.Locale, "date")}), html.Input(html.Props{Key: c.ID + "-date", ID: c.ID + "-date", Name: "date", Type: "date", Class: "chat-input", Aria: aria}), html.Label(html.Props{For: c.ID + "-clock", Text: agentUXAmbientText(m.Locale, "time")}), html.Input(html.Props{Key: c.ID + "-clock", ID: c.ID + "-clock", Name: "clock", Type: "time", Class: "chat-input", Aria: aria}))
		cancelAction, cancelCopy := "CLOSE_EDIT", "cancel_edit"
		if c.Ask != "" || c.State == "NEEDS_TIME" || c.State == "SOURCE_CHANGED" {
			cancelAction, cancelCopy = "DISMISS", "dismiss"
		}
		fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button primary small", Text: agentUXAmbientText(m.Locale, "save_changes"), Disabled: c.Busy || !c.CanManage}), button(cancelAction, cancelCopy, false))
		children = append(children, html.Form(html.Props{Key: c.ID + "-form", Raw: map[string]any{"data-ambient-form": c.ID, "data-ambient-conversation": c.Conversation, "data-ambient-revision": strconv.FormatUint(c.Revision, 10), "data-ambient-kind": c.Kind}}, fields...))
	} else {
		actions := []ui.Node{}
		if c.State == "CANCELLED" || c.State == "SOURCE_UNAVAILABLE" {
			notice := c.Reason
			if notice != "source_deleted" && notice != "source_no_longer_action" && notice != "source_changed_unavailable" {
				notice = "cancelled"
			}
			children = append(children, html.P(html.Props{Role: "status", Text: agentUXAmbientText(m.Locale, notice)}))
			actions = append(actions, button("DISMISS", "dismiss", true))
		} else if c.State == "ADDED" {
			children = append(children, html.P(html.Props{Role: "status", Text: agentUXAmbientText(m.Locale, "added")}))
		} else if c.State == "SET" {
			actions = append(actions, button("EDIT_TIME", "change_time", false), button("SNOOZE", "snooze", false), button("CANCEL", "cancel_reminder", false))
		} else if c.Kind == "TASK" {
			actions = append(actions, button("ADD", "add", true), button("EDIT_TASK", "edit", false))
			if c.Scope == "PRIVATE" {
				actions = append(actions, button("NOT_TASK", "not_task", false))
			}
			actions = append(actions, button("DISMISS", "dismiss", false))
		} else {
			actions = append(actions, button("SET", "set", true), button("EDIT_TIME", "change_time", false), button("DISMISS", "dismiss", false))
		}
		children = append(children, html.Div(html.Props{Class: "agentux-ambient-actions"}, actions...))
	}
	return html.Article(html.Props{Class: "agentux-ambient-card agent-reply-answer", Dir: direction(m.Locale), Raw: map[string]any{"data-ambient-card": c.ID, "data-ambient-scope": c.Scope, "data-source-post": c.Source, "style": "min-width:0;max-width:100%;overflow-wrap:anywhere;padding:12px;border-radius:var(--hcm-radius-control)"}}, children...)
}

func RenderAgentUXAmbientList(m Model, cards []AgentUXAmbientCard, state string) ui.Node {
	var nodes []ui.Node
	if state == "loading" {
		nodes = append(nodes, html.P(html.Props{Role: "status", Text: agentUXAmbientText(m.Locale, "loading")}))
	}
	if state == "error" {
		nodes = append(nodes, html.P(html.Props{Role: "alert", Text: agentUXAmbientText(m.Locale, "read_error")}), html.Button(html.Props{Type: "button", Class: "button secondary small", Text: agentUXAmbientText(m.Locale, "retry"), Raw: map[string]any{"data-ambient-action": "REFRESH", "data-ambient-conversation": m.SelectedID}}))
	}
	if len(cards) == 0 {
		if len(nodes) == 0 {
			nodes = append(nodes, html.P(html.Props{Text: agentUXAmbientText(m.Locale, "empty")}))
		}
		return html.Div(html.Props{Class: "agentux-ambient-list"}, nodes...)
	}
	for _, card := range cards {
		nodes = append(nodes, RenderAgentUXAmbientCard(m, card))
	}
	return html.Div(html.Props{Class: "agentux-ambient-list"}, nodes...)
}

func agentUXAmbientText(locale, key string) string {
	copy := map[string][3]string{
		"read_error":                 {"Offers could not be refreshed. Your existing messages and offers remain available; try again", "Vorschläge konnten nicht aktualisiert werden. Bisherige Nachrichten und Vorschläge bleiben verfügbar; versuchen Sie es erneut", "تعذّر تحديث الاقتراحات. تبقى رسائلك واقتراحاتك الحالية متاحة؛ حاول مجدداً"},
		"retry":                      {"Try again", "Erneut versuchen", "حاول مجدداً"},
		"cancelled":                  {"Cancelled. Dismiss this notice when you are ready", "Abgebrochen. Sie können diesen Hinweis verwerfen", "أُلغي. تجاهل هذا الإشعار عندما تكون مستعداً"},
		"source_deleted":             {"The original message was deleted, so this item was cancelled", "Die ursprüngliche Nachricht wurde gelöscht, daher wurde dieser Eintrag abgebrochen", "حُذفت الرسالة الأصلية، لذلك أُلغي هذا العنصر"},
		"source_no_longer_action":    {"The original message no longer contains this task or reminder, so it was cancelled", "Die ursprüngliche Nachricht enthält diese Aufgabe oder Erinnerung nicht mehr, daher wurde sie abgebrochen", "لم تعد الرسالة الأصلية تتضمن هذه المهمة أو التذكير، لذلك أُلغيت"},
		"source_changed_unavailable": {"The original message changed. Open it and ask the agent again; the previous item cannot be added or scheduled", "Die ursprüngliche Nachricht wurde geändert. Öffnen Sie sie und fragen Sie den Agenten erneut; der bisherige Eintrag kann nicht hinzugefügt oder geplant werden", "تغيّرت الرسالة الأصلية. افتحها واطلب من الوكيل مجدداً؛ لا يمكن إضافة العنصر السابق أو جدولته"},
		"channel_task":               {"Add to this channel's to-do list", "Zur Aufgabenliste dieses Kanals hinzufügen", "أضف إلى قائمة مهام هذه القناة"},
		"channel_reason":             {"This task is for the group or has no named owner", "Diese Aufgabe ist für die Gruppe oder hat keine benannte Zuständigkeit", "هذه المهمة للمجموعة أو ليس لها مسؤول محدد"},
		"source_changed_task":        {"The original message changed. Review the task and confirm before adding it again", "Die ursprüngliche Nachricht wurde geändert. Prüfen und bestätigen Sie die Aufgabe vor dem erneuten Hinzufügen", "تغيّرت الرسالة الأصلية. راجع المهمة وأكّدها قبل إضافتها مجدداً"},
		"my_task":                    {"Add to my tasks", "Zu meinen Aufgaben hinzufügen", "أضف إلى مهامي"},
		"my_reminder":                {"Remind me", "Mich erinnern", "ذكّرني"}, "channel_reminder": {"Remind this channel", "Diesen Kanal erinnern", "ذكّر هذه القناة"},
		"private": {"Only visible to you", "Nur für Sie sichtbar", "مرئي لك فقط"}, "add": {"Add", "Hinzufügen", "أضف"}, "edit": {"Edit", "Bearbeiten", "عدّل"}, "not_task": {"Not a task", "Keine Aufgabe", "ليست مهمة"}, "dismiss": {"Dismiss", "Verwerfen", "تجاهل"},
		"set": {"Set reminder", "Erinnerung festlegen", "اضبط التذكير"}, "change_time": {"Change time", "Zeit ändern", "غيّر الوقت"}, "snooze": {"Snooze", "Verschieben", "أجّل"}, "cancel_reminder": {"Cancel reminder", "Erinnerung abbrechen", "ألغِ التذكير"},
		"self_commitment": {"You said you would do this", "Sie haben zugesagt, dies zu tun", "قلت إنك ستفعل هذا"}, "addressed_member": {"This request was addressed to you", "Diese Bitte war an Sie gerichtet", "هذا الطلب موجّه إليك"},
		"explicit_private": {"You asked for a private reminder", "Sie haben um eine private Erinnerung gebeten", "طلبت تذكيراً خاصاً"}, "explicit_channel": {"You asked to remind the channel", "Sie haben um eine Erinnerung für den Kanal gebeten", "طلبت تذكير القناة"}, "uncertain": {"Offered privately because the owner is unclear", "Privat angeboten, weil die Zuständigkeit unklar ist", "اقتراح خاص لأن المسؤول غير واضح"},
		"due": {"Due", "Fällig", "الموعد النهائي"}, "remind_at": {"Remind at", "Erinnern um", "التذكير عند"}, "source": {"Open the original message", "Ursprüngliche Nachricht öffnen", "افتح الرسالة الأصلية"},
		"deadline_two_hours_and_morning": {"Default: that morning and two hours before the deadline; you can choose another time", "Standard: am Morgen und zwei Stunden vor der Frist; Sie können eine andere Zeit wählen", "الافتراضي: صباح ذلك اليوم وقبل الموعد بساعتين؛ يمكنك اختيار وقت آخر"}, "meeting_fifteen_minutes": {"Default: fifteen minutes before the meeting", "Standard: fünfzehn Minuten vor dem Treffen", "الافتراضي: قبل الاجتماع بخمس عشرة دقيقة"}, "at_requested_time": {"At the time you requested", "Zur von Ihnen gewünschten Zeit", "في الوقت الذي طلبته"},
		"choose_zone": {"Choose your time zone before setting the reminder", "Wählen Sie vor dem Festlegen Ihre Zeitzone", "اختر منطقتك الزمنية قبل ضبط التذكير"}, "choose_date": {"Confirm the date before continuing", "Bestätigen Sie zuerst das Datum", "أكّد التاريخ قبل المتابعة"}, "choose_time": {"Choose an exact time before continuing", "Wählen Sie zuerst eine genaue Uhrzeit", "اختر وقتاً محدداً قبل المتابعة"}, "ambiguous_time": {"This local time is repeated or does not exist; choose another time", "Diese Ortszeit ist doppelt oder existiert nicht; wählen Sie eine andere", "هذا الوقت المحلي مكرر أو غير موجود؛ اختر وقتاً آخر"}, "past_time": {"That time has passed; choose a future time", "Diese Zeit ist vorbei; wählen Sie eine zukünftige", "مضى ذلك الوقت؛ اختر وقتاً مستقبلياً"}, "choose_lead_time": {"Choose a reminder time before the deadline", "Wählen Sie eine Erinnerungszeit vor der Frist", "اختر وقت تذكير قبل الموعد النهائي"},
		"source_changed": {"The original message changed. The reminder was cancelled; review and confirm the new details", "Die ursprüngliche Nachricht wurde geändert. Die Erinnerung wurde abgebrochen; prüfen und bestätigen Sie die neuen Angaben", "تغيّرت الرسالة الأصلية. أُلغي التذكير؛ راجع التفاصيل الجديدة وأكّدها"},
		"error":          {"The change was not saved. Review the details and try again", "Die Änderung wurde nicht gespeichert. Prüfen Sie die Angaben und versuchen Sie es erneut", "لم يُحفظ التغيير. راجع التفاصيل وحاول مجدداً"}, "saving": {"Saving; your messages remain available", "Wird gespeichert; Ihre Nachrichten bleiben verfügbar", "جارٍ الحفظ؛ تبقى رسائلك متاحة"}, "loading": {"Loading offers; you can keep chatting", "Vorschläge werden geladen; Sie können weiter schreiben", "جارٍ تحميل الاقتراحات؛ يمكنك مواصلة المحادثة"}, "empty": {"No tasks or reminders yet. Make a commitment or ask for a reminder in this conversation", "Noch keine Aufgaben oder Erinnerungen. Sagen Sie etwas zu oder bitten Sie in dieser Unterhaltung um eine Erinnerung", "لا توجد مهام أو تذكيرات بعد. التزم بمهمة أو اطلب تذكيراً في هذه المحادثة"}, "added": {"Added to the task list", "Zur Aufgabenliste hinzugefügt", "أُضيفت إلى قائمة المهام"},
		"task_text": {"Task", "Aufgabe", "المهمة"}, "date": {"Date", "Datum", "التاريخ"}, "time": {"Time in your time zone", "Zeit in Ihrer Zeitzone", "الوقت في منطقتك الزمنية"}, "save_changes": {"Save changes", "Änderungen speichern", "احفظ التغييرات"}, "cancel_edit": {"Cancel editing", "Bearbeitung abbrechen", "ألغِ التعديل"},
	}
	index := 0
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		index = 1
	} else if strings.HasPrefix(strings.ToLower(locale), "ar") {
		index = 2
	}
	if row, ok := copy[key]; ok {
		return chatbug039Text(key, row[index], row[0])
	}
	return ""
}
