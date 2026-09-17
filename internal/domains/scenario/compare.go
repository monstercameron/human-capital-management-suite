// Scenario comparison: SCENARIO-004 compares two revisions of one scenario
// and explains every assumption difference with its provenance.
//
// A comparison is descriptive only: it never mutates either revision, never
// touches authoritative facts and never fabricates precision. Two revisions
// compare only when they describe the same scenario over the same baseline
// snapshot, scope and horizon after normalization; anything else is a typed
// incomparability, never a partial diff.
package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ErrIncomparable reports revisions that cannot be compared: different
// scenarios, baselines, scopes or horizons. Callers must match on this
// sentinel with errors.Is rather than parsing the reason.
var ErrIncomparable = errors.New("scenario: revisions are not comparable")

// DiffPartition is the closed partition vocabulary for one assumption key.
type DiffPartition string

const (
	PartitionAdded     DiffPartition = "ADDED"
	PartitionRemoved   DiffPartition = "REMOVED"
	PartitionChanged   DiffPartition = "CHANGED"
	PartitionUnchanged DiffPartition = "UNCHANGED"
)

// Valid reports whether the partition is declared.
func (p DiffPartition) Valid() bool {
	switch p {
	case PartitionAdded, PartitionRemoved, PartitionChanged, PartitionUnchanged:
		return true
	default:
		return false
	}
}

// AssumptionDiff traces one assumption key across both revisions. Exactly
// one of the two sides is present for ADDED/REMOVED; both are present for
// CHANGED/UNCHANGED. Provenance on each present side names the evidence
// behind that side's value, which is how a metric traces to assumptions.
type AssumptionDiff struct {
	Key                string
	Partition          DiffPartition
	HasBaseline        bool
	HasCompared        bool
	BaselineValue      TypedValue
	ComparedValue      TypedValue
	BaselineUnit       string
	ComparedUnit       string
	BaselineProvenance []string
	ComparedProvenance []string
}

// Validate implements validation.
func (d AssumptionDiff) Validate() error {
	if strings.TrimSpace(d.Key) == "" {
		return fmt.Errorf("%w: diff key is required", ErrInvalidScenario)
	}
	if !d.Partition.Valid() {
		return fmt.Errorf("%w: unknown diff partition %q", ErrInvalidScenario, d.Partition)
	}
	switch d.Partition {
	case PartitionAdded:
		if d.HasBaseline || !d.HasCompared {
			return fmt.Errorf("%w: ADDED diff %q must carry only the compared side", ErrInvalidScenario, d.Key)
		}
	case PartitionRemoved:
		if !d.HasBaseline || d.HasCompared {
			return fmt.Errorf("%w: REMOVED diff %q must carry only the baseline side", ErrInvalidScenario, d.Key)
		}
	default:
		if !d.HasBaseline || !d.HasCompared {
			return fmt.Errorf("%w: %s diff %q must carry both sides", ErrInvalidScenario, d.Partition, d.Key)
		}
	}
	if d.HasBaseline {
		if err := d.BaselineValue.Validate(); err != nil {
			return fmt.Errorf("%w: baseline value of %q: %v", ErrInvalidScenario, d.Key, err)
		}
		if len(d.BaselineProvenance) == 0 {
			return fmt.Errorf("%w: baseline provenance of %q is required", ErrInvalidScenario, d.Key)
		}
	}
	if d.HasCompared {
		if err := d.ComparedValue.Validate(); err != nil {
			return fmt.Errorf("%w: compared value of %q: %v", ErrInvalidScenario, d.Key, err)
		}
		if len(d.ComparedProvenance) == 0 {
			return fmt.Errorf("%w: compared provenance of %q is required", ErrInvalidScenario, d.Key)
		}
	}
	return nil
}

