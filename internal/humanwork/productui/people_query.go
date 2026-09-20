package productui

import "strings"

// PeopleQuery is the bounded People directory query behind
// the directory page: normalized strings, the typed sort
// contract, a floored page, and an allowlisted page size.
// One builder owns every bound so consumers cannot assemble
// an unbounded query by omission.
type PeopleQuery struct {
	Query     string
	Team      string
	Location  string
	Sort      PeopleSortField
	Direction PeopleSortDirection
	Page      int
	PageSize  int
	// Status is the lifecycle admission rule. The zero value is
	// the documented default: active-only, hiding terminated
	// and on-leave workers until an explicit opt-in names them.
	Status PeopleStatusFilter
}

// BuildPeopleQuery composes one bounded directory query from
// raw request parts. Strings arrive trimmed and lowered for
// case-insensitive matching, the sort resolves through the
// typed contract, pages floor at one, and sizes pass the
// directory allowlist.
func BuildPeopleQuery(rawQuery, rawTeam, rawLocation, rawSort, rawDirection string, rawPage, rawPageSize int) PeopleQuery {
	return BuildPeopleQueryWithStatus(rawQuery, rawTeam, rawLocation, rawSort, rawDirection, rawPage, rawPageSize, "")
}

// BuildPeopleQueryWithStatus composes one bounded directory query
// with an explicit lifecycle opt-in. Empty and unrecognized
// status parts resolve to the active-only default, fail-closed.
func BuildPeopleQueryWithStatus(rawQuery, rawTeam, rawLocation, rawSort, rawDirection string, rawPage, rawPageSize int, rawStatus string) PeopleQuery {
	sort, direction := ParsePeopleSort(rawSort, rawDirection)
	page := rawPage
	if page < 1 {
		page = 1
	}
	return PeopleQuery{
		Query:     strings.ToLower(strings.TrimSpace(rawQuery)),
		Team:      strings.ToLower(strings.TrimSpace(rawTeam)),
		Location:  strings.ToLower(strings.TrimSpace(rawLocation)),
		Sort:      sort,
		Direction: direction,
		Page:      page,
		PageSize:  normalizePageSize(rawPageSize),
		Status:    ParsePeopleStatusFilter(rawStatus),
	}
}

// matchingPeople admits one population through a query lifecycle
// filter, in admission order. Text, team and location narrowing
// stay with the search and directory selectors; this helper owns
// only the lifecycle admission both paths share.
func matchingPeople(population []Person, query PeopleQuery) []Person {
	kept := make([]Person, 0, len(population))
	for _, person := range population {
		if !query.Status.Admits(person.LifecycleStatus) {
			continue
		}
		kept = append(kept, person)
	}
	return kept
}
