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
	// Basis is the unit the three amounts are in: PayBandBasisAnnual (a
	// salary) or PayBandBasisHourly (a rate). StandardHours is the yearly
	// hours an hourly band annualizes over, and AnnualMinimum, AnnualMidpoint
	// and AnnualMaximum are the band in annual terms either way, so a rate
	// and a salary can be compared and displayed side by side.
	Basis                                        string
	StandardHours                                int
	AnnualMinimum, AnnualMidpoint, AnnualMaximum values.Money
}

// The two pay band bases.
const (
	PayBandBasisAnnual = "ANNUAL"
	PayBandBasisHourly = "HOURLY"
)

// PayBandSpecs publishes the demo company's salary ranges once for all
// staffed roles and zones. A future midpoint that cannot be expressed as
// exact cents after applying the policy is rejected, never silently rounded.
func PayBandSpecs() ([]PayBandSpec, error) { return HarborCarePack.PayBandSpecs() }

// PayBandSpecs publishes this company's pay ranges.
func (p *Pack) PayBandSpecs() ([]PayBandSpec, error) {
	minimumFactor, err := values.NewDecimal(demoBandMinimumFactor, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	maximumFactor, err := values.NewDecimal(demoBandMaximumFactor, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	payZones := p.PayZones()
	// Every published role, staffed or not: a promotion target nobody holds
	// yet still has to be priced, or the band lookup that governs the move
	// finds nothing.
	seats := p.allRoles()
	seen := make(map[string]bool, len(seats))
	specs := make([]PayBandSpec, 0, len(seats)*len(payZones))
	for _, seat := range seats {
		role := seat.Role
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
		basis, annual := PayBandBasisAnnual, [3]values.Money{minimum, midpoint, maximum}
		if p.IsHourlyJob(role.Code) {
			basis = PayBandBasisHourly
			hours := values.MustDecimal(fmt.Sprintf("%d", p.StandardHours()), 0, values.RoundingExactRequired)
			for index, amount := range annual {
				if annual[index], err = amount.MulDecimal(hours, 2, values.RoundingExactRequired); err != nil {
					return nil, fmt.Errorf("demoworkforce: role %s annualized band: %w", role.Code, err)
				}
			}
		}
		for _, zone := range payZones {
			specs = append(specs, PayBandSpec{
				ID:      p.idPrefix + ".band/" + role.Code + "/" + role.Grade + "/" + zone,
				Version: p.payBandPolicyVersion, JobCode: role.Code, Grade: role.Grade, PayZone: zone,
				Minimum: minimum, Midpoint: midpoint, Maximum: maximum,
				Basis: basis, StandardHours: p.StandardHours(),
				AnnualMinimum: annual[0], AnnualMidpoint: annual[1], AnnualMaximum: annual[2],
			})
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs, nil
}
