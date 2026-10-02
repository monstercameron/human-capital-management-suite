package chatui

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// personaReplyRowsForPost places recipient-only agent state immediately after
// the message that invoked it. The invocation and ephemeral answer are two
// transports for one row, never two rows.
func personaReplyRowsForPost(model Model, local localUI, postID string, now time.Time) []ui.Node {
	if strings.TrimSpace(postID) == "" {
		return nil
	}
	private := VisibleEphemeralMessages(model.EphemeralMessages, now)
	used := make(map[string]bool, len(private))
	rows := make([]ui.Node, 0, len(model.PersonaInvocations))
	for _, invocation := range model.PersonaInvocations {
		if invocation.PostID != postID || invocation.Projection.ViewerID != invocation.Projection.InvokerID || (model.CurrentUser != "" && invocation.Projection.ViewerID != model.CurrentUser) {
			continue
		}
		answered := false
		for _, message := range private {
			threadID := invocation.ThreadID
			if threadID == "" {
				threadID = postID
			}
			if used[message.ID] || message.ThreadID != threadID {
				continue
			}
			if !model.selected().Agent {
				rows = append(rows, html.WithKey(renderPersonaPrivateAnswer(model, local, message, agentReplyNamedProjection(model, invocation.Projection, postID)), "agent-answer:"+message.ID))
			}
			used[message.ID], answered = true, true
			break
		}
		if answered {
			continue
		}
		row := agentProgressForPost(model, invocation.Projection, postID)
		rows = append(rows, html.WithKey(row, "agent-state:"+invocation.Projection.InvocationID))
	}
	for _, message := range private {
		claimedByInvocation := false
		for _, invocation := range model.PersonaInvocations {
			threadID := invocation.ThreadID
			if threadID == "" {
				threadID = invocation.PostID
			}
			if threadID == message.ThreadID {
				claimedByInvocation = true
				break
			}
		}
		if !model.selected().Agent && !used[message.ID] && !claimedByInvocation && message.ThreadID == postID {
			rows = append(rows, html.WithKey(renderPersonaPrivateAnswer(model, local, message, PersonaProgressProjection{}), "agent-answer:"+message.ID))
		}
	}
	if len(rows) == 0 {
		// CHATBUG-040: room for the card that is still on its way.
		if reserve := chatbug040ReservedRow(model, postID); reserve != nil {
			rows = append(rows, reserve)
		}
	}
	return rows
}

// renderPersonaPrivateAnswer is the private answer card (CHATUX-003): see
// chatux003AnswerCard.
func renderPersonaPrivateAnswer(model Model, local localUI, message EphemeralMessage, projection PersonaProgressProjection) ui.Node {
	return chatux003AnswerCard(model, local, message, projection)
}

func agentReplyTime(model Model, at time.Time) ui.Node {
	if at.IsZero() {
		return nil
	}
	clock := chat5Clock(model.Locale, at)
	return html.Time(html.Props{Text: clock, Raw: map[string]any{"datetime": at.Format(time.RFC3339)}})
}

func renderAgentReplyIdentity(model Model, name string, identity ...agenticon.Value) ui.Node {
	if name = strings.TrimSpace(name); name == "" {
		name = personaProgressText(model, "chat.agent.name", "Agent")
	}
	value := agenticon.Value{}
	if len(identity) > 0 {
		value = identity[0]
	}
	if !value.Valid() {
		value = storedAgentIcon(model, nil, name)
	}
	if !value.Valid() && model.selected().Agent {
		value = model.selected().Icon
	}
	avatar := agentDMAvatar(name, "avatar small agent-reply-avatar", agentIconFor(model, nil, name, value))
	return html.Header(html.Props{Class: "agent-reply-identity"}, avatar, html.Strong(html.Props{Class: "agent-reply-name", Text: name}), AgentBadgeLabel(model.Locale))
}

func agentReplyDirection(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "ar") {
		return "rtl"
	}
	return "ltr"
}

