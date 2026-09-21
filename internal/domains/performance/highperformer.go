package performance

import (
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// topBandMinimum is the lowest calibrated final rating that counts as the
// top performance band (HIPERF-004). Ratings are decimals on the five-point
// performance scale, so 4.50 is the published high-performer bar: a
// calibrated 4.50 routes to the high-performer variant, a 4.49 stays on the
// execute plan. The comparison is numeric, never scale-textual, so a 4.5 at
// any decimal scale is the same rating.
var topBandMinimum = values.MustDecimal("4.50", 2, values.RoundingHalfEven)

// IsTopBandRating reports whether r reaches the top performance band. An
// invalid rating is never top-band: a rating nobody validated routes
// nowhere.
func IsTopBandRating(r values.Decimal) bool {
	if r.Validate() != nil {
		return false
	}
	return r.Cmp(topBandMinimum) >= 0
}

// IsHighPerformer reports whether a calibrated rating routes its subject to
// the high-performer promotion variant: a valid calibration whose final
// rating reaches the top band. Anything else — unvalidated, forged,
// lower — is false, so plan resolution defaults to the execute plan.
func IsHighPerformer(r FinalCalibratedRating) bool {
	if r.Validate() != nil {
		return false
	}
	return IsTopBandRating(r.FinalRating)
}
