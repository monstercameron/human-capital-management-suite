package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3793: ask for a governed change from the composer. "/leave" opens a
// typed form above the composer: the person fills in the employee, the dates
// and the reason, reviews what will be sent, and sends it. The request goes to
// the server as a proposal for review; nothing on this page says or implies
// that it was approved or carried out. The page only draws what the server
// answered (which commands this person may use here, which fields it refused,
// the receipt); the server decides all of it, so a value checked here is
// checked again there. The state is transient client state, never message
// content.

// Phases of the request panel.
const (
	IntentRequestForm    = "form"
	IntentRequestReview  = "review"
	IntentRequestSending = "sending"
	IntentRequestSent    = "sent"
)

// IntentRequestAvailable is what the server said about one command here.
type IntentRequestAvailable struct {
	Loaded    bool
	Available bool
	// Reason is why it is not available: chat-side "not_installed", "paused",
	// "not_granted" or "not_allowed".
	Reason string
}

// IntentRequestView is the open request panel.
type IntentRequestView struct {
	Open           bool
	Command        string
	ConversationID string
	Phase          string
	// Values are what the person typed, kept so editing never loses them.
	Values map[string]string
	// Errors names the fields the server refused, by code.
	Errors map[string]string
	// Via is the agent that takes the request in, as the review step shows it.
	Via string
	// Notice says why the last step did not go through: "failed", "denied" or
	// "unavailable".
	Notice string
	// Href opens the sent request.
	Href string
}

// IntentRequestsState is the page's view of the typed requests.
type IntentRequestsState struct {
	ConversationID string
	Commands       map[string]IntentRequestAvailable
	View           IntentRequestView
	// Open opens the panel for a command of the open conversation.
	Open func(command string)
}

const (
	keyIntentReqCommand     = "chat.issue3793.command"
	keyIntentReqHeading     = "chat.issue3793.heading"
	keyIntentReqIntro       = "chat.issue3793.intro"
	keyIntentReqClose       = "chat.issue3793.close"
	keyIntentReqReview      = "chat.issue3793.review"
	keyIntentReqSend        = "chat.issue3793.send"
	keyIntentReqEdit        = "chat.issue3793.edit"
	keyIntentReqSending     = "chat.issue3793.sending"
	keyIntentReqChoose      = "chat.issue3793.choose"
	keyIntentReqRequired    = "chat.issue3793.required"
	keyIntentReqOptional    = "chat.issue3793.optional"
	keyIntentReqReviewTitle = "chat.issue3793.review_title"
	keyIntentReqReviewNote  = "chat.issue3793.review_note"
	keyIntentReqSent        = "chat.issue3793.sent"
	keyIntentReqSentNote    = "chat.issue3793.sent_note"
	keyIntentReqOpen        = "chat.issue3793.open"
	keyIntentReqSummary     = "chat.issue3793.summary"
	keyIntentReqNone        = "chat.issue3793.none"
	keyIntentReqFailed      = "chat.issue3793.failed"
	keyIntentReqDenied      = "chat.issue3793.denied"
	keyIntentReqGone        = "chat.issue3793.gone"
	keyIntentReqFieldPrefix = "chat.issue3793.field."
	keyIntentReqErrPrefix   = "chat.issue3793.err."
	keyIntentReqWhyPrefix   = "chat.issue3793.why."
)

