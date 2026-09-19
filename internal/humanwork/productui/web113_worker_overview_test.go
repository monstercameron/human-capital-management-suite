package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-113: the worker overview section. The object
// page resolves sections independently per spec, but the
// overview facts live only inside the page adapter's
// closure: nothing names the overview fact set or its
// verdict behavior outside the page, so a second overview
// consumer reimplements the list by convention. The
// compiler needs the independent constructor — the page's
// exact fact set with the page's exact silent/governed
// behavior — as the section's governed content.
func TestTodo_WEB_113(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{
		ID: "worker-avery", WorkerNumber: "WN-1", JobCode: "JC-2", Grade: "G7",
		HireDate: "2020-01-15", Source: "HRIS", CreatedAt: "2020-01-16",
	}

	silent := ResolveWorkerOverview(locale, person, nil)
	if silent.Title != locale.Text("person.employment_overview") {
		t.Fatalf("overview title = %q", silent.Title)
	}
	var names []string
	values := map[string]string{}
	for _, fact := range silent.Facts {
		names = append(names, fact.Name)
		values[fact.Name] = fact.Value
	}
	wantNames := []string{"worker_number", "job_code", "job_level", "hire_date", "employment_type", "time_type", "record_source", "record_created"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("overview facts = %q", names)
	}
	if values["worker_number"] != "WN-1" || values["job_code"] != "JC-2" || values["job_level"] != "G7" {
		t.Fatalf("overview values = %+v", values)
	}
	if values["employment_type"] != locale.Text("common.not_reported") {
		t.Fatalf("empty fact reads as %q", values["employment_type"])
	}
	if silent.Facts[0].Label != locale.Text("person.worker_number") {
		t.Fatalf("fact label = %q", silent.Facts[0].Label)
	}

	// Governed records hide withheld facts and project the rest.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"worker_number": {Effect: PresentationAllow},
			"job_code":      {Disposition: FieldHide},
		}},
	}
	governed := ResolveWorkerOverview(locale, person, verdicts)
	for _, fact := range governed.Facts {
		if fact.Name == "job_code" {
			t.Fatal("hidden fact survives")
		}
	}
	if len(governed.Facts) != len(silent.Facts)-1 {
		t.Fatalf("governed facts = %d, want %d", len(governed.Facts), len(silent.Facts)-1)
	}
}

// Golden: overview outcomes over person/verdict pairs.
func TestTodo_WEB_113_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", WorkerNumber: "WN-1", JobCode: "JC-1"},
		{ID: "w-2", HireDate: "2021-06-01"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"job_code": {Disposition: FieldHide}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			section := ResolveWorkerOverview(locale, person, verdicts)
			for _, fact := range section.Facts {
				fmt.Fprintf(&builder, "%s|%s\x00", fact.Name, fact.Value)
			}
			builder.WriteString("\n")
		}
		builder.WriteString("==\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	// Re-pinned 2026-09-19: hire_date and record_created now read in the
	// reader's locale (localizedRecordDate) instead of as raw ISO keys; the
	// names, order and every other value are unchanged.
	const want = "299166fca6b93f47d69d7785ad94b1dd91ca23973f9c3ee769f2f7cbafe85bd4"
	if got != want {
		t.Fatalf("overview digest = %s, want %s", got, want)
	}
}

// Browser: overviews over person patterns resolve
// deterministically without mutating the record.
func TestTodo_WEB_113_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", WorkerNumber: "WN-9", Grade: "G1"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerOverview(locale, person, nil)
		second := ResolveWorkerOverview(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("overview resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("overview resolution is nondeterministic")
		}
		if len(first.Facts) != 8 {
			t.Fatalf("silent overview holds %d facts", len(first.Facts))
		}
	}
}

// Conformance: the constructor reproduces the page adapter's
// fact behavior exactly — silent passthrough, HIDE omission,
// unavailable-for-empty.
func TestTodo_WEB_113_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", WorkerNumber: "", JobCode: "JC-3"}
	silent := ResolveWorkerOverview(locale, person, nil)
	if silent.Facts[0].Value != locale.Text("common.not_reported") {
		t.Fatalf("empty silent fact reads as %q", silent.Facts[0].Value)
	}
	if silent.Facts[1].Value != "JC-3" {
		t.Fatalf("silent fact rewritten: %+v", silent.Facts[1])
	}
	if silent.Description != locale.Text("person.employment_overview_detail") {
		t.Fatalf("overview description = %q", silent.Description)
	}
	again := ResolveWorkerOverview(locale, person, nil)
	if !reflect.DeepEqual(silent, again) {
		t.Fatal("overview resolution is unstable")
	}
}
