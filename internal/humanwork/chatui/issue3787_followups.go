package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3787: Follow-ups.
//
// A Follow-ups row sits above the conversation list, next to Followed threads.
// It opens a panel with two lists, both worked out from messages the person can
// already read: what is waiting on them (a message that mentions them and that
// they have not answered in its thread) and what they are waiting on (their own
// message that mentions someone or ends in a question, which the people asked
// have not answered in its thread). Each row shows its age. A reaction, or a
// later message in the channel, is shown as a "Possible reply" hint the person
// reviews; it never removes a row. Only the person's own choice does: snooze for
// a day, mark done, stop tracking, or keep open. Nothing is sent to anyone.

// FollowUpRow is one follow-up as the panel draws it.
type FollowUpRow struct {
	ConversationID, PostID string
	// Direction is "owed" (waiting on the person) or "waiting" (the person is
	// waiting on others).
	Direction string
	// Channel is the conversation's name as the rail shows it; InChannel says to
	// write it as "#name".
	Channel   string
	InChannel bool
	// Who is the asker (owed) or the people asked (waiting), as names.
	Who  string
	Body string
	// AgeHours is how long the message has waited.
	AgeHours int
	// Evidence is "", "reacted", "posted_later" or "replied".
	Evidence     string
	Kept         bool
	Sequence     uint64
	ThreadRootID string
}

const (
	issue3787Toggle   = "followups-toggle"
	issue3787Open     = "followups-open"
	issue3787Snooze   = "followups-snooze"
	issue3787Done     = "followups-done"
	issue3787Dismiss  = "followups-dismiss"
	issue3787Keep     = "followups-keep"
	issue3787Retry    = "followups-retry"
	issue3787ToggleID = "issue3787-toggle"
	issue3787HeadID   = "issue3787-heading"

	keyIssue3787Label        = "chat.issue3787.label"
	keyIssue3787Hint         = "chat.issue3787.hint"
	keyIssue3787Count        = "chat.issue3787.count"
	keyIssue3787Heading      = "chat.issue3787.heading"
	keyIssue3787Intro        = "chat.issue3787.intro"
	keyIssue3787Loading      = "chat.issue3787.loading"
	keyIssue3787Empty        = "chat.issue3787.empty"
	keyIssue3787Error        = "chat.issue3787.error"
	keyIssue3787Retry        = "chat.issue3787.retry"
	keyIssue3787Owed         = "chat.issue3787.owed"
	keyIssue3787Waiting      = "chat.issue3787.waiting"
	keyIssue3787OwedEmpty    = "chat.issue3787.owed_empty"
	keyIssue3787WaitingEmpty = "chat.issue3787.waiting_empty"
	keyIssue3787AskedYou     = "chat.issue3787.asked_you"
	keyIssue3787YouAsked     = "chat.issue3787.you_asked"
	keyIssue3787YouAskedQ    = "chat.issue3787.you_asked_question"
	keyIssue3787Someone      = "chat.issue3787.someone"
	keyIssue3787Somewhere    = "chat.issue3787.somewhere"
	keyIssue3787AgeHours     = "chat.issue3787.age_hours"
	keyIssue3787AgeDays      = "chat.issue3787.age_days"
	keyIssue3787Possible     = "chat.issue3787.possible"
	keyIssue3787OwedReacted  = "chat.issue3787.owed_reacted"
	keyIssue3787OwedPosted   = "chat.issue3787.owed_posted"
	keyIssue3787WaitReacted  = "chat.issue3787.wait_reacted"
	keyIssue3787WaitPosted   = "chat.issue3787.wait_posted"
	keyIssue3787WaitReplied  = "chat.issue3787.wait_replied"
	keyIssue3787Kept         = "chat.issue3787.kept"
	keyIssue3787OpenBtn      = "chat.issue3787.open"
	keyIssue3787SnoozeBtn    = "chat.issue3787.snooze"
	keyIssue3787DoneBtn      = "chat.issue3787.done"
	keyIssue3787DismissBtn   = "chat.issue3787.dismiss"
	keyIssue3787KeepBtn      = "chat.issue3787.keep"
	keyIssue3787Failed       = "chat.issue3787.failed"
	keyIssue3787Shown        = "chat.issue3787.shown"
)

