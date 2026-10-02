// Package chatlang is the pure core of Chat translation: the engine port, the
// fixed prompt, the placeholders that carry names, links, code, numbers and
// glossary terms through an engine untouched, the checks run after it, and the
// administrator's policy. It holds no database, clock or network.
package chatlang

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	// ErrPlaceholder means an engine lost, repeated, invented or damaged a
	// placeholder. The rendering is discarded; the original is shown.
	ErrPlaceholder = errors.New("chatlang: placeholders did not survive translation")
	// ErrNothingToTranslate means the message is only protected items.
	ErrNothingToTranslate = errors.New("chatlang: nothing to translate")
	// ErrBadOutput means the engine's output failed a sanity check.
	ErrBadOutput = errors.New("chatlang: engine output failed a check")
	// ErrUnavailable means no engine can serve the request.
	ErrUnavailable = errors.New("chatlang: no engine is available")
	// ErrBudget means the workspace's monthly translation limit is reached.
	ErrBudget = errors.New("chatlang: monthly translation limit reached")
	// ErrBarred means the channel or workspace forbids the engine asked for.
	ErrBarred = errors.New("chatlang: this engine is not allowed here")
	// ErrInvalid is a malformed request or setting.
	ErrInvalid = errors.New("chatlang: invalid request")
)

// Term is one glossary entry. A "keep" term (Language empty) is never
// translated; any other term has a required translation into Language.
type Term struct {
	Source   string `json:"source"`
	Language string `json:"language,omitempty"`
	Target   string `json:"target,omitempty"`
}

// Glossary is a workspace's small list of terms with a version that is stored
// on every rendering it shaped.
type Glossary struct {
	Version int64  `json:"version"`
	Terms   []Term `json:"terms"`
}

// structural is everything an engine must not touch. Longer, earlier
// alternatives win. Quotation marks are deliberately not protected: quoted
// words are part of the sentence and are translated.
var structural = regexp.MustCompile(strings.Join([]string{
	"```[\\s\\S]*?```",
	"~~~[\\s\\S]*?~~~",
	"``[^`]+``",
	"`[^`\\n]+`",
	"\\]\\([^ )\\n]+\\)",
	"(?:https?://|mailto:|www\\.)[^\\s<>]+",
	"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}",
	"<@[^>\\n]+>",
	"<#[^>\\n]+>",
	"@[\\p{L}\\p{N}_][\\p{L}\\p{N}_.-]*",
	"#[\\p{L}\\p{N}_][\\p{L}\\p{N}_-]*",
	":[a-z0-9_+-]{2,32}:",
	"[\\x{1F000}-\\x{1FAFF}\\x{2600}-\\x{27BF}\\x{FE0F}\\x{200D}]+",
	"[\\w.-]+\\.(?:pdf|docx?|xlsx?|pptx?|csv|txt|zip|png|jpe?g|gif|md|json|go)\\b",
	"[$€£]?[-+]?\\p{N}+(?:[-/:.,]\\p{N}+)*(?:%|[ \\t]?(?i:kg|mg|g|km|cm|mm|m|kb|mb|gb|tb|ms|s|h|min|usd|eur|gbp|am|pm)\\b)?",
	"[⟦⟧]",
}, "|"))

// Protected is a text with its protected items replaced by tokens, and what is
// needed to put them back and to check an engine's answer.
type Protected struct {
	text           string
	tokens, values []string
	prefix         string
}

// Text is what an engine receives.
func (p Protected) Text() string { return p.text }

// Count is the number of protected items.
func (p Protected) Count() int { return len(p.tokens) }

func tokenPrefix(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "⟦HCM:" + hex.EncodeToString(sum[:4]) + ":"
}

