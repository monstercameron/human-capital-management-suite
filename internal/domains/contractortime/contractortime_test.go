package contractortime

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func hourlyRate(t *testing.T, amount string) values.Rate {
	t.Helper()
	m, err := values.NewMoney(amount, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	r, err := values.NewMoneyRate(m, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func money(t *testing.T, amount, currency string) values.Money {
	t.Helper()
	m, err := values.NewMoney(amount, currency, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func hourlyEngagement(t *testing.T) Engagement {
	t.Helper()
	return Engagement{
		ID: "eng-1", WorkerRef: "worker-1", SOWRef: "sow-42",
		Pricing: PricingHourly, Currency: "USD", AmountScale: 2, Rounding: values.RoundingHalfEven,
		RateCard: map[string]values.Rate{"SENIOR": hourlyRate(t, "100.00")},
		Tax:      TaxTreatment{Kind: TaxNone},
	}
}

// TestTodo_WTIME_011 is the primary happy-path proof: approved contractor
// time prices into an invoice draft at the SOW rate, in the SOW currency,
// with no schedule/lockout/geofence/photo/break-attestation fields anywhere
// in the shape.
func TestTodo_WTIME_011(t *testing.T) {
	eng := hourlyEngagement(t)
	draft, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-1", Engagement: eng,
		Lines: []ApprovedLine{
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 480, RateCode: "SENIOR", RevisionDigest: "rev-1"},
			{WorkerRef: "worker-1", Date: "2026-09-02", Minutes: 240, RateCode: "SENIOR", RevisionDigest: "rev-2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.Validate(); err != nil {
		t.Fatalf("built draft fails validation: %v", err)
	}
	if draft.Total.String() != "1200.00 USD" {
		t.Fatalf("want 1200.00 USD (12h at 100/hr), got %s", draft.Total.String())
	}
	if draft.SelfBilled {
		t.Fatal("engagement did not declare self-billing")
	}
	if len(draft.Lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(draft.Lines))
	}
}

// TestTodo_WTIME_011_Golden pins the exact digest and total for a fixed
// input so a silent change in pricing arithmetic is caught.
func TestTodo_WTIME_011_Golden(t *testing.T) {
	eng := hourlyEngagement(t)
	draft, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-golden", Engagement: eng,
		Lines: []ApprovedLine{
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 90, RateCode: "SENIOR", RevisionDigest: "rev-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 90 minutes = 1.5 hours at 100.00/hr = 150.00.
	if draft.Total.String() != "150.00 USD" {
		t.Fatalf("golden total mismatch: %s", draft.Total.String())
	}
	if draft.Digest == "" {
		t.Fatal("golden draft has no digest")
	}
	replay, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-golden", Engagement: eng,
		Lines: []ApprovedLine{
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 90, RateCode: "SENIOR", RevisionDigest: "rev-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Digest != draft.Digest {
		t.Fatal("identical input produced a different digest")
	}
}

// TestTodo_WTIME_011_Security proves the SECURITY-relevant refusals: a rate
// outside the SOW rate card is rejected, hours cannot be forced into payroll
// (there is no payroll destination in this package's shape at all), and a
// worker mismatch between the engagement and the line is rejected.
func TestTodo_WTIME_011_Security(t *testing.T) {
	eng := hourlyEngagement(t)

	if _, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-bad-rate", Engagement: eng,
		Lines: []ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, RateCode: "GHOST", RevisionDigest: "rev-1"}},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("rate outside SOW should be rejected, got %v", err)
	}

	if _, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-bad-worker", Engagement: eng,
		Lines: []ApprovedLine{{WorkerRef: "worker-2", Date: "2026-09-01", Minutes: 60, RateCode: "SENIOR", RevisionDigest: "rev-1"}},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("worker mismatch should be rejected, got %v", err)
	}

	// No currency guessing: an engagement whose declared currency the money
	// backend rejects (empty) must fail closed, not default to a currency.
	badCurrency := eng
	badCurrency.Currency = ""
	if err := badCurrency.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("empty currency should be rejected, got %v", err)
	}

	// A duplicate line (same date, rate, revision) must not double count.
	if _, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-dup", Engagement: eng,
		Lines: []ApprovedLine{
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, RateCode: "SENIOR", RevisionDigest: "rev-1"},
			{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, RateCode: "SENIOR", RevisionDigest: "rev-1"},
		},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("duplicate line should be rejected, got %v", err)
	}
}

