package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3782: reviewed conversation evidence attached to a governed case. The
// server decides everything (who may attach, which messages the person may
// read, what each reader may see of a frozen copy); this file draws what it
// said (EvidenceState) in the Conversation details, under the Case section: the
// evidence already attached, each item marked when its original was edited or is
// gone, and for the case owner (or a workspace administrator) the form that
// attaches more.
//
// The form is the review: it lists the messages with the text that will be
// copied, names the conversation they come from, the case they are attached to
// and who can see them, and asks the person to confirm they reviewed it. The page
// sends message ids, never text.
//
// The copy never claims more than evidence is: it does not record an approval, a
// policy version or an outcome. The service that owns the case holds those.

// EvidenceItem is one frozen message as the server let this reader see it.
type EvidenceItem struct {
	ID string
	// Status is "current", "edited" or "gone". A gone item carries no text.
	Status      string
	AuthorID    string
	AuthoredAt  time.Time
	Body        string
	Fingerprint string
}

// EvidenceExcerpt is the messages one person attached together.
type EvidenceExcerpt struct {
	ID           string
	CaseRevision string
	Note         string
	AttachedBy   string
	AttachedAt   time.Time
	Items        []EvidenceItem
}

// EvidenceState is what the server said about the open conversation's case
// evidence. Transient client state; the text in it is what this reader may read.
type EvidenceState struct {
	// ConversationID is the conversation the answer is about.
	ConversationID string
	Loaded         bool
	Bound          bool
	CaseRef        string
	// CanAttach is set for the case owner and for a workspace administrator who
	// can open the conversation.
	CanAttach bool
	Excerpts  []EvidenceExcerpt
	// Busy is set while an attachment is on its way.
	Busy bool
	// Notice says how the last attachment went: "done", "denied", "invalid",
	// "changed", "gone" or "failed".
	Notice string
}

// evidenceHere returns the state when it describes the open conversation.
func (m Model) evidenceHere() (EvidenceState, bool) {
	s := m.Evidence
	return s, s.Loaded && s.ConversationID != "" && s.ConversationID == m.SelectedID
}

const (
	keyIssue3782Title        = "chat.issue3782.title"
	keyIssue3782Intro        = "chat.issue3782.intro"
	keyIssue3782Empty        = "chat.issue3782.empty"
	keyIssue3782AttachedBy   = "chat.issue3782.attached_by"
	keyIssue3782Someone      = "chat.issue3782.someone"
	keyIssue3782Revision     = "chat.issue3782.revision"
	keyIssue3782Fingerprint  = "chat.issue3782.fingerprint"
	keyIssue3782Current      = "chat.issue3782.status_current"
	keyIssue3782Edited       = "chat.issue3782.status_edited"
	keyIssue3782Gone         = "chat.issue3782.status_gone"
	keyIssue3782FormTitle    = "chat.issue3782.form_title"
	keyIssue3782Choose       = "chat.issue3782.choose"
	keyIssue3782ChooseHint   = "chat.issue3782.choose_hint"
	keyIssue3782NoMessages   = "chat.issue3782.no_messages"
	keyIssue3782ReviewSource = "chat.issue3782.review_source"
	keyIssue3782ReviewCase   = "chat.issue3782.review_case"
	keyIssue3782ReviewWho    = "chat.issue3782.review_who"
	keyIssue3782FieldRev     = "chat.issue3782.field_revision"
	keyIssue3782FieldRevHint = "chat.issue3782.field_revision_hint"
	keyIssue3782FieldNote    = "chat.issue3782.field_note"
	keyIssue3782Reviewed     = "chat.issue3782.reviewed"
	keyIssue3782Submit       = "chat.issue3782.submit"
	keyIssue3782Saving       = "chat.issue3782.saving"
	keyIssue3782Done         = "chat.issue3782.done"
	keyIssue3782Denied       = "chat.issue3782.denied"
	keyIssue3782Invalid      = "chat.issue3782.invalid"
	keyIssue3782Changed      = "chat.issue3782.changed"
	keyIssue3782GoneNotice   = "chat.issue3782.gone_notice"
	keyIssue3782Failed       = "chat.issue3782.failed"
)

