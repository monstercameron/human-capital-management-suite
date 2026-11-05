package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentStudioSuggestionChangeView is one AI-proposed edit as the owner
// reviews it: the field, the current and proposed text, and the reason and
// uncertainty the model gave. It carries no authority; the owner selects
// which changes to send back through the ordinary version path.
type AgentStudioSuggestionChangeView struct {
	Field       string
	Current     string
	Proposed    string
	Reason      string
	Uncertainty float64
}

// AgentStudioSuggestionReview binds a set of proposed edits to the exact
// draft revision the model saw. CanApply mirrors the server's current
// version-creation grant for the viewer; without it the panel explains
// itself and offers no form.
type AgentStudioSuggestionReview struct {
	PersonaID   string
	PersonaName string
	Revision    uint64
	Digest      string
	CanApply    bool
	Changes     []AgentStudioSuggestionChangeView
}

// agentStudioSuggestionFields is the same presentation-only allowlist the
// service enforces: AI output may touch the handle, display name, purpose,
// and channel classes, and nothing that grants authority.
func agentStudioSuggestionFields(field string) bool {
	switch field {
	case "handle", "display_name", "purpose", "channel_classes":
		return true
	default:
		return false
	}
}

// RenderAgentStudioSuggestions renders the owner's review of an AI-proposed
// draft refinement. Every change shows its current and proposed text with
// the model's reason and uncertainty next to one checkbox; a single submit
// applies only the checked changes to the bound revision. A suggestion that
// names any other field is refused as a whole, exactly as the service does,
// so the panel can never hide a scope expansion behind a friendly diff.
func RenderAgentStudioSuggestions(locale LocaleContext, review AgentStudioSuggestionReview) ui.Node {
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	if strings.TrimSpace(review.PersonaID) == "" || review.Revision == 0 || strings.TrimSpace(review.Digest) == "" {
		return html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(agentStudioSuggestionText(locale, "unavailable")))
	}
	if len(review.Changes) == 0 {
		return html.Section(html.Props{Class: "surface agent-studio-suggestions", Raw: map[string]any{"data-agent-studio-suggestions": review.PersonaID, "data-agent-studio-revision": strconv.FormatUint(review.Revision, 10), "data-agent-studio-digest": review.Digest}},
			html.H3(html.Props{}, ui.Text(agentStudioSuggestionText(locale, "title"))),
			html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(agentStudioSuggestionText(locale, "empty"))),
		)
	}
	for _, change := range review.Changes {
		if !agentStudioSuggestionFields(change.Field) {
			return html.Section(html.Props{Class: "surface agent-studio-suggestions", Raw: map[string]any{"data-agent-studio-suggestions": review.PersonaID, "data-agent-studio-revision": strconv.FormatUint(review.Revision, 10), "data-agent-studio-digest": review.Digest}},
				html.H3(html.Props{}, ui.Text(agentStudioSuggestionText(locale, "title"))),
				html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(agentStudioSuggestionText(locale, "refused"))),
			)
		}
	}
	if !review.CanApply {
		return html.Section(html.Props{Class: "surface agent-studio-suggestions", Raw: map[string]any{"data-agent-studio-suggestions": review.PersonaID, "data-agent-studio-revision": strconv.FormatUint(review.Revision, 10), "data-agent-studio-digest": review.Digest}},
			html.H3(html.Props{}, ui.Text(agentStudioSuggestionText(locale, "title"))),
			html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(agentStudioSuggestionText(locale, "denied"))),
		)
	}
	id := "agent-studio-suggest-" + safeAgentDOMToken(review.PersonaID)
	statusID := id + "-status"
	rows := make([]ui.Node, 0, len(review.Changes))
	for _, change := range review.Changes {
		rows = append(rows, agentStudioSuggestionRow(locale, id, change))
	}
	evidence := strings.NewReplacer(
		"{agent}", review.PersonaName,
		"{revision}", locale.FormatNumber(strconv.FormatUint(review.Revision, 10), 0),
	).Replace(agentStudioSuggestionText(locale, "evidence"))
	return html.Section(html.Props{ID: id, Class: "surface agent-studio-suggestions", Raw: map[string]any{"data-agent-studio-suggestions": review.PersonaID, "data-agent-studio-revision": strconv.FormatUint(review.Revision, 10), "data-agent-studio-digest": review.Digest, "aria-labelledby": id + "-title"}},
		html.H3(html.Props{ID: id + "-title"}, ui.Text(agentStudioSuggestionText(locale, "title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(evidence)),
		html.P(html.Props{Class: "muted"}, ui.Text(agentStudioSuggestionText(locale, "manual_contract"))),
		html.Form(html.Props{ID: id + "-form", Class: "agent-studio-suggestions-form", Raw: map[string]any{"data-agent-studio-apply-form": review.PersonaID, "aria-describedby": statusID}},
			html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: review.PersonaID}),
			html.Input(html.Props{Name: "revision", Type: "hidden", Value: strconv.FormatUint(review.Revision, 10)}),
			html.Input(html.Props{Name: "digest", Type: "hidden", Value: review.Digest}),
			html.Ul(html.Props{Class: "agent-studio-suggestions-list"}, rows...),
			html.P(html.Props{ID: statusID, Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true", "tabindex": "-1"}}, ui.Text(agentStudioSuggestionText(locale, "pending"))),
			html.Button(html.Props{Class: "button primary", Type: "submit", Raw: map[string]any{"data-agent-studio-apply": review.PersonaID}}, ui.Text(agentStudioSuggestionText(locale, "apply"))),
		),
	)
}

func agentStudioSuggestionRow(locale LocaleContext, panelID string, change AgentStudioSuggestionChangeView) ui.Node {
	id := panelID + "-" + change.Field
	uncertainty := locale.FormatNumber(strconv.Itoa(int(change.Uncertainty*100+0.5)), 0) + "%"
	detail := strings.NewReplacer(
		"{reason}", change.Reason,
		"{uncertainty}", uncertainty,
	).Replace(agentStudioSuggestionText(locale, "change_detail"))
	return html.Li(html.Props{Class: "agent-studio-suggestion"},
		html.Div(html.Props{Class: "agent-studio-suggestion-pick"},
			html.Input(html.Props{ID: id, Name: "fields", Type: "checkbox", Value: change.Field, Raw: map[string]any{"data-agent-studio-change": change.Field, "data-agent-studio-reason": change.Reason, "data-agent-studio-uncertainty": strconv.FormatFloat(change.Uncertainty, 'f', -1, 64)}}),
			html.Label(html.Props{For: id}, ui.Text(agentStudioSuggestionText(locale, "field_"+change.Field))),
		),
		html.Div(html.Props{Class: "agent-studio-suggestion-diff", Dir: "auto"},
			html.Span(html.Props{Class: "agent-studio-suggestion-current"}, ui.Text(change.Current)),
			html.Span(html.Props{Class: "agent-studio-suggestion-arrow", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text("→")),
			html.Span(html.Props{Class: "agent-studio-suggestion-proposed"}, ui.Text(change.Proposed)),
		),
		html.P(html.Props{Class: "muted agent-studio-suggestion-reason", Dir: "auto"}, ui.Text(detail)),
	)
}

func agentStudioSuggestionText(locale LocaleContext, key string) string {
	translations := map[string][3]string{
		"title":           {"Suggested refinements", "Vorgeschlagene Überarbeitungen", "تحسينات مقترحة"},
		"evidence":        {"Suggested for {agent}, draft revision {revision}. Nothing here is applied yet.", "Vorgeschlagen für {agent}, Entwurfsversion {revision}. Nichts davon ist angewendet.", "مقترح لـ {agent}، مراجعة المسودة {revision}. لم يُطبَّق أي شيء بعد."},
		"manual_contract": {"Tick the changes to keep and apply them. The ordinary draft check, evaluation, reviewer, and publication steps still apply to whatever you accept.", "Wählen Sie die zu übernehmenden Änderungen aus und wenden Sie sie an. Die üblichen Schritte Prüfung, Bewertung, Freigabe und Veröffentlichung gelten weiterhin für alles, was Sie übernehmen.", "حدد التغييرات التي تريد الاحتفاظ بها وطبّقها. تظل خطوات فحص المسودة والتقييم والمراجعة والنشر المعتادة سارية على كل ما تقبله."},
		"pending":         {"No change is selected yet.", "Noch ist keine Änderung ausgewählt.", "لم يُحدَّد أي تغيير بعد."},
		"apply":           {"Apply selected changes", "Ausgewählte Änderungen anwenden", "تطبيق التغييرات المحددة"},
		"change_detail":   {"Why: {reason} (Uncertainty: {uncertainty})", "Warum: {reason} (Unsicherheit: {uncertainty})", "السبب: {reason} (عدم اليقين: {uncertainty})"},
		"field_handle":    {"Agent address", "Agentenadresse", "عنوان الوكيل"}, "field_display_name": {"Name", "Name", "الاسم"}, "field_purpose": {"Purpose", "Zweck", "الغرض"}, "field_channel_classes": {"Channels", "Kanäle", "القنوات"},
		"refused":     {"This suggestion asks for authority the assistant may not propose, such as skills, instructions, audience, tier, data reach, or evaluation. Nothing was applied. Change those fields through the usual review instead.", "Dieser Vorschlag verlangt Befugnisse, die der Assistent nicht vorschlagen darf, etwa Fähigkeiten, Anweisungen, Zielgruppe, Stufe, Datenreichweite oder Bewertung. Nichts wurde angewendet. Ändern Sie diese Felder stattdessen über die übliche Prüfung.", "يطلب هذا الاقتراح صلاحيات لا يجوز للمساعد اقتراحها، مثل المهارات أو التعليمات أو الجمهور أو المستوى أو نطاق البيانات أو التقييم. لم يُطبَّق شيء. غيّر هذه الحقول عبر المراجعة المعتادة بدلاً من ذلك."},
		"denied":      {"Only the agent's owner or steward can apply suggested refinements.", "Nur die fachverantwortliche Person oder der technische Kontakt des Agenten kann vorgeschlagene Überarbeitungen anwenden.", "يمكن لمالك الوكيل أو المشرف عليه فقط تطبيق التحسينات المقترحة."},
		"empty":       {"No suggested refinements for this draft yet.", "Noch keine vorgeschlagenen Überarbeitungen für diesen Entwurf.", "لا توجد تحسينات مقترحة لهذه المسودة بعد."},
		"unavailable": {"Suggested refinements are unavailable for this draft.", "Vorgeschlagene Überarbeitungen sind für diesen Entwurf nicht verfügbar.", "التحسينات المقترحة غير متاحة لهذه المسودة."},
	}
	values, ok := translations[key]
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
