package chatui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3783: information barriers. A workspace administrator keeps two teams
// apart on the Chat settings page: people from the two teams can never be in
// one conversation. The page lists the barriers in force, adds one from two
// teams of the people directory and removes one. The server decides who may
// read or change any of it; this file only draws what it answered.

// ChatBarrierRuleView is one barrier as the page shows it.
type ChatBarrierRuleView struct {
	ID     string
	GroupA string
	GroupB string
}

// ChatBarrierData is what the server answers: the barriers in force and the
// teams of the people directory a new barrier can name.
type ChatBarrierData struct {
	Rules []ChatBarrierRuleView
	Teams []string
}

// ChatBarrierModel is everything the page needs to draw. A nil handler draws
// its control disabled.
type ChatBarrierModel struct {
	Locale string
	Data   ChatBarrierData
	Loaded bool
	// Loading is true while a read or a change is on its way.
	Loading, Failed, Denied, Unavailable, SignedOut bool
	// Notice is "added" or "removed" after a change the server kept.
	Notice string
	// FormError is why the add form was turned away: "invalid", "unknown",
	// "duplicate" or "limit". Empty when it was not.
	FormError string
	// Add submits the add form, Remove submits one barrier's remove form and
	// Retry reads the page again after a failure.
	Add, Remove, Retry ui.Handler
}

// ChatBarrierText returns one string of the page in the person's language.
func ChatBarrierText(locale, key string) string {
	values, ok := chatBarrierCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
}

// BarrierRefusalNotice is what the add-people dialog says when the server
// refused because of an information barrier. It names no team and no person.
func BarrierRefusalNotice(locale string) string { return ChatBarrierText(locale, "refused") }

