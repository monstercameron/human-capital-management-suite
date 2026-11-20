package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3795: a form card in a conversation. An agent that is missing inputs
// posts the fields it needs; the person it names fills them in right in the
// thread and presses Submit. The card is drawn from the post's body (the
// field definitions) and from FormCardsState (what the server says this reader
// may do and has seen of it). Everyone else in the conversation sees who it is
// for and whether it was answered, never what was said. The form is a plain
// <form>: the browser client sends it (tools/uxqual/cmd/journeywasm) and the
// server checks every answer again, so nothing here is trusted.

// FormCardView is what the server said this reader may do with one card.
type FormCardView struct {
	// Loaded is false until the server has answered for this card.
	Loaded      bool
	CanSubmit   bool
	Submitted   bool
	SubmittedAt time.Time
	// Mine holds the reader's own answers once they have submitted; it is never
	// filled for anyone else.
	Mine map[string]string
	// Busy is set while a submission is on its way.
	Busy bool
	// Errors names the fields the server refused, by chat.FormErr* code.
	Errors map[string]string
	// Notice says why the last press of Submit was not accepted: "failed",
	// "denied", "conflict" or "invalid".
	Notice string
}

// FormCardsState is the page's transient view of the form cards on screen.
type FormCardsState struct {
	Views map[string]FormCardView
}

const (
	keyIssue3795ForYou       = "chat.issue3795.for_you"
	keyIssue3795AskedOf      = "chat.issue3795.asked_of"
	keyIssue3795Waiting      = "chat.issue3795.waiting"
	keyIssue3795Loading      = "chat.issue3795.loading"
	keyIssue3795Required     = "chat.issue3795.required"
	keyIssue3795Choose       = "chat.issue3795.choose"
	keyIssue3795Submit       = "chat.issue3795.submit"
	keyIssue3795Sending      = "chat.issue3795.sending"
	keyIssue3795Answered     = "chat.issue3795.answered"
	keyIssue3795YourAnswers  = "chat.issue3795.your_answers"
	keyIssue3795NoAnswer     = "chat.issue3795.no_answer"
	keyIssue3795Summary      = "chat.issue3795.summary"
	keyIssue3795NoticeFailed = "chat.issue3795.notice_failed"
	keyIssue3795NoticeDenied = "chat.issue3795.notice_denied"
	keyIssue3795NoticeDone   = "chat.issue3795.notice_conflict"
	keyIssue3795Someone      = "chat.issue3795.someone"
	keyIssue3795FormLabel    = "chat.issue3795.form_label"
	keyIssue3795From         = "chat.issue3795.from"
	keyIssue3795ErrPrefix    = "chat.issue3795.err."
)

