package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3794: hand off one specific work item that this conversation made. The
// owner of a linked task offers its next step to a person in the conversation
// with a note; the person accepts or declines, and both of them see the real
// state under the task. The offer is private to the two people. A follow-up
// asks for the next step and leaves the task's owner alone; a transfer asks the
// person to become the owner, which the project service decides when they accept.

// WorkHandoffView is one offer as the viewer may see it.
type WorkHandoffView struct {
	ID, PostID, ProjectID, TaskID string
	// Mode is FOLLOW_UP or TRANSFER; Role is "sender" or "recipient".
	Mode, Role string
	FromID     string
	ToID       string
	NextAction string
	Note       string
	// Status is OFFERED, ACCEPTED, DECLINED or WITHDRAWN.
	Status string
	// The work's owner and deadline as offered, and whether they have changed.
	OfferedOwner, OfferedDue string
	Changed                  bool
}

// WorkHandoffState is the hand-off client state. Enabled is set once the server
// has said it keeps offers for this person.
type WorkHandoffState struct {
	Enabled bool
	// ByTask holds the viewer's offers, keyed by task id.
	ByTask map[string][]WorkHandoffView
	// Busy holds the offers being answered right now.
	Busy map[string]bool
	// Offer opens the form for one linked task; Answer sends accept, decline or
	// withdraw for one offer.
	Offer  func(postID, projectID, taskID, title string)
	Answer func(id, verb string)
}

// WorkHandoffPerson is someone the work may be offered to.
type WorkHandoffPerson struct{ ID, Name string }

// WorkHandoffDialogView is everything the offer form shows.
type WorkHandoffDialogView struct {
	Locale, PostID, ProjectID, TaskID, Title string
	Mode, RecipientID, NextAction, Note      string
	People                                   []WorkHandoffPerson
	// Error is a code from WorkHandoffErrorText; Busy is true while sending.
	Error string
	Busy  bool
}

const (
	whOpen         = "open"
	whHeadSender   = "head_sender"
	whHeadReceiver = "head_receiver"
	whGroup        = "group"
	whNextAction   = "next_action"
	whNote         = "note"
	whOwnerThen    = "owner_then"
	whDueThen      = "due_then"
	whEffectFollow = "effect_follow"
	whEffectMove   = "effect_move"
	whAudience     = "audience"
	whChanged      = "changed"
	whAccept       = "accept"
	whDecline      = "decline"
	whWithdraw     = "withdraw"
	whWorking      = "working"
	whStOfferedS   = "st_offered_sender"
	whStOfferedR   = "st_offered_recipient"
	whStAccepted   = "st_accepted"
	whStDeclined   = "st_declined"
	whStWithdrawn  = "st_withdrawn"
	whKindFollow   = "kind_follow"
	whKindMove     = "kind_move"
	whSomeone      = "someone"
	whYou          = "you"
	whNoOne        = "no_one"
	whDlgTitle     = "dlg_title"
	whFieldKind    = "field_kind"
	whFieldTo      = "field_to"
	whFieldAction  = "field_action"
	whFieldNote    = "field_note"
	whChoosePerson = "choose_person"
	whSend         = "send"
	whSending      = "sending"
	whCancel       = "cancel"
	whFollowHelp   = "follow_help"
	whMoveHelp     = "move_help"
	whNoPeople     = "no_people"
	whErrInvalid   = "err_invalid"
	whErrNotFound  = "err_not_found"
	whErrDenied    = "err_denied"
	whErrConflict  = "err_conflict"
	whErrUnavail   = "err_unavailable"
	whErrAction    = "err_action"
	whErrPerson    = "err_person"
	whDone         = "done"
)

