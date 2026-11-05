package workorderbilling

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type bill002Clock struct{ now time.Time }

func (c *bill002Clock) Now() time.Time { return c.now }

func bill002Op() BillOp {
	return BillOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "biller-1", BillingAllowed: true}
}

func bill002Registry(now time.Time) (*SourceClaimRegistry, *bill002Clock) {
	clock := &bill002Clock{now: now}
	return NewSourceClaimRegistry(clock.Now), clock
}

func bill002Rate() BillRateInput {
	return BillRateInput{Ref: "rate:USD90/hour", Currency: "USD", Amount: "90", Unit: "hour"}
}

func bill002TimeSource() BillSourceInput {
	return BillSourceInput{
		Owner: "JOB007", RootID: "alloc-7", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "120", RateRef: "rate:USD90/hour",
		PricingMode: PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-7", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Field labor, Oct 21", ServiceDate: "2026-10-21",
	}
}

// TestTodo_SMB_BILL_002 pins the billable-source acceptance oracles: approved
// 120 minutes at USD 90/hour previews USD 180; claiming 30 then 90 minutes
// totals USD 180 with nothing left; over-claims are refused with the exact
// remainder; corrections after issue raise an adjustment without rewriting
// history; cross-company sources and salary details stay out of reach.
func TestTodo_SMB_BILL_002(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill002Registry(now)
	op := bill002Op()

	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	source, err := registry.RegisterSource(op, bill002TimeSource())
	if err != nil {
		t.Fatal(err)
	}
	if source.FamilyID == "" || source.Revision != 1 {
		t.Fatalf("registered source = %+v, want family identity at rev 1", source)
	}

	preview, err := registry.PreviewClaim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "120",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.BillableQty != "2" || preview.BillableUnit != "hour" || preview.Amount != "180.00 USD" {
		t.Fatalf("preview = %+v, want 2 hour / 180.00 USD", preview)
	}

	first, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "30",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue, DraftRef: "draft-1",
	}, "claim-30")
	if err != nil {
		t.Fatal(err)
	}
	if first.ClaimedQty != "30" || first.RemainingQty != "90" || first.Amount != "45.00 USD" {
		t.Fatalf("first receipt = %+v, want 30 claimed / 90 left / 45.00 USD", first)
	}
	second, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "90",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue, DraftRef: "draft-2",
	}, "claim-90")
	if err != nil {
		t.Fatal(err)
	}
	if second.ClaimedQty != "90" || second.RemainingQty != "0" || second.Amount != "135.00 USD" {
		t.Fatalf("second receipt = %+v, want 90 claimed / 0 left / 135.00 USD", second)
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "claim-over"); !errors.Is(err, ErrBillSourceShortfall) {
		t.Fatalf("over-claim err = %v, want shortfall with exact remainder", err)
	}

	views, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ClaimedQty != "120" || views[0].RemainingQty != "0" || views[0].Eligible {
		t.Fatalf("views = %+v, want one fully billed source", views)
	}
	if len(views[0].Reasons) == 0 {
		t.Fatal("fully billed source carries no reason")
	}

	correction, err := registry.CorrectSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "alloc-7", Revision: 2,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "100", RateRef: "rate:USD90/hour",
		PricingMode: PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-8", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-22T08:00:00Z",
		Description: "Field labor, Oct 21 (corrected)", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !correction.AdjustmentRequired || correction.OverIssuedQty != "20" {
		t.Fatalf("correction = %+v, want adjustment for 20 over-issued minutes", correction)
	}
	replayed, err := registry.ClaimStatus(op, "claim-30")
	if err != nil || replayed.ClaimID != first.ClaimID || replayed.ClaimedQty != "30" || replayed.Amount != "45.00 USD" {
		t.Fatalf("original receipt changed by correction: %+v err=%v", replayed, err)
	}

	otherCompany := op
	otherCompany.LegalEntityID = "le-2"
	if _, err := registry.QuerySources(otherCompany, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrBillSourceDenied) {
		t.Fatalf("cross-company query err = %v, want neutral denied", err)
	}
	if _, err := registry.Claim(otherCompany, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 2, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "claim-foreign"); !errors.Is(err, ErrBillSourceDenied) {
		t.Fatalf("cross-company claim err = %v, want neutral denied", err)
	}

	cost, err := registry.RegisterCost(op, BillCostInput{
		Owner: "TIME", RootID: "timecard-3", WorkOrderID: "wo-1",
		Detail: "worker pay detail", Amount: "60.00 USD", EvidenceRef: "payroll-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReadCost(op, cost.CostID); !errors.Is(err, ErrBillSourceDenied) {
		t.Fatalf("salary read without grant err = %v, want neutral denied", err)
	}
	payroll := op
	payroll.CostVisible = true
	if _, err := registry.ReadCost(payroll, cost.CostID); err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if strings.Contains(view.Description, "pay") {
			t.Fatalf("customer view leaks pay detail: %+v", view)
		}
	}

	expense, err := registry.RegisterExpense(op, BillExpenseInput{
		Owner: "BILL010", RootID: "exp-50", WorkOrderID: "wo-1", JobRef: "job-9",
		CostAmount: "50.00 USD", RechargeRate: "0.20", RechargeEligible: true,
		ReimbursementAmount: "50.00 USD",
		EvidenceRef:         "bill010-appr-4", RevisionBinding: "bill010-rev-4",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
		Description: "Parts run", ServiceDate: "2026-10-21", Category: "recharge",
	})
	if err != nil {
		t.Fatal(err)
	}
	included, err := registry.ClaimExpense(op, expense.ExpenseID, true, "contract-a", "v1", "exp-claim-1")
	if err != nil {
		t.Fatal(err)
	}
	if included.ClaimedAmount != "60.00 USD" || included.RemainingAmount != "0.00 USD" {
		t.Fatalf("expense receipt = %+v, want 60.00 claimed / 0.00 left", included)
	}
	untouched, err := registry.ReadCost(payroll, cost.CostID)
	if err != nil || untouched.Amount != "60.00 USD" {
		t.Fatalf("cost observation moved: %+v err=%v", untouched, err)
	}
}