var issue3795Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3795ForYou:                               "Asked of you",
		keyIssue3795AskedOf:                              "Asked of {name}",
		keyIssue3795Waiting:                              "Waiting for {name} to answer.",
		keyIssue3795Loading:                              "Checking this form...",
		keyIssue3795Required:                             "required",
		keyIssue3795Choose:                               "Choose...",
		keyIssue3795Submit:                               "Submit",
		keyIssue3795Sending:                              "Sending...",
		keyIssue3795Answered:                             "Answered",
		keyIssue3795YourAnswers:                          "Your answers",
		keyIssue3795NoAnswer:                             "Not answered",
		keyIssue3795Summary:                              "Some answers need your attention.",
		keyIssue3795NoticeFailed:                         "Your answers could not be sent. Try again.",
		keyIssue3795NoticeDenied:                         "Only {name} can answer this form.",
		keyIssue3795NoticeDone:                           "This form was already answered.",
		keyIssue3795Someone:                              "someone",
		keyIssue3795FormLabel:                            "Form: {title}",
		keyIssue3795From:                                 "From {name}",
		keyIssue3795ErrPrefix + chat.FormErrRequired:     "This answer is required.",
		keyIssue3795ErrPrefix + chat.FormErrTooLong:      "This answer is too long.",
		keyIssue3795ErrPrefix + chat.FormErrBadText:      "This answer has characters that are not allowed.",
		keyIssue3795ErrPrefix + chat.FormErrBadDate:      "Enter a real date.",
		keyIssue3795ErrPrefix + chat.FormErrBadOption:    "Choose one of the options.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownUser:  "Choose someone who is in this conversation.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownField: "This answer is not accepted.",
	},
	"de-DE": {
		keyIssue3795ForYou:                               "An Sie gerichtet",
		keyIssue3795AskedOf:                              "An {name} gerichtet",
		keyIssue3795Waiting:                              "Wartet auf die Antwort von {name}.",
		keyIssue3795Loading:                              "Das Formular wird geprüft...",
		keyIssue3795Required:                             "erforderlich",
		keyIssue3795Choose:                               "Auswählen...",
		keyIssue3795Submit:                               "Absenden",
		keyIssue3795Sending:                              "Wird gesendet...",
		keyIssue3795Answered:                             "Beantwortet",
		keyIssue3795YourAnswers:                          "Ihre Antworten",
		keyIssue3795NoAnswer:                             "Nicht beantwortet",
		keyIssue3795Summary:                              "Einige Antworten brauchen Ihre Aufmerksamkeit.",
		keyIssue3795NoticeFailed:                         "Ihre Antworten konnten nicht gesendet werden. Versuchen Sie es erneut.",
		keyIssue3795NoticeDenied:                         "Nur {name} kann dieses Formular beantworten.",
		keyIssue3795NoticeDone:                           "Dieses Formular wurde bereits beantwortet.",
		keyIssue3795Someone:                              "jemand",
		keyIssue3795FormLabel:                            "Formular: {title}",
		keyIssue3795From:                                 "Von {name}",
		keyIssue3795ErrPrefix + chat.FormErrRequired:     "Diese Antwort ist erforderlich.",
		keyIssue3795ErrPrefix + chat.FormErrTooLong:      "Diese Antwort ist zu lang.",
		keyIssue3795ErrPrefix + chat.FormErrBadText:      "Diese Antwort enthält nicht erlaubte Zeichen.",
		keyIssue3795ErrPrefix + chat.FormErrBadDate:      "Geben Sie ein gültiges Datum ein.",
		keyIssue3795ErrPrefix + chat.FormErrBadOption:    "Wählen Sie eine der Optionen.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownUser:  "Wählen Sie jemanden aus dieser Unterhaltung.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownField: "Diese Antwort wird nicht akzeptiert.",
	},
	"ar": {
		keyIssue3795ForYou:                               "موجّه إليك",
		keyIssue3795AskedOf:                              "موجّه إلى {name}",
		keyIssue3795Waiting:                              "في انتظار إجابة {name}.",
		keyIssue3795Loading:                              "جارٍ التحقق من هذا النموذج...",
		keyIssue3795Required:                             "مطلوب",
		keyIssue3795Choose:                               "اختر...",
		keyIssue3795Submit:                               "إرسال",
		keyIssue3795Sending:                              "جارٍ الإرسال...",
		keyIssue3795Answered:                             "تمت الإجابة",
		keyIssue3795YourAnswers:                          "إجاباتك",
		keyIssue3795NoAnswer:                             "بلا إجابة",
		keyIssue3795Summary:                              "بعض الإجابات تحتاج إلى انتباهك.",
		keyIssue3795NoticeFailed:                         "تعذّر إرسال إجاباتك. حاول مرة أخرى.",
		keyIssue3795NoticeDenied:                         "{name} فقط يمكنه الإجابة عن هذا النموذج.",
		keyIssue3795NoticeDone:                           "تمت الإجابة عن هذا النموذج من قبل.",
		keyIssue3795Someone:                              "شخص ما",
		keyIssue3795FormLabel:                            "نموذج: {title}",
		keyIssue3795From:                                 "من {name}",
		keyIssue3795ErrPrefix + chat.FormErrRequired:     "هذه الإجابة مطلوبة.",
		keyIssue3795ErrPrefix + chat.FormErrTooLong:      "هذه الإجابة طويلة جدًا.",
		keyIssue3795ErrPrefix + chat.FormErrBadText:      "تحتوي هذه الإجابة على أحرف غير مسموح بها.",
		keyIssue3795ErrPrefix + chat.FormErrBadDate:      "أدخل تاريخًا صحيحًا.",
		keyIssue3795ErrPrefix + chat.FormErrBadOption:    "اختر أحد الخيارات.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownUser:  "اختر شخصًا من هذه المحادثة.",
		keyIssue3795ErrPrefix + chat.FormErrUnknownField: "هذه الإجابة غير مقبولة.",
	},
}

