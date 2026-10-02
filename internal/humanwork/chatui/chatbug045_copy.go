package chatui

// CHATBUG-045: the words of the Emoji skin tone row of Chat preferences. Quiet
// hours, Reading languages and the tone names already have their own en-US,
// de-DE and ar tables (the quiet hours copy, the rendering copy and the emoji
// copy); this table holds only the row's own title. The page's translator is not
// asked: the real product catalog answers a key it does not hold with a
// bracketed key, and a row must never print one.
const (
	keyChatbug045EmojiTone = "chat.prefs.emoji_tone"
	keyChatbug045More      = "chat.prefs.more_options"
	keyChatbug045Reading   = "chat.prefs.reading_language"
)

var chatbug045Copy = map[string]map[string]string{
	"en-US": {keyChatbug045EmojiTone: "Emoji skin tone", keyChatbug045More: "More options", keyChatbug045Reading: "Reading language"},
	"de-DE": {keyChatbug045EmojiTone: "Hautfarbe der Emoji", keyChatbug045More: "Weitere Optionen", keyChatbug045Reading: "Lesesprache"},
	"ar":    {keyChatbug045EmojiTone: "لون بشرة الرموز التعبيرية", keyChatbug045More: "خيارات إضافية", keyChatbug045Reading: "لغة القراءة"},
}

func chatbug045Text(m Model, key string) string {
	return chatbug039Text(key, chatbug045Copy[chatEmojiLocale(m.Locale)][key], chatbug045Copy["en-US"][key])
}
