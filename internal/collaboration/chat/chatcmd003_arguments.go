package chat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Chatcmd003Arguments is shared by the composer and server. Values retain
// their exact wording; doubled quotes represent one literal quote.
type Chatcmd003Arguments struct {
	Text     string
	Items    []string
	Named    map[string]string
	Explicit bool
}

func Chatcmd003ParseArguments(input string) (Chatcmd003Arguments, error) {
	a := Chatcmd003Arguments{Named: map[string]string{}}
	if len(input) > 12000 || !utf8.ValidString(input) {
		return a, ErrInvalidArgument
	}
	var words []string
	var numbered = map[int]string{}
	r := []rune(input)
	for i := 0; i < len(r); {
		for i < len(r) && unicode.IsSpace(r[i]) {
			i++
		}
		if i == len(r) {
			break
		}
		var token strings.Builder
		quoted := false
		assignment := -1
		for i < len(r) {
			c := r[i]
			if c == '"' {
				if quoted && i+1 < len(r) && r[i+1] == '"' {
					token.WriteRune('"')
					i += 2
					continue
				}
				quoted = !quoted
				i++
				continue
			}
			if !quoted && unicode.IsSpace(c) {
				break
			}
			if unicode.IsControl(c) && c != '\n' && c != '\t' {
				return a, ErrInvalidArgument
			}
			if !quoted && c == '=' && assignment < 0 {
				assignment = token.Len()
			}
			token.WriteRune(c)
			i++
		}
		if quoted {
			return a, fmt.Errorf("%w: quote", ErrInvalidArgument)
		}
		word := token.String()
		key, value, pair := "", "", assignment >= 0
		if pair {
			key, value = word[:assignment], word[assignment+1:]
		}
		if pair {
			if n, err := strconv.Atoi(key); err == nil {
				if n < 1 || n > 30 {
					return a, ErrInvalidArgument
				}
				if _, exists := numbered[n]; exists {
					return a, ErrInvalidArgument
				}
				numbered[n] = strings.TrimSpace(value)
				a.Explicit = true
				continue
			}
			key = strings.ToLower(key)
			switch key {
			case "multiple", "anonymous", "closes", "results", "tick", "add":
				if _, exists := a.Named[key]; exists {
					return a, ErrInvalidArgument
				}
				a.Named[key] = value
				continue
			}
			return a, fmt.Errorf("%w: setting", ErrInvalidArgument)
		}
		words = append(words, word)
	}
	keys := make([]int, 0, len(numbered))
	for n := range numbered {
		keys = append(keys, n)
	}
	sort.Ints(keys)
	for _, n := range keys {
		a.Items = append(a.Items, numbered[n])
	}
	a.Text = strings.TrimSpace(strings.Join(words, " "))
	return a, nil
}

func Chatcmd003RenderArguments(a Chatcmd003Arguments) string {
	quote := func(s string) string { return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\"" }
	var parts []string
	if a.Text != "" {
		parts = append(parts, quote(a.Text))
	}
	for i, text := range a.Items {
		parts = append(parts, strconv.Itoa(i+1)+"="+quote(text))
	}
	keys := make([]string, 0, len(a.Named))
	for key := range a.Named {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+quote(a.Named[key]))
	}
	return strings.Join(parts, " ")
}

func chatcmd003LooseItems(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	separator := ","
	if strings.Contains(text, "\n") {
		separator = "\n"
	}
	var items []string
	for _, line := range strings.Split(text, separator) {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}
