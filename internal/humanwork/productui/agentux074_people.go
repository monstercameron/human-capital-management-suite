package productui

import "strings"

// agentUX074WithPeoplePhotos gives each agent's business owner, technical
// contact and reviewer the photograph the people directory holds for them.
// The catalog names these people by subject and carries no picture, so Agent
// setup drew initials for people Chat shows by photograph. A person the
// directory does not list, or lists without a photograph, keeps the initials.
func agentUX074WithPeoplePhotos(view View, snapshot PersonaAdminSnapshot) PersonaAdminSnapshot {
	if len(view.People) == 0 || len(snapshot.Personas) == 0 {
		return snapshot
	}
	photo := func(subject, current string) string {
		if strings.TrimSpace(current) != "" {
			return current
		}
		return docsOwnerPhoto(view, strings.TrimSpace(subject))
	}
	personas := append([]PersonaAdminPersona(nil), snapshot.Personas...)
	for index := range personas {
		persona := &personas[index]
		persona.OwnerAvatarURL = photo(persona.Owner, persona.OwnerAvatarURL)
		persona.StewardAvatarURL = photo(persona.Steward, persona.StewardAvatarURL)
		persona.ReviewerAvatarURL = photo(persona.Reviewer, persona.ReviewerAvatarURL)
	}
	snapshot.Personas = personas
	return snapshot
}

// agentUX074ViewerIs reports whether the named person is the signed-in viewer,
// so a page never tells its reader to go and ask themselves.
func agentUX074ViewerIs(viewer string, person PersonaAdminTarget) bool {
	viewer = strings.TrimSpace(viewer)
	return viewer != "" && strings.TrimSpace(person.ID) == viewer
}
