package productui

import (
	"strings"
	"testing"
)

// seededWorker is a person carrying every employment fact the HarborCare demo
// seed records, in the same vocabulary the service sends them in. It stands
// for a worker the demo tenant actually seeded, which is the case the object
// page must render with nothing unreported.
func seededWorker() Person {
	return Person{
		ID: "hc-015-rosa-santos", WorkerID: "worker-hc-015",
		Name: "Rosa", Team: "care-coordination", Manager: "Gabriel Martinez",
		WorkerNumber: "HC-21015", JobCode: "CARE-CC2", Grade: "P2",
		PositionID: "HC-POS-00015", Location: "Boston, MA", PayZone: "US-EAST",
		HireDate: "2016-03-01", Source: "CREATED", CreatedAt: "2026-09-01",
		BonusTarget:     "0.0500",
		EmploymentType:  "REGULAR",
		TimeType:        "PART_TIME",
		Company:         "HarborCare Health Services, Inc.",
		BusinessUnit:    "Care Operations",
		CostCenter:      "CC-2200 Care Coordination",
		WorkArrangement: "ON_SITE",
		PayBasis:        "ANNUAL_SALARY",
	}
}

func sectionValues(section WorkerSection) (map[string]string, map[string]WorkerFactStatus) {
	values := make(map[string]string, len(section.Facts))
	statuses := make(map[string]WorkerFactStatus, len(section.Facts))
	for _, fact := range section.Facts {
		values[fact.Name] = fact.Value
		statuses[fact.Name] = fact.Status
	}
	return values, statuses
}

// A seeded worker leaves nothing unreported. The seven facts that used to be
// hard-coded to "" in the three section files are the whole point: each one is
// asserted by value, not merely by being non-empty, so an accessor wired to
// the wrong field fails here rather than looking fixed.
func TestSeededWorkerReportsEveryEmploymentFact(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := seededWorker()

	overview, _ := sectionValues(ResolveWorkerOverview(locale, person, nil))
	employment, _ := sectionValues(ResolveWorkerEmployment(locale, person, nil))
	pay, _ := sectionValues(ResolveWorkerPay(locale, person, nil))

	want := map[string]string{
		"employment_type":  "Regular",
		"time_type":        "Part time",
		"company":          "HarborCare Health Services, Inc.",
		"business_unit":    "Care Operations",
		"cost_center":      "CC-2200 Care Coordination",
		"work_arrangement": "On-site",
		"pay_frequency":    "Annual",
	}
	got := map[string]string{
		"employment_type":  overview["employment_type"],
		"time_type":        overview["time_type"],
		"company":          employment["company"],
		"business_unit":    employment["business_unit"],
		"cost_center":      employment["cost_center"],
		"work_arrangement": employment["work_arrangement"],
		"pay_frequency":    pay["pay_frequency"],
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s = %q, want %q", name, got[name], expected)
		}
	}

	// Nothing on the page may read as unreported for this worker. The
	// stand-in is what the missing-fields disclosure collects, so finding it
	// anywhere here means a fact is still unwired.
	standIn := locale.Text("common.not_reported")
	for section, facts := range map[string]map[string]string{
		"overview": overview, "employment": employment, "pay": pay,
	} {
		for name, value := range facts {
			if value == standIn {
				t.Errorf("%s fact %q reads as unreported for a seeded worker", section, name)
			}
		}
	}
}

// Every one of the seven is PRESENT rather than MISSING. Status is what the
// disclosure counts, and a fact carrying a value but the wrong status would
// still be swept into "N fields not reported".
func TestSeededWorkerEmploymentFactsAreStatusPresent(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := seededWorker()

	_, overview := sectionValues(ResolveWorkerOverview(locale, person, nil))
	_, employment := sectionValues(ResolveWorkerEmployment(locale, person, nil))
	_, pay := sectionValues(ResolveWorkerPay(locale, person, nil))

	for _, tc := range []struct {
		name   string
		status WorkerFactStatus
	}{
		{"employment_type", overview["employment_type"]},
		{"time_type", overview["time_type"]},
		{"company", employment["company"]},
		{"business_unit", employment["business_unit"]},
		{"cost_center", employment["cost_center"]},
		{"work_arrangement", employment["work_arrangement"]},
		{"pay_frequency", pay["pay_frequency"]},
	} {
		if tc.status != WorkerFactPresent {
			t.Errorf("%s status = %q, want %q", tc.name, tc.status, WorkerFactPresent)
		}
	}
}

