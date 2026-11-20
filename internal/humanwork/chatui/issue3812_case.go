package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3812: a conversation bound to a governed case. The server decides
// everything (who may change the people in it, what may not leave it); this file
// draws what the server said (CaseState): a badge in the conversation header, a
// Case section in the details for everyone in the conversation, and, for a
// workspace administrator, the form that binds it, changes it or lifts it.
//
// The copy does not promise more than it can do: a restricted conversation cannot
// be forwarded, linked or saved outside itself, but anyone in it can still read it
// aloud, copy it by hand or photograph the screen, and the text says so.

// CaseState is what the server said about the open conversation's case.
// Transient client state, never message content.
type CaseState struct {
	// ConversationID is the conversation the answer is about; an answer for
	// another conversation is not shown.
	ConversationID string
	// Loaded is true once the server has answered.
	Loaded     bool
	Bound      bool
	Restricted bool
	CaseRef    string
	OwnerID    string
	OwnerIsMe  bool
	// CanManage is set for a workspace administrator who is in the conversation.
	CanManage bool
	// Busy is set while a change is on its way.
	Busy bool
	// Notice says why the last change was not accepted ("failed", "denied",
	// "invalid") or that it was ("done").
	Notice string
	// Bind and Unbind send the change; the server decides.
	Bind   func(caseRef, ownerID string, restricted bool)
	Unbind func()
}

// caseHere returns the state when it describes the open conversation.
func (m Model) caseHere() (CaseState, bool) {
	s := m.Case
	return s, s.Loaded && s.ConversationID != "" && s.ConversationID == m.SelectedID
}

// issue3812Restricted reports whether the open conversation is bound to a
// restricted case: what leaves a conversation (links, forwards, saves, work) is
// not offered there. The server refuses these whatever the page shows.
func issue3812Restricted(m Model) bool {
	s, ok := m.caseHere()
	return ok && s.Bound && s.Restricted
}

const (
	keyIssue3812Badge          = "chat.issue3812.badge"
	keyIssue3812BadgeOpen      = "chat.issue3812.badge_open"
	keyIssue3812BadgeHelp      = "chat.issue3812.badge_help"
	keyIssue3812BadgeOpenHelp  = "chat.issue3812.badge_open_help"
	keyIssue3812Title          = "chat.issue3812.title"
	keyIssue3812Bound          = "chat.issue3812.bound"
	keyIssue3812Owner          = "chat.issue3812.owner"
	keyIssue3812OwnerMe        = "chat.issue3812.owner_me"
	keyIssue3812OwnerUnknown   = "chat.issue3812.owner_unknown"
	keyIssue3812RuleMembers    = "chat.issue3812.rule_members"
	keyIssue3812RuleRestricted = "chat.issue3812.rule_restricted"
	keyIssue3812Caveat         = "chat.issue3812.caveat"
	keyIssue3812FormBind       = "chat.issue3812.form_bind"
	keyIssue3812FormChange     = "chat.issue3812.form_change"
	keyIssue3812FieldCase      = "chat.issue3812.field_case"
	keyIssue3812FieldCaseHint  = "chat.issue3812.field_case_hint"
	keyIssue3812FieldOwner     = "chat.issue3812.field_owner"
	keyIssue3812FieldRestrict  = "chat.issue3812.field_restricted"
	keyIssue3812Submit         = "chat.issue3812.submit"
	keyIssue3812Update         = "chat.issue3812.update"
	keyIssue3812Saving         = "chat.issue3812.saving"
	keyIssue3812Lift           = "chat.issue3812.lift"
	keyIssue3812NoMembers      = "chat.issue3812.no_members"
	keyIssue3812Done           = "chat.issue3812.done"
	keyIssue3812Lifted         = "chat.issue3812.lifted"
	keyIssue3812Failed         = "chat.issue3812.failed"
	keyIssue3812Denied         = "chat.issue3812.denied"
	keyIssue3812Invalid        = "chat.issue3812.invalid"
	keyIssue3812RefusedMember  = "chat.issue3812.refused_members"
	keyIssue3812RefusedForward = "chat.issue3812.refused_forward"
	keyIssue3812RefusedShare   = "chat.issue3812.refused_share"
	keyIssue3812RefusedSave    = "chat.issue3812.refused_save"
)

