package time

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timeclock"
)

func profileFixture(category timeprofile.WorkerCategory, capture timeprofile.CaptureMode, pay timeprofile.PayBasis, exemption timeprofile.ExemptionStatus, destination timeprofile.Destination) timeprofile.TimeProfile {
	return timeprofile.TimeProfile{
		ID: "ironridge-", Version: 1, TenantRef: values.TenantId("ironridge"),
		EffectiveFrom: values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Category:      category, Capture: capture, PayBasis: pay, Exemption: exemption,
		Destination: destination, OvertimeMethod: timeprofile.OvertimeSingleRate,
		AggregationKey: "worker:fixture", OvertimeJurisdictions: []string{"US-FED"},
	}
}

// TestTodo_WTIME_018_Conformance proves six worker profiles resolve through
// one tenant's typed resolver and compile the selected workflow. The tests
// assert the destination and legal flags that must stay bound to that
// selection, including contractor/agency payroll firewalls.
func TestTodo_WTIME_018_Conformance(t *testing.T) {
	spanish := profileFixture(timeprofile.CategoryEmployee, timeprofile.CaptureDuration, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	spanish.OvertimeJurisdictions = []string{"ES"}
	spanish.RestPeriodDutyRequired, spanish.DailyRecordingDutyRequired = true, true
	minor := profileFixture(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	minor.MinorAgeBand, minor.MinorPermitVerified = timeprofile.MinorAgeBand16To17, true
	dcaa := profileFixture(timeprofile.CategoryEmployee, timeprofile.CaptureDuration, timeprofile.PaySalary, timeprofile.Exempt, timeprofile.DestinationPayroll)
	dcaa.GovernmentContract.DCAATotalTimeAccounting = true
	dcaa.GovernmentContract.UncompensatedOvertimeTracked = true
	dcaa.Taxonomy.Primary = "DCAA-INDIRECT"
	cases := []struct {
		name            string
		profile         timeprofile.TimeProfile
		wantTemplate    string
		wantDestination timeprofile.Destination
	}{
		{"california hourly", profileFixture(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll), string(timeprofile.TemplatePunchSession), timeprofile.DestinationPayroll},
		{"DCAA salaried", dcaa, string(timeprofile.TemplateDurationSheet), timeprofile.DestinationPayroll},
		{"contractor SOW", profileFixture(timeprofile.CategoryContractor, timeprofile.CaptureDuration, timeprofile.PayContract, timeprofile.NotApplicable, timeprofile.DestinationInvoice), string(timeprofile.TemplateContractorTime), timeprofile.DestinationInvoice},
		{"agency VMS", profileFixture(timeprofile.CategoryAgencyTemp, timeprofile.CaptureDuration, timeprofile.PayHourly, timeprofile.NotApplicable, timeprofile.DestinationAgency), string(timeprofile.TemplateAgencyTime), timeprofile.DestinationAgency},
		{"minor hourly", minor, string(timeprofile.TemplatePunchSession), timeprofile.DestinationPayroll},
		{"Spanish EU employee", spanish, string(timeprofile.TemplateDurationSheet), timeprofile.DestinationPayroll},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.profile.Validate(); err != nil {
				t.Fatalf("profile invalid: %v", err)
			}
			templateID, plan, err := timeclock.ResolvePlan(tc.profile)
			if err != nil {
				t.Fatalf("ResolvePlan: %v", err)
			}
			if templateID != tc.wantTemplate || plan.WorkflowID == "" {
				t.Fatalf("resolved template/workflow = %q/%q, want %q/non-empty", templateID, plan.WorkflowID, tc.wantTemplate)
			}
			if tc.profile.Destination != tc.wantDestination {
				t.Fatalf("destination = %q, want %q", tc.profile.Destination, tc.wantDestination)
			}
			if tc.profile.Category == timeprofile.CategoryContractor && timeclock.DeliversThroughPeriod(templateID) {
				t.Fatal("contractor unexpectedly routed through payroll period plan")
			}
			if tc.profile.Category == timeprofile.CategoryAgencyTemp && timeclock.DeliversThroughPeriod(templateID) {
				t.Fatal("agency worker unexpectedly routed through payroll period plan")
			}
			if tc.profile.MinorAgeBand.IsMinor() && templateID != string(timeprofile.TemplatePunchSession) {
				t.Fatalf("minor profile bypassed punch template: %q", templateID)
			}
			if tc.profile.RestPeriodDutyRequired || tc.profile.DailyRecordingDutyRequired {
				if templateID != string(timeprofile.TemplateDurationSheet) {
					t.Fatalf("EU duty profile bypassed duration template: %q", templateID)
				}
			}
		})
	}
}
