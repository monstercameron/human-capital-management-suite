package productui

// workerEmploymentFacts is the employment fact set, matching
// the page adapter's organization list field for field.
var workerEmploymentFacts = []sectionFact{
	{"person.organization_unit", "organization_unit", func(_ LocaleContext, person Person) string { return person.Team }},
	{"person.manager", "manager", func(_ LocaleContext, person Person) string { return person.Manager }},
	{"person.position_id", "position_id", func(_ LocaleContext, person Person) string { return person.PositionID }},
	{"person.work_location", "work_location", func(_ LocaleContext, person Person) string { return person.Location }},
	// Company, business unit and cost center are recorded names and codes, so
	// they pass through; only the work arrangement is a token needing a word.
	{"person.company", "company", func(_ LocaleContext, person Person) string { return person.Company }},
	{"person.business_unit", "business_unit", func(_ LocaleContext, person Person) string { return person.BusinessUnit }},
	{"person.cost_center", "cost_center", func(_ LocaleContext, person Person) string { return person.CostCenter }},
	{"person.work_arrangement", "work_arrangement", func(locale LocaleContext, person Person) string {
		return employmentTerm(locale, person.WorkArrangement)
	}},
}

// ResolveWorkerEmployment resolves the worker employment
// section for one person record over the shared section
// engine. The record is never mutated.
func ResolveWorkerEmployment(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.organization", "person.organization_detail", workerEmploymentFacts)
}