var issue3793Copy = map[string]map[string]string{
	"en-US": {
		keyIntentReqCommand:                                   "Ask for leave in this conversation",
		keyIntentReqHeading:                                   "Ask for leave",
		keyIntentReqIntro:                                     "Fill in the details. You will review them before anything is sent.",
		keyIntentReqClose:                                     "Close",
		keyIntentReqReview:                                    "Review request",
		keyIntentReqSend:                                      "Send request",
		keyIntentReqEdit:                                      "Edit",
		keyIntentReqSending:                                   "Sending...",
		keyIntentReqChoose:                                    "Choose...",
		keyIntentReqRequired:                                  "required",
		keyIntentReqOptional:                                  "optional",
		keyIntentReqReviewTitle:                               "Check your request",
		keyIntentReqReviewNote:                                "This sends a request for review to {name}. Sending it does not approve or start the leave.",
		keyIntentReqSent:                                      "Request sent for review",
		keyIntentReqSentNote:                                  "It is not approved yet. You can follow it from the request.",
		keyIntentReqOpen:                                      "Open the request",
		keyIntentReqSummary:                                   "Some details need your attention.",
		keyIntentReqNone:                                      "Not given",
		keyIntentReqFailed:                                    "The request could not be sent. Check it and try again.",
		keyIntentReqDenied:                                    "You cannot send this request here.",
		keyIntentReqGone:                                      "Requests are not available here right now.",
		keyIntentReqFieldPrefix + "employee":                  "Employee",
		keyIntentReqFieldPrefix + "starts":                    "First day",
		keyIntentReqFieldPrefix + "ends":                      "Last day",
		keyIntentReqFieldPrefix + "approver":                  "Approver",
		keyIntentReqFieldPrefix + "reason":                    "Reason",
		keyIntentReqErrPrefix + chat.FormErrRequired:          "This is required.",
		keyIntentReqErrPrefix + chat.FormErrTooLong:           "This is too long.",
		keyIntentReqErrPrefix + chat.FormErrBadText:           "This has characters that are not allowed.",
		keyIntentReqErrPrefix + chat.FormErrBadDate:           "Enter a real date.",
		keyIntentReqErrPrefix + chat.FormErrBadOption:         "Choose one of the options.",
		keyIntentReqErrPrefix + chat.FormErrUnknownUser:       "Choose someone who is in this conversation.",
		keyIntentReqErrPrefix + chat.FormErrUnknownField:      "This is not accepted.",
		keyIntentReqErrPrefix + chat.IntentRequestErrBadRange: "The last day cannot be before the first day.",
		keyIntentReqWhyPrefix + "not_installed":               "Requests like this are not set up in this conversation.",
		keyIntentReqWhyPrefix + "paused":                      "The assistant that takes these requests is paused or removed, so nothing can be sent here.",
		keyIntentReqWhyPrefix + "not_granted":                 "The assistant that takes these requests has not been given permission to do so.",
		keyIntentReqWhyPrefix + "not_allowed":                 "You do not have permission to send this kind of request.",
	},
	"de-DE": {
		keyIntentReqCommand:                                   "In dieser Unterhaltung Urlaub beantragen",
		keyIntentReqHeading:                                   "Urlaub beantragen",
		keyIntentReqIntro:                                     "Geben Sie die Angaben ein. Sie prüfen sie, bevor etwas gesendet wird.",
		keyIntentReqClose:                                     "Schließen",
		keyIntentReqReview:                                    "Antrag prüfen",
		keyIntentReqSend:                                      "Antrag senden",
		keyIntentReqEdit:                                      "Bearbeiten",
		keyIntentReqSending:                                   "Wird gesendet...",
		keyIntentReqChoose:                                    "Auswählen...",
		keyIntentReqRequired:                                  "erforderlich",
		keyIntentReqOptional:                                  "freiwillig",
		keyIntentReqReviewTitle:                               "Antrag prüfen",
		keyIntentReqReviewNote:                                "Damit wird ein Antrag zur Prüfung an {name} gesendet. Das Senden genehmigt den Urlaub nicht und startet ihn nicht.",
		keyIntentReqSent:                                      "Antrag zur Prüfung gesendet",
		keyIntentReqSentNote:                                  "Er ist noch nicht genehmigt. Sie können ihn über den Antrag verfolgen.",
		keyIntentReqOpen:                                      "Antrag öffnen",
		keyIntentReqSummary:                                   "Einige Angaben brauchen Ihre Aufmerksamkeit.",
		keyIntentReqNone:                                      "Nicht angegeben",
		keyIntentReqFailed:                                    "Der Antrag konnte nicht gesendet werden. Prüfen Sie ihn und versuchen Sie es erneut.",
		keyIntentReqDenied:                                    "Sie können diesen Antrag hier nicht senden.",
		keyIntentReqGone:                                      "Anträge sind hier gerade nicht verfügbar.",
		keyIntentReqFieldPrefix + "employee":                  "Mitarbeiter:in",
		keyIntentReqFieldPrefix + "starts":                    "Erster Tag",
		keyIntentReqFieldPrefix + "ends":                      "Letzter Tag",
		keyIntentReqFieldPrefix + "approver":                  "Genehmigende Person",
		keyIntentReqFieldPrefix + "reason":                    "Grund",
		keyIntentReqErrPrefix + chat.FormErrRequired:          "Dies ist erforderlich.",
		keyIntentReqErrPrefix + chat.FormErrTooLong:           "Dies ist zu lang.",
		keyIntentReqErrPrefix + chat.FormErrBadText:           "Dies enthält nicht erlaubte Zeichen.",
		keyIntentReqErrPrefix + chat.FormErrBadDate:           "Geben Sie ein gültiges Datum ein.",
		keyIntentReqErrPrefix + chat.FormErrBadOption:         "Wählen Sie eine der Optionen.",
		keyIntentReqErrPrefix + chat.FormErrUnknownUser:       "Wählen Sie jemanden aus dieser Unterhaltung.",
		keyIntentReqErrPrefix + chat.FormErrUnknownField:      "Dies wird nicht akzeptiert.",
		keyIntentReqErrPrefix + chat.IntentRequestErrBadRange: "Der letzte Tag darf nicht vor dem ersten Tag liegen.",
		keyIntentReqWhyPrefix + "not_installed":               "Solche Anträge sind in dieser Unterhaltung nicht eingerichtet.",
		keyIntentReqWhyPrefix + "paused":                      "Der Assistent, der diese Anträge annimmt, ist pausiert oder entfernt, daher kann hier nichts gesendet werden.",
		keyIntentReqWhyPrefix + "not_granted":                 "Der Assistent, der diese Anträge annimmt, hat dafür keine Berechtigung erhalten.",
		keyIntentReqWhyPrefix + "not_allowed":                 "Sie haben keine Berechtigung, diese Art von Antrag zu senden.",
	},
	"ar": {
		keyIntentReqCommand:                                   "طلب إجازة في هذه المحادثة",
		keyIntentReqHeading:                                   "طلب إجازة",
		keyIntentReqIntro:                                     "املأ التفاصيل. ستراجعها قبل إرسال أي شيء.",
		keyIntentReqClose:                                     "إغلاق",
		keyIntentReqReview:                                    "مراجعة الطلب",
		keyIntentReqSend:                                      "إرسال الطلب",
		keyIntentReqEdit:                                      "تعديل",
		keyIntentReqSending:                                   "جارٍ الإرسال...",
		keyIntentReqChoose:                                    "اختر...",
		keyIntentReqRequired:                                  "مطلوب",
		keyIntentReqOptional:                                  "اختياري",
		keyIntentReqReviewTitle:                               "راجع طلبك",
		keyIntentReqReviewNote:                                "يرسل هذا طلبًا للمراجعة إلى {name}. إرساله لا يوافق على الإجازة ولا يبدؤها.",
		keyIntentReqSent:                                      "أُرسل الطلب للمراجعة",
		keyIntentReqSentNote:                                  "لم تتم الموافقة عليه بعد. يمكنك متابعته من الطلب.",
		keyIntentReqOpen:                                      "فتح الطلب",
		keyIntentReqSummary:                                   "بعض التفاصيل تحتاج إلى انتباهك.",
		keyIntentReqNone:                                      "غير محدد",
		keyIntentReqFailed:                                    "تعذّر إرسال الطلب. راجعه وحاول مرة أخرى.",
		keyIntentReqDenied:                                    "لا يمكنك إرسال هذا الطلب هنا.",
		keyIntentReqGone:                                      "الطلبات غير متاحة هنا الآن.",
		keyIntentReqFieldPrefix + "employee":                  "الموظف",
		keyIntentReqFieldPrefix + "starts":                    "أول يوم",
		keyIntentReqFieldPrefix + "ends":                      "آخر يوم",
		keyIntentReqFieldPrefix + "approver":                  "الموافق",
		keyIntentReqFieldPrefix + "reason":                    "السبب",
		keyIntentReqErrPrefix + chat.FormErrRequired:          "هذا الحقل مطلوب.",
		keyIntentReqErrPrefix + chat.FormErrTooLong:           "هذا النص طويل جدًا.",
		keyIntentReqErrPrefix + chat.FormErrBadText:           "يحتوي هذا على أحرف غير مسموح بها.",
		keyIntentReqErrPrefix + chat.FormErrBadDate:           "أدخل تاريخًا صحيحًا.",
		keyIntentReqErrPrefix + chat.FormErrBadOption:         "اختر أحد الخيارات.",
		keyIntentReqErrPrefix + chat.FormErrUnknownUser:       "اختر شخصًا من هذه المحادثة.",
		keyIntentReqErrPrefix + chat.FormErrUnknownField:      "هذا غير مقبول.",
		keyIntentReqErrPrefix + chat.IntentRequestErrBadRange: "لا يمكن أن يسبق آخر يوم أول يوم.",
		keyIntentReqWhyPrefix + "not_installed":               "هذا النوع من الطلبات غير مُعدّ في هذه المحادثة.",
		keyIntentReqWhyPrefix + "paused":                      "المساعد الذي يستقبل هذه الطلبات متوقف أو محذوف، لذا لا يمكن إرسال شيء هنا.",
		keyIntentReqWhyPrefix + "not_granted":                 "لم يُمنح المساعد الذي يستقبل هذه الطلبات إذنًا بذلك.",
		keyIntentReqWhyPrefix + "not_allowed":                 "ليس لديك إذن بإرسال هذا النوع من الطلبات.",
	},
}

