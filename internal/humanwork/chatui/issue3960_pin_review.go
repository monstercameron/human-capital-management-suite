package chatui

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3960: Review pinned guidance. A pin is often the first thing a
// newcomer reads, and a later message can replace what it says. From a pin in
// Conversation details the reader opens a private review, picks later messages
// of this conversation (only messages the reader can already open are listed,
// because the list is the page's own timeline), and sees the pin and each
// chosen message side by side with who wrote them and when. A date or link that
// differs between the two is called out as a possible conflict; nothing is
// ever called obsolete, and nothing is changed: Keep, Unpin and Inspect are
// explicit buttons that use the existing paths. The review is kept only in the
// page (PinReviewState), so it is private to the reader. When the review is not
// wired (Open is nil) the ordinary pinned list is unchanged.

// pinReviewMaxLater bounds the later messages offered for one pin.
const pinReviewMaxLater = 20

// pinReviewBodyRunes bounds how much of a message is shown in the side by side.
const pinReviewBodyRunes = 600

// PinReviewState is the page's transient state of the pin review. Nothing in it
// is stored on the server.
type PinReviewState struct {
	// PinID is the pinned message whose review is open ("" when none is).
	PinID string
	// Selected holds the later messages chosen for the comparison, by id.
	Selected map[string]bool
	// Kept holds the pins the reader chose to Keep after a review this session.
	Kept map[string]bool

	Open   func(pinID string)
	Close  func()
	Toggle func(sourceID string)
	Keep   func(pinID string)
}

const (
	keyIssue3960Button       = "chat.issue3960.button"
	keyIssue3960Title        = "chat.issue3960.title"
	keyIssue3960Private      = "chat.issue3960.private"
	keyIssue3960PinHeading   = "chat.issue3960.pin_heading"
	keyIssue3960LaterHeading = "chat.issue3960.later_heading"
	keyIssue3960LaterMessage = "chat.issue3960.later_message"
	keyIssue3960Scope        = "chat.issue3960.scope"
	keyIssue3960Compared     = "chat.issue3960.compared"
	keyIssue3960Choose       = "chat.issue3960.choose"
	keyIssue3960NoLater      = "chat.issue3960.no_later"
	keyIssue3960NoPosition   = "chat.issue3960.no_position"
	keyIssue3960PinDateGone  = "chat.issue3960.pin_date_unknown"
	keyIssue3960Documents    = "chat.issue3960.documents"
	keyIssue3960Compare      = "chat.issue3960.compare"
	keyIssue3960Uncompare    = "chat.issue3960.uncompare"
	keyIssue3960Conflict     = "chat.issue3960.conflict"
	keyIssue3960DatesDiffer  = "chat.issue3960.dates_differ"
	keyIssue3960LinksDiffer  = "chat.issue3960.links_differ"
	keyIssue3960NoDiff       = "chat.issue3960.no_diff"
	keyIssue3960Mentions     = "chat.issue3960.same_mentions"
	keyIssue3960Inspect      = "chat.issue3960.inspect"
	keyIssue3960Keep         = "chat.issue3960.keep"
	keyIssue3960Close        = "chat.issue3960.close"
	keyIssue3960Kept         = "chat.issue3960.kept"
	keyIssue3960Decide       = "chat.issue3960.decide"
	keyIssue3960Gone         = "chat.issue3960.gone"
	keyIssue3960CompareName  = "chat.issue3960.compare_name"
	keyIssue3960InspectName  = "chat.issue3960.inspect_name"
)

