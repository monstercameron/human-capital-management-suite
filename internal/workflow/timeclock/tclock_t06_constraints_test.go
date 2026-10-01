package timeclock

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WTIME_006_CompiledOverlayFailsClosed(t *testing.T) {
	base := timeprofile.TimeProfile{
		ID: "contractor", Version: 1, TenantRef: "tenant-a",
		EffectiveFrom: instantForT06("2026-01-01T00:00:00Z"), Capture: timeprofile.CapturePunch,
		PayBasis: timeprofile.PayContract, Exemption: timeprofile.NotApplicable,
		Category: timeprofile.CategoryContractor, AggregationKey: "worker:w1",
		OvertimeMethod: timeprofile.OvertimeNone, Destination: timeprofile.DestinationInvoice,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("profile: %v", err)
	}

	unknownDecision := &workflow.CompiledWorkflow{WorkflowID: "overlay", Nodes: []workflow.CompiledNode{{
		ID: "tenant-added-rule", Type: workflow.StepDecision,
		Decision: &workflow.CompiledDecision{RuleRef: "rules.tenant.unknown/v1"},
	}}}
	if _, err := PlanNodes(unknownDecision); !errors.Is(err, ErrUnmanifestedNode) {
		t.Fatalf("unknown decision rule must fail closed, got %v", err)
	}

	unknownForm := &workflow.CompiledWorkflow{WorkflowID: "overlay", Nodes: []workflow.CompiledNode{{
		ID: "tenant-added-form", Type: workflow.StepTask,
		OutputSchema: workflow.SchemaRef{SchemaID: "hcmnext.forms.time.unknown/v1"},
	}}}
	if _, err := PlanNodes(unknownForm); !errors.Is(err, ErrUnmanifestedNode) {
		t.Fatalf("unknown task form must fail closed, got %v", err)
	}

	if err := CheckCompiled(base, (*workflow.CompiledWorkflow)(nil)); !errors.Is(err, timeprofile.ErrPlanConstraintViolated) {
		t.Fatalf("an all-nil plan set must fail closed, got %v", err)
	}
}

func TestTodo_WTIME_006_ShippedPlansResolveAgainstManifestPolicy(t *testing.T) {
	profiles := []struct {
		name string
		p    timeprofile.TimeProfile
	}{
		{
			name: "employee punch",
			p: timeprofile.TimeProfile{
				ID: "employee-punch", Version: 1, TenantRef: "tenant-a", EffectiveFrom: instantForT06("2026-01-01T00:00:00Z"),
				Capture: timeprofile.CapturePunch, PayBasis: timeprofile.PayHourly, Exemption: timeprofile.NonExempt,
				Category: timeprofile.CategoryEmployee, OvertimeMethod: timeprofile.OvertimeSingleRate,
				AggregationKey: "worker:w1", Destination: timeprofile.DestinationPayroll,
			},
		},
		{
			name: "contractor invoice",
			p: timeprofile.TimeProfile{
				ID: "contractor", Version: 1, TenantRef: "tenant-a", EffectiveFrom: instantForT06("2026-01-01T00:00:00Z"),
				Capture: timeprofile.CaptureDuration, PayBasis: timeprofile.PayContract, Exemption: timeprofile.NotApplicable,
				Category: timeprofile.CategoryContractor, OvertimeMethod: timeprofile.OvertimeNone,
				AggregationKey: "worker:w2", Destination: timeprofile.DestinationInvoice,
			},
		},
		{
			name: "minor punch",
			p: timeprofile.TimeProfile{
				ID: "minor", Version: 1, TenantRef: "tenant-a", EffectiveFrom: instantForT06("2026-01-01T00:00:00Z"),
				Capture: timeprofile.CapturePunch, PayBasis: timeprofile.PayHourly, Exemption: timeprofile.NonExempt,
				Category: timeprofile.CategoryEmployee, MinorAgeBand: timeprofile.MinorAgeBand16To17,
				MinorPermitVerified: true, OvertimeMethod: timeprofile.OvertimeSingleRate,
				AggregationKey: "worker:w3", Destination: timeprofile.DestinationPayroll,
			},
		},
		{
			name: "eu duty duration",
			p: timeprofile.TimeProfile{
				ID: "eu", Version: 1, TenantRef: "tenant-a", EffectiveFrom: instantForT06("2026-01-01T00:00:00Z"),
				Capture: timeprofile.CaptureDuration, PayBasis: timeprofile.PaySalary, Exemption: timeprofile.Exempt,
				Category: timeprofile.CategoryEmployee, RestPeriodDutyRequired: true, DailyRecordingDutyRequired: true,
				OvertimeMethod: timeprofile.OvertimeNone, AggregationKey: "worker:w4", Destination: timeprofile.DestinationPayroll,
			},
		},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.p.Validate(); err != nil {
				t.Fatalf("profile: %v", err)
			}
			if _, _, err := ResolvePlan(tc.p); err != nil {
				t.Fatalf("ResolvePlan: %v", err)
			}
		})
	}
}

func instantForT06(value string) values.Instant {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return values.NewInstant(at)
}