// TestTodo_SMB_BILL_002_Unit pins numeric and lifecycle boundaries: zero and
// negative quantities, malformed decimals, unknown approval, missing rate,
// stale revisions, lease edges and inexact unit conversions.
func TestTodo_SMB_BILL_002_Unit(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, clock := bill002Registry(now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	base := bill002TimeSource()

	for _, tc := range []struct {
		name   string
		mutate func(*BillSourceInput)
	}{
		{"zero qty", func(i *BillSourceInput) { i.ApprovedQty = "0" }},
		{"negative qty", func(i *BillSourceInput) { i.ApprovedQty = "-5" }},
		{"bad decimal", func(i *BillSourceInput) { i.ApprovedQty = "12.x" }},
		{"missing evidence", func(i *BillSourceInput) { i.EvidenceRef = "" }},
		{"missing approver", func(i *BillSourceInput) { i.ApprovedBy = "" }},
		{"zero approval time", func(i *BillSourceInput) { i.ApprovedAt = "" }},
		{"missing rate", func(i *BillSourceInput) { i.RateRef = "" }},
		{"unknown rate", func(i *BillSourceInput) { i.RateRef = "rate:missing" }},
		{"unit mismatch", func(i *BillSourceInput) { i.Unit = "each" }},
		{"money without amount", func(i *BillSourceInput) {
			i.Kind = BillKindMaterial
			i.UnitDimension = "money"
			i.Unit = ""
			i.ApprovedQty = ""
		}},
		{"qty with amount", func(i *BillSourceInput) { i.ApprovedAmount = "10.00 USD" }},
		{"bad service date", func(i *BillSourceInput) { i.ServiceDate = "21-10-2026" }},
	} {
		input := base
		input.RootID = "alloc-unit-" + strings.ReplaceAll(tc.name, " ", "-")
		tc.mutate(&input)
		if _, err := registry.RegisterSource(op, input); !errors.Is(err, ErrBillSourceValidation) {
			t.Errorf("%s: err = %v, want validation refusal", tc.name, err)
		}
	}

	source, err := registry.RegisterSource(op, base)
	if err != nil {
		t.Fatal(err)
	}
	again, err := registry.RegisterSource(op, base)
	if err != nil || again.FamilyID != source.FamilyID {
		t.Fatalf("identical re-register = %+v err=%v, want same source", again, err)
	}
	changed := base
	changed.ApprovedQty = "60"
	if _, err := registry.RegisterSource(op, changed); !errors.Is(err, ErrBillSourceConflict) {
		t.Fatalf("same revision new payload err = %v, want conflict", err)
	}

	for _, tc := range []struct {
		name  string
		claim BillClaimInput
		key   string
	}{
		{"zero qty", BillClaimInput{FamilyID: source.FamilyID, Revision: 1, Qty: "0", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}, "u-zero"},
		{"bad decimal", BillClaimInput{FamilyID: source.FamilyID, Revision: 1, Qty: "1..2", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}, "u-bad"},
		{"missing policy", BillClaimInput{FamilyID: source.FamilyID, Revision: 1, Qty: "1", Kind: BillClaimIssue}, "u-policy"},
		{"hold without lease", BillClaimInput{FamilyID: source.FamilyID, Revision: 1, Qty: "1", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold}, "u-lease"},
		{"money on qty source", BillClaimInput{FamilyID: source.FamilyID, Revision: 1, Qty: "1", Amount: "1.00 USD", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}, "u-both"},
	} {
		if _, err := registry.Claim(op, tc.claim, tc.key); !errors.Is(err, ErrBillSourceValidation) {
			t.Errorf("%s: err = %v, want validation refusal", tc.name, err)
		}
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 7, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "u-stale"); !errors.Is(err, ErrBillSourceStale) {
		t.Fatalf("wrong-revision claim err = %v, want stale refusal", err)
	}

	odd := base
	odd.RootID = "alloc-odd"
	odd.ApprovedQty = "7"
	oddSource, err := registry.RegisterSource(op, odd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PreviewClaim(op, BillClaimInput{
		FamilyID: oddSource.FamilyID, Revision: 1, Qty: "7",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}); !errors.Is(err, ErrBillSourceValidation) {
		t.Fatalf("inexact conversion preview err = %v, want validation refusal", err)
	}

	hold, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "60",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold, LeaseMinutes: 10,
	}, "u-hold")
	if err != nil {
		t.Fatal(err)
	}
	if hold.Status != "held" || hold.LeaseExpiry == "" {
		t.Fatalf("hold receipt = %+v, want held status with lease expiry", hold)
	}
	clock.now = clock.now.Add(11 * time.Minute)
	if _, err := registry.CommitHold(op, hold.ClaimID, "u-commit"); !errors.Is(err, ErrBillSourceState) {
		t.Fatalf("expired hold commit err = %v, want state refusal", err)
	}
	if swept := registry.SweepExpired(clock.now); swept != 1 {
		t.Fatalf("swept = %d, want 1 expired hold", swept)
	}
	views, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.FamilyID == source.FamilyID && view.RemainingQty != "120" {
			t.Fatalf("view after sweep = %+v, want full 120 restored", view)
		}
	}
}