var issue3782Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3782Title:        "Case evidence",
		keyIssue3782Intro:        "Messages attached to case {case} as supporting evidence. They show what was said. They do not record an approval, a policy version or an outcome: the system that owns the case holds those.",
		keyIssue3782Empty:        "Nothing is attached yet.",
		keyIssue3782AttachedBy:   "Attached by {name} on {when}",
		keyIssue3782Someone:      "someone",
		keyIssue3782Revision:     "Proposal revision {rev}",
		keyIssue3782Fingerprint:  "Fingerprint {fingerprint}",
		keyIssue3782Current:      "Unchanged since it was attached",
		keyIssue3782Edited:       "Edited since it was attached. This is the text that was attached.",
		keyIssue3782Gone:         "The original message was deleted or is no longer open to you. The attached text is withheld.",
		keyIssue3782FormTitle:    "Attach messages as evidence",
		keyIssue3782Choose:       "Choose messages",
		keyIssue3782ChooseHint:   "Only messages loaded in this conversation are listed. Each one is copied as you can read it now.",
		keyIssue3782NoMessages:   "No messages are loaded in this conversation yet.",
		keyIssue3782ReviewSource: "From: {conversation}",
		keyIssue3782ReviewCase:   "Attached to: case {case}",
		keyIssue3782ReviewWho:    "Who can see it: everyone in this conversation ({count} in all). Each person sees the attached text only while they can open the original message. The case owner decides who is in the conversation.",
		keyIssue3782FieldRev:     "Proposal revision (optional)",
		keyIssue3782FieldRevHint: "As the case system names it, for example rev-3. Chat does not check it.",
		keyIssue3782FieldNote:    "Why it matters (optional)",
		keyIssue3782Reviewed:     "I reviewed the selected messages, the case and who can see them",
		keyIssue3782Submit:       "Attach selected messages",
		keyIssue3782Saving:       "Attaching...",
		keyIssue3782Done:         "The messages are attached to the case.",
		keyIssue3782Denied:       "Only the case owner or a workspace administrator can attach evidence.",
		keyIssue3782Invalid:      "Choose at least one message and confirm that you reviewed it.",
		keyIssue3782Changed:      "This conversation is bound to a different case now. Open the case section and review again.",
		keyIssue3782GoneNotice:   "One of the messages is no longer available to you. Choose again.",
		keyIssue3782Failed:       "The evidence could not be attached. Try again.",
	},
	"de-DE": {
		keyIssue3782Title:        "Fallnachweise",
		keyIssue3782Intro:        "Nachrichten, die dem Fall {case} als Beleg beigefügt sind. Sie zeigen, was gesagt wurde. Sie halten keine Genehmigung, keine Richtlinienversion und kein Ergebnis fest: Das System, dem der Fall gehört, führt diese.",
		keyIssue3782Empty:        "Noch nichts beigefügt.",
		keyIssue3782AttachedBy:   "Beigefügt von {name} am {when}",
		keyIssue3782Someone:      "jemandem",
		keyIssue3782Revision:     "Antragsversion {rev}",
		keyIssue3782Fingerprint:  "Fingerabdruck {fingerprint}",
		keyIssue3782Current:      "Seit dem Beifügen unverändert",
		keyIssue3782Edited:       "Seit dem Beifügen bearbeitet. Dies ist der Text, der beigefügt wurde.",
		keyIssue3782Gone:         "Die ursprüngliche Nachricht wurde gelöscht oder ist für Sie nicht mehr zugänglich. Der beigefügte Text wird zurückgehalten.",
		keyIssue3782FormTitle:    "Nachrichten als Nachweis beifügen",
		keyIssue3782Choose:       "Nachrichten auswählen",
		keyIssue3782ChooseHint:   "Es werden nur Nachrichten aufgeführt, die in dieser Unterhaltung geladen sind. Jede wird so kopiert, wie Sie sie jetzt lesen können.",
		keyIssue3782NoMessages:   "In dieser Unterhaltung sind noch keine Nachrichten geladen.",
		keyIssue3782ReviewSource: "Aus: {conversation}",
		keyIssue3782ReviewCase:   "Beigefügt zu: Fall {case}",
		keyIssue3782ReviewWho:    "Wer es sieht: alle in dieser Unterhaltung ({count} insgesamt). Jede Person sieht den beigefügten Text nur, solange sie die ursprüngliche Nachricht öffnen kann. Die Fallverantwortlichen bestimmen, wer in der Unterhaltung ist.",
		keyIssue3782FieldRev:     "Antragsversion (optional)",
		keyIssue3782FieldRevHint: "So, wie das Fallsystem sie nennt, zum Beispiel rev-3. Der Chat prüft sie nicht.",
		keyIssue3782FieldNote:    "Warum es wichtig ist (optional)",
		keyIssue3782Reviewed:     "Ich habe die ausgewählten Nachrichten, den Fall und den Kreis der Personen geprüft, die sie sehen",
		keyIssue3782Submit:       "Ausgewählte Nachrichten beifügen",
		keyIssue3782Saving:       "Wird beigefügt...",
		keyIssue3782Done:         "Die Nachrichten sind dem Fall beigefügt.",
		keyIssue3782Denied:       "Nur die Fallverantwortlichen oder Workspace-Administratoren können Nachweise beifügen.",
		keyIssue3782Invalid:      "Wählen Sie mindestens eine Nachricht aus und bestätigen Sie, dass Sie sie geprüft haben.",
		keyIssue3782Changed:      "Diese Unterhaltung ist jetzt an einen anderen Fall gebunden. Öffnen Sie den Abschnitt Fall und prüfen Sie erneut.",
		keyIssue3782GoneNotice:   "Eine der Nachrichten ist für Sie nicht mehr verfügbar. Wählen Sie erneut.",
		keyIssue3782Failed:       "Der Nachweis konnte nicht beigefügt werden. Versuchen Sie es erneut.",
	},
	"ar": {
		keyIssue3782Title:        "أدلة القضية",
		keyIssue3782Intro:        "رسائل مرفقة بالقضية {case} كدليل داعم. تُظهر ما قيل، ولا تسجّل موافقة أو إصدار سياسة أو نتيجة: النظام الذي يملك القضية هو من يحتفظ بها.",
		keyIssue3782Empty:        "لم يُرفق شيء بعد.",
		keyIssue3782AttachedBy:   "أرفقها {name} في {when}",
		keyIssue3782Someone:      "شخص ما",
		keyIssue3782Revision:     "مراجعة المقترح {rev}",
		keyIssue3782Fingerprint:  "البصمة {fingerprint}",
		keyIssue3782Current:      "لم تتغير منذ إرفاقها",
		keyIssue3782Edited:       "عُدّلت منذ إرفاقها. هذا هو النص الذي أُرفق.",
		keyIssue3782Gone:         "حُذفت الرسالة الأصلية أو لم تعد متاحة لك. النص المرفق محجوب.",
		keyIssue3782FormTitle:    "إرفاق رسائل كدليل",
		keyIssue3782Choose:       "اختر الرسائل",
		keyIssue3782ChooseHint:   "تُعرض فقط الرسائل المحمّلة في هذه المحادثة. تُنسخ كل رسالة كما تستطيع قراءتها الآن.",
		keyIssue3782NoMessages:   "لم تُحمَّل أي رسائل في هذه المحادثة بعد.",
		keyIssue3782ReviewSource: "من: {conversation}",
		keyIssue3782ReviewCase:   "مُرفقة بـ: القضية {case}",
		keyIssue3782ReviewWho:    "من يراها: جميع المشاركين في هذه المحادثة ({count} في المجموع). يرى كل شخص النص المرفق فقط ما دام يستطيع فتح الرسالة الأصلية. صاحب القضية يحدد من في المحادثة.",
		keyIssue3782FieldRev:     "مراجعة المقترح (اختياري)",
		keyIssue3782FieldRevHint: "كما يسميها نظام القضايا، مثل rev-3. لا تتحقق الدردشة منها.",
		keyIssue3782FieldNote:    "سبب الأهمية (اختياري)",
		keyIssue3782Reviewed:     "راجعت الرسائل المحددة والقضية ومن يستطيع رؤيتها",
		keyIssue3782Submit:       "إرفاق الرسائل المحددة",
		keyIssue3782Saving:       "جارٍ الإرفاق...",
		keyIssue3782Done:         "أُرفقت الرسائل بالقضية.",
		keyIssue3782Denied:       "يمكن لصاحب القضية أو مسؤول مساحة العمل فقط إرفاق الأدلة.",
		keyIssue3782Invalid:      "اختر رسالة واحدة على الأقل وأكّد أنك راجعتها.",
		keyIssue3782Changed:      "هذه المحادثة مرتبطة الآن بقضية أخرى. افتح قسم القضية وراجع من جديد.",
		keyIssue3782GoneNotice:   "إحدى الرسائل لم تعد متاحة لك. اختر من جديد.",
		keyIssue3782Failed:       "تعذّر إرفاق الدليل. حاول مرة أخرى.",
	},
}

