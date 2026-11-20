package productui

// #3975: the words the composer announces when the browser puts text back in it.
var issue3975Copy = map[string][3]string{
	"follow_up": {
		"Your earlier question is quoted in the box. Add your next question below it, then choose Ask.",
		"Ihre frühere Frage steht zitiert im Feld. Ergänzen Sie darunter Ihre nächste Frage und wählen Sie dann Fragen.",
		"سؤالك السابق مقتبس في المربع. أضف سؤالك التالي تحته ثم اختر اسأل.",
	},
	"draft_restored": {
		"Your unsent question is back in the box.",
		"Ihre noch nicht gesendete Frage steht wieder im Feld.",
		"عاد سؤالك الذي لم يُرسل إلى المربع.",
	},
}

func issue3975Text(locale LocaleContext, key string) string {
	return agentuxCopyText(issue3975Copy, locale, key)
}
