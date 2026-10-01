package timeprofile

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the timeprofile golden files")

// TestTodo_WTIME_001 proves the GREEN behavior directly against the three
// RED scenarios named in the todo: a salaried non-exempt worker collapsed
// into exempt, a contractor inheriting an employee-shaped profile, and an
// assignment whose overtime aggregation key is left implicit.
func TestTodo_WTIME_001(t *testing.T) {
	t.Run("salaried non-exempt requires a salary pay basis", func(t *testing.T) {
		p := baseProfile(t)
		p.Exemption = SalariedNonExempt
		p.PayBasis = PayHourly // the collapsed bug: hourly pay with the salaried-non-exempt label
		err := p.Validate()
		if !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected ErrInvalidProfile, got %v", err)
		}
		var rej *ProfileRejection
		if !errors.As(err, &rej) || rej.Field != "pay_basis" {
			t.Fatalf("expected a pay_basis rejection, got %+v", err)
		}
		p.PayBasis = PaySalary
		mustValid(t, p)
	})

	t.Run("contractor cannot carry an employee exemption or reach payroll", func(t *testing.T) {
		p := baseProfile(t)
		p.Category = CategoryContractor
		p.PayBasis = PayContract
		// Still shaped like the department's employee profile: exemption
		// and destination were never adjusted for the new category.
		err := p.Validate()
		if !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected ErrInvalidProfile for an inherited employee profile, got %v", err)
		}
		p.Exemption = NotApplicable
		p.Destination = DestinationInvoice
		mustValid(t, p)
	})

	t.Run("the overtime aggregation key must be explicit", func(t *testing.T) {
		p := baseProfile(t)
		p.AggregationKey = ""
		err := p.Validate()
		var rej *ProfileRejection
		if !errors.As(err, &rej) || rej.Field != "aggregation_key" {
			t.Fatalf("expected an aggregation_key rejection, got %+v", err)
		}
	})

	t.Run("an assignment with no resolvable profile records no time", func(t *testing.T) {
		// This is proven at the Resolve layer (TestTodo_WTIME_002); recorded
		// here so the todo's full RED sentence has one place that names
		// both halves of its proof.
		_, err := Resolve(nil, AssignmentFacts{TenantRef: "acme-co", Category: CategoryEmployee}, instant(t, "2026-02-01T00:00:00Z"))
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})
}

// TestTodo_WTIME_001_Golden pins Explain() for a representative profile per
// category so a change to Validate, Digest or TemplateFor that reaches the
// explanation shows as a diff against a committed file, not a silent
// behavior change nobody had to approve.
func TestTodo_WTIME_001_Golden(t *testing.T) {
	profiles := []struct {
		name    string
		profile TimeProfile
	}{
		{"hourly_employee_punch", baseProfile(t)},
		{"salaried_exempt_duration", func() TimeProfile {
			p := baseProfile(t)
			p.Capture = CaptureDuration
			p.Exemption = Exempt
			p.PayBasis = PaySalary
			p.OvertimeMethod = OvertimeNone
			return p
		}()},
		{"contractor_invoice", func() TimeProfile {
			p := baseProfile(t)
			p.Category = CategoryContractor
			p.PayBasis = PayContract
			p.Exemption = NotApplicable
			p.Destination = DestinationInvoice
			p.OvertimeMethod = OvertimeNone
			return p
		}()},
		{"minor_employee_punch", func() TimeProfile {
			p := baseProfile(t)
			p.MinorAgeBand = MinorAgeBand16To17
			p.MinorPermitVerified = true
			return p
		}()},
		{"eu_daily_recording_duration", func() TimeProfile {
			p := baseProfile(t)
			p.Capture = CaptureDuration
			p.Exemption = Exempt
			p.PayBasis = PaySalary
			p.OvertimeMethod = OvertimeNone
			p.DailyRecordingDutyRequired = true
			p.RestPeriodDutyRequired = true
			return p
		}()},
	}

	var b strings.Builder
	for _, tc := range profiles {
		exp, err := tc.profile.Explain()
		if err != nil {
			t.Fatalf("%s: Explain: %v", tc.name, err)
		}
		fmt.Fprintf(&b, "%s:\n", tc.name)
		fmt.Fprintf(&b, "  category:    %s\n", exp.Category)
		fmt.Fprintf(&b, "  capture:     %s\n", exp.Capture)
		fmt.Fprintf(&b, "  exemption:   %s\n", exp.Exemption)
		fmt.Fprintf(&b, "  destination: %s\n", exp.Destination)
		fmt.Fprintf(&b, "  template:    %s\n", exp.ResolvedTemplate)
		fmt.Fprintf(&b, "  digest:      %s\n", exp.Digest)
	}
	assertGolden(t, "profile-explain.txt", b.String())
}

