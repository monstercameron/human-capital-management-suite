package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_044 proves accessible failure and recovery pages seed per
// error class, each with a recovery action and an announced label.
func TestTodo_ALIGN_044(t *testing.T) {
	failures := DefaultFailurePages()
	if err := failures.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := failures.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	page, err := failures.PageFor("stale_projection")
	if err != nil {
		t.Fatalf("PageFor: %v", err)
	}
	if page.RecoveryAction != "failure.refetch" || page.AccessibilityLabel == "" {
		t.Fatalf("page = %+v", page)
	}
	if _, err := failures.PageFor("unknown_catastrophe"); !errors.Is(err, ErrFailureUnknown) {
		t.Fatalf("PageFor(unknown) = %v, want ErrFailureUnknown", err)
	}
}

func TestTodo_ALIGN_044_Property(t *testing.T) {
	first, second := DefaultFailurePages(), DefaultFailurePages()
	if first.Digest != second.Digest {
		t.Fatalf("failure seed is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	for _, class := range []string{"unauthorized", "stale_projection", "invalid_submission", "service_unavailable"} {
		firstPage, err := first.PageFor(class)
		if err != nil {
			t.Fatalf("PageFor(%s): %v", class, err)
		}
		secondPage, err := second.PageFor(class)
		if err != nil || firstPage != secondPage {
			t.Fatalf("PageFor(%s) is not stable", class)
		}
	}
}

func TestTodo_ALIGN_044_Golden(t *testing.T) {
	failures := DefaultFailurePages()
	const wantDigest = "sha256:40962d4d7d39d25064dc1ccaad56aefe949e3f56705900f04840ab83b4c1a766"
	if failures.Digest != wantDigest {
		t.Fatalf("failure digest=%q want=%q", failures.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_044_Security(t *testing.T) {
	// A class without a label is refused: silent failures cannot ship.
	unlabeled := DefaultFailurePages()
	unlabeled.Pages[0].AccessibilityLabel = ""
	if err := unlabeled.Validate(); !errors.Is(err, ErrFailureInvalid) {
		t.Fatalf("Validate(unlabeled) = %v, want ErrFailureInvalid", err)
	}
	// A class without a recovery action is a dead end: refused.
	deadEnd := DefaultFailurePages()
	deadEnd.Pages[0].RecoveryAction = ""
	if err := deadEnd.Validate(); !errors.Is(err, ErrFailureInvalid) {
		t.Fatalf("Validate(dead end) = %v, want ErrFailureInvalid", err)
	}
	// Duplicate classes are refused.
	duplicated := DefaultFailurePages()
	duplicated.Pages = append(duplicated.Pages, duplicated.Pages[0])
	if err := duplicated.Validate(); !errors.Is(err, ErrFailureInvalid) {
		t.Fatalf("Validate(duplicate) = %v, want ErrFailureInvalid", err)
	}
}

func TestTodo_ALIGN_044_Conformance(t *testing.T) {
	failures := DefaultFailurePages()
	// The four product failure modes each carry a page, a recovery, and
	// an announced label: no error is a dead end and none is invisible
	// to assistive technology.
	for _, class := range []string{"unauthorized", "stale_projection", "invalid_submission", "service_unavailable"} {
		page, err := failures.PageFor(class)
		if err != nil {
			t.Fatalf("failure mode %q has no page: %v", class, err)
		}
		if !safeID(page.PageID) || !safeID(page.RecoveryAction) || len(page.AccessibilityLabel) < 8 {
			t.Fatalf("failure page %+v is incomplete", page)
		}
	}
}

func FuzzTodo_ALIGN_044_Fuzz(f *testing.F) {
	f.Add("stale_projection")
	f.Fuzz(func(t *testing.T, class string) {
		failures := DefaultFailurePages()
		first, firstErr := failures.PageFor(class)
		second, secondErr := failures.PageFor(class)
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("failure lookup is not deterministic for %q", class)
		}
	})
}
