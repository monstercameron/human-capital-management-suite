package demoworkforce

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PayBandPolicyVersion pins HarborCare's illustrative salary-range policy.
// The staffed role's published base is the midpoint in every demo pay zone;
// 80% and 120% are the authored bounds, not inferred from an employee's pay.
const PayBandPolicyVersion = "harborcare.pay_bands/2026.09.1"

const (
	demoBandMinimumFactor = "0.8000"
	demoBandMaximumFactor = "1.2000"
)

// PayBandSpec is a versioned, exact-money reference for one role and zone.
// Production tenants must supply their own governed pay-band catalog.
type PayBandSpec struct {
	ID, Version, JobCode, Grade, PayZone string
	Minimum, Midpoint, Maximum           values.Money
}

// PayBandSpecs publishes the demo company's salary ranges once for all
// staffed roles and zones. A future midpoint that cannot be expressed as
// exact cents after applying the policy is rejected, never silently rounded.
func PayBandSpecs() ([]PayBandSpec, error) {
	minimumFactor, err := values.NewDecimal(demoBandMinimumFactor, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	maximumFactor, err := values.NewDecimal(demoBandMaximumFactor, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	payZones := PayZones()
	seen := make(map[string]bool)
	specs := make([]PayBandSpec, 0, len(staffing)*len(payZones))
	for _, group := range staffing {
		for _, role := range group.Roles {
			if seen[role.Code] {
				return nil, fmt.Errorf("demoworkforce: duplicate job code %q in pay-band policy", role.Code)
			}
			seen[role.Code] = true
			midpoint, err := values.NewMoney(role.BasePay, "USD", 2, values.RoundingExactRequired)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: role %s midpoint: %w", role.Code, err)
			}
			minimum, err := midpoint.MulDecimal(minimumFactor, 2, values.RoundingExactRequired)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: role %s minimum: %w", role.Code, err)
			}
			maximum, err := midpoint.MulDecimal(maximumFactor, 2, values.RoundingExactRequired)
			if err != nil {
				return nil, fmt.Errorf("demoworkforce: role %s maximum: %w", role.Code, err)
			}
			for _, zone := range payZones {
				specs = append(specs, PayBandSpec{
					ID:      "harborcare-demo.band/" + role.Code + "/" + role.Grade + "/" + zone,
					Version: PayBandPolicyVersion, JobCode: role.Code, Grade: role.Grade, PayZone: zone,
					Minimum: minimum, Midpoint: midpoint, Maximum: maximum,
				})
			}
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs, nil
}
