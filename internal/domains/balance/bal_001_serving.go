package balance

import "fmt"

// ServingContractID identifies the immutable balance contract composed by the
// shipped application cell. It grants no authority and performs no writes.
const ServingContractID = "hcmnext.conformance.balance/v1"

// ValidateAccumulatorServingContract checks the closed balance vocabularies and a
// complete definition that a serving cell must preserve. Keeping this check
// with the semantic owner makes the application composition depend on the
// production balance symbols exercised by BAL-001 through BAL-004.
func ValidateAccumulatorServingContract() error {
	definition := AccumulatorDefinition{
		ID:         "serving-balance",
		Version:    "v1",
		Name:       "Serving balance",
		Unit:       "HOURS",
		Currency:   "HOUR",
		Subject:    "WORKER",
		Period:     PeriodPayPeriod,
		Dimensions: []BalanceDimension{{Name: "worker_id", ValueType: "uuid", Required: true}},
		EntryTypes: []string{"ACCRUAL", AdjustmentEntryType},
		Authority:  Authority{SourceID: "serving-policy", Version: "v1"},
		Floor:      PolicyRule{Kind: RuleNone, Version: "v1"},
		Cap:        PolicyRule{Kind: RuleNone, Version: "v1"},
		Expiry:     PolicyRule{Kind: RuleNone, Version: "v1"},
		Rollover:   PolicyRule{Kind: RuleNone, Version: "v1"},
		Correction: PolicyRule{Kind: RuleNone, Version: "v1"},
	}
	if _, err := Publish(definition); err != nil {
		return fmt.Errorf("balance: serving definition: %w", err)
	}
	for _, kind := range []EntryKind{Debit, Credit} {
		if kind != Debit && kind != Credit {
			return fmt.Errorf("balance: serving entry kind %q is not declared", kind)
		}
	}
	for _, kind := range []BalanceRuleKind{BalanceRuleFloor, BalanceRuleCap, BalanceRuleThreshold} {
		if kind != BalanceRuleFloor && kind != BalanceRuleCap && kind != BalanceRuleThreshold {
			return fmt.Errorf("balance: serving rule kind %q is not declared", kind)
		}
	}
	for _, reason := range []ExclusionReason{
		ExcludedNotRecorded,
		ExcludedNoEffectiveTime,
		ExcludedFutureEffective,
		ExcludedNotAuthorized,
	} {
		if reason == "" {
			return fmt.Errorf("balance: serving exclusion reason is empty")
		}
	}
	if RuleApplicationEntryType == "" || AdjustmentEntryType == "" {
		return fmt.Errorf("balance: serving generated entry type is empty")
	}
	return nil
}
