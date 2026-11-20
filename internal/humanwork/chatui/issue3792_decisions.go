package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3792: /decide. A conversation settles something; a person records it
// as a decision with its words, a reason, who recorded it and when, and the
// message it came from. The Decisions list of a conversation shows the ones in
// force and, folded away, the ones a newer decision replaced. Every decision is
// labelled informal: it is a record of what people agreed in a conversation and
// never a company policy.

// DecisionsState is the decisions feature as the page offers it. Enabled is set
// once the server has said it records decisions for this person; the callbacks
// open the dialog (the browser client draws it over the page).
type DecisionsState struct {
	Enabled bool
	// Record opens the dialog with the decision's words already typed in (the
	// text after /decide). parent is the open thread's message, "" otherwise.
	Record func(text, parent string)
	// OpenList opens the dialog on the conversation's decisions.
	OpenList func()
	// Mark opens the dialog for an existing message: its words are filled in as
	// the decision and the record points at that message instead of posting a new
	// line. nil leaves the "Mark as decision" row out of the message menu.
	Mark func(postID, text string)
	// Count is how many decisions are in force in the open conversation, once read.
	Count int
}

// DecisionItem is one decision as the dialog shows it.
type DecisionItem struct {
	ID, Text, Rationale, Owner   string
	DecidedAt                    time.Time
	SourcePostID                 string
	SourceSequence               uint64
	SupersedesID, SupersededByID string
	CanReplace                   bool
}

// DecisionsDialogView is everything the dialog shows.
type DecisionsDialogView struct {
	Locale, Channel string
	// State is "loading", "ready" or "error"; Error is a code from
	// DecisionsErrorText.
	State, Error string
	Items        []DecisionItem
	// CanRecord says the person may record a decision here.
	CanRecord bool
	// Text and Rationale are what the person has typed; ReplacingID names the
	// decision the new one will replace.
	Text, Rationale, ReplacingID string
	Busy                         bool
	// Notice is a short confirmation ("Decision recorded") announced politely.
	Notice string
	// FromMessage says the decision points at a message the person chose, so
	// recording it posts nothing new.
	FromMessage bool
}

const (
	dcTitle        = "title"
	dcInformal     = "informal"
	dcBadge        = "badge"
	dcLoading      = "loading"
	dcEmpty        = "empty"
	dcFieldText    = "field_text"
	dcFieldWhy     = "field_why"
	dcHintText     = "hint_text"
	dcSubmit       = "submit"
	dcSubmitSwap   = "submit_swap"
	dcBusy         = "busy"
	dcClose        = "close"
	dcWhy          = "why"
	dcBy           = "by"
	dcOn           = "recorded_on"
	dcGoto         = "goto"
	dcReplace      = "replace"
	dcReplacing    = "replacing"
	dcKeepBoth     = "keep_both"
	dcReplaces     = "replaces"
	dcReplacedBy   = "replaced_by"
	dcEarlier      = "earlier"
	dcReadOnly     = "read_only"
	dcDone         = "done"
	dcDetails      = "details"
	dcDetailsHint  = "details_hint"
	dcCommand      = "command"
	dcListLabel    = "list_label"
	dcErrInvalid   = "err_invalid"
	dcErrNotFound  = "err_not_found"
	dcErrDenied    = "err_denied"
	dcErrConflict  = "err_conflict"
	dcErrText      = "err_text"
	dcErrUnavail   = "err_unavailable"
	dcYou          = "owner_you"
	dcUnknownOwner = "unknown_owner"
	dcPostLabel    = "post_label"
	dcMark         = "mark"
	dcFromMessage  = "from_message"
)

