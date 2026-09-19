package productui

import (
	"strings"
	"time"
)

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
	{"person.hire_date", "hire_date", func(locale LocaleContext, person Person) string { return localizedRecordDate(locale, person.HireDate) }},
	{"person.employment_type", "employment_type", func(LocaleContext, Person) string { return "" }},
	{"person.time_type", "time_type", func(LocaleContext, Person) string { return "" }},
	{"person.record_source", "record_source", func(_ LocaleContext, person Person) string { return person.Source }},
	{"person.record_created", "record_created", func(locale LocaleContext, person Person) string { return localizedRecordDate(locale, person.CreatedAt) }},
}

// localizedRecordDate shows a record's date the way the reader's locale
// writes dates ("17 Aug 2019"), as the journey pages already do, rather than
// the raw ISO key ("2019-08-17"). A value that is not an ISO date or
// timestamp is shown as it came, so nothing is ever lost to a failed parse.
func localizedRecordDate(locale LocaleContext, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return raw
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return locale.FormatDate(parsed)
		}
	}
	return raw
}

// ResolveWorkerOverview resolves the worker overview section
// for one person record over the shared section engine. The
// record is never mutated.
func ResolveWorkerOverview(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.employment_overview", "person.employment_overview_detail", workerOverviewFacts)
}
