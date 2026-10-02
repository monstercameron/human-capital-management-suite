package chatui

// The composer's tool row and command menu own the strings below. Each key has
// an en-US, de-DE and ar entry; shared resolution uses these reviewed tables.
const (
	keyComposerAdd            = "chat.composer.add"
	keyComposerAddPoll        = "chat.composer.add_poll"
	keyComposerAddPollNote    = "chat.composer.add_poll_note"
	keyComposerAddTodo        = "chat.composer.add_todo"
	keyComposerAddTodoNote    = "chat.composer.add_todo_note"
	keyComposerAddPollHere    = "chat.composer.add_poll_here"
	keyComposerAddTodoHere    = "chat.composer.add_todo_here"
	keyComposerAddLocation    = "chat.composer.add_location"
	keyComposerAddLocNote     = "chat.composer.add_location_note"
	keyComposerAddVoice       = "chat.composer.add_voice"
	keyComposerAddVoiceNote   = "chat.composer.add_voice_note"
	keyComposerAddAttachNote  = "chat.composer.add_attach_note"
	keyComposerMention        = "chat.composer.mention"
	keyComposerFormat         = "chat.composer.format"
	keyComposerFormatTip      = "chat.composer.format_tip"
	keyComposerCommands       = "chat.composer.commands"
	keyComposerCommandGiphy   = "chat.composer.command_giphy"
	keyComposerCommandPlace   = "chat.composer.command_location"
	keyComposerCommandUnknown = "chat.composer.command_unknown"
)

var composerToolsCopy = map[string]map[string]string{
	"en-US": {
		keyComposerAdd:            "Add to your message",
		keyComposerAddPoll:        "Poll",
		keyComposerAddPollNote:    "Ask the channel a question with answer options",
		keyComposerAddTodo:        "To-do list",
		keyComposerAddTodoNote:    "Keep track of this channel's tasks",
		keyComposerAddPollHere:    "Ask this conversation a question with answer options",
		keyComposerAddTodoHere:    "Post a list of tasks people can tick off",
		keyComposerAddLocation:    "Location",
		keyComposerAddLocNote:     "Share where you are or a place",
		keyComposerAddVoice:       "Voice message",
		keyComposerAddVoiceNote:   "Record a short audio message",
		keyComposerAddAttachNote:  "Upload images, PDFs or documents",
		keyComposerMention:        "Mention someone",
		keyComposerFormat:         "Formatting",
		keyComposerFormatTip:      "Show or hide formatting",
		keyComposerCommands:       "Commands",
		keyComposerCommandGiphy:   "Search for a GIF and post it",
		keyComposerCommandPlace:   "Share your location",
		keyComposerCommandUnknown: "/{name} is not a command here. To send it as text, start the line with //.",
	},
	"de-DE": {
		keyComposerAdd:            "Zur Nachricht hinzufügen",
		keyComposerAddPoll:        "Umfrage",
		keyComposerAddPollNote:    "Dem Kanal eine Frage mit Antwortoptionen stellen",
		keyComposerAddTodo:        "Aufgabenliste",
		keyComposerAddTodoNote:    "Die Aufgaben dieses Kanals im Blick behalten",
		keyComposerAddPollHere:    "Dieser Unterhaltung eine Frage mit Antwortoptionen stellen",
		keyComposerAddTodoHere:    "Eine Aufgabenliste posten, die andere abhaken können",
		keyComposerAddLocation:    "Standort",
		keyComposerAddLocNote:     "Den eigenen Standort oder einen Ort teilen",
		keyComposerAddVoice:       "Sprachnachricht",
		keyComposerAddVoiceNote:   "Eine kurze Audionachricht aufnehmen",
		keyComposerAddAttachNote:  "Bilder, PDFs oder Dokumente hochladen",
		keyComposerMention:        "Jemanden erwähnen",
		keyComposerFormat:         "Formatierung",
		keyComposerFormatTip:      "Formatierung ein- oder ausblenden",
		keyComposerCommands:       "Befehle",
		keyComposerCommandGiphy:   "Ein GIF suchen und senden",
		keyComposerCommandPlace:   "Den eigenen Standort teilen",
		keyComposerCommandUnknown: "/{name} ist hier kein Befehl. Um es als Text zu senden, beginne die Zeile mit //.",
	},
	"ar": {
		keyComposerAdd:            "إضافة إلى الرسالة",
		keyComposerAddPoll:        "استطلاع",
		keyComposerAddPollNote:    "اطرح على القناة سؤالًا مع خيارات للإجابة",
		keyComposerAddTodo:        "قائمة المهام",
		keyComposerAddTodoNote:    "تابع مهام هذه القناة",
		keyComposerAddPollHere:    "اطرح على هذه المحادثة سؤالًا مع خيارات للإجابة",
		keyComposerAddTodoHere:    "انشر قائمة مهام يمكن للآخرين وضع علامة عليها",
		keyComposerAddLocation:    "الموقع",
		keyComposerAddLocNote:     "شارك مكانك أو موقعًا",
		keyComposerAddVoice:       "رسالة صوتية",
		keyComposerAddVoiceNote:   "سجّل رسالة صوتية قصيرة",
		keyComposerAddAttachNote:  "ارفع صورًا أو ملفات PDF أو مستندات",
		keyComposerMention:        "الإشارة إلى شخص",
		keyComposerFormat:         "التنسيق",
		keyComposerFormatTip:      "إظهار التنسيق أو إخفاؤه",
		keyComposerCommands:       "الأوامر",
		keyComposerCommandGiphy:   "ابحث عن صورة GIF وانشرها",
		keyComposerCommandPlace:   "شارك موقعك",
		keyComposerCommandUnknown: "/{name} ليس أمرًا هنا. لإرساله كنص، ابدأ السطر بـ //.",
	},
}

// composerText resolves tool copy through the shared feature-table fallback.
func composerText(m Model, key string) string {
	return chatbug039Text(key, composerToolsCopy[chatEmojiLocale(m.Locale)][key], composerToolsCopy["en-US"][key])
}
