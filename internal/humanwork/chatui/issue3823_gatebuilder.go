package chatui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3823: the owner's joining-questions page is called what the channel's
// details call it, names the channel, and uses plain words for its fields.
func init() {
	for locale, words := range map[string]map[string]string{
		"en-US": {"joining": "Joining questions", "joiningFor": "Joining questions for #%s", "backTo": "Back to #%s", "crumbs": "Where you are",
			"formPurpose": "What this form is for", "askWhy": "Why you ask this", "answerType": "Type of answer", "privacy": "How private the answer is",
			"alsoShare": "Also share answers with", "tryAs": "Preview as", "tryAsNone": "Choose a person", "admissionMode": "How people get in"},
		"de-DE": {"joining": "Beitrittsfragen", "joiningFor": "Beitrittsfragen für #%s", "backTo": "Zurück zu #%s", "crumbs": "Ihr Standort",
			"formPurpose": "Wofür dieses Formular ist", "askWhy": "Warum Sie das fragen", "answerType": "Antworttyp", "privacy": "Wie vertraulich die Antwort ist",
			"alsoShare": "Antworten zusätzlich teilen mit", "tryAs": "Vorschau als", "tryAsNone": "Person auswählen", "admissionMode": "Wie Personen beitreten"},
		"ar": {"joining": "أسئلة الانضمام", "joiningFor": "أسئلة الانضمام إلى #%s", "backTo": "العودة إلى #%s", "crumbs": "موقعك الحالي",
			"formPurpose": "الغرض من هذا النموذج", "askWhy": "سبب طرح هذا السؤال", "answerType": "نوع الإجابة", "privacy": "مدى خصوصية الإجابة",
			"alsoShare": "مشاركة الإجابات أيضًا مع", "tryAs": "معاينة بصفة", "tryAsNone": "اختر شخصًا", "admissionMode": "كيف ينضم الأشخاص"},
	} {
		for key, value := range words {
			chatgateExtraCopy[locale][key] = value
		}
	}
}

// GateTitle is the page's heading: "Joining questions for #sales".
func GateTitle(v GateView) string {
	if name := strings.TrimSpace(v.ChannelName); name != "" {
		return fmt.Sprintf(GateText(v.Locale, "joiningFor"), name)
	}
	return GateText(v.Locale, "joining")
}

// gateBreadcrumb leads back to the channel; the current page is the last step.
func gateBreadcrumb(v GateView) ui.Node {
	name := strings.TrimSpace(v.ChannelName)
	if name == "" {
		return nil
	}
	back := fmt.Sprintf(GateText(v.Locale, "backTo"), name)
	return html.Nav(html.Props{Class: "chatgate-crumbs", Aria: map[string]string{"label": GateText(v.Locale, "crumbs")}},
		html.Button(html.Props{Type: "button", Class: "chatgate-crumb", Text: "#" + name, Aria: map[string]string{"label": back}, Data: map[string]string{"gate-action": "close", "id": ""}}),
		html.Span(html.Props{Text: " › ", Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Text: GateText(v.Locale, "joining"), Aria: map[string]string{"current": "page"}}))
}

// GateFullName is a person's full name: the legal name, with the name they go
// by in place of the legal given name when it differs ("Bob Smith").
func GateFullName(preferred, legal string) string {
	preferred, legal = strings.TrimSpace(preferred), strings.TrimSpace(legal)
	switch {
	case legal == "":
		return preferred
	case preferred == "" || strings.Contains(strings.ToLower(legal), strings.ToLower(preferred)):
		return legal
	}
	words := strings.Fields(legal)
	if len(words) < 2 {
		return preferred
	}
	return preferred + " " + strings.Join(words[1:], " ")
}
