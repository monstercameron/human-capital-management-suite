package clockservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// TestTodo_UXBLIND_123_ReasonOfNamesEveryDecisionAndNothingElse proves the
// availability vocabulary is closed: an ineligibility decision keeps its
// reason through wrapping, and an outage, a foreign rejection or a forged state
// never becomes one.
func TestTodo_UXBLIND_123_ReasonOfNamesEveryDecisionAndNothingElse(t *testing.T) {
	for _, reason := range []SelfClockReason{ReasonNoWorkerRecord, ReasonNoAssignment, ReasonNoTimeProfile, ReasonExempt, ReasonCaptureNotPunch} {
		err := NotEligible(reason, "detail must not matter")
		if !errors.Is(err, ErrWorkerNotEligible) {
			t.Fatalf("%s: not an ErrWorkerNotEligible: %v", reason, err)
		}
		if got, ok := ReasonOf(fmt.Errorf("wrapped: %w", err)); !ok || got != reason {
			t.Fatalf("%s: ReasonOf(wrapped) = %q, %v", reason, got, ok)
		}
	}
	for name, err := range map[string]error{
		"nil":               nil,
		"outage":            ErrUnavailable,
		"other sentinel":    reject(ErrInvalidRequest, "x", string(ReasonExempt), "state carried by the wrong sentinel"),
		"plain eligibility": ErrWorkerNotEligible,
		"unknown state":     reject(ErrWorkerNotEligible, "self_clock", "ASK_YOUR_SUPERVISOR", "forged"),
		"not enabled":       reject(ErrWorkerNotEligible, "self_clock", string(ReasonNotEnabled), "the service cannot say it is absent"),
		"empty state":       reject(ErrWorkerNotEligible, "capture", "", "legacy rejection"),
	} {
		if got, ok := ReasonOf(err); ok {
			t.Fatalf("%s: ReasonOf = %q, want no reason", name, got)
		}
	}
	if ReasonNotEnabled.Valid() != true || SelfClockReason("").Valid() || SelfClockReason("other").Valid() {
		t.Fatal("the reason vocabulary is not closed")
	}
}

func selfProfileWith(capture timeprofile.CaptureMode, exemption timeprofile.ExemptionStatus, pay timeprofile.PayBasis) timeprofile.TimeProfile {
	profile := selfPunchProfile()
	profile.Capture, profile.Exemption, profile.PayBasis = capture, exemption, pay
	return profile
}

func TestTodo_UXBLIND_123_SelfClockRefusesNonPunchProfilesWithTheirReason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile timeprofile.TimeProfile
		want    SelfClockReason
	}{
		{"salaried exempt", selfProfileWith(timeprofile.CaptureNone, timeprofile.Exempt, timeprofile.PaySalary), ReasonExempt},
		{"exempt exception capture", selfProfileWith(timeprofile.CaptureException, timeprofile.Exempt, timeprofile.PaySalary), ReasonExempt},
		{"hourly duration sheet", selfProfileWith(timeprofile.CaptureDuration, timeprofile.NonExempt, timeprofile.PayHourly), ReasonCaptureNotPunch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := WorkerSelfService{
				Workers:  &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}},
				Profiles: selfProfileFake{profile: tc.profile}, Store: selfStoreFake{}, Actions: &selfActionFake{},
				Clock: func() time.Time { return time.Unix(10, 0) },
			}
			if _, err := svc.GetSelfClock(context.Background(), selfPrincipal(t)); !errors.Is(err, ErrWorkerNotEligible) {
				t.Fatalf("read error = %v", err)
			} else if got, _ := ReasonOf(err); got != tc.want {
				t.Fatalf("read reason = %q, want %q", got, tc.want)
			}
			_, err := svc.ExecuteSelfClockAction(context.Background(), selfPrincipal(t), SelfClockActionRequest{Action: "IN", ExpectedRevision: 1, IdempotencyKey: "k"})
			if got, ok := ReasonOf(err); !ok || got != tc.want {
				t.Fatalf("action reason = %q, %v, want %q", got, ok, tc.want)
			}
		})
	}
}

func TestTodo_UXBLIND_123_SelfClockNamesWorkerWithoutRecordOrAssignment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		worker SelfWorker
		want   SelfClockReason
	}{
		{"inactive worker", SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: false}, ReasonNoWorkerRecord},
		{"no worker ref", SelfWorker{AssignmentRef: "assignment-1", Active: true}, ReasonNoWorkerRecord},
		{"no assignment", SelfWorker{WorkerRef: "worker-1", Active: true}, ReasonNoAssignment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := WorkerSelfService{
				Workers:  &selfWorkerResolverFake{worker: tc.worker},
				Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{},
				Clock: func() time.Time { return time.Unix(10, 0) },
			}
			_, err := svc.GetSelfClock(context.Background(), selfPrincipal(t))
			if got, ok := ReasonOf(err); !ok || got != tc.want {
				t.Fatalf("reason = %q, %v, want %q (err %v)", got, ok, tc.want, err)
			}
		})
	}
}
