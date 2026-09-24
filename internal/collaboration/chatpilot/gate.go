// Package chatpilot evaluates a proposed chat-pilot evidence packet. It does
// not create rollout authority: every artifact must be resolved and verified
// by an explicitly supplied evidence authority before the gate can pass.
package chatpilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Kind string

const (
	ScopeDecision Kind = "scope_decision"
	ServedUse     Kind = "served_partner_use"
	SLODashboard  Kind = "slo_dashboard"
	IncidentPlan  Kind = "incident_playbook"
	RollbackDrill Kind = "rollback_drill"
	Adoption      Kind = "adoption_observation"
)

var requiredKinds = [...]Kind{ScopeDecision, ServedUse, SLODashboard, IncidentPlan, RollbackDrill, Adoption}

// Reference identifies an immutable artifact. The verifier, not the packet
// submitter, establishes signer authority, digest authenticity, freshness and
// the truth of the artifact's claims.
type Reference struct {
	Kind   Kind   `json:"kind"`
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type Packet struct {
	Evidence []Reference `json:"evidence"`
}

// Verified is a verifier-produced projection of a signed artifact. A concrete
// verifier must only return this value after checking the signature, signer
// authority, reference digest, tenant binding and validity window.
type Verified struct {
	Reference        Reference
	Signer           string
	TenantID         string
	Owner            string
	DisplacedWork    string
	WindowStart      time.Time
	WindowEnd        time.Time
	ObservedAt       time.Time
	Numerator        uint64
	Denominator      uint64
	MinimumRate      float64
	Budgets          map[string]MetricBudget
	RollbackObserved bool
	Metrics          []Metric
}

type MetricBudget struct {
	Limit float64
	Unit  string
}

type Metric struct {
	Name     string
	Samples  uint64
	Observed float64
	Limit    float64
	Unit     string
}

type Verifier interface {
	Verify(context.Context, Reference, time.Time) (Verified, error)
}

type Decision struct {
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

var requiredMetrics = [...]string{
	"chat.send_commit.p95",
	"chat.send_commit.p99",
	"chat.watch_delivery.p99",
	"workflow.admission_to_start.p99",
	"workflow.timer_lateness.p99",
	"workflow.queue_age.p99",
	"chat.cost_per_active_employee",
}

// Review is fail-closed. No data supplied directly in Packet is treated as a
// pilot fact; the verifier must resolve each immutable reference to its signed
// source. A nil verifier always blocks.
func Review(ctx context.Context, packet Packet, verifier Verifier, now time.Time) Decision {
	var reasons []string
	if verifier == nil {
		return Decision{Reasons: []string{"trusted evidence verifier is unavailable"}}
	}
	refs := make(map[Kind]Reference, len(packet.Evidence))
	for _, ref := range packet.Evidence {
		if !knownKind(ref.Kind) {
			reasons = append(reasons, "unknown evidence kind: "+string(ref.Kind))
			continue
		}
		if _, ok := refs[ref.Kind]; ok {
			reasons = append(reasons, "duplicate evidence kind: "+string(ref.Kind))
			continue
		}
		refs[ref.Kind] = ref
		if strings.TrimSpace(ref.ID) == "" || !validDigest(ref.SHA256) {
			reasons = append(reasons, "invalid immutable reference: "+string(ref.Kind))
		}
	}
	for _, kind := range requiredKinds {
		if _, ok := refs[kind]; !ok {
			reasons = append(reasons, "missing signed evidence: "+string(kind))
		}
	}
	if len(reasons) > 0 {
		return Decision{Reasons: reasons}
	}

	verified := make(map[Kind]Verified, len(refs))
	for _, kind := range requiredKinds {
		ref := refs[kind]
		evidence, err := verifier.Verify(ctx, ref, now)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("%s verification failed: %v", kind, err))
			continue
		}
		if evidence.Reference != ref || strings.TrimSpace(evidence.Signer) == "" || evidence.ObservedAt.IsZero() || evidence.ObservedAt.After(now) {
			reasons = append(reasons, "unbound or unobserved verified evidence: "+string(kind))
			continue
		}
		verified[kind] = evidence
	}
	if len(reasons) > 0 {
		return Decision{Reasons: reasons}
	}

	scope := verified[ScopeDecision]
	partner := verified[ServedUse]
	if strings.TrimSpace(scope.Owner) == "" || strings.TrimSpace(scope.DisplacedWork) == "" || strings.TrimSpace(scope.TenantID) == "" {
		reasons = append(reasons, "signed scope decision must name an accountable owner, displaced work, and pilot tenant")
	}
	if math.IsNaN(scope.MinimumRate) || math.IsInf(scope.MinimumRate, 0) || scope.MinimumRate <= 0 || scope.MinimumRate > 1 {
		reasons = append(reasons, "signed scope decision must set a valid adoption threshold")
	}
	for _, kind := range [...]Kind{ServedUse, SLODashboard, IncidentPlan, RollbackDrill, Adoption} {
		if verified[kind].TenantID == "" || verified[kind].TenantID != scope.TenantID {
			reasons = append(reasons, "evidence is not bound to the signed pilot tenant: "+string(kind))
		}
	}
	if !observedWindow(partner, now) || partner.Denominator == 0 || partner.Numerator == 0 || partner.Numerator > partner.Denominator {
		reasons = append(reasons, "served partner use lacks a valid observed employee denominator")
	}
	if !observedWindow(verified[Adoption], now) || verified[Adoption].Denominator == 0 || verified[Adoption].Numerator == 0 || verified[Adoption].Numerator > verified[Adoption].Denominator {
		reasons = append(reasons, "adoption evidence lacks a valid observed numerator and employee denominator")
	} else if float64(verified[Adoption].Numerator)/float64(verified[Adoption].Denominator) < scope.MinimumRate {
		reasons = append(reasons, "observed adoption is below the signed scope decision threshold")
	}
	if !observedWindow(verified[SLODashboard], now) {
		reasons = append(reasons, "SLO dashboard has no valid observed window")
	} else {
		for _, name := range requiredMetrics {
			metric, ok := metricByName(verified[SLODashboard].Metrics, name)
			budget, budgetOK := scope.Budgets[name]
			if !budgetOK || math.IsNaN(budget.Limit) || math.IsInf(budget.Limit, 0) || budget.Limit <= 0 || strings.TrimSpace(budget.Unit) == "" ||
				!ok || metric.Samples == 0 || math.IsNaN(metric.Observed) || math.IsInf(metric.Observed, 0) || metric.Observed < 0 ||
				metric.Limit != budget.Limit || metric.Unit != budget.Unit || metric.Observed > budget.Limit {
				reasons = append(reasons, "SLO dashboard lacks a passing sampled metric: "+name)
			}
		}
	}
	if !observedWindow(verified[IncidentPlan], now) {
		reasons = append(reasons, "incident playbook is not current")
	}
	rollback := verified[RollbackDrill]
	if !observedWindow(rollback, now) || !rollback.RollbackObserved {
		reasons = append(reasons, "rollback drill has no successful observed rollback")
	}
	return Decision{Ready: len(reasons) == 0, Reasons: reasons}
}

func observedWindow(e Verified, now time.Time) bool {
	return !e.WindowStart.IsZero() && !e.WindowEnd.IsZero() && !e.WindowStart.After(e.WindowEnd) && !e.WindowEnd.After(now) && !e.ObservedAt.Before(e.WindowStart) && !e.ObservedAt.After(e.WindowEnd)
}

func metricByName(metrics []Metric, name string) (Metric, bool) {
	var found Metric
	count := 0
	for _, metric := range metrics {
		if metric.Name == name {
			found = metric
			count++
		}
	}
	return found, count == 1
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func knownKind(kind Kind) bool {
	for _, required := range requiredKinds {
		if required == kind {
			return true
		}
	}
	return false
}

var ErrEvidenceUnavailable = errors.New("evidence unavailable")
