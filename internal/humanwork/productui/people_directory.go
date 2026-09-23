package productui

import "strings"

// PeopleSortField is the typed People directory sort field.
// The zero value is name, matching the documented directory
// default: a sort must always produce an order, so unknown
// fields fall back rather than failing closed.
type PeopleSortField int

const (
	// PeopleSortName orders by person name.
	PeopleSortName PeopleSortField = iota
	// PeopleSortRole orders by role.
	PeopleSortRole
	// PeopleSortTeam orders by team.
	PeopleSortTeam
	// PeopleSortManager orders by manager.
	PeopleSortManager
	// PeopleSortLocation orders by location.
	PeopleSortLocation
	PeopleSortWorkerNumber
	PeopleSortJobCode
	PeopleSortGrade
	PeopleSortCompany
	PeopleSortBusinessUnit
	PeopleSortCostCenter
)

// PeopleSortDirection is the typed directory sort direction.
// The zero value is ascending, matching the documented
// directory default.
type PeopleSortDirection int

const (
	// PeopleSortAscending orders A to Z.
	PeopleSortAscending PeopleSortDirection = iota
	// PeopleSortDescending orders Z to A.
	PeopleSortDescending
)

// sortKey resolves a typed field to the directory's sort
// vocabulary.
func (field PeopleSortField) sortKey() string {
	for _, column := range peopleColumnDefinitions() {
		if column.Sort == field {
			return column.ID
		}
	}
	return peopleSortName
}

// ParsePeopleSort resolves request sort strings to the typed
// directory contract, preserving the documented name and
// ascending defaults for empty and unrecognized values.
func ParsePeopleSort(rawField, rawDirection string) (PeopleSortField, PeopleSortDirection) {
	var field PeopleSortField
	for _, column := range peopleColumnDefinitions() {
		if column.ID == strings.ToLower(strings.TrimSpace(rawField)) {
			field = column.Sort
			break
		}
	}
	direction := PeopleSortAscending
	if strings.EqualFold(strings.TrimSpace(rawDirection), peopleSortDescending) {
		direction = PeopleSortDescending
	}
	return field, direction
}

// SortPeopleDirectory orders one directory population by a
// typed field and direction with the directory's stable
// name/id tie-breaks. The population passes through
// untouched.
func SortPeopleDirectory(people []Person, field PeopleSortField, direction PeopleSortDirection) []Person {
	return sortPeopleValues(people, field.sortKey(), direction == PeopleSortDescending)
}
