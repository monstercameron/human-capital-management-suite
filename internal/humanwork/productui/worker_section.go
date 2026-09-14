package productui

// sectionFact is one worker section fact field: label key,
// field key, and locale-aware record accessor, in page
// order. Accessors needing no locale ignore it.
type sectionFact struct {
	labelKey string
	name     string
	value    func(LocaleContext, Person) string
}

// resolveWorkerSection resolves one worker object page
// section with the page adapter's exact fact behavior:
// silent servers pass values through (unavailable for empty),
// governed records project each field through its verdict with
// HIDE omitting the row. It additionally records whether each
// visible value is PRESENT, MISSING, UNKNOWN, or WITHHELD;
// status is kept separate from display text so a safe stand-in
// cannot be mistaken for source data. Sections share this engine
// so fact order, labels, and verdict handling cannot drift between
// sections. The record is never mutated.
func resolveWorkerSection(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord, titleKey, descriptionKey string, fields []sectionFact) WorkerSection {
	silent := len(verdicts) == 0
	verdictFields := map[string]AuthorizedField{}
	verdict, hasVerdict := verdicts[person.ID]
	if hasVerdict {
		verdictFields = verdict.Fields
	}
	facts := make([]WorkerFact, 0, len(fields))
	for _, field := range fields {
		raw := field.value(locale, person)
		if silent {
			status := WorkerFactPresent
			if raw == "" {
				status = WorkerFactMissing
			}
			facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: valueOrUnavailableFor(locale, raw), Status: status})
			continue
		}
		if hasVerdict && !verdict.Disclosable {
			facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: locale.Text("provenance.value.withheld"), Status: WorkerFactWithheld})
			continue
		}
		fieldVerdict, fieldKnown := verdictFields[field.name]
		if !fieldKnown {
			// A governed record without a field verdict is not the same as an
			// empty source value. Keep the row visible but label the uncertainty
			// explicitly; the legacy withheld text remains the safe stand-in.
			projected, _ := ProjectField(locale, raw, AuthorizedField{})
			facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: projected.Text, Status: WorkerFactUnknown})
			continue
		}
		projected, admitted := ProjectField(locale, raw, fieldVerdict)
		if !admitted {
			continue
		}
		status := WorkerFactWithheld
		if fieldVerdict.Disposition == FieldShow || fieldVerdict.Disposition == "" && fieldVerdict.Effect == PresentationAllow {
			status = WorkerFactPresent
			if raw == "" {
				status = WorkerFactMissing
			}
		}
		facts = append(facts, WorkerFact{Name: field.name, Label: locale.Text(field.labelKey), Value: projected.Text, Status: status})
	}
	return WorkerSection{
		Title:       locale.Text(titleKey),
		Description: locale.Text(descriptionKey),
		Facts:       facts,
	}
}
