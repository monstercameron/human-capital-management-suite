package importing

import (
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// Validation rule identities. Every [ValidationError] carries exactly one of
// these as RuleID. The set is closed: RULE-004 is not on this package's
// allowed import list, so the validator defines its own stable rule
// vocabulary rather than delegating to a rules engine it cannot import.
const (
	// RuleRequiredMissing fires when a required property's mapped value is
	// empty after a successful transform.
	RuleRequiredMissing = "presence.required_missing"
	// RuleIdentityDuplicate fires when two distinct rows in the same batch
	// share the same non-empty composite identity value.
	RuleIdentityDuplicate = "identity.duplicate"
	// RuleColumnUnmapped is a batch-level warning: a header column the
	// compiled mapping never reads.
	RuleColumnUnmapped = "mapping.column_unmapped"
	// RuleDateParseFailed, RuleMoneyParseFailed and RuleLookupUnresolved are
	// re-exported for convenience; they are produced by [MappingProfile.Apply]
	// (see mapping.go) and surfaced here as validation errors.
)

// rowIdentityProperty is the synthetic property token an identity.duplicate
// error is filed under. It is not a real [model.PropertyRef]: a duplicate is
// a fact about the row as a whole, keyed by every IsIdentity field together,
// not about any single mapped field.
const rowIdentityProperty = "row_identity"

// Validate errors. All are matchable with errors.Is.
var (
	ErrValidationInput      = errors.New("importing: validation input is invalid")
	ErrMappingColumnMissing = errors.New("importing: mapping reads a column the batch does not have")
)

// computeIdentity derives the stable error identity.
func computeIdentity(batchID, rowID, property, ruleID string) string {
	w := importdraft.NewCanon("hcmnext.dataops.importing.ValidationErrorIdentity", 1)
	w.Str(batchID).Str(rowID).Str(property).Str(ruleID)
	return w.DigestHex()
}

// RowResult is one row's validation outcome.
type RowResult struct {
	RowID  string
	Valid  bool
	Errors []ValidationError // ValidationSeverityError entries for this row, sorted by RuleID then Property.
}

// ValidationSummary partitions a validation run's findings. Counts always
// sum consistently: ValidRows + InvalidRows == TotalRows, and every entry in
// ByRule counts a distinct (row, property, rule) violation exactly once.
type ValidationSummary struct {
	BatchDigest   string
	MappingDigest string
	TotalRows     int
	ValidRows     int
	InvalidRows   int
	ByRule        map[string]int
	WarningCount  int
}

// MaxErrorSample bounds [Result.Sample].
const MaxErrorSample = 100

// Result is the full output of [ValidateBatch].
type Result struct {
	Summary  ValidationSummary
	Rows     []RowResult       // in batch row order.
	Errors   []ValidationError // every ValidationSeverityError entry, sorted by RowID, then RuleID, then Property.
	Warnings []ValidationError // batch-level ValidationSeverityWarning entries, sorted by Property.
}

// Sample returns up to MaxErrorSample errors, in the same stable order as
// Errors. A validation result travels to dashboards and tickets in full; this
// is a convenience for a caller that only wants a bounded preview.
func (r Result) Sample() []ValidationError {
	if len(r.Errors) <= MaxErrorSample {
		return append([]ValidationError(nil), r.Errors...)
	}
	return append([]ValidationError(nil), r.Errors[:MaxErrorSample]...)
}

// closedRuleIDs is every rule this validator can produce, in a fixed order,
// so [ValidationSummary.ByRule] can be built without ranging over a map.
var closedRuleIDs = []string{
	RuleRequiredMissing,
	RuleIdentityDuplicate,
	RuleDateParseFailed,
	RuleMoneyParseFailed,
	RuleLookupUnresolved,
}

// ValidateBatch applies mapping to every row of b and returns a stable,
// reproducible validation result. It performs no domain write and no I/O: it
// is a pure function of the batch's content, the mapping's content, and the
// canonical property registry's declared presence rules baked into the
// compiled mapping's targets.
//
// Row order never changes a row's diagnosis or its error identities, because
// both are keyed by [Row.ID] and by the mapping's own (fixed) field order,
// never by position.
func ValidateBatch(b Batch, m MappingProfile, reg *model.Registry) (Result, error) {
	if reg == nil {
		return Result{}, fmt.Errorf("%w: registry is nil", ErrValidationInput)
	}
	if err := b.Validate(); err != nil {
		return Result{}, err
	}
	header := b.Header()
	headerSet := make(map[string]struct{}, len(header))
	for _, c := range header {
		headerSet[c] = struct{}{}
	}
	for _, col := range m.Reads {
		if _, ok := headerSet[col]; !ok {
			return Result{}, fmt.Errorf("%w: %q", ErrMappingColumnMissing, col)
		}
	}

	// Pre-resolve each mapped field's presence rule once; this is a registry
	// lookup, not a per-row cost, and its result never varies across rows.
	presence := make(map[model.PropertyRef]bool, len(m.Fields)) // true when REQUIRED.
	for _, f := range m.Fields {
		resolution, err := reg.ResolveProperty(f.Target)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %q: %w", ErrValidationInput, f.Target, err)
		}
		presence[f.Target] = resolution.Property.Presence == model.PresenceRequired
	}

	batchID := b.Digest()
	rows := b.Rows()

	type identityBucket struct {
		key  string
		rows []string // row IDs sharing this composite identity.
	}
	identityIndex := make(map[string]int) // composite key -> index into buckets.
	var buckets []identityBucket

	rowResults := make([]RowResult, 0, len(rows))
	perRowFieldErrors := make(map[string][]ValidationError, len(rows))

	for _, row := range rows {
		mapped, err := m.Apply(header, row)
		if err != nil {
			return Result{}, err
		}
		var rowErrors []ValidationError
		var identityParts []string
		identityComplete := true

		for _, mv := range mapped {
			if !mv.OK {
				e := ValidationError{
					BatchID:  batchID,
					RowID:    row.ID(),
					Property: string(mv.Target),
					RuleID:   mv.ErrorCode,
					Severity: ValidationSeverityError,
					Message:  fmt.Sprintf("column %q failed rule %s", mv.SourceColumn, mv.ErrorCode),
				}
				e.Identity = computeIdentity(e.BatchID, e.RowID, e.Property, e.RuleID)
				rowErrors = append(rowErrors, e)
				identityComplete = false
				continue
			}
			if presence[mv.Target] && mv.Value == "" {
				e := ValidationError{
					BatchID:  batchID,
					RowID:    row.ID(),
					Property: string(mv.Target),
					RuleID:   RuleRequiredMissing,
					Severity: ValidationSeverityError,
					Message:  fmt.Sprintf("required property %s has no value", mv.Target),
				}
				e.Identity = computeIdentity(e.BatchID, e.RowID, e.Property, e.RuleID)
				rowErrors = append(rowErrors, e)
			}
			if isIdentityField(m, mv.Target) {
				if mv.Value == "" {
					identityComplete = false
				}
				identityParts = append(identityParts, mv.Value)
			}
		}

		if identityComplete && len(identityParts) > 0 {
			key := identityKey(identityParts)
			if idx, ok := identityIndex[key]; ok {
				buckets[idx].rows = append(buckets[idx].rows, row.ID())
			} else {
				identityIndex[key] = len(buckets)
				buckets = append(buckets, identityBucket{key: key, rows: []string{row.ID()}})
			}
		}

		sortErrors(rowErrors)
		perRowFieldErrors[row.ID()] = rowErrors
		rowResults = append(rowResults, RowResult{RowID: row.ID(), Valid: len(rowErrors) == 0, Errors: rowErrors})
	}

	// Second pass: flag every row in a bucket with more than one member.
	for _, bucket := range buckets {
		if len(bucket.rows) < 2 {
			continue
		}
		for _, rid := range bucket.rows {
			e := ValidationError{
				BatchID:  batchID,
				RowID:    rid,
				Property: rowIdentityProperty,
				RuleID:   RuleIdentityDuplicate,
				Severity: ValidationSeverityError,
				Message:  fmt.Sprintf("row identity is shared by %d rows", len(bucket.rows)),
			}
			e.Identity = computeIdentity(e.BatchID, e.RowID, e.Property, e.RuleID)
			perRowFieldErrors[rid] = append(perRowFieldErrors[rid], e)
		}
	}
	for i := range rowResults {
		errs := perRowFieldErrors[rowResults[i].RowID]
		sortErrors(errs)
		rowResults[i].Errors = errs
		rowResults[i].Valid = len(errs) == 0
	}

	// Batch-level warnings: a header column the mapping never reads.
	readSet := make(map[string]struct{}, len(m.Reads))
	for _, c := range m.Reads {
		readSet[c] = struct{}{}
	}
	var warnings []ValidationError
	for _, c := range header {
		if _, mapped := readSet[c]; mapped {
			continue
		}
		e := ValidationError{
			BatchID:  batchID,
			Property: c,
			RuleID:   RuleColumnUnmapped,
			Severity: ValidationSeverityWarning,
			Message:  fmt.Sprintf("column %q is not read by mapping %s", c, m.Version),
		}
		e.Identity = computeIdentity(e.BatchID, "", e.Property, e.RuleID)
		warnings = append(warnings, e)
	}
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].Property < warnings[j].Property })

	summary := ValidationSummary{
		BatchDigest:   batchID,
		MappingDigest: m.Digest,
		TotalRows:     len(rowResults),
		WarningCount:  len(warnings),
		ByRule:        make(map[string]int, len(closedRuleIDs)),
	}
	var allErrors []ValidationError
	for _, rr := range rowResults {
		if rr.Valid {
			summary.ValidRows++
		} else {
			summary.InvalidRows++
		}
		allErrors = append(allErrors, rr.Errors...)
	}
	for _, ruleID := range closedRuleIDs {
		count := 0
		for _, e := range allErrors {
			if e.RuleID == ruleID {
				count++
			}
		}
		summary.ByRule[ruleID] = count
	}

	sort.Slice(allErrors, func(i, j int) bool {
		if allErrors[i].RowID != allErrors[j].RowID {
			return allErrors[i].RowID < allErrors[j].RowID
		}
		if allErrors[i].RuleID != allErrors[j].RuleID {
			return allErrors[i].RuleID < allErrors[j].RuleID
		}
		return allErrors[i].Property < allErrors[j].Property
	})

	return Result{Summary: summary, Rows: rowResults, Errors: allErrors, Warnings: warnings}, nil
}

func isIdentityField(m MappingProfile, target model.PropertyRef) bool {
	for _, f := range m.Fields {
		if f.Target == target {
			return f.IsIdentity
		}
	}
	return false
}

// identityKey builds a delimiter-safe composite key from ordered identity
// parts, length-prefixing each so that ["ab","c"] and ["a","bc"] never
// collide.
func identityKey(parts []string) string {
	w := importdraft.NewCanon("hcmnext.dataops.importing.IdentityKey", 1)
	w.Strings(parts)
	return w.DigestHex()
}

func sortErrors(errs []ValidationError) {
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].RuleID != errs[j].RuleID {
			return errs[i].RuleID < errs[j].RuleID
		}
		return errs[i].Property < errs[j].Property
	})
}
