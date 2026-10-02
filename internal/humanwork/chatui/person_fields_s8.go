package chatui

import (
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// personFieldRows keeps the rows a person actually has. A row the directory
// could not fill is nil and is dropped, so the panel never prints a label
// with nothing behind it.
func personFieldRows(rows ...ui.Node) []ui.Node {
	kept := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			kept = append(kept, row)
		}
	}
	return kept
}

// personPhotoURL is the photo to draw for a coworker shown inside Person
// details: the one the details carry, else the one every other avatar of that
// person in Chat already uses (the message list reads m.PhotoURLs).
func personPhotoURL(m Model, id, carried string) string {
	if url := strings.TrimSpace(carried); url != "" {
		return url
	}
	return strings.TrimSpace(m.PhotoURLs[id])
}

// personDepartmentName is the department as a person reads it. The directory
// supplies the unit's name; if a bare identifier ("project-management") is all
// that arrived it is turned into words rather than printed as the identifier.
// A value with a space or an uppercase letter is already a name and is kept.
func personDepartmentName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsUpper(r) }) {
		return value
	}
	if !strings.ContainsAny(value, "-_") {
		return value
	}
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(value))
	for i, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