var decisionsCopy = map[string]map[string]string{
	"en-US": {
		dcTitle: "Decisions", dcInformal: "A decision records what this conversation settled. It is informal and does not change company policy.",
		dcBadge: "Informal", dcLoading: "Loading decisions", dcEmpty: "No decisions recorded here yet. Type /decide and the decision in the message box, or use the box below.",
		dcFieldText: "Decision", dcFieldWhy: "Why (optional)", dcHintText: "One or two sentences. Up to 500 characters.",
		dcSubmit: "Record decision", dcSubmitSwap: "Record and replace", dcBusy: "Recording the decision", dcClose: "Close",
		dcWhy: "Why", dcBy: "Recorded by", dcOn: "on", dcGoto: "Go to message", dcReplace: "Replace",
		dcReplacing: "This will replace", dcKeepBoth: "Don't replace", dcReplaces: "Replaces an earlier decision", dcReplacedBy: "Replaced by a newer decision",
		dcEarlier: "Replaced decisions", dcReadOnly: "You can read decisions here but not record new ones.", dcDone: "Decision recorded",
		dcDetails: "Decisions", dcDetailsHint: "What this conversation settled", dcCommand: "Record a decision the conversation reached", dcListLabel: "Decisions in force",
		dcErrInvalid:  "Check the decision and the reason, then try again. The decision needs words and fits in 500 characters.",
		dcErrNotFound: "That decision or message is no longer available to you.",
		dcErrDenied:   "You can't record decisions in this conversation right now.",
		dcErrConflict: "That decision was already replaced. Close this and open Decisions again to see the latest.",
		dcErrText:     "Write the decision first.",
		dcErrUnavail:  "The decision could not be saved right now. Try again; the same request never records two decisions.",
		dcYou:         "you", dcUnknownOwner: "a former member", dcPostLabel: "Decision (informal)",
		dcMark: "Mark as decision", dcFromMessage: "This decision points to the message you chose. Nothing new is posted in the conversation.",
	},
	"de-DE": {
		dcTitle: "Entscheidungen", dcInformal: "Eine Entscheidung hält fest, was diese Unterhaltung geklärt hat. Sie ist informell und ändert keine Unternehmensrichtlinie.",
		dcBadge: "Informell", dcLoading: "Entscheidungen werden geladen", dcEmpty: "Hier ist noch keine Entscheidung festgehalten. Geben Sie /decide und die Entscheidung im Nachrichtenfeld ein oder nutzen Sie das Feld unten.",
		dcFieldText: "Entscheidung", dcFieldWhy: "Begründung (optional)", dcHintText: "Ein bis zwei Sätze, höchstens 500 Zeichen.",
		dcSubmit: "Entscheidung festhalten", dcSubmitSwap: "Festhalten und ersetzen", dcBusy: "Entscheidung wird festgehalten", dcClose: "Schließen",
		dcWhy: "Begründung", dcBy: "Festgehalten von", dcOn: "am", dcGoto: "Zur Nachricht", dcReplace: "Ersetzen",
		dcReplacing: "Ersetzt wird", dcKeepBoth: "Nicht ersetzen", dcReplaces: "Ersetzt eine frühere Entscheidung", dcReplacedBy: "Durch eine neuere Entscheidung ersetzt",
		dcEarlier: "Ersetzte Entscheidungen", dcReadOnly: "Sie können Entscheidungen hier lesen, aber keine neuen festhalten.", dcDone: "Entscheidung festgehalten",
		dcDetails: "Entscheidungen", dcDetailsHint: "Was diese Unterhaltung geklärt hat", dcCommand: "Eine in der Unterhaltung getroffene Entscheidung festhalten", dcListLabel: "Geltende Entscheidungen",
		dcErrInvalid:  "Prüfen Sie Entscheidung und Begründung und versuchen Sie es erneut. Die Entscheidung braucht Text und darf höchstens 500 Zeichen lang sein.",
		dcErrNotFound: "Diese Entscheidung oder Nachricht ist für Sie nicht mehr verfügbar.",
		dcErrDenied:   "Sie können in dieser Unterhaltung gerade keine Entscheidungen festhalten.",
		dcErrConflict: "Diese Entscheidung wurde bereits ersetzt. Schließen Sie das Fenster und öffnen Sie Entscheidungen erneut, um den neuesten Stand zu sehen.",
		dcErrText:     "Schreiben Sie zuerst die Entscheidung.",
		dcErrUnavail:  "Die Entscheidung konnte gerade nicht gespeichert werden. Versuchen Sie es erneut; dieselbe Anfrage hält nie zwei Entscheidungen fest.",
		dcYou:         "Sie", dcUnknownOwner: "ein ehemaliges Mitglied", dcPostLabel: "Entscheidung (informell)",
		dcMark: "Als Entscheidung markieren", dcFromMessage: "Diese Entscheidung verweist auf die gewählte Nachricht. In der Unterhaltung wird nichts Neues gepostet.",
	},
	"ar": {
		dcTitle: "القرارات", dcInformal: "يسجّل القرار ما اتفقت عليه هذه المحادثة. هو قرار غير رسمي ولا يغيّر سياسة الشركة.",
		dcBadge: "غير رسمي", dcLoading: "جارٍ تحميل القرارات", dcEmpty: "لم يُسجَّل أي قرار هنا بعد. اكتب /decide ثم القرار في مربع الرسالة، أو استخدم المربع أدناه.",
		dcFieldText: "القرار", dcFieldWhy: "السبب (اختياري)", dcHintText: "جملة أو جملتان، بحد أقصى 500 حرف.",
		dcSubmit: "تسجيل القرار", dcSubmitSwap: "تسجيل واستبدال", dcBusy: "جارٍ تسجيل القرار", dcClose: "إغلاق",
		dcWhy: "السبب", dcBy: "سجّله", dcOn: "في", dcGoto: "الانتقال إلى الرسالة", dcReplace: "استبدال",
		dcReplacing: "سيستبدل هذا", dcKeepBoth: "عدم الاستبدال", dcReplaces: "يستبدل قرارًا سابقًا", dcReplacedBy: "استُبدل بقرار أحدث",
		dcEarlier: "القرارات المستبدلة", dcReadOnly: "يمكنك قراءة القرارات هنا ولكن لا يمكنك تسجيل قرارات جديدة.", dcDone: "تم تسجيل القرار",
		dcDetails: "القرارات", dcDetailsHint: "ما اتفقت عليه هذه المحادثة", dcCommand: "تسجيل قرار توصلت إليه المحادثة", dcListLabel: "القرارات السارية",
		dcErrInvalid:  "تحقق من القرار والسبب ثم حاول مرة أخرى. يجب أن يحتوي القرار على نص وألا يتجاوز 500 حرف.",
		dcErrNotFound: "لم يعد هذا القرار أو هذه الرسالة متاحًا لك.",
		dcErrDenied:   "لا يمكنك تسجيل قرارات في هذه المحادثة الآن.",
		dcErrConflict: "تم استبدال هذا القرار بالفعل. أغلق النافذة وافتح القرارات مرة أخرى لرؤية الأحدث.",
		dcErrText:     "اكتب القرار أولًا.",
		dcErrUnavail:  "تعذر حفظ القرار الآن. حاول مرة أخرى؛ لن يسجّل الطلب نفسه قرارين أبدًا.",
		dcYou:         "أنت", dcUnknownOwner: "عضو سابق", dcPostLabel: "قرار (غير رسمي)",
		dcMark: "تحديد كقرار", dcFromMessage: "يشير هذا القرار إلى الرسالة التي اخترتها. لن يُنشر أي شيء جديد في المحادثة.",
	},
}

