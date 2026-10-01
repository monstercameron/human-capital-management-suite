package commit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func validCommand(t *testing.T) commit.Command {
	t.Helper()
	pay, err := values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	cmd := commit.Command{
		TenantID: "11111111-1111-4111-8111-111111111111", IntentID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab",
		ProposalRevisionID: "22222222-2222-4222-8222-222222222222", ProposalRevisionNumber: 1,
		ProposalDigest: "sha256:proposal", PlanID: "plan-1", PlanDigest: "sha256:transaction-resolution", WorkflowPlanDigest: "sha256:workflow-plan",
		AuthorityDigest: "sha256:authority", ActorPrincipalID: "principal:hrbp",
		EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		WorkerID: "33333333-3333-4333-8333-333333333333", EmploymentID: "44444444-4444-4444-8444-444444444444",
		AssignmentID: "55555555-5555-4555-8555-555555555555", TargetJobCode: "ENG-MGR1",
		TargetJobID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", TargetGrade: "M1",
		TargetOrganizationID: "66666666-6666-4666-8666-666666666666", TargetPositionID: "77777777-7777-4777-8777-777777777777",
		ManagerRelationshipRef: "manager-rel:8888", ManagerWorkerID: "88888888-8888-4888-8888-888888888888",
		ManagerAncestorWorkerIDs: []string{"99999999-9999-4999-8999-999999999999"}, Location: "NYC", PayZone: "US-NY", FTE: "1.0000",
		PositionOccupancyID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PositionReservationRef: "position-hold:1",
		CompensationPackageID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", BasePayComponentID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		BasePay: pay, PayFrequency: "ANNUAL", BudgetReservationID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		BudgetReservationRef: "budget-hold:1", ExpectedWorkerDigest: "sha256:worker", ExpectedEmploymentDigest: "sha256:employment",
		ExpectedAssignmentDigest: "sha256:assignment", ExpectedJobDigest: "sha256:job", ExpectedPositionDigest: "sha256:position",
		ExpectedPackageDigest: "sha256:package", ExpectedBasePayDigest: "sha256:base", ExpectedBudgetDigest: "sha256:budget",
		ApprovalDecisionIDs: []string{"decision:manager", "decision:finance"}, TaskSubmissionIDs: []string{"submission:ack"},
		Effects:          []commit.ExternalEffect{{EffectID: "payroll:1", DestinationRef: "payroll", SchemaRef: "promotion.payroll/v1", Payload: []byte(`{"ok":true}`)}},
		AssignmentWrites: []commit.AssignmentWrite{{FieldPath: "assignment.grade", CurrentValue: "P3", ProposedValue: "M1", AuthorityDecision: "authority.promotion/v1", ExpectedSource: "revision:1"}},
	}
	cmd.PlanParticipants = cmd.Participants()
	return cmd
}

func TestCommandValidatesTheBoundedPromotion(t *testing.T) {
	cmd := validCommand(t)
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := cmd.ValidateParticipants(cmd.Participants()); err != nil {
		t.Fatalf("participants: %v", err)
	}
}

func TestCommandRefusesMissingApprovalAndManagerCycle(t *testing.T) {
	cmd := validCommand(t)
	cmd.ApprovalDecisionIDs = cmd.ApprovalDecisionIDs[:1]
	if err := cmd.Validate(); !errors.Is(err, commit.ErrApprovalEvidence) {
		t.Fatalf("approval error = %v", err)
	}

	cmd = validCommand(t)
	cmd.ApprovalDecisionIDs = []string{"decision:same", "decision:same"}
	if err := cmd.Validate(); !errors.Is(err, commit.ErrApprovalEvidence) {
		t.Fatalf("duplicate approval error = %v", err)
	}

	cmd = validCommand(t)
	cmd.ManagerAncestorWorkerIDs = append(cmd.ManagerAncestorWorkerIDs, cmd.WorkerID)
	if err := cmd.Validate(); !errors.Is(err, commit.ErrManagerCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestCommandRefusesAPlanThatDropsAParticipant(t *testing.T) {
	cmd := validCommand(t)
	if err := cmd.ValidateParticipants(cmd.Participants()[:3]); !errors.Is(err, commit.ErrParticipantSet) {
		t.Fatalf("participant error = %v", err)
	}
}

func TestCommandRequiresExactAssignmentProvenance(t *testing.T) {
	for name, mutate := range map[string]func(*commit.Command){
		"missing revision number": func(cmd *commit.Command) { cmd.ProposalRevisionNumber = 0 },
		"missing intent":          func(cmd *commit.Command) { cmd.IntentID = "" },
		"missing authority":       func(cmd *commit.Command) { cmd.AssignmentWrites[0].AuthorityDecision = "" },
		"duplicate field": func(cmd *commit.Command) {
			cmd.AssignmentWrites = append(cmd.AssignmentWrites, cmd.AssignmentWrites[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := validCommand(t)
			mutate(&cmd)
			if err := cmd.Validate(); !errors.Is(err, commit.ErrInvalidCommand) {
				t.Fatalf("Validate() = %v, want ErrInvalidCommand", err)
			}
		})
	}
}
