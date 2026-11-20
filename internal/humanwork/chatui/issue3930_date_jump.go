package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3930: Jump to date.
//
// A header button opens a slim bar under the header with a date field. The
// browser finds the first message on or after that local day through the same
// paged, authorized read the timeline uses and loads the window around it; the
// bar then says what it did (the day itself, the next day that has messages, or
// the oldest/newest message the person can open) and offers a way back to the
// place they left. The bar never names a date or count the person cannot open:
// every date it states is the date of a message already on their screen.
//
// Threads: the control moves the channel's timeline only. A thread pane that is
// open stays as it is; replies are not timeline rows, so the jump lands on the
// first top-level message of the day.

// The statuses of a jump, as the browser reports them.
const (
	DateJumpLoading  = "loading"
	DateJumpExact    = "exact"
	DateJumpLater    = "later"
	DateJumpEarliest = "earliest"
	DateJumpNewest   = "newest"
	DateJumpEmpty    = "empty"
	DateJumpError    = "error"
	// DateJumpInvalid is the bar's own: no usable date in the field.
	DateJumpInvalid = "invalid"
)

// DateJumpView is the state of the last jump in one conversation.
type DateJumpView struct {
	// ConversationID is the conversation the state belongs to; any other
	// conversation draws nothing.
	ConversationID string
	Status         string
	// Date is the local day asked for (YYYY-MM-DD).
	Date string
	// Landed is when the message the jump landed on was sent.
	Landed time.Time
	// CanGoBack is true while the place the person jumped from can be restored.
	CanGoBack bool
}

var issue3930Copy = map[string][3]string{
	"label":    {"Jump to date", "Zu Datum springen", "الانتقال إلى تاريخ"},
	"field":    {"Date", "Datum", "التاريخ"},
	"go":       {"Go", "Los", "انتقال"},
	"close":    {"Close", "Schließen", "إغلاق"},
	"back":     {"Back to where you were", "Zurück zur vorherigen Stelle", "العودة إلى موضعك السابق"},
	"loading":  {"Finding that day…", "Der Tag wird gesucht …", "جارٍ البحث عن ذلك اليوم…"},
	"exact":    {"Showing messages from {date}.", "Nachrichten vom {date} werden angezeigt.", "عرض رسائل {date}."},
	"later":    {"No messages on {date}. Showing the next ones, from {landed}.", "Am {date} gibt es keine Nachrichten. Die nächsten vom {landed} werden angezeigt.", "لا توجد رسائل في {date}. عرض الرسائل التالية من {landed}."},
	"earliest": {"The oldest message you can open here is from {landed}. Showing it.", "Die älteste Nachricht, die Sie hier öffnen können, ist vom {landed}. Sie wird angezeigt.", "أقدم رسالة يمكنك فتحها هنا من {landed}. يتم عرضها."},
	"newest":   {"No messages on or after {date}. Showing the newest one, from {landed}.", "Ab dem {date} gibt es keine Nachrichten. Die neueste vom {landed} wird angezeigt.", "لا توجد رسائل في {date} أو بعده. عرض أحدث رسالة من {landed}."},
	"empty":    {"There are no messages you can open here yet.", "Hier gibt es noch keine Nachrichten, die Sie öffnen können.", "لا توجد رسائل يمكنك فتحها هنا بعد."},
	"error":    {"That day could not be found. Check your connection and try again.", "Der Tag konnte nicht gefunden werden. Prüfen Sie die Verbindung und versuchen Sie es erneut.", "تعذّر العثور على ذلك اليوم. تحقق من اتصالك وحاول مرة أخرى."},
	"invalid":  {"Choose a date to jump to.", "Wählen Sie ein Datum aus.", "اختر تاريخًا للانتقال إليه."},
}