// Issue3782Text is the copy of the evidence surface in a locale; the browser
// client uses it for the sentences it announces.
func Issue3782Text(locale, key string) string {
	return chatbug039Text(key, issue3782Copy[chatEmojiLocale(locale)][key], issue3782Copy["en-US"][key])
}

// Issue3782DoneKey names the sentence the browser client announces on success.
const Issue3782DoneKey = keyIssue3782Done

// issue3782MaxChoices bounds the messages offered for selection: the newest.
const issue3782MaxChoices = 30

// issue3782BodyRunes bounds the text shown for one message in the chooser.
const issue3782BodyRunes = 240

func issue3782Clip(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= issue3782BodyRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:issue3782BodyRunes])) + "…"
}

func issue3782Name(m Model, id string) string {
	if name := strings.TrimSpace(chatKnownName(m, id)); name != "" && name != id {
		return name
	}
	return laneText(m, issue3782Copy, keyIssue3782Someone)
}

func issue3782Date(m Model, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	local := at.In(issue3920Location(m))
	return formatDay(m.Locale, local, local.Year() != time.Now().In(issue3920Location(m)).Year())
}

// issue3782Section is the Case evidence section of the details. It is shown to
// anyone who can open a case-bound conversation and has something to read, and
// to whoever may attach.
func issue3782Section(m Model) ui.Node {
	s, ok := m.evidenceHere()
	if !ok || !s.Bound || (len(s.Excerpts) == 0 && !s.CanAttach) {
		return nil
	}
	children := []ui.Node{
		html.H4(html.Props{Class: "chatevid-title", ID: "chatevid-title", Text: laneText(m, issue3782Copy, keyIssue3782Title)}),
		html.P(html.Props{Class: "chatevid-line chatevid-muted", Dir: "auto", Text: laneTextf(m, issue3782Copy, keyIssue3782Intro, map[string]string{"case": s.CaseRef})}),
	}
	if len(s.Excerpts) == 0 {
		children = append(children, html.P(html.Props{Class: "chatevid-line", Text: laneText(m, issue3782Copy, keyIssue3782Empty)}))
	} else {
		rows := make([]ui.Node, 0, len(s.Excerpts))
		for _, e := range s.Excerpts {
			rows = append(rows, html.WithKey(issue3782Excerpt(m, e), "chatevid-"+e.ID))
		}
		children = append(children, html.Ul(html.Props{Class: "chatevid-list", Aria: map[string]string{"labelledby": "chatevid-title"}}, rows...))
	}
	if s.CanAttach {
		children = append(children, issue3782Form(m, s))
	}
	if s.Notice != "" {
		children = append(children, html.P(html.Props{Class: "chatevid-notice", Role: issue3782NoticeRole(s.Notice), Text: issue3782NoticeText(m, s.Notice)}))
	}
	return html.Section(html.Props{Class: "details-section chatevid", Aria: map[string]string{"labelledby": "chatevid-title"}, Data: map[string]string{"evidence-section": "true"}}, children...)
}

