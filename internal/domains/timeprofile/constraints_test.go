package timeprofile

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func contractorProfile(t *testing.T) TimeProfile {
	t.Helper()
	p := baseProfile(t)
	p.Category, p.PayBasis, p.Exemption, p.Destination = CategoryContractor, PayContract, NotApplicable, DestinationInvoice
	p.OvertimeMethod = OvertimeNone
	return p
}

func minorProfile(t *testing.T) TimeProfile {
	t.Helper()
	p := baseProfile(t)
	p.MinorAgeBand, p.MinorPermitVerified = MinorAgeBand16To17, true
	return p
}

func euProfile(t *testing.T) TimeProfile {
	t.Helper()
	p := baseProfile(t)
	p.Capture, p.Exemption, p.PayBasis, p.OvertimeMethod = CaptureDuration, Exempt, PaySalary, OvertimeNone
	p.RestPeriodDutyRequired, p.DailyRecordingDutyRequired = true, true
	return p
}

// TestTodo_WTIME_006 proves the RED scenarios: a tenant overlay adding a
// geofence or photo step to the contractor template, a scheduling lockout
// reaching contractor entries, and an overlay omitting the minor-hours
// decision.
func TestTodo_WTIME_006(t *testing.T) {
	t.Run("a contractor overlay adding geofence and photo steps fails publish", func(t *testing.T) {
		nodes := []PlanNode{
			{NodeID: "invoice-review", CapabilityClasses: []string{"APPROVAL"}},
			{NodeID: "overlay-geofence", CapabilityClasses: []string{string(ControlGeofence)}},
			{NodeID: "overlay-photo", CapabilityClasses: []string{string(ControlPunchPhoto)}},
		}
		err := CheckPlan(contractorProfile(t), nodes)
		if !errors.Is(err, ErrPlanConstraintViolated) {
			t.Fatalf("expected ErrPlanConstraintViolated, got %v", err)
		}
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) || violation.NodeID != "overlay-geofence" {
			t.Fatalf("expected the violation to name overlay-geofence, got %+v", violation)
		}
	})

	t.Run("a scheduling lockout on a contractor plan fails publish", func(t *testing.T) {
		nodes := []PlanNode{{NodeID: "lockout", CapabilityClasses: []string{string(ControlScheduleLockout)}}}
		err := CheckPlan(contractorProfile(t), nodes)
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) || violation.Class != string(ControlScheduleLockout) {
			t.Fatalf("expected a SCHEDULE_LOCKOUT violation, got %v", err)
		}
	})

	t.Run("an overlay that omits the minor-hours decision fails publish", func(t *testing.T) {
		nodes := []PlanNode{{NodeID: "punch-in", CapabilityClasses: []string{string(ControlMandatoryClockIn)}}}
		err := CheckPlan(minorProfile(t), nodes)
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) || violation.Class != string(DecisionMinorHours) || violation.NodeID != "" {
			t.Fatalf("expected a missing MINOR_HOURS_DECISION violation naming no node, got %+v", violation)
		}
	})

	t.Run("a compliant minor plan publishes", func(t *testing.T) {
		nodes := []PlanNode{
			{NodeID: "punch-in", CapabilityClasses: []string{string(ControlMandatoryClockIn)}},
			{NodeID: "minor-hours-check", CapabilityClasses: []string{string(DecisionMinorHours)}},
		}
		if err := CheckPlan(minorProfile(t), nodes); err != nil {
			t.Fatalf("expected the compliant plan to publish, got %v", err)
		}
	})

	t.Run("an EU plan without daily recording or the rest decision fails publish", func(t *testing.T) {
		nodes := []PlanNode{{NodeID: "duration-entry", CapabilityClasses: nil}}
		err := CheckPlan(euProfile(t), nodes)
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) {
			t.Fatalf("expected a violation, got %v", err)
		}
		if violation.Class != string(DecisionDailyRecording) && violation.Class != string(DecisionRestPeriod) {
			t.Fatalf("expected a daily-recording or rest-period violation, got %+v", violation)
		}
	})

	t.Run("a compliant EU plan carrying both decisions publishes", func(t *testing.T) {
		nodes := []PlanNode{
			{NodeID: "daily-record", CapabilityClasses: []string{string(DecisionDailyRecording)}},
			{NodeID: "rest-check", CapabilityClasses: []string{string(DecisionRestPeriod)}},
		}
		if err := CheckPlan(euProfile(t), nodes); err != nil {
			t.Fatalf("expected the compliant EU plan to publish, got %v", err)
		}
	})

	t.Run("an ordinary employee plan carrying every control class publishes", func(t *testing.T) {
		nodes := []PlanNode{{NodeID: "schedule", CapabilityClasses: []string{
			string(ControlScheduleLockout), string(ControlMandatoryClockIn), string(ControlGeofence),
			string(ControlPunchPhoto), string(ControlBreakAttestation), string(ControlShiftAssignment),
		}}}
		if err := CheckPlan(baseProfile(t), nodes); err != nil {
			t.Fatalf("an employee plan should not forbid any control class, got %v", err)
		}
	})
}

