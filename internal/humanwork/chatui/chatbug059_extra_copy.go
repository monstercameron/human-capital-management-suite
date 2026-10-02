package chatui

// CHATBUG-059. Words that reached a German or Arabic page in English because
// their lookup table had no row for the language: the approval card an agent
// draws for its invoker, and the placeholder the "/giphy" command shows for what
// it takes. Each row is English, German, Arabic.

// chatbug059ApprovalCopy keys the approval card's own lines.
var chatbug059ApprovalCopy = map[string][3]string{
	"chat.persona_approval.label":     {"Persona approval", "Genehmigung durch den Agenten", "موافقة الوكيل"},
	"chat.persona_approval.expired":   {"This approval is no longer available.", "Diese Genehmigung ist nicht mehr verfügbar.", "لم تعد هذه الموافقة متاحة."},
	"chat.persona_approval.open_task": {"Open in task view", "In der Aufgabenansicht öffnen", "فتح في عرض المهام"},
}

// chatbug059CommandArgs are the argument placeholders of a command that are
// plain words (not syntax the page parses), keyed by what the registry gives.
var chatbug059CommandArgs = map[string][3]string{
	"what to search for": {"what to search for", "wonach gesucht werden soll", "ما تريد البحث عنه"},
}

// chatbug059CommandArguments is composerCommandArguments in the viewer's
// language, for the placeholders the table above carries.
func chatbug059CommandArguments(m Model, command composerCommand) string {
	args := composerCommandArguments(command)
	if row, ok := chatbug059CommandArgs[args]; ok {
		return chatbug039Text("", row[chatbug039LocaleIndex(m.Locale)], args)
	}
	return args
}
