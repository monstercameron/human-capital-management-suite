package chatrender

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CHATLANG-002: in-process language detection. No call leaves the deployment,
// no model is downloaded, and the tables are the hand-written word and
// character lists in chatlang002_words.go. Text that is too short, too
// ambiguous, a name, a link, a mention or code is "und" (no language) and is
// never translated.

const (
	chatlangScriptNone = iota
	chatlangScriptLatin
	chatlangScriptArabic
	chatlangScriptCJK
	chatlangScriptDeva
	chatlangScriptOther
)

func chatlangScriptOf(r rune) int {
	switch {
	case !unicode.IsLetter(r):
		return chatlangScriptNone
	case unicode.In(r, unicode.Arabic):
		return chatlangScriptArabic
	case unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han), r == 'ー', r == 'ｰ':
		// The prolonged sound mark belongs to the kana around it.
		return chatlangScriptCJK
	case unicode.In(r, unicode.Devanagari):
		return chatlangScriptDeva
	case unicode.In(r, unicode.Latin):
		return chatlangScriptLatin
	}
	return chatlangScriptOther
}

func chatlangScriptTag(script int) string {
	switch script {
	case chatlangScriptArabic:
		return "ar"
	case chatlangScriptCJK:
		return "ja"
	case chatlangScriptDeva:
		return "hi"
	}
	return "latin"
}

// chatlangSentenceEnd reports whether the rune at text[i] ends a sentence. A
// full stop inside a token (3.5, example.com) does not.
func chatlangSentenceEnd(text string, i int, r rune) bool {
	switch r {
	case '\n', '!', '?', ';', '。', '؟', '।', '！', '？', '…':
		return true
	case '.':
		next := i + 1
		if next >= len(text) {
			return true
		}
		n, _ := utf8.DecodeRuneInString(text[next:])
		return unicode.IsSpace(n) || n == '"' || n == '\'' || n == ')'
	}
	return false
}

// chatlang002Detect is the whole-message detector behind Detect. Long messages
// use bounded beginning, middle and end samples. Offsets always refer to the
// original UTF-8 text, including when a sample starts mid-rune.
func chatlang002Detect(text string) Detection {
	windows := [][2]int{{0, len(text)}}
	if len(text) > 2048 {
		windows = [][2]int{{0, 384}, {len(text) / 2, len(text)/2 + 384}, {len(text) - 384, len(text)}}
	}
	var spans []Span
	// A sentence of Arabic-script text is judged with the rest of the message:
	// "من خوبم" alone cannot tell Arabic from Persian.
	persian := chatlangNotArabic(text)
	add := func(from, to int) {
		if from >= to {
			return
		}
		if d := chatlang002Segment(text[from:to]); d.Language != "und" && !(persian && d.Language == "ar") {
			spans = append(spans, Span{from, to, d.Language, d.Confidence})
		}
	}
	for _, window := range windows {
		start, end := window[0], window[1]
		for start < end && !utf8.RuneStart(text[start]) {
			start++
		}
		for end < len(text) && end > start && !utf8.RuneStart(text[end]) {
			end--
		}
		segmentStart, count, script := start, 0, chatlangScriptNone
		for at, r := range text[start:end] {
			position := start + at
			next := chatlangScriptOf(r)
			if script != chatlangScriptNone && next != chatlangScriptNone && script != next {
				add(segmentStart, position)
				segmentStart = position
				if count++; count == 4 {
					break
				}
			}
			if next != chatlangScriptNone {
				script = next
			}
			if chatlangSentenceEnd(text, position, r) {
				add(segmentStart, position)
				segmentStart = position + utf8.RuneLen(r)
				script = chatlangScriptNone
				if count++; count == 4 {
					break
				}
			}
		}
		if count < 4 && segmentStart < end {
			add(segmentStart, end)
		}
	}
	whole := Detection{Language: "und"}
	languages := map[string]bool{}
	for _, span := range spans {
		languages[span.Language] = true
		if span.Confidence > whole.Confidence {
			whole.Language = span.Language
			whole.Confidence = span.Confidence
		}
	}
	if len(languages) > 1 {
		whole.Language = "mul"
		whole.Spans = spans
	}
	return whole
}