var issue3787Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3787Label:        "Follow-ups",
		keyIssue3787Hint:         "See what is waiting on you and what you are waiting on",
		keyIssue3787Count:        "{n} open",
		keyIssue3787Heading:      "Follow-ups",
		keyIssue3787Intro:        "Nothing is sent to anyone from here. A reply or reaction is only a hint; you decide when something is done.",
		keyIssue3787Loading:      "Loading your follow-ups",
		keyIssue3787Empty:        "Nothing needs a follow-up right now. Messages that mention you, and questions you asked, show up here once they have waited an hour.",
		keyIssue3787Error:        "Your follow-ups could not be loaded.",
		keyIssue3787Retry:        "Try again",
		keyIssue3787Owed:         "Waiting on you",
		keyIssue3787Waiting:      "You are waiting on",
		keyIssue3787OwedEmpty:    "Nobody is waiting on you.",
		keyIssue3787WaitingEmpty: "Nothing you asked is still open.",
		keyIssue3787AskedYou:     "{who} asked you",
		keyIssue3787YouAsked:     "You asked {who}",
		keyIssue3787YouAskedQ:    "You asked a question",
		keyIssue3787Someone:      "Someone",
		keyIssue3787Somewhere:    "a conversation",
		keyIssue3787AgeHours:     "Waiting {n} h",
		keyIssue3787AgeDays:      "Waiting {n} d",
		keyIssue3787Possible:     "Possible reply",
		keyIssue3787OwedReacted:  "You reacted, but did not reply.",
		keyIssue3787OwedPosted:   "You posted next in this conversation.",
		keyIssue3787WaitReacted:  "They reacted, but did not reply.",
		keyIssue3787WaitPosted:   "Someone else posted next in this conversation.",
		keyIssue3787WaitReplied:  "Someone replied in the thread. Check whether it answers your question.",
		keyIssue3787Kept:         "Kept open",
		keyIssue3787OpenBtn:      "Open",
		keyIssue3787SnoozeBtn:    "Snooze a day",
		keyIssue3787DoneBtn:      "Mark done",
		keyIssue3787DismissBtn:   "Stop tracking",
		keyIssue3787KeepBtn:      "Keep open",
		keyIssue3787Failed:       "That change could not be saved. Try again.",
		keyIssue3787Shown:        "Follow-ups: {a} waiting on you, {b} waiting on others",
	},
	"de-DE": {
		keyIssue3787Label:        "Nachfassen",
		keyIssue3787Hint:         "Sehen, worauf Sie antworten müssen und worauf Sie warten",
		keyIssue3787Count:        "{n} offen",
		keyIssue3787Heading:      "Nachfassen",
		keyIssue3787Intro:        "Von hier wird nichts an andere gesendet. Eine Antwort oder Reaktion ist nur ein Hinweis; Sie entscheiden, wann etwas erledigt ist.",
		keyIssue3787Loading:      "Ihre Nachfass-Liste wird geladen",
		keyIssue3787Empty:        "Im Moment gibt es nichts nachzufassen. Nachrichten, die Sie erwähnen, und Fragen von Ihnen erscheinen hier, sobald sie eine Stunde offen sind.",
		keyIssue3787Error:        "Ihre Nachfass-Liste konnte nicht geladen werden.",
		keyIssue3787Retry:        "Erneut versuchen",
		keyIssue3787Owed:         "Wartet auf Sie",
		keyIssue3787Waiting:      "Sie warten auf",
		keyIssue3787OwedEmpty:    "Niemand wartet auf Sie.",
		keyIssue3787WaitingEmpty: "Keine Ihrer Fragen ist noch offen.",
		keyIssue3787AskedYou:     "{who} hat Sie gefragt",
		keyIssue3787YouAsked:     "Sie haben {who} gefragt",
		keyIssue3787YouAskedQ:    "Sie haben eine Frage gestellt",
		keyIssue3787Someone:      "Jemand",
		keyIssue3787Somewhere:    "einer Unterhaltung",
		keyIssue3787AgeHours:     "Wartet seit {n} Std.",
		keyIssue3787AgeDays:      "Wartet seit {n} T.",
		keyIssue3787Possible:     "Mögliche Antwort",
		keyIssue3787OwedReacted:  "Sie haben reagiert, aber nicht geantwortet.",
		keyIssue3787OwedPosted:   "Sie haben als Nächstes in dieser Unterhaltung geschrieben.",
		keyIssue3787WaitReacted:  "Die Person hat reagiert, aber nicht geantwortet.",
		keyIssue3787WaitPosted:   "Jemand anderes hat als Nächstes in dieser Unterhaltung geschrieben.",
		keyIssue3787WaitReplied:  "Jemand hat im Thread geantwortet. Prüfen Sie, ob damit Ihre Frage beantwortet ist.",
		keyIssue3787Kept:         "Offen gehalten",
		keyIssue3787OpenBtn:      "Öffnen",
		keyIssue3787SnoozeBtn:    "Einen Tag zurückstellen",
		keyIssue3787DoneBtn:      "Als erledigt markieren",
		keyIssue3787DismissBtn:   "Nicht mehr verfolgen",
		keyIssue3787KeepBtn:      "Offen lassen",
		keyIssue3787Failed:       "Die Änderung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		keyIssue3787Shown:        "Nachfassen: {a} warten auf Sie, {b} warten auf andere",
	},
	"ar": {
		keyIssue3787Label:        "المتابعات",
		keyIssue3787Hint:         "اطّلع على ما ينتظر ردك وعلى ما تنتظره من الآخرين",
		keyIssue3787Count:        "{n} مفتوحة",
		keyIssue3787Heading:      "المتابعات",
		keyIssue3787Intro:        "لا يُرسَل أي شيء إلى أحد من هنا. الرد أو التفاعل مجرد إشارة؛ أنت من يقرر متى ينتهي الأمر.",
		keyIssue3787Loading:      "جارٍ تحميل متابعاتك",
		keyIssue3787Empty:        "لا شيء يحتاج إلى متابعة الآن. تظهر هنا الرسائل التي تذكرك والأسئلة التي طرحتها بعد أن تبقى ساعة دون رد.",
		keyIssue3787Error:        "تعذر تحميل متابعاتك.",
		keyIssue3787Retry:        "حاول مرة أخرى",
		keyIssue3787Owed:         "بانتظار ردك",
		keyIssue3787Waiting:      "تنتظر ردهم",
		keyIssue3787OwedEmpty:    "لا أحد ينتظر ردك.",
		keyIssue3787WaitingEmpty: "لا يوجد سؤال طرحته ما زال مفتوحاً.",
		keyIssue3787AskedYou:     "سألك {who}",
		keyIssue3787YouAsked:     "سألتَ {who}",
		keyIssue3787YouAskedQ:    "طرحتَ سؤالاً",
		keyIssue3787Someone:      "شخص ما",
		keyIssue3787Somewhere:    "محادثة",
		keyIssue3787AgeHours:     "بانتظار الرد منذ {n} س",
		keyIssue3787AgeDays:      "بانتظار الرد منذ {n} ي",
		keyIssue3787Possible:     "رد محتمل",
		keyIssue3787OwedReacted:  "تفاعلتَ لكنك لم تردّ.",
		keyIssue3787OwedPosted:   "كتبتَ الرسالة التالية في هذه المحادثة.",
		keyIssue3787WaitReacted:  "تفاعل الشخص لكنه لم يردّ.",
		keyIssue3787WaitPosted:   "كتب شخص آخر الرسالة التالية في هذه المحادثة.",
		keyIssue3787WaitReplied:  "ردّ أحدهم في السلسلة. تحقق مما إذا كان الرد يجيب عن سؤالك.",
		keyIssue3787Kept:         "أُبقيت مفتوحة",
		keyIssue3787OpenBtn:      "فتح",
		keyIssue3787SnoozeBtn:    "تأجيل ليوم",
		keyIssue3787DoneBtn:      "وضع علامة منجز",
		keyIssue3787DismissBtn:   "إيقاف التتبع",
		keyIssue3787KeepBtn:      "إبقاؤها مفتوحة",
		keyIssue3787Failed:       "تعذر حفظ هذا التغيير. حاول مرة أخرى.",
		keyIssue3787Shown:        "المتابعات: {a} بانتظار ردك، و{b} بانتظار ردود الآخرين",
	},
}

