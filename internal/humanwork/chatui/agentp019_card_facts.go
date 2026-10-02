package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTP-019: an agent's card says the same things wherever it is opened from:
// the mention menu, a post, and the member list or the agent's name. The three
// facts that were missing from the last two, the version, the skills with their
// plain tier labels and "acts with your access", are drawn by these helpers so
// every opener shows one wording and one markup.

// agentp019Version is the "Version: N" line, or nil when the version is not
// known and the caller would rather say nothing than "Not provided".
func agentp019Version(locale string, persona ResolvedPersonaMention, always bool) ui.Node {
	if !always && strings.TrimSpace(persona.Version) == "" {
		return nil
	}
	return html.P(html.Props{Class: "mention-profile-version"}, html.Strong(html.Props{Text: personaMentionText(locale, "version") + ": "}), ui.Text(personaProfileFact(persona.Version, locale)))
}

// agentp019Access is the line that says the agent works with the asker's own
// access and no more.
func agentp019Access(locale string) ui.Node {
	return html.P(html.Props{Class: "mention-profile-access", Text: personaMentionText(locale, "acts_with_access")})
}

// agentp019Skills is the skills list, each with its tier in words.
func agentp019Skills(locale string, persona ResolvedPersonaMention) ui.Node {
	return personaProfileList(locale, "skills", personaSkillRows(locale, persona.Skills))
}