// Issue3795Text is the copy of the form card in a locale (used by the browser
// client for the sentences it announces).
func Issue3795Text(locale, key string) string {
	return chatbug039Text(key, issue3795Copy[chatEmojiLocale(locale)][key], issue3795Copy["en-US"][key])
}

func issue3795Errorf(m Model, code string) string {
	key := keyIssue3795ErrPrefix + code
	if _, known := issue3795Copy["en-US"][key]; !known {
		key = keyIssue3795ErrPrefix + chat.FormErrUnknownField
	}
	return laneText(m, issue3795Copy, key)
}

// Issue3795FieldID is the DOM id of one field of one card. The browser client
// reads the answers by field name inside the form, so the id is only for
// labels and described-by.
func issue3795FieldID(post, field string) string { return "form-card-" + post + "-" + field }

func issue3795Name(m Model, subject string) string {
	if name := chatKnownName(m, subject); name != "" {
		return name
	}
	return laneText(m, issue3795Copy, keyIssue3795Someone)
}

// issue3795Display is a stored answer in words: a select shows the option's
// label, a person their name, a date the day.
func issue3795Display(m Model, f chat.FormField, value string) string {
	switch f.Type {
	case chat.FormFieldSelect:
		for _, o := range f.Options {
			if o.Value == value {
				return o.Label
			}
		}
	case chat.FormFieldPerson:
		return issue3795Name(m, value)
	case chat.FormFieldDate:
		if at, err := time.Parse("2006-01-02", value); err == nil {
			return formatDay(m.Locale, at, true)
		}
	}
	return value
}

func issue3795Field(m Model, post string, f chat.FormField, view FormCardView) ui.Node {
	id := issue3795FieldID(post, f.ID)
	code := view.Errors[f.ID]
	aria := map[string]string{}
	if f.Required {
		aria["required"] = "true"
	}
	children := []ui.Node{}
	labelChildren := []ui.Node{html.Span(html.Props{Dir: "auto", Text: f.Label})}
	if f.Required {
		labelChildren = append(labelChildren, html.Span(html.Props{Class: "chat-form-card-required", Text: " (" + laneText(m, issue3795Copy, keyIssue3795Required) + ")"}))
	}
	children = append(children, html.Label(html.Props{For: id, Class: "chat-form-card-label"}, labelChildren...))
	if code != "" {
		aria["invalid"] = "true"
		aria["describedby"] = id + "-error"
	}
	var control ui.Node
	switch f.Type {
	case chat.FormFieldDate:
		control = html.Input(html.Props{Key: id, ID: id, Name: f.ID, Type: "date", Class: "chat-input", Required: f.Required, Disabled: view.Busy, Aria: aria})
	case chat.FormFieldSelect:
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: laneText(m, issue3795Copy, keyIssue3795Choose)})}
		for _, o := range f.Options {
			options = append(options, html.Option(html.Props{Raw: map[string]any{"value": o.Value}, Text: o.Label}))
		}
		control = html.Select(html.Props{Key: id, ID: id, Name: f.ID, Class: "chat-input", Required: f.Required, Disabled: view.Busy, Aria: aria}, options...)
	case chat.FormFieldPerson:
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: laneText(m, issue3795Copy, keyIssue3795Choose)})}
		for _, member := range m.Members {
			if member.ID == "" || strings.TrimSpace(member.Name) == "" {
				continue
			}
			options = append(options, html.Option(html.Props{Raw: map[string]any{"value": member.ID}, Text: member.Name}))
		}
		control = html.Select(html.Props{Key: id, ID: id, Name: f.ID, Class: "chat-input", Required: f.Required, Disabled: view.Busy, Aria: aria}, options...)
	default:
		max := f.MaxLength
		if max == 0 {
			max = 500
		}
		if max > 120 {
			control = html.Textarea(html.Props{Key: id, ID: id, Name: f.ID, Class: "chat-input", Rows: 3, MaxLength: max, Dir: "auto", Required: f.Required, Disabled: view.Busy, Aria: aria})
		} else {
			control = html.Input(html.Props{Key: id, ID: id, Name: f.ID, Type: "text", Class: "chat-input", MaxLength: max, Dir: "auto", Required: f.Required, Disabled: view.Busy, Aria: aria})
		}
	}
	children = append(children, control)
	if code != "" {
		children = append(children, html.P(html.Props{ID: id + "-error", Class: "chat-form-card-error", Text: issue3795Errorf(m, code)}))
	}
	return html.Div(html.Props{Class: "chat-form-card-field", Key: id + "-field"}, children...)
}