func issue3787Counts(rows []FollowUpRow) (owed, waiting int) {
	for _, row := range rows {
		if row.Direction == "owed" {
			owed++
		} else {
			waiting++
		}
	}
	return owed, waiting
}

func issue3787Age(m Model, hours int) string {
	if hours >= 24 {
		return laneTextf(m, issue3787Copy, keyIssue3787AgeDays, map[string]string{"n": m.n(hours / 24)})
	}
	if hours < 1 {
		hours = 1
	}
	return laneTextf(m, issue3787Copy, keyIssue3787AgeHours, map[string]string{"n": m.n(hours)})
}

func issue3787Channel(m Model, row FollowUpRow) string {
	name := strings.TrimSpace(row.Channel)
	switch {
	case name == "":
		return laneText(m, issue3787Copy, keyIssue3787Somewhere)
	case row.InChannel:
		return "#" + name
	}
	return name
}

func issue3787Line(m Model, row FollowUpRow) string {
	who := strings.TrimSpace(row.Who)
	if row.Direction == "owed" {
		if who == "" {
			who = laneText(m, issue3787Copy, keyIssue3787Someone)
		}
		return laneTextf(m, issue3787Copy, keyIssue3787AskedYou, map[string]string{"who": who})
	}
	if who == "" {
		return laneText(m, issue3787Copy, keyIssue3787YouAskedQ)
	}
	return laneTextf(m, issue3787Copy, keyIssue3787YouAsked, map[string]string{"who": who})
}

