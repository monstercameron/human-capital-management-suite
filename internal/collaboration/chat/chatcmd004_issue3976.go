package chat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// chatcmd004SplitCommas cuts one typed line into tasks at its commas, except
// where a comma belongs to the task: inside quotation marks ("Review wage,
// tax and benefit changes") or inside a number ($5,000). Each task that was
// wrapped whole in quotation marks loses them.
func chatcmd004SplitCommas(text string) []string {
	var parts []string
	last, inQuote := 0, false
	for i, r := range text {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == '“':
			inQuote = true
		case r == '”':
			inQuote = false
		case r == ',' && !inQuote && !chatcmd004ThousandsComma(text, i):
			if part := chatcmd004Unquote(strings.TrimSpace(text[last:i])); part != "" {
				parts = append(parts, part)
			}
			last = i + 1
		}
	}
	if part := chatcmd004Unquote(strings.TrimSpace(text[last:])); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func chatcmd004ASCIIDigit(text string, i int) bool {
	return i >= 0 && i < len(text) && text[i] >= '0' && text[i] <= '9'
}

// chatcmd004ThousandsComma reports whether the comma at text[i] sits between
// a digit and exactly three more digits (5,000 and 1,250,000, not 5, 000).
func chatcmd004ThousandsComma(text string, i int) bool {
	return chatcmd004ASCIIDigit(text, i-1) && chatcmd004ASCIIDigit(text, i+1) && chatcmd004ASCIIDigit(text, i+2) && chatcmd004ASCIIDigit(text, i+3) && !chatcmd004ASCIIDigit(text, i+4)
}

// chatcmd004Unquote takes one pair of quotation marks off a task that they
// wrap whole.
func chatcmd004Unquote(s string) string {
	r := []rune(s)
	if len(r) >= 2 && ((r[0] == '"' && r[len(r)-1] == '"') || (r[0] == '“' && r[len(r)-1] == '”')) {
		inner := string(r[1 : len(r)-1])
		if !strings.ContainsAny(inner, "\"“”") {
			return strings.TrimSpace(inner)
		}
	}
	return s
}

var (
	chatcmd003OptionBreakAt = regexp.MustCompile(`^(?i:\s*[,;،]\s*|\s+(?:or|oder|أو)\s+)`)
	chatcmd003RegionTail    = regexp.MustCompile(`^,\s*[A-Z]{2}(?:[^A-Za-z]|$)`)
)

// chatcmd003SplitOptions cuts a run of loosely typed options at the breaks of
// chatcmd003OptionBreak, except where a break belongs to an option: inside
// quotation marks (each option wrapped whole in them loses them), a
// thousands comma, and the comma between a capitalised place and its
// two-letter region ("Boston, MA or Portland, ME").
func chatcmd003SplitOptions(text string) []string {
	var parts []string
	last, closing := 0, rune(0)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case closing != 0:
			if r == closing {
				closing = 0
			}
		case r == '"':
			closing = '"'
		case r == '“':
			closing = '”'
		default:
			if m := chatcmd003OptionBreakAt.FindString(text[i:]); m != "" {
				if c := strings.IndexByte(m, ','); c >= 0 && (chatcmd004ThousandsComma(text, i+c) ||
					(chatcmd003RegionTail.MatchString(text[i+c:]) && chatcmd003CapitalisedLastWord(text[last:i+c]))) {
					break
				}
				parts = append(parts, chatcmd004Unquote(strings.TrimSpace(text[last:i])))
				last, i = i+len(m), i+len(m)
				continue
			}
		}
		i += size
	}
	return append(parts, chatcmd004Unquote(strings.TrimSpace(text[last:])))
}

func chatcmd003CapitalisedLastWord(s string) bool {
	words := strings.Fields(s)
	if len(words) == 0 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(words[len(words)-1])
	return unicode.IsUpper(r)
}
