package productui

import (
	"sort"
	"strconv"

	operationstelemetry "github.com/monstercameron/human-capital-management-suite/internal/operations/telemetry"
)

// ComposePrivacyTelemetryPolicy projects the governed ops telemetry policy onto the privacy-telemetry admin page (WEB-235). It is the service-backed data path the page renders: every fact comes from the validated policy, never from hand-built projections.
//
// Ready is false exactly when the policy is invalid; the caller keeps the honest fallback instead of a partial page. Critical classes render sorted, mirroring the digest ordering, so markup is deterministic. EvidenceRef stays empty: decision receipts stay with the sampling decisions they evidence, and the policy view invents no receipt to fill the field.
func ComposePrivacyTelemetryPolicy(policy operationstelemetry.Policy) (PrivacyTelemetryProjection, error) {
	if err := policy.Validate(); err != nil {
		return PrivacyTelemetryProjection{}, err
	}
	classes := make([]string, 0, len(policy.CriticalFailures))
	for _, class := range policy.CriticalFailures {
		classes = append(classes, string(class))
	}
	sort.Strings(classes)
	return PrivacyTelemetryProjection{
		Ready:                  true,
		PolicyVersion:          policy.Version,
		PolicyDigest:           policy.Digest(),
		SuccessSampleRate:      strconv.FormatFloat(policy.SuccessSampleRate, 'f', -1, 64),
		MetricCardinalityLimit: policy.MetricCardinalityLimit,
		CriticalFailureClasses: classes,
	}, nil
}
