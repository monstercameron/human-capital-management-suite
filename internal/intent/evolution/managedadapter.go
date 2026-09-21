// ManagedChecker adapts CompatibilityCheck to the registry MANAGED publish
// gate (intent.CompatibilityChecker). It lives here rather than in the
// intent package because this package already imports intent: the registry
// calls the injected checker, and this adapter supplies the real evolution
// check without creating an import cycle.
package evolution

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// ManagedChecker returns the canonical MANAGED publish checker: the real
// CompatibilityCheck verdict with deterministic evidence. Caller mistakes
// (different intent type, non-advancing version, invalid definition) come
// back as errors rather than verdicts, so the registry never mistakes them
// for an acceptance.
func ManagedChecker() intent.CompatibilityChecker {
	return func(previous, current intent.Definition) (bool, string, error) {
		report, err := CompatibilityCheck(previous, current)
		if err != nil {
			return false, "", err
		}
		return report.OK(), EvidenceSummary(report), nil
	}
}

// EvidenceSummary renders a deterministic evidence string for a
// compatibility report: the verdict plus the sorted change and violation
// codes with the fields they name. CompatibilityCheck already sorts both
// lists, so the summary is byte-stable for a given transition.
func EvidenceSummary(report Report) string {
	changes := make([]string, 0, len(report.Changes))
	for _, c := range report.Changes {
		changes = append(changes, string(c.Code)+":"+c.Field)
	}
	violations := make([]string, 0, len(report.Violations))
	for _, v := range report.Violations {
		violations = append(violations, string(v.Code)+":"+v.Field)
	}
	return fmt.Sprintf("verdict=%s changes=[%s] violations=[%s]",
		report.Verdict, strings.Join(changes, ","), strings.Join(violations, ","))
}
