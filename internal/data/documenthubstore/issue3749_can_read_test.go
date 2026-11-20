package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// CanRead is the gate the document's "Discussed in" list stands behind: a
// reader with a grant passes, one without and an unknown document both read as
// denied, and nothing crosses a tenant.
func TestIssue3749_CanReadGatesOnTheReadersOwnAccess_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	doc, _ := linkDoc(t, s, ctx, "tenant-a", "# Handbook\n")
	grantRead(t, s, ctx, "tenant-a", doc, "u-anna")
	if err := s.CanRead(ctx, "tenant-a", doc, "person", "u-anna"); err != nil {
		t.Fatalf("a reader with a grant: %v", err)
	}
	for name, call := range map[string]func() error{
		"no grant":       func() error { return s.CanRead(ctx, "tenant-a", doc, "person", "u-bob") },
		"unknown":        func() error { return s.CanRead(ctx, "tenant-a", "missing", "person", "u-anna") },
		"other tenant":   func() error { return s.CanRead(ctx, "tenant-b", doc, "person", "u-anna") },
		"blank reader":   func() error { return s.CanRead(ctx, "tenant-a", doc, "person", "") },
		"blank document": func() error { return s.CanRead(ctx, "tenant-a", "", "person", "u-anna") },
	} {
		if err := call(); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v, want denied", name, err)
		}
	}
}
