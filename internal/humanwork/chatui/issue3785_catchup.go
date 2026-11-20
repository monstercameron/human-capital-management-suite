package chatui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3785: "Catch me up". On the "New" line of a conversation, and in a
// thread's header, one press asks the server for a private brief of what the
// person missed: the messages that ask them something, the ones that read like
// a decision, and the ones that point at work, each with a link to the exact
// message. The brief is a card only the person sees. It is drawn at the end of
// the message list (or in the thread pane), is never a message, never reaches
// search or other people, and producing it does not move the read position.
//
// What a line says is what someone wrote, so the card says so: it does not
// report approvals or workflow state. It also says how much it covered, how
// much it did not read, and how many newer messages arrived after it was made.

const (
	// CatchUpLoading, CatchUpReady and CatchUpFailed are the phases of a brief.
	CatchUpLoading = "loading"
	CatchUpReady   = "ready"
	CatchUpFailed  = "failed"

	catchUpOpenAction  = "catchup-open"
	catchUpCloseAction = "catchup-close"
)

// CatchUpLine is one cited line: the post it comes from and what was written.
type CatchUpLine struct {
	PostID       string
	Sequence     uint64
	ThreadRootID string
	AuthorID     string
	Text         string
}

// CatchUpSection is one kind of line ("asked_you", "decisions" or "work").
type CatchUpSection struct {
	Kind  string
	Lines []CatchUpLine
	// More is how many lines of this kind did not fit.
	More int
}

// CatchUpBrief is the last brief asked for. ThreadRootID is empty for a
// conversation. Error is "denied", "gone" or "unavailable" when it failed.
type CatchUpBrief struct {
	Phase, ConversationID, ThreadRootID, Error string
	// Through is the newest message the brief was taken at; Cursor and After are
	// where the person's read position and the covered range started.
	Through, Cursor, After, CoveredFrom uint64
	Total, Covered, Omitted             int
	GeneratedAt                         time.Time
	Sections                            []CatchUpSection
}

// issue3785Act takes one of the card's presses. id is the message the range
// starts from (the "New" line) and thread the thread root, both from the
// button. It reports whether the press was the card's own.
func issue3785Act(m Model, action, id, thread string) bool {
	switch action {
	case catchUpOpenAction:
		busy := m.CatchUp.Phase == CatchUpLoading && m.CatchUp.ConversationID == m.SelectedID && m.CatchUp.ThreadRootID == thread
		if m.Callbacks.RequestCatchUp != nil && !busy && m.SelectedID != "" {
			m.Callbacks.RequestCatchUp(thread, id)
			chatux026Focus(`.catchup-card[data-catchup]`)
		}
		return true
	case catchUpCloseAction:
		if m.Callbacks.DismissCatchUp != nil {
			m.Callbacks.DismissCatchUp(thread)
			chatux026Focus(`[data-action="` + catchUpOpenAction + `"]`)
		}
		return true
	}
	return false
}

// issue3785Click handles the card's presses.
func issue3785Click(e ui.Event, m Model) bool {
	action, id, extra := eventAction(e)
	if !strings.HasPrefix(action, "catchup-") {
		return false
	}
	e.PreventDefault()
	return issue3785Act(m, action, id, extra)
}

// issue3785Offers reports whether a brief can be asked for here.
func issue3785Offers(m Model) bool {
	return m.Callbacks.RequestCatchUp != nil && m.Callbacks.DismissCatchUp != nil && m.State == StateReady && m.SelectedID != ""
}

// issue3785UnreadDivider is the "New" line. With a way to ask for a brief the
// line carries the button, so it is plain text beside a control rather than a
// separator that holds one.
func issue3785UnreadDivider(m Model, messageID string) ui.Node {
	label := m.t(KeyNew)
	if !issue3785Offers(m) {
		return html.Div(html.Props{Class: "unread-divider", Role: "separator", Aria: map[string]string{"label": label}}, html.Span(html.Props{Text: label}))
	}
	busy := m.CatchUp.Phase == CatchUpLoading && m.CatchUp.ConversationID == m.SelectedID && m.CatchUp.ThreadRootID == ""
	return html.Div(html.Props{Class: "unread-divider has-catchup"},
		html.Span(html.Props{Text: label}),
		html.Button(html.Props{Class: "catchup-offer", Type: "button", Disabled: busy, Data: map[string]string{"action": catchUpOpenAction, "id": messageID},
			Aria: map[string]string{"busy": boolString(busy)}, Text: issue3785Text(m, "title")}))
}

