package garnishment

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the pure garnishment-limit contract exposed by
// the shipped application cell. It grants no authority and performs no writes.
const ServingContractID = "hcmnext.conformance.garnishment-limit/v1"

// ValidateServingContract exercises the calculation through the contract a
// serving cell links. It uses only ephemeral values and requires provenance in
// the bounded trace; missing rule evidence must remain blocked.
func ValidateServingContract() error {
	in := EarningsInput{
		Tenant: "tenant-serving", WorkerRef: "worker-serving", OrderRef: "order-serving",
		OrderType: OrderCreditor, Jurisdiction: "US-CA",
		RulePackRef: "rulepack:garnishment", RulePackVersion: "v1", RulePackDigest: "sha256:serving",
		Gross: values.MustDecimal("1000.00", 2, values.RoundingHalfUp),
		Deductions: []MandatoryDeduction{{
			Kind: "FEDERAL_TAX", Amount: values.MustDecimal("100.00", 2, values.RoundingHalfUp),
			BasisRef: "statute:serving",
		}},
		LimitBps: 2500, ProtectedFloor: values.MustDecimal("100.00", 2, values.RoundingHalfUp),
		OrderedAmount: values.MustDecimal("300.00", 2, values.RoundingHalfUp),
	}
	result, err := ComputeWithholdingLimit(in)
	if err != nil {
		return fmt.Errorf("garnishment: serving calculation: %w", err)
	}
	if result.Disposable.String() != "900.00" || result.MaxWithholding.String() != "225.00" || result.OrderedCapped.String() != "225.00" {
		return fmt.Errorf("garnishment: serving calculation produced %+v", result)
	}
	if result.CanonicalDigest == "" || len(result.Trace) < 3 {
		return fmt.Errorf("garnishment: serving calculation is not sealed with a trace")
	}
	trace := strings.Join(result.Trace, "\n")
	if !strings.Contains(trace, in.Jurisdiction) || !strings.Contains(trace, in.RulePackRef) || !strings.Contains(trace, in.RulePackDigest) {
		return fmt.Errorf("garnishment: serving trace lost jurisdiction or rule-pack provenance")
	}
	in.RulePackDigest = ""
	if _, err := ComputeWithholdingLimit(in); err == nil {
		return fmt.Errorf("garnishment: serving calculation accepted a missing rule-pack digest")
	}
	return nil
}
