package chatui

// CHATBUG-048 owns the group captions of Manage channel, in en-US, de-DE and ar.
const (
	keyChatbug048Channel = "chat.bug048.group_channel"
	keyChatbug048Rules   = "chat.bug048.group_rules"
	keyChatbug048Apps    = "chat.bug048.group_apps"
)

var chatbug048Copy = map[string]map[string]string{
	"en-US": {keyChatbug048Channel: "Channel", keyChatbug048Rules: "Rules and language", keyChatbug048Apps: "Apps"},
	"de-DE": {keyChatbug048Channel: "Kanal", keyChatbug048Rules: "Regeln und Sprache", keyChatbug048Apps: "Apps"},
	"ar":    {keyChatbug048Channel: "القناة", keyChatbug048Rules: "القواعد واللغة", keyChatbug048Apps: "التطبيقات"},
}
