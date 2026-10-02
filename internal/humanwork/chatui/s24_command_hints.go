package chatui

// The "/" menu's argument hints for /poll, /todo and /ask come from the shared
// registry in English. The server reads the keywords of all three product
// languages in a poll's options and a task's due date (chatcmd003OptionBreak:
// or / oder / أو; chatcmd004DateLead: by / bis / بحلول, with the weekday in the
// same language), so each language's hint is written whole in its own words and
// every keyword it shows is one the server understands. The keys are what the
// registry gives for the command, as chatbug059CommandArgs is keyed.
func init() {
	for english, translated := range map[string][2]string{
		"question? option, option or option": {"Frage? Option, Option oder Option", "سؤال؟ خيار، خيار أو خيار"},
		"task, task @Name by Friday, task":   {"Aufgabe, Aufgabe @Name bis Freitag, Aufgabe", "مهمة, مهمة @الاسم بحلول الجمعة, مهمة"},
		"@Agent your question":               {"@Agent Ihre Frage", "@الوكيل سؤالك"},
	} {
		chatbug059CommandArgs[english] = [3]string{english, translated[0], translated[1]}
	}
	for english, translated := range map[string][2]string{
		`/poll "Where for the offsite?" 1="Lisbon" 2="Porto" 3="Remote"`:              {`/poll "Wohin soll der Teamausflug gehen?" 1="Lissabon" 2="Porto" 3="Remote"`, `/poll "أين نذهب في رحلة الفريق؟" 1="لشبونة" 2="بورتو" 3="عن بُعد"`},
		`/todo "Launch checklist" 1="Book the room" 2="Send invites @Dana by Friday"`: {`/todo "Checkliste zum Start" 1="Raum buchen" 2="Einladungen senden @Dana bis Freitag"`, `/todo "قائمة الإطلاق" 1="احجز القاعة" 2="أرسل الدعوات @Dana بحلول الجمعة"`},
		`/ask @Assistant How many vacation days carry over?`:                          {`/ask @Assistant Wie viele Urlaubstage werden übertragen?`, `/ask @Assistant كم يومًا من الإجازة يُرحَّل؟`},
	} {
		s24CommandExamples[english] = [3]string{english, translated[0], translated[1]}
	}
}

// s24CommandExamples are the sample lines of the same commands, English first,
// shown as the row's tooltip and the usage line's title.
var s24CommandExamples = map[string][3]string{}

// s24CommandExample is a command's sample line in the viewer's language, the
// registry's own when the table has no row for it.
func s24CommandExample(m Model, command composerCommand) string {
	if row, ok := s24CommandExamples[command.Example]; ok {
		return chatbug039Text("", row[chatbug039LocaleIndex(m.Locale)], command.Example)
	}
	return command.Example
}
