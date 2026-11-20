package chatui

import (
	"fmt"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3779, slice 1: legal holds. On the Chat settings page a workspace
// administrator places a hold on a conversation with a reason, sees the holds
// in force and the ones lifted, and releases one with a reason. The server
// decides who may do any of it and what a hold covers; this file draws what it
// answered. The page never shows message text or a person's id.

// ChatHoldItem is one hold as the server describes it.
type ChatHoldItem struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversation_id"`
	ConversationName string `json:"conversation_name"`
	Matter           string `json:"matter"`
	Reason           string `json:"reason"`
	PlacedAt         string `json:"placed_at"`
	PlacedByMe       bool   `json:"placed_by_me"`
	Active           bool   `json:"active"`
	ReleasedAt       string `json:"released_at"`
	ReleasedByMe     bool   `json:"released_by_me"`
	ReleaseReason    string `json:"release_reason"`
}

// ChatHoldChoice is a conversation the form offers.
type ChatHoldChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ChatHoldData is what the server answers: every hold, and the conversations
// the administrator can open and so can place a hold on.
type ChatHoldData struct {
	Holds         []ChatHoldItem   `json:"holds"`
	Conversations []ChatHoldChoice `json:"conversations"`
}

// ChatHoldModel is everything the page needs to draw. A nil handler draws its
// control disabled.
type ChatHoldModel struct {
	Locale string
	Data   ChatHoldData
	Loaded bool
	// Loading is true while a read or a change is on its way.
	Loading, Failed, Denied, Unavailable, SignedOut bool
	// Notice is "placed" or "released" after a change the server kept.
	Notice string
	// FormError is why the place form was turned away: "invalid", "missing" or
	// "exists". ReleaseError names the hold whose release form was turned away.
	FormError               string
	ReleaseError, ReleaseID string
	// Place submits the place form, Release submits one hold's release form and
	// Retry reads the page again after a failure.
	Place, Release, Retry ui.Handler
}

// ChatHoldText returns one string of the page in the person's language.
func ChatHoldText(locale, key string) string {
	values, ok := chatHoldCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
}