// issue3787Evidence is the sentence under "Possible reply"; empty when there is
// nothing to review or the person already chose to keep the row open.
func issue3787Evidence(m Model, row FollowUpRow) string {
	if row.Kept {
		return ""
	}
	key := ""
	switch {
	case row.Evidence == "replied" && row.Direction == "waiting":
		key = keyIssue3787WaitReplied
	case row.Evidence == "reacted" && row.Direction == "owed":
		key = keyIssue3787OwedReacted
	case row.Evidence == "posted_later" && row.Direction == "owed":
		key = keyIssue3787OwedPosted
	case row.Evidence == "reacted":
		key = keyIssue3787WaitReacted
	case row.Evidence == "posted_later":
		key = keyIssue3787WaitPosted
	default:
		return ""
	}
	return laneText(m, issue3787Copy, key)
}

// issue3787Control is the Follow-ups row above the list: a disclosure button
// that names the panel it controls and carries the count once loaded.
func issue3787Control(m Model, h handlers) ui.Node {
	on := h.local.followUpsOpen
	label := laneText(m, issue3787Copy, keyIssue3787Label)
	name := label
	children := []ui.Node{icon("at-sign"), html.Span(html.Props{Class: "chat-row-name", Text: label})}
	if m.FollowUpsLoaded && !m.FollowUpsError {
		count := laneTextf(m, issue3787Copy, keyIssue3787Count, map[string]string{"n": m.n(len(m.FollowUps))})
		name += ", " + count
		if len(m.FollowUps) > 0 {
			children = append(children, html.Span(html.Props{Class: "chat-count issue3787-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(len(m.FollowUps))}))
		}
	}
	class := "chat-row issue3787-row"
	if on {
		class += " on"
	}
	return html.Div(html.Props{Class: "issue3787-control"},
		html.Button(html.Props{ID: issue3787ToggleID, Class: class, Type: "button", Title: laneText(m, issue3787Copy, keyIssue3787Hint),
			Data: map[string]string{"action": issue3787Toggle},
			Aria: map[string]string{"expanded": boolString(on), "controls": "issue3787-panel", "label": name}}, children...))
}

// issue3787Panel is the open list: a labelled region with the heading, a polite
// status line, and the loading, empty, unavailable or populated body.
func issue3787Panel(m Model) ui.Node {
	heading := html.H2(html.Props{ID: issue3787HeadID, Class: "issue3787-heading", Text: laneText(m, issue3787Copy, keyIssue3787Heading), Raw: map[string]any{"tabindex": "-1"}})
	status := ""
	var body ui.Node
	switch {
	case m.FollowUpsLoading && !m.FollowUpsLoaded:
		body = html.P(html.Props{Class: "rail-empty issue3787-note", Role: "status", Text: laneText(m, issue3787Copy, keyIssue3787Loading)})
	case m.FollowUpsError && !m.FollowUpsLoaded:
		body = html.Div(html.Props{Class: "issue3787-note", Role: "alert"},
			html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3787Copy, keyIssue3787Error)}),
			html.Button(html.Props{Class: "button secondary small issue3787-retry", Type: "button", Data: map[string]string{"action": issue3787Retry}, Text: laneText(m, issue3787Copy, keyIssue3787Retry)}))
	case len(m.FollowUps) == 0:
		body = html.P(html.Props{Class: "rail-empty issue3787-note", Role: "status", Text: laneText(m, issue3787Copy, keyIssue3787Empty)})
	default:
		owed, waiting := issue3787Counts(m.FollowUps)
		status = laneTextf(m, issue3787Copy, keyIssue3787Shown, map[string]string{"a": m.n(owed), "b": m.n(waiting)})
		body = html.Div(html.Props{Class: "issue3787-sections"},
			issue3787Section(m, "owed", keyIssue3787Owed, keyIssue3787OwedEmpty),
			issue3787Section(m, "waiting", keyIssue3787Waiting, keyIssue3787WaitingEmpty))
	}
	children := []ui.Node{heading,
		html.P(html.Props{Class: "issue3787-intro", Text: laneText(m, issue3787Copy, keyIssue3787Intro)}),
		html.Span(html.Props{Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}), body}
	if m.FollowUpsNote != "" {
		children = append(children, html.P(html.Props{Class: "issue3787-failed", Role: "alert", Text: laneText(m, issue3787Copy, keyIssue3787Failed)}))
	}
	return html.Section(html.Props{ID: "issue3787-panel", Class: "issue3787-panel", Aria: map[string]string{"labelledby": issue3787HeadID}}, children...)
}

