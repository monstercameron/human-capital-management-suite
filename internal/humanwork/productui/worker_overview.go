package productui

// WorkerFact is one resolved section fact: the field key,
// its localized label, and its verdict-projected value.
type WorkerFact struct {
	Name  string
	Label string
	Value string
	// Status is the authoritative presentation state for the fact. It keeps
	// an empty value (MISSING), an absent field verdict (UNKNOWN), and a
	// policy-withheld value (WITHHELD) distinct from a real value (PRESENT).
	Status WorkerFactStatus
}

// WorkerFactStatus is the deliberately small vocabulary rendered by worker
// object sections. These are presentation states, not business lifecycle
// states and must not be inferred by callers from the display text.
type WorkerFactStatus string

const (
	WorkerFactPresent  WorkerFactStatus = "PRESENT"
	WorkerFactMissing  WorkerFactStatus = "MISSING"
	WorkerFactUnknown  WorkerFactStatus = "UNKNOWN"
	WorkerFactWithheld WorkerFactStatus = "WITHHELD"
	// Verbose aliases make call sites read naturally while retaining the
	// compact names used by the worker section constructors.
	WorkerFactStatusPresent  = WorkerFactPresent
	WorkerFactStatusMissing  = WorkerFactMissing
	WorkerFactStatusUnknown  = WorkerFactUnknown
	WorkerFactStatusWithheld = WorkerFactWithheld
)

// WorkerSection is one independently resolved worker object
// page section: title, description, and facts.
type WorkerSection struct {
	Title       string
	Description string
	Facts       []WorkerFact
}

// workerOverviewFacts is the overview fact set, matching the
// page adapter's details list field for field.
var workerOverviewFacts = []sectionFact{
	{"person.worker_number", "worker_number", func(_ LocaleContext, person Person) string { return person.WorkerNumber }},
	{"person.job_code", "job_code", func(_ LocaleContext, person Person) string { return person.JobCode }},
	{"person.job_level", "job_level", func(_ LocaleContext, person Person) string { return person.Grade }},
	{"person.hire_date", "hire_date", func(_ LocaleContext, person Person) string { return person.HireDate }},
	{"person.employment_type", "employment_type", func(LocaleContext, Person) string { return "" }},
	{"person.time_type", "time_type", func(LocaleContext, Person) string { return "" }},
	{"person.record_source", "record_source", func(_ LocaleContext, person Person) string { return person.Source }},
	{"person.record_created", "record_created", func(_ LocaleContext, person Person) string { return person.CreatedAt }},
}

// ResolveWorkerOverview resolves the worker overview section
// for one person record over the shared section engine. The
// record is never mutated.
func ResolveWorkerOverview(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.employment_overview", "person.employment_overview_detail", workerOverviewFacts)
}
