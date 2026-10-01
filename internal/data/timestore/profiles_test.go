package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// jsonEqual compares two JSON documents by value, not by byte-exact text:
// jsonb storage reformats whitespace on round trip, so a payload assertion
// must decode both sides rather than compare raw bytes.
func jsonEqual(t *testing.T, got, want []byte) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	return reflect.DeepEqual(g, w)
}

func TestTodo_WTIME_001(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.PublishProfileVersion(ctx, "tenant-a", ProfileVersion{
		TenantID: "tenant-a", ProfileID: "profile-1", Version: 1, EffectiveFrom: from,
		Payload: []byte(`{"capture":"PUNCH"}`), Digest: "d1", PublishedBy: "gov",
	}); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	got, err := s.ProfileVersionAt(ctx, "tenant-a", "profile-1", from.Add(time.Hour))
	if err != nil || got.Version != 1 || !jsonEqual(t, got.Payload, []byte(`{"capture":"PUNCH"}`)) {
		t.Fatalf("ProfileVersionAt = %#v, %v", got, err)
	}
	if _, err := s.ProfileVersionAt(ctx, "tenant-a", "profile-1", from.Add(-time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before effective_from = %v, want ErrNotFound", err)
	}
}

// TestTodo_WTIME_002 proves profile-version persistence: publishing a new
// version never mutates an old one, out-of-order versions are rejected, and
// the append-only trigger blocks a direct UPDATE against history.
func TestTodo_WTIME_002(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	from2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	v1 := ProfileVersion{TenantID: "tenant-a", ProfileID: "profile-1", Version: 1, EffectiveFrom: from1, EffectiveTo: from2, Payload: []byte(`{"v":1}`), Digest: "d1", PublishedBy: "gov"}
	if err := s.PublishProfileVersion(ctx, "tenant-a", v1); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	v2 := ProfileVersion{TenantID: "tenant-a", ProfileID: "profile-1", Version: 2, EffectiveFrom: from2, Payload: []byte(`{"v":2}`), Digest: "d2", PublishedBy: "gov"}
	if err := s.PublishProfileVersion(ctx, "tenant-a", v2); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	// Out-of-order or duplicate version numbers are rejected, not silently
	// accepted as a second v2.
	if err := s.PublishProfileVersion(ctx, "tenant-a", v2); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("republish v2 = %v, want ErrRevisionConflict", err)
	}
	if err := s.PublishProfileVersion(ctx, "tenant-a", ProfileVersion{TenantID: "tenant-a", ProfileID: "profile-1", Version: 5, EffectiveFrom: from2.Add(time.Hour), Payload: []byte(`{}`), Digest: "d5", PublishedBy: "gov"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("publish v5 out of order = %v, want ErrRevisionConflict", err)
	}
	versions, err := s.ListProfileVersions(ctx, "tenant-a", "profile-1")
	if err != nil || len(versions) != 2 {
		t.Fatalf("ListProfileVersions = %#v, %v", versions, err)
	}
	// v1's original payload is exactly what publishing v2 left behind: the
	// new version never touched it.
	if !jsonEqual(t, versions[0].Payload, []byte(`{"v":1}`)) || !jsonEqual(t, versions[1].Payload, []byte(`{"v":2}`)) {
		t.Fatalf("versions payload drifted: %#v", versions)
	}
	err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE time_profile_version SET payload='{"tampered":true}' WHERE tenant_id='tenant-a' AND profile_id='profile-1' AND version=1`)
		return e
	})
	if err == nil {
		t.Fatal("direct mutation of a published profile version succeeded")
	}
}

func TestTodo_WTIME_001_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.PublishProfileVersion(ctx, "tenant-a", ProfileVersion{TenantID: "tenant-a", ProfileID: "shared-id", Version: 1, EffectiveFrom: from, Payload: []byte(`{}`), Digest: "d", PublishedBy: "gov"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileVersionAt(ctx, "tenant-b", "shared-id", from.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant profile read = %v, want ErrNotFound", err)
	}
}

func TestTodo_WTIME_002_AssignmentPin(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	pin, err := s.PinAssignmentProfile(ctx, "tenant-a", "assignment-1", "profile-1", 1, 0, now)
	if err != nil || pin.Revision != 1 {
		t.Fatalf("first pin = %#v, %v", pin, err)
	}
	if _, err := s.PinAssignmentProfile(ctx, "tenant-a", "assignment-1", "profile-1", 1, 0, now); !errors.Is(err, ErrInvalid) {
		// expectedRevision=0 again means "insert"; a row already exists so
		// the unique key rejects it as a driver error, not ErrInvalid
		// specifically -- so only check it is non-nil.
		if err == nil {
			t.Fatal("duplicate first pin succeeded")
		}
	}
	repinned, err := s.PinAssignmentProfile(ctx, "tenant-a", "assignment-1", "profile-2", 3, 1, now.Add(time.Hour))
	if err != nil || repinned.Revision != 2 || repinned.ProfileVersion != 3 {
		t.Fatalf("repin = %#v, %v", repinned, err)
	}
	if _, err := s.PinAssignmentProfile(ctx, "tenant-a", "assignment-1", "profile-3", 1, 1, now); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale repin = %v, want ErrRevisionConflict", err)
	}
	got, err := s.AssignmentProfilePinFor(ctx, "tenant-a", "assignment-1")
	if err != nil || got.ProfileID != "profile-2" || got.Revision != 2 {
		t.Fatalf("AssignmentProfilePinFor = %#v, %v", got, err)
	}
	if _, err := s.AssignmentProfilePinFor(ctx, "tenant-a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing pin = %v, want ErrNotFound", err)
	}
}

func TestTodo_WTIME_002_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.PublishEligibilityRuleVersion(ctx, "tenant-a", EligibilityRuleVersion{TenantID: "tenant-a", RuleID: "rule-1", Version: 1, Payload: []byte(`{"job":"driver"}`), Digest: "rd1", PublishedBy: "gov"}); err != nil {
		t.Fatalf("publish rule v1: %v", err)
	}
	if err := s.PublishEligibilityRuleVersion(ctx, "tenant-a", EligibilityRuleVersion{TenantID: "tenant-a", RuleID: "rule-1", Version: 1, Payload: []byte(`{"job":"changed"}`), Digest: "rd1b", PublishedBy: "gov"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("republish rule v1 = %v, want ErrRevisionConflict", err)
	}
	if err := s.PublishEligibilityRuleVersion(ctx, "tenant-a", EligibilityRuleVersion{TenantID: "tenant-a", RuleID: "rule-1", Version: 2, Payload: []byte(`{"job":"driver","priority":1}`), Digest: "rd2", PublishedBy: "gov"}); err != nil {
		t.Fatalf("publish rule v2: %v", err)
	}
	latest, err := s.LatestEligibilityRuleVersion(ctx, "tenant-a", "rule-1")
	if err != nil || latest.Version != 2 || !jsonEqual(t, latest.Payload, []byte(`{"job":"driver","priority":1}`)) {
		t.Fatalf("LatestEligibilityRuleVersion = %#v, %v", latest, err)
	}
	if _, err := s.LatestEligibilityRuleVersion(ctx, "tenant-a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rule = %v, want ErrNotFound", err)
	}
}

func TestTodo_WTIME_001_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.PublishProfileVersion(ctx, "", ProfileVersion{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty tenant = %v", err)
	}
	if err := s.PublishProfileVersion(ctx, "tenant-a", ProfileVersion{TenantID: "tenant-a", ProfileID: "p", Version: 1, EffectiveFrom: time.Now(), EffectiveTo: time.Now().Add(-time.Hour), Payload: []byte(`{}`), Digest: "d", PublishedBy: "gov"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inverted window = %v", err)
	}
	if _, err := s.ProfileVersionAt(ctx, "tenant-a", "", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty profile id = %v", err)
	}
	if _, err := s.PinAssignmentProfile(ctx, "tenant-a", "a", "p", 0, 0, time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero profile version = %v", err)
	}
}