func issue3787Section(m Model, direction, titleKey, emptyKey string) ui.Node {
	items := []ui.Node{}
	for _, row := range m.FollowUps {
		if row.Direction == direction {
			items = append(items, html.WithKey(issue3787Item(m, row), "followup:"+row.Direction+"\x00"+row.ConversationID+"\x00"+row.PostID))
		}
	}
	var list ui.Node
	if len(items) == 0 {
		list = html.P(html.Props{Class: "rail-empty issue3787-section-empty", Text: laneText(m, issue3787Copy, emptyKey)})
	} else {
		list = html.Ul(html.Props{Class: "issue3787-list"}, items...)
	}
	return html.Section(html.Props{Class: "issue3787-section", Aria: map[string]string{"label": laneText(m, issue3787Copy, titleKey)}},
		html.H3(html.Props{Class: "issue3787-section-title", Text: laneText(m, issue3787Copy, titleKey)}), list)
}

func issue3787Item(m Model, row FollowUpRow) ui.Node {
	preview := strings.Join(strings.Fields(row.Body), " ")
	extra := row.Direction + ":" + row.ConversationID
	data := func(action string) map[string]string {
		return map[string]string{"action": action, "id": row.PostID, "extra": extra}
	}
	button := func(action, key string) ui.Node {
		text := laneText(m, issue3787Copy, key)
		return html.Button(html.Props{Class: "button secondary small issue3787-act", Type: "button", Data: data(action), Aria: map[string]string{"label": text + ": " + preview}, Text: text})
	}
	head := []ui.Node{
		html.Span(html.Props{Class: "issue3787-channel", Dir: "auto", Text: issue3787Channel(m, row)}),
		html.Span(html.Props{Class: "issue3787-line", Dir: "auto", Text: issue3787Line(m, row)}),
		html.Span(html.Props{Class: "issue3787-preview", Dir: "auto", Text: preview}),
		html.Span(html.Props{Class: "issue3787-age", Text: issue3787Age(m, row.AgeHours)}),
	}
	if note := issue3787Evidence(m, row); note != "" {
		head = append(head, html.Span(html.Props{Class: "issue3787-evidence", Dir: "auto"},
			html.Span(html.Props{Class: "issue3787-chip", Text: laneText(m, issue3787Copy, keyIssue3787Possible)}),
			ui.Text(" "+note)))
	} else if row.Kept {
		head = append(head, html.Span(html.Props{Class: "issue3787-evidence", Text: laneText(m, issue3787Copy, keyIssue3787Kept)}))
	}
	actions := []ui.Node{button(issue3787Open, keyIssue3787OpenBtn), button(issue3787Snooze, keyIssue3787SnoozeBtn), button(issue3787Done, keyIssue3787DoneBtn)}
	if issue3787Evidence(m, row) != "" {
		actions = append(actions, button(issue3787Keep, keyIssue3787KeepBtn))
	}
	actions = append(actions, button(issue3787Dismiss, keyIssue3787DismissBtn))
	return html.Li(html.Props{Class: "issue3787-item", Data: map[string]string{"followup-post": row.PostID}},
		html.Div(html.Props{Class: "issue3787-body"}, head...),
		html.Div(html.Props{Class: "issue3787-actions"}, actions...))
}