// IntentRequestText is the copy of the typed requests in a locale.
func IntentRequestText(locale, key string) string {
	return chatbug039Text(key, issue3793Copy[chatEmojiLocale(locale)][key], issue3793Copy["en-US"][key])
}

// intentRequestCommandText is the one line /leave shows in the command list.
func intentRequestCommandText(locale string) string {
	return IntentRequestText(locale, keyIntentReqCommand)
}

// intentRequestShared is the shared registry with every command this page can
// run: the product's own, "/ask", and the typed requests.
func intentRequestShared() chat.Chatcmd001Registry {
	return chat.Chatcmd001WithIntentRequests(chat.Chatcmd001WithAsk(chat.Chatcmd001Defaults()))
}

// intentRequestAvailable says whether /leave is in the command list here: the
// page reaches the requests, the viewer is signed in, the room is this
// company's, and the server did not say requests are not set up here at all.
// A request whose agent is paused, revoked or not permitted stays listed, so
// choosing it explains why nothing can be sent instead of the command
// vanishing.
func intentRequestAvailable(command string) func(Model) bool {
	return func(m Model) bool {
		if m.IntentRequests.Open == nil || m.CurrentUser == "" || m.SelectedID == "" || m.IntentRequests.ConversationID != m.SelectedID {
			return false
		}
		c := m.selected()
		if c.Agent || (c.HostTenantID != "" && c.HostTenantID != m.CurrentTenantID) {
			return false
		}
		state := m.IntentRequests.Commands[command]
		return state.Loaded && (state.Available || state.Reason != "not_installed")
	}
}

