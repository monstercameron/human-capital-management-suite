package productui

// WorkerIdentity is the governed worker header facts for one
// person record: the verdict-projected display name and
// role plus the pass-through avatar facts. It generalizes
// the exact pattern the search surface already follows, so
// every header projects denied names identically and no
// header invents its own rule.
type WorkerIdentity struct {
	Name       string
	Role       string
	Initials   string
	PhotoURL   string
	NameStatus WorkerFactStatus
	RoleStatus WorkerFactStatus
}

// ResolveWorkerIdentity resolves one person record to its
// worker identity header facts. Name and role travel through
// the discovery projection; initials and photo pass through
// untouched. The record is never mutated.
func ResolveWorkerIdentity(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerIdentity {
	name, nameStatus := resolveIdentityField(locale, person, verdicts, "name", person.Name)
	role, roleStatus := resolveIdentityField(locale, person, verdicts, "role", person.Role)
	return WorkerIdentity{
		Name: name, Role: role, Initials: person.Initials, PhotoURL: person.PhotoURL,
		NameStatus: nameStatus, RoleStatus: roleStatus,
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