// issue3787Click handles the panel's controls. It reports whether it consumed
// the click.
func issue3787Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, id, extra := eventAction(e)
	if !strings.HasPrefix(action, "followups-") {
		return false
	}
	direction, conversation, _ := strings.Cut(extra, ":")
	switch action {
	case issue3787Toggle:
		if local.get().followUpsOpen {
			local.update(func(u *localUI) { u.followUpsOpen = false })
			return true
		}
		local.update(func(u *localUI) { u.followUpsOpen = true })
		// Opening always asks again, so the list is never older than the click.
		if m.Callbacks.LoadFollowUps != nil {
			m.Callbacks.LoadFollowUps()
		}
		focusField(issue3787HeadID)
		return true
	case issue3787Retry:
		if m.Callbacks.LoadFollowUps != nil {
			m.Callbacks.LoadFollowUps()
		}
		return true
	case issue3787Open:
		if m.Callbacks.OpenFollowUp == nil {
			return true
		}
		for _, row := range m.FollowUps {
			if row.PostID == id && row.ConversationID == conversation && row.Direction == direction {
				m.Callbacks.OpenFollowUp(conversation, id, row.Sequence, row.ThreadRootID)
				break
			}
		}
		return true
	case issue3787Snooze, issue3787Done, issue3787Dismiss, issue3787Keep:
		if m.Callbacks.SetFollowUp != nil {
			m.Callbacks.SetFollowUp(conversation, id, direction, strings.TrimPrefix(action, "followups-"))
			// The row may go away; focus moves to the heading rather than falling to the page.
			focusField(issue3787HeadID)
		}
		return true
	}
	return false
}

// Issue3787Styles lays the panel out as a bounded, scrolling list at the top of
// the conversation list; controls are 44px targets on phones and logical
// properties keep the layout right in right-to-left.
const Issue3787Styles = `.issue3787-control{padding:0 .75rem}` +
	`.issue3787-row{width:100%}.issue3787-row.on{font-weight:600}` +
	`.issue3787-panel{margin:.25rem .75rem .5rem;padding:.5rem;border:1px solid var(--line,rgba(0,0,0,.15));border-radius:.5rem;max-height:60vh;overflow-y:auto}` +
	`.issue3787-heading{margin:0 0 .25rem;font-size:.875rem;font-weight:600}.issue3787-heading:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3787-intro{margin:0 0 .5rem;font-size:.75rem;opacity:.8}` +
	`.issue3787-note{margin:.25rem 0;display:grid;gap:.5rem;justify-items:start}` +
	`.issue3787-sections{display:grid;gap:.75rem}.issue3787-section-title{margin:0 0 .25rem;font-size:.8125rem;font-weight:600}` +
	`.issue3787-list{list-style:none;margin:0;padding:0;display:grid;gap:.5rem}` +
	`.issue3787-item{display:grid;gap:.375rem;padding:.375rem .5rem;border-radius:.375rem;background:var(--accent-soft,rgba(0,0,0,.04))}` +
	`.issue3787-body{display:grid;gap:.125rem;min-width:0}` +
	`.issue3787-channel{font-weight:600;font-size:.8125rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.issue3787-preview{overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow-wrap:anywhere}` +
	`.issue3787-line,.issue3787-age,.issue3787-evidence{font-size:.75rem;opacity:.85}` +
	`.issue3787-chip{display:inline-block;padding:0 .375rem;border:1px solid currentColor;border-radius:999px;font-weight:600}` +
	`.issue3787-actions{display:flex;flex-wrap:wrap;gap:.25rem}` +
	`.issue3787-act:focus-visible,.issue3787-row:focus-visible,.issue3787-retry:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3787-failed{margin:.5rem 0 0;font-size:.8125rem}` +
	`@media (max-width:640px){.issue3787-row,.issue3787-act,.issue3787-retry{min-height:44px}}` +
	`@media (prefers-reduced-motion:reduce){.issue3787-panel{scroll-behavior:auto}}`