// intentRequestRun is what the command does: it consumes the line and opens
// the panel, which shows either the form or why requests are unavailable.
func intentRequestRun(command string) func(composerCommandRuntime, string) {
	return func(rt composerCommandRuntime, _ string) {
		if !intentRequestAvailable(command)(rt.Model) {
			return
		}
		if rt.ClearDraft != nil {
			rt.ClearDraft()
		}
		rt.Model.IntentRequests.Open(command)
	}
}

func intentRequestField(m Model, view IntentRequestView, f chat.FormField) ui.Node {
	id := "chat-request-" + f.ID
	code := view.Errors[f.ID]
	busy := view.Phase == IntentRequestSending
	aria := map[string]string{}
	if f.Required {
		aria["required"] = "true"
	}
	if code != "" {
		aria["invalid"] = "true"
		aria["describedby"] = id + "-error"
	}
	label := laneText(m, issue3793Copy, keyIntentReqFieldPrefix+f.ID)
	marker := keyIntentReqOptional
	if f.Required {
		marker = keyIntentReqRequired
	}
	value := view.Values[f.ID]
	var control ui.Node
	switch f.Type {
	case chat.FormFieldDate:
		control = html.Input(html.Props{Key: id, ID: id, Name: f.ID, Type: "date", Class: "chat-input", Value: value, Required: f.Required, Disabled: busy, Aria: aria})
	case chat.FormFieldPerson:
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: value == "", Text: laneText(m, issue3793Copy, keyIntentReqChoose)})}
		for _, member := range m.Members {
			if member.ID == "" || strings.TrimSpace(member.Name) == "" {
				continue
			}
			options = append(options, html.Option(html.Props{Raw: map[string]any{"value": member.ID}, Selected: member.ID == value, Text: member.Name}))
		}
		control = html.Select(html.Props{Key: id, ID: id, Name: f.ID, Class: "chat-input", Value: value, Required: f.Required, Disabled: busy, Aria: aria}, options...)
	default:
		max := f.MaxLength
		if max == 0 {
			max = 500
		}
		control = html.Textarea(html.Props{Key: id, ID: id, Name: f.ID, Class: "chat-input", Rows: 3, MaxLength: max, Dir: "auto", Value: value, Required: f.Required, Disabled: busy, Aria: aria})
	}
	children := []ui.Node{
		html.Label(html.Props{For: id, Class: "chat-request-label"},
			html.Span(html.Props{Dir: "auto", Text: label}),
			html.Span(html.Props{Class: "chat-request-marker", Text: " (" + laneText(m, issue3793Copy, marker) + ")"})),
		control,
	}
	if code != "" {
		key := keyIntentReqErrPrefix + code
		if _, known := issue3793Copy["en-US"][key]; !known {
			key = keyIntentReqErrPrefix + chat.FormErrUnknownField
		}
		children = append(children, html.P(html.Props{ID: id + "-error", Class: "chat-request-error", Text: laneText(m, issue3793Copy, key)}))
	}
	return html.Div(html.Props{Class: "chat-request-field", Key: id + "-field"}, children...)
}

