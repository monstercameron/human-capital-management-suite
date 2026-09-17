package clock

import (
	"errors"
	"testing"
	"time"
)

// observationFixture builds a valid observation request around an
// authenticated CLOCK-002 capture. The caller-supplied recorded time is
// deliberately wrong: the server clock must win.
func observationFixture(t *testing.T, now time.Time) ObservationRequest {
	t.Helper()
	capReq := validCaptureRequest(t)
	result, err := AuthenticateCapture(capReq)
	if err != nil {
		t.Fatalf("AuthenticateCapture: %v", err)
	}
	occurred := now.Add(-90 * time.Second)
	req := ObservationRequest{
		Tenant:            capReq.Tenant,
		Capture:           result.Evidence,
		EventType:         EventClockIn,
		OccurredAt:        occurred,
		ClaimedRecordedAt: now.Add(-time.Hour),
		Timezone:          "America/New_York",
		Location:          capReq.Location,
		Signature: DeviceSignature{
			KeyRef:    "key:device-1/v1",
			Algorithm: "ed25519",
		},
		Now: now,
	}
	req.Signature.PayloadDigest = observationPayloadDigest(req)
	return req
}

// TestTodo_CLOCK_003 is the primary CLOCK-003 contract test: a signed
// observation binds event type, device/worker, occurred/recorded time and
// timezone/location evidence, while the caller can never supply trusted
// recorded time.
func TestTodo_CLOCK_003(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	t.Run("valid observation is accepted with server recorded time", func(t *testing.T) {
		req := observationFixture(t, now)
		obs, err := CaptureObservation(req)
		if err != nil {
			t.Fatalf("CaptureObservation: %v", err)
		}
		if !obs.Accepted {
			t.Fatal("observation must be accepted")
		}
		if !obs.OccurredAt.Equal(req.OccurredAt) {
			t.Fatalf("occurred time must be preserved, got %v want %v", obs.OccurredAt, req.OccurredAt)
		}
		if !obs.RecordedAt.Equal(now) {
			t.Fatalf("recorded time must be the server clock, got %v want %v", obs.RecordedAt, now)
		}
		if !obs.RecordedAt.After(obs.OccurredAt) {
			t.Fatal("recorded time must follow occurred time")
		}
		if obs.Digest == "" {
			t.Fatal("observation must carry a digest")
		}
		if obs.EventType != EventClockIn || obs.Timezone != "America/New_York" {
			t.Fatalf("observation must bind event and timezone: %+v", obs)
		}
	})

	t.Run("caller recorded time is never trusted", func(t *testing.T) {
		req := observationFixture(t, now)
		req.ClaimedRecordedAt = now.Add(24 * time.Hour)
		obs, err := CaptureObservation(req)
		if err != nil {
			t.Fatalf("CaptureObservation: %v", err)
		}
		if !obs.RecordedAt.Equal(now) {
			t.Fatalf("caller-supplied recorded time leaked into evidence: %v", obs.RecordedAt)
		}
	})

	t.Run("missing fields are rejected without effect", func(t *testing.T) {
		base := observationFixture(t, now)
		cases := map[string]func(*ObservationRequest){
			"event type":     func(r *ObservationRequest) { r.EventType = "" },
			"occurred time":  func(r *ObservationRequest) { r.OccurredAt = time.Time{} },
			"timezone":       func(r *ObservationRequest) { r.Timezone = "" },
			"device proof":   func(r *ObservationRequest) { r.Capture.Device = DeviceEvidence{} },
			"worker":         func(r *ObservationRequest) { r.Capture.Worker = WorkerEvidence{} },
			"location":       func(r *ObservationRequest) { r.Location = LocationEvidence{} },
			"signature":      func(r *ObservationRequest) { r.Signature = DeviceSignature{} },
			"server clock":   func(r *ObservationRequest) { r.Now = time.Time{} },
			"capture digest": func(r *ObservationRequest) { r.Capture.Digest = "" },
		}
		for name, mutate := range cases {
			req := base
			mutate(&req)
			req.Signature.PayloadDigest = observationPayloadDigest(req)
			_, err := CaptureObservation(req)
			var rej *ObservationRejection
			if !errors.As(err, &rej) {
				t.Fatalf("%s: expected *ObservationRejection, got %v", name, err)
			}
			if !errors.Is(err, ErrObservationRejected) {
				t.Fatalf("%s: expected CLOCK_003_REJECTED, got %v", name, err)
			}
			if rej.Field == "" || rej.State == "" || rej.Version == "" {
				t.Fatalf("%s: rejection must name field/state/version: %+v", name, rej)
			}
		}
	})

	t.Run("tampered occurred time breaks the device signature", func(t *testing.T) {
		req := observationFixture(t, now)
		req.OccurredAt = req.OccurredAt.Add(time.Minute)
		_, err := CaptureObservation(req)
		if !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("expected CLOCK_003_REJECTED for signature mismatch, got %v", err)
		}
	})

	t.Run("future occurred time beyond skew is rejected", func(t *testing.T) {
		req := observationFixture(t, now)
		req.OccurredAt = now.Add(time.Hour)
		req.Signature.PayloadDigest = observationPayloadDigest(req)
		_, err := CaptureObservation(req)
		if !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("expected CLOCK_003_REJECTED for future occurred time, got %v", err)
		}
	})

	t.Run("cross-tenant capture is rejected", func(t *testing.T) {
		req := observationFixture(t, now)
		req.Capture.Principal.Tenant = "other"
		req.Signature.PayloadDigest = observationPayloadDigest(req)
		_, err := CaptureObservation(req)
		if !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("expected CLOCK_003_REJECTED for tenant mismatch, got %v", err)
		}
	})
}

