package settlement

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

func releaseDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func releaseLimits(t *testing.T) ReleaseLimits {
	t.Helper()
	limits, err := NewReleaseLimits(releaseDecimal(t, "500.00"), releaseDecimal(t, "300.00"), "USD")
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

func releaseBatch(t *testing.T) PaymentReleaseBatch {
	t.Helper()
	run := releasedRun(t)
	a := validInstruction(t)
	bSpec := instructionSpec("instruction-2")
	bSpec.PayeeRef = "worker:payee-2"
	bSpec.PaymentMethodElection = settlementElection(bSpec.PayeeRef, bSpec.BankDetailRef)
	b, err := NewPaymentInstruction(run, bSpec)
	if err != nil {
		t.Fatal(err)
	}
	population := releasePopulation(t, run, "worker:payee-1", "worker:payee-2")
	batch, err := NewPaymentReleaseBatch("batch-1", values.TenantId("tenant-1"), []PaymentInstruction{a, b}, run, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", population, releaseLimits(t))
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func releasePopulation(t *testing.T, run payroll.PayrollRun, workers ...string) payroll.FrozenPopulation {
	t.Helper()
	members := make([]payroll.PopulationMember, 0, len(workers))
	for _, worker := range workers {
		members = append(members, payroll.PopulationMember{WorkerRef: worker, EmploymentRef: "employment:" + worker, PayGroupRef: run.PayGroupRef})
	}
	population, err := payroll.FreezePopulation(run, values.NewInstant(time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)), members, payroll.LateEntryPolicyExclude)
	if err != nil {
		t.Fatal(err)
	}
	return population
}

func releaseRequest(t *testing.T) PaymentReleaseAuthorizationRequest {
	t.Helper()
	batch := releaseBatch(t)
	return PaymentReleaseAuthorizationRequest{
		Batch: batch, Binding: batch.Binding(), ProposalID: "proposal:" + batch.CanonicalDigest,
		RequesterID: "payroll-preparer", ApproverID: "payroll-controller", ExecutorID: "payroll-executor",
		Approval:        PaymentReleaseApproval{DecisionID: "decision-1", ProposalID: "proposal:" + batch.CanonicalDigest, RequesterID: "payroll-preparer", ApproverID: "payroll-controller", BatchDigest: batch.CanonicalDigest, LimitsDigest: batch.LimitsDigest, Approved: true},
		Constraints:     sod.Constraints{RuleID: "payroll.payment.release.dual_control/v1", RequesterMayNotApprove: true, ExecutorMayNotApprove: true, OneApprovalPerPrincipal: true},
		DecisionContext: sod.DecisionContext{Requester: sod.Actor{Subject: "payroll-preparer"}, Approvers: []sod.Actor{{Subject: "payroll-controller"}}, Executor: sod.Actor{Subject: "payroll-executor"}},
		StepUp:          stepup.Obligation{Code: stepup.CodeStepUpSatisfied, Required: true, Satisfied: true, Tenant: values.TenantId("tenant-1"), Subject: "payroll-controller", SessionRef: "session-controller", Purpose: PaymentReleasePurpose, Capability: PaymentReleaseCapability, Action: PaymentReleaseOperation, ProposalID: "proposal:" + batch.CanonicalDigest, BindingDigest: "ev:stepup:release"},
		AuthorizedAt:    time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

// TestTodo_SETTLE_003 is the primary acceptance case for proposal-bound dual
// control and the release limits.
func TestTodo_SETTLE_003(t *testing.T) {
	d, err := AuthorizePaymentRelease(releaseRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.BatchDigest == "" || d.LimitsDigest == "" || d.ApprovalDecisionID != "decision-1" {
		t.Fatalf("decision did not bind exact control context: %+v", d)
	}
}

func TestTodo_SETTLE_003_Golden(t *testing.T) {
	d, err := AuthorizePaymentRelease(releaseRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.CanonicalDigest, "sha256:") || len(d.CanonicalDigest) != len("sha256:")+64 {
		t.Fatalf("decision digest = %q", d.CanonicalDigest)
	}
	if !strings.Contains(d.Explain(), d.BatchDigest) || !strings.Contains(d.Explain(), d.LimitsDigest) {
		t.Fatalf("Explain lost protected bindings: %q", d.Explain())
	}
}

func TestTodo_SETTLE_003_Race(t *testing.T) {
	req := releaseRequest(t)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := AuthorizePaymentRelease(req)
			if err != nil {
				errs <- err
				return
			}
			if err := decision.Validate(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTodo_SETTLE_003_Integration(t *testing.T) {
	req := releaseRequest(t)
	d, err := AuthorizeRelease(req)
	if err != nil {
		t.Fatal(err)
	}
	if d.SODRuleID != req.Constraints.RuleID || d.StepUpBindingDigest != req.StepUp.BindingDigest || d.TenantID != req.Batch.TenantID {
		t.Fatalf("authorization evidence = %+v", d)
	}
}

func TestTodo_SETTLE_003_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PaymentReleaseAuthorizationRequest)
		want   error
	}{
		{"changed instruction", func(r *PaymentReleaseAuthorizationRequest) {
			r.Binding.InstructionDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}, ErrInstructionChanged},
		{"changed funding", func(r *PaymentReleaseAuthorizationRequest) {
			r.Binding.FundingDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}, ErrFundingChanged},
		{"changed payee", func(r *PaymentReleaseAuthorizationRequest) {
			r.Binding.PayeeDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}, ErrPayeeChanged},
		{"changed limits", func(r *PaymentReleaseAuthorizationRequest) {
			r.Binding.LimitsDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}, ErrLimitsChanged},
		{"self approval", func(r *PaymentReleaseAuthorizationRequest) {
			r.ApproverID = r.RequesterID
			r.Approval.ApproverID = r.RequesterID
			r.StepUp.Subject = r.RequesterID
			r.DecisionContext.Approvers = []sod.Actor{{Subject: r.RequesterID}}
		}, ErrSelfApproval},
		{"step up", func(r *PaymentReleaseAuthorizationRequest) {
			r.StepUp.Satisfied = false
			r.StepUp.Code = stepup.CodeStepUpRequired
		}, ErrReleaseStepUpRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := releaseRequest(t)
			tc.mutate(&req)
			_, err := AuthorizePaymentRelease(req)
			if !errors.Is(err, tc.want) || !errors.Is(err, ErrReleaseRejected) {
				t.Fatalf("error = %v, want %v and SETTLE_003_REJECTED", err, tc.want)
			}
		})
	}
}

func TestTodo_REV_044_01_Security(t *testing.T) {
	req := releaseRequest(t)
	_, err := NewPaymentReleaseBatch("batch-omitted-worker", req.Batch.TenantID, req.Batch.Instructions[:1], req.Batch.PayrollRun, req.Batch.FundingDigest, req.Batch.Population, req.Batch.Limits)
	if !errors.Is(err, ErrInvalidReleaseBatch) {
		t.Fatalf("batch omitted an affected worker from its authoritative population: %v", err)
	}
	req.Batch.Instructions[0].PaymentMethodElection.Consented = false
	if _, err := AuthorizePaymentRelease(req); !errors.Is(err, ErrReleaseRejected) || !errors.Is(err, ErrInvalidReleaseBatch) {
		t.Fatalf("release authorization accepted an unconsented payment election: %v", err)
	}
}

func TestTodo_SETTLE_003_Mutation(t *testing.T) {
	req := releaseRequest(t)
	d, err := AuthorizePaymentRelease(req)
	if err != nil {
		t.Fatal(err)
	}
	originalBatchDigest := d.BatchDigest
	changedBatch := req.Batch
	changedBatch.PayeeDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	changedBatch.CanonicalDigest = changedBatch.digest()
	req.Batch = changedBatch
	if _, err := AuthorizePaymentRelease(req); !errors.Is(err, ErrInvalidReleaseBatch) {
		t.Fatalf("independently changed payee digest was accepted: %v", err)
	}
	if d.BatchDigest != originalBatchDigest || d.Validate() != nil {
		t.Fatal("accepted decision was mutated by a later request")
	}
	req = releaseRequest(t)
	amountChanged := req.Batch
	amountChanged.InstructionAmounts[0] = releaseDecimal(t, "126.00")
	amountChanged.InstructionDigest = instructionSetDigest(amountChanged.InstructionDigests, amountChanged.InstructionAmounts)
	amountChanged.CanonicalDigest = amountChanged.digest()
	req.Batch = amountChanged
	if _, err := AuthorizePaymentRelease(req); !errors.Is(err, ErrInvalidReleaseBatch) || !errors.Is(err, ErrReleaseRejected) {
		t.Fatalf("changed instruction amount was accepted: %v", err)
	}
}
