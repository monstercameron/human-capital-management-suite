package application

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/appointment"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedAppointmentRequirement() appointment.Requirement {
	start := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	return appointment.Requirement{
		ID: "interview", Version: "v1", Purpose: "candidate interview",
		Participants: []appointment.ParticipantRole{{Role: "candidate", Required: true, Min: 1, Max: 1}},
		Duration:     30 * time.Minute,
		Window:       appointment.TimeWindow{Start: start, End: start.Add(time.Hour), TimeZone: "America/New_York"},
		Location:     appointment.Location{Mode: "VIRTUAL", Channel: "video"}, Qualification: "engineering",
		PrivacyClass:     "candidate-confidential",
		Resources:        []appointment.ResourceType{{ID: "room-video", Version: "v1", Name: "video room", Kind: appointment.ResourceCapability, Qualification: "video", PrivacyClass: "candidate-confidential", Capacity: 1}},
		Cancellation:     appointment.CancellationPolicy{Notice: time.Hour, FeePolicy: "none", NoShowPolicy: "follow-up"},
		ExternalCalendar: appointment.ExternalCalendarPolicy{Mode: "DISABLED", DetailDisclosure: "free_busy"},
	}
}

func servedAppointmentParticipant() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
}

// TestTodo_APPT_001 proves the requirement and resource contracts are served
// through the application package imported by cmd/hcmnext.
func TestTodo_APPT_001(t *testing.T) {
	surface := NewServedAppointmentSurface()
	requirement := servedAppointmentRequirement()
	if _, err := surface.Publish(requirement); err != nil {
		t.Fatalf("served appointment requirement rejected: %v", err)
	}
	if err := surface.ValidateRequirement(requirement); err != nil {
		t.Fatalf("served appointment validator rejected: %v", err)
	}
	resource, err := surface.NewResourceType(requirement.Resources[0])
	if err != nil || resource.CanonicalDigest == "" {
		t.Fatalf("served resource = %+v, err = %v", resource, err)
	}
	broken := requirement
	broken.Duration = 0
	if _, err := surface.Publish(broken); err == nil {
		t.Fatal("served appointment accepted a requirement without duration")
	}
}

// TestTodo_APPT_002 proves unknown availability remains explicit and does not
// disclose participant identity or calendar detail.
func TestTodo_APPT_002(t *testing.T) {
	surface := NewServedAppointmentSurface()
	result, err := surface.ResolveAvailability(servedAppointmentRequirement(), []appointment.CandidateSignal{{
		Participant: servedAppointmentParticipant(), Role: "candidate", Busy: appointment.PresenceUnknown,
		Leave: appointment.LeaveClear, Qualification: appointment.Qualified, Timezone: "America/New_York", PrivacyConsent: true,
	}})
	if err != nil {
		t.Fatalf("served availability resolution: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Verdict != appointment.VerdictUnknown || result.Candidates[0].Reasons[0] != "busy-unknown" {
		t.Fatalf("served availability result = %+v", result)
	}
	if result.Candidates[0].Participant != (values.EntityRef{}) {
		t.Fatalf("served availability disclosed participant identity: %+v", result.Candidates[0].Participant)
	}
}

// TestTodo_APPT_003 proves an invalid reservation is rejected through the
// served ledger without leaving an active hold.
func TestTodo_APPT_003(t *testing.T) {
	surface := NewServedAppointmentSurface()
	ledger := surface.NewLedger()
	_, err := surface.Reserve(ledger, appointment.Requirement{}, appointment.ReservationRequest{}, time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC))
	if !errors.Is(err, appointment.ErrReservationRejected) {
		t.Fatalf("served invalid reservation error = %v, want ErrReservationRejected", err)
	}
	if got := surface.ActiveReservations(ledger); got != 0 {
		t.Fatalf("served invalid reservation left %d active holds", got)
	}
}

// TestTodo_APPT_004 proves a rejected lifecycle transition emits no governed
// communication through the served appointment surface.
func TestTodo_APPT_004(t *testing.T) {
	surface := NewServedAppointmentSurface()
	ledger := surface.NewLedger()
	_, err := surface.Confirm(ledger, "missing", "", "confirm-1", time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC))
	if !errors.Is(err, appointment.ErrTransitionRejected) {
		t.Fatalf("served invalid transition error = %v, want ErrTransitionRejected", err)
	}
	if got := surface.Outbox(ledger); len(got) != 0 {
		t.Fatalf("served rejected transition emitted %d notifications", len(got))
	}
}