// DecisionsText is the copy of the decision surfaces in the viewer's language.
func DecisionsText(locale, key string) string {
	return chatbug039Text(key, decisionsCopy[chatEmojiLocale(locale)][key], decisionsCopy["en-US"][key])
}

// DecisionsCommandText is the one-line description of /decide in the command list.
func DecisionsCommandText(locale string) string { return DecisionsText(locale, dcCommand) }

// DecisionsDoneText announces a recorded decision to a screen reader.
func DecisionsDoneText(locale string) string { return DecisionsText(locale, dcDone) }

// DecisionsOwnerLabels are the words used for who recorded a decision: "you",
// and the name used when the person has left the conversation.
func DecisionsOwnerLabels(locale string) (you, unknown string) {
	return DecisionsText(locale, dcYou), DecisionsText(locale, dcUnknownOwner)
}

// DecisionsErrorText names a refusal code in plain words.
func DecisionsErrorText(locale, code string) string {
	switch code {
	case "":
		return ""
	case "invalid_argument":
		return DecisionsText(locale, dcErrInvalid)
	case "not_found":
		return DecisionsText(locale, dcErrNotFound)
	case "permission_denied":
		return DecisionsText(locale, dcErrDenied)
	case "conflict":
		return DecisionsText(locale, dcErrConflict)
	case "text":
		return DecisionsText(locale, dcErrText)
	default:
		return DecisionsText(locale, dcErrUnavail)
	}
}