var issue3960Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3960Button:       "Review pinned guidance",
		keyIssue3960Title:        "Review pinned guidance",
		keyIssue3960Private:      "Only you see this review. Nothing is changed unless you press a button below.",
		keyIssue3960PinHeading:   "Pinned",
		keyIssue3960LaterHeading: "Later messages",
		keyIssue3960LaterMessage: "Later message",
		keyIssue3960Scope:        "{count} later messages are loaded in this conversation ({from} to {to}).",
		keyIssue3960Compared:     "Compared {chosen} of {count} later messages.",
		keyIssue3960Choose:       "Choose the later messages to compare with this pin.",
		keyIssue3960NoLater:      "No later messages are loaded in this conversation, so there is nothing to compare yet.",
		keyIssue3960NoPosition:   "This page cannot tell which messages are newer than this pin, so none are listed.",
		keyIssue3960PinDateGone:  "The date of this pin is not available.",
		keyIssue3960Documents:    "Documents were not checked. Only messages of this conversation are compared.",
		keyIssue3960Compare:      "Compare",
		keyIssue3960Uncompare:    "Remove",
		keyIssue3960Conflict:     "Possible conflict: {what}. Read both before you decide.",
		keyIssue3960DatesDiffer:  "dates differ",
		keyIssue3960LinksDiffer:  "links differ",
		keyIssue3960NoDiff:       "No differing dates or links were found. This does not mean the message agrees with the pin.",
		keyIssue3960Mentions:     "{what}: {values}",
		keyIssue3960Inspect:      "Inspect source",
		keyIssue3960Keep:         "Keep this pin",
		keyIssue3960Close:        "Close review",
		keyIssue3960Kept:         "Kept after your review",
		keyIssue3960Decide:       "You decide: keep the pin, or unpin it. Replacing it is a message you write yourself.",
		keyIssue3960Gone:         "{count} chosen messages are no longer available and were left out.",
		keyIssue3960CompareName:  "Compare with the message from {name}, {when}",
		keyIssue3960InspectName:  "Inspect the message from {name}, {when}",
	},
	"de-DE": {
		keyIssue3960Button:       "Angeheftete Hinweise prüfen",
		keyIssue3960Title:        "Angeheftete Hinweise prüfen",
		keyIssue3960Private:      "Nur Sie sehen diese Prüfung. Es ändert sich nichts, solange Sie keine Schaltfläche darunter drücken.",
		keyIssue3960PinHeading:   "Angeheftet",
		keyIssue3960LaterHeading: "Spätere Nachrichten",
		keyIssue3960LaterMessage: "Spätere Nachricht",
		keyIssue3960Scope:        "In dieser Unterhaltung sind {count} spätere Nachrichten geladen ({from} bis {to}).",
		keyIssue3960Compared:     "{chosen} von {count} späteren Nachrichten verglichen.",
		keyIssue3960Choose:       "Wählen Sie die späteren Nachrichten, die mit dieser Anheftung verglichen werden sollen.",
		keyIssue3960NoLater:      "In dieser Unterhaltung sind keine späteren Nachrichten geladen, daher gibt es noch nichts zu vergleichen.",
		keyIssue3960NoPosition:   "Diese Seite kann nicht erkennen, welche Nachrichten neuer als diese Anheftung sind, daher wird keine aufgelistet.",
		keyIssue3960PinDateGone:  "Das Datum dieser Anheftung ist nicht verfügbar.",
		keyIssue3960Documents:    "Dokumente wurden nicht geprüft. Es werden nur Nachrichten dieser Unterhaltung verglichen.",
		keyIssue3960Compare:      "Vergleichen",
		keyIssue3960Uncompare:    "Entfernen",
		keyIssue3960Conflict:     "Möglicher Widerspruch: {what}. Lesen Sie beide, bevor Sie entscheiden.",
		keyIssue3960DatesDiffer:  "Daten weichen ab",
		keyIssue3960LinksDiffer:  "Links weichen ab",
		keyIssue3960NoDiff:       "Es wurden keine abweichenden Daten oder Links gefunden. Das heißt nicht, dass die Nachricht mit der Anheftung übereinstimmt.",
		keyIssue3960Mentions:     "{what}: {values}",
		keyIssue3960Inspect:      "Quelle ansehen",
		keyIssue3960Keep:         "Anheftung behalten",
		keyIssue3960Close:        "Prüfung schließen",
		keyIssue3960Kept:         "Nach Ihrer Prüfung behalten",
		keyIssue3960Decide:       "Sie entscheiden: Anheftung behalten oder lösen. Ein Ersatz ist eine Nachricht, die Sie selbst schreiben.",
		keyIssue3960Gone:         "{count} gewählte Nachrichten sind nicht mehr verfügbar und wurden ausgelassen.",
		keyIssue3960CompareName:  "Mit der Nachricht von {name}, {when}, vergleichen",
		keyIssue3960InspectName:  "Die Nachricht von {name}, {when}, ansehen",
	},
	"ar": {
		keyIssue3960Button:       "مراجعة الإرشادات المثبتة",
		keyIssue3960Title:        "مراجعة الإرشادات المثبتة",
		keyIssue3960Private:      "أنت وحدك ترى هذه المراجعة. لا يتغير شيء ما لم تضغط على أحد الأزرار أدناه.",
		keyIssue3960PinHeading:   "مثبّتة",
		keyIssue3960LaterHeading: "رسائل لاحقة",
		keyIssue3960LaterMessage: "رسالة لاحقة",
		keyIssue3960Scope:        "تم تحميل {count} رسائل لاحقة في هذه المحادثة (من {from} إلى {to}).",
		keyIssue3960Compared:     "تمت مقارنة {chosen} من {count} رسائل لاحقة.",
		keyIssue3960Choose:       "اختر الرسائل اللاحقة لمقارنتها بهذا التثبيت.",
		keyIssue3960NoLater:      "لا توجد رسائل لاحقة محمّلة في هذه المحادثة، فلا شيء للمقارنة بعد.",
		keyIssue3960NoPosition:   "لا تستطيع هذه الصفحة تحديد الرسائل الأحدث من هذا التثبيت، لذلك لا تُعرض أي رسالة.",
		keyIssue3960PinDateGone:  "تاريخ هذا التثبيت غير متاح.",
		keyIssue3960Documents:    "لم يتم فحص المستندات. تتم مقارنة رسائل هذه المحادثة فقط.",
		keyIssue3960Compare:      "قارن",
		keyIssue3960Uncompare:    "إزالة",
		keyIssue3960Conflict:     "تعارض محتمل: {what}. اقرأ الاثنتين قبل أن تقرر.",
		keyIssue3960DatesDiffer:  "التواريخ مختلفة",
		keyIssue3960LinksDiffer:  "الروابط مختلفة",
		keyIssue3960NoDiff:       "لم يتم العثور على تواريخ أو روابط مختلفة. هذا لا يعني أن الرسالة تتفق مع التثبيت.",
		keyIssue3960Mentions:     "{what}: {values}",
		keyIssue3960Inspect:      "فحص المصدر",
		keyIssue3960Keep:         "الإبقاء على التثبيت",
		keyIssue3960Close:        "إغلاق المراجعة",
		keyIssue3960Kept:         "تم الإبقاء عليه بعد مراجعتك",
		keyIssue3960Decide:       "القرار لك: أبقِ التثبيت أو ألغِه. أما استبداله فرسالة تكتبها بنفسك.",
		keyIssue3960Gone:         "{count} رسائل مختارة لم تعد متاحة وتم استبعادها.",
		keyIssue3960CompareName:  "قارن مع رسالة {name}، {when}",
		keyIssue3960InspectName:  "افحص رسالة {name}، {when}",
	},
}

