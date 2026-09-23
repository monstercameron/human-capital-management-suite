package productui

import "strings"

type peopleColumnDefinition struct {
	ID, LabelKey string
	Sort         PeopleSortField
}

// A single allowlist defines selectable, searchable, sortable directory facts.
// It intentionally excludes compensation, private identifiers and contact data.
func peopleColumnDefinitions() []peopleColumnDefinition {
	return []peopleColumnDefinition{
		{"name", "people.column.person", PeopleSortName},
		{"role", "people.column.role", PeopleSortRole},
		{"team", "people.column.team", PeopleSortTeam},
		{"manager", "people.column.manager", PeopleSortManager},
		{"location", "people.column.location", PeopleSortLocation},
		{"worker_number", "person.worker_number", PeopleSortWorkerNumber},
		{"job_code", "person.job_code", PeopleSortJobCode},
		{"grade", "person.job_level", PeopleSortGrade},
		{"company", "person.company", PeopleSortCompany},
		{"business_unit", "person.business_unit", PeopleSortBusinessUnit},
		{"cost_center", "person.cost_center", PeopleSortCostCenter},
	}
}

const defaultPeopleColumns = "name,role,team,manager,location"

func peopleColumnIDs() []string {
	ids := make([]string, 0, len(peopleColumnDefinitions()))
	for _, column := range peopleColumnDefinitions() {
		ids = append(ids, column.ID)
	}
	return ids
}

func peopleColumnChooserProps(view View) ColumnChooserProps {
	props := ColumnChooserProps{I18nProps: I18nProps{Locale: view.Locale}, ID: "people-columns", Selected: NormalizePeopleColumns(view.PeopleColumns), Draft: view.PeopleColumnDraft}
	for _, column := range peopleColumnDefinitions() {
		props.Options = append(props.Options, ColumnChoice{ID: column.ID, Label: view.Locale.Text(column.LabelKey), Required: column.ID == "name"})
	}
	if view.Navigate != nil {
		props.Apply = func(selected string) {
			next := view
			next.PeopleColumns = NormalizePeopleColumns(selected)
			if !strings.Contains(","+next.PeopleColumns+",", ","+normalizePeopleSort(next.PeopleSort)+",") {
				next.PeopleSort, next.PeopleDirection = "name", "asc"
			}
			href := peopleDirectoryHref(next, 1, next.Query, next.PeopleTeam, next.PeopleLocation, next.PeopleEligibleOnly, next.PeopleSort, next.PeopleDirection)
			href = withExplicitQueryValue(href, []string{"columns"}, next.PeopleColumns)
			href = withExplicitQueryValue(href, []string{"sort"}, normalizePeopleSort(next.PeopleSort))
			href = withExplicitQueryValue(href, []string{"dir"}, normalizePeopleDirection(next.PeopleDirection))
			view.Navigate(href)
		}
		props.Reset = func() { props.Apply(defaultPeopleColumns) }
	}
	return props
}

func peopleColumnsQueryValue(raw string) string {
	if raw == "" || NormalizePeopleColumns(raw) == defaultPeopleColumns {
		return ""
	}
	return NormalizePeopleColumns(raw)
}

// NormalizePeopleColumns bounds stored/URL configuration to public directory
// column IDs. Person stays visible; ordering is stable and duplicates disappear.
func NormalizePeopleColumns(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return defaultPeopleColumns
	}
	selected := map[string]bool{"name": true}
	for _, id := range strings.Split(raw, ",") {
		selected[strings.TrimSpace(id)] = true
	}
	ids := []string{}
	for _, column := range peopleColumnDefinitions() {
		if selected[column.ID] {
			ids = append(ids, column.ID)
		}
	}
	return strings.Join(ids, ",")
}

func extraPeopleValues(person Person) [6]string {
	return [6]string{person.WorkerNumber, person.JobCode, person.Grade, person.Company, person.BusinessUnit, person.CostCenter}
}