// decisionsAvailable says /decide and the Decisions list can be used here: the
// server records decisions and this conversation is one of ours.
func decisionsAvailable(m Model) bool {
	if !m.Decisions.Enabled || m.Decisions.Record == nil || m.SelectedID == "" || m.selected().Agent {
		return false
	}
	// A conversation hosted by another company is served by that company.
	if host := m.selected().HostTenantID; host != "" && host != m.CurrentTenantID {
		return false
	}
	return true
}

// decisionsRun is what the registry runs for /decide: it opens the dialog with
// the typed words filled in. The dialog is the preview; nothing is recorded
// until Record decision is pressed.
func decisionsRun(rt composerCommandRuntime, args string) {
	if rt.Model.Decisions.Record == nil {
		if rt.Notice != nil {
			rt.Notice(DecisionsText(rt.Model.Locale, dcErrUnavail))
		}
		return
	}
	parent := ""
	if rt.Target == "thread-composer" && rt.Model.ThreadParent != nil {
		parent = rt.Model.ThreadParent.ID
	}
	if rt.ClearDraft != nil {
		rt.ClearDraft()
	}
	rt.Model.Decisions.Record(strings.TrimSpace(args), parent)
}

// decisionsDetailsSection is the Decisions row of Conversation details. It
// opens the dialog; the browser client listens for data-decisions-open.
func decisionsDetailsSection(m Model) ui.Node {
	if !decisionsAvailable(m) || m.Decisions.OpenList == nil {
		return nil
	}
	label := DecisionsText(m.Locale, dcDetails)
	name := label
	if m.Decisions.Count > 0 {
		name = countedLabel(m, label, m.Decisions.Count)
	}
	return html.Section(html.Props{ID: "chat-details-decisions", Class: "details-section details-decisions", Aria: map[string]string{"label": label}},
		html.H3(html.Props{Text: label}),
		html.P(html.Props{Class: "field-hint", Text: DecisionsText(m.Locale, dcDetailsHint)}),
		html.Button(html.Props{Type: "button", Class: "button secondary details-decisions-open", Data: map[string]string{"decisions-open": "list"},
			Aria: map[string]string{"haspopup": "dialog"}}, icon("checklist"), html.Span(html.Props{Text: name})))
}

func decisionsFromNote(v DecisionsDialogView) ui.Node {
	if !v.FromMessage || !v.CanRecord {
		return nil
	}
	return html.P(html.Props{Class: "decisions-notice", Text: DecisionsText(v.Locale, dcFromMessage)})
}

// decisionsMenuItem is the Mark as decision row of a message's menu. It is
// offered only where the server records decisions and the message has words.
func decisionsMenuItem(m Model, msg Message) []ui.Node {
	if !decisionsAvailable(m) || m.Decisions.Mark == nil || msg.ID == "" || strings.TrimSpace(msg.Body) == "" || chatcmd002IsCard(msg.Body) {
		return nil
	}
	return []ui.Node{chatux022Item("menu-item decisions-menu-item", "decision-mark", msg.ID, "checklist", DecisionsText(m.Locale, dcMark), false)}
}

// decisionsMark runs when the Mark as decision row is chosen.
func (m Model) decisionsMark(postID string) {
	if m.Decisions.Mark == nil || postID == "" {
		return
	}
	var found *Message
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for i := range list {
			if list[i].ID == postID {
				found = &list[i]
			}
		}
	}
	if found == nil && m.ThreadParent != nil && m.ThreadParent.ID == postID {
		found = m.ThreadParent
	}
	if found == nil {
		return
	}
	// The dispatcher closes the menu after every action.
	m.Decisions.Mark(postID, DecisionsMarkText(found.Body))
}

