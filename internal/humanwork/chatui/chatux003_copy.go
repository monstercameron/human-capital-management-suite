package chatui

import "strings"

// chatux003Text is the wording for the answer card's header note, the line that
// says why an answer is private, the share control and the "asked by" line, in
// the three languages the product speaks. {name} is the agent or the asker and
// {channel} the conversation the answer was asked in.
func chatux003Text(model Model, key string) string {
	// The product catalog answers a key it does not hold with the key in brackets
	// (⟦key⟧). That answer, or the key itself, is never copy: the table below is.
	language := "en-US"
	switch {
	case strings.HasPrefix(strings.ToLower(model.Locale), "de"):
		language = "de-DE"
	case strings.HasPrefix(strings.ToLower(model.Locale), "ar"):
		language = "ar"
	}
	return chatbug039Text(key, chatux003Copy[language][key], chatux003Copy["en-US"][key])
}

func chatux003Format(model Model, key string, values map[string]string) string {
	text := chatux003Text(model, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

var chatux003Copy = map[string]map[string]string{
	"en-US": {
		"chatux003.why.asked":              "You asked for this answer to stay private.",
		"chatux003.why.agent":              "{name} always answers privately.",
		"chatux003.why.channel":            "{channel} requires agent answers to be private.",
		"chatux003.share.refused.channel":  "{channel} requires agent answers to be private, so this stays private.",
		"chatux003.why.audience":           "Not everyone in {channel} can open the sources, so the answer stays private.",
		"chatux003.share":                  "Share to channel",
		"chatux003.share.hint":             "Post this answer to {channel} for everyone to read",
		"chatux003.sharing":                "Sharing…",
		"chatux003.shared":                 "Shared to {channel}",
		"chatux003.share.refused.agent":    "{name} always answers privately, so this stays private.",
		"chatux003.share.refused.audience": "Not everyone in {channel} can open the sources, so this stays private.",
		"chatux003.share.refused.source":   "{source} is not open to everyone in {channel}, so this stays private.",
		"chatux003.share.refused.expired":  "This answer is too old to share.",
		"chatux003.share.refused.denied":   "Only the person who asked can share this answer.",
		"chatux003.share.failed":           "Could not share this answer. Try again.",
		"chatux003.more":                   "More actions",
		"chatux003.asked_by":               "asked by {name}",
		"chatux003.ask_again":              "Ask again",
		"chatux003.channel_fallback":       "this channel",
		"chatux003.hint.public":            "Everyone in {channel} will see your question and {name}'s answer. To keep the answer to yourself, add \"keep this private\".",
		"chatux003.hint.agent":             "Everyone in {channel} will see your question. {name} always answers privately: only you will see the answer.",
	},
	"de-DE": {
		"chatux003.why.asked":              "Sie haben darum gebeten, dass diese Antwort privat bleibt.",
		"chatux003.why.agent":              "{name} antwortet immer privat.",
		"chatux003.why.channel":            "{channel} verlangt, dass Antworten von Agenten privat sind.",
		"chatux003.share.refused.channel":  "{channel} verlangt, dass Antworten von Agenten privat sind, daher bleibt dies privat.",
		"chatux003.why.audience":           "Nicht alle in {channel} können die Quellen öffnen, daher bleibt die Antwort privat.",
		"chatux003.share":                  "In den Kanal teilen",
		"chatux003.share.hint":             "Diese Antwort in {channel} für alle lesbar veröffentlichen",
		"chatux003.sharing":                "Wird geteilt…",
		"chatux003.shared":                 "Geteilt in {channel}",
		"chatux003.share.refused.agent":    "{name} antwortet immer privat, daher bleibt dies privat.",
		"chatux003.share.refused.audience": "Nicht alle in {channel} können die Quellen öffnen, daher bleibt dies privat.",
		"chatux003.share.refused.source":   "{source} ist nicht für alle in {channel} zugänglich, daher bleibt dies privat.",
		"chatux003.share.refused.expired":  "Diese Antwort ist zu alt, um geteilt zu werden.",
		"chatux003.share.refused.denied":   "Nur die Person, die gefragt hat, kann diese Antwort teilen.",
		"chatux003.share.failed":           "Diese Antwort konnte nicht geteilt werden. Versuchen Sie es erneut.",
		"chatux003.more":                   "Weitere Aktionen",
		"chatux003.asked_by":               "gefragt von {name}",
		"chatux003.ask_again":              "Erneut fragen",
		"chatux003.channel_fallback":       "diesem Kanal",
		"chatux003.hint.public":            "Alle in {channel} sehen Ihre Frage und die Antwort von {name}. Damit die Antwort privat bleibt, schreiben Sie „privat halten“.",
		"chatux003.hint.agent":             "Alle in {channel} sehen Ihre Frage. {name} antwortet immer privat: Nur Sie sehen die Antwort.",
	},
	"ar": {
		"chatux003.why.asked":              "طلبت أن تبقى هذه الإجابة خاصة.",
		"chatux003.why.agent":              "{name} يجيب دائمًا بشكل خاص.",
		"chatux003.why.channel":            "{channel} تشترط أن تكون إجابات الوكلاء خاصة.",
		"chatux003.share.refused.channel":  "{channel} تشترط أن تكون إجابات الوكلاء خاصة، لذلك يبقى هذا خاصًا.",
		"chatux003.why.audience":           "لا يستطيع الجميع في {channel} فتح المصادر، لذلك تبقى الإجابة خاصة.",
		"chatux003.share":                  "مشاركة في القناة",
		"chatux003.share.hint":             "انشر هذه الإجابة في {channel} ليقرأها الجميع",
		"chatux003.sharing":                "جارٍ المشاركة…",
		"chatux003.shared":                 "تمت المشاركة في {channel}",
		"chatux003.share.refused.agent":    "{name} يجيب دائمًا بشكل خاص، لذلك يبقى هذا خاصًا.",
		"chatux003.share.refused.audience": "لا يستطيع الجميع في {channel} فتح المصادر، لذلك يبقى هذا خاصًا.",
		"chatux003.share.refused.source":   "{source} غير متاح للجميع في {channel}، لذلك يبقى هذا خاصًا.",
		"chatux003.share.refused.expired":  "هذه الإجابة قديمة جدًا ولا يمكن مشاركتها.",
		"chatux003.share.refused.denied":   "لا يمكن مشاركة هذه الإجابة إلا لمن طرح السؤال.",
		"chatux003.share.failed":           "تعذرت مشاركة هذه الإجابة. حاول مرة أخرى.",
		"chatux003.more":                   "إجراءات أخرى",
		"chatux003.asked_by":               "بطلب من {name}",
		"chatux003.ask_again":              "اسأل مرة أخرى",
		"chatux003.channel_fallback":       "هذه القناة",
		"chatux003.hint.public":            "سيرى الجميع في {channel} سؤالك وإجابة {name}. لإبقاء الإجابة لك وحدك، أضف «اجعل هذا خاصا».",
		"chatux003.hint.agent":             "سيرى الجميع في {channel} سؤالك. {name} يجيب دائمًا بشكل خاص: ستراها أنت فقط.",
	},
}
