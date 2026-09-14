package onboarding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ONBOARD-006: reconcile and compensate onboarding results.
//
// Reconciliation is row by row and field by field, never by aggregate count:
// equal totals can hide a swapped value, a shifted effective date or an
// observation that disagrees with what was committed. Every source row is
// classified; every target row the import did not account for is classified
// too. Drift is compensated by governed corrective intents that append new
// history -- a compensation never deletes or rewrites a ledger event -- and an
// effect that cannot be reversed (a notification sent, a payment released) is
// reported as irreversible rather than silently "compensated".

// Row classifications.
const (
	RowMatched            = "MATCHED"
	RowFieldDrift         = "FIELD_DRIFT"
	RowEffectiveDateDrift = "EFFECTIVE_DATE_DRIFT"
	RowObservationDrift   = "OBSERVATION_DRIFT"
	RowMissingInTarget    = "MISSING_IN_TARGET"
	RowUnexpectedInTarget = "UNEXPECTED_IN_TARGET"
)

// ErrReconcileInput reports malformed reconciliation input.
var ErrReconcileInput = errors.New("onboarding: reconciliation input is invalid")

// SourceRow is what the source said, after mapping.
type SourceRow struct {
	RowID         string
	SubjectID     string
	EffectiveDate string
	Fields        map[string]string
}

// TargetRecord is what the platform committed for a subject.
type TargetRecord struct {
	SubjectID     string
	EffectiveDate string
	Fields        map[string]string
	// LedgerEventRefs are the committed events for the subject.
	LedgerEventRefs []string
	// Observed is the independently observed downstream state, when one exists.
	Observed map[string]string
	// IrreversibleEffects names effects already released for the subject.
	IrreversibleEffects []string
}