// intentRequestShow is a typed value in words: a person by name, a date as the
// day.
func intentRequestShow(m Model, f chat.FormField, value string) string {
	if strings.TrimSpace(value) == "" {
		return laneText(m, issue3793Copy, keyIntentReqNone)
	}
	switch f.Type {
	case chat.FormFieldPerson:
		if name := chatKnownName(m, value); name != "" {
			return name
		}
		return laneText(m, issue3793Copy, keyIntentReqNone)
	case chat.FormFieldDate:
		if at, err := time.Parse("2006-01-02", value); err == nil {
			return formatDay(m.Locale, at, true)
		}
	}
	return value
}

func intentRequestNotice(m Model, view IntentRequestView) ui.Node {
	key := ""
	switch view.Notice {
	case "failed":
		key = keyIntentReqFailed
	case "denied":
		key = keyIntentReqDenied
	case "unavailable":
		key = keyIntentReqGone
	}
	if key == "" {
		return nil
	}
	return html.P(html.Props{ID: "chat-request-notice", Class: "chat-request-error", Role: "alert", TabIndex: -1, Text: laneText(m, issue3793Copy, key)})
}

// intentRequestPanel is the open request above the composer, or nil.
func intentRequestPanel(m Model) ui.Node {
	view := m.IntentRequests.View
	command, found := chat.IntentRequestLookup(view.Command)
	if !view.Open || !found || view.ConversationID != m.SelectedID || m.SelectedID == "" {
		return nil
	}
	state := m.IntentRequests.Commands[command.Name]
	heading := laneText(m, issue3793Copy, keyIntentReqHeading)
	closeButton := html.Button(html.Props{Type: "button", Class: "icon-button", Title: laneText(m, issue3793Copy, keyIntentReqClose), Data: map[string]string{"request-action": "close"},
		Aria: map[string]string{"label": laneText(m, issue3793Copy, keyIntentReqClose)}}, icon("close"))
	nodes := []ui.Node{html.Div(html.Props{Class: "chat-request-top"}, html.H2(html.Props{ID: "chat-request-heading", Class: "chat-request-heading", TabIndex: -1, Text: heading}), closeButton)}

	busy := view.Phase == IntentRequestSending
	switch {
	case view.Phase != IntentRequestSent && state.Loaded && !state.Available:
		key := keyIntentReqWhyPrefix + state.Reason
		if _, known := issue3793Copy["en-US"][key]; !known {
			key = keyIntentReqGone
		}
		nodes = append(nodes, html.P(html.Props{Class: "chat-request-status", Role: "status", Text: laneText(m, issue3793Copy, key)}))
	case view.Phase == IntentRequestSent:
		nodes = append(nodes,
			html.P(html.Props{Class: "chat-request-status chat-request-done", Role: "status", Text: laneText(m, issue3793Copy, keyIntentReqSent)}),
			html.P(html.Props{Class: "chat-request-hint", Text: laneText(m, issue3793Copy, keyIntentReqSentNote)}))
		if view.Href != "" {
			nodes = append(nodes, html.A(html.Props{Class: "button secondary small", Href: view.Href, Text: laneText(m, issue3793Copy, keyIntentReqOpen)}))
		}
	case view.Phase == IntentRequestReview || busy:
		rows := []ui.Node{}
		for _, f := range command.Fields {
			rows = append(rows, html.Div(html.Props{Class: "chat-request-answer"},
				html.Span(html.Props{Class: "chat-request-question", Dir: "auto", Text: laneText(m, issue3793Copy, keyIntentReqFieldPrefix+f.ID)}),
				html.Span(html.Props{Class: "chat-request-value", Dir: "auto", Text: intentRequestShow(m, f, view.Values[f.ID])})))
		}
		sendLabel := laneText(m, issue3793Copy, keyIntentReqSend)
		if busy {
			sendLabel = laneText(m, issue3793Copy, keyIntentReqSending)
		}
		nodes = append(nodes,
			html.Strong(html.Props{Class: "chat-request-subtitle", Text: laneText(m, issue3793Copy, keyIntentReqReviewTitle)}),
			html.Div(html.Props{Class: "chat-request-answers", Role: "group", Aria: map[string]string{"label": laneText(m, issue3793Copy, keyIntentReqReviewTitle)}}, rows...),
			html.P(html.Props{Class: "chat-request-hint", Text: laneTextf(m, issue3793Copy, keyIntentReqReviewNote, map[string]string{"name": view.Via})}))
		if notice := intentRequestNotice(m, view); notice != nil {
			nodes = append(nodes, notice)
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chat-request-actions"},
			html.Button(html.Props{Type: "button", Class: "button primary small chat-request-send", Disabled: busy, Data: map[string]string{"request-action": "send"}, Aria: map[string]string{"busy": boolString(busy)}, Text: sendLabel}),
			html.Button(html.Props{Type: "button", Class: "button secondary small", Disabled: busy, Data: map[string]string{"request-action": "edit"}, Text: laneText(m, issue3793Copy, keyIntentReqEdit)})))
	default:
		fields := []ui.Node{html.P(html.Props{Class: "chat-request-hint", Text: laneText(m, issue3793Copy, keyIntentReqIntro)})}
		if len(view.Errors) > 0 {
			fields = append(fields, html.P(html.Props{Class: "chat-request-error", Role: "alert", Text: laneText(m, issue3793Copy, keyIntentReqSummary)}))
		}
		for _, f := range command.Fields {
			fields = append(fields, intentRequestField(m, view, f))
		}
		if notice := intentRequestNotice(m, view); notice != nil {
			fields = append(fields, notice)
		}
		fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button primary small chat-request-submit", Text: laneText(m, issue3793Copy, keyIntentReqReview)}))
		nodes = append(nodes, html.Form(html.Props{Key: "chat-request-form", Class: "chat-request-form", Raw: map[string]any{"data-intent-request": command.Name, "noValidate": true}}, fields...))
	}
	return html.Section(html.Props{ID: "chat-request-panel", Class: "chat-request-panel", Dir: direction(m.Locale), Data: map[string]string{"request-phase": view.Phase},
		Aria: map[string]string{"labelledby": "chat-request-heading", "busy": boolString(busy)}}, nodes...)
}

