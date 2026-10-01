package pgstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

func TestTodo_UXBLIND_002_RealPGFailureTimelineReloadAndRetry(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	const tenant = "tenant-uxblind-002"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	record := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, record); err != nil {
		t.Fatalf("AppendIntent: %v", err)
	}
	event := app.JourneyFailureEvent{
		Tenant: tenant, IntentID: record.IntentID, Actor: "principal:reviewer",
		ReasonRef: "intent.domain_unavailable", RevisionID: "revision-1",
		OccurredAt: time.Date(2026, 4, 5, 6, 8, 0, 0, time.UTC), IdempotencyKey: "failure-1",
	}
	if err := store.AppendJourneyFailure(ctx, event); err != nil {
		t.Fatalf("AppendJourneyFailure: %v", err)
	}
	fresh, err := New(db.Conn, WithClock(func() time.Time { return time.Date(2026, 4, 5, 6, 9, 0, 0, time.UTC) }))
	if err != nil {
		t.Fatalf("fresh New: %v", err)
	}
	entries, err := fresh.Timeline(ctx, tenant, record.IntentID)
	if err != nil {
		t.Fatalf("fresh Timeline: %v", err)
	}
	failures := failureEntries(entries)
	if len(failures) != 1 || string(failures[0].Payload) == "" {
		t.Fatalf("fresh failure entries = %+v, want one typed payload", failures)
	}
	if err := fresh.Verify(ctx, tenant, record.IntentID); err != nil {
		t.Fatalf("Verify after reload: %v", err)
	}
	if err := store.AppendJourneyFailure(ctx, event); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	if got := countFailureEvents(t, db, tenant, record.IntentID); got != 1 {
		t.Fatalf("exact retry failure count = %d, want 1", got)
	}
	distinct := event
	distinct.RevisionID = "revision-2"
	distinct.IdempotencyKey = "failure-2"
	if err := store.AppendJourneyFailure(ctx, distinct); err != nil {
		t.Fatalf("distinct revision: %v", err)
	}
	if got := countFailureEvents(t, db, tenant, record.IntentID); got != 2 {
		t.Fatalf("distinct revision failure count = %d, want 2", got)
	}
}

func TestTodo_UXBLIND_002_RealPGFailureAuthorizationAndBounds(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	const tenant, otherTenant = "tenant-uxblind-owner", "tenant-uxblind-other"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, otherTenant); err != nil {
		t.Fatal(err)
	}
	record := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, record); err != nil {
		t.Fatalf("AppendIntent: %v", err)
	}
	crossTenant := app.JourneyFailureEvent{
		Tenant: otherTenant, IntentID: record.IntentID, Actor: "principal:attacker",
		ReasonRef: "intent.domain_unavailable", RevisionID: "revision-1",
		OccurredAt: time.Date(2026, 4, 5, 6, 8, 0, 0, time.UTC), IdempotencyKey: "forged-failure",
	}
	if err := store.AppendJourneyFailure(ctx, crossTenant); !errors.Is(err, app.ErrIntentNotFound) && !strings.Contains(err.Error(), app.ErrIntentNotFound.Error()) {
		t.Fatalf("cross-tenant append error = %v, want hidden not-found", err)
	}
	if got := countFailureEvents(t, db, otherTenant, record.IntentID); got != 0 {
		t.Fatalf("cross-tenant forged stream failure count = %d, want 0", got)
	}
	invalid := crossTenant
	invalid.Tenant = tenant
	invalid.IntentID = record.IntentID
	invalid.ReasonRef = "provider.raw.failure"
	invalid.IdempotencyKey = "invalid-reason"
	if err := store.AppendJourneyFailure(ctx, invalid); err == nil {
		t.Fatal("invalid reason append succeeded")
	}
	if got := countFailureEvents(t, db, tenant, record.IntentID); got != 0 {
		t.Fatalf("invalid reason wrote %d failure events, want 0", got)
	}
	bounded := invalid
	bounded.ReasonRef = "intent.domain_unavailable"
	bounded.Actor = strings.Repeat("x", 257)
	bounded.IdempotencyKey = "oversized-actor"
	if err := store.AppendJourneyFailure(ctx, bounded); err == nil {
		t.Fatal("oversized actor append succeeded")
	}
	if got := countFailureEvents(t, db, tenant, record.IntentID); got != 0 {
		t.Fatalf("oversized actor wrote %d failure events, want 0", got)
	}
}

func TestTodo_UXBLIND_002_RealPGFailureDefaultStoreClock(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	store, err := New(db.Conn, WithClock(nil))
	if err != nil {
		t.Fatalf("New with nil clock: %v", err)
	}
	const tenant = "tenant-uxblind-default-clock"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	record := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, record); err != nil {
		t.Fatalf("AppendIntent with default clock: %v", err)
	}
	if err := store.AppendJourneyFailure(ctx, app.JourneyFailureEvent{
		Tenant: tenant, IntentID: record.IntentID, Actor: "principal:reviewer",
		ReasonRef: "intent.domain_unavailable", RevisionID: "revision-default",
		OccurredAt: time.Date(2026, 4, 5, 6, 8, 0, 0, time.UTC), IdempotencyKey: "failure-default-clock",
	}); err != nil {
		t.Fatalf("AppendJourneyFailure with default clock: %v", err)
	}
}

func failureEntries(entries []app.TimelineEntry) []app.TimelineEntry {
	out := make([]app.TimelineEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.SchemaRef == app.JourneyFailureSchemaRef {
			out = append(out, entry)
		}
	}
	return out
}

func countFailureEvents(t *testing.T, db *pgtest.DB, tenant, intentID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), `
		SELECT count(*) FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND schema_ref = $3`,
		TenantID(tenant), StreamKey(intentID), app.JourneyFailureSchemaRef).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
