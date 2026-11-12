package chatui

import (
	"strings"
	"unicode/utf8"
)

// chat3715MaxPostRunes is the character ceiling the chat service refuses above
// (transport/chat MaxPostBodyRunes). The composer checks it before sending, so
// an over-long message is explained in the composer instead of being dropped.
const chat3715MaxPostRunes = 4000

// chat3715Copy is the refusal sentence in en-US, de-DE and ar, in that order.
var chat3715Copy = [3]string{
	"This message is too long. The limit is 4000 characters, so shorten it and send it again.",
	"Diese Nachricht ist zu lang. Das Limit liegt bei 4000 Zeichen. Kürzen Sie sie und senden Sie sie erneut.",
	"هذه الرسالة طويلة جدًا. الحد 4000 حرف، فاختصرها وأرسلها مرة أخرى.",
}

// chat3715LengthNotice returns the composer notice for a body over the
// character ceiling, in the reader's locale, or "" when the body may be sent.
func chat3715LengthNotice(locale, body string) string {
	if utf8.RuneCountInString(strings.TrimSpace(body)) <= chat3715MaxPostRunes {
		return ""
	}
	return chat3715Copy[chatbug039LocaleIndex(locale)]
}