// issue3785Rows is the conversation's card, at the end of the message list.
func issue3785Rows(m Model) []ui.Node {
	b := m.CatchUp
	if b.Phase == "" || b.ConversationID != m.SelectedID || b.ThreadRootID != "" || m.Callbacks.DismissCatchUp == nil {
		return nil
	}
	newer := 0
	for _, msg := range m.Messages {
		if b.Phase == CatchUpReady && msg.Sequence > b.Through {
			newer++
		}
	}
	return []ui.Node{html.WithKey(issue3785Card(m, newer), "catchup:"+b.ConversationID)}
}

// issue3785ThreadSlot is the thread header's offer, or its card once asked.
func issue3785ThreadSlot(m Model) ui.Node {
	if m.ThreadParentID == "" || m.SelectedID == "" || m.Callbacks.RequestCatchUp == nil || m.Callbacks.DismissCatchUp == nil || len(m.ThreadMessages) == 0 {
		return nil
	}
	b := m.CatchUp
	if b.Phase != "" && b.ConversationID == m.SelectedID && b.ThreadRootID == m.ThreadParentID {
		newer := 0
		for _, msg := range m.ThreadMessages {
			if b.Phase == CatchUpReady && msg.Sequence > b.Through {
				newer++
			}
		}
		return issue3785Card(m, newer)
	}
	return html.Div(html.Props{Class: "catchup-thread-offer"},
		html.Button(html.Props{Class: "catchup-offer", Type: "button", Data: map[string]string{"action": catchUpOpenAction, "extra": m.ThreadParentID}, Text: issue3785Text(m, "title")}))
}

func issue3785Name(m Model, id string) string {
	if name := chatKnownName(m, id); name != "" {
		return name
	}
	return issue3785Text(m, "someone")
}

// issue3785Href opens the message a line cites.
func issue3785Href(conversation string, line CatchUpLine) string {
	href := ChannelReferenceURL(conversation)
	if line.PostID == "" || line.Sequence == 0 {
		return href
	}
	return href + "&message=" + url.QueryEscape(line.PostID) + "&at=" + strconv.FormatUint(line.Sequence, 10)
}

