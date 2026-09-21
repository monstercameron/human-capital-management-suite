package payroll

import (
	"errors"
	"testing"
	"time"
)

func payRunFixture(t *testing.T) PayRunInput {
	t.Helper()
	return PayRunInput{
		Wage:             wageInput(t),
		PeriodStart:      time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
		PeriodEnd:        time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		PreTaxDeductions: taxMoney(t, "100.00"),
		TaxProfileRef:    "profile-1",
		RegistrationRef:  "registration-1",
		ElectionRef:      "election-1",
		WageBasisRef:     "wagebasis-1",
		Pack:             taxPackFixture(t, "2026-v1"),
		PriorAccumulator: taxMoney(t, "0.00"),
		Filing: FilingTerms{
			ReportDefinition: "payroll-period-close",
			ReportVersion:    "2026-v1",
			SchemaVersion:    "filing-schema/v1",
			PeriodRef:        "2026-06-A",
			Jurisdiction:     "US-CA",
			SourceWatermark:  "payroll.ledger.stream_head@900",
			SignerRef:        "signer:payroll-ops-1",
			ApprovalRef:      "approval:payroll-release-7",
			IdempotencyKey:   "payrun-acme-worker-1-2026-06-A",
		},
	}
}

// TestTodo_REV_021_02 is the PRIMARY claim: one worker's period runs
// EvaluateWages, then CalculateTax on the wage result's gross, then
// BuildFilingPackage on both digests. The tax input derives from the wage
// result (taxable wages equal gross minus pre-tax deductions) and the filing
// package's source digest recomputes from exactly the wage and tax digests.
func TestTodo_REV_021_02(t *testing.T) {
	got, err := RunPayrollPeriod(payRunFixture(t))
	if err != nil {
		t.Fatalf("RunPayrollPeriod: %v", err)
	}
	if got.Wage.Status != WageOK {
		t.Fatalf("wage status = %s, want OK", got.Wage.Status)
	}
	if got.Wage.GrossPay.String() != "1100.00" {
		t.Fatalf("gross pay = %s, want 1100.00", got.Wage.GrossPay)
	}
	if got.Tax.TaxableWages.String() != "1000.00" {
		t.Fatalf("taxable wages = %s, want 1000.00 (gross 1100.00 minus 100.00 pre-tax)", got.Tax.TaxableWages)
	}
	if got.Tax.TaxBase.String() != "1000.00" {
		t.Fatalf("tax base = %s, want 1000.00", got.Tax.TaxBase)
	}
	if got.Tax.EmployeeTax.String() != "76.50" || got.Tax.EmployerTax.String() != "76.50" {
		t.Fatalf("employee/employer tax = %s/%s, want 76.50/76.50", got.Tax.EmployeeTax, got.Tax.EmployerTax)
	}
	if got.Tax.RuleVersion != "2026-v1" {
		t.Fatalf("tax rule version = %q, want the pinned pack version", got.Tax.RuleVersion)
	}
	wantSource := SourceDigest(got.Wage.Digest, got.Tax.Digest)
	if got.SourceValueDigest != wantSource {
		t.Fatalf("source digest = %q, want recomputation %q", got.SourceValueDigest, wantSource)
	}
	if got.Package.SourceValueDigest != wantSource {
		t.Fatalf("package source digest = %q, want %q", got.Package.SourceValueDigest, wantSource)
	}
	if got.Package.Digest == "" {
		t.Fatal("filing package must carry a digest")
	}
	if got.Package.Tenant != "acme" || got.Package.PeriodRef != "2026-06-A" {
		t.Fatalf("package identity = %+v, want tenant acme period 2026-06-A", got.Package)
	}
}

// TestTodo_REV_021_02_Integration runs the chain for a second worker and
// period and proves refusals fail the whole run: unapproved time never
// produces a tax result or a filing package.
func TestTodo_REV_021_02_Integration(t *testing.T) {
	input := payRunFixture(t)
	input.Wage.WorkerRef = "worker-2"
	input.Wage.ApprovedBy = "manager-9"
	input.PeriodStart = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	input.PeriodEnd = time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	input.Filing.PeriodRef = "2026-06-B"
	input.Filing.IdempotencyKey = "payrun-acme-worker-2-2026-06-B"
	first, err := RunPayrollPeriod(input)
	if err != nil {
		t.Fatalf("RunPayrollPeriod: %v", err)
	}
	second, err := RunPayrollPeriod(input)
	if err != nil {
		t.Fatalf("RunPayrollPeriod: %v", err)
	}
	if first.Package.Digest != second.Package.Digest {
		t.Fatal("identical period inputs must seal identical packages")
	}

	refused := payRunFixture(t)
	refused.Wage.Approved = false
	if _, err := RunPayrollPeriod(refused); !errors.Is(err, ErrPayRun) {
		t.Fatalf("unapproved time must fail the run, got %v", err)
	}

	missing := payRunFixture(t)
	missing.TaxProfileRef = ""
	if _, err := RunPayrollPeriod(missing); !errors.Is(err, ErrPayRun) {
		t.Fatalf("missing tax profile must fail the run, got %v", err)
	}

}
