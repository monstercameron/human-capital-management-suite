package chatui

import "strings"

// CHATMOD-002: what a person is told when a workspace language filter acts on
// their text. Reviewed en-US, de-DE and ar copy uses the shared copy guard.

// Surfaces a blocked sentence can be about.
const (
	// ModAuthorSurfaceMessage is text sent as a message: the composer, a thread
	// reply, an edit.
	ModAuthorSurfaceMessage = "message"
	// ModAuthorSurfaceSaved is text saved somewhere else: a channel name, a
	// to-do item, a poll, a widget.
	ModAuthorSurfaceSaved = "saved"
)

// modAuthorCopy holds {en-US, de-DE, ar}. "{words}" is where the quoted words go.
var modAuthorCopy = map[string][3]string{
	"modauthor_sent_one": {
		"This message was not sent: it contains a word this workspace does not allow: {words}.",
		"Diese Nachricht wurde nicht gesendet: Sie enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt: {words}.",
		"لم تُرسل هذه الرسالة: فهي تحتوي على كلمة لا تسمح بها مساحة العمل هذه: {words}.",
	},
	"modauthor_sent_many": {
		"This message was not sent: it contains words this workspace does not allow: {words}.",
		"Diese Nachricht wurde nicht gesendet: Sie enthält Wörter, die dieser Arbeitsbereich nicht erlaubt: {words}.",
		"لم تُرسل هذه الرسالة: فهي تحتوي على كلمات لا تسمح بها مساحة العمل هذه: {words}.",
	},
	"modauthor_sent_none": {
		"This message was not sent: it contains a word this workspace does not allow.",
		"Diese Nachricht wurde nicht gesendet: Sie enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt.",
		"لم تُرسل هذه الرسالة: فهي تحتوي على كلمة لا تسمح بها مساحة العمل هذه.",
	},
	"modauthor_saved_one": {
		"This was not saved: it contains a word this workspace does not allow: {words}.",
		"Das wurde nicht gespeichert: Es enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt: {words}.",
		"لم يُحفظ هذا: فهو يحتوي على كلمة لا تسمح بها مساحة العمل هذه: {words}.",
	},
	"modauthor_saved_many": {
		"This was not saved: it contains words this workspace does not allow: {words}.",
		"Das wurde nicht gespeichert: Es enthält Wörter, die dieser Arbeitsbereich nicht erlaubt: {words}.",
		"لم يُحفظ هذا: فهو يحتوي على كلمات لا تسمح بها مساحة العمل هذه: {words}.",
	},
	"modauthor_saved_none": {
		"This was not saved: it contains a word this workspace does not allow.",
		"Das wurde nicht gespeichert: Es enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt.",
		"لم يُحفظ هذا: فهو يحتوي على كلمة لا تسمح بها مساحة العمل هذه.",
	},
	"modauthor_masked": {
		"Readers see this message with a word hidden.",
		"Leser sehen diese Nachricht mit einem verborgenen Wort.",
		"يرى القراء هذه الرسالة مع إخفاء كلمة.",
	},
}

// modAuthorQuotes is the opening and closing quote and the list separator each
// language uses around a named word.
func modAuthorQuotes(locale string) (open, closing, separator string) {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "„", "“", ", "
	case strings.HasPrefix(locale, "ar"):
		return "\"", "\"", "، "
	}
	return "\"", "\"", ", "
}

// modAuthorQuoteWords is the named words as one list: "damn", "idiot". In
// Arabic each quoted word sits in its own directional isolate, so a Latin word
// inside an Arabic sentence is not reordered by its neighbours.
func modAuthorQuoteWords(locale string, words []string) string {
	open, closing, separator := modAuthorQuotes(locale)
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		item := open + word + closing
		if strings.HasPrefix(locale, "ar") {
			item = "⁨" + item + "⁩"
		}
		quoted = append(quoted, item)
	}
	return strings.Join(quoted, separator)
}

func modAuthorTable(key, locale string) string {
	values, ok := modAuthorCopy[key]
	if !ok {
		return ""
	}
	switch {
	case strings.HasPrefix(locale, "de"):
		return chatbug039Text(key, values[1], values[0])
	case strings.HasPrefix(locale, "ar"):
		return chatbug039Text(key, values[2], values[0])
	}
	return chatbug039Text(key, values[0], "")
}

// modAuthorText resolves the author table through the shared copy guard.
func modAuthorText(m Model, key string) string {
	return chatbug039Text(key, modAuthorTable(key, m.Locale), modAuthorTable(key, "en-US"))
}

// ModAuthorSentence is the one line an author reads when a filter refused their
// text. words are the offending words when they could be named, else nil.
func ModAuthorSentence(locale, surface string, words []string) string {
	return modAuthorSentence(Model{Locale: locale}, surface, words)
}

func modAuthorSentence(m Model, surface string, words []string) string {
	prefix := "modauthor_sent_"
	if surface == ModAuthorSurfaceSaved {
		prefix = "modauthor_saved_"
	}
	var clean []string
	for _, word := range words {
		if strings.TrimSpace(word) != "" {
			clean = append(clean, word)
		}
	}
	switch len(clean) {
	case 0:
		return modAuthorText(m, prefix+"none")
	case 1:
		return strings.ReplaceAll(modAuthorText(m, prefix+"one"), "{words}", modAuthorQuoteWords(m.Locale, clean))
	}
	return strings.ReplaceAll(modAuthorText(m, prefix+"many"), "{words}", modAuthorQuoteWords(m.Locale, clean))
}

// ModAuthorMaskedNote is the line under an author's own message that readers
// see with a word hidden.
func ModAuthorMaskedNote(locale string) string {
	return modAuthorText(Model{Locale: locale}, "modauthor_masked")
}