func issue3785Card(m Model, newer int) ui.Node {
	b := m.CatchUp
	title := issue3785Text(m, "title")
	thread := b.ThreadRootID
	closeButton := html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": catchUpCloseAction, "extra": thread}, Text: issue3785Text(m, "close")})
	children := []ui.Node{
		html.P(html.Props{Class: "catchup-marker", Text: issue3785Text(m, "private")}),
		html.H4(html.Props{ID: "catchup-title", Class: "catchup-title", Text: title}),
	}
	attrs := map[string]string{"catchup": b.Phase}
	props := html.Props{Class: "catchup-card", Role: "region", TabIndex: -1, Data: attrs, Aria: map[string]string{"labelledby": "catchup-title"}}
	switch b.Phase {
	case CatchUpLoading:
		props.Aria["busy"] = "true"
		children = append(children, html.P(html.Props{Class: "catchup-status", Role: "status", Text: issue3785Text(m, "loading")}))
	case CatchUpFailed:
		key := "err_unavailable"
		switch b.Error {
		case "denied":
			key = "err_denied"
		case "gone":
			key = "err_gone"
		}
		children = append(children,
			html.P(html.Props{Class: "catchup-failed", Role: "alert", Text: issue3785Text(m, key)}),
			html.Div(html.Props{Class: "catchup-actions"},
				html.Button(html.Props{Class: "button small", Type: "button", Data: map[string]string{"action": catchUpOpenAction, "extra": thread}, Text: issue3785Text(m, "retry")}), closeButton))
	default:
		children = append(children, issue3785Result(m, newer)...)
		children = append(children,
			html.P(html.Props{Class: "catchup-note", Text: issue3785Text(m, "private_note")}),
			html.Div(html.Props{Class: "catchup-actions"},
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": catchUpOpenAction, "extra": thread}, Text: issue3785Text(m, "retry")}), closeButton))
	}
	return html.Div(props, children...)
}

func issue3785Result(m Model, newer int) []ui.Node {
	b := m.CatchUp
	if b.Total == 0 && b.Covered == 0 {
		nodes := []ui.Node{html.P(html.Props{Class: "catchup-status", Role: "status", Text: issue3785Text(m, "nothing")})}
		return append(nodes, issue3785Newer(m, newer)...)
	}
	// A thread has no read position of its own, so its brief covers the whole
	// thread and must not claim these were messages the person missed.
	rangeKey, noneKey := "range", "none"
	if b.ThreadRootID != "" {
		rangeKey, noneKey = "range_thread", "none_thread"
	}
	nodes := []ui.Node{html.P(html.Props{Class: "catchup-status", Role: "status", Text: issue3785Format(m, rangeKey, map[string]string{"covered": itoa(b.Covered), "total": itoa(b.Total)})})}
	if b.Omitted > 0 {
		omitted := "omitted"
		if b.Omitted == 1 {
			omitted = "omitted_one"
		}
		nodes = append(nodes, html.P(html.Props{Class: "catchup-partial", Text: issue3785Format(m, omitted, map[string]string{"n": itoa(b.Omitted)})}))
	}
	nodes = append(nodes, issue3785Newer(m, newer)...)
	if len(b.Sections) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "catchup-empty", Text: issue3785Format(m, noneKey, map[string]string{"covered": itoa(b.Covered)})}))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "catchup-note", Text: issue3785Text(m, "attributed")}))
	}
	for _, section := range b.Sections {
		rows := make([]ui.Node, 0, len(section.Lines)+1)
		for _, line := range section.Lines {
			name := issue3785Name(m, line.AuthorID)
			rows = append(rows, html.Li(html.Props{Class: "catchup-line", Data: map[string]string{"post": line.PostID}},
				html.Strong(html.Props{Dir: "auto", Text: name}),
				html.Span(html.Props{Class: "catchup-text", Dir: "auto", Text: line.Text}),
				html.A(html.Props{Class: "catchup-open", Href: issue3785Href(b.ConversationID, line),
					Aria: map[string]string{"label": issue3785Format(m, "open_from", map[string]string{"name": name})}, Text: issue3785Text(m, "open")})))
		}
		if section.More > 0 {
			rows = append(rows, html.Li(html.Props{Class: "catchup-more", Text: issue3785Format(m, "more", map[string]string{"n": itoa(section.More)})}))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "catchup-group", Data: map[string]string{"group": section.Kind}},
			html.H5(html.Props{Text: issue3785Text(m, "group_"+section.Kind)}), html.Ul(html.Props{Class: "catchup-list"}, rows...)))
	}
	return nodes
}

func issue3785Newer(m Model, newer int) []ui.Node {
	if newer <= 0 {
		return nil
	}
	key := "newer"
	if newer == 1 {
		key = "newer_one"
	}
	return []ui.Node{html.P(html.Props{Class: "catchup-newer", Role: "status", Text: issue3785Format(m, key, map[string]string{"n": itoa(newer)})})}
}