var workHandoffCopy = map[string]map[string]string{
	"en-US": {
		whOpen: "Hand off", whHeadSender: "You offered this to {name}", whHeadReceiver: "{name} offered you the next step",
		whGroup: "Hand-offs for this work", whNextAction: "Next step", whNote: "Note", whOwnerThen: "Owner when offered", whDueThen: "Due when offered",
		whEffectFollow: "If accepted, {name} takes this next step. The work keeps its owner, deadline and history, and nobody gains approval rights.",
		whEffectMove:   "If accepted, the work becomes {name}'s through the project, which checks their access again. Its deadline and history are kept.",
		whAudience:     "Only you and {name} can see this offer. It does not hand over anything else discussed here.",
		whChanged:      "The work's owner or deadline has changed since this was offered. Check it before you answer.",
		whAccept:       "Accept", whDecline: "Decline", whWithdraw: "Withdraw offer", whWorking: "Working",
		whStOfferedS: "Waiting for {name}", whStOfferedR: "Waiting for your answer", whStAccepted: "Accepted", whStDeclined: "Declined", whStWithdrawn: "Withdrawn",
		whKindFollow: "Take the next step", whKindMove: "Take over the work", whSomeone: "A colleague", whYou: "You", whNoOne: "no one",
		whDlgTitle: "Hand off this work", whFieldKind: "What are you offering?", whFieldTo: "Offer to", whFieldAction: "What should they do next?", whFieldNote: "Note (optional)",
		whChoosePerson: "Choose a person", whSend: "Send offer", whSending: "Sending the offer", whCancel: "Cancel",
		whFollowHelp: "They take the next step. The work stays with its current owner.",
		whMoveHelp:   "They become the owner if they accept and are allowed to edit this work.",
		whNoPeople:   "There is nobody else in this conversation to offer it to.",
		whErrInvalid: "Check the person and the next step, then try again.", whErrNotFound: "This work or conversation is no longer available to you.",
		whErrDenied:   "You are not allowed to do that with this work.",
		whErrConflict: "This offer is out of date: the work changed or the offer was already answered. Reload and check it.",
		whErrUnavail:  "That could not be done right now. Try again; the same offer is never made twice.",
		whErrAction:   "Enter what they should do next.", whErrPerson: "Choose who to offer it to.", whDone: "Hand-off updated",
	},
	"de-DE": {
		whOpen: "Übergeben", whHeadSender: "Sie haben dies {name} angeboten", whHeadReceiver: "{name} hat Ihnen den nächsten Schritt angeboten",
		whGroup: "Übergaben für diese Arbeit", whNextAction: "Nächster Schritt", whNote: "Notiz", whOwnerThen: "Verantwortlich beim Angebot", whDueThen: "Fällig beim Angebot",
		whEffectFollow: "Bei Annahme übernimmt {name} diesen nächsten Schritt. Die Arbeit behält Verantwortliche, Frist und Verlauf, und niemand erhält Genehmigungsrechte.",
		whEffectMove:   "Bei Annahme geht die Arbeit über das Projekt an {name} über, das den Zugriff erneut prüft. Frist und Verlauf bleiben erhalten.",
		whAudience:     "Nur Sie und {name} sehen dieses Angebot. Es übergibt nichts anderes, was hier besprochen wurde.",
		whChanged:      "Verantwortliche oder Frist der Arbeit haben sich seit dem Angebot geändert. Prüfen Sie dies vor Ihrer Antwort.",
		whAccept:       "Annehmen", whDecline: "Ablehnen", whWithdraw: "Angebot zurückziehen", whWorking: "Wird bearbeitet",
		whStOfferedS: "Wartet auf {name}", whStOfferedR: "Wartet auf Ihre Antwort", whStAccepted: "Angenommen", whStDeclined: "Abgelehnt", whStWithdrawn: "Zurückgezogen",
		whKindFollow: "Nächsten Schritt übernehmen", whKindMove: "Arbeit übernehmen", whSomeone: "Ein Kollege", whYou: "Sie", whNoOne: "niemand",
		whDlgTitle: "Diese Arbeit übergeben", whFieldKind: "Was bieten Sie an?", whFieldTo: "Angebot an", whFieldAction: "Was soll als Nächstes geschehen?", whFieldNote: "Notiz (optional)",
		whChoosePerson: "Person wählen", whSend: "Angebot senden", whSending: "Angebot wird gesendet", whCancel: "Abbrechen",
		whFollowHelp: "Die Person übernimmt den nächsten Schritt. Die Arbeit bleibt bei den bisherigen Verantwortlichen.",
		whMoveHelp:   "Die Person wird bei Annahme verantwortlich, sofern sie diese Arbeit bearbeiten darf.",
		whNoPeople:   "In dieser Unterhaltung ist niemand sonst, dem Sie es anbieten könnten.",
		whErrInvalid: "Prüfen Sie die Person und den nächsten Schritt und versuchen Sie es erneut.", whErrNotFound: "Diese Arbeit oder Unterhaltung ist für Sie nicht mehr verfügbar.",
		whErrDenied:   "Sie dürfen das mit dieser Arbeit nicht tun.",
		whErrConflict: "Dieses Angebot ist veraltet: Die Arbeit hat sich geändert oder das Angebot wurde schon beantwortet. Laden Sie neu und prüfen Sie es.",
		whErrUnavail:  "Das ging gerade nicht. Versuchen Sie es erneut; dasselbe Angebot wird nie doppelt gemacht.",
		whErrAction:   "Geben Sie ein, was als Nächstes geschehen soll.", whErrPerson: "Wählen Sie, wem Sie es anbieten.", whDone: "Übergabe aktualisiert",
	},
	"ar": {
		whOpen: "تسليم", whHeadSender: "عرضت هذا على {name}", whHeadReceiver: "عرض عليك {name} الخطوة التالية",
		whGroup: "عمليات التسليم لهذا العمل", whNextAction: "الخطوة التالية", whNote: "ملاحظة", whOwnerThen: "المسؤول وقت العرض", whDueThen: "الاستحقاق وقت العرض",
		whEffectFollow: "عند القبول يتولى {name} هذه الخطوة التالية. يبقى للعمل مسؤوله وموعده وسجله، ولا يحصل أحد على صلاحيات اعتماد.",
		whEffectMove:   "عند القبول ينتقل العمل إلى {name} عبر المشروع الذي يتحقق من وصوله مرة أخرى. يبقى موعده وسجله.",
		whAudience:     "أنت و{name} فقط ترون هذا العرض. لا يسلّم أي شيء آخر نوقش هنا.",
		whChanged:      "تغيّر مسؤول العمل أو موعده منذ تقديم العرض. تحقق من ذلك قبل الرد.",
		whAccept:       "قبول", whDecline: "رفض", whWithdraw: "سحب العرض", whWorking: "جارٍ التنفيذ",
		whStOfferedS: "بانتظار {name}", whStOfferedR: "بانتظار ردك", whStAccepted: "تم القبول", whStDeclined: "تم الرفض", whStWithdrawn: "تم السحب",
		whKindFollow: "تولّي الخطوة التالية", whKindMove: "تولّي العمل", whSomeone: "زميل", whYou: "أنت", whNoOne: "لا أحد",
		whDlgTitle: "تسليم هذا العمل", whFieldKind: "ماذا تعرض؟", whFieldTo: "العرض إلى", whFieldAction: "ماذا ينبغي أن يفعل بعد ذلك؟", whFieldNote: "ملاحظة (اختياري)",
		whChoosePerson: "اختر شخصًا", whSend: "إرسال العرض", whSending: "جارٍ إرسال العرض", whCancel: "إلغاء",
		whFollowHelp: "يتولى الخطوة التالية. يبقى العمل لدى مسؤوله الحالي.",
		whMoveHelp:   "يصبح مسؤولًا إذا قبل وكان مسموحًا له بتعديل هذا العمل.",
		whNoPeople:   "لا يوجد أحد آخر في هذه المحادثة لتعرض عليه.",
		whErrInvalid: "تحقق من الشخص والخطوة التالية ثم حاول مرة أخرى.", whErrNotFound: "لم يعد هذا العمل أو هذه المحادثة متاحًا لك.",
		whErrDenied:   "غير مسموح لك بفعل ذلك مع هذا العمل.",
		whErrConflict: "هذا العرض قديم: تغيّر العمل أو تم الرد على العرض. أعد التحميل وتحقق منه.",
		whErrUnavail:  "تعذر ذلك الآن. حاول مرة أخرى؛ لا يُقدَّم العرض نفسه مرتين أبدًا.",
		whErrAction:   "أدخل ما ينبغي فعله بعد ذلك.", whErrPerson: "اختر من تعرض عليه.", whDone: "تم تحديث التسليم",
	},
}