func issue3782Excerpt(m Model, e EvidenceExcerpt) ui.Node {
	head := []ui.Node{
		html.P(html.Props{Class: "chatevid-line chatevid-meta", Dir: "auto", Text: laneTextf(m, issue3782Copy, keyIssue3782AttachedBy, map[string]string{"name": issue3782Name(m, e.AttachedBy), "when": issue3782Date(m, e.AttachedAt)})}),
	}
	if e.CaseRevision != "" {
		head = append(head, html.P(html.Props{Class: "chatevid-line chatevid-meta", Dir: "auto", Text: laneTextf(m, issue3782Copy, keyIssue3782Revision, map[string]string{"rev": e.CaseRevision})}))
	}
	if e.Note != "" {
		head = append(head, html.P(html.Props{Class: "chatevid-line chatevid-note", Dir: "auto", Text: e.Note}))
	}
	items := make([]ui.Node, 0, len(e.Items))
	for _, item := range e.Items {
		items = append(items, html.WithKey(issue3782Item(m, item), "chatevid-item-"+item.ID))
	}
	return html.Li(html.Props{Class: "chatevid-excerpt", Data: map[string]string{"evidence-excerpt": e.ID}}, append(head, html.Ul(html.Props{Class: "chatevid-items"}, items...))...)
}

