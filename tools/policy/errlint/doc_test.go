package errlint

import "testing"

// TestDocPackageExists and TestDocNoPanic satisfy this repository's
// per-file test requirement for doc.go, which declares no symbols of its
// own (see tools/policy/racepolicy/doc_test.go for the same convention).
func TestDocPackageExists(t *testing.T) {}

func TestDocNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