func issue3930Text(m Model, key string) string {
	copy := issue3930Copy[key]
	return chatbug039Text("chat.issue3930."+key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// issue3930MaxDate is the newest day the field offers: today, in local time.
func issue3930MaxDate(now time.Time) string { return now.Local().Format("2006-01-02") }

// issue3930Status is the sentence that tells the person what the jump did. Days
// are written the way the timeline's separators write them.
func issue3930Status(m Model, view DateJumpView) string {
	text := issue3930Text(m, view.Status)
	if text == "" {
		return ""
	}
	date, landed := "", ""
	if asked, err := time.ParseInLocation("2006-01-02", view.Date, time.Local); err == nil {
		date = formatDay(m.Locale, asked, true)
	}
	if !view.Landed.IsZero() {
		landed = formatDay(m.Locale, view.Landed.Local(), true)
	}
	return chatcmd002Fill(text, "date", date, "landed", landed)
}

// issue3930Button is the header's Jump to date icon.
func issue3930Button(m Model, h handlers) ui.Node {
	label := issue3930Text(m, "label")
	return html.Button(html.Props{Class: "icon-button chatux001-date-jump", Type: "button", Title: label, Disabled: m.Callbacks.JumpToDate == nil,
		Data: map[string]string{"action": "date-jump-toggle"},
		Aria: map[string]string{"label": label, "expanded": boolString(h.local.dateJumpOpen), "controls": "chat-date-jump-bar"}}, icon("calendar"))
}

// issue3930Bar is the bar under the header: the date field while it is open, the
// status of the last jump and the way back. It is nil when there is nothing to
// show.
func issue3930Bar(m Model, h handlers) ui.Node {
	view := m.DateJump
	if view.ConversationID != m.SelectedID || m.SelectedID == "" {
		view = DateJumpView{}
	}
	open := h.local.dateJumpOpen
	if !open && !view.CanGoBack {
		return nil
	}
	status := view.Status
	if h.local.dateJumpInvalid && open {
		status = DateJumpInvalid
	}
	statusText := ""
	if status != "" {
		statusText = issue3930Status(m, DateJumpView{Status: status, Date: view.Date, Landed: view.Landed})
	}
	children := []ui.Node{}
	if open {
		children = append(children, html.Div(html.Props{Class: "date-jump-form"},
			html.Label(html.Props{For: "chat-date-jump", Text: issue3930Text(m, "field")}),
			html.Input(html.Props{ID: "chat-date-jump", Class: "chat-input date-jump-field", Type: "date", Name: "date",
				Data: map[string]string{"chat-value": view.Date}, Raw: map[string]any{"max": issue3930MaxDate(time.Now()), "min": "1970-01-01"},
				Aria: map[string]string{"describedby": "chat-date-jump-status"}}),
			html.Button(html.Props{Class: "button small", Type: "button", Disabled: m.Callbacks.JumpToDate == nil || view.Status == DateJumpLoading,
				Data: map[string]string{"action": "date-jump-go"}, Text: issue3930Text(m, "go")}),
			html.Button(html.Props{Class: "icon-button", Type: "button", Data: map[string]string{"action": "date-jump-close"},
				Aria: map[string]string{"label": issue3930Text(m, "close")}, Title: issue3930Text(m, "close")}, icon("close"))))
	}
	children = append(children, html.P(html.Props{ID: "chat-date-jump-status", Class: "date-jump-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: statusText}))
	if view.CanGoBack {
		children = append(children, html.Button(html.Props{Class: "button secondary small date-jump-back", Type: "button", Disabled: m.Callbacks.JumpBackFromDate == nil,
			Data: map[string]string{"action": "date-jump-back"}, Text: issue3930Text(m, "back")}))
	}
	return html.Div(html.Props{ID: "chat-date-jump-bar", Class: "date-jump-bar", Role: "group", Dir: direction(m.Locale), Aria: map[string]string{"label": issue3930Text(m, "label")}}, children...)
}

// issue3930Click handles the bar's buttons. It reports whether the press was one
// of them.
func issue3930Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, _, _ := eventAction(e)
	switch action {
	case "date-jump-toggle":
		open := !local.get().dateJumpOpen
		local.update(func(u *localUI) { u.dateJumpOpen, u.dateJumpInvalid = open, false })
		if open {
			focusDateJumpField()
		} else {
			focusDateJumpTrigger()
		}
	case "date-jump-close":
		issue3930Close(local)
	case "date-jump-go":
		issue3930Go(m, local)
	case "date-jump-back":
		if m.Callbacks.JumpBackFromDate != nil {
			m.Callbacks.JumpBackFromDate()
			// The button goes away with the jump; keep focus on a control that
			// stays (a landed message takes it from here once it is drawn).
			focusDateJumpTrigger()
		}
	default:
		return false
	}
	return true
}

func issue3930Close(local localStore) {
	local.update(func(u *localUI) { u.dateJumpOpen, u.dateJumpInvalid = false, false })
	focusDateJumpTrigger()
}

// issue3930Go sends the field's day to the browser, or says there is none.
func issue3930Go(m Model, local localStore) {
	value := domValue("chat-date-jump")
	if m.Callbacks.JumpToDate == nil {
		return
	}
	if value == "" || value > issue3930MaxDate(time.Now()) {
		local.update(func(u *localUI) { u.dateJumpInvalid = true })
		focusDateJumpField()
		return
	}
	local.update(func(u *localUI) { u.dateJumpInvalid = false })
	m.Callbacks.JumpToDate(value)
}

// issue3930Key gives the field Enter (go) and the bar Escape (close). It reports
// whether the key was used.
func issue3930Key(e ui.KeyboardEvent, m Model, local localStore) bool {
	if !local.get().dateJumpOpen {
		return false
	}
	switch zone := dateJumpFocusZone(); {
	case e.GetKey() == "Enter" && zone == "field":
		issue3930Go(m, local)
		return true
	case e.GetKey() == "Escape" && zone != "":
		issue3930Close(local)
		return true
	}
	return false
}

const issue3930Styles = `.chat-workspace .date-jump-bar{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;margin:0;padding:8px 16px;border-block-end:1px solid var(--line);background:var(--soft);min-width:0}` +
	`.chat-workspace .date-jump-form{display:flex;flex-wrap:wrap;align-items:center;gap:6px 8px;min-width:0}` +
	`.chat-workspace .date-jump-field{min-height:36px;max-width:100%;box-sizing:border-box}` +
	`.chat-workspace .date-jump-status{flex:1 1 12rem;margin:0;min-width:0;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}.chat-workspace .date-jump-status:empty{display:none}` +
	`.chat-workspace .date-jump-bar .button,.chat-workspace .date-jump-bar .icon-button{min-height:44px;min-width:44px}` +
	`@media(max-width:560px){.chat-workspace .date-jump-bar{padding-inline:12px}.chat-workspace .date-jump-form{flex:1 1 100%}.chat-workspace .date-jump-field{flex:1 1 9rem;min-height:44px}}`