// Issue3793Styles lays the panel out for a phone first: fields stack and fill
// the panel, every control is a 44px target, the panel wraps rather than
// scrolls at 320px, and logical properties keep it right in a right-to-left
// language.
const Issue3793Styles = `
.chat-request-panel{display:flex;flex-direction:column;align-items:stretch;gap:8px;box-sizing:border-box;margin:8px 16px;padding:12px 14px;max-inline-size:36rem;min-inline-size:0;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}
.chat-request-top{display:flex;align-items:center;justify-content:space-between;gap:8px}
.chat-request-heading{margin:0;font-size:.9375rem}
.chat-request-hint,.chat-request-status,.chat-request-subtitle{margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted)}
.chat-request-done{color:var(--hcm-color-text);font-weight:600}
.chat-request-form{display:flex;flex-direction:column;gap:10px;margin:0}
.chat-request-field{display:flex;flex-direction:column;gap:4px;min-inline-size:0}
.chat-request-label{font-size:.8125rem;font-weight:600}
.chat-request-marker{font-weight:400;color:var(--hcm-color-text-muted)}
.chat-request-panel .chat-input{inline-size:100%;box-sizing:border-box;min-block-size:44px;font:inherit}
.chat-request-panel textarea.chat-input{resize:vertical}
.chat-request-error{margin:0;font-size:.8125rem;color:var(--hcm-color-danger,#b3261e)}
.chat-request-panel [aria-invalid="true"]{border-color:var(--hcm-color-danger,#b3261e)}
.chat-request-submit,.chat-request-send,.chat-request-actions .button,.chat-request-panel a.button{min-block-size:44px;min-inline-size:44px;align-self:flex-start}
.chat-request-panel .icon-button{min-block-size:44px;min-inline-size:44px}
.chat-request-answers{display:flex;flex-direction:column;gap:6px;margin:0}
.chat-request-answer{display:flex;flex-direction:column;gap:2px}
.chat-request-question{font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-request-value{font-size:.875rem}
.chat-request-actions{display:flex;flex-wrap:wrap;gap:8px}
@media (prefers-reduced-motion:reduce){.chat-request-panel{transition:none}}
`