var chatHoldCopy = map[string][3]string{
	"title":           {"Legal holds", "Rechtliche Aufbewahrungspflichten", "الحجوزات القانونية"},
	"intro":           {"A legal hold keeps the evidence in one conversation. While it is in force, retention never removes anything in that conversation, and a message deleted there keeps a protected copy. People can still write, edit and delete as usual, and they are not told about the hold. Placing and releasing a hold are recorded in the audit log.", "Eine rechtliche Aufbewahrungspflicht sichert die Beweise einer Unterhaltung. Solange sie gilt, entfernt die Aufbewahrung dort nie etwas, und eine dort gelöschte Nachricht behält eine geschützte Kopie. Personen können weiterhin wie gewohnt schreiben, bearbeiten und löschen und werden über die Pflicht nicht informiert. Das Festlegen und Aufheben wird im Prüfprotokoll festgehalten.", "يحفظ الحجز القانوني الأدلة في محادثة واحدة. وطالما كان ساريًا لا تزيل سياسة الاحتفاظ أي شيء في تلك المحادثة، وتحتفظ الرسالة المحذوفة هناك بنسخة محمية. يمكن للأشخاص الكتابة والتعديل والحذف كالمعتاد ولا يُبلَّغون بالحجز. يُسجَّل فرض الحجز ورفعه في سجل التدقيق."},
	"loading":         {"Loading legal holds. Please wait.", "Rechtliche Aufbewahrungspflichten werden geladen. Bitte warten.", "جارٍ تحميل الحجوزات القانونية. يرجى الانتظار."},
	"error":           {"Legal holds could not load or save. Nothing was changed. Try again.", "Rechtliche Aufbewahrungspflichten konnten nicht geladen oder gespeichert werden. Es wurde nichts geändert. Versuchen Sie es erneut.", "تعذر تحميل الحجوزات القانونية أو حفظها. لم يتغير شيء. حاول مجددًا."},
	"signed_out":      {"You were signed out, so legal holds could not load or save. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnten die rechtlichen Aufbewahrungspflichten nicht geladen oder gespeichert werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذر تحميل الحجوزات القانونية أو حفظها. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"retry":           {"Try again", "Erneut versuchen", "حاول مجددًا"},
	"denied":          {"Only a workspace administrator can see or change legal holds.", "Nur Administratoren des Arbeitsbereichs können rechtliche Aufbewahrungspflichten sehen oder ändern.", "يمكن لمسؤول مساحة العمل فقط رؤية الحجوزات القانونية أو تغييرها."},
	"unavailable":     {"Legal holds are not set up on this server.", "Rechtliche Aufbewahrungspflichten sind auf diesem Server nicht eingerichtet.", "الحجوزات القانونية غير مُعدّة على هذا الخادم."},
	"active_list":     {"Holds in force", "Geltende Pflichten", "الحجوزات السارية"},
	"active_empty":    {"No legal holds are in force.", "Es gelten keine rechtlichen Aufbewahrungspflichten.", "لا توجد حجوزات قانونية سارية."},
	"released_list":   {"Released holds", "Aufgehobene Pflichten", "الحجوزات المرفوعة"},
	"unknown_conv":    {"A conversation you cannot open", "Eine Unterhaltung, die Sie nicht öffnen können", "محادثة لا يمكنك فتحها"},
	"matter":          {"Matter: %s", "Angelegenheit: %s", "القضية: %s"},
	"reason_line":     {"Reason: %s", "Grund: %s", "السبب: %s"},
	"placed":          {"Placed on %s", "Festgelegt am %s", "فُرض في %s"},
	"placed_you":      {"Placed by you on %s", "Von Ihnen festgelegt am %s", "فرضتَه في %s"},
	"released":        {"Released on %s", "Aufgehoben am %s", "رُفع في %s"},
	"released_you":    {"Released by you on %s", "Von Ihnen aufgehoben am %s", "رفعتَه في %s"},
	"release_reason":  {"Release reason: %s", "Grund der Aufhebung: %s", "سبب الرفع: %s"},
	"release_field":   {"Reason for releasing", "Grund der Aufhebung", "سبب الرفع"},
	"release_for":     {"Reason for releasing the hold on %s", "Grund für die Aufhebung der Pflicht für %s", "سبب رفع الحجز عن %s"},
	"release":         {"Release hold", "Pflicht aufheben", "رفع الحجز"},
	"release_for_btn": {"Release the hold on %s", "Pflicht für %s aufheben", "رفع الحجز عن %s"},
	"release_missing": {"Say why the hold is being released.", "Geben Sie an, warum die Pflicht aufgehoben wird.", "اذكر سبب رفع الحجز."},
	"release_gone":    {"That hold was already released or no longer exists.", "Diese Pflicht wurde bereits aufgehoben oder existiert nicht mehr.", "تم رفع هذا الحجز بالفعل أو لم يعد موجودًا."},
	"placed_notice":   {"Hold placed. It is recorded in the audit log.", "Pflicht festgelegt. Sie wurde im Prüfprotokoll festgehalten.", "تم فرض الحجز. وقد سُجّل في سجل التدقيق."},
	"released_notice": {"Hold released. It is recorded in the audit log.", "Pflicht aufgehoben. Sie wurde im Prüfprotokoll festgehalten.", "تم رفع الحجز. وقد سُجّل في سجل التدقيق."},
	"add":             {"Place a hold", "Pflicht festlegen", "فرض حجز"},
	"conversation":    {"Conversation", "Unterhaltung", "المحادثة"},
	"conv_hint":       {"Only conversations you can open are listed.", "Es werden nur Unterhaltungen aufgelistet, die Sie öffnen können.", "تُدرج المحادثات التي يمكنك فتحها فقط."},
	"conv_choose":     {"Choose a conversation", "Unterhaltung wählen", "اختر محادثة"},
	"conv_none":       {"You cannot open any conversation, so there is nothing to place a hold on.", "Sie können keine Unterhaltung öffnen, daher gibt es nichts, worauf eine Pflicht gelegt werden kann.", "لا يمكنك فتح أي محادثة، لذا لا يوجد ما يُفرض عليه حجز."},
	"direct":          {"Direct or group message", "Direkt- oder Gruppennachricht", "رسالة مباشرة أو جماعية"},
	"matter_field":    {"Matter reference (optional)", "Angelegenheit (optional)", "مرجع القضية (اختياري)"},
	"matter_hint":     {"For example the case or matter number your legal team uses.", "Zum Beispiel die Fall- oder Aktennummer Ihrer Rechtsabteilung.", "مثل رقم القضية أو الملف الذي يستخدمه فريقك القانوني."},
	"reason_field":    {"Reason for the hold", "Grund der Pflicht", "سبب الحجز"},
	"reason_hint":     {"Required. Up to 500 characters. It is kept in the audit log.", "Erforderlich. Bis zu 500 Zeichen. Er wird im Prüfprotokoll gespeichert.", "مطلوب. حتى 500 حرف. يُحفظ في سجل التدقيق."},
	"submit":          {"Place hold", "Pflicht festlegen", "فرض الحجز"},
	"invalid":         {"Check the reason: it is required and can be up to 500 characters.", "Prüfen Sie den Grund: Er ist erforderlich und darf bis zu 500 Zeichen lang sein.", "تحقق من السبب: فهو مطلوب ويمكن أن يصل إلى 500 حرف."},
	"missing":         {"Choose a conversation you can open.", "Wählen Sie eine Unterhaltung, die Sie öffnen können.", "اختر محادثة يمكنك فتحها."},
	"gone":            {"That conversation is not available to you, so no hold was placed.", "Diese Unterhaltung ist für Sie nicht verfügbar, daher wurde keine Pflicht festgelegt.", "هذه المحادثة غير متاحة لك، لذا لم يُفرض أي حجز."},
}

func chatHoldTime(locale, at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	return localize.FormatDay(locale, t.UTC(), true)
}

func chatHoldConversationName(locale string, kind, name string) string {
	switch {
	case name != "":
		return name
	case kind == "DIRECT" || kind == "GROUP":
		return ChatHoldText(locale, "direct")
	}
	return ChatHoldText(locale, "unknown_conv")
}

func chatHoldShell(m ChatHoldModel, body ...ui.Node) ui.Node {
	nodes := append([]ui.Node{html.H2(html.Props{ID: "chathold-title", Text: ChatHoldText(m.Locale, "title")})}, body...)
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chathold", Dir: direction(m.Locale), Data: map[string]string{"chathold": "panel"}, Aria: map[string]string{"labelledby": "chathold-title"}}, nodes...)
}

// ChatHoldSettings draws the legal holds for the Chat settings page. It is the
// shell (loading, refusal, failure with Try again) until the holds have been
// read, and nothing is guessed meanwhile.
func ChatHoldSettings(m ChatHoldModel) ui.Node {
	t := func(key string) string { return ChatHoldText(m.Locale, key) }
	switch {
	case m.Denied:
		return chatHoldShell(m, html.P(html.Props{Role: "alert", Text: t("denied")}))
	case m.Unavailable:
		return chatHoldShell(m, html.P(html.Props{Role: "status", Text: t("unavailable")}))
	case !m.Loaded && !m.Failed:
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chathold", Dir: direction(m.Locale), Role: "status", Aria: map[string]string{"busy": "true", "labelledby": "chathold-title"}},
			html.H2(html.Props{ID: "chathold-title", Text: t("title")}), html.P(html.Props{Text: t("loading")}))
	case m.Failed && !m.Loaded:
		key := "error"
		if m.SignedOut {
			key = "signed_out"
		}
		return chatHoldShell(m, html.P(html.Props{Role: "alert", Text: t(key)}),
			html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("retry"), OnClick: m.Retry, Disabled: m.Loading || m.Retry.Value() == nil}))
	}

	status := ""
	switch {
	case m.Loading:
		status = t("loading")
	case m.Failed && m.SignedOut:
		status = t("signed_out")
	case m.Failed:
		status = t("error")
	case m.Notice == "placed" || m.Notice == "released":
		status = t(m.Notice + "_notice")
	}

	kinds := map[string]string{}
	for _, c := range m.Data.Conversations {
		kinds[c.ID] = c.Kind
	}
	var active, released []ui.Node
	for _, h := range m.Data.Holds {
		name := chatHoldConversationName(m.Locale, kinds[h.ConversationID], h.ConversationName)
		lines := []ui.Node{html.Strong(html.Props{Dir: "auto", Text: name})}
		if h.Matter != "" {
			lines = append(lines, html.Span(html.Props{Class: "chathold-line", Dir: "auto", Text: fmt.Sprintf(t("matter"), h.Matter)}))
		}
		lines = append(lines, html.Span(html.Props{Class: "chathold-line", Dir: "auto", Text: fmt.Sprintf(t("reason_line"), h.Reason)}))
		placedKey := "placed"
		if h.PlacedByMe {
			placedKey = "placed_you"
		}
		lines = append(lines, html.Span(html.Props{Class: "chathold-line", Text: fmt.Sprintf(t(placedKey), chatHoldTime(m.Locale, h.PlacedAt))}))
		if !h.Active {
			releasedKey := "released"
			if h.ReleasedByMe {
				releasedKey = "released_you"
			}
			lines = append(lines, html.Span(html.Props{Class: "chathold-line", Text: fmt.Sprintf(t(releasedKey), chatHoldTime(m.Locale, h.ReleasedAt))}),
				html.Span(html.Props{Class: "chathold-line", Dir: "auto", Text: fmt.Sprintf(t("release_reason"), h.ReleaseReason)}))
			released = append(released, html.Li(html.Props{Class: "chatlangadmin-term chathold-item", Data: map[string]string{"chathold": "released"}},
				html.Div(html.Props{Class: "chatlangadmin-term-text"}, lines...)))
			continue
		}
		fieldID := "chathold-release-" + h.ID
		aria := map[string]string{}
		formNodes := []ui.Node{
			html.Input(html.Props{Type: "hidden", Name: "id", Value: h.ID}),
			html.Label(html.Props{For: fieldID, Text: fmt.Sprintf(t("release_for"), name)}),
		}
		if m.ReleaseError != "" && m.ReleaseID == h.ID {
			aria["invalid"] = "true"
			aria["describedby"] = fieldID + "-error"
		}
		formNodes = append(formNodes, html.Input(html.Props{ID: fieldID, Name: "reason", Type: "text", MaxLength: 500, Required: true, Disabled: m.Loading, Dir: "auto", Aria: aria, Raw: map[string]any{"autocomplete": "off"}}))
		if m.ReleaseError != "" && m.ReleaseID == h.ID {
			formNodes = append(formNodes, html.P(html.Props{ID: fieldID + "-error", Class: "field-hint field-error", Role: "alert", Text: t(m.ReleaseError)}))
		}
		formNodes = append(formNodes, html.Button(html.Props{Type: "submit", Class: "button secondary", Text: t("release"), Disabled: m.Loading || m.Release.Value() == nil, Aria: map[string]string{"label": fmt.Sprintf(t("release_for_btn"), name)}}))
		active = append(active, html.Li(html.Props{Class: "chatlangadmin-term chathold-item", Data: map[string]string{"chathold": "active"}},
			html.Div(html.Props{Class: "chatlangadmin-term-text"}, lines...),
			html.Form(html.Props{Class: "chatlangadmin-form chathold-release", Data: map[string]string{"chathold": "release"}, OnSubmit: m.Release}, formNodes...)))
	}

	activeList := ui.Node(html.P(html.Props{Class: "chathold-empty", Text: t("active_empty")}))
	if len(active) > 0 {
		activeList = html.Ul(html.Props{Class: "chatlangadmin-terms", Aria: map[string]string{"label": t("active_list")}}, active...)
	}

	options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: t("conv_choose")})}
	for _, c := range m.Data.Conversations {
		options = append(options, html.Option(html.Props{Raw: map[string]any{"value": c.ID}, Text: chatHoldConversationName(m.Locale, c.Kind, c.Name)}))
	}
	convAria := map[string]string{"describedby": "chathold-conv-hint"}
	reasonAria := map[string]string{"describedby": "chathold-reason-hint"}
	formError := []ui.Node{}
	if m.FormError != "" {
		target := reasonAria
		if m.FormError == "missing" || m.FormError == "gone" {
			target = convAria
		}
		target["invalid"] = "true"
		target["describedby"] += " chathold-error"
		formError = append(formError, html.P(html.Props{ID: "chathold-error", Class: "field-hint field-error", Role: "alert", Text: t(m.FormError)}))
	}
	hint := t("conv_hint")
	if len(m.Data.Conversations) == 0 {
		hint = t("conv_none")
	}
	form := []ui.Node{
		html.H3(html.Props{ID: "chathold-add-title", Text: t("add")}),
		html.Label(html.Props{For: "chathold-conversation", Text: t("conversation")}),
		html.Select(html.Props{ID: "chathold-conversation", Name: "conversation", Class: "chat-input", Required: true, Disabled: m.Loading || len(m.Data.Conversations) == 0, Aria: convAria}, options...),
		html.P(html.Props{ID: "chathold-conv-hint", Class: "field-hint", Text: hint}),
		html.Label(html.Props{For: "chathold-matter", Text: t("matter_field")}),
		html.Input(html.Props{ID: "chathold-matter", Name: "matter", Type: "text", MaxLength: 120, Disabled: m.Loading, Dir: "auto", Aria: map[string]string{"describedby": "chathold-matter-hint"}, Raw: map[string]any{"autocomplete": "off"}}),
		html.P(html.Props{ID: "chathold-matter-hint", Class: "field-hint", Text: t("matter_hint")}),
		html.Label(html.Props{For: "chathold-reason", Text: t("reason_field")}),
		html.Input(html.Props{ID: "chathold-reason", Name: "reason", Type: "text", MaxLength: 500, Required: true, Disabled: m.Loading, Dir: "auto", Aria: reasonAria, Raw: map[string]any{"autocomplete": "off"}}),
		html.P(html.Props{ID: "chathold-reason-hint", Class: "field-hint", Text: t("reason_hint")}),
	}
	form = append(form, formError...)
	form = append(form, html.Div(html.Props{Class: "chatlangadmin-actions"},
		html.Button(html.Props{Type: "submit", Class: "button", Text: t("submit"), Disabled: m.Loading || len(m.Data.Conversations) == 0 || m.Place.Value() == nil})))

	nodes := []ui.Node{
		html.P(html.Props{Class: "field-hint", Text: t("intro")}),
		html.P(html.Props{ID: "chathold-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}),
		html.H3(html.Props{Text: t("active_list")}),
		activeList,
	}
	if len(released) > 0 {
		nodes = append(nodes, html.H3(html.Props{Text: t("released_list")}),
			html.Ul(html.Props{Class: "chatlangadmin-terms", Aria: map[string]string{"label": t("released_list")}}, released...))
	}
	nodes = append(nodes, html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chathold": "place"}, OnSubmit: m.Place, Aria: map[string]string{"labelledby": "chathold-add-title"}}, form...))
	return chatHoldShell(m, nodes...)
}
