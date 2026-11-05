package app

import (
	"fmt"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The P1A intent request payloads.
//
// The four P1A definitions declare input schemas
// (hcmnext.people.v1.PromoteWorkerRequest and friends) that the generation
// toolchain does not emit a descriptor for yet: schema/proto carries the
// intent, capability, registry and common contracts, and the per-domain
// request messages are still to be authored. Until they exist, a request
// travels as a google.protobuf.Struct under the declared schema identity, and
// the decoders below are the one place that knows it.
//
// This is a deliberate seam, not an encoding decision: the kernel already
// treats the payload as opaque typed bytes it only digests, so replacing the
// Struct with the generated message changes these decoders and nothing else.

// PayBandInputs is the resolved input for an evaluate_pay_band_position intent.
type PayBandInputs struct {
	Query  rewards.BandQuery
	Amount values.Money
}

// SeveranceInputs is everything the severance capability needs: the
// published plan it prices with and the declared per-worker query.
type SeveranceInputs struct {
	Plan  rewards.SeverancePlan
	Query rewards.SeveranceInput
}

// decodeStruct reads the temporary dynamic request through the approved
// Protobuf-to-intent mapping seam.
func decodeStruct(wire []byte) (*protomap.Struct, error) { return protomap.DecodeStruct(wire) }

// fieldsOf returns the named sub-object, or an error naming the path.
func fieldsOf(s *protomap.Struct, path string) (*protomap.Struct, error) {
	v, ok := s.GetFields()[path]
	if !ok {
		return nil, fmt.Errorf("app: request payload has no %q object", path)
	}
	sub := v.GetStructValue()
	if sub == nil {
		return nil, fmt.Errorf("app: request payload field %q is not an object", path)
	}
	return sub, nil
}

// str returns a required string field.
func str(s *protomap.Struct, path string) (string, error) {
	v, ok := s.GetFields()[path]
	if !ok {
		return "", fmt.Errorf("app: request payload has no %q field", path)
	}
	text := v.GetStringValue()
	if text == "" {
		return "", fmt.Errorf("app: request payload field %q is empty or not a string", path)
	}
	return text, nil
}

// optionalStr returns a string field that may be absent.
func optionalStr(s *protomap.Struct, path string) string {
	if v, ok := s.GetFields()[path]; ok {
		return v.GetStringValue()
	}
	return ""
}

// optionalStrings returns a repeated string field that may be absent. A value
// that is not a list, or a list element that is not a string, contributes
// nothing rather than an empty entry: a malformed population is caught by the
// resolver that finds it empty, not by silently inventing a blank reference.
func optionalStrings(s *protomap.Struct, path string) []string {
	v, ok := s.GetFields()[path]
	if !ok {
		return nil
	}
	list := v.GetListValue()
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list.GetValues()))
	for _, item := range list.GetValues() {
		if text := item.GetStringValue(); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// localDate reads a required YYYY-MM-DD field.
func localDate(s *protomap.Struct, path string) (values.LocalDate, error) {
	text, err := str(s, path)
	if err != nil {
		return values.LocalDate{}, err
	}
	d, parseErr := values.ParseLocalDate(text)
	if parseErr != nil {
		return values.LocalDate{}, fmt.Errorf("app: request payload field %q: %w", path, parseErr)
	}
	return d, nil
}

// uintField reads a required non-negative integer field, accepting both a JSON
// number and a decimal string so a caller need not care which the encoder
// chose.
func uintField(s *protomap.Struct, path string) (uint64, error) {
	v, ok := s.GetFields()[path]
	if !ok {
		return 0, fmt.Errorf("app: request payload has no %q field", path)
	}
	switch inner := v.GetKind().(type) {
	case *protomap.NumberValue:
		if inner.NumberValue < 0 {
			return 0, fmt.Errorf("app: request payload field %q is negative", path)
		}
		return uint64(inner.NumberValue), nil
	case *protomap.StringValue:
		n, err := strconv.ParseUint(inner.StringValue, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("app: request payload field %q: %w", path, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("app: request payload field %q is not a number", path)
	}
}

// payBasis parses the mandatory pay basis token.
func payBasis(text string) (rewards.PayBasis, error) {
	switch text {
	case "ANNUAL_SALARY", "":
		return rewards.PayBasisAnnualSalary, nil
	case "MONTHLY_SALARY":
		return rewards.PayBasisMonthlySalary, nil
	case "HOURLY":
		return rewards.PayBasisHourly, nil
	default:
		return rewards.PayBasisUnspecified, fmt.Errorf("app: unknown pay basis %q", text)
	}
}

// compensationSnapshot decodes one pinned compensation side.
func compensationSnapshot(s *protomap.Struct, path string) (rewards.CompensationSnapshot, error) {
	sub, err := fieldsOf(s, path)
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	amount, err := str(sub, "base")
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	currency, err := str(sub, "currency")
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	base, err := fixtures.Money(amount, currency)
	if err != nil {
		return rewards.CompensationSnapshot{}, fmt.Errorf("app: %s.base: %w", path, err)
	}
	basis, err := payBasis(optionalStr(sub, "pay_basis"))
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	effective, err := localDate(sub, "effective_date")
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	stream, err := str(sub, "revision_stream")
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	sequence, err := uintField(sub, "revision_sequence")
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	watermark, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		return rewards.CompensationSnapshot{}, fmt.Errorf("app: %s.revision: %w", path, err)
	}

	snapshot := rewards.CompensationSnapshot{
		Base:          values.Value(base),
		PayBasis:      basis,
		EffectiveDate: effective,
		Watermark:     watermark,
		Complete:      true,
	}
	if fraction := optionalStr(sub, "bonus_target"); fraction != "" {
		pct, pctErr := fixtures.Percent(fraction)
		if pctErr != nil {
			return rewards.CompensationSnapshot{}, fmt.Errorf("app: %s.bonus_target: %w", path, pctErr)
		}
		snapshot.BonusTargetPercent = values.Value(pct)
	} else {
		snapshot.BonusTargetPercent = values.Absent[values.Percentage]()
	}
	return snapshot, nil
}

// budgetObservation decodes the optional workforce-budget observation.
func budgetObservation(s *protomap.Struct) (*promotion.BudgetAuthorityRef, error) {
	v, ok := s.GetFields()["budget"]
	if !ok || v.GetStructValue() == nil {
		return nil, nil
	}
	sub := v.GetStructValue()
	amount, err := str(sub, "available_amount")
	if err != nil {
		return nil, err
	}
	currency, err := str(sub, "currency")
	if err != nil {
		return nil, err
	}
	available, err := fixtures.Money(amount, currency)
	if err != nil {
		return nil, fmt.Errorf("app: budget.available_amount: %w", err)
	}
	return &promotion.BudgetAuthorityRef{
		BudgetType:      promotion.BudgetTypeCompensationPool,
		OwnerSystem:     optionalStr(sub, "owner_system"),
		PolicyRef:       optionalStr(sub, "policy_ref"),
		Scope:           optionalStr(sub, "scope"),
		Period:          optionalStr(sub, "period"),
		Currency:        currency,
		Unit:            "MONEY",
		BaselineVersion: optionalStr(sub, "baseline_version"),
		AvailableAmount: values.Value(available),
		ObservationID:   optionalStr(sub, "observation_id"),
	}, nil
}

// targetPlacement decodes the proposed job, grade and organizational placement.
func targetPlacement(s *protomap.Struct) (promotion.TargetPlacement, error) {
	sub, err := fieldsOf(s, "target")
	if err != nil {
		return promotion.TargetPlacement{}, err
	}
	jobCode, err := str(sub, "job_code")
	if err != nil {
		return promotion.TargetPlacement{}, err
	}
	grade, err := str(sub, "grade")
	if err != nil {
		return promotion.TargetPlacement{}, err
	}
	orgUnit, err := str(sub, "org_unit")
	if err != nil {
		return promotion.TargetPlacement{}, err
	}
	return promotion.TargetPlacement{
		JobCode:    jobCode,
		Grade:      grade,
		OrgUnit:    orgUnit,
		PositionID: optionalStr(sub, "position_id"),
		PayZone:    optionalStr(sub, "pay_zone"),
	}, nil
}

// bandQuery decodes a pay-band address.
func bandQuery(s *protomap.Struct, tenant values.TenantId, asOf values.LocalDate) (rewards.BandQuery, error) {
	jobCode, err := str(s, "job_code")
	if err != nil {
		return rewards.BandQuery{}, err
	}
	grade, err := str(s, "grade")
	if err != nil {
		return rewards.BandQuery{}, err
	}
	payZone, err := str(s, "pay_zone")
	if err != nil {
		return rewards.BandQuery{}, err
	}
	currency, err := str(s, "currency")
	if err != nil {
		return rewards.BandQuery{}, err
	}
	return rewards.BandQuery{
		Tenant:   tenant,
		JobCode:  jobCode,
		Grade:    grade,
		PayZone:  payZone,
		Currency: currency,
		AsOf:     asOf,
	}, nil
}

// structValue is the decoded P1A request payload. It is an alias rather than a
// wrapper so that replacing the Struct with the generated request message is a
// type change here and nothing more.
type structValue = protomap.Struct
