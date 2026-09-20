package productui

import "strings"

// SearchPeopleDirectory searches one admitted population
// without enumerating it: blank queries match nothing, never
// the population. Non-blank queries substring-match the
// directory's own normalized index with team and location
// narrowing when set, in admission order. The query lifecycle
// filter applies first, so terminated workers stay out of
// search results until an explicit opt-in names them. Browsing the full
// population stays on the authorized directory page; the
// search path cannot be used to harvest it.
func SearchPeopleDirectory(population []Person, query PeopleQuery) []Person {
	hits := make([]Person, 0, len(population))
	text := strings.ToLower(strings.TrimSpace(query.Query))
	if text == "" {
		return hits
	}
	team := strings.ToLower(strings.TrimSpace(query.Team))
	location := strings.ToLower(strings.TrimSpace(query.Location))
	for _, person := range matchingPeople(population, query) {
		index := normalizedPerson(person)
		if !strings.Contains(index.search, text) {
			continue
		}
		if team != "" && index.team != team {
			continue
		}
		if location != "" && index.location != location {
			continue
		}
		hits = append(hits, person)
	}
	return hits
}
