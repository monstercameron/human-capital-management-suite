package payinput

import (
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func checkoffDefinition(t *testing.T) Definition {
	t.Helper()
	d, err := NewDefinition(Definition{DefinitionID: "union-dues", Code: "UNION_DUES", Kind: KindDeduction, Currency: "USD", Taxability: map[JurisdictionClass]bool{JurisdictionFederal: false}, CalculationBasis: BasisFlatAmount, Limits: DecimalLimits{Minimum: payInputDecimal(t, "1.00"), Maximum: payInputDecimal(t, "75.00")}, AccountingCode: "UNION_DUES", OwnerRef: "cba-checkoff", Effective: payInputInterval(t, "2026-01-01", "2027-01-01"), Version: "v1", State: StatePublished})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func checkoffSource(t *testing.T) CheckoffDeductionSource {
	t.Helper()
	return CheckoffDeductionSource{State: CheckoffAuthorizationActive, TenantID: "tenant-1", WorkerRef: "worker-1", MembershipID: "membership-1", AuthorizationDigest: "sha256:authorization", UnionRef: "union-local-7", PeriodID: "2026-09", Amount: payInputDecimal(t, "25.00"), Effective: payInputInterval(t, "2026-09-01", "2026-10-01")}
}
func checkoffDeductions(t *testing.T, count int) []CheckoffDeduction {
	t.Helper()
	d := checkoffDefinition(t)
	out := make([]CheckoffDeduction, 0, count)
	for i := 0; i < count; i++ {
		s := checkoffSource(t)
		s.WorkerRef = fmt.Sprintf("worker-%d", i+1)
		s.MembershipID = fmt.Sprintf("membership-%d", i+1)
		s.AuthorizationDigest = fmt.Sprintf("sha256:authorization-%d", i+1)
		got, err := NewCheckoffDeduction(d, s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, got)
	}
	return out
}

func TestTodo_REV_052_02(t *testing.T) {
	d, err := NewCheckoffDeduction(checkoffDefinition(t), checkoffSource(t))
	if err != nil {
		t.Fatal(err)
	}
	if d.Assignment.WorkerRef != "worker-1" || d.MembershipID != "membership-1" || d.AuthorizationDigest != "sha256:authorization" || d.Assignment.Amount.String() != "25.00" {
		t.Fatalf("assignment lost authorization lineage or amount: %+v", d)
	}
	if d.Assignment.AssignmentID == "" || d.Assignment.DefinitionRef.ID != "union-dues" {
		t.Fatalf("assignment not bound: %+v", d)
	}
	if digest, err := d.Digest(); err != nil || digest == "" {
		t.Fatalf("deduction lineage digest=%q err=%v", digest, err)
	}
	deductions := checkoffDeductions(t, 5)
	o, err := NewRemittanceObligation("remit-1", deductions)
	if err != nil {
		t.Fatal(err)
	}
	if o.Amount.String() != "125.00" || len(o.DeductionDigests) != 5 {
		t.Fatalf("remittance did not aggregate source deductions: %+v", o)
	}
	if o.State != RemittanceOpen {
		t.Fatalf("new obligation state=%s", o.State)
	}
	closed, err := o.Reconcile(payInputDecimal(t, "125.00"), "bank-transfer-1", "statement:sept")
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != RemittanceReconciled || closed.RemittanceRef == "" {
		t.Fatalf("not independently reconciled: %+v", closed)
	}
}

func TestTodo_REV_052_02_Property(t *testing.T) {
	definition := checkoffDefinition(t)
	for _, amount := range []string{"1.00", "4.25", "25.00", "75.00"} {
		s := checkoffSource(t)
		s.Amount = payInputDecimal(t, amount)
		got, err := NewCheckoffDeduction(definition, s)
		if err != nil {
			t.Fatalf("bounded amount %s rejected: %v", amount, err)
		}
		if got.Assignment.Amount.String() != amount {
			t.Fatalf("amount=%s want=%s", got.Assignment.Amount, amount)
		}
	}
	for _, state := range []CheckoffAuthorizationState{"REVOKED", "AGENCY_FEE_ONLY", "PROHIBITED_BY_JURISDICTION", "UNKNOWN", CheckoffAuthorizationActive} {
		s := checkoffSource(t)
		s.State = state
		if state == CheckoffAuthorizationActive {
			s.AuthorizationDigest = ""
		}
		if _, err := NewCheckoffDeduction(definition, s); !errors.Is(err, ErrInvalidCheckoff) {
			t.Fatalf("state %s/invalid source error=%v", state, err)
		}
	}
	bad := checkoffSource(t)
	bad.Amount = payInputDecimal(t, "75.01")
	if _, err := NewCheckoffDeduction(definition, bad); err == nil {
		t.Fatal("unbounded deduction accepted")
	}
	bad = checkoffSource(t)
	bad.TenantID = ""
	if _, err := NewCheckoffDeduction(definition, bad); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("tenantless deduction error=%v", err)
	}
}

func TestTodo_REV_052_02_Golden(t *testing.T) {
	o, err := NewRemittanceObligation("remit-1", checkoffDeductions(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := o.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:024214f034a7f162768c8aabef1d5ddd694c3dc1b7a8562bfa202fe520558b20"
	if digest != want {
		t.Fatalf("remittance digest=%s want=%s", digest, want)
	}
}

func TestTodo_REV_052_02_Conformance(t *testing.T) {
	deductions := checkoffDeductions(t, 5)
	o, err := NewRemittanceObligation("remit-1", deductions)
	if err != nil {
		t.Fatal(err)
	}
	// Payroll settlement is intentionally not a reconciliation input.
	if _, err := o.Reconcile(payInputDecimal(t, "124.99"), "settlement:payroll", "evidence:payroll"); err == nil {
		t.Fatal("payroll settlement shortfall reconciled union remittance")
	}
	if o.State != RemittanceOpen {
		t.Fatalf("failed reconcile mutated source: %+v", o)
	}
	if _, err := NewRemittanceObligation("duplicate-source", append(deductions, deductions[0])); err == nil {
		t.Fatal("duplicate deduction inflated remittance amount")
	}
	mismatched := append([]CheckoffDeduction(nil), deductions...)
	mismatched[0].UnionRef = "other-union"
	if _, err := NewRemittanceObligation("mixed-union", mismatched); err == nil {
		t.Fatal("mixed union deductions entered one obligation")
	}
	mismatched = append([]CheckoffDeduction(nil), deductions...)
	mismatched[0].TenantID = "tenant-2"
	if _, err := NewRemittanceObligation("mixed-tenant", mismatched); err == nil {
		t.Fatal("cross-tenant deductions entered one obligation")
	}
}

func TestTodo_REV_052_02_Security(t *testing.T) {
	d := checkoffDefinition(t)
	for _, state := range []CheckoffAuthorizationState{"REVOKED", "AGENCY_FEE_ONLY", "PROHIBITED_BY_JURISDICTION", "UNKNOWN"} {
		s := checkoffSource(t)
		s.State = state
		if _, err := NewCheckoffDeduction(d, s); !errors.Is(err, ErrInvalidCheckoff) {
			t.Fatalf("state %s deduction error=%v", state, err)
		}
	}
	o, err := NewRemittanceObligation("remit-1", checkoffDeductions(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ amount, reference, evidence string }{{"124.00", "bank", "evidence"}, {"125.00", "", "evidence"}, {"125.00", "bank", ""}} {
		if _, err := o.Reconcile(payInputDecimal(t, tc.amount), tc.reference, tc.evidence); err == nil {
			t.Fatalf("incomplete reconciliation accepted: %+v", tc)
		}
	}
	if _, err := o.Reconcile(values.Decimal{}, "bank", "evidence"); err == nil {
		t.Fatal("invalid amount accepted")
	}
}