// Issue3960Text is the copy of the pin review in a locale.
func Issue3960Text(locale, key string) string {
	return chatbug039Text(key, issue3960Copy[chatEmojiLocale(locale)][key], issue3960Copy["en-US"][key])
}

const issue3960Months = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`

var (
	issue3960DateRE = regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}[./]\d{1,2}[./]\d{2,4}\b|\b` + issue3960Months + `\.? \d{1,2}\b|\b\d{1,2}\.? ` + issue3960Months + `\b`)
	issue3960LinkRE = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)
)

// issue3960Facts returns the dates and links a message writes, normalised and
// without repeats, in the order written. It reads words only: no model.
func issue3960Facts(body string) (dates, links []string) {
	seen := map[string]bool{}
	add := func(list *[]string, kind, value string) {
		key := kind + "\x00" + value
		if value != "" && !seen[key] {
			seen[key] = true
			*list = append(*list, value)
		}
	}
	for _, link := range issue3960LinkRE.FindAllString(body, -1) {
		add(&links, "l", strings.ToLower(strings.TrimRight(link, ".,;:!?")))
	}
	// Dates are read from the text without its links, so a path is not a date.
	for _, date := range issue3960DateRE.FindAllString(issue3960LinkRE.ReplaceAllString(body, " "), -1) {
		add(&dates, "d", strings.ToLower(strings.Join(strings.Fields(date), " ")))
	}
	return dates, links
}