// WorkHandoffText is the copy of the hand-off surfaces in the viewer's language.
func WorkHandoffText(locale, key string) string {
	return chatbug039Text(key, workHandoffCopy[chatEmojiLocale(locale)][key], workHandoffCopy["en-US"][key])
}

// WorkHandoffDoneText announces a changed hand-off to a screen reader.
func WorkHandoffDoneText(locale string) string { return WorkHandoffText(locale, whDone) }

// WorkHandoffErrorText names a refusal code from the server in plain words.
func WorkHandoffErrorText(locale, code string) string {
	switch code {
	case "":
		return ""
	case "invalid_argument":
		return WorkHandoffText(locale, whErrInvalid)
	case "not_found":
		return WorkHandoffText(locale, whErrNotFound)
	case "permission_denied":
		return WorkHandoffText(locale, whErrDenied)
	case "conflict":
		return WorkHandoffText(locale, whErrConflict)
	case "action":
		return WorkHandoffText(locale, whErrAction)
	case "person":
		return WorkHandoffText(locale, whErrPerson)
	}
	return WorkHandoffText(locale, whErrUnavail)
}

func workHandoffFill(text, name string) string { return strings.ReplaceAll(text, "{name}", name) }

// workHandoffName is how the viewer's own name and others' names read in an offer.
func workHandoffName(m Model, msg Message, id string) string {
	if id == m.CurrentUser {
		return WorkHandoffText(m.Locale, whYou)
	}
	if name := chatworkOwnerName(m, msg, id); name != "" {
		return name
	}
	for _, member := range m.Members {
		if member.ID == id && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	return WorkHandoffText(m.Locale, whSomeone)
}

func workHandoffStatus(m Model, h WorkHandoffView, other string) string {
	switch h.Status {
	case "ACCEPTED":
		return WorkHandoffText(m.Locale, whStAccepted)
	case "DECLINED":
		return WorkHandoffText(m.Locale, whStDeclined)
	case "WITHDRAWN":
		return WorkHandoffText(m.Locale, whStWithdrawn)
	}
	if h.Role == "recipient" {
		return WorkHandoffText(m.Locale, whStOfferedR)
	}
	return workHandoffFill(WorkHandoffText(m.Locale, whStOfferedS), other)
}

// workHandoffBlocks is what sits under a message's work chips: for each task the
// message became, the Hand off action (to its owner) and the offers about it.
func workHandoffBlocks(m Model, msg Message) []ui.Node {
	state := m.WorkHandoff
	links := m.MessageWork.Links[msg.ID]
	if !state.Enabled || len(links) == 0 {
		return nil
	}
	var groups []ui.Node
	for _, link := range links {
		offers := state.ByTask[link.TaskID]
		canOffer := state.Offer != nil && m.CurrentUser != "" && link.OwnerID == m.CurrentUser && m.workHandoffPeople() > 0
		var shown []ui.Node
		for _, offer := range offers {
			if offer.ProjectID != link.ProjectID || offer.PostID != msg.ID {
				continue
			}
			shown = append(shown, workHandoffCard(m, msg, link, offer))
		}
		if !canOffer && len(shown) == 0 {
			continue
		}
		var children []ui.Node
		if canOffer {
			label := WorkHandoffText(m.Locale, whOpen)
			children = append(children, html.Button(html.Props{Type: "button", Class: "button secondary small chathandoff-open", Data: map[string]string{"action": "handoff-open", "id": link.TaskID},
				Aria: map[string]string{"label": label + ": " + link.Title}, Text: label}))
		}
		children = append(children, shown...)
		groups = append(groups, html.Div(html.Props{Class: "chathandoff-group", Role: "group", Aria: map[string]string{"label": WorkHandoffText(m.Locale, whGroup) + ": " + link.Title}}, children...))
	}
	return groups
}

// workHandoffPeople counts the people the viewer could offer work to.
func (m Model) workHandoffPeople() int { return len(m.workHandoffCandidates()) }

func (m Model) workHandoffCandidates() []WorkHandoffPerson {
	var out []WorkHandoffPerson
	for _, member := range m.Members {
		if member.Agent || member.ID == "" || member.ID == m.CurrentUser || strings.TrimSpace(member.Name) == "" {
			continue
		}
		out = append(out, WorkHandoffPerson{ID: member.ID, Name: member.Name})
	}
	return out
}

// WorkHandoffCandidates lists the people in the open conversation the work may
// be offered to: people only, never the viewer, never agents.
func WorkHandoffCandidates(m Model) []WorkHandoffPerson { return m.workHandoffCandidates() }

func workHandoffCard(m Model, msg Message, link MessageWorkLink, h WorkHandoffView) ui.Node {
	t := func(key string) string { return WorkHandoffText(m.Locale, key) }
	other, head := workHandoffName(m, msg, h.ToID), t(whHeadSender)
	if h.Role == "recipient" {
		other, head = workHandoffName(m, msg, h.FromID), t(whHeadReceiver)
	}
	head = workHandoffFill(head, other)
	status := workHandoffStatus(m, h, other)
	effect := t(whEffectFollow)
	kind := t(whKindFollow)
	if h.Mode == "TRANSFER" {
		effect, kind = t(whEffectMove), t(whKindMove)
	}
	// The effect names the person who would act: the recipient.
	recipient := workHandoffName(m, msg, h.ToID)
	rows := []ui.Node{
		html.Li(html.Props{Class: "chathandoff-row"}, html.Span(html.Props{Class: "chathandoff-label", Text: t(whNextAction)}), html.Span(html.Props{Class: "chathandoff-value", Dir: "auto", Text: h.NextAction})),
	}
	if strings.TrimSpace(h.Note) != "" {
		rows = append(rows, html.Li(html.Props{Class: "chathandoff-row"}, html.Span(html.Props{Class: "chathandoff-label", Text: t(whNote)}), html.Span(html.Props{Class: "chathandoff-value", Dir: "auto", Text: h.Note})))
	}
	owner := workHandoffName(m, msg, h.OfferedOwner)
	if h.OfferedOwner == "" {
		owner = t(whNoOne)
	}
	rows = append(rows, html.Li(html.Props{Class: "chathandoff-row"}, html.Span(html.Props{Class: "chathandoff-label", Text: t(whOwnerThen)}), html.Span(html.Props{Class: "chathandoff-value", Text: owner})))
	if h.OfferedDue != "" {
		rows = append(rows, html.Li(html.Props{Class: "chathandoff-row"}, html.Span(html.Props{Class: "chathandoff-label", Text: t(whDueThen)}), html.Span(html.Props{Class: "chathandoff-value", Text: h.OfferedDue})))
	}
	parts := []ui.Node{
		html.P(html.Props{Class: "chathandoff-head"}, html.Span(html.Props{Class: "chathandoff-kind", Text: head}), html.Span(html.Props{Class: "chathandoff-status chathandoff-status-" + strings.ToLower(h.Status), Role: "status", Text: status})),
		html.P(html.Props{Class: "chathandoff-type", Text: kind}),
		html.Ul(html.Props{Class: "chathandoff-facts"}, rows...),
	}
	if h.Status == "OFFERED" {
		parts = append(parts, html.P(html.Props{Class: "chathandoff-effect", Text: workHandoffFill(effect, recipient)}))
		if h.Changed {
			parts = append(parts, html.P(html.Props{Class: "chathandoff-changed", Role: "alert", Text: t(whChanged)}))
		}
	}
	parts = append(parts, html.P(html.Props{Class: "chathandoff-audience", Text: workHandoffFill(t(whAudience), other)}))
	if h.Status == "OFFERED" && m.WorkHandoff.Answer != nil {
		busy := m.WorkHandoff.Busy[h.ID]
		button := func(verb, class, label string) ui.Node {
			if busy {
				label = t(whWorking)
			}
			return html.Button(html.Props{Type: "button", Class: class, Disabled: busy, Data: map[string]string{"action": "handoff-" + verb, "id": h.ID},
				Aria: map[string]string{"label": label + ": " + link.Title, "busy": boolString(busy)}, Text: label})
		}
		var actions []ui.Node
		if h.Role == "recipient" {
			actions = append(actions, button("accept", "button primary small", t(whAccept)), button("decline", "button secondary small", t(whDecline)))
		} else {
			actions = append(actions, button("withdraw", "button secondary small", t(whWithdraw)))
		}
		parts = append(parts, html.Div(html.Props{Class: "chathandoff-actions"}, actions...))
	}
	return html.Section(html.Props{Class: "chathandoff", Data: map[string]string{"handoff-id": h.ID, "handoff-status": strings.ToLower(h.Status)}, Aria: map[string]string{"label": head + ": " + link.Title}}, parts...)
}

// workHandoffAction runs when a hand-off button of a message is chosen.
func (m Model) workHandoffAction(action, id string) {
	state := m.WorkHandoff
	if !state.Enabled || id == "" {
		return
	}
	switch action {
	case "handoff-open":
		if state.Offer == nil {
			return
		}
		for _, links := range m.MessageWork.Links {
			for _, link := range links {
				if link.TaskID == id {
					state.Offer(link.PostID, link.ProjectID, link.TaskID, link.Title)
					return
				}
			}
		}
	case "handoff-accept", "handoff-decline", "handoff-withdraw":
		if state.Answer != nil && !state.Busy[id] {
			state.Answer(id, strings.TrimPrefix(action, "handoff-"))
		}
	}
}

// RenderWorkHandoffDialog is the offer form. The browser client draws it over
// the page, fills it, and sends the reviewed values.
func RenderWorkHandoffDialog(v WorkHandoffDialogView) ui.Node {
	t := func(key string) string { return WorkHandoffText(v.Locale, key) }
	dir := "ltr"
	if chatEmojiLocale(v.Locale) == "ar" {
		dir = "rtl"
	}
	body := []ui.Node{html.H2(html.Props{ID: "chathandoff-title", Class: "chathandoff-heading", Dir: "auto", Text: t(whDlgTitle) + ": " + v.Title})}
	if len(v.People) == 0 {
		body = append(body, html.P(html.Props{Class: "chathandoff-note", Role: "status", Text: t(whNoPeople)}),
			html.Div(html.Props{Class: "chathandoff-dialog-actions"}, html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chathandoff-close": "true"}, Text: t(whCancel)})))
	} else {
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: t(whChoosePerson), Selected: v.RecipientID == ""})}
		for _, person := range v.People {
			options = append(options, html.Option(html.Props{Value: person.ID, Text: person.Name, Selected: person.ID == v.RecipientID}))
		}
		mode := v.Mode
		if mode != "TRANSFER" {
			mode = "FOLLOW_UP"
		}
		radio := func(value, label, help string) ui.Node {
			id := "chathandoff-kind-" + strings.ToLower(value)
			return html.Div(html.Props{Class: "chathandoff-choice"},
				html.Input(html.Props{ID: id, Type: "radio", Name: "mode", Value: value, Checked: mode == value, Disabled: v.Busy, Aria: map[string]string{"describedby": id + "-help"}}),
				html.Label(html.Props{For: id, Text: label}), html.Span(html.Props{ID: id + "-help", Class: "chathandoff-help", Text: help}))
		}
		field := func(id, label string, control ui.Node) ui.Node {
			return html.Div(html.Props{Class: "chathandoff-field"}, html.Label(html.Props{For: id, Text: label}), control)
		}
		submit := t(whSend)
		if v.Busy {
			submit = t(whSending)
		}
		form := []ui.Node{
			html.Fieldset(html.Props{Class: "chathandoff-kinds"}, html.Legend(html.Props{Text: t(whFieldKind)}),
				radio("FOLLOW_UP", t(whKindFollow), t(whFollowHelp)), radio("TRANSFER", t(whKindMove), t(whMoveHelp))),
			field("chathandoff-field-to", t(whFieldTo), html.Select(html.Props{ID: "chathandoff-field-to", Name: "to", Disabled: v.Busy, Required: true}, options...)),
			field("chathandoff-field-action", t(whFieldAction), html.Input(html.Props{ID: "chathandoff-field-action", Name: "action", Type: "text", Dir: "auto", Value: v.NextAction, Disabled: v.Busy, Required: true, Raw: map[string]any{"maxlength": "200", "autocomplete": "off"}})),
			field("chathandoff-field-note", t(whFieldNote), html.Textarea(html.Props{ID: "chathandoff-field-note", Name: "note", Dir: "auto", Value: v.Note, Disabled: v.Busy, Raw: map[string]any{"maxlength": "1000", "rows": "3"}})),
		}
		if v.Error != "" {
			form = append(form, html.P(html.Props{Class: "chathandoff-error", ID: "chathandoff-error", Role: "alert", Text: WorkHandoffErrorText(v.Locale, v.Error)}))
		}
		form = append(form, html.Div(html.Props{Class: "chathandoff-dialog-actions"},
			html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chathandoff-close": "true"}, Disabled: v.Busy, Text: t(whCancel)}),
			html.Button(html.Props{Type: "submit", Class: "button primary", Disabled: v.Busy, Aria: map[string]string{"busy": boolString(v.Busy)}, Text: submit})))
		body = append(body, html.Form(html.Props{Class: "chathandoff-form", Data: map[string]string{"chathandoff-form": v.TaskID}, Raw: map[string]any{"novalidate": ""}}, form...))
	}
	return html.Div(html.Props{Class: "chat-dialog-backdrop chathandoff-overlay", Dir: dir, Data: map[string]string{"chathandoff-overlay": "true"}},
		html.Div(html.Props{Class: "chathandoff-dialog", Role: "dialog", Dir: dir, Aria: map[string]string{"modal": "true", "labelledby": "chathandoff-title"}}, body...))
}