// TestTodo_SMB_BILL_002_Property proves conservation under a deterministic
// pseudo-random split stream: claimed plus remaining always equals approved,
// and the same split multiset reaches the same total in any order.
func TestTodo_SMB_BILL_002_Property(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(20260214))
	splits := make([]int, 0, 24)
	for i := 0; i < 24; i++ {
		splits = append(splits, 15*(1+rng.Intn(4)))
	}
	run := func(t *testing.T, order []int, keyPrefix string) string {
		t.Helper()
		registry, _ := bill002Registry(now)
		op := bill002Op()
		if err := registry.RegisterRate(op, bill002Rate()); err != nil {
			t.Fatal(err)
		}
		input := bill002TimeSource()
		input.RootID = "alloc-prop-" + keyPrefix
		input.ApprovedQty = "600"
		source, err := registry.RegisterSource(op, input)
		if err != nil {
			t.Fatal(err)
		}
		claimed := 0
		for i, qty := range order {
			receipt, err := registry.Claim(op, BillClaimInput{
				FamilyID: source.FamilyID, Revision: 1, Qty: itoa(qty),
				PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
			}, keyPrefix+itoa(i))
			if err != nil {
				if !errors.Is(err, ErrBillSourceShortfall) {
					t.Fatalf("split %d err = %v, want issue or shortfall", qty, err)
				}
				continue
			}
			claimed += qty
			if receipt.ClaimedQty != itoa(qty) || receipt.RemainingQty != itoa(600-claimed) {
				t.Fatalf("receipt = %+v, want claimed %d / left %d", receipt, qty, 600-claimed)
			}
			views, err := registry.QuerySources(op, "wo-1",
				time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if len(views) != 1 || views[0].ClaimedQty != itoa(claimed) || views[0].RemainingQty != itoa(600-claimed) {
				t.Fatalf("views = %+v, want conservation at %d claimed", views, claimed)
			}
		}
		return itoa(claimed)
	}
	want := run(t, splits, "fwd-")
	reversed := append([]int(nil), splits...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if got := run(t, reversed, "rev-"); got != want {
		t.Fatalf("reversed splits claimed %s, want %s", got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// TestTodo_SMB_BILL_002_Security proves the neutral authorization boundary:
// cross-tenant references, guessed claim keys and denied roles all produce
// the same denied response, stale revisions are refused exactly, and an
// observed timeout never becomes an invented success.
func TestTodo_SMB_BILL_002_Security(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill002Registry(now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	source, err := registry.RegisterSource(op, bill002TimeSource())
	if err != nil {
		t.Fatal(err)
	}

	foreign := op
	foreign.TenantID = "tenant-b"
	if _, err := registry.QuerySources(foreign, "wo-1", now.Add(-time.Hour), now); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("cross-tenant query err = %v, want neutral denied", err)
	}
	if _, err := registry.Claim(foreign, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "sec-foreign"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("cross-tenant claim err = %v, want neutral denied", err)
	}
	if _, err := registry.ClaimStatus(foreign, "claim-30"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("cross-tenant status err = %v, want neutral denied", err)
	}
	if _, err := registry.FamilyTrace(foreign, source.FamilyID); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("cross-tenant trace err = %v, want neutral denied", err)
	}

	if _, err := registry.ClaimStatus(op, "never-used-key"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("unknown key status err = %v, want neutral denied, never success", err)
	}
	if _, err := registry.ReleaseClaim(op, "claim-missing", "sec-release"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("guessed release err = %v, want neutral denied", err)
	}
	if _, err := registry.FamilyTrace(op, "NOPE:missing"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("guessed trace err = %v, want neutral denied", err)
	}

	denied := op
	denied.BillingAllowed = false
	if _, err := registry.QuerySources(denied, "wo-1", now.Add(-time.Hour), now); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("denied-role query err = %v, want neutral denied", err)
	}
	if _, err := registry.PreviewClaim(denied, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("denied-role preview err = %v, want neutral denied", err)
	}
	if _, err := registry.Claim(denied, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "sec-denied"); !errors.Is(err, ErrBillSourceDenied) {
		t.Errorf("denied-role claim err = %v, want neutral denied", err)
	}

	if _, err := registry.CorrectSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "alloc-7", Revision: 2,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "120", RateRef: "rate:USD90/hour",
		PricingMode: PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-8", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-22T08:00:00Z",
		Description: "Field labor, Oct 21 (corrected)", ServiceDate: "2026-10-21",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "1",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "sec-stale"); !errors.Is(err, ErrBillSourceStale) {
		t.Errorf("stale revision claim err = %v, want stale refusal", err)
	}
}

// TestTodo_SMB_BILL_002_Race hammers one 8-hour remainder with concurrent
// claims: a 5-hour and a 3-hour claim win exactly once each, duplicate keys
// converge on one receipt, and a concurrent ninth hour is refused.
func TestTodo_SMB_BILL_002_Race(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill002Registry(now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	input := bill002TimeSource()
	input.RootID = "alloc-race"
	input.ApprovedQty = "480"
	source, err := registry.RegisterSource(op, input)
	if err != nil {
		t.Fatal(err)
	}

	type attempt struct {
		qty string
		key string
	}
	type outcome struct {
		key     string
		receipt BillReceipt
		err     error
	}
	plan := []attempt{
		{"300", "race-a"}, {"180", "race-b"}, {"60", "race-c"},
		{"300", "race-a"}, {"180", "race-b"}, {"60", "race-c"},
		{"300", "race-d"}, {"180", "race-e"}, {"60", "race-f"},
	}
	results := make(chan outcome, len(plan))
	var wg sync.WaitGroup
	for _, a := range plan {
		wg.Add(1)
		go func(a attempt) {
			defer wg.Done()
			receipt, err := registry.Claim(op, BillClaimInput{
				FamilyID: source.FamilyID, Revision: 1, Qty: a.qty,
				PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
			}, a.key)
			results <- outcome{key: a.key, receipt: receipt, err: err}
		}(a)
	}
	wg.Wait()
	close(results)

	byKey := map[string][]outcome{}
	for r := range results {
		byKey[r.key] = append(byKey[r.key], r)
	}
	for key, outcomes := range byKey {
		var winner *BillReceipt
		for _, o := range outcomes {
			if o.err == nil {
				if winner != nil && winner.ClaimID != o.receipt.ClaimID {
					t.Fatalf("key %s produced two receipts: %+v vs %+v", key, *winner, o.receipt)
				}
				r := o.receipt
				winner = &r
			} else if !errors.Is(o.err, ErrBillSourceShortfall) {
				t.Fatalf("key %s err = %v, want issue or shortfall", key, o.err)
			}
		}
	}
	views, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %+v, want one family view", views)
	}
	claimed, remaining := views[0].ClaimedQty, views[0].RemainingQty
	if !bill002Conserves(t, "480", claimed, remaining) {
		t.Fatalf("views = %+v, want claimed+remaining == 480 with claimed > 0", views)
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "481",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}, "race-ninth"); !errors.Is(err, ErrBillSourceShortfall) {
		t.Fatalf("over-remainder err = %v, want shortfall", err)
	}
}

