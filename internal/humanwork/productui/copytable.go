package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// parseCopyTable reads a copy table written as text: one entry per line,
// `key = text` for a plain message and `key#category = text` for each plural
// category of a plural message (`workflow_editor.problem_count#one = ...`).
//
// A value that starts with a double quote is a Go quoted string, which is how
// a text with a leading or trailing space is written; any other value is
// taken exactly as written after the first " = ". Blank lines are skipped.
// Keys never contain a space, so the first " = " always ends the key.
//
// Large copy tables are held in this form rather than as map literals: a
// map literal is compiled into one insert per entry, which in the browser
// bundle costs several times the text itself.
func parseCopyTable(data string) map[string]localize.Message {
	messages := make(map[string]localize.Message, strings.Count(data, "\n"))
	for line := range strings.Lines(data) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, `"`) {
			if unquoted, err := strconv.Unquote(value); err == nil {
				value = unquoted
			}
		}
		name, category, plural := strings.Cut(key, "#")
		if !plural {
			messages[key] = localize.Message{Text: value}
			continue
		}
		message := messages[name]
		if message.Plural == nil {
			message.Plural = make(map[string]string, 6)
		}
		message.Plural[category] = value
		messages[name] = message
	}
	return messages
}