// TestTodo_WTIME_006_Golden pins ConstraintsFor's derived policy per category
// so a change to the required/forbidden sets shows as a diff.
func TestTodo_WTIME_006_Golden(t *testing.T) {
	cases := []struct {
		name    string
		profile TimeProfile
	}{
		{"employee", baseProfile(t)},
		{"contractor", contractorProfile(t)},
		{"minor_employee", minorProfile(t)},
		{"eu_duty_employee", euProfile(t)},
	}
	var b strings.Builder
	for _, tc := range cases {
		c := ConstraintsFor(tc.profile)
		fmt.Fprintf(&b, "%s:\n", tc.name)
		fmt.Fprintf(&b, "  required:  %s\n", strings.Join(c.RequiredClasses, ","))
		fmt.Fprintf(&b, "  forbidden: %s\n", strings.Join(c.ForbiddenClasses, ","))
	}
	assertGolden(t, "plan-constraints.txt", b.String())
}

// TestTodo_WTIME_006_Property checks, over every declared category and every
// single ControlClass, that CheckPlan's forbidden-class verdict matches an
// independent reference policy (contractor forbids every control class,
// nobody else forbids any).
func TestTodo_WTIME_006_Property(t *testing.T) {
	categories := []WorkerCategory{CategoryEmployee, CategoryPlatform, CategoryContractor, CategoryAgencyTemp}
	for _, category := range categories {
		for _, class := range ControlClasses() {
			p := baseProfile(t)
			p.Category = category
			switch category {
			case CategoryContractor:
				p.PayBasis, p.Exemption, p.Destination = PayContract, NotApplicable, DestinationInvoice
				p.OvertimeMethod = OvertimeNone
			case CategoryAgencyTemp:
				p.Exemption, p.Destination = NotApplicable, DestinationAgency
			}
			nodes := []PlanNode{{NodeID: "n1", CapabilityClasses: []string{string(class)}}}
			err := CheckPlan(p, nodes)
			wantForbidden := category == CategoryContractor
			if wantForbidden && !errors.Is(err, ErrPlanConstraintViolated) {
				t.Fatalf("category=%s class=%s: expected a violation, got %v", category, class, err)
			}
			if !wantForbidden && err != nil {
				t.Fatalf("category=%s class=%s: expected no violation, got %v", category, class, err)
			}
		}
	}
}

// TestTodo_WTIME_006_Security proves CheckPlan's matching is exact-string:
// a near-miss token (different case, or padded with whitespace) never
// stands in for a real forbidden or required class on either side of the
// check, and an empty capability-class token can never satisfy a required
// class by coincidence.
func TestTodo_WTIME_006_Security(t *testing.T) {
	t.Run("case-folded class tokens do not trigger a forbidden-class match", func(t *testing.T) {
		lowered := strings.ToLower(string(ControlGeofence))
		nodes := []PlanNode{{NodeID: "n1", CapabilityClasses: []string{lowered}}}
		if err := CheckPlan(contractorProfile(t), nodes); err != nil {
			t.Fatalf("a non-canonical token must not be treated as the forbidden class it resembles: %v", err)
		}
	})

	t.Run("an empty capability-class entry never satisfies a required class", func(t *testing.T) {
		nodes := []PlanNode{{NodeID: "n1", CapabilityClasses: []string{"", " ", ""}}}
		err := CheckPlan(minorProfile(t), nodes)
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) || violation.Class != string(DecisionMinorHours) {
			t.Fatalf("expected the empty tokens to leave MINOR_HOURS_DECISION missing, got %+v", violation)
		}
	})

	t.Run("a forbidden class buried among many compliant nodes is still caught", func(t *testing.T) {
		nodes := make([]PlanNode, 0, 20)
		for i := 0; i < 19; i++ {
			nodes = append(nodes, PlanNode{NodeID: fmt.Sprintf("ok-%d", i), CapabilityClasses: []string{"APPROVAL"}})
		}
		nodes = append(nodes, PlanNode{NodeID: "buried", CapabilityClasses: []string{string(ControlBreakAttestation)}})
		err := CheckPlan(contractorProfile(t), nodes)
		var violation *PlanConstraintViolation
		if !errors.As(err, &violation) || violation.NodeID != "buried" {
			t.Fatalf("expected the buried violation to be found and named, got %+v", violation)
		}
	})
}