// bill002Conserves asserts approved == claimed+remaining with claimed > 0.
func bill002Conserves(t *testing.T, approved, claimed, remaining string) bool {
	t.Helper()
	dec := func(text string) values.Decimal {
		scale := int32(0)
		if dot := strings.IndexByte(text, '.'); dot >= 0 {
			scale = int32(len(text) - dot - 1)
		}
		d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
		if err != nil {
			t.Fatalf("bad test decimal %q: %v", text, err)
		}
		return d
	}
	sum, err := dec(claimed).Add(dec(remaining))
	if err != nil {
		t.Fatal(err)
	}
	return sum.Cmp(dec(approved)) == 0 && dec(claimed).Sign() > 0
}

// TestTodo_SMB_BILL_002_Fault proves the failure semantics: expired draft
// holds can release but never issue; issued allocations need an authorized
// reversal; a commit observed as timed out replays the original receipt on
// retry instead of inventing a second effect.
func TestTodo_SMB_BILL_002_Fault(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, clock := bill002Registry(now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	source, err := registry.RegisterSource(op, bill002TimeSource())
	if err != nil {
		t.Fatal(err)
	}

	hold, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "60",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold,
		LeaseMinutes: 30, DraftRef: "draft-hold",
	}, "fault-hold")
	if err != nil {
		t.Fatal(err)
	}
	committed, err := registry.CommitHold(op, hold.ClaimID, "fault-commit")
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != "issued" || committed.ClaimedQty != "60" || committed.RemainingQty != "60" {
		t.Fatalf("committed = %+v, want issued 60 / 60 left", committed)
	}
	if _, err := registry.ReleaseClaim(op, hold.ClaimID, "fault-release-issued"); !errors.Is(err, ErrBillSourceState) {
		t.Fatalf("issued release err = %v, want state refusal (reversal required)", err)
	}
	reversed, err := registry.ReverseIssued(op, hold.ClaimID, "customer credit", "adj-1", "fault-reverse")
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Status != "reversed" || reversed.RemainingQty != "120" {
		t.Fatalf("reversed = %+v, want reversed with full 120 restored", reversed)
	}
	if _, err := registry.ReverseIssued(op, hold.ClaimID, "again", "adj-2", "fault-reverse-2"); !errors.Is(err, ErrBillSourceState) {
		t.Fatalf("double reversal err = %v, want state refusal", err)
	}

	expiring, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "120",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold,
		LeaseMinutes: 5, DraftRef: "draft-short",
	}, "fault-expiring")
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(6 * time.Minute)
	if _, err := registry.CommitHold(op, expiring.ClaimID, "fault-commit-late"); !errors.Is(err, ErrBillSourceState) {
		t.Fatalf("late commit err = %v, want state refusal", err)
	}
	released, err := registry.ReleaseClaim(op, expiring.ClaimID, "fault-release")
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != "released" || released.RemainingQty != "120" {
		t.Fatalf("released = %+v, want released with 120 restored", released)
	}

	before, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// A caller that timed out retries the same key: the original receipt
	// replays with no second effect.
	retry, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "60",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold,
		LeaseMinutes: 30, DraftRef: "draft-hold",
	}, "fault-hold")
	if err != nil {
		t.Fatal(err)
	}
	if retry.ClaimID != hold.ClaimID || retry.Status != "held" || retry.RemainingQty != "60" {
		t.Fatalf("retry = %+v, want original receipt replayed", retry)
	}
	after, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) || before[0].ClaimedQty != after[0].ClaimedQty {
		t.Fatalf("retry changed durable state: before=%+v after=%+v", before, after)
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "61",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimHold,
		LeaseMinutes: 30, DraftRef: "draft-hold",
	}, "fault-hold"); !errors.Is(err, ErrBillSourceConflict) {
		t.Fatalf("reused key new payload err = %v, want conflict", err)
	}

	data, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewSourceClaimRegistry(clock.Now)
	if err := restored.Import(data); err != nil {
		t.Fatal(err)
	}
	restoredViews, err := restored.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredViews) != 1 || restoredViews[0].RemainingQty != "120" {
		t.Fatalf("restored views = %+v, want 120 remaining", restoredViews)
	}
	if _, err := restored.ClaimStatus(op, "fault-hold"); err != nil {
		t.Fatalf("restored status err = %v, want replayable receipt", err)
	}
}