var issue3812Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3812Badge:          "Case-bound, restricted",
		keyIssue3812BadgeOpen:      "Case-bound",
		keyIssue3812BadgeHelp:      "This conversation belongs to a case. Only the case owner changes who is in it, and its messages cannot be forwarded, linked or saved outside it.",
		keyIssue3812BadgeOpenHelp:  "This conversation belongs to a case. Only the case owner changes who is in it.",
		keyIssue3812Title:          "Case",
		keyIssue3812Bound:          "Bound to case {case}",
		keyIssue3812Owner:          "Case owner: {name}",
		keyIssue3812OwnerMe:        "You are the case owner",
		keyIssue3812OwnerUnknown:   "Someone in this conversation owns the case",
		keyIssue3812RuleMembers:    "Only the case owner adds or removes people. Nobody joins by themselves.",
		keyIssue3812RuleRestricted: "Messages here cannot be forwarded, linked, saved or turned into tasks outside this conversation.",
		keyIssue3812Caveat:         "People in it can still copy a message by hand or photograph the screen, so share only what the case needs.",
		keyIssue3812FormBind:       "Bind this conversation to a case",
		keyIssue3812FormChange:     "Change the case binding",
		keyIssue3812FieldCase:      "Case reference",
		keyIssue3812FieldCaseHint:  "As the case system names it, for example ER-2041.",
		keyIssue3812FieldOwner:     "Case owner",
		keyIssue3812FieldRestrict:  "Restricted: messages cannot leave this conversation",
		keyIssue3812Submit:         "Bind to case",
		keyIssue3812Update:         "Save changes",
		keyIssue3812Saving:         "Saving...",
		keyIssue3812Lift:           "Lift the case binding",
		keyIssue3812NoMembers:      "Add the people who belong to the case before binding it.",
		keyIssue3812Done:           "The case binding is saved.",
		keyIssue3812Lifted:         "The case binding is lifted.",
		keyIssue3812Failed:         "The change could not be saved. Try again.",
		keyIssue3812Denied:         "Only a workspace administrator in this conversation can change the case binding.",
		keyIssue3812Invalid:        "Enter a case reference and choose an owner who is in this conversation.",
		keyIssue3812RefusedMember:  "This conversation belongs to a case. Only the case owner changes who is in it.",
		keyIssue3812RefusedForward: "This conversation is restricted to its case. Its messages cannot be forwarded.",
		keyIssue3812RefusedShare:   "This conversation is restricted to its case. It cannot be linked.",
		keyIssue3812RefusedSave:    "This conversation is restricted to its case. Its messages cannot be saved elsewhere.",
	},
	"de-DE": {
		keyIssue3812Badge:          "Fallgebunden, eingeschränkt",
		keyIssue3812BadgeOpen:      "Fallgebunden",
		keyIssue3812BadgeHelp:      "Diese Unterhaltung gehört zu einem Fall. Nur die Fallverantwortlichen ändern, wer dabei ist, und ihre Nachrichten lassen sich nicht weiterleiten, verlinken oder außerhalb speichern.",
		keyIssue3812BadgeOpenHelp:  "Diese Unterhaltung gehört zu einem Fall. Nur die Fallverantwortlichen ändern, wer dabei ist.",
		keyIssue3812Title:          "Fall",
		keyIssue3812Bound:          "Gebunden an Fall {case}",
		keyIssue3812Owner:          "Fallverantwortlich: {name}",
		keyIssue3812OwnerMe:        "Sie sind für den Fall verantwortlich",
		keyIssue3812OwnerUnknown:   "Jemand in dieser Unterhaltung ist für den Fall verantwortlich",
		keyIssue3812RuleMembers:    "Nur die Fallverantwortlichen fügen Personen hinzu oder entfernen sie. Niemand tritt selbst bei.",
		keyIssue3812RuleRestricted: "Nachrichten hier lassen sich nicht weiterleiten, verlinken, speichern oder außerhalb dieser Unterhaltung zu Aufgaben machen.",
		keyIssue3812Caveat:         "Wer dabei ist, kann eine Nachricht trotzdem von Hand abschreiben oder den Bildschirm fotografieren. Teilen Sie nur, was der Fall braucht.",
		keyIssue3812FormBind:       "Diese Unterhaltung an einen Fall binden",
		keyIssue3812FormChange:     "Fallbindung ändern",
		keyIssue3812FieldCase:      "Fallnummer",
		keyIssue3812FieldCaseHint:  "So, wie das Fallsystem sie nennt, zum Beispiel ER-2041.",
		keyIssue3812FieldOwner:     "Fallverantwortliche Person",
		keyIssue3812FieldRestrict:  "Eingeschränkt: Nachrichten dürfen diese Unterhaltung nicht verlassen",
		keyIssue3812Submit:         "An Fall binden",
		keyIssue3812Update:         "Änderungen speichern",
		keyIssue3812Saving:         "Wird gespeichert...",
		keyIssue3812Lift:           "Fallbindung aufheben",
		keyIssue3812NoMembers:      "Fügen Sie die Personen hinzu, die zum Fall gehören, bevor Sie ihn binden.",
		keyIssue3812Done:           "Die Fallbindung ist gespeichert.",
		keyIssue3812Lifted:         "Die Fallbindung ist aufgehoben.",
		keyIssue3812Failed:         "Die Änderung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		keyIssue3812Denied:         "Nur Workspace-Administratoren in dieser Unterhaltung können die Fallbindung ändern.",
		keyIssue3812Invalid:        "Geben Sie eine Fallnummer ein und wählen Sie eine Person aus dieser Unterhaltung.",
		keyIssue3812RefusedMember:  "Diese Unterhaltung gehört zu einem Fall. Nur die Fallverantwortlichen ändern, wer dabei ist.",
		keyIssue3812RefusedForward: "Diese Unterhaltung ist auf ihren Fall beschränkt. Ihre Nachrichten lassen sich nicht weiterleiten.",
		keyIssue3812RefusedShare:   "Diese Unterhaltung ist auf ihren Fall beschränkt. Sie lässt sich nicht verlinken.",
		keyIssue3812RefusedSave:    "Diese Unterhaltung ist auf ihren Fall beschränkt. Ihre Nachrichten lassen sich nicht anderswo speichern.",
	},
	"ar": {
		keyIssue3812Badge:          "مرتبطة بقضية، مقيّدة",
		keyIssue3812BadgeOpen:      "مرتبطة بقضية",
		keyIssue3812BadgeHelp:      "هذه المحادثة تخص قضية. صاحب القضية وحده يغيّر من فيها، ولا يمكن إعادة توجيه رسائلها أو ربطها أو حفظها خارجها.",
		keyIssue3812BadgeOpenHelp:  "هذه المحادثة تخص قضية. صاحب القضية وحده يغيّر من فيها.",
		keyIssue3812Title:          "القضية",
		keyIssue3812Bound:          "مرتبطة بالقضية {case}",
		keyIssue3812Owner:          "صاحب القضية: {name}",
		keyIssue3812OwnerMe:        "أنت صاحب القضية",
		keyIssue3812OwnerUnknown:   "أحد المشاركين في هذه المحادثة هو صاحب القضية",
		keyIssue3812RuleMembers:    "صاحب القضية وحده يضيف الأشخاص أو يزيلهم. لا ينضم أحد بنفسه.",
		keyIssue3812RuleRestricted: "لا يمكن إعادة توجيه الرسائل هنا أو ربطها أو حفظها أو تحويلها إلى مهام خارج هذه المحادثة.",
		keyIssue3812Caveat:         "يستطيع المشاركون مع ذلك نسخ رسالة يدويًا أو تصوير الشاشة، فشارك ما تحتاجه القضية فقط.",
		keyIssue3812FormBind:       "ربط هذه المحادثة بقضية",
		keyIssue3812FormChange:     "تغيير ربط القضية",
		keyIssue3812FieldCase:      "مرجع القضية",
		keyIssue3812FieldCaseHint:  "كما يسميه نظام القضايا، مثل ER-2041.",
		keyIssue3812FieldOwner:     "صاحب القضية",
		keyIssue3812FieldRestrict:  "مقيّدة: لا تخرج الرسائل من هذه المحادثة",
		keyIssue3812Submit:         "ربط بقضية",
		keyIssue3812Update:         "حفظ التغييرات",
		keyIssue3812Saving:         "جارٍ الحفظ...",
		keyIssue3812Lift:           "إلغاء ربط القضية",
		keyIssue3812NoMembers:      "أضف الأشخاص المعنيين بالقضية قبل ربطها.",
		keyIssue3812Done:           "تم حفظ ربط القضية.",
		keyIssue3812Lifted:         "تم إلغاء ربط القضية.",
		keyIssue3812Failed:         "تعذّر حفظ التغيير. حاول مرة أخرى.",
		keyIssue3812Denied:         "يمكن لمسؤول مساحة العمل الموجود في هذه المحادثة فقط تغيير ربط القضية.",
		keyIssue3812Invalid:        "أدخل مرجع القضية واختر صاحبًا من المشاركين في هذه المحادثة.",
		keyIssue3812RefusedMember:  "هذه المحادثة تخص قضية. صاحب القضية وحده يغيّر من فيها.",
		keyIssue3812RefusedForward: "هذه المحادثة مقيّدة بقضيتها. لا يمكن إعادة توجيه رسائلها.",
		keyIssue3812RefusedShare:   "هذه المحادثة مقيّدة بقضيتها. لا يمكن ربطها.",
		keyIssue3812RefusedSave:    "هذه المحادثة مقيّدة بقضيتها. لا يمكن حفظ رسائلها في مكان آخر.",
	},
}