// DecisionsMarkText folds a message into the one line a decision may be: its
// words on one line, cut at the decision limit.
func DecisionsMarkText(body string) string {
	text := strings.Join(strings.Fields(body), " ")
	if runes := []rune(text); len(runes) > 500 {
		text = strings.TrimSpace(string(runes[:499])) + "…"
	}
	return text
}

func decisionsDate(locale string, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return formatDay(locale, at.Local(), true)
}

func decisionsItem(v DecisionsDialogView, item DecisionItem, replaced bool) ui.Node {
	t := func(key string) string { return DecisionsText(v.Locale, key) }
	owner := item.Owner
	if owner == "" {
		owner = t(dcUnknownOwner)
	}
	meta := t(dcBy) + " " + owner
	if date := decisionsDate(v.Locale, item.DecidedAt); date != "" {
		meta += " " + t(dcOn) + " " + date
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "decisions-badges"},
			html.Span(html.Props{Class: "decisions-badge", Text: t(dcBadge)}),
			decisionsMaybeBadge(item.SupersedesID != "", t(dcReplaces)),
			decisionsMaybeBadge(replaced, t(dcReplacedBy))),
		html.P(html.Props{Class: "decisions-text", Dir: "auto", Text: item.Text}),
	}
	if item.Rationale != "" {
		children = append(children, html.P(html.Props{Class: "decisions-why", Dir: "auto"},
			html.Strong(html.Props{Text: t(dcWhy) + ": "}), html.Span(html.Props{Text: item.Rationale})))
	}
	children = append(children, html.P(html.Props{Class: "decisions-meta", Text: meta}))
	var actions []ui.Node
	if item.SourcePostID != "" {
		actions = append(actions, html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"decisions-goto": item.SourcePostID, "decisions-seq": strconv.FormatUint(item.SourceSequence, 10)},
			Aria: map[string]string{"label": t(dcGoto) + ": " + item.Text}, Text: t(dcGoto)}))
	}
	if item.CanReplace && !replaced && v.CanRecord {
		actions = append(actions, html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"decisions-replace": item.ID}, Disabled: v.Busy,
			Aria: map[string]string{"label": t(dcReplace) + ": " + item.Text}, Text: t(dcReplace)}))
	}
	if len(actions) > 0 {
		children = append(children, html.Div(html.Props{Class: "decisions-actions"}, actions...))
	}
	class := "decisions-item"
	if replaced {
		class += " decisions-item-replaced"
	}
	return html.Li(html.Props{Class: class, Data: map[string]string{"decision": item.ID}}, children...)
}

func decisionsMaybeBadge(show bool, text string) ui.Node {
	if !show {
		return nil
	}
	return html.Span(html.Props{Class: "decisions-badge decisions-badge-note", Text: text})
}