func issue3782Item(m Model, item EvidenceItem) ui.Node {
	key := keyIssue3782Current
	switch item.Status {
	case "edited":
		key = keyIssue3782Edited
	case "gone":
		key = keyIssue3782Gone
	default:
		item.Status = "current"
	}
	status := html.Span(html.Props{Class: "chatevid-status chatevid-status-" + item.Status, Data: map[string]string{"evidence-status": item.Status}, Text: laneText(m, issue3782Copy, key)})
	if item.Status == "gone" {
		return html.Li(html.Props{Class: "chatevid-item chatevid-item-gone"}, status)
	}
	byline := issue3782Name(m, item.AuthorID)
	if when := issue3782Date(m, item.AuthoredAt); when != "" {
		byline += ", " + when
	}
	children := []ui.Node{
		html.Strong(html.Props{Class: "chatevid-author", Dir: "auto", Text: byline}),
		html.Blockquote(html.Props{Class: "chatevid-body", Dir: "auto", Text: item.Body}),
		status,
	}
	if item.Fingerprint != "" {
		children = append(children, html.Span(html.Props{Class: "chatevid-fingerprint", Dir: "ltr", Text: laneTextf(m, issue3782Copy, keyIssue3782Fingerprint, map[string]string{"fingerprint": item.Fingerprint})}))
	}
	return html.Li(html.Props{Class: "chatevid-item"}, children...)
}

// issue3782Choices are the messages offered for selection: the newest of the
// loaded ones that have an id and text.
func issue3782Choices(m Model) []Message {
	var out []Message
	for _, msg := range m.Messages {
		if msg.ID != "" && strings.TrimSpace(msg.Body) != "" {
			out = append(out, msg)
		}
	}
	if len(out) > issue3782MaxChoices {
		out = out[len(out)-issue3782MaxChoices:]
	}
	return out
}

// issue3782Audience is how many people the audience statement counts.
func issue3782Audience(m Model) int {
	if c := m.selected(); c.MemberCount > 0 {
		return c.MemberCount
	}
	return len(m.Members)
}

