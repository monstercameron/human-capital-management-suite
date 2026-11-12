package chatui

import "strings"

// chatissue3711Already reports whether the typed query names someone who is
// already on the list the picker builds: a chip already picked, or, when the
// details panel adds people, a member of the conversation. pickCandidates
// leaves those people out of the options, so an empty list with such a match
// means "already added", not "no one matches".
func chatissue3711Already(m Model, query string, picked []mentionCandidate) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	for _, p := range picked {
		if strings.Contains(strings.ToLower(p.Name), q) {
			return true
		}
	}
	if m.ShowAddMembers {
		for _, p := range m.Members {
			if strings.Contains(strings.ToLower(p.Name), q) {
				return true
			}
		}
	}
	return false
}

// chatissue3711AlreadyText says that the typed name is already added or is a
// member, in the reader's locale, with the typed query kept verbatim.
func chatissue3711AlreadyText(locale, query string) string {
	switch dateLocale(locale) {
	case "de":
		return "„" + query + "“ ist bereits hinzugefügt oder Mitglied dieser Unterhaltung."
	case "ar":
		return "«" + query + "» مضاف بالفعل أو عضو في هذه المحادثة."
	}
	return "“" + query + "” is already added or is a member of this conversation."
}