// RenderDecisionsDialog is the Decisions dialog: the conversation's decisions
// and, where the person may record one, the form. The browser client draws it
// over the page and fills it.
func RenderDecisionsDialog(v DecisionsDialogView) ui.Node {
	t := func(key string) string { return DecisionsText(v.Locale, key) }
	dir := "ltr"
	if chatEmojiLocale(v.Locale) == "ar" {
		dir = "rtl"
	}
	heading := t(dcTitle)
	if v.Channel != "" {
		heading += " · " + v.Channel
	}
	body := []ui.Node{
		html.H2(html.Props{ID: "decisions-title", Class: "decisions-heading", Dir: "auto", Text: heading}),
		html.P(html.Props{Class: "decisions-notice", Text: t(dcInformal)}),
		decisionsFromNote(v),
		html.Div(html.Props{ID: "decisions-live", Class: "decisions-live", Role: "status", Aria: map[string]string{"live": "polite"}, Text: v.Notice}),
	}
	switch {
	case v.State == "loading":
		body = append(body, html.P(html.Props{Class: "decisions-status", Text: t(dcLoading)}))
	case v.State == "error" && len(v.Items) == 0 && v.Error != "":
		body = append(body, html.P(html.Props{Class: "decisions-error", ID: "decisions-error", Role: "alert", Text: DecisionsErrorText(v.Locale, v.Error)}))
	default:
		var current, replaced []ui.Node
		for _, item := range v.Items {
			if item.SupersededByID == "" {
				current = append(current, decisionsItem(v, item, false))
			} else {
				replaced = append(replaced, decisionsItem(v, item, true))
			}
		}
		if len(current) == 0 && len(replaced) == 0 {
			body = append(body, html.P(html.Props{Class: "decisions-status", Text: t(dcEmpty)}))
		}
		if len(current) > 0 {
			body = append(body, html.Ul(html.Props{Class: "decisions-list", Aria: map[string]string{"label": t(dcListLabel)}}, current...))
		}
		if len(replaced) > 0 {
			body = append(body, chatPolishDisclosure(html.Props{Class: "decisions-earlier"},
				chatPolishDisclosureLabel(html.Props{Text: countedLabel(Model{Locale: v.Locale}, t(dcEarlier), len(replaced))}),
				html.Ul(html.Props{Class: "decisions-list"}, replaced...)))
		}
		body = append(body, decisionsForm(v))
	}
	body = append(body, html.Div(html.Props{Class: "decisions-actions decisions-footer"},
		html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"decisions-close": "true"}, Disabled: v.Busy, Text: t(dcClose)})))
	return html.Div(html.Props{Class: "decisions-overlay", Dir: dir, Data: map[string]string{"decisions-overlay": "true"}},
		html.Div(html.Props{Class: "decisions-dialog", Role: "dialog", Dir: dir, Aria: map[string]string{"modal": "true", "labelledby": "decisions-title"}}, body...))
}

func decisionsForm(v DecisionsDialogView) ui.Node {
	t := func(key string) string { return DecisionsText(v.Locale, key) }
	if !v.CanRecord {
		return html.P(html.Props{Class: "decisions-status", Text: t(dcReadOnly)})
	}
	submit := t(dcSubmit)
	if v.ReplacingID != "" {
		submit = t(dcSubmitSwap)
	}
	if v.Busy {
		submit = t(dcBusy)
	}
	var form []ui.Node
	if v.ReplacingID != "" {
		replacing := ""
		for _, item := range v.Items {
			if item.ID == v.ReplacingID {
				replacing = item.Text
			}
		}
		form = append(form, html.Div(html.Props{Class: "decisions-replacing"},
			html.P(html.Props{Class: "decisions-replacing-text", Dir: "auto"}, html.Strong(html.Props{Text: t(dcReplacing) + ": "}), html.Span(html.Props{Text: replacing})),
			html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"decisions-keep": "true"}, Disabled: v.Busy, Text: t(dcKeepBoth)})))
	}
	hint := "decisions-hint"
	describedBy := hint
	if v.Error != "" {
		describedBy += " decisions-error"
	}
	form = append(form,
		html.Div(html.Props{Class: "decisions-field"},
			html.Label(html.Props{For: "decisions-field-text", Text: t(dcFieldText)}),
			html.Textarea(html.Props{ID: "decisions-field-text", Name: "text", Dir: "auto", Rows: 2, MaxLength: 500, Disabled: v.Busy, Required: true,
				Aria: map[string]string{"describedby": describedBy, "invalid": boolString(v.Error != "")}}, ui.Text(v.Text)),
			html.P(html.Props{ID: hint, Class: "decisions-hint", Text: t(dcHintText)})),
		html.Div(html.Props{Class: "decisions-field"},
			html.Label(html.Props{For: "decisions-field-why", Text: t(dcFieldWhy)}),
			html.Textarea(html.Props{ID: "decisions-field-why", Name: "why", Dir: "auto", Rows: 3, MaxLength: 2000, Disabled: v.Busy}, ui.Text(v.Rationale))))
	if v.Error != "" {
		form = append(form, html.P(html.Props{Class: "decisions-error", ID: "decisions-error", Role: "alert", Text: DecisionsErrorText(v.Locale, v.Error)}))
	}
	form = append(form, html.Div(html.Props{Class: "decisions-actions"},
		html.Button(html.Props{Type: "submit", Class: "button primary", Disabled: v.Busy, Aria: map[string]string{"busy": boolString(v.Busy)}, Text: submit})))
	return html.Form(html.Props{Class: "decisions-form", Data: map[string]string{"decisions-form": "true"}, Raw: map[string]any{"novalidate": ""}}, form...)
}