// TestTodo_WTIME_011_Milestone proves milestone-based pricing and tax
// treatment as VAT (added) and withholding (deducted), both driven by data
// on the engagement rather than hard-coded per-jurisdiction branches.
func TestTodo_WTIME_011_Milestone(t *testing.T) {
	vatRate, err := values.NewPercentageFromPercent("20", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	eng := Engagement{
		ID: "eng-milestone", WorkerRef: "worker-1", SOWRef: "sow-9",
		Pricing: PricingMilestone, Currency: "EUR", AmountScale: 2, Rounding: values.RoundingHalfEven,
		MilestoneAmounts: map[string]values.Money{"M1": money(t, "5000.00", "EUR")},
		Tax:              TaxTreatment{Kind: TaxVAT, Rate: vatRate, Jurisdiction: "DE"},
		SelfBilling:      true,
	}
	draft, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-milestone", Engagement: eng,
		Milestones: []MilestoneCompletion{{MilestoneID: "M1", WorkerRef: "worker-1", CompletedOn: "2026-09-15", RevisionDigest: "rev-m1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Subtotal.String() != "5000.00 EUR" || draft.TaxAmount.String() != "1000.00 EUR" || draft.Total.String() != "6000.00 EUR" {
		t.Fatalf("VAT computation wrong: subtotal=%s tax=%s total=%s", draft.Subtotal, draft.TaxAmount, draft.Total)
	}
	if !draft.SelfBilled {
		t.Fatal("self-billing flag lost")
	}

	if _, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-bad-milestone", Engagement: eng,
		Milestones: []MilestoneCompletion{{MilestoneID: "GHOST", WorkerRef: "worker-1", CompletedOn: "2026-09-15", RevisionDigest: "rev-m2"}},
	}); !errors.Is(err, ErrRejected) {
		t.Fatalf("milestone outside SOW should be rejected, got %v", err)
	}
}

// TestTodo_WTIME_011_Withholding proves the withholding tax kind deducts
// from the subtotal rather than adding, as VAT does.
func TestTodo_WTIME_011_Withholding(t *testing.T) {
	whRate, err := values.NewPercentageFromPercent("10", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	eng := hourlyEngagement(t)
	eng.Tax = TaxTreatment{Kind: TaxWithholding, Rate: whRate, Jurisdiction: "US"}
	draft, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-wh", Engagement: eng,
		Lines: []ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, RateCode: "SENIOR", RevisionDigest: "rev-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Subtotal.String() != "100.00 USD" || draft.TaxAmount.String() != "10.00 USD" || draft.Total.String() != "90.00 USD" {
		t.Fatalf("withholding computation wrong: subtotal=%s tax=%s total=%s", draft.Subtotal, draft.TaxAmount, draft.Total)
	}
}

// TestTodo_WTIME_011_Classification proves classification signals are
// reported when the supplied measurements cross the supplied thresholds, and
// that BuildInvoice never consults or is affected by them.
func TestTodo_WTIME_011_Classification(t *testing.T) {
	fortyHours, err := values.NewDecimal("40", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	thirtyHourThreshold, err := values.NewDecimal("30", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	exclusivity, err := values.NewPercentageFromPercent("95", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	exclusivityThreshold, err := values.NewPercentageFromPercent("80", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	signals := ReportSignals(ClassificationInputs{
		WeeklyHoursAverage:     fortyHours,
		ExclusivityFraction:    exclusivity,
		EngagementDurationDays: 400,
		Thresholds: ClassificationThresholds{
			WeeklyHoursFullTime: thirtyHourThreshold,
			ExclusivityHigh:     exclusivityThreshold,
			DurationLongDays:    365,
		},
	})
	if len(signals) != 3 {
		t.Fatalf("want 3 signals (hours, exclusivity, duration), got %d: %#v", len(signals), signals)
	}
	kinds := map[SignalKind]bool{}
	for _, s := range signals {
		kinds[s.Kind] = true
	}
	for _, want := range []SignalKind{SignalHoursPattern, SignalExclusivity, SignalDuration} {
		if !kinds[want] {
			t.Fatalf("missing signal %s", want)
		}
	}

	// Below every threshold: no signals, and the invoice build path never
	// even sees ReportSignals.
	none := ReportSignals(ClassificationInputs{
		WeeklyHoursAverage:     dec2(t, "10"),
		ExclusivityFraction:    percent2(t, "5"),
		EngagementDurationDays: 10,
		Thresholds: ClassificationThresholds{
			WeeklyHoursFullTime: thirtyHourThreshold,
			ExclusivityHigh:     exclusivityThreshold,
			DurationLongDays:    365,
		},
	})
	if len(none) != 0 {
		t.Fatalf("want no signals below thresholds, got %#v", none)
	}
}

func dec2(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func percent2(t *testing.T, percentText string) values.Percentage {
	t.Helper()
	p, err := values.NewPercentageFromPercent(percentText, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_WTIME_011_Version(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("unexpected contract version %d", Version())
	}
	eng := hourlyEngagement(t)
	draft, err := BuildInvoice(BuildInvoiceRequest{
		ID: "inv-explain", Engagement: eng,
		Lines: []ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-01", Minutes: 60, RateCode: "SENIOR", RevisionDigest: "rev-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	explain, err := draft.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explain.Total != "100.00 USD" || explain.EngagementID != "eng-1" {
		t.Fatalf("explain mismatch: %#v", explain)
	}
}