// chatlangMask returns text with the parts that are not prose (links, mentions,
// channel names, addresses, paths, identifiers, inline code) replaced by spaces
// of the same byte length, and whether the whole text is code.
func chatlangMask(text string) (string, bool) {
	if strings.Contains(text, "```") || chatlangSQL(text) {
		return "", true
	}
	b := []byte(text)
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		end := strings.IndexByte(text[i+1:], '`')
		if end < 0 {
			break
		}
		for j := i; j <= i+1+end; j++ {
			b[j] = ' '
		}
		i += 1 + end
	}
	symbols, visible := 0, 0
	for i := 0; i < len(b); {
		for i < len(b) && unicode.IsSpace(rune(b[i])) {
			i++
		}
		start := i
		for i < len(b) && !unicode.IsSpace(rune(b[i])) {
			i++
		}
		if start == i {
			break
		}
		chunk := string(b[start:i])
		core := strings.Trim(chunk, `.,;:!?()[]{}"'«»“”‘’*~`)
		for _, r := range chunk {
			visible++
			if strings.ContainsRune("{}();=<>[]\\|&", r) {
				symbols++
			}
		}
		if chatlangNotProse(core) {
			for j := start; j < i; j++ {
				b[j] = ' '
			}
		}
	}
	if symbols >= 3 && symbols*5 >= visible {
		return "", true
	}
	return string(b), false
}

// chatlangSQL is true for a query written with upper-case keywords.
func chatlangSQL(text string) bool {
	return (strings.Contains(text, "SELECT ") && strings.Contains(text, " FROM ")) || strings.Contains(text, "INSERT INTO ") ||
		(strings.Contains(text, "UPDATE ") && strings.Contains(text, " SET ")) || strings.Contains(text, "DELETE FROM ") || strings.Contains(text, "CREATE TABLE ")
}

func chatlangNotProse(core string) bool {
	if core == "" {
		return false
	}
	switch core[0] {
	case '@', '#', '/', '\\':
		return true
	}
	lower := strings.ToLower(core)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "www.") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "doc:") || strings.HasPrefix(core, "[[") {
		return true
	}
	if strings.ContainsAny(core, "@_\\") || strings.Contains(core, "::") || strings.Contains(core, "()") {
		return true
	}
	if strings.Contains(core, "/") && (strings.Contains(core, ".") || strings.Count(core, "/") >= 2) {
		return true
	}
	runes := []rune(core)
	for i := 1; i+1 < len(runes); i++ {
		if runes[i] == '.' && (unicode.IsLetter(runes[i-1]) || unicode.IsDigit(runes[i-1])) && (unicode.IsLetter(runes[i+1]) || unicode.IsDigit(runes[i+1])) {
			return true
		}
	}
	return false
}

// chatlangWord is one run of letters (with inner apostrophes and hyphens).
type chatlangWord struct {
	text   string
	script int
	letter int
}

func chatlangWords(masked string) []chatlangWord {
	var words []chatlangWord
	runes := []rune(strings.ToLower(masked))
	for i := 0; i < len(runes); {
		if chatlangScriptOf(runes[i]) == chatlangScriptNone {
			i++
			continue
		}
		start := i
		script := chatlangScriptOf(runes[i])
		letters := 0
		for i < len(runes) {
			r := runes[i]
			s := chatlangScriptOf(r)
			switch {
			case s != chatlangScriptNone:
				if s != script && (script == chatlangScriptLatin || s == chatlangScriptLatin) {
					goto done
				}
				letters++
			case unicode.IsMark(r):
			case (r == '\'' || r == '’' || r == '-') && i+1 < len(runes) && chatlangScriptOf(runes[i+1]) != chatlangScriptNone && letters > 0:
			default:
				goto done
			}
			i++
		}
	done:
		words = append(words, chatlangWord{text: strings.ReplaceAll(string(runes[start:i]), "’", "'"), script: script, letter: letters})
	}
	return words
}

// chatlang002Segment classifies one sentence-sized piece of text.
func chatlang002Segment(text string) Detection {
	none := Detection{Language: "und"}
	if len(text) > 1024 {
		end := 1024
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end]
	}
	masked, code := chatlangMask(text)
	if code {
		return none
	}
	words := chatlangWords(masked)
	var letters [chatlangScriptOther + 1]int
	var wordCount [chatlangScriptOther + 1]int
	for _, w := range words {
		letters[w.script] += w.letter
		wordCount[w.script]++
	}
	total := 0
	for _, n := range letters {
		total += n
	}
	if total == 0 {
		return none
	}
	for _, script := range []int{chatlangScriptArabic, chatlangScriptCJK, chatlangScriptDeva} {
		if letters[script]*2 >= total {
			return chatlangNonLatin(script, masked, letters[script], wordCount[script], total)
		}
	}
	if letters[chatlangScriptLatin]*2 < total {
		return none
	}
	return chatlangLatinDetect(words, masked, letters[chatlangScriptLatin])
}

