package execution

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// fakeReceipts is a ProviderReceiptReader over a fixed map, recording the
// change refs it was asked about.
type fakeReceipts struct {
	receipts map[string]promotionsteps.ProviderReceipt
	err      error
	asked    []string
}

func (f *fakeReceipts) LatestForChange(_ context.Context, _ dbport.Tx, _ uuid.UUID, changeRef string) (promotionsteps.ProviderReceipt, bool, error) {
	f.asked = append(f.asked, changeRef)
	receipt, ok := f.receipts[changeRef]
	return receipt, ok, f.err
}

func providerCommand(t *testing.T) domaincommit.Command {
	t.Helper()
	pay, err := values.NewMoney("98000.00", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return domaincommit.Command{
		TenantID: wfrun034Tenant.String(), ProposalRevisionID: "rev-1",
		TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", BasePay: pay,
		EffectiveAt: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
	}
}

func appliedPayroll(amount string) promotionsteps.ProviderReceipt {
	return promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeApplied, Details: map[string]string{
		promotionsteps.ProviderDetailAmount: amount, promotionsteps.ProviderDetailCurrency: "USD", promotionsteps.ProviderDetailEffectiveDate: "2026-10-16",
	}}
}

func grantedAccess(job, grade string) promotionsteps.ProviderReceipt {
	return promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeGranted, Details: map[string]string{
		promotionsteps.ProviderDetailJobCode: job, promotionsteps.ProviderDetailGrade: grade,
	}}
}

// TestProviderReceiptJudgesThe1_1Observations proves that on a plan with
// provider waits the payroll and access observations additionally require the
// provider's receipt for payroll:/iam:<proposal revision>, that a missing,
// rejected or mismatched receipt observes FAIL with its outcome recorded as a
// fact, that an unconfigured reader never passes, and that a 1.0.0 plan keeps
// the local-only check and never reads a receipt.
func TestProviderReceiptJudgesThe1_1Observations(t *testing.T) {
	ctx := context.Background()
	cmd := providerCommand(t)
	current, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := promotionexec.CompileV1_0()
	if err != nil {
		t.Fatal(err)
	}
	observed := func(context.Context, dbport.Tx, domaincommit.Command) (observation, error) {
		return observation{status: promotionsteps.ObservationObserved, facts: []string{"local=true"}}, nil
	}
	notObserved := func(context.Context, dbport.Tx, domaincommit.Command) (observation, error) {
		return observation{status: promotionsteps.ObservationFailed, facts: []string{"local=false"}}, nil
	}
	payrollRef, accessRef := promotionsteps.PayrollChangeRef("rev-1"), promotionsteps.AccessChangeRef("rev-1")

	for _, tc := range []struct {
		name     string
		receipts map[string]promotionsteps.ProviderReceipt
		nilRead  bool
		local    localCheck
		payroll  bool
		want     string
		fact     string
	}{
		{"payroll applied as committed", map[string]promotionsteps.ProviderReceipt{payrollRef: appliedPayroll("98000.00")}, false, observed, true, promotionsteps.ObservationObserved, "provider.outcome=APPLIED"},
		{"payroll applied another amount", map[string]promotionsteps.ProviderReceipt{payrollRef: appliedPayroll("90000.00")}, false, observed, true, promotionsteps.ObservationFailed, "provider.receipt.matches=false"},
		{"payroll rejected", map[string]promotionsteps.ProviderReceipt{payrollRef: {Outcome: promotionsteps.ProviderOutcomeRejected}}, false, observed, true, promotionsteps.ObservationFailed, "provider.outcome=REJECTED"},
		{"payroll never answered", nil, false, observed, true, promotionsteps.ObservationFailed, "provider.outcome=MISSING"},
		{"payroll applied but local state moved not", map[string]promotionsteps.ProviderReceipt{payrollRef: appliedPayroll("98000.00")}, false, notObserved, true, promotionsteps.ObservationFailed, "local=false"},
		{"payroll reader unconfigured", nil, true, observed, true, promotionsteps.ObservationFailed, "provider.reader=unconfigured"},
		{"access granted as committed", map[string]promotionsteps.ProviderReceipt{accessRef: grantedAccess("OPS-HRBP3", "P3")}, false, observed, false, promotionsteps.ObservationObserved, "provider.outcome=GRANTED"},
		{"access granted another grade", map[string]promotionsteps.ProviderReceipt{accessRef: grantedAccess("OPS-HRBP3", "P2")}, false, observed, false, promotionsteps.ObservationFailed, "provider.receipt.matches=false"},
		{"access answered APPLIED", map[string]promotionsteps.ProviderReceipt{accessRef: {Outcome: promotionsteps.ProviderOutcomeApplied, Details: grantedAccess("OPS-HRBP3", "P3").Details}}, false, observed, false, promotionsteps.ObservationFailed, "provider.outcome=APPLIED"},
		{"access reader unconfigured", nil, true, observed, false, promotionsteps.ObservationFailed, "provider.reader=unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReceipts{receipts: tc.receipts}
			ports := &promotionStepPorts{receipts: reader}
			if tc.nilRead {
				ports.receipts = nil
			}
			ref, matches := accessRef, accessReceiptMatches
			changeRef := promotionsteps.AccessChangeRef
			if tc.payroll {
				ref, matches, changeRef = payrollRef, payrollReceiptMatches, promotionsteps.PayrollChangeRef
			}
			check := ports.providerChecked(execute.StepRequest{Plan: current}, tc.local, changeRef, matches)
			got, err := check(ctx, &scriptTx{}, cmd)
			if err != nil || got.status != tc.want || !slices.Contains(got.facts, tc.fact) {
				t.Fatalf("observation = %+v, %v; want %s with fact %s", got, err, tc.want, tc.fact)
			}
			if !tc.nilRead && !slices.Equal(reader.asked, []string{ref}) {
				t.Fatalf("reader asked about %v, want [%s]", reader.asked, ref)
			}
		})
	}

	// A 1.0.0 plan (and a request with no plan) keeps the local-only check.
	for _, plan := range []*execute.StepRequest{{Plan: frozen}, {}} {
		reader := &fakeReceipts{}
		check := (&promotionStepPorts{receipts: reader}).providerChecked(*plan, observed, promotionsteps.PayrollChangeRef, payrollReceiptMatches)
		got, err := check(ctx, &scriptTx{}, cmd)
		if err != nil || got.status != promotionsteps.ObservationObserved || len(reader.asked) != 0 {
			t.Fatalf("1.0.0 observation = %+v, %v, reads %v; want the local-only OBSERVED", got, err, reader.asked)
		}
	}

	// A failing reader is a port failure, not a verdict.
	boom := errors.New("receipt store unavailable")
	check := (&promotionStepPorts{receipts: &fakeReceipts{err: boom}}).providerChecked(execute.StepRequest{Plan: current}, observed, promotionsteps.PayrollChangeRef, payrollReceiptMatches)
	if _, err := check(ctx, &scriptTx{}, cmd); !errors.Is(err, boom) {
		t.Fatalf("failing reader = %v, want its error", err)
	}
	// An unparseable tenant never reaches the reader.
	bad := cmd
	bad.TenantID = "not-a-uuid"
	reader := &fakeReceipts{}
	got, err := providerObservation(ctx, &scriptTx{}, reader, bad, observation{status: promotionsteps.ObservationObserved}, payrollRef, payrollReceiptMatches)
	if err != nil || got.status != promotionsteps.ObservationFailed || len(reader.asked) != 0 {
		t.Fatalf("malformed tenant = %+v, %v; want FAIL without a read", got, err)
	}
}

