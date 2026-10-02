package chatlang

import "strings"

// CHATLANG-005: what carries the reader's language beyond messages. These are
// the pieces that do not depend on who calls them; the agent run path, the voice
// pipeline and the search page ask for them.

var languageNames = map[string]string{
	"en": "English", "de": "German", "fr": "French", "es": "Spanish",
	"pt": "Portuguese", "ar": "Arabic", "ja": "Japanese", "hi": "Hindi",
}

// LanguageName is the English name of a language the product offers, or "" for
// any other tag (including "no language"). It is the only way a tag becomes
// words in an instruction, so nothing but a known name can reach a prompt.
func LanguageName(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if end := strings.IndexAny(tag, "-_"); end >= 0 {
		tag = tag[:end]
	}
	return languageNames[tag]
}

// ReaderLanguageInstruction is the sentence an agent is given about the person
// it answers: the language to answer in, with names, numbers, links and section
// anchors left exactly as they are. It is empty when the language is not one the
// product offers, and the agent answers as it did before.
func ReaderLanguageInstruction(tag string) string {
	name := LanguageName(tag)
	if name == "" {
		return ""
	}
	return "Answer in " + name + ", the language the person asking reads. Keep names, numbers, dates, links and section anchors exactly as they are, and cite documents as you would in any language."
}
