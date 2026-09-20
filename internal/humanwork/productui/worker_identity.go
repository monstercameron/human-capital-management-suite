package productui

// WorkerIdentity is the governed worker header facts for one
// person record: the verdict-projected display name and
// role plus the pass-through avatar facts. It generalizes
// the exact pattern the search surface already follows, so
// every header projects denied names identically and no
// header invents its own rule.
type WorkerIdentity struct {
	// StatusLabel is the distinct lifecycle mark the header
	// shows: the terminated, on-leave or contingent label, or
	// empty for an active employee, who is the unmarked default.
	StatusLabel        string
	Name               string
	WorkerNumber       string
	Label              string
	Role               string
	Initials           string
	PhotoURL           string
	NameStatus         WorkerFactStatus
	WorkerNumberStatus WorkerFactStatus
	RoleStatus         WorkerFactStatus
}

// workerIdentityVerdicts applies the population-scoped verdict contract.
// Journey-only decisions do not imply that the worker directory is governed.
func workerIdentityVerdicts(view View) map[string]AuthorizedRecord {
	if !peopleVerdictsPresent(view) {
		return nil
	}
	return view.RecordVerdicts
}

// ResolveWorkerIdentity resolves one person record to its
// worker identity header facts. Name and role travel through
// the discovery projection; initials and photo pass through
// untouched. The record is never mutated.
func ResolveWorkerIdentity(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerIdentity {
	name, nameStatus := resolveIdentityField(locale, person, verdicts, "name", person.Name)
	workerNumber, workerNumberStatus := resolveIdentityField(locale, person, verdicts, "worker_number", person.WorkerNumber)
	role, roleStatus := resolveIdentityField(locale, person, verdicts, "role", person.Role)
	label := name
	if nameStatus == WorkerFactPresent && workerNumberStatus == WorkerFactPresent {
		label = locale.Text("person.profile_identity", map[string]string{"name": name, "worker": workerNumber})
	} else if nameStatus != WorkerFactPresent && workerNumberStatus == WorkerFactPresent {
		label = workerNumber
	}
	return WorkerIdentity{
		StatusLabel: lifecycleDisplayLabel(locale, ParseLifecycleStatus(person.LifecycleStatus), ParseWorkerType(person.WorkerType)),
		Name:        name, WorkerNumber: workerNumber, Label: label, Role: role,
		Initials: person.Initials, PhotoURL: person.PhotoURL,
		NameStatus: nameStatus, WorkerNumberStatus: workerNumberStatus, RoleStatus: roleStatus,
	}
}

func resolveIdentityField(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord, name, raw string) (string, WorkerFactStatus) {
	if len(verdicts) == 0 {
		if raw == "" {
			return valueOrUnavailableFor(locale, raw), WorkerFactMissing
		}
		return raw, WorkerFactPresent
	}
	record, ok := verdicts[person.ID]
	if !ok {
		return locale.Text("provenance.value.withheld"), WorkerFactUnknown
	}
	if !record.Disclosable {
		return locale.Text("provenance.value.withheld"), WorkerFactWithheld
	}
	// Older discovery responses authorize whole worker summaries without
	// emitting per-field decisions. Preserve that record-level contract;
	// once any field verdict is present, absent fields fail closed below.
	if len(record.Fields) == 0 {
		if raw == "" {
			return valueOrUnavailableFor(locale, raw), WorkerFactMissing
		}
		return raw, WorkerFactPresent
	}
	field, ok := record.Fields[name]
	if !ok {
		return locale.Text("provenance.value.withheld"), WorkerFactUnknown
	}
	projected, admitted := ProjectField(locale, raw, field)
	if !admitted {
		return locale.Text("provenance.value.withheld"), WorkerFactWithheld
	}
	status := WorkerFactWithheld
	if field.Disposition == FieldShow || field.Disposition == "" && field.Effect == PresentationAllow {
		status = WorkerFactPresent
		if raw == "" {
			status = WorkerFactMissing
		}
	}
	return projected.Text, status
}