// FieldDiff is one field disagreement.
type FieldDiff struct {
	Field  string `json:"field"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// RowResult is one classified row.
type RowResult struct {
	RowID          string      `json:"row_id,omitempty"`
	SubjectID      string      `json:"subject_id"`
	Classification string      `json:"classification"`
	Diffs          []FieldDiff `json:"diffs,omitempty"`
}

// Compensation is one governed corrective intent.
type Compensation struct {
	SubjectID       string      `json:"subject_id"`
	Classification  string      `json:"classification"`
	Corrections     []FieldDiff `json:"corrections"`
	AppendsAfter    []string    `json:"appends_after"`
	IdempotencyKey  string      `json:"idempotency_key"`
	DeletesHistory  bool        `json:"deletes_history"`
	IrreversibleRef []string    `json:"irreversible_effects,omitempty"`
}

// ReconciliationReport is the complete result.
type ReconciliationReport struct {
	ImportID      string         `json:"import_id"`
	Rows          []RowResult    `json:"rows"`
	Counts        map[string]int `json:"counts"`
	Compensations []Compensation `json:"compensations"`
	// Irreversible lists subjects with released effects no compensation can undo.
	Irreversible []string `json:"irreversible"`
	Digest       string   `json:"digest"`
}

// Reconcile classifies every source row and every unaccounted target record,
// and plans compensation.
func Reconcile(importID string, source []SourceRow, target []TargetRecord) (ReconciliationReport, error) {
	if strings.TrimSpace(importID) == "" {
		return ReconciliationReport{}, fmt.Errorf("%w: import id is required", ErrReconcileInput)
	}
	targets := map[string]TargetRecord{}
	for _, t := range target {
		if t.SubjectID == "" {
			return ReconciliationReport{}, fmt.Errorf("%w: target record without subject", ErrReconcileInput)
		}
		if _, dup := targets[t.SubjectID]; dup {
			return ReconciliationReport{}, fmt.Errorf("%w: duplicate target subject %s", ErrReconcileInput, t.SubjectID)
		}
		targets[t.SubjectID] = t
	}
	report := ReconciliationReport{ImportID: importID, Rows: []RowResult{}, Counts: map[string]int{}, Compensations: []Compensation{}, Irreversible: []string{}}
	seenSubjects := map[string]bool{}
	for _, s := range source {
		if s.RowID == "" || s.SubjectID == "" {
			return ReconciliationReport{}, fmt.Errorf("%w: source row without id or subject", ErrReconcileInput)
		}
		if seenSubjects[s.SubjectID] {
			return ReconciliationReport{}, fmt.Errorf("%w: subject %s appears in two source rows", ErrReconcileInput, s.SubjectID)
		}
		seenSubjects[s.SubjectID] = true
		t, ok := targets[s.SubjectID]
		row := RowResult{RowID: s.RowID, SubjectID: s.SubjectID}
		switch {
		case !ok:
			row.Classification = RowMissingInTarget
			for _, f := range sortedKeys(s.Fields) {
				row.Diffs = append(row.Diffs, FieldDiff{Field: f, Source: s.Fields[f]})
			}
		default:
			row.Diffs = diffFields(s.Fields, t.Fields)
			switch {
			case len(row.Diffs) > 0:
				row.Classification = RowFieldDrift
			case s.EffectiveDate != t.EffectiveDate:
				row.Classification = RowEffectiveDateDrift
				row.Diffs = []FieldDiff{{Field: "effective_date", Source: s.EffectiveDate, Target: t.EffectiveDate}}
			case t.Observed != nil && len(diffFields(t.Fields, t.Observed)) > 0:
				row.Classification = RowObservationDrift
				row.Diffs = diffFields(t.Fields, t.Observed)
			default:
				row.Classification = RowMatched
			}
		}
		report.add(row, t)
	}
	for _, id := range sortedKeys(targetsAsStrings(targets)) {
		if seenSubjects[id] {
			continue
		}
		t := targets[id]
		row := RowResult{SubjectID: id, Classification: RowUnexpectedInTarget}
		for _, f := range sortedKeys(t.Fields) {
			row.Diffs = append(row.Diffs, FieldDiff{Field: f, Target: t.Fields[f]})
		}
		report.add(row, t)
	}
	sort.Strings(report.Irreversible)
	body, _ := json.Marshal(report)
	sum := sha256.Sum256(body)
	report.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return report, nil
}

func (r *ReconciliationReport) add(row RowResult, t TargetRecord) {
	r.Rows = append(r.Rows, row)
	r.Counts[row.Classification]++
	if row.Classification == RowMatched {
		return
	}
	sum := sha256.Sum256([]byte(r.ImportID + "\x00" + row.SubjectID + "\x00" + row.Classification))
	comp := Compensation{SubjectID: row.SubjectID, Classification: row.Classification, AppendsAfter: append([]string{}, t.LedgerEventRefs...),
		IdempotencyKey: "onboard-compensate:" + hex.EncodeToString(sum[:12])}
	for _, d := range row.Diffs {
		// A compensation proposes the source-of-truth value; for a record the
		// source never had, it proposes retiring the target value.
		comp.Corrections = append(comp.Corrections, FieldDiff{Field: d.Field, Source: d.Target, Target: d.Source})
	}
	if len(t.IrreversibleEffects) > 0 {
		comp.IrreversibleRef = append([]string{}, t.IrreversibleEffects...)
		sort.Strings(comp.IrreversibleRef)
		r.Irreversible = append(r.Irreversible, row.SubjectID)
	}
	r.Compensations = append(r.Compensations, comp)
}

func diffFields(want, got map[string]string) []FieldDiff {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var out []FieldDiff
	for _, k := range sortedKeys(boolsAsStrings(keys)) {
		if want[k] != got[k] {
			out = append(out, FieldDiff{Field: k, Source: want[k], Target: got[k]})
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func targetsAsStrings(m map[string]TargetRecord) map[string]string {
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = ""
	}
	return out
}

func boolsAsStrings(m map[string]bool) map[string]string {
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = ""
	}
	return out
}