// TestPromotionExecuteResolverServesBothVersions proves new starts resolve
// 1.1.0, a continuation pinned to the frozen 1.0.0 digest resolves 1.0.0, a
// continuation pinned to 1.1.0 resolves 1.1.0 and any other pin is refused.
func TestPromotionExecuteResolverServesBothVersions(t *testing.T) {
	current, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := promotionexec.CompileV1_0()
	if err != nil {
		t.Fatal(err)
	}
	resolver := promotionExecuteResolver(current, frozen)
	for _, tc := range []struct {
		pin  string
		want string
	}{
		{"", current.Digest()},
		{current.Digest(), current.Digest()},
		{frozen.Digest(), frozen.Digest()},
	} {
		got, err := resolver.ResolveWorkflow(context.Background(), runtime.StartRequest{PinnedCompiledPlanDigest: tc.pin})
		if err != nil || got.Plan.Digest() != tc.want || got.Pin.CompiledPlanDigest != tc.want || got.WorkflowID != promotionexec.WorkflowID {
			t.Fatalf("pin %q resolved %+v, %v; want %s", tc.pin, got.Pin, err, tc.want)
		}
	}
	if _, err := resolver.ResolveWorkflow(context.Background(), runtime.StartRequest{PinnedCompiledPlanDigest: "sha256:unknown"}); err == nil {
		t.Fatal("a continuation pinned to an unserved digest resolved")
	}

	ports := &promotionStepPorts{planDigest: current.Digest()}
	if got := ports.planDigestOf(frozen); got != frozen.Digest() {
		t.Fatalf("planDigestOf(1.0.0) = %s, want the request's own plan", got)
	}
	if got := ports.planDigestOf(nil); got != current.Digest() {
		t.Fatalf("planDigestOf(nil) = %s, want the composition default", got)
	}
	if got := ports.pinnedPlanDigest(runtime.StartRequest{PinnedCompiledPlanDigest: frozen.Digest()}); got != frozen.Digest() {
		t.Fatalf("pinnedPlanDigest = %s, want the pinned 1.0.0", got)
	}
	if got := ports.pinnedPlanDigest(runtime.StartRequest{}); got != current.Digest() {
		t.Fatalf("pinnedPlanDigest(no pin) = %s, want the composition default", got)
	}
}