// TestTodo_CLOCK_003_Property holds the CLOCK-003 invariants: recorded time
// always equals the server clock, the digest is stable per input and unique
// per occurred instant, and the observation never moves backward.
func TestTodo_CLOCK_003_Property(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)
	base := observationFixture(t, now)

	t.Run("digest is stable and input sensitive", func(t *testing.T) {
		first, err := CaptureObservation(base)
		if err != nil {
			t.Fatal(err)
		}
		second, err := CaptureObservation(base)
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest != second.Digest {
			t.Fatal("same input must produce a stable digest")
		}
		moved := base
		moved.OccurredAt = moved.OccurredAt.Add(time.Second)
		moved.Signature.PayloadDigest = observationPayloadDigest(moved)
		third, err := CaptureObservation(moved)
		if err != nil {
			t.Fatal(err)
		}
		if third.Digest == first.Digest {
			t.Fatal("different occurred instants must produce different digests")
		}
	})

	t.Run("recorded time tracks the server clock across instants", func(t *testing.T) {
		for _, skew := range []time.Duration{0, time.Second, time.Hour, 30 * 24 * time.Hour} {
			server := now.Add(skew)
			req := observationFixture(t, now)
			req.OccurredAt = server.Add(-time.Minute)
			req.Signature.PayloadDigest = observationPayloadDigest(req)
			req.Now = server
			obs, err := CaptureObservation(req)
			if err != nil {
				t.Fatalf("skew %v: %v", skew, err)
			}
			if !obs.RecordedAt.Equal(server) {
				t.Fatalf("skew %v: recorded %v is not the server clock %v", skew, obs.RecordedAt, server)
			}
		}
	})
}

// TestTodo_CLOCK_003_Mutation kills the mutants that matter: removing the
// signature check, trusting caller recorded time, or dropping the tenant
// fence must each fail this test.
func TestTodo_CLOCK_003_Mutation(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	t.Run("signature verification cannot be skipped", func(t *testing.T) {
		req := observationFixture(t, now)
		req.Signature.PayloadDigest = "sha256:forged"
		if _, err := CaptureObservation(req); !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("forged signature payload must be rejected, got %v", err)
		}
	})

	t.Run("tenant fence cannot be removed", func(t *testing.T) {
		req := observationFixture(t, now)
		req.Tenant = "attacker"
		if _, err := CaptureObservation(req); !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("tenant mismatch must be rejected, got %v", err)
		}
	})

	t.Run("recorded time cannot fall back to caller input", func(t *testing.T) {
		req := observationFixture(t, now)
		req.ClaimedRecordedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
		obs, err := CaptureObservation(req)
		if err != nil {
			t.Fatal(err)
		}
		if obs.RecordedAt.Year() == 1999 {
			t.Fatal("caller recorded time became evidence")
		}
	})
}