// Issue3812Text is the copy of the case surfaces in a locale; the browser client
// uses it for the sentences it announces.
func Issue3812Text(locale, key string) string {
	return chatbug039Text(key, issue3812Copy[chatEmojiLocale(locale)][key], issue3812Copy["en-US"][key])
}

// The exported keys name the sentences the browser client announces or reports.
const (
	Issue3812DoneKey           = keyIssue3812Done
	Issue3812LiftedKey         = keyIssue3812Lifted
	Issue3812RefusedMemberKey  = keyIssue3812RefusedMember
	Issue3812RefusedForwardKey = keyIssue3812RefusedForward
	Issue3812RefusedShareKey   = keyIssue3812RefusedShare
	Issue3812RefusedSaveKey    = keyIssue3812RefusedSave
)

// Issue3812RefusalNotice is the sentence for a case refusal the server reported
// with the given reason reference ("chat.case_forward" and the others), or "" when
// the reason is not a case refusal.
func Issue3812RefusalNotice(locale, reason string) string {
	switch reason {
	case "chat.case_membership":
		return Issue3812Text(locale, keyIssue3812RefusedMember)
	case "chat.case_forward":
		return Issue3812Text(locale, keyIssue3812RefusedForward)
	case "chat.case_share":
		return Issue3812Text(locale, keyIssue3812RefusedShare)
	case "chat.case_save":
		return Issue3812Text(locale, keyIssue3812RefusedSave)
	}
	return ""
}