// issue3782Form is the review and attach form. It is a real form: Enter
// submits, the fields are labelled, and the client reads them by name.
func issue3782Form(m Model, s EvidenceState) ui.Node {
	choices := issue3782Choices(m)
	heading := html.Strong(html.Props{Class: "chatevid-form-title", Text: laneText(m, issue3782Copy, keyIssue3782FormTitle)})
	if len(choices) == 0 {
		return html.Div(html.Props{Class: "chatevid-manage"}, heading, html.P(html.Props{Class: "chatevid-line chatevid-muted", Text: laneText(m, issue3782Copy, keyIssue3782NoMessages)}))
	}
	boxes := make([]ui.Node, 0, len(choices))
	for _, msg := range choices {
		author := msg.Author
		if author == "" {
			author = issue3782Name(m, msg.AuthorID)
		}
		boxes = append(boxes, html.WithKey(html.Label(html.Props{Class: "chatevid-choice", For: "chatevid-msg-" + msg.ID},
			html.Input(html.Props{ID: "chatevid-msg-" + msg.ID, Type: "checkbox", Name: "message", Value: msg.ID, Disabled: s.Busy}),
			html.Span(html.Props{Class: "chatevid-choice-text", Dir: "auto"},
				html.Strong(html.Props{Text: author}),
				html.Span(html.Props{Class: "chatevid-muted", Text: " " + msg.TimeLabel}),
				html.Span(html.Props{Class: "chatevid-choice-body", Text: issue3782Clip(msg.Body)}))), "chatevid-choice-"+msg.ID))
	}
	label := laneText(m, issue3782Copy, keyIssue3782Submit)
	if s.Busy {
		label = laneText(m, issue3782Copy, keyIssue3782Saving)
	}
	conversation := displayName(m, m.selected())
	fields := []ui.Node{
		heading,
		html.Div(html.Props{Class: "chatevid-group", Role: "group", Aria: map[string]string{"labelledby": "chatevid-choose", "describedby": "chatevid-choose-hint"}},
			html.Span(html.Props{Class: "chatevid-label", ID: "chatevid-choose", Text: laneText(m, issue3782Copy, keyIssue3782Choose)}),
			html.P(html.Props{Class: "chatevid-hint", ID: "chatevid-choose-hint", Text: laneText(m, issue3782Copy, keyIssue3782ChooseHint)}),
			html.Div(html.Props{Class: "chatevid-choices"}, boxes...)),
		html.Div(html.Props{Class: "chatevid-review", Data: map[string]string{"evidence-review": "true"}},
			html.P(html.Props{Class: "chatevid-line", Dir: "auto", Text: laneTextf(m, issue3782Copy, keyIssue3782ReviewSource, map[string]string{"conversation": conversation})}),
			html.P(html.Props{Class: "chatevid-line", Dir: "auto", Text: laneTextf(m, issue3782Copy, keyIssue3782ReviewCase, map[string]string{"case": s.CaseRef})}),
			html.P(html.Props{Class: "chatevid-line", Text: laneTextf(m, issue3782Copy, keyIssue3782ReviewWho, map[string]string{"count": strconv.Itoa(issue3782Audience(m))})})),
		html.Label(html.Props{Class: "chatevid-label", For: "chatevid-revision", Text: laneText(m, issue3782Copy, keyIssue3782FieldRev)}),
		html.Input(html.Props{ID: "chatevid-revision", Class: "chat-input", Type: "text", Name: "case_revision", MaxLength: 80, AutoComplete: "off", Dir: "auto", Disabled: s.Busy,
			Aria: map[string]string{"describedby": "chatevid-revision-hint"}}),
		html.P(html.Props{Class: "chatevid-hint", ID: "chatevid-revision-hint", Text: laneText(m, issue3782Copy, keyIssue3782FieldRevHint)}),
		html.Label(html.Props{Class: "chatevid-label", For: "chatevid-note", Text: laneText(m, issue3782Copy, keyIssue3782FieldNote)}),
		html.Textarea(html.Props{ID: "chatevid-note", Class: "chat-input", Name: "note", Rows: 2, MaxLength: 500, Dir: "auto", Disabled: s.Busy}),
		html.Label(html.Props{Class: "chatevid-check", For: "chatevid-reviewed"},
			html.Input(html.Props{ID: "chatevid-reviewed", Name: "reviewed", Type: "checkbox", Required: true, Disabled: s.Busy}),
			html.Span(html.Props{Text: laneText(m, issue3782Copy, keyIssue3782Reviewed)})),
		html.Button(html.Props{Type: "submit", Class: "button primary chatevid-button", Disabled: s.Busy, Aria: map[string]string{"busy": boolString(s.Busy)}, Text: label}),
	}
	return html.Div(html.Props{Class: "chatevid-manage"}, html.Form(html.Props{Class: "chatevid-form", Data: map[string]string{"evidence-form": m.SelectedID}}, fields...))
}

