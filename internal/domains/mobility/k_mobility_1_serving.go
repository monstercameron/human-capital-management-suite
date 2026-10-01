package mobility

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ValidateServingContract proves that the authoritative mobility aggregate is
// usable by the serving composition. It is deliberately deterministic and
// read-only: serving startup may validate the domain contract, but it must not
// persist a plan or infer an external jurisdiction's law.
func ValidateServingContract() error {
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		return fmt.Errorf("mobility: serving contract start date: %w", err)
	}
	end, err := values.NewLocalDate(2026, time.January, 20)
	if err != nil {
		return fmt.Errorf("mobility: serving contract end date: %w", err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		return fmt.Errorf("mobility: serving contract interval: %w", err)
	}
	split, err := values.NewDecimal("0.50", 2, values.RoundingExactRequired)
	if err != nil {
		return fmt.Errorf("mobility: serving contract allocation: %w", err)
	}
	assignment := func(id string, role AssignmentRole) (AssignmentRevision, error) {
		return NewAssignmentRevision(AssignmentRevision{
			AssignmentID: id, Revision: 1, Role: role, Type: LongTerm,
			EntityRef: "entity:serving", EmploymentRef: "employment:serving",
			LocationRef: "location:" + id, Jurisdiction: "US-CA",
			PayrollRef: "payroll:" + id, PayrollModel: PayrollSplit,
			Effective: interval, CostAllocationSplit: split,
			SourceAuthority: "hcmnext.mobility.serving/v1",
		})
	}
	home, err := assignment("home:serving", AssignmentHome)
	if err != nil {
		return fmt.Errorf("mobility: serving contract home assignment: %w", err)
	}
	host, err := assignment("host:serving", AssignmentHost)
	if err != nil {
		return fmt.Errorf("mobility: serving contract host assignment: %w", err)
	}
	relocation, err := NewRelocationPackage(RelocationPackage{
		PackageID: "relocation:serving", MobilityID: "mobility:serving",
		WorkerRef: "worker:serving", OwnerRef: "mobility-owner",
		Milestones: []RelocationMilestone{{
			MilestoneID: "arrival", Kind: RelocationArrival, Effective: interval,
			OwnerRef: "mobility-owner", EvidenceRef: "evidence:arrival",
		}},
	})
	if err != nil {
		return fmt.Errorf("mobility: serving contract relocation: %w", err)
	}
	milestones := make([]ImmigrationMilestone, 0, 4)
	for i, kind := range []ImmigrationMilestoneKind{ImmigrationPetition, ImmigrationApproval, ImmigrationExpiry, ImmigrationReverification} {
		at, err := values.NewLocalDate(2026, time.March, i+1)
		if err != nil {
			return fmt.Errorf("mobility: serving contract milestone date: %w", err)
		}
		milestone, err := NewImmigrationMilestone(ImmigrationMilestone{
			MilestoneID: string(kind), ProcessRef: "process:serving", Kind: kind,
			Jurisdiction: "DE-BE", At: at, SourceRef: "immigration.source/serving",
			Disclosure: DisclosureScope{Jurisdiction: "DE-BE", Allowed: true},
		})
		if err != nil {
			return fmt.Errorf("mobility: serving contract immigration milestone: %w", err)
		}
		milestones = append(milestones, milestone)
	}
	plan, err := NewMobilityPlan(MobilityPlan{
		MobilityID: "mobility:serving", Revision: 1, WorkerRef: "worker:serving",
		HomeAssignment: home, HostAssignment: host,
		Legs: []MobilityLeg{{
			LegID: "leg:serving", OriginJurisdiction: "US-CA", DestinationJurisdiction: "DE-BE",
			Effective: interval, Purpose: "serving-contract", WorkPresence: true, SourceRef: "travel:serving",
		}},
		Relocation: relocation, Immigration: milestones,
		Obligations: []MobilityObligation{
			{Kind: ObligationPayroll, Ref: "payroll:serving", Status: ObligationReady, OwnerRef: "payroll", EvidenceRef: "evidence:payroll"},
			{Kind: ObligationTax, Ref: "tax:serving", Status: ObligationReady, OwnerRef: "tax", EvidenceRef: "evidence:tax"},
			{Kind: ObligationPE, Ref: "pe:serving", Status: ObligationReady, OwnerRef: "legal", EvidenceRef: "evidence:pe"},
			{Kind: ObligationPrivacy, Ref: "privacy:serving", Status: ObligationReady, OwnerRef: "privacy", EvidenceRef: "evidence:privacy"},
		},
	})
	if err != nil {
		return fmt.Errorf("mobility: serving contract plan: %w", err)
	}
	if plan.Status != StatusReady || plan.CanonicalDigest == "" {
		return fmt.Errorf("mobility: serving contract plan is not ready")
	}
	if status, err := plan.Assess(); err != nil || status != StatusReady {
		return fmt.Errorf("mobility: serving contract assessment: status=%s err=%v", status, err)
	}
	return nil
}
