// CHAT-052 gate support. The GREEN condition requires the mixed-load profile
// (workflow and chat p95/p99 latency, queue age and cost) to be recorded
// against a signed regression gate before broad chat/workflow coexistence is
// authorized. This file provides the gate record shape, its schema
// validation, its canonical digest, and the evaluation code that checks a
// measured profile against the recorded regression budgets. It does not and
// cannot supply the human sign-off the GREEN condition also requires: the
// gate record this repository ships stays PROPOSED/unsigned, and Evaluate
// always reports the profile as unauthorized to activate until a real
// approval with named signers replaces it.
package chatdelivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// RegressionGateArtifactPath is the CHAT-052 regression-gate record.
const RegressionGateArtifactPath = "definitions/planning/gates/chat-052-regression-gate.json"

// MetricBudget names one measured signal and the maximum regression the gate
// permits against its recorded baseline, or an absolute ceiling.
type MetricBudget struct {
	Name                    string  `json:"name"`
	Unit                    string  `json:"unit"`
	BaselineSource          string  `json:"baseline_source"`
	RegressionBudgetPercent float64 `json:"regression_budget_percent"`
	HardCeiling             float64 `json:"hard_ceiling,omitempty"`
}

// RegressionGateRecord is the CHAT-052 signed regression gate artifact.
type RegressionGateRecord struct {
	TodoID          string         `json:"todo_id"`
	Status          string         `json:"status"`
	Profile         string         `json:"profile"`
	Metrics         []MetricBudget `json:"metrics"`
	Evidence        []Evidence     `json:"source_evidence"`
	Approval        Approval       `json:"approval"`
	CanonicalDigest string         `json:"canonical_digest"`
}

type canonicalRegressionGate struct {
	TodoID   string         `json:"todo_id"`
	Status   string         `json:"status"`
	Profile  string         `json:"profile"`
	Metrics  []MetricBudget `json:"metrics"`
	Evidence []Evidence     `json:"source_evidence"`
	Approval Approval       `json:"approval"`
}

func canonicalRegression(r RegressionGateRecord) canonicalRegressionGate {
	return canonicalRegressionGate{r.TodoID, r.Status, r.Profile, r.Metrics, r.Evidence, r.Approval}
}

// DigestRegressionGate returns the canonical digest of a CHAT-052 gate record.
func DigestRegressionGate(r RegressionGateRecord) (string, error) {
	b, err := json.Marshal(canonicalRegression(r))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// LoadRegressionGate reads and decodes the CHAT-052 gate record.
func LoadRegressionGate(path string) (RegressionGateRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RegressionGateRecord{}, err
	}
	var r RegressionGateRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return RegressionGateRecord{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return r, nil
}

// ValidateRegressionGate checks the CHAT-052 gate record's schema. It refuses
// to accept a record that claims signed approval: this repository does not
// ship real signers, so a valid record must stay PROPOSED with no signers.
func ValidateRegressionGate(r RegressionGateRecord) []Violation {
	var out []Violation
	add := func(field, issue string) { out = append(out, Violation{field, issue}) }
	if r.TodoID != "CHAT-052" {
		add("todo_id", "must be CHAT-052")
	}
	if r.Status != "PROPOSED" {
		add("status", "must remain PROPOSED until approval evidence exists")
	}
	if r.Profile == "" {
		add("profile", "must name the measured mixed-load profile")
	}
	if len(r.Metrics) == 0 {
		add("metrics", "must record at least one regression budget")
	}
	for i, m := range r.Metrics {
		if m.Name == "" || m.Unit == "" || m.BaselineSource == "" {
			add(fmt.Sprintf("metrics[%d]", i), "name, unit and baseline_source are required")
		}
		if m.RegressionBudgetPercent <= 0 && m.HardCeiling <= 0 {
			add(fmt.Sprintf("metrics[%d]", i), "must set a regression_budget_percent or a hard_ceiling")
		}
	}
	if len(r.Evidence) == 0 {
		add("source_evidence", "must cite repository evidence for the recorded budgets")
	}
	for i, item := range r.Evidence {
		if item.Path == "" || item.Claim == "" {
			add(fmt.Sprintf("source_evidence[%d]", i), "path and claim are required")
		}
	}
	if r.Approval.Status != "PENDING" {
		add("approval.status", "must remain PENDING until real signers and evidence are recorded")
	}
	if len(r.Approval.RequiredRoles) == 0 {
		add("approval.required_roles", "must name the approval roles")
	}
	if len(r.Approval.Signers) != 0 {
		add("approval.signers", "must stay empty until external approval is actually obtained")
	}
	d, err := DigestRegressionGate(r)
	if err != nil || r.CanonicalDigest != d {
		add("canonical_digest", "must match the canonical record digest")
	}
	return out
}

// MetricObservation is one measured signal from a mixed-load profile run.
type MetricObservation struct {
	Name      string
	Baseline  float64
	Candidate float64
}

// RegressionResult is the per-metric outcome of checking an observation
// against its recorded budget.
type RegressionResult struct {
	Name              string
	RegressionPercent float64
	WithinBudget      bool
	Reason            string
}

// EvaluateRegression checks measured mixed-load observations against the
// gate's recorded budgets. Authorized is only ever true when every named
// metric is present, within its budget, and the gate record itself is
// signed (Approval.Status == "APPROVED" with at least one signer). This
// repository's shipped gate record is unsigned, so Authorized is always
// false here; that is intentional, not a defect — CHAT-052's GREEN also
// requires a signed regression gate, which is outside what code can supply.
func EvaluateRegression(gate RegressionGateRecord, observed []MetricObservation) (authorized bool, results []RegressionResult, violations []Violation) {
	byName := make(map[string]MetricObservation, len(observed))
	for _, o := range observed {
		byName[o.Name] = o
	}
	allWithinBudget := true
	for _, budget := range gate.Metrics {
		o, ok := byName[budget.Name]
		if !ok {
			violations = append(violations, Violation{Field: "metrics." + budget.Name, Issue: "no observation recorded for this budgeted metric"})
			allWithinBudget = false
			continue
		}
		result := RegressionResult{Name: budget.Name}
		if budget.HardCeiling > 0 {
			result.WithinBudget = o.Candidate <= budget.HardCeiling
			if !result.WithinBudget {
				result.Reason = fmt.Sprintf("candidate %.4f exceeds hard ceiling %.4f", o.Candidate, budget.HardCeiling)
			}
		} else {
			pct := 0.0
			if o.Baseline != 0 {
				pct = (o.Candidate - o.Baseline) / o.Baseline * 100
			} else if o.Candidate != 0 {
				pct = 100
			}
			result.RegressionPercent = pct
			result.WithinBudget = pct <= budget.RegressionBudgetPercent
			if !result.WithinBudget {
				result.Reason = fmt.Sprintf("regression %.2f%% exceeds budget %.2f%%", pct, budget.RegressionBudgetPercent)
			}
		}
		if !result.WithinBudget {
			allWithinBudget = false
			violations = append(violations, Violation{Field: "metrics." + budget.Name, Issue: result.Reason})
		}
		results = append(results, result)
	}
	if gate.Approval.Status != "APPROVED" || len(gate.Approval.Signers) == 0 {
		violations = append(violations, Violation{Field: "approval", Issue: "regression gate is unsigned; measured profile cannot authorize activation"})
		return false, results, violations
	}
	return allWithinBudget, results, violations
}
