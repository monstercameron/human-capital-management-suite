package mobility

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustTaxMoney(t *testing.T, text, currency string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, currency, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("money %s %s: %v", text, currency, err)
	}
	return m
}

func mustTaxDate(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %s: %v", s, err)
	}
	return d
}

// TestTodo_WF_CAP_018 proves the PRIMARY matrix of WF-CAP-018: shadow
// payroll, hypothetical tax, equalization settlement and gross-up each exist
// as a DELEGATED contract (the tax provider computes; this side observes and
// reconciles); recurring hypothetical runs expand from a schedule trigger;
// the annual settlement reconciles against posted shadow amounts.
func TestTodo_WF_CAP_018(t *testing.T) {
	contracts := DelegatedContracts("vendor:tax-provider/acme-tax")
	if len(contracts) != 4 {
		t.Fatalf("contracts = %d, want 4 (shadow, hypothetical, equalization, gross-up)", len(contracts))
	}
	seen := map[string]bool{}
	for _, c := range contracts {
		if err := c.Validate(); err != nil {
			t.Fatalf("contract %s: %v", c.Service, err)
		}
		if !c.VendorComputes() {
			t.Fatalf("contract %s is not delegated to the vendor", c.Service)
		}
		id := TaxContractID(c.Service)
		if seen[id] || !strings.HasPrefix(id, "hcmnext.mobility.tax.") {
			t.Fatalf("contract id %q is duplicated or outside the mobility tax namespace", id)
		}
		seen[id] = true
		if c.Digest() == "" || c.Digest() != c.Digest() {
			t.Fatalf("contract %s has no stable digest", c.Service)
		}
	}

	for _, bad := range []DelegatedContract{
		{Service: "BOGUS", VendorRef: "vendor:tax-provider/acme-tax", RequestSchema: "s", ResponseSchema: "s", DataDomains: []string{"mobility.tax"}},
		{Service: TaxShadowPayroll, VendorRef: "  ", RequestSchema: "s", ResponseSchema: "s", DataDomains: []string{"mobility.tax"}},
		{Service: TaxGrossUp, VendorRef: "vendor:tax-provider/acme-tax", DataDomains: []string{"mobility.tax"}},
		{Service: TaxHypothetical, VendorRef: "vendor:tax-provider/acme-tax", RequestSchema: "s", ResponseSchema: "s"},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidTaxContract) {
			t.Fatalf("invalid contract %+v returned %v, want ErrInvalidTaxContract", bad, err)
		}
	}

	schedule := HypotheticalRunSchedule{
		AssignmentRef: "assign-1",
		Cadence:       TaxMonthly,
		Anchor:        mustTaxDate(t, "2026-01-15"),
		PeriodStart:   mustTaxDate(t, "2026-01-01"),
		PeriodEnd:     mustTaxDate(t, "2026-03-31"),
		VendorRef:     "vendor:tax-provider/acme-tax",
	}
	dues, err := schedule.DueDates()
	if err != nil {
		t.Fatalf("DueDates: %v", err)
	}
	var got []string
	for _, d := range dues {
		got = append(got, d.String())
	}
	if strings.Join(got, ",") != "2026-01-15,2026-02-15,2026-03-15" {
		t.Fatalf("monthly dues = %v", got)
	}
	next, ok := schedule.NextDue(mustTaxDate(t, "2026-01-15"))
	if !ok || next.String() != "2026-02-15" {
		t.Fatalf("NextDue = %v, %v; want 2026-02-15", next, ok)
	}
	req, err := schedule.RunRequest(mustTaxDate(t, "2026-02-15"))
	if err != nil {
		t.Fatalf("RunRequest: %v", err)
	}
	if req.IdempotencyKey != "hypotax:assign-1:2026-02-15" || req.Period != "2026-02" || req.VendorRef != schedule.VendorRef {
		t.Fatalf("run request = %+v", req)
	}
	if _, err := schedule.RunRequest(mustTaxDate(t, "2026-02-16")); !errors.Is(err, ErrTaxDateUnscheduled) {
		t.Fatalf("off-schedule run request = %v, want ErrTaxDateUnscheduled", err)
	}
	quarterly := schedule
	quarterly.Cadence = TaxQuarterly
	quarterly.PeriodEnd = mustTaxDate(t, "2026-12-31")
	qdues, err := quarterly.DueDates()
	if err != nil {
		t.Fatalf("quarterly DueDates: %v", err)
	}
	if len(qdues) != 4 || qdues[3].String() != "2026-10-15" {
		t.Fatalf("quarterly dues = %v", qdues)
	}

	statement := SettlementStatement{
		AssignmentRef: "assign-1", TaxYear: 2026, VendorRef: "vendor:tax-provider/acme-tax", StatementRef: "stmt-2026",
		Lines: []SettlementLine{
			{Jurisdiction: "DE-BE", Kind: SettlementActual, Amount: mustTaxMoney(t, "12000.00", "EUR")},
			{Jurisdiction: "US-CA", Kind: SettlementActual, Amount: mustTaxMoney(t, "8000.00", "USD")},
		},
	}
	posted := []PostedShadowAmount{
		{AssignmentRef: "assign-1", TaxYear: 2026, Jurisdiction: "DE-BE", Amount: mustTaxMoney(t, "12000.00", "EUR"), PostingRef: "pay-1"},
		{AssignmentRef: "assign-1", TaxYear: 2026, Jurisdiction: "US-CA", Amount: mustTaxMoney(t, "8000.00", "USD"), PostingRef: "pay-2"},
	}
	rec, err := ReconcileSettlement(posted, statement)
	if err != nil {
		t.Fatalf("ReconcileSettlement: %v", err)
	}
	if !rec.Balanced || len(rec.Deltas) != 0 || len(rec.Covered) != 2 {
		t.Fatalf("balanced reconciliation = %+v", rec)
	}
	posted[0].Amount = mustTaxMoney(t, "11900.00", "EUR")
	rec, err = ReconcileSettlement(posted, statement)
	if err != nil {
		t.Fatalf("ReconcileSettlement: %v", err)
	}
	if rec.Balanced || len(rec.Deltas) != 1 || rec.Deltas[0].Difference.String() != "-100.00 EUR" {
		t.Fatalf("delta reconciliation = %+v", rec)
	}
	posted[0].Amount = mustTaxMoney(t, "12000.00", "USD")
	if _, err := ReconcileSettlement(posted, statement); !errors.Is(err, ErrTaxCurrencyMismatch) {
		t.Fatalf("currency mismatch = %v, want ErrTaxCurrencyMismatch", err)
	}
	posted[0].Amount = mustTaxMoney(t, "12000.00", "EUR")
	posted[0].TaxYear = 2025
	if _, err := ReconcileSettlement(posted, statement); !errors.Is(err, ErrInvalidSettlement) {
		t.Fatalf("year mismatch = %v, want ErrInvalidSettlement", err)
	}
}