func issue3782NoticeRole(notice string) string {
	if notice == "done" {
		return "status"
	}
	return "alert"
}

func issue3782NoticeText(m Model, notice string) string {
	switch notice {
	case "done":
		return laneText(m, issue3782Copy, keyIssue3782Done)
	case "denied":
		return laneText(m, issue3782Copy, keyIssue3782Denied)
	case "invalid":
		return laneText(m, issue3782Copy, keyIssue3782Invalid)
	case "changed":
		return laneText(m, issue3782Copy, keyIssue3782Changed)
	case "gone":
		return laneText(m, issue3782Copy, keyIssue3782GoneNotice)
	}
	return laneText(m, issue3782Copy, keyIssue3782Failed)
}

// Issue3782Styles lays the section out for a phone first: text wraps instead of
// clipping, the choices, fields and button are 44px targets, and logical
// properties keep it right in a right-to-left language.
const Issue3782Styles = `
.chatevid{display:flex;flex-direction:column;gap:6px;padding:12px 16px;border-block-start:1px solid var(--hcm-color-border);overflow-wrap:anywhere}
.chatevid-title{margin:0;font-size:.875rem}
.chatevid-line,.chatevid-hint{margin:0;font-size:.8125rem}
.chatevid-muted,.chatevid-hint,.chatevid-meta,.chatevid-fingerprint{color:var(--hcm-color-text-muted)}
.chatevid-list,.chatevid-items{display:flex;flex-direction:column;gap:8px;margin:0;padding:0;list-style:none}
.chatevid-excerpt{display:flex;flex-direction:column;gap:4px;padding-block:6px;border-block-start:1px solid var(--hcm-color-border)}
.chatevid-item{display:flex;flex-direction:column;gap:2px;padding-inline-start:8px;border-inline-start:3px solid var(--hcm-color-border)}
.chatevid-body{margin:0;font-size:.8125rem;white-space:pre-wrap}
.chatevid-status{font-size:.75rem;font-weight:600}
.chatevid-status-edited,.chatevid-status-gone{color:var(--hcm-color-warning-text,#8a4b00)}
.chatevid-fingerprint{font-size:.6875rem;font-family:ui-monospace,monospace}
.chatevid-manage,.chatevid-form,.chatevid-group{display:flex;flex-direction:column;gap:6px}
.chatevid-choices{display:flex;flex-direction:column;gap:4px;max-block-size:240px;overflow:auto}
.chatevid-choice,.chatevid-check{display:flex;align-items:flex-start;gap:8px;min-block-size:44px;font-size:.8125rem}
.chatevid-choice-text{display:flex;flex-direction:column;min-inline-size:0}
.chatevid-review{display:flex;flex-direction:column;gap:2px;padding:8px;border:1px solid var(--hcm-color-border);border-radius:6px;background:var(--soft)}
.chatevid-label{font-size:.8125rem;font-weight:600}
.chatevid-form .chat-input{box-sizing:border-box;inline-size:100%;min-block-size:44px}
.chatevid-button{min-block-size:44px;min-inline-size:44px;max-inline-size:100%;white-space:normal}
.chatevid-notice{margin:0;font-size:.8125rem}
.chatevid-notice[role=alert]{color:var(--hcm-color-danger,#b3261e)}
@media (prefers-reduced-motion:reduce){.chatevid,.chatevid *{transition:none;animation:none}}
`
