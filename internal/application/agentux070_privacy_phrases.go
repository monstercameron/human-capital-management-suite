package application

import (
	"regexp"
	"strings"
	"unicode"
)

// An asker keeps an agent's answer to themselves by saying so in the question:
// "keep this private", "just for me" and their equivalents. The phrases are
// matched whole-word, in order, with any punctuation or spacing between the
// words, so "in private" matches "Tell me, in private: ..." and never
// "in privately" or "privateer". The phrase is a request about where the answer
// goes, not a question: it is removed before the text reaches the model.

// agentPrivacyPhrase is one phrase and the words that, when they come next,
// show the phrase was used in another sense ("in private equity").
type agentPrivacyPhrase struct {
	Words string
	// NotBefore lists words that cancel the match when they follow it.
	NotBefore []string
}

// agentPrivacyPhrases is the one table of phrases, by language. Arabic is
// written naturally; matching ignores diacritics, tatweel and the common
// letter variants, so "خاصة" and "خاصه" are the same word.
var agentPrivacyPhrases = map[string][]agentPrivacyPhrase{
	"en": {
		{Words: "keep this private"}, {Words: "keep it private"}, {Words: "keep that private"},
		{Words: "keep this answer private"}, {Words: "keep the answer private"}, {Words: "keep this between us"},
		{Words: "keep private"}, {Words: "make this private"}, {Words: "make it private"},
		{Words: "private answer"}, {Words: "answer privately"}, {Words: "reply privately"}, {Words: "respond privately"},
		{Words: "in private", NotBefore: []string{"equity", "sector", "practice", "company", "companies", "school", "schools", "label", "investment", "investments", "capital"}},
		{Words: "privately", NotBefore: []string{"held", "owned", "funded", "run"}},
		{Words: "just for me"}, {Words: "only for me"}, {Words: "only me"}, {Words: "for my eyes only"},
	},
	"de": {
		{Words: "privat halten"}, {Words: "halte das privat"}, {Words: "halte es privat"}, {Words: "halte diese antwort privat"},
		{Words: "bitte privat"}, {Words: "privat antworten"}, {Words: "antworte privat"}, {Words: "antworte mir privat"}, {Words: "privat beantworten"},
		{Words: "unter vier augen"}, {Words: "nur für mich"}, {Words: "nur fuer mich"}, {Words: "nur ich"}, {Words: "im privaten"},
	},
	"ar": {
		{Words: "اجعله خاصا"}, {Words: "اجعلها خاصة"}, {Words: "اجعل هذا خاصا"}, {Words: "اجعل الرد خاصا"}, {Words: "اجعل الإجابة خاصة"},
		{Words: "أبق هذا خاصا"}, {Words: "أبقه خاصا"}, {Words: "بشكل خاص"}, {Words: "بصورة خاصة"}, {Words: "في الخاص"}, {Words: "على الخاص"},
		{Words: "لي وحدي"}, {Words: "لي فقط"}, {Words: "فقط لي"}, {Words: "أنا فقط"},
		{Words: "رد خاص"}, {Words: "إجابة خاصة"}, {Words: "أجب بشكل خاص"}, {Words: "رد بشكل خاص"}, {Words: "بسرية"},
	},
}

type agentPrivacyToken struct {
	norm       string
	start, end int
}

// agentPrivacyCompiled is the table with each phrase split and folded once.
type agentPrivacyCompiled struct {
	words []string
	not   map[string]bool
}

var agentPrivacyIndex = compileAgentPrivacyPhrases()

func compileAgentPrivacyPhrases() []agentPrivacyCompiled {
	var out []agentPrivacyCompiled
	for _, phrases := range agentPrivacyPhrases {
		for _, phrase := range phrases {
			compiled := agentPrivacyCompiled{not: map[string]bool{}}
			for _, token := range agentPrivacyTokens(phrase.Words) {
				compiled.words = append(compiled.words, token.norm)
			}
			for _, word := range phrase.NotBefore {
				compiled.not[agentPrivacyFold(word)] = true
			}
			if len(compiled.words) > 0 {
				out = append(out, compiled)
			}
		}
	}
	return out
}