var issue3785Words = map[string][3]string{
	"title":           {"Catch me up", "Auf den neuesten Stand bringen", "أطلعني على ما فاتني"},
	"private":         {"Only visible to you", "Nur für Sie sichtbar", "مرئي لك وحدك"},
	"loading":         {"Reading what you missed…", "Verpasste Nachrichten werden gelesen …", "جارٍ قراءة ما فاتك…"},
	"range":           {"Covers {covered} of {total} messages you missed, up to the newest at the time you asked.", "Umfasst {covered} von {total} verpassten Nachrichten, bis zur neuesten zum Zeitpunkt Ihrer Anfrage.", "يشمل {covered} من أصل {total} رسالة فاتتك، حتى أحدث رسالة وقت طلبك."},
	"omitted_one":     {"1 older message was not read.", "1 ältere Nachricht wurde nicht gelesen.", "لم تُقرأ رسالة أقدم واحدة."},
	"range_thread":    {"Covers {covered} of {total} messages in this thread, up to the newest at the time you asked.", "Umfasst {covered} von {total} Nachrichten in diesem Thread, bis zur neuesten zum Zeitpunkt Ihrer Anfrage.", "يشمل {covered} من أصل {total} رسالة في هذا النقاش، حتى أحدث رسالة وقت طلبك."},
	"none_thread":     {"Nothing in the {covered} messages of this thread asks you something or reads as a decision.", "Keine der {covered} Nachrichten in diesem Thread richtet eine Frage an Sie oder liest sich wie eine Entscheidung.", "لا شيء في رسائل هذا النقاش الـ {covered} يسألك عن شيء أو يبدو كقرار."},
	"omitted":         {"{n} older messages were not read.", "{n} ältere Nachrichten wurden nicht gelesen.", "لم تُقرأ {n} رسالة أقدم."},
	"newer":           {"{n} newer messages arrived after this brief. Catch up again to include them.", "{n} neuere Nachrichten sind nach dieser Übersicht eingetroffen. Aktualisieren Sie die Übersicht, um sie einzubeziehen.", "وصلت {n} رسائل أحدث بعد هذا الملخص. اطلب الملخص مرة أخرى لتضمينها."},
	"newer_one":       {"1 newer message arrived after this brief. Catch up again to include it.", "1 neuere Nachricht ist nach dieser Übersicht eingetroffen. Aktualisieren Sie die Übersicht, um sie einzubeziehen.", "وصلت رسالة أحدث بعد هذا الملخص. اطلب الملخص مرة أخرى لتضمينها."},
	"attributed":      {"These are the words of the people who wrote them, not confirmed decisions or approvals. Open a message to check it, and open linked work to see where it stands now.", "Das sind die Worte der Verfasserinnen und Verfasser, keine bestätigten Entscheidungen oder Genehmigungen. Öffnen Sie eine Nachricht, um sie zu prüfen, und die verknüpfte Arbeit, um den aktuellen Stand zu sehen.", "هذه كلمات كاتبيها، وليست قرارات أو موافقات مؤكدة. افتح الرسالة للتحقق منها، وافتح العمل المرتبط لترى وضعه الآن."},
	"private_note":    {"Only you can see this brief. Making this did not mark anything as read.", "Nur Sie sehen diese Übersicht. Beim Erstellen wurde nichts als gelesen markiert.", "أنت وحدك ترى هذا الملخص. إنشاؤه لم يضع علامة مقروء على أي شيء."},
	"group_asked_you": {"Asked of you", "An Sie gerichtet", "موجّه إليك"},
	"group_decisions": {"Possible decisions", "Möglicherweise Entscheidungen", "قرارات محتملة"},
	"group_work":      {"Work mentioned", "Erwähnte Arbeit", "أعمال مذكورة"},
	"more":            {"{n} more not listed", "{n} weitere nicht aufgeführt", "{n} أخرى غير مدرجة"},
	"open":            {"Open message", "Nachricht öffnen", "فتح الرسالة"},
	"open_from":       {"Open the message from {name}", "Nachricht von {name} öffnen", "فتح الرسالة من {name}"},
	"someone":         {"Someone", "Jemand", "شخص ما"},
	"nothing":         {"You have not missed anything.", "Sie haben nichts verpasst.", "لم يفتك شيء."},
	"none":            {"Nothing in the {covered} messages you missed asks you something or reads as a decision.", "Keine der {covered} verpassten Nachrichten richtet eine Frage an Sie oder liest sich wie eine Entscheidung.", "لا شيء في الرسائل الـ {covered} التي فاتتك يسألك عن شيء أو يبدو كقرار."},
	"retry":           {"Catch me up again", "Erneut aktualisieren", "أطلعني مجددًا"},
	"close":           {"Close", "Schließen", "إغلاق"},
	"err_denied":      {"This conversation can no longer be opened by you, so no brief was made.", "Sie können diese Unterhaltung nicht mehr öffnen, daher wurde keine Übersicht erstellt.", "لم يعد بإمكانك فتح هذه المحادثة، لذا لم يُنشأ ملخص."},
	"err_gone":        {"The message this brief starts from is no longer available, so no brief was made. Try again.", "Die Nachricht, ab der die Übersicht beginnen sollte, ist nicht mehr verfügbar, daher wurde keine Übersicht erstellt. Versuchen Sie es erneut.", "الرسالة التي يبدأ منها هذا الملخص لم تعد متاحة، لذا لم يُنشأ ملخص. حاول مرة أخرى."},
	"err_unavailable": {"The brief could not be made, so it is incomplete and does not say that nothing happened. Nothing was changed. Try again.", "Die Übersicht konnte nicht erstellt werden. Sie ist daher unvollständig und sagt nicht, dass nichts passiert ist. Es wurde nichts geändert. Versuchen Sie es erneut.", "تعذّر إنشاء الملخص، فهو غير مكتمل ولا يعني أنه لم يحدث شيء. لم يتغير شيء. حاول مرة أخرى."},
}

