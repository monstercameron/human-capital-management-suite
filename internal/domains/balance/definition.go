// Package balance owns the semantic definition of multidimensional balances.
// It deliberately stops at publication: posting and calculating balances are
// separate concerns owned by later balance-engine work.
package balance

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrDefinitionInvalid identifies a definition that cannot be published.
	ErrDefinitionInvalid = errors.New("balance: invalid accumulator definition")
	// ErrPublicationRejected identifies a definition rejected at the publication
	// boundary. It wraps ErrDefinitionInvalid and includes the offending field.
	ErrPublicationRejected = errors.New("balance: publication rejected")
)

// PeriodKind identifies the reset boundary of an accumulator.
type PeriodKind string

const (
	PeriodUnspecified  PeriodKind = ""
	PeriodPayPeriod    PeriodKind = "PAY_PERIOD"
	PeriodCalendarYear PeriodKind = "CALENDAR_YEAR"
	PeriodFiscalYear   PeriodKind = "FISCAL_YEAR"
	PeriodLifetime     PeriodKind = "LIFETIME"
	// PeriodRollingBackward is a lookback window ending at the evaluation
	// instant (WF-CAP-005): the entitlement year rolls rather than resetting
	// on a calendar boundary.
	PeriodRollingBackward PeriodKind = "ROLLING_BACKWARD"
)

// RuleKind is used by each policy rule. NONE is explicit: an omitted rule is
// not equivalent to a rule that intentionally has no limit or behaviour.
type RuleKind string

const (
	RuleUnspecified RuleKind = ""
	RuleNone        RuleKind = "NONE"
	RuleConfigured  RuleKind = "CONFIGURED"
)

// BalanceDimension is a governed key in an accumulator's balance identity.
// ValueType describes the canonical representation (for example worker_id or
// jurisdiction); it is metadata, not an untyped value bag.
type BalanceDimension struct {
	Name      string
	ValueType string
	Required  bool
}

// Authority identifies the source permitted to define and interpret a balance.
type Authority struct {
	SourceID string
	Version  string
}

// PolicyRule is the explicit policy for one balance lifecycle concern. Value
// is used only for CONFIGURED rules and remains opaque to this definition
// package; later engines interpret it according to the rule family.
type PolicyRule struct {
	Kind    RuleKind
	Version string
	Value   string
}

// AccumulatorDefinition is the complete, publishable contract for one balance
// family. All identity dimensions and lifecycle policies are required so an
// entry can never produce an unexplained balance.
type AccumulatorDefinition struct {
	ID      string
	Version string
	Name    string

	Unit     string
	Currency string
	Subject  string
	Period   PeriodKind

	Dimensions []BalanceDimension
	EntryTypes []string
	Authority  Authority

	Floor      PolicyRule
	Cap        PolicyRule
	Expiry     PolicyRule
	Rollover   PolicyRule
	Correction PolicyRule
}

// Publication is an immutable, validated definition release.
type Publication struct {
	Definition AccumulatorDefinition
}

// DefinitionError retains the exact field that prevented publication.
type DefinitionError struct {
	Field  string
	Reason string
}

func (e *DefinitionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

func (e *DefinitionError) Unwrap() error { return ErrDefinitionInvalid }

func invalid(field, reason string) error {
	return &DefinitionError{Field: field, Reason: reason}
}

// Validate checks structural completeness and semantic uniqueness. It has no
// side effects and does not imply that the definition is published.
func (d AccumulatorDefinition) Validate() error {
	for _, required := range []struct{ field, value string }{
		{"id", d.ID}, {"version", d.Version}, {"name", d.Name},
		{"unit", d.Unit}, {"currency", d.Currency}, {"subject", d.Subject},
	} {
		if strings.TrimSpace(required.value) == "" {
			return invalid(required.field, "is required")
		}
	}
	switch d.Period {
	case PeriodPayPeriod, PeriodCalendarYear, PeriodFiscalYear, PeriodLifetime, PeriodRollingBackward:
	default:
		return invalid("period", "must be specified")
	}
	if len(d.Dimensions) == 0 {
		return invalid("dimensions", "at least one dimension is required")
	}
	seen := make(map[string]struct{}, len(d.Dimensions))
	for i, dimension := range d.Dimensions {
		prefix := fmt.Sprintf("dimensions[%d]", i)
		if strings.TrimSpace(dimension.Name) == "" {
			return invalid(prefix+".name", "is required")
		}
		if strings.TrimSpace(dimension.ValueType) == "" {
			return invalid(prefix+".value_type", "is required")
		}
		key := strings.ToLower(strings.TrimSpace(dimension.Name))
		if _, ok := seen[key]; ok {
			return invalid(prefix+".name", "duplicates another dimension")
		}
		seen[key] = struct{}{}
	}
	if len(d.EntryTypes) == 0 {
		return invalid("entry_types", "at least one entry type is required")
	}
	entrySeen := make(map[string]struct{}, len(d.EntryTypes))
	for i, entryType := range d.EntryTypes {
		if strings.TrimSpace(entryType) == "" {
			return invalid(fmt.Sprintf("entry_types[%d]", i), "is required")
		}
		key := strings.ToUpper(strings.TrimSpace(entryType))
		if _, ok := entrySeen[key]; ok {
			return invalid(fmt.Sprintf("entry_types[%d]", i), "duplicates another entry type")
		}
		entrySeen[key] = struct{}{}
	}
	if strings.TrimSpace(d.Authority.SourceID) == "" {
		return invalid("authority.source_id", "is required")
	}
	if strings.TrimSpace(d.Authority.Version) == "" {
		return invalid("authority.version", "is required")
	}
	for _, policy := range []struct {
		field string
		rule  PolicyRule
	}{
		{"floor", d.Floor}, {"cap", d.Cap}, {"expiry", d.Expiry},
		{"rollover", d.Rollover}, {"correction", d.Correction},
	} {
		if err := validateRule(policy.field, policy.rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(field string, rule PolicyRule) error {
	if rule.Kind != RuleNone && rule.Kind != RuleConfigured {
		return invalid(field+".kind", "must explicitly be NONE or CONFIGURED")
	}
	if strings.TrimSpace(rule.Version) == "" {
		return invalid(field+".version", "is required")
	}
	if rule.Kind == RuleConfigured && strings.TrimSpace(rule.Value) == "" {
		return invalid(field+".value", "is required for a configured rule")
	}
	if rule.Kind == RuleNone && strings.TrimSpace(rule.Value) != "" {
		return invalid(field+".value", "must be empty for a NONE rule")
	}
	return nil
}

// Publish validates and returns an immutable publication value. No storage,
// events, outbox rows, human work, or provider requests are created here.
func Publish(d AccumulatorDefinition) (Publication, error) {
	if err := d.Validate(); err != nil {
		return Publication{}, fmt.Errorf("%w: BAL_001_REJECTED definition=%s: %w", ErrPublicationRejected, d.Version, err)
	}
	// A publication is a release value. Copy slices at the boundary so the
	// caller cannot mutate the released dimension or entry-type contract.
	released := d
	released.Dimensions = append([]BalanceDimension(nil), d.Dimensions...)
	released.EntryTypes = append([]string(nil), d.EntryTypes...)
	return Publication{Definition: released}, nil
}