func issue3960Differ(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return true
		}
	}
	other := map[string]bool{}
	for _, v := range b {
		other[v] = true
	}
	for _, v := range a {
		if !other[v] {
			return true
		}
	}
	return false
}

// issue3960Later lists the messages of the loaded timeline that came after the
// pin, oldest first and bounded. known is false when the pin's position is not
// known, because then newer cannot be told.
func issue3960Later(m Model, pin ChannelPin) (later []Message, known bool) {
	if pin.Sequence == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	for _, msg := range m.Messages {
		if msg.ID == "" || msg.ID == pin.PostID || msg.Sequence <= pin.Sequence || seen[msg.ID] || strings.TrimSpace(msg.Body) == "" {
			continue
		}
		seen[msg.ID] = true
		later = append(later, msg)
	}
	sort.SliceStable(later, func(i, j int) bool { return later[i].Sequence < later[j].Sequence })
	if len(later) > pinReviewMaxLater {
		later = later[len(later)-pinReviewMaxLater:]
	}
	return later, true
}

func issue3960When(m Model, msg Message) string {
	if !msg.SentAt.IsZero() {
		return chatux019PinTime(m, msg.SentAt)
	}
	return msg.TimeLabel
}

func issue3960Body(m Model, id, body string, revision uint64) string {
	text := strings.TrimSpace(chatDisplayBody(readerText(m, id, body, revision)))
	if runes := []rune(text); len(runes) > pinReviewBodyRunes {
		text = strings.TrimSpace(string(runes[:pinReviewBodyRunes])) + "…"
	}
	return text
}

func issue3960Snippet(m Model, msg Message) string {
	text := strings.Join(strings.Fields(chatDisplayBody(readerText(m, msg.ID, msg.Body, msg.Revision))), " ")
	if runes := []rune(text); len(runes) > 120 {
		text = strings.TrimSpace(string(runes[:120])) + "…"
	}
	return text
}

func issue3960PinMessage(m Model, pin ChannelPin) (Message, bool) {
	for _, msg := range m.Messages {
		if msg.ID == pin.PostID {
			return msg, true
		}
	}
	return Message{}, false
}

// issue3960Controls is what sits in a pin's action row: the Review button, and
// the note that the reader kept the pin. It is empty when the review is not
// wired, so the ordinary pinned list is unchanged.
func issue3960Controls(m Model, pin ChannelPin) []ui.Node {
	if m.PinReview.Open == nil {
		return nil
	}
	open := m.PinReview.PinID == pin.PostID
	label := laneText(m, issue3960Copy, keyIssue3960Button)
	out := []ui.Node{html.Button(html.Props{Class: "button secondary small", Type: "button", Title: label,
		Data: map[string]string{"action": "pinreview-open", "id": pin.PostID},
		Aria: map[string]string{"expanded": strconv.FormatBool(open), "controls": "pin-review-" + pin.PostID}}, ui.Text(label))}
	if m.PinReview.Kept[pin.PostID] && !open {
		out = append(out, html.Span(html.Props{Class: "pinreview-kept", Role: "status", Text: laneText(m, issue3960Copy, keyIssue3960Kept)}))
	}
	return out
}

