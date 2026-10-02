package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const agentDocInstructionLimit = 8000

func agentDocInstructionEditor(locale LocaleContext, id, text string, disabled bool) ui.Node {
	helperID := id + "-review-help"
	counterID := id + "-counter"
	validationID := id + "-validation"
	menuID := id + "-mentions"
	return html.Div(html.Props{Class: "persona-admin-editor-field persona-admin-instructions-field"},
		html.Label(html.Props{For: id}, ui.Text(agentDocInstructionText(locale, "label"))),
		html.Div(html.Props{Class: "agentdoc-instruction-control"},
			html.Textarea(html.Props{ID: id, Name: "instructions", Rows: 8, Disabled: disabled, Dir: "auto", Raw: map[string]any{
				"maxlength": agentDocInstructionLimit, "aria-describedby": helperID + " " + counterID + " " + validationID, "aria-controls": menuID, "aria-expanded": "false", "aria-autocomplete": "list", "data-agentdoc-instructions": "true",
			}}, ui.Text(text)),
			html.Div(html.Props{ID: menuID, Class: "agentdoc-mention-menu", Hidden: true, Raw: map[string]any{"data-agentdoc-mention-menu": "true", "data-agentdoc-active-index": "-1"}},
				html.P(html.Props{Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "data-agentdoc-mention-status": "true"}}, ui.Text("")),
				html.Ul(html.Props{Class: "agentdoc-mention-results", Role: "listbox", Raw: map[string]any{"data-agentdoc-mention-results": "true"}}),
			),
		),
		html.P(html.Props{ID: validationID, Class: "field-error", Hidden: true, Role: "alert", Raw: map[string]any{"data-agentdoc-instruction-validation": "true"}}, ui.Text("")),
		html.Div(html.Props{Class: "persona-admin-instructions-meta"},
			html.P(html.Props{ID: helperID, Class: "muted"}, ui.Text(agentDocInstructionText(locale, "review_help"))),
			html.P(html.Props{ID: counterID, Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "data-agentdoc-instruction-counter": id, "data-agentdoc-counter-template": agentDocInstructionText(locale, "counter")}}, ui.Text(agentDocInstructionCounter(locale, len([]rune(text))))),
		),
	)
}

// AgentDocInstructionValidationText names the visible mention that must be
// selected from the authorized document list before submission.
func AgentDocInstructionValidationText(locale LocaleContext, mention string) string {
	return strings.ReplaceAll(agentDocInstructionText(locale, "unknown_mention"), "{document}", mention)
}

func agentDocBuiltInInstructions(locale LocaleContext, id, text string) ui.Node {
	return html.Details(html.Props{Class: "persona-admin-built-in-instructions", Raw: map[string]any{"data-agentdoc-built-in": "true"}},
		html.Summary(html.Props{}, ui.Text(agentDocInstructionText(locale, "built_in_label"))),
		html.P(html.Props{ID: id, Dir: "auto", Raw: map[string]any{"data-agentdoc-built-in-text": "true"}}, ui.Text(text)),
	)
}

func agentDocInstructionCounter(locale LocaleContext, count int) string {
	return strings.NewReplacer("{count}", strconv.Itoa(count), "{limit}", strconv.Itoa(agentDocInstructionLimit)).Replace(agentDocInstructionText(locale, "counter"))
}

func agentDocInstructionText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"label":          {"Your instructions", "Ihre Anweisungen", "تعليماتك"},
		"built_in_label": {"Built-in instructions", "Integrierte Anweisungen", "التعليمات المضمنة"},
		"review_help": {"Tell the agent how to work for your organization. Type @ to mention a document it should follow. People only get this version after it is reviewed and evaluated.",
			"Sagen Sie dem Agenten, wie er für Ihre Organisation arbeiten soll. Geben Sie @ ein, um ein Dokument zu erwähnen, dem er folgen soll. Personen erhalten diese Version erst, nachdem sie geprüft und bewertet wurde.",
			"أخبر الوكيل بكيفية العمل لمؤسستك. اكتب @ للإشارة إلى مستند يجب أن يتبعه. لا يحصل الأشخاص على هذا الإصدار إلا بعد مراجعته وتقييمه."},
		"counter":         {"{count} of {limit} characters", "{count} von {limit} Zeichen", "{count} من {limit} حرفاً"},
		"unknown_mention": {"Choose “{document}” from the document list before creating this version.", "Wählen Sie „{document}“ aus der Dokumentliste aus, bevor Sie diese Version erstellen.", "اختر «{document}» من قائمة المستندات قبل إنشاء هذا الإصدار."},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