func issue3795Answers(m Model, card chat.FormCard, view FormCardView) ui.Node {
	rows := []ui.Node{}
	for _, f := range card.Fields {
		value, given := view.Mine[f.ID]
		text := laneText(m, issue3795Copy, keyIssue3795NoAnswer)
		if given {
			text = issue3795Display(m, f, value)
		}
		rows = append(rows, html.Div(html.Props{Class: "chat-form-card-answer"},
			html.Span(html.Props{Class: "chat-form-card-question", Dir: "auto", Text: f.Label}),
			html.Span(html.Props{Class: "chat-form-card-value", Dir: "auto", Text: text})))
	}
	return html.Div(html.Props{Class: "chat-form-card-answers", Role: "group", Aria: map[string]string{"label": laneText(m, issue3795Copy, keyIssue3795YourAnswers)}}, rows...)
}

// issue3795FormCard draws the card as its own row, or reports false for any
// other message.
func issue3795FormCard(m Model, msg Message) (ui.Node, bool) {
	card, ok := chat.ParseFormCard(msg.Body)
	if !ok {
		return nil, false
	}
	view := m.Forms.Views[msg.ID]
	forYou := card.Addressee == m.CurrentUser
	name := issue3795Name(m, card.Addressee)
	who := laneTextf(m, issue3795Copy, keyIssue3795AskedOf, map[string]string{"name": name})
	if forYou {
		who = laneText(m, issue3795Copy, keyIssue3795ForYou)
	}
	children := []ui.Node{
		html.Strong(html.Props{Class: "chat-form-card-title", Dir: "auto", Text: card.Title}),
		html.P(html.Props{Class: "chat-form-card-who", Text: who}),
	}
	// The card is not a message row, so it names who is asking.
	if asker := issue3795Asker(m, msg); asker != "" {
		children = append(children, html.P(html.Props{Class: "chat-form-card-who", Dir: "auto", Text: laneTextf(m, issue3795Copy, keyIssue3795From, map[string]string{"name": asker})}))
	}
	switch {
	case !view.Loaded:
		children = append(children, html.P(html.Props{Class: "chat-form-card-status", Role: "status", Text: laneText(m, issue3795Copy, keyIssue3795Loading)}))
	case view.Submitted:
		status := laneText(m, issue3795Copy, keyIssue3795Answered)
		if !view.SubmittedAt.IsZero() {
			status += " - " + chat5Clock(m.Locale, view.SubmittedAt)
		}
		children = append(children, html.P(html.Props{Class: "chat-form-card-status chat-form-card-done", Role: "status", Text: status}))
		if view.Mine != nil {
			children = append(children, html.Strong(html.Props{Class: "chat-form-card-subtitle", Text: laneText(m, issue3795Copy, keyIssue3795YourAnswers)}), issue3795Answers(m, card, view))
		}
	case view.CanSubmit:
		fields := []ui.Node{}
		if len(view.Errors) > 0 {
			fields = append(fields, html.P(html.Props{Class: "chat-form-card-error", Role: "alert", Text: laneText(m, issue3795Copy, keyIssue3795Summary)}))
		}
		for _, f := range card.Fields {
			fields = append(fields, issue3795Field(m, msg.ID, f, view))
		}
		switch view.Notice {
		case "failed":
			fields = append(fields, html.P(html.Props{Class: "chat-form-card-error", Role: "alert", Text: laneText(m, issue3795Copy, keyIssue3795NoticeFailed)}))
		case "conflict":
			fields = append(fields, html.P(html.Props{Class: "chat-form-card-error", Role: "alert", Text: laneText(m, issue3795Copy, keyIssue3795NoticeDone)}))
		}
		label := laneText(m, issue3795Copy, keyIssue3795Submit)
		if view.Busy {
			label = laneText(m, issue3795Copy, keyIssue3795Sending)
		}
		fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button primary small chat-form-card-submit", Text: label, Disabled: view.Busy, Aria: map[string]string{"busy": boolString(view.Busy)}}))
		children = append(children, html.Form(html.Props{Key: "form-card-" + msg.ID, Class: "chat-form-card-form", Raw: map[string]any{"data-form-card": msg.ID, "noValidate": true}}, fields...))
	default:
		text := laneTextf(m, issue3795Copy, keyIssue3795Waiting, map[string]string{"name": name})
		if view.Notice == "denied" {
			text = laneTextf(m, issue3795Copy, keyIssue3795NoticeDenied, map[string]string{"name": name})
		}
		children = append(children, html.P(html.Props{Class: "chat-form-card-status", Role: "status", Text: text}))
	}
	if !msg.SentAt.IsZero() {
		children = append(children, html.Time(html.Props{Class: "message-time", Text: chat5Clock(m.Locale, msg.SentAt)}))
	}
	region := html.Section(html.Props{Class: "chat-form-card", Aria: map[string]string{"label": laneTextf(m, issue3795Copy, keyIssue3795FormLabel, map[string]string{"title": card.Title})}}, children...)
	return html.Div(html.Props{Class: "message-row chat-form-card-row", Data: map[string]string{"message-id": msg.ID}}, region), true
}