// issue3960Panel is the open review of one pin, or nil.
func issue3960Panel(m Model, pin ChannelPin) ui.Node {
	if m.PinReview.Open == nil || m.PinReview.PinID != pin.PostID {
		return nil
	}
	t := func(key string) string { return laneText(m, issue3960Copy, key) }
	tf := func(key string, vars map[string]string) string { return laneTextf(m, issue3960Copy, key, vars) }
	later, known := issue3960Later(m, pin)
	pinMsg, pinLoaded := issue3960PinMessage(m, pin)

	pinBody := pin.Body
	pinRev := pin.Revision
	if pinLoaded && strings.TrimSpace(pinBody) == "" {
		pinBody, pinRev = pinMsg.Body, pinMsg.Revision
	}
	pinWhen := ""
	if pinLoaded && !pinMsg.SentAt.IsZero() {
		pinWhen = chatux019PinTime(m, pinMsg.SentAt)
	}
	pinHead := []ui.Node{html.Strong(html.Props{Class: "pinreview-label", Text: t(keyIssue3960PinHeading)}),
		html.Span(html.Props{Class: "pinreview-who", Dir: "auto", Text: pin.Author})}
	if pinWhen != "" {
		pinHead = append(pinHead, html.Span(html.Props{Class: "pinreview-when", Text: pinWhen}))
	}
	pinCard := []ui.Node{html.Div(html.Props{Class: "pinreview-head"}, pinHead...),
		html.P(html.Props{Class: "pinreview-body", Dir: "auto", Text: issue3960Body(m, pin.PostID, pinBody, pinRev)})}
	if pinWhen == "" {
		pinCard = append(pinCard, html.P(html.Props{Class: "pinreview-missing muted", Text: t(keyIssue3960PinDateGone)}))
	}
	pinDates, pinLinks := issue3960Facts(chatDisplayBody(readerText(m, pin.PostID, pinBody, pinRev)))

	byID := map[string]Message{}
	for _, msg := range later {
		byID[msg.ID] = msg
	}
	var chosen []Message
	gone := 0
	for id, on := range m.PinReview.Selected {
		if !on {
			continue
		}
		if msg, ok := byID[id]; ok {
			chosen = append(chosen, msg)
		} else {
			gone++
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Sequence < chosen[j].Sequence })

	children := []ui.Node{
		html.H4(html.Props{Class: "pinreview-title", Text: t(keyIssue3960Title)}),
		html.P(html.Props{Class: "field-hint pinreview-private", Text: t(keyIssue3960Private)}),
	}

	// What was checked, and what could not be.
	var scope string
	switch {
	case !known:
		scope = t(keyIssue3960NoPosition)
	case len(later) == 0:
		scope = t(keyIssue3960NoLater)
	case len(chosen) > 0:
		scope = tf(keyIssue3960Compared, map[string]string{"chosen": issue3960Num(m, len(chosen)), "count": issue3960Num(m, len(later))})
	default:
		scope = tf(keyIssue3960Scope, map[string]string{"count": issue3960Num(m, len(later)),
			"from": issue3960When(m, later[0]), "to": issue3960When(m, later[len(later)-1])}) + " " + t(keyIssue3960Choose)
	}
	status := []ui.Node{html.Span(html.Props{Text: scope})}
	if gone > 0 {
		status = append(status, html.Span(html.Props{Text: " " + tf(keyIssue3960Gone, map[string]string{"count": issue3960Num(m, gone)})}))
	}
	if pinWhen == "" {
		status = append(status, html.Span(html.Props{Text: " " + t(keyIssue3960PinDateGone)}))
	}
	status = append(status, html.Span(html.Props{Text: " " + t(keyIssue3960Documents)}))
	children = append(children, html.P(html.Props{Class: "pinreview-scope", Role: "status", Aria: map[string]string{"live": "polite"}}, status...))

	// The later messages to choose from.
	if len(later) > 0 {
		items := make([]ui.Node, 0, len(later))
		for _, msg := range later {
			on := m.PinReview.Selected[msg.ID]
			when := issue3960When(m, msg)
			label := tf(keyIssue3960CompareName, map[string]string{"name": msg.Author, "when": when})
			text := t(keyIssue3960Compare)
			if on {
				text = t(keyIssue3960Uncompare)
			}
			items = append(items, html.Li(html.Props{Class: "pinreview-candidate"},
				html.Span(html.Props{Class: "pinreview-candidate-text", Dir: "auto"},
					html.Strong(html.Props{Text: msg.Author}), html.Span(html.Props{Class: "pinreview-when", Text: " " + when}),
					html.Span(html.Props{Class: "pinreview-snippet", Text: issue3960Snippet(m, msg)})),
				html.Button(html.Props{Class: "button secondary small", Type: "button", Title: label,
					Data: map[string]string{"action": "pinreview-toggle", "id": msg.ID},
					Aria: map[string]string{"pressed": strconv.FormatBool(on), "label": label}}, ui.Text(text))))
		}
		children = append(children, html.H5(html.Props{Class: "pinreview-sub", Text: t(keyIssue3960LaterHeading)}),
			html.Ul(html.Props{Class: "pinreview-list"}, items...))
	}

	// Each chosen message beside the pin.
	for _, msg := range chosen {
		when := issue3960When(m, msg)
		dates, links := issue3960Facts(chatDisplayBody(readerText(m, msg.ID, msg.Body, msg.Revision)))
		var notes []ui.Node
		var what []string
		if issue3960Differ(pinDates, dates) {
			what = append(what, t(keyIssue3960DatesDiffer))
			notes = append(notes, html.P(html.Props{Class: "pinreview-values", Dir: "auto", Text: tf(keyIssue3960Mentions, map[string]string{"what": t(keyIssue3960PinHeading), "values": strings.Join(pinDates, ", ")})}),
				html.P(html.Props{Class: "pinreview-values", Dir: "auto", Text: tf(keyIssue3960Mentions, map[string]string{"what": t(keyIssue3960LaterMessage), "values": strings.Join(dates, ", ")})}))
		}
		if issue3960Differ(pinLinks, links) {
			what = append(what, t(keyIssue3960LinksDiffer))
			notes = append(notes, html.P(html.Props{Class: "pinreview-values", Dir: "auto", Text: tf(keyIssue3960Mentions, map[string]string{"what": t(keyIssue3960PinHeading), "values": strings.Join(pinLinks, ", ")})}),
				html.P(html.Props{Class: "pinreview-values", Dir: "auto", Text: tf(keyIssue3960Mentions, map[string]string{"what": t(keyIssue3960LaterMessage), "values": strings.Join(links, ", ")})}))
		}
		verdict := html.P(html.Props{Class: "pinreview-nodiff muted", Text: t(keyIssue3960NoDiff)})
		var verdictNode ui.Node = verdict
		if len(what) > 0 {
			verdictNode = html.P(html.Props{Class: "pinreview-conflict", Text: tf(keyIssue3960Conflict, map[string]string{"what": strings.Join(what, ", ")})})
		}
		inspect := tf(keyIssue3960InspectName, map[string]string{"name": msg.Author, "when": when})
		children = append(children, html.Div(html.Props{Class: "pinreview-pair"},
			html.Div(html.Props{Class: "pinreview-side"}, pinCard...),
			html.Div(html.Props{Class: "pinreview-side"},
				html.Div(html.Props{Class: "pinreview-head"},
					html.Strong(html.Props{Class: "pinreview-label", Text: t(keyIssue3960LaterMessage)}),
					html.Span(html.Props{Class: "pinreview-who", Dir: "auto", Text: msg.Author}),
					html.Span(html.Props{Class: "pinreview-when", Text: when})),
				html.P(html.Props{Class: "pinreview-body", Dir: "auto", Text: issue3960Body(m, msg.ID, msg.Body, msg.Revision)}),
				html.Button(html.Props{Class: "button secondary small", Type: "button", Title: inspect,
					Data: map[string]string{"action": "pinreview-inspect", "id": msg.ID}, Aria: map[string]string{"label": inspect},
					Disabled: m.Callbacks.JumpToPin == nil || msg.Sequence == 0}, ui.Text(t(keyIssue3960Inspect)))),
			html.Div(html.Props{Class: "pinreview-verdict"}, append([]ui.Node{verdictNode}, notes...)...)))
	}

	// The decision stays with the reader.
	children = append(children, html.P(html.Props{Class: "field-hint pinreview-decide", Text: t(keyIssue3960Decide)}),
		html.Div(html.Props{Class: "pinreview-actions"},
			actionButton("button secondary small", "pinreview-keep", pin.PostID, t(keyIssue3960Keep), m.PinReview.Keep == nil, ui.Text(t(keyIssue3960Keep))),
			actionButton("button secondary small", "unpin", pin.PostID, m.t(KeyUnpin), m.Callbacks.Unpin == nil, ui.Text(laneText(m, chatux021Copy, keyChatux019PinnedAction))),
			actionButton("button secondary small", "pinreview-close", pin.PostID, t(keyIssue3960Close), m.PinReview.Close == nil, ui.Text(t(keyIssue3960Close)))))
	return html.Div(html.Props{ID: "pin-review-" + pin.PostID, Class: "pinreview", Role: "region", TabIndex: -1, Aria: map[string]string{"label": t(keyIssue3960Title)}}, children...)
}