// TestTodo_WTIME_001_Property generates every combination of category,
// capture mode and exemption status and checks that Validate's verdict
// matches an independent reference computation of the same cross-field law,
// rather than checking a handful of hand-picked cases.
func TestTodo_WTIME_001_Property(t *testing.T) {
	categories := []WorkerCategory{CategoryEmployee, CategoryContractor, CategoryAgencyTemp, CategoryPlatform}
	captures := []CaptureMode{CapturePunch, CaptureDuration, CaptureException, CaptureNone}
	exemptions := []ExemptionStatus{NonExempt, Exempt, SalariedNonExempt, NotApplicable}

	cases := 0
	for _, category := range categories {
		for _, capture := range captures {
			for _, exemption := range exemptions {
				cases++
				p := baseProfile(t)
				p.Category = category
				p.Capture = capture
				p.Exemption = exemption
				if category == CategoryContractor {
					p.Destination = DestinationInvoice
					p.PayBasis = PayContract
				} else if category == CategoryAgencyTemp {
					p.Destination = DestinationAgency
					p.PayBasis = PayHourly
				}
				if exemption == SalariedNonExempt {
					p.PayBasis = PaySalary
				}

				err := p.Validate()
				wantErr := referenceValidate(category, capture, exemption)
				if wantErr && err == nil {
					t.Fatalf("category=%s capture=%s exemption=%s: expected rejection, Validate accepted", category, capture, exemption)
				}
				if !wantErr && err != nil {
					t.Fatalf("category=%s capture=%s exemption=%s: expected acceptance, got %v", category, capture, exemption, err)
				}
			}
		}
	}
	if cases != len(categories)*len(captures)*len(exemptions) {
		t.Fatalf("generated %d cases, expected the full cross product", cases)
	}
}

// referenceValidate restates WTIME-001's cross-field law independently of
// Validate's implementation, so the property test is not just Validate
// checking itself.
func referenceValidate(category WorkerCategory, capture CaptureMode, exemption ExemptionStatus) (rejects bool) {
	isContractorLike := category == CategoryContractor || category == CategoryAgencyTemp
	if isContractorLike && exemption != NotApplicable {
		return true
	}
	if !isContractorLike && exemption == NotApplicable {
		return true
	}
	nonExempt := exemption == NonExempt || exemption == SalariedNonExempt
	if capture == CaptureException && nonExempt {
		return true
	}
	if capture == CaptureNone && nonExempt {
		return true
	}
	return false
}

// TestTodo_WTIME_001_Security proves a forged profile is rejected rather
// than silently accepted: an unknown enum token spelled to look canonical,
// and a category/destination pair a caller could try to use to route
// contractor time into payroll.
func TestTodo_WTIME_001_Security(t *testing.T) {
	t.Run("an unknown exemption token is rejected, not coerced", func(t *testing.T) {
		p := baseProfile(t)
		p.Exemption = ExemptionStatus("EXEMPT_") // looks canonical, is not declared
		if err := p.Validate(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected the forged exemption token to be rejected, got %v", err)
		}
	})

	t.Run("a contractor profile can never resolve to payroll", func(t *testing.T) {
		p := baseProfile(t)
		p.Category = CategoryContractor
		p.PayBasis = PayContract
		p.Exemption = NotApplicable
		p.Destination = DestinationPayroll // forged: try to route contractor pay through payroll
		if err := p.Validate(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected PAYROLL destination on a contractor profile to be rejected, got %v", err)
		}
	})

	t.Run("an agency-temp profile cannot borrow the invoice destination", func(t *testing.T) {
		p := baseProfile(t)
		p.Category = CategoryAgencyTemp
		p.Exemption = NotApplicable
		p.Destination = DestinationInvoice // wrong destination for this category
		if err := p.Validate(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected the mismatched destination to be rejected, got %v", err)
		}
	})

	t.Run("a tampered effective window is rejected", func(t *testing.T) {
		p := baseProfile(t)
		p.EffectiveTo = p.EffectiveFrom // end not strictly after start
		if err := p.Validate(); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("expected the inverted effective window to be rejected, got %v", err)
		}
	})
}

func assertGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
