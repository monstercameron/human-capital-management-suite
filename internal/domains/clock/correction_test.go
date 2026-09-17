// CLOCK-006 RED: a correction must bind the original observation, a reason,
// attestation/approval authority and recalculation impacts; the original
// signed observation stays immutable.
package clock

import (
	"errors"
	"testing"
	"time"
)

func correctionFixture(t *testing.T, now time.Time) (TimeObservation, CorrectionRequest) {
	t.Helper()
	req := observationFixture(t, now)
	obs, err := CaptureObservation(req)
	if err != nil {
		t.Fatalf("CaptureObservation: %v", err)
	}
	corrected := obs.OccurredAt.Add(-5 * time.Minute)
	return obs, CorrectionRequest{
		Original:            obs,
		Reason:              "device clock drift confirmed against server log",
		ReasonRef:           "reasons/drift/v1",
		AttestationRef:      "attestation:supervisor-2/v3",
		ApprovalRef:         "approval:operations-lead/v1",
		CorrectedOccurredAt: corrected,
		Impacts:             []string{"attendance", "payroll"},
		Now:                 now,
	}
}

// TestTodo_CLOCK_006 is the PRIMARY contract: a correction binds the
// original observation, reason, attestation/approval and impacts, and the
// original signed observation remains immutable.
func TestTodo_CLOCK_006(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	t.Run("valid correction binds original and impacts", func(t *testing.T) {
		obs, req := correctionFixture(t, now)
		got, err := CorrectObservation(req)
		if err != nil {
			t.Fatalf("CorrectObservation: %v", err)
		}
		if got.OriginalDigest != obs.Digest || !got.OriginalOccurredAt.Equal(obs.OccurredAt) {
			t.Fatalf("correction does not bind the original: %+v", got)
		}
		if !got.CorrectedOccurredAt.Equal(req.CorrectedOccurredAt) {
			t.Fatalf("corrected occurred=%v want %v", got.CorrectedOccurredAt, req.CorrectedOccurredAt)
		}
		if got.Reason != req.Reason || got.AttestationRef != req.AttestationRef || got.ApprovalRef != req.ApprovalRef {
			t.Fatalf("correction does not bind authority: %+v", got)
		}
		if len(got.Impacts) != 2 || got.Impacts[0] != "attendance" || got.Impacts[1] != "payroll" {
			t.Fatalf("correction does not bind recalculation impacts: %+v", got)
		}
		if !got.CorrectedAt.Equal(now.UTC()) || got.Digest == "" {
			t.Fatalf("correction lacks server time or digest: %+v", got)
		}
	})

	t.Run("original observation is immutable", func(t *testing.T) {
		obs, req := correctionFixture(t, now)
		before := obs
		if _, err := CorrectObservation(req); err != nil {
			t.Fatal(err)
		}
		if obs != before {
			t.Fatalf("original mutated:\nbefore=%+v\nafter=%+v", before, obs)
		}
	})

	t.Run("missing authority is rejected without effect", func(t *testing.T) {
		_, base := correctionFixture(t, now)
		cases := map[string]func(*CorrectionRequest){
			"reason":      func(r *CorrectionRequest) { r.Reason = "" },
			"reason ref":  func(r *CorrectionRequest) { r.ReasonRef = "" },
			"attestation": func(r *CorrectionRequest) { r.AttestationRef = "" },
			"approval":    func(r *CorrectionRequest) { r.ApprovalRef = "" },
			"impacts":     func(r *CorrectionRequest) { r.Impacts = nil },
			"server clock": func(r *CorrectionRequest) {
				r.Now = time.Time{}
			},
		}
		for name, mutate := range cases {
			req := base
			mutate(&req)
			_, err := CorrectObservation(req)
			var rej *CorrectionRejection
			if !errors.As(err, &rej) || !errors.Is(err, ErrCorrectionRejected) {
				t.Fatalf("%s: err=%v, want CLOCK_006_REJECTED", name, err)
			}
			if rej.Field == "" || rej.State == "" || rej.Version == "" {
				t.Fatalf("%s: rejection must name field/state/version: %+v", name, rej)
			}
		}
	})

	t.Run("unchanged occurred time is rejected", func(t *testing.T) {
		obs, req := correctionFixture(t, now)
		req.CorrectedOccurredAt = obs.OccurredAt
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("no-op correction err=%v, want CLOCK_006_REJECTED", err)
		}
	})

	t.Run("future corrected time beyond skew is rejected", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		req.CorrectedOccurredAt = now.Add(time.Hour)
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("future correction err=%v, want CLOCK_006_REJECTED", err)
		}
	})

	t.Run("unaccepted original is rejected", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		req.Original = TimeObservation{}
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("empty original err=%v, want CLOCK_006_REJECTED", err)
		}
	})
}

// TestTodo_CLOCK_006_Property holds the correction invariants: the digest is
// stable per input and sensitive to the corrected instant, and repeated
// corrections never touch the original.
func TestTodo_CLOCK_006_Property(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	t.Run("digest is stable and input sensitive", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		first, err := CorrectObservation(req)
		if err != nil {
			t.Fatal(err)
		}
		second, err := CorrectObservation(req)
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest != second.Digest {
			t.Fatal("same correction must produce a stable digest")
		}
		moved := req
		moved.CorrectedOccurredAt = moved.CorrectedOccurredAt.Add(time.Second)
		third, err := CorrectObservation(moved)
		if err != nil {
			t.Fatal(err)
		}
		if third.Digest == first.Digest {
			t.Fatal("different corrected instants must produce different digests")
		}
	})

	t.Run("original survives repeated corrections", func(t *testing.T) {
		obs, req := correctionFixture(t, now)
		for i := 0; i < 3; i++ {
			moved := req
			moved.CorrectedOccurredAt = req.CorrectedOccurredAt.Add(time.Duration(i) * time.Minute)
			if _, err := CorrectObservation(moved); err != nil {
				t.Fatal(err)
			}
			if obs.Digest == "" || !obs.Accepted {
				t.Fatal("original acceptance evidence must survive corrections")
			}
		}
	})
}

// TestTodo_CLOCK_006_Mutation kills the mutants that matter: dropping the
// attestation gate, the approval gate, the reason gate or the no-op guard
// must each fail this test.
func TestTodo_CLOCK_006_Mutation(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	t.Run("attestation gate cannot be removed", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		req.AttestationRef = ""
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("unattested correction err=%v", err)
		}
	})

	t.Run("approval gate cannot be removed", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		req.ApprovalRef = ""
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("unapproved correction err=%v", err)
		}
	})

	t.Run("reason gate cannot be removed", func(t *testing.T) {
		_, req := correctionFixture(t, now)
		req.Reason = ""
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("unreasoned correction err=%v", err)
		}
	})

	t.Run("no-op guard cannot be removed", func(t *testing.T) {
		obs, req := correctionFixture(t, now)
		req.CorrectedOccurredAt = obs.OccurredAt
		if _, err := CorrectObservation(req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("no-op correction err=%v", err)
		}
	})
}
