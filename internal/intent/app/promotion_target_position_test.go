package app

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// WF-RUN-034: a cell with no execution database cannot read a committed
// placement or pick a target position, and must say so quietly rather than
// inventing one or panicking on the missing database.
func TestJourneyEngineTargetPositionWithoutDatabase(t *testing.T) {
	t.Parallel()
	engine := &journeyEngine{}
	principal := &trust.Principal{}

	ref, err := engine.selectTargetPosition(context.Background(), principal, "org.eng", "JOB-1", "L5", mustPromotionLocalDate(t, 2026, time.March, 1))
	if err != nil {
		t.Fatalf("select the target position without a database: %v", err)
	}
	if ref != "" {
		t.Fatalf("target position reference without a database = %q, want empty", ref)
	}

	placement, found, err := engine.committedPay(context.Background(), principal, "worker-1")
	if err != nil {
		t.Fatalf("read the committed pay without a database: %v", err)
	}
	if found {
		t.Fatalf("committed pay without a database reported %+v, want none", placement)
	}
}

// WF-RUN-034: a proposal's effective start anchors the budget reservation, so
// it must resolve for both interval kinds the served path mints - the
// LOCAL_DATE intervals the fixtures carry and the INSTANT intervals a served
// propose derives from its requested effective instant - and each must land on
// the same day-aligned UTC instant.
func TestEffectiveStartOfBothIntervalKinds(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	dated, err := values.NewOpenLocalDateInterval(mustPromotionLocalDate(t, 2026, time.March, 1), values.CalendarRef{Ref: "gregorian", Version: "v1"})
	if err != nil {
		t.Fatalf("build a local-date interval: %v", err)
	}
	got, ok := effectiveStartOf(dated)
	if !ok || !got.Equal(want) {
		t.Fatalf("effectiveStartOf(local date) = %s, %t, want %s, true", got, ok, want)
	}

	instant, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(2026, time.March, 1, 17, 42, 13, 0, time.UTC)))
	if err != nil {
		t.Fatalf("build an instant interval: %v", err)
	}
	got, ok = effectiveStartOf(instant)
	if !ok || !got.Equal(want) {
		t.Fatalf("effectiveStartOf(instant) = %s, %t, want %s, true", got, ok, want)
	}

	if got, ok := effectiveStartOf(values.EffectiveInterval{}); ok {
		t.Fatalf("effectiveStartOf(zero interval) = %s, true, want no start", got)
	}
}

func mustPromotionLocalDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	date, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("build a local date: %v", err)
	}
	return date
}
