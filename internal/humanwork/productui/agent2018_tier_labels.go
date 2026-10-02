package productui

import "strings"

// agentTierLabels say, in the words a person uses, what a skill's side-effect
// tier lets an agent do. The raw tier codes stay in data attributes for
// scripts; the page never prints "T3_SUBMIT_GOVERNED" at a person.
var agentTierLabels = map[string][3]string{
	"T0": {"Read information", "Informationen lesen", "قراءة المعلومات"},
	"T1": {"Prepare a private draft", "Privaten Entwurf vorbereiten", "إعداد مسودة خاصة"},
	"T2": {"Send messages", "Nachrichten senden", "إرسال الرسائل"},
	"T3": {"Submit for approval", "Zur Genehmigung einreichen", "الإرسال للموافقة"},
	"T4": {"Change another system", "Anderes System ändern", "تغيير نظام آخر"},
}

// agentTierLabel returns the localized words for a tier code such as "T3" or
// "T3_SUBMIT_GOVERNED". A value it does not know is returned as given, so an
// unexpected tier is visible rather than blank.
func agentTierLabel(locale LocaleContext, tier string) string {
	code := strings.ToUpper(strings.TrimSpace(tier))
	if cut := strings.IndexByte(code, '_'); cut > 0 {
		code = code[:cut]
	}
	labels, ok := agentTierLabels[code]
	if !ok {
		return tier
	}
	language := strings.ToLower(locale.Resolved)
	switch {
	case strings.HasPrefix(language, "de"):
		return labels[1]
	case strings.HasPrefix(language, "ar"):
		return labels[2]
	}
	return labels[0]
}