// Comparison is the bound result of comparing two revisions. Diffs are
// sorted by key so identical inputs always produce identical bytes.
type Comparison struct {
	ScenarioID          string
	BaselineRevision    uint64
	ComparedRevision    uint64
	BaselineSnapshotRef string
	Scope               string
	Horizon             string
	Diffs               []AssumptionDiff
	CanonicalDigest     string
}

// Counts partitions the diffs by partition.
func (c Comparison) Counts() (added, removed, changed, unchanged int) {
	for _, diff := range c.Diffs {
		switch diff.Partition {
		case PartitionAdded:
			added++
		case PartitionRemoved:
			removed++
		case PartitionChanged:
			changed++
		case PartitionUnchanged:
			unchanged++
		}
	}
	return added, removed, changed, unchanged
}

// Validate implements validation.
func (c Comparison) Validate() error {
	if strings.TrimSpace(c.ScenarioID) == "" {
		return fmt.Errorf("%w: scenario id is required", ErrInvalidScenario)
	}
	if c.BaselineRevision == 0 || c.ComparedRevision == 0 {
		return fmt.Errorf("%w: both comparison revisions are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(c.BaselineSnapshotRef) == "" || strings.TrimSpace(c.Scope) == "" || strings.TrimSpace(c.Horizon) == "" {
		return fmt.Errorf("%w: comparison baseline, scope and horizon bindings are required", ErrInvalidScenario)
	}
	if len(c.Diffs) == 0 {
		return fmt.Errorf("%w: comparison carries no diffs", ErrInvalidScenario)
	}
	for i, diff := range c.Diffs {
		if err := diff.Validate(); err != nil {
			return err
		}
		if i > 0 && c.Diffs[i-1].Key >= diff.Key {
			return fmt.Errorf("%w: diffs are not sorted by key", ErrInvalidScenario)
		}
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (c Comparison) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.scenario.Comparison", schemaVersion).
		String("scenario_id", c.ScenarioID).Int("baseline_revision", int64(c.BaselineRevision)).
		Int("compared_revision", int64(c.ComparedRevision)).
		String("baseline_snapshot_ref", c.BaselineSnapshotRef).String("scope", c.Scope).
		String("horizon", c.Horizon).Count("diffs", len(c.Diffs))
	for _, diff := range c.Diffs {
		d := canonicalbytes.New("hcmnext.domains.scenario.AssumptionDiff", schemaVersion).
			String("key", diff.Key).String("partition", string(diff.Partition)).
			Bool("has_baseline", diff.HasBaseline).Bool("has_compared", diff.HasCompared).
			String("baseline_unit", normalizeUnit(diff.BaselineUnit)).
			String("compared_unit", normalizeUnit(diff.ComparedUnit)).
			SortedStrings("baseline_provenance", append([]string(nil), diff.BaselineProvenance...)).
			SortedStrings("compared_provenance", append([]string(nil), diff.ComparedProvenance...))
		if diff.HasBaseline {
			d.Value("baseline_value", diff.BaselineValue)
		}
		if diff.HasCompared {
			d.Value("compared_value", diff.ComparedValue)
		}
		w.Nested("diff", d)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (c Comparison) computedDigest() string {
	raw := c.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical bytes of a valid comparison.
func (c Comparison) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	return c.body()
}

// normalizeUnit folds the cosmetic unit spellings a comparison must not
// treat as a semantic change. A renamed unit is still a change; only
// case and surrounding whitespace normalize away.
func normalizeUnit(unit string) string {
	return strings.ToUpper(strings.TrimSpace(unit))
}

func typedValuesEqual(a, b TypedValue) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case ValueText:
		return a.Text == b.Text
	case ValueDecimal:
		return a.Number.Equal(b.Number)
	case ValueBoolean:
		return a.Boolean == b.Boolean
	default:
		return false
	}
}

// Compare normalizes two revisions onto a common baseline, horizon, scope
// and unit footing, partitions every assumption key and traces each side
// to its provenance. It is pure: no rows, events, effects or writes.
// Revisions that do not describe the same scenario over the same baseline
// return ErrIncomparable.
func Compare(baseline, compared ScenarioRevision) (Comparison, error) {
	if err := baseline.Validate(); err != nil {
		return Comparison{}, err
	}
	if err := compared.Validate(); err != nil {
		return Comparison{}, err
	}
	if baseline.ScenarioID != compared.ScenarioID {
		return Comparison{}, fmt.Errorf("%w: scenario %q is not %q", ErrIncomparable, compared.ScenarioID, baseline.ScenarioID)
	}
	if baseline.BaselineSnapshotRef != compared.BaselineSnapshotRef {
		return Comparison{}, fmt.Errorf("%w: baseline %q is not %q", ErrIncomparable, compared.BaselineSnapshotRef, baseline.BaselineSnapshotRef)
	}
	if baseline.Scope != compared.Scope {
		return Comparison{}, fmt.Errorf("%w: scope %q is not %q", ErrIncomparable, compared.Scope, baseline.Scope)
	}
	if baseline.Horizon.String() != compared.Horizon.String() {
		return Comparison{}, fmt.Errorf("%w: horizon %q is not %q", ErrIncomparable, compared.Horizon.String(), baseline.Horizon.String())
	}
	left := make(map[string]Assumption, len(baseline.Assumptions))
	for _, assumption := range baseline.Assumptions {
		left[assumption.Key] = assumption
	}
	right := make(map[string]Assumption, len(compared.Assumptions))
	for _, assumption := range compared.Assumptions {
		right[assumption.Key] = assumption
	}
	keys := make([]string, 0, len(left)+len(right))
	for key := range left {
		keys = append(keys, key)
	}
	for key := range right {
		if _, ok := left[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := Comparison{
		ScenarioID:          baseline.ScenarioID,
		BaselineRevision:    baseline.Revision,
		ComparedRevision:    compared.Revision,
		BaselineSnapshotRef: baseline.BaselineSnapshotRef,
		Scope:               baseline.Scope,
		Horizon:             baseline.Horizon.String(),
	}
	for _, key := range keys {
		before, hasBefore := left[key]
		after, hasAfter := right[key]
		diff := AssumptionDiff{Key: key, HasBaseline: hasBefore, HasCompared: hasAfter}
		switch {
		case hasBefore && !hasAfter:
			diff.Partition = PartitionRemoved
			diff.BaselineValue, diff.BaselineUnit = before.Value, before.Unit
			diff.BaselineProvenance = append([]string(nil), before.ProvenanceRefs...)
		case !hasBefore && hasAfter:
			diff.Partition = PartitionAdded
			diff.ComparedValue, diff.ComparedUnit = after.Value, after.Unit
			diff.ComparedProvenance = append([]string(nil), after.ProvenanceRefs...)
		case typedValuesEqual(before.Value, after.Value) && normalizeUnit(before.Unit) == normalizeUnit(after.Unit):
			diff.Partition = PartitionUnchanged
			diff.BaselineValue, diff.BaselineUnit = before.Value, before.Unit
			diff.BaselineProvenance = append([]string(nil), before.ProvenanceRefs...)
			diff.ComparedValue, diff.ComparedUnit = after.Value, after.Unit
			diff.ComparedProvenance = append([]string(nil), after.ProvenanceRefs...)
		default:
			diff.Partition = PartitionChanged
			diff.BaselineValue, diff.BaselineUnit = before.Value, before.Unit
			diff.BaselineProvenance = append([]string(nil), before.ProvenanceRefs...)
			diff.ComparedValue, diff.ComparedUnit = after.Value, after.Unit
			diff.ComparedProvenance = append([]string(nil), after.ProvenanceRefs...)
		}
		result.Diffs = append(result.Diffs, diff)
	}
	result.CanonicalDigest = result.computedDigest()
	if err := result.Validate(); err != nil {
		return Comparison{}, err
	}
	return result, nil
}