// ChatWorkHandoffStyles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatWorkHandoffStyles = `
.chathandoff-group{display:flex;flex-direction:column;align-items:flex-start;gap:8px;margin-block-start:6px;max-inline-size:100%}
.chathandoff-open{min-block-size:44px;min-inline-size:44px}
.chathandoff{inline-size:min(100%,520px);padding:12px;border:1px solid var(--hcm-color-border);border-radius:12px;background:var(--hcm-color-surface-muted,transparent);color:var(--hcm-color-text);font-size:.875rem;line-height:1.45;box-sizing:border-box}
.chathandoff p{margin:0 0 6px}
.chathandoff-head{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:4px 8px}
.chathandoff-kind{font-weight:600;min-inline-size:0;overflow-wrap:anywhere}
.chathandoff-status{padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:12px;font-size:.75rem;font-weight:600}
.chathandoff-type{color:var(--hcm-color-text-muted,inherit)}
.chathandoff-facts{display:grid;gap:4px;margin:0 0 8px;padding:0;list-style:none}
.chathandoff-row{display:flex;flex-wrap:wrap;gap:2px 8px}
.chathandoff-label{font-weight:600}
.chathandoff-value{min-inline-size:0;overflow-wrap:anywhere}
.chathandoff-effect,.chathandoff-audience{color:var(--hcm-color-text-muted,inherit);font-size:.8125rem}
.chathandoff-changed{color:var(--hcm-color-danger,#b00020);font-weight:600}
.chathandoff-actions{display:flex;flex-wrap:wrap;gap:8px;margin-block-start:8px}
.chathandoff-actions .button{min-block-size:44px;min-inline-size:44px}
.chathandoff-overlay{z-index:60;box-sizing:border-box}
.chathandoff-dialog{inline-size:min(100%,520px);max-block-size:100%;overflow:auto;padding:20px;border-radius:12px;background:var(--hcm-color-surface,Canvas);color:var(--hcm-color-text,CanvasText);box-shadow:var(--hcm-shadow-raised);box-sizing:border-box}
.chathandoff-heading{margin:0 0 12px;font-size:1.125rem;line-height:1.3;overflow-wrap:anywhere}
.chathandoff-kinds{margin:0 0 12px;padding:0;border:0}
.chathandoff-kinds legend{padding:0;margin-block-end:4px;font-size:.875rem;font-weight:600}
.chathandoff-choice{display:grid;grid-template-columns:auto 1fr;column-gap:8px;align-items:center;min-block-size:44px}
.chathandoff-choice input{inline-size:20px;block-size:20px;margin:0}
.chathandoff-help{grid-column:2;font-size:.8125rem;color:var(--hcm-color-text-muted,inherit)}
.chathandoff-field{display:flex;flex-direction:column;gap:4px;margin-block-end:12px}
.chathandoff-field label{font-size:.875rem;font-weight:600}
.chathandoff-field input,.chathandoff-field select,.chathandoff-field textarea{min-block-size:44px;padding:8px 10px;border:1px solid var(--hcm-color-border);border-radius:8px;background:transparent;color:inherit;font:inherit;box-sizing:border-box;inline-size:100%}
.chathandoff-note{margin:0 0 12px;font-size:.875rem;color:var(--hcm-color-text-muted,inherit)}
.chathandoff-error{margin:0 0 12px;font-size:.875rem;color:var(--hcm-color-danger,#b00020)}
.chathandoff-dialog-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end}
.chathandoff-dialog-actions .button{min-block-size:44px;min-inline-size:44px}
`