// agentPrivacyFold lowers a word and folds the Arabic variants that people
// type interchangeably, so one spelling of a phrase matches the others.
func agentPrivacyFold(word string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.Trim(word, "'’")) {
		switch {
		case r == 0x0640 || unicode.Is(unicode.Mn, r):
			continue
		case r == 'أ' || r == 'إ' || r == 'آ' || r == 'ٱ':
			r = 'ا'
		case r == 'ى':
			r = 'ي'
		case r == 'ة':
			r = 'ه'
		}
		out.WriteRune(r)
	}
	return out.String()
}

func agentPrivacyWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '\'' || r == '’' || r == 0x0640
}

// agentPrivacyTokens splits text into whole words with their byte spans.
func agentPrivacyTokens(text string) []agentPrivacyToken {
	var tokens []agentPrivacyToken
	start := -1
	for i, r := range text {
		if agentPrivacyWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			tokens = append(tokens, agentPrivacyToken{norm: agentPrivacyFold(text[start:i]), start: start, end: i})
			start = -1
		}
	}
	if start >= 0 {
		tokens = append(tokens, agentPrivacyToken{norm: agentPrivacyFold(text[start:]), start: start, end: len(text)})
	}
	return tokens
}

// agentPrivacySpans returns the byte spans of every privacy phrase in text.
func agentPrivacySpans(text string) [][2]int {
	tokens := agentPrivacyTokens(text)
	var spans [][2]int
	for i := range tokens {
		for _, phrase := range agentPrivacyIndex {
			n := len(phrase.words)
			if i+n > len(tokens) {
				continue
			}
			matched := true
			for k, word := range phrase.words {
				if tokens[i+k].norm != word {
					matched = false
					break
				}
			}
			if !matched || (i+n < len(tokens) && phrase.not[tokens[i+n].norm]) {
				continue
			}
			spans = append(spans, [2]int{tokens[i].start, tokens[i+n-1].end})
		}
	}
	return spans
}

// AskedForPrivacy reports whether the asker asked, in the question itself, for
// the answer to stay with them.
func AskedForPrivacy(question string) bool {
	return len(agentPrivacySpans(question)) > 0
}

var (
	agentPrivacySpaces  = regexp.MustCompile(`[ \t]{2,}`)
	agentPrivacyBeforeP = regexp.MustCompile(`\s+([,;:.!?\x{060C}\x{061B}\x{061F}])`)
	agentPrivacyStray   = regexp.MustCompile(`[,\x{060C}]\s*([:;.?!])|([?!])\.`)
)

// WithoutPrivacyRequest returns the question with every privacy phrase taken
// out, so the model is asked the question and not told where to send the answer.
// A question that is nothing but the phrase is returned unchanged: there is
// nothing left to ask, and the run should say so rather than send nothing.
func WithoutPrivacyRequest(question string) string {
	spans := agentPrivacySpans(question)
	if len(spans) == 0 {
		return question
	}
	var out strings.Builder
	last := 0
	for _, span := range spans {
		if span[0] < last {
			if span[1] > last {
				last = span[1]
			}
			continue
		}
		out.WriteString(question[last:span[0]])
		last = span[1]
	}
	out.WriteString(question[last:])
	cleaned := agentPrivacySpaces.ReplaceAllString(out.String(), " ")
	cleaned = agentPrivacyBeforeP.ReplaceAllString(cleaned, "$1")
	// What stood between the phrase and its neighbours is left behind: "Tell
	// me,: how", "carry over?.", ". How many".
	cleaned = agentPrivacyStray.ReplaceAllString(cleaned, "${1}${2}")
	cleaned = strings.TrimLeft(strings.TrimRight(cleaned, " \t\r\n,;:-–—،؛"), " \t\r\n.,;:-–—،؛")
	if cleaned == "" {
		return question
	}
	return cleaned
}