func issue3785Text(m Model, key string) string {
	words := issue3785Words[key]
	return chatbug039Text("issue3785."+key, words[chatbug039LocaleIndex(m.Locale)], words[0])
}

func issue3785Format(m Model, key string, values map[string]string) string {
	text := issue3785Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

const issue3785Styles = `.unread-divider.has-catchup{flex-wrap:wrap}` +
	`.catchup-offer{display:inline-flex;align-items:center;min-height:28px;padding:2px 12px;border:1px solid var(--line);border-radius:999px;background:var(--surface);color:var(--ink);font:inherit;font-size:.75rem;font-weight:600;letter-spacing:0;cursor:pointer}` +
	`.catchup-offer:hover,.catchup-offer:focus-visible{border-color:var(--accent);color:var(--accent)}.catchup-offer:disabled{opacity:.6;cursor:progress}` +
	`.catchup-card{display:grid;gap:8px;box-sizing:border-box;inline-size:min(100%,var(--chat-column,720px));margin-block:8px;margin-inline:16px;padding:12px;border:1px solid var(--line);border-inline-start:3px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--soft);min-width:0;overflow-wrap:anywhere}` +
	`.catchup-marker{margin:0;font-size:.75rem;font-weight:650;color:var(--muted)}.catchup-title,.catchup-card h5{margin:0;font-size:.9375rem}` +
	`.catchup-card p{margin:0;font-size:.8125rem;line-height:1.4}.catchup-note,.catchup-more{color:var(--muted)}.catchup-partial,.catchup-newer{font-weight:600}.catchup-failed{color:var(--danger,var(--ink))}` +
	`.catchup-list{display:grid;gap:8px;margin:0;padding:0;list-style:none}.catchup-line{display:grid;gap:2px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);min-width:0}` +
	`.catchup-text{font-size:.8125rem}.catchup-open{justify-self:start;display:inline-flex;align-items:center;min-height:32px;font-size:.8125rem}` +
	`.catchup-actions{display:flex;flex-wrap:wrap;gap:8px}.catchup-thread-offer{padding:4px 12px}` +
	`@media (pointer:coarse),(max-width:480px){.catchup-offer,.catchup-open,.catchup-actions .button{min-height:44px}.catchup-card{margin-inline:8px}.catchup-actions{flex-direction:column}.catchup-actions .button{inline-size:100%}}` +
	`@media (prefers-reduced-motion:reduce){.catchup-card,.catchup-offer{transition:none!important;animation:none!important}}`