func issue3795Asker(m Model, msg Message) string {
	if name := strings.TrimSpace(msg.Author); name != "" && name != msg.AuthorID {
		return name
	}
	return chatKnownName(m, msg.AuthorID)
}

// issue3795CardText is the sentence a form card stands for wherever a message
// shows as one line of text (the Saved panel, a preview): its title, never the
// body's marker or field definitions.
func issue3795CardText(m Model, msg Message) (string, bool) {
	card, ok := chat.ParseFormCard(msg.Body)
	if !ok {
		return "", false
	}
	return laneTextf(m, issue3795Copy, keyIssue3795FormLabel, map[string]string{"title": card.Title}), true
}

// IsFormCardBody reports whether a message body is a form card. The browser
// client asks the server about the cards of the open conversation only.
func IsFormCardBody(body string) bool {
	_, ok := chat.ParseFormCard(body)
	return ok
}

// Issue3795Styles lays the card out for a phone first: fields stack and fill
// the card, the Submit button is a 44px target, the card wraps rather than
// scrolls at 320px, and logical properties keep it right in a right-to-left
// language.
const Issue3795Styles = `
.chat-form-card-row{display:flex;padding:6px 16px}
.chat-form-card{display:flex;flex-direction:column;align-items:stretch;gap:8px;box-sizing:border-box;inline-size:min(100%,32rem);min-inline-size:0;padding:12px 14px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}
.chat-form-card-title{font-size:.9375rem}
.chat-form-card-who,.chat-form-card-status,.chat-form-card-subtitle{margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted)}
.chat-form-card-done{color:var(--hcm-color-text)}
.chat-form-card-form{display:flex;flex-direction:column;gap:10px;margin:0}
.chat-form-card-field{display:flex;flex-direction:column;gap:4px;min-inline-size:0}
.chat-form-card-label{font-size:.8125rem;font-weight:600}
.chat-form-card-required{font-weight:400;color:var(--hcm-color-text-muted)}
.chat-form-card .chat-input{inline-size:100%;box-sizing:border-box;min-block-size:44px;font:inherit}
.chat-form-card textarea.chat-input{resize:vertical}
.chat-form-card-error{margin:0;font-size:.8125rem;color:var(--hcm-color-danger,#b3261e)}
.chat-form-card [aria-invalid="true"]{border-color:var(--hcm-color-danger,#b3261e)}
.chat-form-card-submit{align-self:flex-start;min-block-size:44px;min-inline-size:44px}
.chat-form-card-answers{display:flex;flex-direction:column;gap:6px;margin:0}
.chat-form-card-answer{display:flex;flex-direction:column;gap:2px}
.chat-form-card-question{font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-form-card-value{font-size:.875rem}
.chat-form-card .message-time{font-size:.6875rem;color:var(--hcm-color-text-muted)}
`