func issue3960Num(m Model, n int) string {
	if m.Number != nil {
		return m.Number(n)
	}
	return strconv.Itoa(n)
}

// pinReviewAction runs a control of the pin review.
func (m Model) pinReviewAction(action, id string) {
	r := m.PinReview
	switch action {
	case "pinreview-open":
		if r.Open == nil || id == "" {
			return
		}
		if r.PinID == id && r.Close != nil {
			r.Close()
			return
		}
		r.Open(id)
	case "pinreview-close":
		if r.Close != nil {
			r.Close()
		}
	case "pinreview-toggle":
		if r.Toggle != nil && r.PinID != "" && id != "" {
			r.Toggle(id)
		}
	case "pinreview-keep":
		if r.Keep != nil && id != "" {
			r.Keep(id)
		}
	case "pinreview-inspect":
		if m.Callbacks.JumpToPin == nil {
			return
		}
		for _, msg := range m.Messages {
			if msg.ID == id && msg.Sequence > 0 {
				m.Callbacks.JumpToPin(id, msg.Sequence)
				return
			}
		}
	}
}

// Issue3960Styles is the pin review's layout. Logical properties only, so it
// holds in right-to-left; targets are 44px on a phone.
const Issue3960Styles = `
.pinreview{margin-block-start:8px;padding:10px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);min-inline-size:0;display:grid;gap:8px}
.pinreview-title,.pinreview-sub{margin:0;font-size:.875rem}
.pinreview-sub{font-size:.8125rem;color:var(--hcm-color-text-muted)}
.pinreview-private,.pinreview-decide,.pinreview-scope{margin:0;font-size:.8125rem;overflow-wrap:anywhere}
.pinreview-list{list-style:none;margin:0;padding:0;display:grid;gap:6px}
.pinreview-candidate{display:flex;align-items:center;gap:8px;min-inline-size:0}
.pinreview-candidate-text{display:grid;gap:2px;min-inline-size:0;flex:1 1 auto;overflow-wrap:anywhere}
.pinreview-snippet{display:block;color:var(--hcm-color-text-muted);font-size:.8125rem}
.pinreview-when{color:var(--hcm-color-text-muted);font-size:.75rem}
.pinreview-pair{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,150px),1fr));gap:8px;min-inline-size:0}
.pinreview-side{display:grid;gap:4px;align-content:start;min-inline-size:0;padding:8px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}
.pinreview-head{display:flex;flex-wrap:wrap;gap:4px 8px;align-items:baseline}
.pinreview-body{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font-size:.8125rem}
.pinreview-verdict{grid-column:1/-1;display:grid;gap:2px;font-size:.8125rem;overflow-wrap:anywhere}
.pinreview-verdict p{margin:0}
.pinreview-conflict{font-weight:600;color:var(--hcm-color-text)}
.pinreview-actions{display:flex;flex-wrap:wrap;gap:8px}
.pinreview-kept{font-size:.75rem;color:var(--hcm-color-text-muted);align-self:center}
@media(max-width:767px){.pinreview button{min-block-size:44px;min-inline-size:44px}}
`
