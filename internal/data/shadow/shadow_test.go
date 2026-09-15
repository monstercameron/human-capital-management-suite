package shadow

import (
	"errors"
	"testing"
	"time"
)

func fixtureSnapshots() (active Snapshot, shadow Snapshot) {
	active = Snapshot{
		Tenant: "tenant-a", Projection: "worker_projection",
		Watermark: 100, Rows: 10, Digest: "sha256:active",
		AuthzDigest: "sha256:authz", ReplayComplete: true,
		Lag: 5 * time.Second,
	}
	shadow = Snapshot{
		Tenant: "tenant-a", Projection: "worker_projection",
		Watermark: 100, Rows: 10, Digest: "sha256:active",
		AuthzDigest: "sha256:authz", ReplayComplete: true,
		Lag: 4 * time.Second,
	}
	return active, shadow
}

func fixtureApproval() Approval {
	return Approval{Approver: "release-captain", Approved: true, Reason: "fixtures verified"}
}

// TestTodo_DATA_011 is DATA-011's PRIMARY proof: only an approved,
// fully-replayed, compatible shadow promotes, and the previous active
// pointer is retained for rollback.
func TestTodo_DATA_011(t *testing.T) {
	active, shadow := fixtureSnapshots()
	comparison, err := Compare(active, shadow, 30*time.Second)
	if err != nil {
		t.Fatalf("Compare identical: %v", err)
	}
	if !comparison.Compatible {
		t.Fatalf("identical snapshots incompatible: %+v", comparison)
	}
	pointer := Pointer{Active: Version{Digest: "sha256:old", Watermark: 90}}
	promoted, err := Promote(pointer, shadow, fixtureApproval(), comparison)
	if err != nil {
		t.Fatalf("Promote compatible: %v", err)
	}
	if promoted.Active.Digest != shadow.Digest || promoted.Active.Watermark != shadow.Watermark {
		t.Fatalf("promoted active = %+v, want the shadow version", promoted.Active)
	}
	if promoted.Rollback == nil || promoted.Rollback.Digest != "sha256:old" || promoted.Rollback.Watermark != 90 {
		t.Fatalf("rollback pointer = %+v, want the previous active retained", promoted.Rollback)
	}

	// Differing rows refuse.
	shadow.Rows = 11
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrComparisonGap) {
		t.Fatalf("row-count drift error = %v, want ErrComparisonGap", err)
	}

	// Differing authorization behavior refuses.
	active, shadow = fixtureSnapshots()
	shadow.AuthzDigest = "sha256:other"
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrComparisonGap) {
		t.Fatalf("authz drift error = %v, want ErrComparisonGap", err)
	}

	// Unapproved promotion refuses even a compatible comparison.
	active, shadow = fixtureSnapshots()
	comparison, err = Compare(active, shadow, 30*time.Second)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if _, err := Promote(pointer, shadow, Approval{}, comparison); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("unapproved promote error = %v, want ErrNotApproved", err)
	}
}

// TestTodo_DATA_011_Golden pins the incompatible-comparison report so a
// dropped difference class cannot hide behind a passing count.
func TestTodo_DATA_011_Golden(t *testing.T) {
	active, shadow := fixtureSnapshots()
	shadow.Rows = 9
	shadow.Digest = "sha256:drifted"
	_, err := Compare(active, shadow, 30*time.Second)
	if !errors.Is(err, ErrComparisonGap) {
		t.Fatalf("Compare drifted: %v, want ErrComparisonGap", err)
	}
	var gap *GapError
	if !errors.As(err, &gap) {
		t.Fatalf("gap error carries no report: %T", err)
	}
	report := gap.Report
	const wantDigest = "sha256:886d798fab6b0ba599a5bf0ac4bb88152575e95025e2cef74bb0c418108fdfff"
	if report.Digest != wantDigest {
		t.Fatalf("comparison digest = %q, want pinned golden %q", report.Digest, wantDigest)
	}
}

// TestTodo_DATA_011_Security proves tenant isolation and separation of
// duties: a foreign-tenant shadow and a self-approved promotion refuse.
func TestTodo_DATA_011_Security(t *testing.T) {
	active, shadow := fixtureSnapshots()
	shadow.Tenant = "tenant-b"
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant compare error = %v, want ErrTenantMismatch", err)
	}
	active, shadow = fixtureSnapshots()
	comparison, err := Compare(active, shadow, 30*time.Second)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	pointer := Pointer{Active: Version{Digest: "sha256:active", Watermark: 100}}
	selfApproved := Approval{Approver: "requester", Approved: true, Reason: "self", Requester: "requester"}
	if _, err := Promote(pointer, shadow, selfApproved, comparison); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("self-approved promote error = %v, want ErrNotApproved", err)
	}
}

// TestTodo_DATA_011_Recovery proves the rollback pointer restores the
// previous active version after a promotion.
func TestTodo_DATA_011_Recovery(t *testing.T) {
	active, shadow := fixtureSnapshots()
	shadow.Digest = "sha256:active"
	comparison, err := Compare(active, shadow, 30*time.Second)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	pointer := Pointer{Active: Version{Digest: "sha256:previous", Watermark: 90}}
	promoted, err := Promote(pointer, shadow, fixtureApproval(), comparison)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	rolledBack, err := Rollback(promoted)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolledBack.Active.Digest != "sha256:previous" {
		t.Fatalf("rolled back active = %+v, want the retained previous version", rolledBack.Active)
	}
	if _, err := Rollback(Pointer{}); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("empty rollback error = %v, want ErrNoRollback", err)
	}
}

// TestTodo_DATA_011_Fault proves incomplete replay, lag and watermark
// regressions refuse the active-pointer switch.
func TestTodo_DATA_011_Fault(t *testing.T) {
	active, shadow := fixtureSnapshots()
	shadow.ReplayComplete = false
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrIncompleteReplay) {
		t.Fatalf("incomplete replay error = %v, want ErrIncompleteReplay", err)
	}
	active, shadow = fixtureSnapshots()
	shadow.Lag = 5 * time.Minute
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrLagExceeded) {
		t.Fatalf("lag error = %v, want ErrLagExceeded", err)
	}
	active, shadow = fixtureSnapshots()
	shadow.Watermark = 90
	if _, err := Compare(active, shadow, 30*time.Second); !errors.Is(err, ErrComparisonGap) {
		t.Fatalf("watermark regression error = %v, want ErrComparisonGap", err)
	}
}