func agentReplyFallback(locale, key, fallback string) string {
	copy := map[string]map[string]string{
		"en-US": {
			"chat.agent.badge": "Agent", "chat.agent.name": "Agent", "chat.agent.response_region": "Agent response", "chat.agent.working": "is working on this", "chat.agent.elapsed_seconds": "seconds", "chat.agent.only_visible": "Only visible to you", "chat.agent.open_conversation": "Open in your conversation with", "chat.agent.open_original": "Open the original message", "chat.agent.asked_in": "Asked in {conversation} · View question", "chat.agent.conversation": "the conversation", "chat.agent.follow_up": "Ask {name} a follow-up", "chat.agent.sources": "Sources", "chat.agent.try_again": "Try again",
			"chat.agent.finding_answer": "Finding an answer in your policy documents…", "chat.agent.still_working": "Still working…", "chat.agent.saved_conversation": "Saved in your conversation with", "chat.agent.subtitle": "Answers from policy documents · Only you can see this conversation", "chat.agent.private_note": "Only you can see this conversation", "chat.agent.failed_now": "{name} couldn't answer just now.", "chat.agent.thread_continue": "Reply to continue in this thread", "chat.agent.legacy_private": "The answer was sent privately.", "chat.agent.feedback_owner_named": "Thanks. {name}'s owner has been told.", "chat.agent.undo": "Undo", "chat.agent.you_asked_in": "You asked in", "chat.agent.view_in": "View in {conversation}", KeyCancel: "Cancel",
			"chat.agent.failed_model_unavailable": "The agent could not answer because the model is unavailable.", "chat.agent.failed_no_permission": "The agent could not answer because you do not have permission.", "chat.agent.failed_nothing_found": "The agent could not answer because it found nothing in the documents it may read.", "chat.agent.failed_timeout": "The agent could not answer because it took too long.", "chat.agent.failed_stopped": "The agent could not answer because an administrator stopped it.", "chat.agent.failed_generic": "The agent could not answer because something went wrong.",
			"chat.agent.feedback": "Rate this answer", "chat.agent.helpful": "Helpful", "chat.agent.not_right": "Not right", "chat.agent.feedback_owner": "Feedback sent to the agent's owner.", "chat.persona.awaiting_approval": "Awaiting approval", "chat.persona.open_task": "Open task", "chat.persona.task": "Task", "chat.agent.rating_not_saved": "Your rating was not saved. Try again.",
		},
		"de-DE": {
			"chat.agent.badge": "Agent", "chat.agent.name": "Agent", "chat.agent.response_region": "Antwort des Agenten", "chat.agent.working": "arbeitet daran", "chat.agent.elapsed_seconds": "Sekunden", "chat.agent.only_visible": "Nur für Sie sichtbar", "chat.agent.open_conversation": "In Ihrer Unterhaltung öffnen mit", "chat.agent.open_original": "Ursprüngliche Nachricht öffnen", "chat.agent.asked_in": "Gefragt in {conversation} · Frage ansehen", "chat.agent.conversation": "der Unterhaltung", "chat.agent.follow_up": "{name} eine Folgefrage stellen", "chat.agent.sources": "Quellen", "chat.agent.try_again": "Erneut versuchen",
			"chat.agent.finding_answer": "Eine Antwort wird in Ihren Richtliniendokumenten gesucht…", "chat.agent.still_working": "Wird noch bearbeitet…", "chat.agent.saved_conversation": "Gespeichert in Ihrer Unterhaltung mit", "chat.agent.subtitle": "Antworten aus Richtliniendokumenten · Nur Sie können diese Unterhaltung sehen", "chat.agent.private_note": "Nur Sie können diese Unterhaltung sehen", "chat.agent.failed_now": "{name} konnte gerade nicht antworten.", "chat.agent.thread_continue": "Antworten Sie, um diesen Thread fortzusetzen", "chat.agent.legacy_private": "Die Antwort wurde privat gesendet.", "chat.agent.feedback_owner_named": "Danke. Der Eigentümer von {name} wurde informiert.", "chat.agent.undo": "Rückgängig", "chat.agent.you_asked_in": "Sie fragten in", "chat.agent.view_in": "In {conversation} ansehen", KeyCancel: "Abbrechen",
			"chat.agent.failed_model_unavailable": "Der Agent konnte nicht antworten, weil das Modell nicht verfügbar ist.", "chat.agent.failed_no_permission": "Der Agent konnte nicht antworten, weil Sie keine Berechtigung haben.", "chat.agent.failed_nothing_found": "Der Agent konnte nicht antworten, weil er in den Dokumenten, die er lesen darf, nichts gefunden hat.", "chat.agent.failed_timeout": "Der Agent konnte nicht antworten, weil die Bearbeitung zu lange gedauert hat.", "chat.agent.failed_stopped": "Der Agent konnte nicht antworten, weil ein Administrator ihn gestoppt hat.", "chat.agent.failed_generic": "Der Agent konnte nicht antworten, weil etwas schiefgegangen ist.",
			"chat.agent.feedback": "Diese Antwort bewerten", "chat.agent.helpful": "Hilfreich", "chat.agent.not_right": "Nicht richtig", "chat.agent.feedback_owner": "Das Feedback wurde an den Eigentümer des Agenten gesendet.", "chat.persona.awaiting_approval": "Wartet auf Genehmigung", "chat.persona.open_task": "Aufgabe öffnen", "chat.persona.task": "Aufgabe", "chat.agent.rating_not_saved": "Ihre Bewertung wurde nicht gespeichert. Versuchen Sie es erneut.",
		},
		"ar": {
			"chat.agent.badge": "وكيل", "chat.agent.name": "الوكيل", "chat.agent.response_region": "رد الوكيل", "chat.agent.working": "يعمل على هذا", "chat.agent.elapsed_seconds": "ثانية", "chat.agent.only_visible": "مرئي لك فقط", "chat.agent.open_conversation": "فتح محادثتك مع", "chat.agent.open_original": "فتح الرسالة الأصلية", "chat.agent.asked_in": "طُرح في {conversation} · عرض السؤال", "chat.agent.conversation": "المحادثة", "chat.agent.follow_up": "اطرح سؤال متابعة على {name}", "chat.agent.sources": "المصادر", "chat.agent.try_again": "حاول مرة أخرى",
			"chat.agent.finding_answer": "جارٍ البحث عن إجابة في مستندات السياسات…", "chat.agent.still_working": "ما زال العمل جاريًا…", "chat.agent.saved_conversation": "محفوظ في محادثتك مع", "chat.agent.subtitle": "إجابات من مستندات السياسات · يمكنك وحدك رؤية هذه المحادثة", "chat.agent.private_note": "يمكنك وحدك رؤية هذه المحادثة", "chat.agent.failed_now": "تعذر على {name} الإجابة الآن.", "chat.agent.thread_continue": "رد للمتابعة في سلسلة الرسائل هذه", "chat.agent.legacy_private": "تم إرسال الإجابة بشكل خاص.", "chat.agent.feedback_owner_named": "شكرًا. تم إبلاغ مالك {name}.", "chat.agent.undo": "تراجع", "chat.agent.you_asked_in": "طرحت سؤالًا في", "chat.agent.view_in": "عرض في {conversation}", KeyCancel: "إلغاء",
			"chat.agent.failed_model_unavailable": "تعذر على الوكيل الإجابة لأن النموذج غير متاح.", "chat.agent.failed_no_permission": "تعذر على الوكيل الإجابة لأنك لا تملك الإذن.", "chat.agent.failed_nothing_found": "تعذر على الوكيل الإجابة لأنه لم يجد شيئًا في المستندات التي يمكنه قراءتها.", "chat.agent.failed_timeout": "تعذر على الوكيل الإجابة لأن الطلب استغرق وقتًا طويلًا.", "chat.agent.failed_stopped": "تعذر على الوكيل الإجابة لأن أحد المسؤولين أوقفه.", "chat.agent.failed_generic": "تعذر على الوكيل الإجابة بسبب حدوث خطأ.",
			"chat.agent.feedback": "قيّم هذه الإجابة", "chat.agent.helpful": "مفيد", "chat.agent.not_right": "غير صحيح", "chat.agent.feedback_owner": "تم إرسال الملاحظات إلى مالك الوكيل.", "chat.persona.awaiting_approval": "بانتظار الموافقة", "chat.persona.open_task": "فتح المهمة", "chat.persona.task": "مهمة", "chat.agent.rating_not_saved": "لم يتم حفظ تقييمك. حاول مرة أخرى.",
		},
	}
	language := "en-US"
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		language = "de-DE"
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		language = "ar"
	}
	return chatbug039Text(key, copy[language][key], chatbug039Text(key, copy["en-US"][key], fallback))
}
