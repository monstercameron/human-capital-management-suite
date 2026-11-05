package balance

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// RollingBackwardWindow derives the rolling-backward entitlement period
// (WF-CAP-005): the half-open window [asOf-monthsBack, asOf) under the
// ROLLING_BACKWARD kind. Derivation is pure calendar math on the given
// instant; no clock is read.
func RollingBackwardWindow(asOf values.Instant, monthsBack int, id, version string) (PeriodDefinition, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" {
		return PeriodDefinition{}, fmt.Errorf("%w: id and version are required", ErrPeriodInvalid)
	}
	if !asOf.IsSet() {
		return PeriodDefinition{}, fmt.Errorf("%w: as-of instant must be set", ErrPeriodInvalid)
	}
	if monthsBack < 1 || monthsBack > 1200 {
		return PeriodDefinition{}, fmt.Errorf("%w: months must be within 1..1200", ErrPeriodInvalid)
	}
	start := values.NewInstant(asOf.Time().AddDate(0, -monthsBack, 0))
	period := PeriodDefinition{ID: id, Version: version, Kind: PeriodRollingBackward, Start: start, End: asOf}
	if err := period.Validate(); err != nil {
		return PeriodDefinition{}, err
	}
	return period, nil
}
