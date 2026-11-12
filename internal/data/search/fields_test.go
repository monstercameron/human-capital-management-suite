package search_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/search"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// TestClassificationClearedIsTheDeclaredAllowlist proves the allowlist is
// exactly the small set of directory-shaped fields this package documents,
// and that every other people.FieldID -- in particular the
// compensation/status-adjacent ones a search box has no business matching on
// -- is excluded.
func TestClassificationClearedIsTheDeclaredAllowlist(t *testing.T) {
	t.Parallel()
	wantCleared := map[people.FieldID]bool{
		people.FieldWorkerNumber:     true,
		people.FieldLegalName:        true,
		people.FieldPreferredName:    true,
		people.FieldJobCode:          true,
		people.FieldOrgUnit:          true,
		people.FieldLocation:         true,
		people.FieldPositionID:       true,
		people.FieldLifecycleStatus:  false,
		people.FieldEmploymentID:     false,
		people.FieldLegalEntity:      false,
		people.FieldWorkerType:       false,
		people.FieldHireDate:         false,
		people.FieldEmploymentStatus: false,
		people.FieldAssignmentID:     false,
		people.FieldGrade:            false,
		people.FieldPayZone:          false,
		people.FieldFTE:              false,
		people.FieldManagerRelation:  false,
		// WTIME-001 time-profile fields: overtime exemption, capture mode and
		// profile reference are pay/time classification, not directory text.
		people.FieldExemptionStatus: false,
		people.FieldTimeCaptureMode: false,
		people.FieldTimeProfileRef:  false,
	}
	for _, field := range people.AllFields() {
		want, declared := wantCleared[field]
		if !declared {
			t.Fatalf("field %s is not covered by this test's expectation table; add it explicitly", field)
		}
		if got := search.ClassificationCleared(field); got != want {
			t.Errorf("ClassificationCleared(%s) = %v, want %v", field, got, want)
		}
	}
}

// TestClearedFieldsIsSortedAndMatchesClassificationCleared proves
// [search.ClearedFields] enumerates exactly the fields
// [search.ClassificationCleared] accepts, in a fixed sorted order so
// [search.BuildSearchText] never depends on map iteration order.
func TestClearedFieldsIsSortedAndMatchesClassificationCleared(t *testing.T) {
	t.Parallel()
	fields := search.ClearedFields()
	if len(fields) == 0 {
		t.Fatal("ClearedFields() is empty")
	}
	for i, f := range fields {
		if !search.ClassificationCleared(f) {
			t.Errorf("ClearedFields()[%d] = %s, but ClassificationCleared reports it uncleared", i, f)
		}
		if i > 0 && !(fields[i-1] < f) {
			t.Errorf("ClearedFields() is not strictly sorted at index %d: %s then %s", i, fields[i-1], f)
		}
	}
}

// TestEntityKindValidateAcceptsOnlyTheDeclaredSet proves the closed subject
// kind set: [search.KindWorker] validates, and any other token -- including
// an empty one -- is refused with [search.InvalidKindError].
func TestEntityKindValidateAcceptsOnlyTheDeclaredSet(t *testing.T) {
	t.Parallel()
	if err := search.KindWorker.Validate(); err != nil {
		t.Fatalf("KindWorker.Validate() = %v, want nil", err)
	}
	for _, bad := range []search.EntityKind{"", "candidate", "WORKER", "position"} {
		if err := bad.Validate(); err == nil {
			t.Errorf("EntityKind(%q).Validate() = nil, want an error", bad)
		}
	}
}