// TestTodo_SMB_BILL_002_Conformance pins every critical guard by name so a
// removed check fails a named row instead of silently widening billing.
func TestTodo_SMB_BILL_002_Conformance(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	setup := func(t *testing.T) (*SourceClaimRegistry, BillOp, BillSource) {
		t.Helper()
		registry, _ := bill002Registry(now)
		op := bill002Op()
		if err := registry.RegisterRate(op, bill002Rate()); err != nil {
			t.Fatal(err)
		}
		source, err := registry.RegisterSource(op, bill002TimeSource())
		if err != nil {
			t.Fatal(err)
		}
		return registry, op, source
	}
	claimOf := func(family string) BillClaimInput {
		return BillClaimInput{FamilyID: family, Revision: 1, Qty: "30", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}
	}
	rows := []struct {
		name string
		run  func(*testing.T, *SourceClaimRegistry, BillOp, BillSource) error
		want error
	}{
		{"unknown approval blocks register", func(t *testing.T, r *SourceClaimRegistry, op BillOp, _ BillSource) error {
			bad := bill002TimeSource()
			bad.RootID = "alloc-no-evidence"
			bad.EvidenceRef = ""
			_, err := r.RegisterSource(op, bad)
			return err
		}, ErrBillSourceValidation},
		{"missing rate blocks register", func(t *testing.T, r *SourceClaimRegistry, op BillOp, _ BillSource) error {
			bad := bill002TimeSource()
			bad.RootID = "alloc-no-rate"
			bad.RateRef = ""
			_, err := r.RegisterSource(op, bad)
			return err
		}, ErrBillSourceValidation},
		{"stale revision blocks claim", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			_, err := r.Claim(op, BillClaimInput{FamilyID: s.FamilyID, Revision: 2, Qty: "10", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}, "conf-stale")
			return err
		}, ErrBillSourceStale},
		{"cross-tenant blocks claim", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			foreign := op
			foreign.TenantID = "tenant-z"
			_, err := r.Claim(foreign, claimOf(s.FamilyID), "conf-tenant")
			return err
		}, ErrBillSourceDenied},
		{"denied role blocks claim", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			denied := op
			denied.BillingAllowed = false
			_, err := r.Claim(denied, claimOf(s.FamilyID), "conf-role")
			return err
		}, ErrBillSourceDenied},
		{"shortfall blocks over-claim", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			_, err := r.Claim(op, BillClaimInput{FamilyID: s.FamilyID, Revision: 1, Qty: "121", PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue}, "conf-short")
			return err
		}, ErrBillSourceShortfall},
		{"key reuse with new payload conflicts", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			if _, err := r.Claim(op, claimOf(s.FamilyID), "conf-key"); err != nil {
				return err
			}
			bigger := claimOf(s.FamilyID)
			bigger.Qty = "60"
			_, err := r.Claim(op, bigger, "conf-key")
			return err
		}, ErrBillSourceConflict},
		{"expired hold cannot issue", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			holdInput := claimOf(s.FamilyID)
			holdInput.Kind = BillClaimHold
			holdInput.LeaseMinutes = 1
			hold, err := r.Claim(op, holdInput, "conf-hold")
			if err != nil {
				return err
			}
			r.SweepExpired(now.Add(2 * time.Minute))
			_, err = r.CommitHold(op, hold.ClaimID, "conf-commit")
			return err
		}, ErrBillSourceState},
		{"issued allocation cannot silently release", func(t *testing.T, r *SourceClaimRegistry, op BillOp, s BillSource) error {
			issued, err := r.Claim(op, claimOf(s.FamilyID), "conf-issued")
			if err != nil {
				return err
			}
			_, err = r.ReleaseClaim(op, issued.ClaimID, "conf-release")
			return err
		}, ErrBillSourceState},
		{"unknown key never fabricates success", func(t *testing.T, r *SourceClaimRegistry, op BillOp, _ BillSource) error {
			_, err := r.ClaimStatus(op, "conf-missing")
			return err
		}, ErrBillSourceDenied},
	}
	for _, row := range rows {
		registry, op, source := setup(t)
		if err := row.run(t, registry, op, source); !errors.Is(err, row.want) {
			t.Errorf("%s: err = %v, want %v", row.name, err, row.want)
		}
	}
}

// TestTodo_SMB_BILL_002_Mutation pins the two oracles proven by real
// flip-and-revert mutation runs during development (recorded in the issue
// evidence): the tenant-isolation check and the family shortfall guard. Each
// subtest fails if its guard stops refusing.
func TestTodo_SMB_BILL_002_Mutation(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill002Registry(now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	source, err := registry.RegisterSource(op, bill002TimeSource())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("tenant isolation oracle", func(t *testing.T) {
		foreign := op
		foreign.TenantID = "tenant-mutant"
		if _, err := registry.Claim(foreign, BillClaimInput{
			FamilyID: source.FamilyID, Revision: 1, Qty: "60",
			PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
		}, "mut-tenant"); !errors.Is(err, ErrBillSourceDenied) {
			t.Fatalf("mutant survived: cross-tenant claim err = %v", err)
		}
	})
	t.Run("shortfall guard oracle", func(t *testing.T) {
		if _, err := registry.Claim(op, BillClaimInput{
			FamilyID: source.FamilyID, Revision: 1, Qty: "150",
			PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
		}, "mut-short"); !errors.Is(err, ErrBillSourceShortfall) {
			t.Fatalf("mutant survived: over-claim err = %v", err)
		}
	})
}
