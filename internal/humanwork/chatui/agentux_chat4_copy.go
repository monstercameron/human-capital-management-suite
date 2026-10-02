package chatui

import "strings"

func agentUXChat4Text(model Model, key string) string {
	language := "en-US"
	if strings.HasPrefix(strings.ToLower(model.Locale), "de") {
		language = "de-DE"
	} else if strings.HasPrefix(strings.ToLower(model.Locale), "ar") {
		language = "ar"
	}
	copy := map[string]map[string]string{
		"en-US": {
			"chat.channel.public_short":   "Public",
			"chat.agent.ask_follow_up":    "Ask a follow-up",
			"chat.agent.thread_follow_up": "To ask {name} more, use Ask a follow-up.",
			"chat.agent.reply_everyone":   "Reply to everyone in {channel}",
			"chat.agent.private_short":    "Only you can see this",
			"chat.agent.channel_privacy":  "Everyone in {channel} will see your question. Only you will see {name}'s answer.",
			"chat.agent.suggest_mention":  "Did you mean @{name}?",
			"chat.agent.unresolved_plain": "Send as an ordinary message to everyone; no agent will answer.",
			"chat.agent.not_mentioned":    "{name} was not mentioned.",
			"chat.agent.ask_named":        "Ask {name}",
			"chat.agent.request_access":   "Ask People Operations for access",
			"chat.agent.access_draft":     "Please help me get access to the policy documents needed to answer my question.",
		},
		"de-DE": {
			"chat.channel.public_short":   "Öffentlich",
			"chat.agent.ask_follow_up":    "Folgefrage stellen",
			"chat.agent.thread_follow_up": "Für weitere Fragen an {name} wählen Sie „Folgefrage stellen“.",
			"chat.agent.reply_everyone":   "Allen in {channel} antworten",
			"chat.agent.private_short":    "Nur Sie können dies sehen",
			"chat.agent.channel_privacy":  "Alle in {channel} sehen Ihre Frage. Nur Sie sehen die Antwort von {name}.",
			"chat.agent.suggest_mention":  "Meinten Sie @{name}?",
			"chat.agent.unresolved_plain": "Als normale Nachricht an alle senden; kein Agent wird antworten.",
			"chat.agent.not_mentioned":    "{name} wurde nicht erwähnt.",
			"chat.agent.ask_named":        "{name} fragen",
			"chat.agent.request_access":   "People Operations um Zugriff bitten",
			"chat.agent.access_draft":     "Bitte helfen Sie mir, Zugriff auf die Richtliniendokumente für meine Frage zu erhalten.",
		},
		"ar": {
			"chat.channel.public_short":   "عام",
			"chat.agent.ask_follow_up":    "اطرح سؤال متابعة",
			"chat.agent.thread_follow_up": "لطرح المزيد من الأسئلة على {name}، استخدم «اطرح سؤال متابعة».",
			"chat.agent.reply_everyone":   "رد على الجميع في {channel}",
			"chat.agent.private_short":    "يمكنك وحدك رؤية هذا",
			"chat.agent.channel_privacy":  "سيرى الجميع في {channel} سؤالك. وسترى أنت فقط إجابة {name}.",
			"chat.agent.suggest_mention":  "هل تقصد @{name}؟",
			"chat.agent.unresolved_plain": "أرسل رسالة عادية إلى الجميع؛ لن يجيب أي وكيل.",
			"chat.agent.not_mentioned":    "لم تتم الإشارة إلى {name}.",
			"chat.agent.ask_named":        "اسأل {name}",
			"chat.agent.request_access":   "اطلب الوصول من فريق شؤون الموظفين",
			"chat.agent.access_draft":     "يرجى مساعدتي في الوصول إلى مستندات السياسات اللازمة للإجابة عن سؤالي.",
		},
	}
	return chatbug039Text(key, copy[language][key], copy["en-US"][key])
}

func agentUXChat4Format(model Model, key string, values map[string]string) string {
	copy := agentUXChat4Text(model, key)
	for key, value := range values {
		copy = strings.ReplaceAll(copy, "{"+key+"}", value)
	}
	return copy
}