// Protect replaces everything structural, and every glossary term for the
// target language, with numbered tokens. Structural spans win over glossary
// spans that overlap them. A literal ⟦ or ⟧ in the message is itself protected,
// so an author can never forge a token.
func Protect(text string, glossary Glossary, target string) Protected {
	type span struct {
		start, end int
		value      string
	}
	var spans []span
	for _, at := range structural.FindAllStringIndex(text, -1) {
		spans = append(spans, span{at[0], at[1], text[at[0]:at[1]]})
	}
	overlaps := func(a, b int) bool {
		for _, s := range spans {
			if a < s.end && b > s.start {
				return true
			}
		}
		return false
	}
	terms := append([]Term(nil), glossary.Terms...)
	sort.SliceStable(terms, func(i, j int) bool { return len(terms[i].Source) > len(terms[j].Source) })
	for _, term := range terms {
		if term.Source == "" || (term.Language != "" && term.Language != target) {
			continue
		}
		matcher, err := regexp.Compile("(?i)" + regexp.QuoteMeta(term.Source))
		if err != nil {
			continue
		}
		for _, at := range matcher.FindAllStringIndex(text, -1) {
			if !wordEdge(text, at[0], at[1]) || overlaps(at[0], at[1]) {
				continue
			}
			value := text[at[0]:at[1]]
			if term.Language != "" {
				value = term.Target
			}
			spans = append(spans, span{at[0], at[1], value})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	p := Protected{prefix: tokenPrefix(text)}
	var out strings.Builder
	cursor := 0
	for _, s := range spans {
		out.WriteString(text[cursor:s.start])
		token := p.prefix + strconv.Itoa(len(p.tokens)) + "⟧"
		p.tokens = append(p.tokens, token)
		p.values = append(p.values, s.value)
		out.WriteString(token)
		cursor = s.end
	}
	out.WriteString(text[cursor:])
	p.text = out.String()
	return p
}

// wordEdge reports whether text[start:end] is a whole word or phrase.
func wordEdge(text string, start, end int) bool {
	before, after := true, true
	if start > 0 {
		r := []rune(text[:start])
		before = !isWordRune(r[len(r)-1])
	}
	if end < len(text) {
		for _, r := range text[end:] {
			after = !isWordRune(r)
			break
		}
	}
	return before && after
}
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// HasLetters reports whether the protected text still holds words an engine
// must translate. A message that is only a name, a number or a link does not.
func (p Protected) HasLetters() bool { return p.plainLetters() >= 2 }

// plainLetters counts the letters outside every token.
func (p Protected) plainLetters() int {
	rest := p.text
	for _, token := range p.tokens {
		rest = strings.ReplaceAll(rest, token, " ")
	}
	return letterCount(rest)
}

// Restore puts the originals back. Every token must appear exactly once and
// nothing that looks like a token may be left over; otherwise it fails with
// ErrPlaceholder and the failures name which items were lost.
func (p Protected) Restore(output string) (string, []string, error) {
	seen := make([]int, len(p.tokens))
	var out strings.Builder
	var failures []string
	rest := output
	for {
		at := strings.Index(rest, "⟦")
		if at < 0 {
			break
		}
		out.WriteString(rest[:at])
		rest = rest[at:]
		if !strings.HasPrefix(rest, p.prefix) {
			failures = append(failures, "foreign marker")
			rest = rest[len("⟦"):]
			continue
		}
		end := strings.Index(rest, "⟧")
		if end < 0 {
			failures = append(failures, "unterminated marker")
			break
		}
		number := rest[len(p.prefix):end]
		index, err := strconv.Atoi(number)
		if err != nil || index < 0 || index >= len(p.tokens) {
			failures = append(failures, "unknown marker")
			rest = rest[end+len("⟧"):]
			continue
		}
		seen[index]++
		out.WriteString(p.values[index])
		rest = rest[end+len("⟧"):]
	}
	out.WriteString(rest)
	for i, n := range seen {
		switch {
		case n == 0:
			failures = append(failures, "lost item "+strconv.Itoa(i))
		case n > 1:
			failures = append(failures, "repeated item "+strconv.Itoa(i))
		}
	}
	if len(failures) > 0 {
		return "", failures, ErrPlaceholder
	}
	return out.String(), nil, nil
}

// Verify runs the checks that need no model: the answer is not empty, not
// absurdly long or short for the input, not the untouched input, and not an
// echo of the instruction. It returns the failures, empty when the answer
// passes. protectedOutput is the engine's text before restoring.
func (p Protected) Verify(protectedOutput string) []string {
	var failures []string
	trimmed := strings.TrimSpace(protectedOutput)
	in := strings.TrimSpace(p.text)
	switch {
	case trimmed == "":
		return []string{"empty output"}
	case len(trimmed) > 6*len(in)+200:
		failures = append(failures, "output much longer than the input")
	case len(in) > 40 && len(trimmed)*8 < len(in):
		failures = append(failures, "output much shorter than the input")
	}
	if trimmed == in && p.plainLetters() >= 12 {
		failures = append(failures, "output is the input unchanged")
	}
	if strings.Contains(trimmed, "untrusted_data") || strings.Contains(trimmed, "chat-translation/v") {
		failures = append(failures, "output repeats the instruction")
	}
	return failures
}

func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}
