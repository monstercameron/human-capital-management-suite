package privacy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/hipaa"
)

// HIPAAClockDecision is one recipient's clock in a HIPAA breach decision.
// State media decisions remain jurisdiction-scoped; no individual identifiers
// enter this certificate.
type HIPAAClockDecision struct {
	Recipient    hipaa.BreachRecipient `json:"recipient"`
	Jurisdiction string                `json:"jurisdiction,omitempty"`
	Decision     NotifyDecision        `json:"decision"`
	Consumers    int64                 `json:"consumers"`
	Threshold    int64                 `json:"threshold"`
	DeadlineDays int                   `json:"deadline_days"`
	DeadlineFrom string                `json:"deadline_from"`
	Authority    string                `json:"authority"`
	Reason       string                `json:"reason"`
}

func compareHIPAAClockDecision(a, b HIPAAClockDecision) int {
	if a.Recipient != b.Recipient {
		return strings.Compare(string(a.Recipient), string(b.Recipient))
	}
	return strings.Compare(a.Jurisdiction, b.Jurisdiction)
}

// NewNotificationMatrixWithHIPAA pins the independently versioned HIPAA
// clock table to the PRIV-009 matrix version. Its statutory recipient clocks
// are evaluated alongside the existing federal, state, GLBA, and contract
// duties when an incident is HIPAA-applicable.
func NewNotificationMatrixWithHIPAA(version string, rules []MatrixRule, extension hipaa.BreachMatrixExtension) (NotificationMatrix, error) {
	matrix, err := NewNotificationMatrix(version, rules)
	if err != nil {
		return NotificationMatrix{}, err
	}
	if err := extension.Validate(); err != nil {
		return NotificationMatrix{}, fmt.Errorf("%w: HIPAA extension: %v", ErrBreachBlocked, err)
	}
	if extension.PRIV009MatrixVersion != version {
		return NotificationMatrix{}, fmt.Errorf("%w: HIPAA extension is pinned to %q, matrix is %q", ErrBreachBlocked, extension.PRIV009MatrixVersion, version)
	}
	clone := extension
	clone.Rules = append([]hipaa.BreachClock(nil), extension.Rules...)
	matrix.HIPAA = &clone
	return matrix, nil
}

func hipaaClockRule(extension hipaa.BreachMatrixExtension, recipient hipaa.BreachRecipient, match func(hipaa.BreachClock) bool) (hipaa.BreachClock, bool) {
	for _, rule := range extension.Rules {
		if rule.Recipient == recipient && match(rule) {
			return rule, true
		}
	}
	return hipaa.BreachClock{}, false
}