// issue3812Badge is the header's mark: the conversation is bound to a case, and
// whether it is restricted. It is text first (no colour or icon carries it
// alone), and its help sentence is the element's own description.
func issue3812Badge(m Model) ui.Node {
	s, ok := m.caseHere()
	if !ok || !s.Bound {
		return nil
	}
	label, help := keyIssue3812BadgeOpen, keyIssue3812BadgeOpenHelp
	if s.Restricted {
		label, help = keyIssue3812Badge, keyIssue3812BadgeHelp
	}
	text, description := laneText(m, issue3812Copy, label), laneText(m, issue3812Copy, help)
	return html.Span(html.Props{Class: "chatcase-badge", Title: description, Role: "note", Data: map[string]string{"case-badge": boolString(s.Restricted)}, Aria: map[string]string{"label": text + ". " + description}},
		icon("lock"), html.Span(html.Props{Class: "chatcase-badge-text", Aria: map[string]string{"hidden": "true"}, Text: text}))
}

// caseOwnerName is the name the page has for the case owner, or "".
func caseOwnerName(m Model, s CaseState) string {
	if s.OwnerIsMe {
		return laneText(m, issue3812Copy, keyIssue3812OwnerMe)
	}
	if name := chatKnownName(m, s.OwnerID); name != "" {
		return laneTextf(m, issue3812Copy, keyIssue3812Owner, map[string]string{"name": name})
	}
	return laneText(m, issue3812Copy, keyIssue3812OwnerUnknown)
}

