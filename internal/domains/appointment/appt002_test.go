package appointment

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func appt002Signal(role, suffix string) CandidateSignal {
	return CandidateSignal{
		Participant:    typedAppointmentRef(values.Kind("worker"), suffix),
		Role:           role,
		Busy:           PresenceFree,
		BusyRef:        "freebusy@v3",
		Leave:          LeaveClear,
		LeaveRef:       "leave@v1",
		Qualification:  Qualified,
		QualRef:        "qual@v2",
		Timezone:       "America/New_York",
		PrivacyConsent: true,
	}
}

// TestTodo_APPT_002 is the RED contract: busy, leave, timezone,
// qualification and privacy unknowns stay explicit, and the resolution never
// carries another person's calendar detail.
func TestTodo_APPT_002(t *testing.T) {
	req := validRequirement()
	signals := []CandidateSignal{appt002Signal("candidate", "011"), appt002Signal("interviewer", "012")}
	res, err := ResolveAvailability(req, signals)
	if err != nil {
		t.Fatalf("ResolveAvailability: %v", err)
	}
	if len(res.Candidates) != 2 || res.Digest == "" {
		t.Fatalf("resolution = %+v", res)
	}
	for _, c := range res.Candidates {
		if c.Verdict != VerdictAvailable {
			t.Fatalf("candidate %s = %s, want AVAILABLE", c.Role, c.Verdict)
		}
	}

	cases := []struct {
		name    string
		mutate  func(*CandidateSignal)
		verdict AvailabilityVerdict
		reason  string
	}{
		{"busy", func(s *CandidateSignal) { s.Busy = PresenceBusy }, VerdictUnavailable, "busy"},
		{"leave", func(s *CandidateSignal) { s.Leave = LeaveOnLeave }, VerdictUnavailable, "on-leave"},
		{"unqualified", func(s *CandidateSignal) { s.Qualification = Unqualified }, VerdictUnavailable, "unqualified"},
		{"busy-unknown", func(s *CandidateSignal) { s.Busy = PresenceUnknown }, VerdictUnknown, "busy-unknown"},
		{"leave-unknown", func(s *CandidateSignal) { s.Leave = LeaveUnknown }, VerdictUnknown, "leave-unknown"},
		{"qualification-unknown", func(s *CandidateSignal) { s.Qualification = QualificationUnknown }, VerdictUnknown, "qualification-unknown"},
		{"timezone-unknown", func(s *CandidateSignal) { s.Timezone = "" }, VerdictUnknown, "timezone-unknown"},
		{"privacy-unknown", func(s *CandidateSignal) { s.PrivacyConsent = false }, VerdictUnknown, "privacy-unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := appt002Signal("candidate", "011")
			tc.mutate(&sig)
			got, err := ResolveAvailability(req, []CandidateSignal{sig, appt002Signal("interviewer", "012")})
			if err != nil {
				t.Fatalf("ResolveAvailability: %v", err)
			}
			if got.Candidates[0].Verdict != tc.verdict {
				t.Fatalf("verdict = %s, want %s", got.Candidates[0].Verdict, tc.verdict)
			}
			found := false
			for _, r := range got.Candidates[0].Reasons {
				if r == tc.reason {
					found = true
				}
				if strings.ContainsAny(r, "0123456789:") && strings.Contains(r, "2026") {
					t.Fatalf("reason %q leaks calendar detail", r)
				}
			}
			if !found {
				t.Fatalf("reasons = %v, want %q", got.Candidates[0].Reasons, tc.reason)
			}
		})
	}
}

// TestTodo_APPT_002_Security proves no calendar detail of another person is
// disclosed: verdicts carry only reason codes, undisclosed free/busy stays
// UNKNOWN, and cross-tenant signals are refused.
func TestTodo_APPT_002_Security(t *testing.T) {
	req := validRequirement()
	res, err := ResolveAvailability(req, []CandidateSignal{appt002Signal("candidate", "011"), appt002Signal("interviewer", "012")})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Candidates {
		if c.Participant != (values.EntityRef{}) {
			t.Fatalf("resolution discloses participant identity: %+v", c.Participant)
		}
		for _, r := range c.Reasons {
			if strings.Contains(r, "2026") || strings.Contains(r, "America/") {
				t.Fatalf("reason %q discloses calendar detail", r)
			}
		}
	}
	foreign := appt002Signal("candidate", "011")
	foreign.Participant.Tenant = "tenant-b"
	if _, err := ResolveAvailability(req, []CandidateSignal{foreign}); !errors.As(err, new(*Rejection)) {
		t.Fatalf("cross-tenant err=%v, want *Rejection", err)
	}
}

// TestTodo_APPT_002_Mutation proves forged or inconsistent inputs cannot slip
// through as a clean resolution.
func TestTodo_APPT_002_Mutation(t *testing.T) {
	req := validRequirement()
	good := []CandidateSignal{appt002Signal("candidate", "011"), appt002Signal("interviewer", "012")}
	if _, err := ResolveAvailability(req, good); err != nil {
		t.Fatal(err)
	}
	dup := append(append([]CandidateSignal(nil), good...), appt002Signal("candidate", "011"))
	if _, err := ResolveAvailability(req, dup); !errors.As(err, new(*Rejection)) {
		t.Fatalf("duplicate err=%v, want *Rejection", err)
	}
	badReq := validRequirement()
	badReq.Duration = 0
	if _, err := ResolveAvailability(badReq, good); !errors.As(err, new(*Rejection)) {
		t.Fatalf("invalid requirement err=%v, want *Rejection", err)
	}
	uncovered := []CandidateSignal{appt002Signal("candidate", "011")}
	var rej *Rejection
	if _, err := ResolveAvailability(req, uncovered); !errors.As(err, &rej) || rej.Code != "APPT_002_REJECTED" {
		t.Fatalf("uncovered err=%v, want APPT_002_REJECTED", err)
	}
	none := []CandidateSignal{}
	quiet := appt002Signal("candidate", "011")
	quiet.Busy = PresenceBusy
	free, err := ResolveAvailability(req, good)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := ResolveAvailability(req, []CandidateSignal{quiet, appt002Signal("interviewer", "012")})
	if err != nil {
		t.Fatal(err)
	}
	_ = none
	if free.Digest == busy.Digest {
		t.Fatal("busy and free resolutions share a digest")
	}
}