func decideHIPAAClocks(incident BreachIncident, extension hipaa.BreachMatrixExtension) []HIPAAClockDecision {
	count := incident.consumerCount()
	individualRule, _ := hipaaClockRule(extension, hipaa.RecipientIndividuals, func(r hipaa.BreachClock) bool { return true })
	clocks := []HIPAAClockDecision{{
		Recipient: hipaa.RecipientIndividuals, Decision: DecisionNotify,
		Consumers: count, Threshold: individualRule.Threshold, DeadlineDays: individualRule.DeadlineDays,
		DeadlineFrom: individualRule.DeadlineFrom, Authority: individualRule.Authority,
		Reason: "notice is required for each affected individual with unsecured PHI",
	}}

	mediaRule, _ := hipaaClockRule(extension, hipaa.RecipientMedia, func(r hipaa.BreachClock) bool { return true })
	for _, population := range incident.Populations {
		decision, reason := DecisionNoNotify, "jurisdiction population does not exceed the HIPAA media threshold"
		if population.Consumers > mediaRule.Threshold {
			decision, reason = DecisionNotify, "jurisdiction population exceeds the HIPAA media threshold"
		}
		clocks = append(clocks, HIPAAClockDecision{
			Recipient: hipaa.RecipientMedia, Jurisdiction: population.Jurisdiction.String(),
			Decision: decision, Consumers: population.Consumers, Threshold: mediaRule.Threshold,
			DeadlineDays: mediaRule.DeadlineDays, DeadlineFrom: mediaRule.DeadlineFrom,
			Authority: mediaRule.Authority, Reason: reason,
		})
	}

	secretaryRule, _ := hipaaClockRule(extension, hipaa.RecipientSecretary, func(r hipaa.BreachClock) bool {
		return (count >= 500) == strings.Contains(r.DeadlineFrom, "contemporaneous")
	})
	secretaryReason := "fewer than 500 affected individuals; report to HHS within 60 days after calendar year end"
	if count >= 500 {
		secretaryReason = "500 or more affected individuals; report to HHS with individual notice"
	}
	clocks = append(clocks, HIPAAClockDecision{
		Recipient: hipaa.RecipientSecretary, Decision: DecisionNotify,
		Consumers: count, Threshold: secretaryRule.Threshold, DeadlineDays: secretaryRule.DeadlineDays,
		DeadlineFrom: secretaryRule.DeadlineFrom, Authority: secretaryRule.Authority, Reason: secretaryReason,
	})

	coveredEntityRule, _ := hipaaClockRule(extension, hipaa.RecipientCoveredEntity, func(r hipaa.BreachClock) bool { return true })
	baDecision, baReason := DecisionNoNotify, "covered-entity role; business-associate notice clock does not apply"
	if incident.HIPAAEntityRole == hipaa.EntityRoleBusinessAssociate || incident.HIPAAEntityRole == hipaa.EntityRoleBusinessAssociateSubcontractor {
		baDecision, baReason = DecisionNotify, "business associate must notify its covered entity without unreasonable delay"
	}
	clocks = append(clocks, HIPAAClockDecision{
		Recipient: hipaa.RecipientCoveredEntity, Decision: baDecision,
		Consumers: count, Threshold: coveredEntityRule.Threshold, DeadlineDays: coveredEntityRule.DeadlineDays,
		DeadlineFrom: coveredEntityRule.DeadlineFrom, Authority: coveredEntityRule.Authority, Reason: baReason,
	})
	sort.Slice(clocks, func(i, j int) bool { return compareHIPAAClockDecision(clocks[i], clocks[j]) < 0 })
	return clocks
}

func validateHIPAAClockDecisions(version string, clocks []HIPAAClockDecision) error {
	if version != hipaa.BreachMatrixVersion || len(clocks) < 4 {
		return fmt.Errorf("%w: HIPAA decision has no current clock matrix or recipient results", ErrBreachBlocked)
	}
	seen := make(map[string]bool, len(clocks))
	for i, clock := range clocks {
		key := string(clock.Recipient) + "|" + clock.Jurisdiction
		if clock.Recipient != hipaa.RecipientIndividuals && clock.Recipient != hipaa.RecipientMedia && clock.Recipient != hipaa.RecipientSecretary && clock.Recipient != hipaa.RecipientCoveredEntity {
			return fmt.Errorf("%w: HIPAA clock %d has undeclared recipient", ErrBreachBlocked, i)
		}
		if seen[key] || clock.Decision != DecisionNotify && clock.Decision != DecisionNoNotify || clock.Consumers < 1 || clock.Threshold < 1 || clock.DeadlineDays < 1 || strings.TrimSpace(clock.DeadlineFrom) == "" || strings.TrimSpace(clock.Authority) == "" || strings.TrimSpace(clock.Reason) == "" {
			return fmt.Errorf("%w: HIPAA clock %d is incomplete or duplicated", ErrBreachBlocked, i)
		}
		if clock.Recipient == hipaa.RecipientMedia && strings.TrimSpace(clock.Jurisdiction) == "" || clock.Recipient != hipaa.RecipientMedia && clock.Jurisdiction != "" {
			return fmt.Errorf("%w: HIPAA clock %d has invalid jurisdiction scope", ErrBreachBlocked, i)
		}
		if i > 0 && compareHIPAAClockDecision(clocks[i-1], clock) >= 0 {
			return fmt.Errorf("%w: HIPAA clocks are not in canonical order", ErrBreachBlocked)
		}
		seen[key] = true
	}
	for _, recipient := range []hipaa.BreachRecipient{hipaa.RecipientIndividuals, hipaa.RecipientSecretary, hipaa.RecipientCoveredEntity} {
		if !seen[string(recipient)+"|"] {
			return fmt.Errorf("%w: HIPAA clock for %s is missing", ErrBreachBlocked, recipient)
		}
	}
	return nil
}