// caseOwnerChoices are the people who may own the case: the people of this
// company in the conversation, never an agent.
func caseOwnerChoices(m Model) []Member {
	var out []Member
	for _, p := range m.Members {
		if p.Agent || p.ID == "" || (p.HomeTenantID != "" && m.CurrentTenantID != "" && p.HomeTenantID != m.CurrentTenantID) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// issue3812Section is the Case section of the details. Everyone in a
// case-bound conversation sees what the binding means for them; a workspace
// administrator in it also sees the form.
func issue3812Section(m Model) ui.Node {
	s, ok := m.caseHere()
	if !ok || (!s.Bound && !s.CanManage) {
		return nil
	}
	head := html.H4(html.Props{Class: "chatcase-title", ID: "chatcase-title", Text: laneText(m, issue3812Copy, keyIssue3812Title)})
	children := []ui.Node{head}
	if s.Bound {
		children = append(children,
			html.P(html.Props{Class: "chatcase-line", Dir: "auto", Text: laneTextf(m, issue3812Copy, keyIssue3812Bound, map[string]string{"case": s.CaseRef})}),
			html.P(html.Props{Class: "chatcase-line", Dir: "auto", Text: caseOwnerName(m, s)}),
			html.P(html.Props{Class: "chatcase-line chatcase-muted", Text: laneText(m, issue3812Copy, keyIssue3812RuleMembers)}))
		if s.Restricted {
			children = append(children,
				html.P(html.Props{Class: "chatcase-line chatcase-muted", Text: laneText(m, issue3812Copy, keyIssue3812RuleRestricted)}),
				html.P(html.Props{Class: "chatcase-line chatcase-muted", Text: laneText(m, issue3812Copy, keyIssue3812Caveat)}))
		}
	}
	if s.CanManage {
		children = append(children, issue3812Form(m, s))
	}
	if s.Notice != "" {
		children = append(children, html.P(html.Props{Class: "chatcase-notice", Role: caseNoticeRole(s.Notice), Text: caseNoticeText(m, s.Notice)}))
	}
	return html.Section(html.Props{Class: "details-section chatcase", Aria: map[string]string{"labelledby": "chatcase-title"}, Data: map[string]string{"case-section": "true"}}, children...)
}

func caseNoticeRole(notice string) string {
	if notice == "done" || notice == "lifted" {
		return "status"
	}
	return "alert"
}

func caseNoticeText(m Model, notice string) string {
	switch notice {
	case "done":
		return laneText(m, issue3812Copy, keyIssue3812Done)
	case "lifted":
		return laneText(m, issue3812Copy, keyIssue3812Lifted)
	case "denied":
		return laneText(m, issue3812Copy, keyIssue3812Denied)
	case "invalid":
		return laneText(m, issue3812Copy, keyIssue3812Invalid)
	}
	return laneText(m, issue3812Copy, keyIssue3812Failed)
}

// issue3812Form binds, changes or lifts the binding. It is a real form: Enter
// submits, the fields are labelled, and the client reads the fields by name.
func issue3812Form(m Model, s CaseState) ui.Node {
	owners := caseOwnerChoices(m)
	heading := keyIssue3812FormBind
	submit := keyIssue3812Submit
	if s.Bound {
		heading, submit = keyIssue3812FormChange, keyIssue3812Update
	}
	if len(owners) == 0 {
		notice := html.P(html.Props{Class: "chatcase-line chatcase-muted", Text: laneText(m, issue3812Copy, keyIssue3812NoMembers)})
		if !s.Bound {
			return notice
		}
		// A binding can always be lifted, even when nobody can be chosen as owner.
		return html.Div(html.Props{Class: "chatcase-manage"}, notice,
			html.Button(html.Props{Type: "button", Class: "button chatcase-button chatcase-lift", Disabled: s.Busy, Data: map[string]string{"action": "case-unbind"}, Text: laneText(m, issue3812Copy, keyIssue3812Lift)}))
	}
	options := make([]ui.Node, 0, len(owners))
	selected := s.OwnerID
	if selected == "" {
		selected = m.CurrentUser
	}
	for _, p := range owners {
		name := p.Name
		if name == "" {
			name = chatKnownName(m, p.ID)
		}
		options = append(options, html.Option(html.Props{Value: p.ID, Selected: p.ID == selected, Text: name}))
	}
	label := laneText(m, issue3812Copy, submit)
	if s.Busy {
		label = laneText(m, issue3812Copy, keyIssue3812Saving)
	}
	restricted := !s.Bound || s.Restricted
	fields := []ui.Node{
		html.Strong(html.Props{Class: "chatcase-form-title", Text: laneText(m, issue3812Copy, heading)}),
		html.Label(html.Props{Class: "chatcase-label", For: "chatcase-ref", Text: laneText(m, issue3812Copy, keyIssue3812FieldCase)}),
		html.Input(html.Props{ID: "chatcase-ref", Class: "chat-input", Type: "text", Name: "case_ref", MaxLength: 120, AutoComplete: "off", Value: s.CaseRef, Disabled: s.Busy, Dir: "auto",
			Aria: map[string]string{"describedby": "chatcase-ref-hint", "required": "true"}}),
		html.P(html.Props{Class: "chatcase-hint", ID: "chatcase-ref-hint", Text: laneText(m, issue3812Copy, keyIssue3812FieldCaseHint)}),
		html.Label(html.Props{Class: "chatcase-label", For: "chatcase-owner", Text: laneText(m, issue3812Copy, keyIssue3812FieldOwner)}),
		html.Select(html.Props{ID: "chatcase-owner", Class: "chat-input", Name: "owner", Disabled: s.Busy}, options...),
		html.Label(html.Props{Class: "chatcase-check", For: "chatcase-restricted"},
			html.Input(html.Props{ID: "chatcase-restricted", Name: "restricted", Type: "checkbox", Checked: restricted, Disabled: s.Busy}),
			html.Span(html.Props{Text: laneText(m, issue3812Copy, keyIssue3812FieldRestrict)})),
		html.Button(html.Props{Type: "submit", Class: "button primary chatcase-button", Disabled: s.Busy, Aria: map[string]string{"busy": boolString(s.Busy)}, Text: label}),
	}
	nodes := []ui.Node{html.Form(html.Props{Class: "chatcase-form", Data: map[string]string{"case-form": m.SelectedID}}, fields...)}
	if s.Bound {
		nodes = append(nodes, html.Button(html.Props{Type: "button", Class: "button chatcase-button chatcase-lift", Disabled: s.Busy, Data: map[string]string{"action": "case-unbind"}, Text: laneText(m, issue3812Copy, keyIssue3812Lift)}))
	}
	return html.Div(html.Props{Class: "chatcase-manage"}, nodes...)
}

// caseUnbind runs when the Lift button is pressed.
func (m Model) caseUnbind() {
	if m.Case.Unbind == nil || m.Case.Busy {
		return
	}
	m.Case.Unbind()
}

// Issue3812Styles lays the badge and the section out for a phone first: the
// badge wraps instead of clipping, the fields and buttons are 44px targets, and
// logical properties keep it right in a right-to-left language.
const Issue3812Styles = `
.chatcase-badge{display:inline-flex;align-items:center;gap:4px;flex:0 1 auto;min-inline-size:0;max-inline-size:100%;padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:999px;background:var(--soft);color:var(--hcm-color-text);font-size:.75rem;font-weight:600;line-height:1.3;white-space:normal;overflow-wrap:anywhere}
.chatcase-badge .chat-icon{inline-size:14px;block-size:14px;flex:none}
.chatcase{display:flex;flex-direction:column;gap:6px;padding:12px 16px;border-block-start:1px solid var(--hcm-color-border);overflow-wrap:anywhere}
.chatcase-title{margin:0;font-size:.875rem}
.chatcase-line,.chatcase-hint{margin:0;font-size:.8125rem}
.chatcase-muted,.chatcase-hint{color:var(--hcm-color-text-muted)}
.chatcase-manage,.chatcase-form{display:flex;flex-direction:column;gap:6px}
.chatcase-label{font-size:.8125rem;font-weight:600}
.chatcase-check{display:flex;align-items:center;gap:8px;min-block-size:44px;font-size:.8125rem}
.chatcase-form .chat-input{box-sizing:border-box;inline-size:100%;min-block-size:44px}
.chatcase-button{min-block-size:44px;min-inline-size:44px;max-inline-size:100%;white-space:normal}
.chatcase-notice{margin:0;font-size:.8125rem}
.chatcase-notice[role=alert]{color:var(--hcm-color-danger,#b3261e)}
@media (prefers-reduced-motion:reduce){.chatcase,.chatcase *{transition:none;animation:none}}
`
