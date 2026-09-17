package labor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrCostExportRejected is the LABOR-005 refusal boundary. Canonical costs
// map to Finance and Payroll with no lost dimension; anything external that
// disagrees creates scoped repair instead of silent acceptance.
var ErrCostExportRejected = errors.New("LABOR_005_REJECTED")

// CostLine is one canonical labor-cost line. Dimensions are never optional:
// a line without a governed dimension binding is a lost dimension.
type CostLine struct {
	Ordinal      int
	Dimensions   []Dimension
	WageAmount   values.Decimal
	BurdenAmount values.Decimal
	Currency     string
	LineTotal    values.Decimal
}

// CostExportRequest binds canonical lines to one rule version and source.
type CostExportRequest struct {
	ExportID    string
	Rule        LaborRule
	RuleVersion string
	Source      string
	Lines       []CostLine
}

// CostExport is the immutable canonical costing handed to Finance/Payroll.
type CostExport struct {
	ExportID    string
	RuleID      string
	RuleVersion string
	Source      string
	Currency    string
	Lines       []CostLine
	TotalWages  values.Decimal
	TotalBurden values.Decimal
	TotalCost   values.Decimal
	Scale       int32
	Rounding    values.RoundingMode
	Digest      string
}

// NewCostExport validates dimensions, currency and totals, then seals the
// export with a content digest.
func NewCostExport(req CostExportRequest) (CostExport, error) {
	fail := func(format string, args ...any) (CostExport, error) {
		return CostExport{}, errors.Join(ErrCostExportRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(req.ExportID) == "" || strings.TrimSpace(req.Source) == "" {
		return fail("export id and source are required")
	}
	if err := req.Rule.Validate(); err != nil {
		return fail("rule: %v", err)
	}
	if req.RuleVersion != req.Rule.Version {
		return fail("export binds rule version %q, want %q", req.RuleVersion, req.Rule.Version)
	}
	if len(req.Lines) == 0 {
		return fail("at least one cost line is required")
	}
	scale, rounding := int32(2), values.RoundingHalfUp
	lines := make([]CostLine, len(req.Lines))
	seenOrdinal := map[int]struct{}{}
	var currency string
	for i, line := range req.Lines {
		where := fmt.Sprintf("line %d", i)
		if line.Ordinal <= 0 {
			return fail("%s: ordinal must be positive", where)
		}
		if _, ok := seenOrdinal[line.Ordinal]; ok {
			return fail("%s: duplicate ordinal %d", where, line.Ordinal)
		}
		seenOrdinal[line.Ordinal] = struct{}{}
		if len(line.Dimensions) == 0 {
			return fail("%s: at least one dimension is required", where)
		}
		seenDim := map[string]struct{}{}
		for _, dim := range line.Dimensions {
			if err := dim.Validate(); err != nil {
				return fail("%s: %v", where, err)
			}
			key := dim.Kind.String() + "\x00" + dim.Value + "\x00" + dim.Version
			if _, ok := seenDim[key]; ok {
				return fail("%s: duplicate dimension", where)
			}
			seenDim[key] = struct{}{}
		}
		for name, amount := range map[string]values.Decimal{"wages": line.WageAmount, "burden": line.BurdenAmount} {
			if err := amount.Validate(); err != nil {
				return fail("%s %s: %v", where, name, err)
			}
			if amount.Scale() != scale || amount.Rounding() != rounding {
				return fail("%s %s precision differs", where, name)
			}
			if amount.Sign() < 0 {
				return fail("%s %s cannot be negative", where, name)
			}
		}
		if strings.TrimSpace(line.Currency) == "" {
			return fail("%s: currency is required", where)
		}
		if currency == "" {
			currency = line.Currency
		} else if line.Currency != currency {
			return fail("%s: mixed currencies %q and %q", where, currency, line.Currency)
		}
		total, err := line.WageAmount.Add(line.BurdenAmount)
		if err != nil {
			return fail("%s: %v", where, err)
		}
		lines[i] = line
		lines[i].LineTotal = total
	}
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j].Ordinal < lines[j-1].Ordinal; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	zero, err := values.NewDecimal("0", scale, rounding)
	if err != nil {
		return fail("zero: %v", err)
	}
	wages, burden := zero, zero
	for _, line := range lines {
		wages, err = wages.Add(line.WageAmount)
		if err != nil {
			return fail("wage total: %v", err)
		}
		burden, err = burden.Add(line.BurdenAmount)
		if err != nil {
			return fail("burden total: %v", err)
		}
	}
	cost, err := wages.Add(burden)
	if err != nil {
		return fail("total cost: %v", err)
	}
	out := CostExport{
		ExportID: req.ExportID, RuleID: req.Rule.ID, RuleVersion: req.Rule.Version,
		Source: req.Source, Currency: currency, Lines: lines,
		TotalWages: wages, TotalBurden: burden, TotalCost: cost,
		Scale: scale, Rounding: rounding,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func (e CostExport) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.labor.CostExport", 1).
		String("export_id", e.ExportID).String("rule_id", e.RuleID).
		String("rule_version", e.RuleVersion).String("source", e.Source).
		String("currency", e.Currency).Value("total_wages", e.TotalWages).
		Value("total_burden", e.TotalBurden).Value("total_cost", e.TotalCost).
		Count("lines", len(e.Lines))
	for _, line := range e.Lines {
		w.Int("ordinal", int64(line.Ordinal)).Value("wages", line.WageAmount).
			Value("burden", line.BurdenAmount).Value("line_total", line.LineTotal).
			Count("dimensions", len(line.Dimensions))
		for _, dim := range line.Dimensions {
			w.Value("dimension", dim)
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate recomputes line totals, export totals and the digest binding.
func (e CostExport) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrCostExportRejected, fmt.Errorf(format, args...))
	}
	zero, err := values.NewDecimal("0", e.Scale, e.Rounding)
	if err != nil {
		return fail("zero: %v", err)
	}
	wages, burden := zero, zero
	for _, line := range e.Lines {
		if len(line.Dimensions) == 0 {
			return fail("line %d lost its dimensions", line.Ordinal)
		}
		want, err := line.WageAmount.Add(line.BurdenAmount)
		if err != nil || !want.Equal(line.LineTotal) {
			return fail("line %d total mismatch", line.Ordinal)
		}
		wages, err = wages.Add(line.WageAmount)
		if err != nil {
			return fail("wage total: %v", err)
		}
		burden, err = burden.Add(line.BurdenAmount)
		if err != nil {
			return fail("burden total: %v", err)
		}
	}
	if !wages.Equal(e.TotalWages) || !burden.Equal(e.TotalBurden) {
		return fail("component totals mismatch")
	}
	total, err := wages.Add(burden)
	if err != nil || !total.Equal(e.TotalCost) {
		return fail("total cost mismatch")
	}
	if e.Digest != canonicalbytes.Digest(e.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}

// ExternalLineState is the closed external observation vocabulary.
type ExternalLineState string

const (
	ExternalAccepted ExternalLineState = "ACCEPTED"
	ExternalRejected ExternalLineState = "REJECTED"
	ExternalPartial  ExternalLineState = "PARTIAL"
	ExternalUnknown  ExternalLineState = "UNKNOWN"
)

// ExternalCostLine is one externally observed cost line.
type ExternalCostLine struct {
	Ordinal      int
	ExternalID   string
	Amount       values.Decimal
	Currency     string
	DimensionIDs []string
	State        ExternalLineState
}

// ExternalCostObservation is the Finance/Payroll-side view of an export.
type ExternalCostObservation struct {
	Source       string
	ExternalID   string
	ExportDigest string
	Lines        []ExternalCostLine
	Total        values.Decimal
	Currency     string
}

// LineStatus is the per-line reconciliation outcome.
type LineStatus string

const (
	LineConsistent LineStatus = "CONSISTENT"
	LineMismatch   LineStatus = "MISMATCH"
	LineUnknown    LineStatus = "UNKNOWN"
)

// RepairScope names the span a repair case must cover.
type RepairScope string

const (
	RepairScopeTotals   RepairScope = "TOTALS"
	RepairScopeLine     RepairScope = "LINE"
	RepairScopeCoverage RepairScope = "COVERAGE"
)

// RepairRef is a scoped repair obligation. It carries ordinals and detail,
// never an instruction to rewrite history.
type RepairRef struct {
	Scope   RepairScope
	Ordinal int
	Detail  string
}

// ReconcileStatus is the aggregate reconciliation outcome.
type ReconcileStatus string

const (
	ReconcileConsistent     ReconcileStatus = "CONSISTENT"
	ReconcileRepairRequired ReconcileStatus = "REPAIR_REQUIRED"
)

// LineResult pairs one exported line with its reconciliation outcome.
type LineResult struct {
	Ordinal int
	Status  LineStatus
	Detail  string
}

// CostReconciliation is the immutable reconciliation of one export against
// one external observation.
type CostReconciliation struct {
	ExportDigest  string
	ObservationID string
	Lines         []LineResult
	Aggregate     ReconcileStatus
	Repairs       []RepairRef
}

// ReconcileCostExport compares canonical lines, totals, dimensions and
// external identities. Agreement returns CONSISTENT; any reject, partial,
// unknown, mismatch or gap returns REPAIR_REQUIRED with scoped repairs.
func ReconcileCostExport(exp CostExport, obs ExternalCostObservation) (CostReconciliation, error) {
	fail := func(format string, args ...any) (CostReconciliation, error) {
		return CostReconciliation{}, errors.Join(ErrCostExportRejected, fmt.Errorf(format, args...))
	}
	if err := exp.Validate(); err != nil {
		return fail("export: %v", err)
	}
	if strings.TrimSpace(obs.Source) == "" || strings.TrimSpace(obs.ExternalID) == "" || strings.TrimSpace(obs.Currency) == "" {
		return fail("observation source, id and currency are required")
	}
	if err := obs.Total.Validate(); err != nil {
		return fail("observation total: %v", err)
	}
	if obs.ExportDigest != "" && obs.ExportDigest != exp.Digest {
		return fail("observation binds another export")
	}
	rec := CostReconciliation{ExportDigest: exp.Digest, ObservationID: obs.ExternalID}
	byOrdinal := map[int]ExternalCostLine{}
	for _, line := range obs.Lines {
		byOrdinal[line.Ordinal] = line
	}
	currencyMatch := obs.Currency == exp.Currency
	for _, line := range exp.Lines {
		result := LineResult{Ordinal: line.Ordinal, Status: LineConsistent}
		external, ok := byOrdinal[line.Ordinal]
		switch {
		case !ok:
			result.Status = LineUnknown
			result.Detail = "no external line observed"
			rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeCoverage, Ordinal: line.Ordinal, Detail: result.Detail})
		case !currencyMatch:
			result.Status = LineUnknown
			result.Detail = fmt.Sprintf("currency %q cannot be compared to %q", external.Currency, exp.Currency)
			rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State == ExternalRejected || external.State == ExternalPartial:
			result.Status = LineMismatch
			result.Detail = fmt.Sprintf("external state %s", external.State)
			rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State == ExternalUnknown:
			result.Status = LineUnknown
			result.Detail = "external state unknown"
			rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State != ExternalAccepted:
			return fail("line %d: external state %q is not declared", line.Ordinal, external.State)
		case !external.Amount.Equal(line.LineTotal):
			result.Status = LineMismatch
			result.Detail = fmt.Sprintf("external amount %s, canonical %s", external.Amount, line.LineTotal)
			rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeLine, Ordinal: line.Ordinal, Detail: result.Detail})
		default:
			missing := []string{}
			for _, dim := range line.Dimensions {
				found := false
				for _, id := range external.DimensionIDs {
					if id == dim.Value {
						found = true
						break
					}
				}
				if !found {
					missing = append(missing, dim.Value)
				}
			}
			if len(missing) > 0 {
				result.Status = LineMismatch
				result.Detail = fmt.Sprintf("lost dimensions %v", missing)
				rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeLine, Ordinal: line.Ordinal, Detail: result.Detail})
			}
		}
		rec.Lines = append(rec.Lines, result)
	}
	if len(obs.Lines) > len(exp.Lines) {
		rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeCoverage, Detail: "external lines without canonical counterpart"})
	}
	totalsOK := currencyMatch && obs.Total.Equal(exp.TotalCost) && len(obs.Lines) == len(exp.Lines)
	if !totalsOK {
		rec.Repairs = append(rec.Repairs, RepairRef{Scope: RepairScopeTotals, Detail: fmt.Sprintf("external total %s %s, canonical %s %s", obs.Total, obs.Currency, exp.TotalCost, exp.Currency)})
	}
	rec.Aggregate = ReconcileConsistent
	for _, result := range rec.Lines {
		if result.Status != LineConsistent {
			rec.Aggregate = ReconcileRepairRequired
			break
		}
	}
	if !totalsOK {
		rec.Aggregate = ReconcileRepairRequired
	}
	return rec, nil
}
