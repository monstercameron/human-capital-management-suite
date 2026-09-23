package fixtures_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func positionAsOf(t *testing.T, effective string) position.AsOf {
	t.Helper()
	effectiveOn, err := values.ParseLocalDate(effective)
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, "2026-05-15T12:00:00Z")
	if err != nil {
		t.Fatalf("parse instant: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(parsed.UTC()))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return position.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt}
}

func positionRef(t *testing.T, catalog *fixtures.MemoryPositionCatalog, code string) values.EntityRef {
	t.Helper()
	ref, ok := catalog.PositionRefForCode(code)
	if !ok {
		t.Fatalf("PositionRefForCode(%q) unknown", code)
	}
	return ref
}

func TestMemoryPositionCatalog(t *testing.T) {
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		t.Fatalf("NewMemoryPositionCatalog: %v", err)
	}
	ctx := context.Background()

	revision, exists, err := catalog.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant:   fixtures.Tenant,
		Position: positionRef(t, catalog, "POS-HRBP-301"),
		AsOf:     positionAsOf(t, "2026-06-01"),
	})
	if err != nil {
		t.Fatalf("PositionRevisionAt: %v", err)
	}
	if !exists {
		t.Fatal("POS-HRBP-301 does not resolve")
	}
	if revision.JobCode != "OPS-HRBP3" || revision.OrgUnit != "people-ops" {
		t.Fatalf("revision=%+v", revision)
	}
	if revision.Lifecycle != position.LifecycleOpen {
		t.Fatalf("lifecycle=%s", revision.Lifecycle)
	}
	if err := revision.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// A code the catalog does not name has no reference: callers turn
	// that into a governed absence, never a guessed identity.
	if _, ok := catalog.PositionRefForCode("POS-HRBP-999"); ok {
		t.Fatal("POS-HRBP-999 resolved")
	}

	// A coordinate before the revision's effective start has no revision.
	_, exists, err = catalog.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant:   fixtures.Tenant,
		Position: positionRef(t, catalog, "POS-HRBP-301"),
		AsOf:     positionAsOf(t, "2025-01-01"),
	})
	if err != nil {
		t.Fatalf("PositionRevisionAt early: %v", err)
	}
	if exists {
		t.Fatal("position resolved before its effective start")
	}

	if _, _, err := catalog.PositionRevisionAt(ctx, position.PositionQuery{}); err == nil {
		t.Fatal("empty query succeeded")
	}
}