func chatlangNonLatin(script int, masked string, letters, words, total int) Detection {
	if letters < 6 {
		return Detection{Language: "und"}
	}
	switch script {
	case chatlangScriptArabic:
		if words < 2 || chatlangNotArabic(masked) {
			return Detection{Language: "und"}
		}
	case chatlangScriptDeva:
		if words < 2 {
			return Detection{Language: "und"}
		}
	case chatlangScriptCJK:
		// Han without kana is Chinese, which is not offered: no language.
		kana := 0
		for _, r := range masked {
			if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
				kana++
			}
		}
		if kana < 2 {
			return Detection{Language: "und"}
		}
	}
	return Detection{Language: chatlangScriptTag(script), Confidence: float64(letters) / float64(total)}
}

// chatlangNotArabic is true for Arabic-script text in Persian, Urdu or Pashto,
// which uses letters Arabic does not.
func chatlangNotArabic(masked string) bool {
	arabic, strong, weak := 0, 0, 0
	for _, r := range masked {
		if !unicode.In(r, unicode.Arabic) || !unicode.IsLetter(r) {
			continue
		}
		arabic++
		switch r {
		case 'پ', 'چ', 'ژ', 'گ', 'ٹ', 'ڈ', 'ڑ', 'ں', 'ے':
			strong++
		case 'ھ', 'ہ', 'ک', 'ی', 'ڤ':
			weak++
		}
	}
	if strong >= 1 || weak >= 2 {
		return true
	}
	// Persian function words that Arabic does not use.
	for _, w := range strings.Fields(masked) {
		switch strings.Trim(w, "،؟.!:؛") {
		case "است", "که", "را", "این", "برای", "نیست", "هستم", "نمی", "خیلی", "ممنون":
			return true
		}
	}
	return false
}

func chatlangLatinDetect(words []chatlangWord, masked string, letters int) Detection {
	none := Detection{Language: "und"}
	if letters < 6 {
		return none
	}
	var score, hits [chatlangLatinCount]int
	content := 0
	for _, w := range words {
		if w.script != chatlangScriptLatin {
			continue
		}
		if w.letter >= 2 {
			content++
		}
		for lang := chatlangLatinEN; lang < chatlangLatinCount; lang++ {
			weight := chatlangWordWeight(lang, w.text)
			if weight == 0 && strings.ContainsRune(w.text, '\'') {
				// l'équipe, j'ai, qu'il: the elided article is the evidence.
				if cut := strings.IndexByte(w.text, '\''); lang == chatlangLatinFR && chatlangFrenchElision(w.text[:cut+1]) {
					weight = 2
				}
			}
			if weight == 0 && strings.ContainsRune(w.text, '-') {
				// as-tu, peux-tu, dis-moi: each part is a word.
				for _, part := range strings.Split(w.text, "-") {
					weight = max(weight, chatlangWordWeight(lang, part))
				}
			}
			if weight > 0 {
				score[lang] += weight
				hits[lang]++
			}
		}
	}
	if content < 2 {
		return none
	}
	lower := strings.ToLower(masked)
	for lang := chatlangLatinEN; lang < chatlangLatinCount; lang++ {
		score[lang] += chatlangCharacterEvidence(lang, lower)
	}
	best := chatlangLatinEN
	for lang := chatlangLatinEN; lang < chatlangLatinCount; lang++ {
		if score[lang] > score[best] {
			best = lang
		}
	}
	top, runnerUp := score[best], 0
	for lang := chatlangLatinEN; lang < chatlangLatinCount; lang++ {
		if lang != best && score[lang] > runnerUp {
			runnerUp = score[lang]
		}
	}
	// A name or a lone word has no function word; two words need real evidence.
	if hits[best] < 1 || top < 4 || top-runnerUp < 3 || top*2 < runnerUp*3 {
		return none
	}
	confidence := float64(top-runnerUp) / float64(top+2)
	if confidence < 0.5 {
		return none
	}
	if confidence > 0.99 {
		confidence = 0.99
	}
	return Detection{Language: best.tag(), Confidence: confidence}
}