// ChatDecisionsStyles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatDecisionsStyles = `
.decisions-overlay{position:fixed;inset:0;z-index:60;display:flex;align-items:center;justify-content:center;padding:16px;background:color-mix(in srgb,var(--ink) 45%,transparent);box-sizing:border-box}
.decisions-dialog{inline-size:min(100%,560px);max-block-size:100%;overflow:auto;padding:20px;border-radius:12px;background:var(--hcm-color-surface,Canvas);color:var(--hcm-color-text,CanvasText);box-shadow:var(--hcm-shadow-raised);box-sizing:border-box}
.decisions-heading{margin:0 0 8px;font-size:1.125rem;line-height:1.3;overflow-wrap:anywhere}
.decisions-notice,.decisions-status,.decisions-hint{margin:0 0 12px;font-size:.8125rem;line-height:1.45;color:var(--hcm-color-text-muted,inherit)}
.decisions-live:empty{display:none}
.decisions-live{margin:0 0 12px;font-size:.875rem;font-weight:600}
.decisions-list{list-style:none;margin:0 0 12px;padding:0;display:flex;flex-direction:column;gap:10px}
.decisions-item{padding:12px;border:1px solid var(--hcm-color-border);border-radius:10px;min-inline-size:0}
.decisions-item-replaced{opacity:.85}
.decisions-item-replaced .decisions-text{text-decoration:line-through;text-decoration-thickness:1px}
.decisions-badges{display:flex;flex-wrap:wrap;gap:6px;margin-block-end:6px}
.decisions-badge{display:inline-block;padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:10px;font-size:.75rem;font-weight:600}
.decisions-badge-note{font-weight:400}
.decisions-text{margin:0 0 6px;font-size:.9375rem;line-height:1.45;overflow-wrap:anywhere}
.decisions-why,.decisions-meta{margin:0 0 6px;font-size:.8125rem;line-height:1.45;overflow-wrap:anywhere}
.decisions-meta{color:var(--hcm-color-text-muted,inherit)}
.decisions-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end}
.decisions-item .decisions-actions{justify-content:flex-start}
.decisions-actions .button{min-block-size:44px;min-inline-size:44px}
.decisions-field{display:flex;flex-direction:column;gap:4px;margin-block-end:12px}
.decisions-field label{font-size:.875rem;font-weight:600}
.decisions-field textarea{min-block-size:44px;padding:8px 10px;border:1px solid var(--hcm-color-border);border-radius:8px;background:transparent;color:inherit;font:inherit;box-sizing:border-box;inline-size:100%;resize:vertical}
.decisions-field textarea[aria-invalid=true]{border-color:var(--hcm-color-danger,#b00020)}
.decisions-replacing{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin-block-end:12px;padding:8px 10px;border:1px dashed var(--hcm-color-border);border-radius:8px}
.decisions-replacing-text{margin:0;flex:1 1 200px;min-inline-size:0;font-size:.875rem;overflow-wrap:anywhere}
.decisions-error{margin:0 0 12px;font-size:.875rem;line-height:1.45;color:var(--hcm-color-danger,#b00020)}
.decisions-earlier{margin-block-end:12px}
.decisions-footer{margin-block-start:8px}
.details-decisions-open{display:inline-flex;align-items:center;gap:8px;min-block-size:44px}
@media (max-width:480px){.decisions-overlay{padding:0;align-items:stretch}.decisions-dialog{border-radius:0;inline-size:100%}}
`

// DecisionsPostLabel opens the line posted into the conversation when a
// decision is recorded: "Decision (informal): ..." in the recorder's language.
func DecisionsPostLabel(locale string) string { return DecisionsText(locale, dcPostLabel) }