// A fact nobody asserts still collapses into the disclosure. Wiring the
// accessors must not have cost the page its ability to say "unreported": a
// corpus worker asserts none of these, and inventing values for them would be
// worse than the bug being fixed.
func TestAbsentEmploymentFactsStayUnreported(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	// A corpus-shaped worker: placement only, no employment facts at all.
	person := Person{ID: "jane-doe", Team: "Alpha", Manager: "Yolanda", PositionID: "POS-7", Location: "SF"}

	employmentValues, employmentStatus := sectionValues(ResolveWorkerEmployment(locale, person, nil))
	overviewValues, overviewStatus := sectionValues(ResolveWorkerOverview(locale, person, nil))
	payValues, payStatus := sectionValues(ResolveWorkerPay(locale, person, nil))

	standIn := locale.Text("common.not_reported")
	for _, tc := range []struct {
		name   string
		value  string
		status WorkerFactStatus
	}{
		{"employment_type", overviewValues["employment_type"], overviewStatus["employment_type"]},
		{"time_type", overviewValues["time_type"], overviewStatus["time_type"]},
		{"company", employmentValues["company"], employmentStatus["company"]},
		{"business_unit", employmentValues["business_unit"], employmentStatus["business_unit"]},
		{"cost_center", employmentValues["cost_center"], employmentStatus["cost_center"]},
		{"work_arrangement", employmentValues["work_arrangement"], employmentStatus["work_arrangement"]},
		{"pay_frequency", payValues["pay_frequency"], payStatus["pay_frequency"]},
	} {
		if tc.status != WorkerFactMissing {
			t.Errorf("absent %s status = %q, want %q", tc.name, tc.status, WorkerFactMissing)
		}
		if tc.value != standIn {
			t.Errorf("absent %s = %q, want the unreported stand-in %q", tc.name, tc.value, standIn)
		}
	}

	// The four employment facts being MISSING is what the disclosure counts,
	// and its threshold is two, so this worker's Organization section is
	// exactly the case the disclosure exists for.
	missing := 0
	for _, status := range employmentStatus {
		if status == WorkerFactMissing {
			missing++
		}
	}
	if missing != 4 {
		t.Fatalf("organization section has %d missing facts, want the 4 unasserted employment facts", missing)
	}
}

// The tokens are translated, not printed. A locale change must move these
// words without touching a stored row, which is the whole reason the service
// sends vocabulary rather than display text.
func TestEmploymentVocabularyIsLocalized(t *testing.T) {
	german := ResolveProductLocale("de-DE")
	person := seededWorker()

	overview, _ := sectionValues(ResolveWorkerOverview(german, person, nil))
	employment, _ := sectionValues(ResolveWorkerEmployment(german, person, nil))
	pay, _ := sectionValues(ResolveWorkerPay(german, person, nil))

	for _, tc := range []struct{ name, got, want string }{
		{"employment_type", overview["employment_type"], "Unbefristet"},
		{"time_type", overview["time_type"], "Teilzeit"},
		{"work_arrangement", employment["work_arrangement"], "Vor Ort"},
		{"pay_frequency", pay["pay_frequency"], "Jährlich"},
	} {
		if tc.got != tc.want {
			t.Errorf("de-DE %s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	// A recorded name is not a token and must survive the locale unchanged.
	if employment["company"] != "HarborCare Health Services, Inc." {
		t.Errorf("de-DE company = %q, want the recorded legal entity name", employment["company"])
	}
}

// Every token the storage vocabulary admits has a word, and a token it does
// not admit degrades to the token rather than to "⟦key⟧" debug output.
func TestEmploymentTermCoversTheVocabularyAndDegradesSafely(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	for token, key := range employmentVocabulary {
		got := employmentTerm(locale, token)
		if got == "" || got == token {
			t.Errorf("token %q rendered as %q, want the word for %q", token, got, key)
		}
		if strings.Contains(got, "⟦") {
			t.Errorf("token %q rendered an unresolved catalog key: %q", token, got)
		}
	}
	if got := employmentTerm(locale, "SABBATICAL"); got != "SABBATICAL" {
		t.Errorf("unknown token rendered as %q, want the token itself", got)
	}
	if got := employmentTerm(locale, "  "); got != "" {
		t.Errorf("blank token rendered as %q, want the empty value the section engine reports as unreported", got)
	}
}

// A withheld compensation disclosure takes the frequency with it. The basis
// is how the amount reads, so disclosing it beside a withheld figure would
// narrow exactly what the withholding protects.
func TestWithheldPayAlsoWithholdsTheFrequency(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := seededWorker()
	// The transport clears PayBasis with BasePay; this is that row as the
	// page receives it.
	person.PayBasis = ""

	values, statuses := sectionValues(ResolveWorkerPay(locale, person, nil))
	if statuses["pay_frequency"] != WorkerFactMissing {
		t.Fatalf("pay frequency status = %q, want %q when the basis was not disclosed", statuses["pay_frequency"], WorkerFactMissing)
	}
	if values["pay_frequency"] != locale.Text("common.not_reported") {
		t.Fatalf("pay frequency = %q, want the unreported stand-in", values["pay_frequency"])
	}
}
