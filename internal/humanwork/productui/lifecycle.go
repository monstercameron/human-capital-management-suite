package productui

import "strings"

// LifecycleStatus is the worker lifecycle token the directory,
// the worker object and action discovery share. Tokens arrive
// from journey_worker through the listing (ACTIVE by column
// default) and travel as plain strings on WorkerSummary, the
// wire Worker and Person; this type is the single vocabulary
// interpreting them, so the query builder, the action gate and
// the presentation layer can never disagree about what
// "terminated" means.
type LifecycleStatus string

const (
	LifecycleActive     LifecycleStatus = "ACTIVE"
	LifecycleTerminated LifecycleStatus = "TERMINATED"
	LifecycleOnLeave    LifecycleStatus = "ON_LEAVE"
	// LifecycleUnknown is the zero value: nobody asserted a
	// status. Unknown workers stay visible under the default
	// directory filter — corpus workers assert no lifecycle at
	// all, and an unevaluated record must not vanish — while a
	// terminated worker is always explicit.
	LifecycleUnknown LifecycleStatus = ""
)

// ParseLifecycleStatus normalizes one stored token. Matching is
// case-insensitive with surrounding whitespace ignored, because
// created workers carry the lower-case corpus spelling
// ("active") beside the column default ("ACTIVE"), with hyphens
// read as underscores ("on-leave" is ON_LEAVE). Anything
// unrecognized reports LifecycleUnknown, never a guessed state.
func ParseLifecycleStatus(token string) LifecycleStatus {
	normalized := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(token)), "-", "_")
	switch LifecycleStatus(normalized) {
	case LifecycleActive:
		return LifecycleActive
	case LifecycleTerminated:
		return LifecycleTerminated
	case LifecycleOnLeave:
		return LifecycleOnLeave
	default:
		return LifecycleUnknown
	}
}

// ExcludedByDefault reports whether the default active-only
// directory keeps this status out. Only explicit terminated and
// on-leave states hide; active and unreported stay.
func (s LifecycleStatus) ExcludedByDefault() bool {
	return s == LifecycleTerminated || s == LifecycleOnLeave
}

// WorkerType is the employment worker-type token the directory
// and the worker object share: EMPLOYEE, CONTRACTOR, INTERN or
// TEMPORARY per the journey_worker vocabulary. It travels beside
// the lifecycle status on WorkerSummary, the wire Worker and
// Person, and is interpreted only here.
type WorkerType string

const (
	WorkerTypeEmployee   WorkerType = "EMPLOYEE"
	WorkerTypeContractor WorkerType = "CONTRACTOR"
	WorkerTypeIntern     WorkerType = "INTERN"
	WorkerTypeTemporary  WorkerType = "TEMPORARY"
	WorkerTypeUnknown    WorkerType = ""
)

// ParseWorkerType normalizes one stored token, case-insensitively.
// Anything unrecognized reports WorkerTypeUnknown.
func ParseWorkerType(token string) WorkerType {
	switch WorkerType(strings.ToUpper(strings.TrimSpace(token))) {
	case WorkerTypeEmployee:
		return WorkerTypeEmployee
	case WorkerTypeContractor:
		return WorkerTypeContractor
	case WorkerTypeIntern:
		return WorkerTypeIntern
	case WorkerTypeTemporary:
		return WorkerTypeTemporary
	default:
		return WorkerTypeUnknown
	}
}

// PeopleStatusFilter is the directory lifecycle admission rule.
// The zero value is the documented default: active-only, with
// unreported workers staying visible and terminated and
// on-leave workers hidden until an explicit opt-in names them.
type PeopleStatusFilter int

const (
	PeopleStatusActiveOnly PeopleStatusFilter = iota
	PeopleStatusIncludeTerminated
	PeopleStatusIncludeOnLeave
	PeopleStatusIncludeAll
)

// ParsePeopleStatusFilter resolves one raw directory status
// part. Empty, "active" and anything unrecognized all resolve
// to the active-only default, fail-closed: no raw string can
// widen the directory by omission or typo.
func ParsePeopleStatusFilter(raw string) PeopleStatusFilter {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "terminated":
		return PeopleStatusIncludeTerminated
	case "on-leave", "on_leave", "leave":
		return PeopleStatusIncludeOnLeave
	case "all":
		return PeopleStatusIncludeAll
	default:
		return PeopleStatusActiveOnly
	}
}

// Admits reports whether a worker carrying the given lifecycle
// token passes this filter. Unknown and empty tokens pass every
// filter but the reading is explicit: only named terminated and
// on-leave states are ever hidden.
func (f PeopleStatusFilter) Admits(token string) bool {
	status := ParseLifecycleStatus(token)
	switch f {
	case PeopleStatusIncludeAll:
		return true
	case PeopleStatusIncludeTerminated:
		return status != LifecycleOnLeave
	case PeopleStatusIncludeOnLeave:
		return status != LifecycleTerminated
	default:
		return !status.ExcludedByDefault()
	}
}

// lifecycleDisplayLabel renders the distinct header state for one
// worker: the terminated and on-leave marks first, then the
// contingent worker types. Active employees carry no mark —
// they are the directory's unmarked default — and unknown
// states render nothing rather than a guessed badge.
func lifecycleDisplayLabel(locale LocaleContext, status LifecycleStatus, workerType WorkerType) string {
	switch {
	case status == LifecycleTerminated:
		return locale.Text("person.lifecycle_status.terminated")
	case status == LifecycleOnLeave:
		return locale.Text("person.lifecycle_status.on_leave")
	case workerType == WorkerTypeContractor:
		return locale.Text("person.worker_type.contractor")
	case workerType == WorkerTypeIntern:
		return locale.Text("person.worker_type.intern")
	case workerType == WorkerTypeTemporary:
		return locale.Text("person.worker_type.temporary")
	default:
		return ""
	}
}
