package geofence

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

func testBoundary(t *testing.T, now time.Time) Boundary {
	t.Helper()
	effective, err := values.NewOpenInstantInterval(values.NewInstant(now.Add(-24 * time.Hour)))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return Boundary{
		SiteRef: "site-riverside", Revision: 1, Center: Coordinate{Latitude: 37.7749, Longitude: -122.4194},
		RadiusMeters: 150, AccuracyThresholdMeters: 50, MaxEvidenceAge: 5 * time.Minute,
		ExitGrace: 10 * time.Minute, HysteresisMeters: 15, Effective: effective,
		RetentionPeriod: 30 * 24 * time.Hour, AuthorizedBy: "site-admin-1", Version: "v1",
	}
}

func testEvidence(now time.Time, lat, lon, accuracy float64) ServerEvidence {
	return ServerEvidence{
		Observation: DeviceObservation{
			ObservedAt: now, Position: Coordinate{Latitude: lat, Longitude: lon}, AccuracyMeters: accuracy,
			Source: SourceDeviceGPS, PermissionGranted: true, ConsentRef: "consent-1",
		},
		ReceivedAt: now, EvidenceRef: "evidence-1",
	}
}

// TestTodo_FTIME_010 is the PRIMARY test: a fix inside the boundary radius
// with good accuracy evaluates INSIDE, one well outside evaluates OUTSIDE,
// and a fix just past the radius but within the hysteresis band still
// evaluates INSIDE to avoid flapping at the edge.
func TestTodo_FTIME_010(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	boundary := testBoundary(t, now)

	inside, err := Evaluate(boundary, testEvidence(now, 37.7749, -122.4194, 20), now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if inside.Status != StatusInside {
		t.Fatalf("expected INSIDE at the center point, got %+v", inside)
	}

	outside, err := Evaluate(boundary, testEvidence(now, 37.8, -122.5, 20), now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if outside.Status != StatusOutside {
		t.Fatalf("expected OUTSIDE far from the boundary, got %+v", outside)
	}
	if !outside.HasDistance || outside.DistanceMeters <= boundary.RadiusMeters {
		t.Fatalf("expected a computed distance beyond the radius, got %+v", outside)
	}

	// Roughly 160m north of the center: past the 150m radius but within
	// the 15m hysteresis band.
	nearEdgeLat := boundary.Center.Latitude + (160.0 / 111320.0)
	nearEdge, err := Evaluate(boundary, testEvidence(now, nearEdgeLat, boundary.Center.Longitude, 20), now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if nearEdge.Status != StatusInside {
		t.Fatalf("expected hysteresis band to still evaluate INSIDE, got %+v", nearEdge)
	}

	proof, err := Retain(boundary, outside, "evidence-1")
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if proof.DueForPurge(now.Add(29 * 24 * time.Hour)) {
		t.Fatalf("expected retained proof not yet due for purge")
	}
	if !proof.DueForPurge(now.Add(31 * 24 * time.Hour)) {
		t.Fatalf("expected retained proof due for purge past the retention period")
	}
}

// TestTodo_FTIME_010_Golden pins a boundary-not-effective evaluation's
// rendered explanation so a change to the effective-interval gate is
// deliberate.
func TestTodo_FTIME_010_Golden(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	boundary := testBoundary(t, now)
	past := now.Add(-48 * time.Hour)
	evaluation, err := Evaluate(boundary, testEvidence(past, boundary.Center.Latitude, boundary.Center.Longitude, 20), past)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	exp, err := Explain(evaluation)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	got := fmt.Sprintf("site=%s revision=%d status=%s reason=%s digest=%s\n", exp.SiteRef, exp.Revision, exp.Status, exp.Reason, exp.Digest)
	path := filepath.Join("testdata", "golden", "boundary_not_effective.txt")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestTodo_FTIME_010_Security proves that missing location permission,
// stale evidence and weak accuracy all return UNKNOWN even when the
// underlying position is squarely inside the boundary — never a confirmed
// INSIDE or OUTSIDE that could wrongly authorize or close a shift.
func TestTodo_FTIME_010_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	boundary := testBoundary(t, now)

	noPermission := testEvidence(now, boundary.Center.Latitude, boundary.Center.Longitude, 20)
	noPermission.Observation.PermissionGranted = false
	eval, err := Evaluate(boundary, noPermission, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Status != StatusUnknown || eval.Reason != ReasonMissingPermission {
		t.Fatalf("expected UNKNOWN/MISSING_PERMISSION, got %+v", eval)
	}

	stale := testEvidence(now.Add(-10*time.Minute), boundary.Center.Latitude, boundary.Center.Longitude, 20)
	eval, err = Evaluate(boundary, stale, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Status != StatusUnknown || eval.Reason != ReasonStaleEvidence {
		t.Fatalf("expected UNKNOWN/STALE_EVIDENCE, got %+v", eval)
	}

	weakAccuracy := testEvidence(now, boundary.Center.Latitude, boundary.Center.Longitude, 500)
	eval, err = Evaluate(boundary, weakAccuracy, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Status != StatusUnknown || eval.Reason != ReasonWeakAccuracy {
		t.Fatalf("expected UNKNOWN/WEAK_ACCURACY, got %+v", eval)
	}

	invalidSource := testEvidence(now, boundary.Center.Latitude, boundary.Center.Longitude, 20)
	invalidSource.Observation.Source = Source("SPOOFED")
	eval, err = Evaluate(boundary, invalidSource, now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Status != StatusUnknown || eval.Reason != ReasonInvalidSource {
		t.Fatalf("expected UNKNOWN/INVALID_SOURCE, got %+v", eval)
	}

	if _, err := Evaluate(Boundary{}, testEvidence(now, 0, 0, 20), now); !errors.Is(err, ErrBoundaryRejected) {
		t.Fatalf("expected an invalid boundary to be rejected, got %v", err)
	}
}

// TestTodo_FTIME_010_Property proves, over many random points, that the
// distance computed by Evaluate agrees with an independent haversine
// implementation and that the INSIDE/OUTSIDE verdict is always consistent
// with radius plus hysteresis.
func TestTodo_FTIME_010_Property(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	boundary := testBoundary(t, now)
	rng := rand.New(rand.NewPCG(3, 5))
	for i := 0; i < 300; i++ {
		dLat := (rng.Float64() - 0.5) * 0.01
		dLon := (rng.Float64() - 0.5) * 0.01
		lat := boundary.Center.Latitude + dLat
		lon := boundary.Center.Longitude + dLon
		eval, err := Evaluate(boundary, testEvidence(now, lat, lon, 20), now)
		if err != nil {
			t.Fatalf("case %d: Evaluate: %v", i, err)
		}
		if !eval.HasDistance {
			t.Fatalf("case %d: expected a computed distance", i)
		}
		independent := referenceHaversine(boundary.Center, Coordinate{Latitude: lat, Longitude: lon})
		if math.Abs(independent-eval.DistanceMeters) > 0.5 {
			t.Fatalf("case %d: distance mismatch: reference=%.3f evaluate=%.3f", i, independent, eval.DistanceMeters)
		}
		wantInside := eval.DistanceMeters <= boundary.RadiusMeters+boundary.HysteresisMeters
		if wantInside != (eval.Status == StatusInside) {
			t.Fatalf("case %d: distance %.3f status %s disagrees with radius+hysteresis %.3f", i, eval.DistanceMeters, eval.Status, boundary.RadiusMeters+boundary.HysteresisMeters)
		}
	}
}

// referenceHaversine is an independently written haversine implementation
// used only to cross-check Evaluate's distance computation.
func referenceHaversine(a, b Coordinate) float64 {
	const r = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	phi1, phi2 := toRad(a.Latitude), toRad(b.Latitude)
	dPhi := toRad(b.Latitude - a.Latitude)
	dLambda := toRad(b.Longitude - a.Longitude)
	x := math.Sin(dPhi/2)*math.Sin(dPhi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return 2 * r * math.Atan2(math.Sqrt(x), math.Sqrt(1-x))
}