var chatBarrierCopy = map[string][3]string{
	"title":           {"Information barriers", "Informationsbarrieren", "حواجز المعلومات"},
	"intro":           {"Keep two teams apart: people from those two teams can never be in the same conversation. A barrier is checked when someone is added to a conversation or a new one is made. It does not remove people who are already together. Every change is recorded in the audit log.", "Halten Sie zwei Teams getrennt: Personen dieser beiden Teams können nie in derselben Unterhaltung sein. Eine Barriere wird geprüft, wenn jemand zu einer Unterhaltung hinzugefügt oder eine neue erstellt wird. Personen, die bereits zusammen sind, werden nicht entfernt. Jede Änderung wird im Prüfprotokoll festgehalten.", "أبقِ فريقين منفصلين: لا يمكن لأشخاص من هذين الفريقين أن يكونوا معًا في المحادثة نفسها. يُفحص الحاجز عند إضافة شخص إلى محادثة أو إنشاء محادثة جديدة، ولا يُزيل الأشخاص الموجودين معًا بالفعل. يُسجَّل كل تغيير في سجل التدقيق."},
	"loading":         {"Loading information barriers. Please wait.", "Informationsbarrieren werden geladen. Bitte warten.", "جارٍ تحميل حواجز المعلومات. يرجى الانتظار."},
	"error":           {"Information barriers could not load or save. Try again.", "Informationsbarrieren konnten nicht geladen oder gespeichert werden. Versuchen Sie es erneut.", "تعذر تحميل حواجز المعلومات أو حفظها. حاول مجددًا."},
	"signed_out":      {"You were signed out, so information barriers could not load or save. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnten die Informationsbarrieren nicht geladen oder gespeichert werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذر تحميل حواجز المعلومات أو حفظها. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"retry":           {"Try again", "Erneut versuchen", "حاول مجددًا"},
	"denied":          {"Only a workspace administrator can see or change information barriers.", "Nur Administratoren des Arbeitsbereichs können Informationsbarrieren sehen oder ändern.", "يمكن لمسؤول مساحة العمل فقط رؤية حواجز المعلومات أو تغييرها."},
	"unavailable":     {"Information barriers are not set up on this server, because it has no people directory to read teams from.", "Informationsbarrieren sind auf diesem Server nicht eingerichtet, da er kein Personenverzeichnis hat, aus dem Teams gelesen werden können.", "حواجز المعلومات غير مُعدّة على هذا الخادم لأنه لا يملك دليل أشخاص تُقرأ منه الفرق."},
	"list":            {"Barriers in force", "Geltende Barrieren", "الحواجز السارية"},
	"empty":           {"No barriers are set. Any team can share a conversation with any other team.", "Es sind keine Barrieren festgelegt. Jedes Team kann eine Unterhaltung mit jedem anderen Team teilen.", "لا توجد حواجز. يمكن لأي فريق مشاركة محادثة مع أي فريق آخر."},
	"row":             {"%s and %s cannot share a conversation", "%s und %s können keine Unterhaltung teilen", "لا يمكن لـ %s و%s مشاركة محادثة"},
	"remove":          {"Remove", "Entfernen", "إزالة"},
	"remove_for":      {"Remove the barrier between %s and %s", "Barriere zwischen %s und %s entfernen", "إزالة الحاجز بين %s و%s"},
	"added":           {"Barrier added.", "Barriere hinzugefügt.", "تمت إضافة الحاجز."},
	"removed":         {"Barrier removed.", "Barriere entfernt.", "تمت إزالة الحاجز."},
	"add":             {"Add a barrier", "Barriere hinzufügen", "إضافة حاجز"},
	"team_a":          {"First team", "Erstes Team", "الفريق الأول"},
	"team_b":          {"Second team", "Zweites Team", "الفريق الثاني"},
	"team_hint":       {"Choose a team from the people directory.", "Wählen Sie ein Team aus dem Personenverzeichnis.", "اختر فريقًا من دليل الأشخاص."},
	"submit":          {"Add barrier", "Barriere hinzufügen", "إضافة الحاجز"},
	"invalid":         {"Choose two different teams.", "Wählen Sie zwei verschiedene Teams.", "اختر فريقين مختلفين."},
	"unknown":         {"One of those is not a team in the people directory. Choose from the suggestions.", "Eines davon ist kein Team im Personenverzeichnis. Wählen Sie aus den Vorschlägen.", "أحدهما ليس فريقًا في دليل الأشخاص. اختر من الاقتراحات."},
	"duplicate":       {"There is already a barrier between those two teams.", "Zwischen diesen beiden Teams besteht bereits eine Barriere.", "يوجد بالفعل حاجز بين هذين الفريقين."},
	"limit":           {"This workspace has reached its limit of 200 barriers.", "Dieser Arbeitsbereich hat sein Limit von 200 Barrieren erreicht.", "بلغت مساحة العمل هذه حدّها البالغ 200 حاجز."},
	"refused_partial": {"The others you picked were added.", "Die übrigen ausgewählten Personen wurden hinzugefügt.", "تمت إضافة الأشخاص الآخرين الذين اخترتهم."},
	"refused":         {"An information barrier does not allow these people to be in one conversation.", "Eine Informationsbarriere erlaubt es nicht, dass diese Personen in einer Unterhaltung sind.", "يمنع حاجز معلومات وجود هؤلاء الأشخاص في محادثة واحدة."},
}

func chatBarrierShell(m ChatBarrierModel, body ...ui.Node) ui.Node {
	nodes := append([]ui.Node{html.H2(html.Props{ID: "chatbarrier-title", Text: ChatBarrierText(m.Locale, "title")})}, body...)
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatbarrier", Dir: direction(m.Locale), Aria: map[string]string{"labelledby": "chatbarrier-title"}}, nodes...)
}

// ChatBarrierSettings draws the information barriers for the Chat settings
// page. It is the shell (loading, refusal, failure with Try again) until the
// barriers have been read, and nothing is guessed meanwhile.
func ChatBarrierSettings(m ChatBarrierModel) ui.Node {
	t := func(key string) string { return ChatBarrierText(m.Locale, key) }
	switch {
	case m.Denied:
		return chatBarrierShell(m, html.P(html.Props{Role: "alert", Text: t("denied")}))
	case m.Unavailable:
		return chatBarrierShell(m, html.P(html.Props{Role: "status", Text: t("unavailable")}))
	case !m.Loaded && !m.Failed:
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatbarrier", Dir: direction(m.Locale), Role: "status", Aria: map[string]string{"busy": "true", "labelledby": "chatbarrier-title"}},
			html.H2(html.Props{ID: "chatbarrier-title", Text: t("title")}), html.P(html.Props{Text: t("loading")}))
	case m.Failed && !m.Loaded:
		key := "error"
		if m.SignedOut {
			key = "signed_out"
		}
		return chatBarrierShell(m, html.P(html.Props{Role: "alert", Text: t(key)}),
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
	case m.Notice == "added" || m.Notice == "removed":
		status = t(m.Notice)
	}

	var list ui.Node
	if len(m.Data.Rules) == 0 {
		list = html.P(html.Props{Class: "chatbarrier-empty", Text: t("empty")})
	} else {
		rows := make([]ui.Node, 0, len(m.Data.Rules))
		for _, r := range m.Data.Rules {
			rows = append(rows, html.Li(html.Props{Class: "chatlangadmin-term chatbarrier-rule"},
				html.Span(html.Props{Class: "chatlangadmin-term-text", Dir: "auto", Text: fmt.Sprintf(t("row"), r.GroupA, r.GroupB)}),
				html.Form(html.Props{Class: "chatbarrier-remove", Data: map[string]string{"chatbarrier": "remove"}, OnSubmit: m.Remove},
					html.Input(html.Props{Type: "hidden", Name: "id", Value: r.ID}),
					html.Button(html.Props{Type: "submit", Class: "button secondary", Text: t("remove"), Disabled: m.Loading || m.Remove.Value() == nil, Aria: map[string]string{"label": fmt.Sprintf(t("remove_for"), r.GroupA, r.GroupB)}}))))
		}
		list = html.Ul(html.Props{Class: "chatlangadmin-terms", Aria: map[string]string{"label": t("list")}}, rows...)
	}

	suggestions := make([]ui.Node, 0, len(m.Data.Teams))
	for _, team := range m.Data.Teams {
		suggestions = append(suggestions, html.Option(html.Props{Value: team}))
	}
	invalid := map[string]string{"describedby": "chatbarrier-team-hint"}
	formError := []ui.Node{}
	if m.FormError != "" {
		invalid["invalid"] = "true"
		invalid["describedby"] = "chatbarrier-error chatbarrier-team-hint"
		formError = append(formError, html.P(html.Props{ID: "chatbarrier-error", Class: "field-hint field-error", Role: "alert", Text: t(m.FormError)}))
	}
	team := func(id, name, label string) []ui.Node {
		return []ui.Node{
			html.Label(html.Props{For: id, Text: label}),
			html.Input(html.Props{ID: id, Name: name, Type: "text", MaxLength: 80, Required: true, Disabled: m.Loading, Dir: "auto", Aria: invalid, Raw: map[string]any{"list": "chatbarrier-teams", "autocomplete": "off"}}),
		}
	}
	form := []ui.Node{html.H3(html.Props{ID: "chatbarrier-add-title", Text: t("add")})}
	form = append(form, team("chatbarrier-team-a", "groupa", t("team_a"))...)
	form = append(form, team("chatbarrier-team-b", "groupb", t("team_b"))...)
	form = append(form, formError...)
	form = append(form,
		html.Datalist(html.Props{ID: "chatbarrier-teams"}, suggestions...),
		html.P(html.Props{ID: "chatbarrier-team-hint", Class: "field-hint", Text: t("team_hint")}),
		html.Div(html.Props{Class: "chatlangadmin-actions"}, html.Button(html.Props{Type: "submit", Class: "button", Text: t("submit"), Disabled: m.Loading || m.Add.Value() == nil})))

	return chatBarrierShell(m,
		html.P(html.Props{Class: "field-hint", Text: t("intro")}),
		html.P(html.Props{ID: "chatbarrier-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}),
		html.H3(html.Props{Text: t("list")}),
		list,
		html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatbarrier": "add"}, OnSubmit: m.Add, Aria: map[string]string{"labelledby": "chatbarrier-add-title"}}, form...))
}
