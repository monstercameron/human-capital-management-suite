// Pilot cutover drills: CUSTOMER-003 executes dry-run, cutover,
// rollback and hypercare migration drills as one time-sequenced,
// side-effect-free record.
//
// The drill records exact prechecks, cutover decisions, RPO/RTO,
// rollback/fail-forward results, resumption fences, item-level
// reconciliation and hypercare ownership. Injected failures reach only
// allowed states, unresolved ambiguity blocks go-live, and retried
// deltas never duplicate an accepted intent or effect.
package readiness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CutoverDecision is the closed drill outcome.
type CutoverDecision string

// The cutover outcomes.
const (
	CutoverGoLive   CutoverDecision = "GO_LIVE"
	CutoverRollback CutoverDecision = "ROLLBACK"
	CutoverHold     CutoverDecision = "HOLD"
)

// DeltaItem is one captured change with its content digest. Applied is
// the drill-local application flag: retrying an applied delta is a
// duplicate, never a second effect.
type DeltaItem struct {
	Ref    string
	Digest string
}

// StopGoThresholds bound the drill's go-live criteria.
type StopGoThresholds struct {
	MaxRPO time.Duration
	MaxRTO time.Duration
}

// Transition declares one credential or webhook move with its owner.
type Transition struct {
	Name  string
	Owner string
}

// CutoverDrill is the complete drill declaration.
type CutoverDrill struct {
	DrillID          string
	TenantRef        string
	FreezeWatermark  string
	Deltas           []DeltaItem
	Thresholds       StopGoThresholds
	RollbackBoundary string
	Transitions      []Transition
	HypercareOwner   string
	CustomerContact  string
	ManualContinuity string
}

// CutoverInput is the execution envelope.
type CutoverInput struct {
	Drill       CutoverDrill
	At          time.Time
	ObservedRPO time.Duration
	ObservedRTO time.Duration
	// Failures names injected failures; each must resolve to an allowed
	// state or the drill rolls back.
	Failures []string
	// FailForward, when true, records a fail-forward result instead of a
	// rollback for recoverable injected failures.
	FailForward bool
}

// CutoverReconciliation is the item-level applied/total account.
type CutoverReconciliation struct {
	Applied int
	Total   int
}

// CutoverFinding names one drill defect.
type CutoverFinding struct {
	Code   string
	Detail string
}

// CutoverReport is the deterministic drill receipt.
type CutoverReport struct {
	DrillID         string
	Decision        CutoverDecision
	RPO             time.Duration
	RTO             time.Duration
	Reconciliation  CutoverReconciliation
	FailForward     bool
	ResumptionFence string
	HypercareOwner  string
	Duplicates      int
	Findings        []CutoverFinding
	Digest          string
}

// Allowed injected failures and the states they may reach.
var allowedFailures = map[string][]CutoverDecision{
	"delta-gap":       {CutoverRollback},
	"source-freeze":   {CutoverRollback, CutoverHold},
	"webhook-lag":     {CutoverRollback, CutoverHold},
	"credential-lag":  {CutoverRollback, CutoverHold},
	"ambiguous-delta": {CutoverRollback, CutoverHold},
}

// ExecuteCutover runs one time-sequenced drill. Missing prechecks,
// unresolved ambiguity and disallowed failures roll back or hold with
// named findings; a clean drill goes live with exact reconciliation.
func ExecuteCutover(in CutoverInput) (CutoverReport, error) {
	drill := in.Drill
	if strings.TrimSpace(drill.DrillID) == "" || drill.DrillID != strings.TrimSpace(drill.DrillID) ||
		strings.TrimSpace(drill.TenantRef) == "" || drill.TenantRef != strings.TrimSpace(drill.TenantRef) {
		return CutoverReport{}, errors.New("readiness: drill id and tenant ref are required exact, without padding")
	}
	if in.At.IsZero() {
		return CutoverReport{}, errors.New("readiness: drill time is required")
	}
	if in.ObservedRPO < 0 || in.ObservedRTO < 0 {
		return CutoverReport{}, errors.New("readiness: observed RPO and RTO cannot be negative")
	}
	report := CutoverReport{
		DrillID: drill.DrillID, Decision: CutoverGoLive,
		RPO: in.ObservedRPO, RTO: in.ObservedRTO,
		ResumptionFence: "fence:" + drill.DrillID, HypercareOwner: drill.HypercareOwner,
	}
	hold := func(code, detail string) {
		if report.Decision == CutoverGoLive {
			report.Decision = CutoverHold
		}
		report.Findings = append(report.Findings, CutoverFinding{Code: code, Detail: detail})
	}
	rollback := func(code, detail string) {
		report.Decision = CutoverRollback
		report.Findings = append(report.Findings, CutoverFinding{Code: code, Detail: detail})
	}
	// Prechecks: freeze, thresholds, rollback boundary, transitions,
	// hypercare, customer contact and manual continuity.
	if strings.TrimSpace(drill.FreezeWatermark) == "" {
		rollback("PRECHECK_FREEZE", "freeze watermark is required")
	}
	if drill.Thresholds.MaxRPO <= 0 || drill.Thresholds.MaxRTO <= 0 {
		rollback("PRECHECK_THRESHOLDS", "stop/go thresholds are required")
	}
	if strings.TrimSpace(drill.RollbackBoundary) == "" {
		rollback("PRECHECK_ROLLBACK", "rollback boundary is required")
	}
	for _, transition := range drill.Transitions {
		if strings.TrimSpace(transition.Name) == "" || strings.TrimSpace(transition.Owner) == "" {
			rollback("PRECHECK_TRANSITION", "credential/webhook transitions need owners")
			break
		}
	}
	if strings.TrimSpace(drill.HypercareOwner) == "" {
		rollback("PRECHECK_HYPERCARE", "hypercare owner is required")
	}
	if strings.TrimSpace(drill.CustomerContact) == "" {
		rollback("PRECHECK_CONTACT", "customer communication contact is required")
	}
	if strings.TrimSpace(drill.ManualContinuity) == "" {
		rollback("PRECHECK_CONTINUITY", "manual continuity path is required")
	}
	// Delta capture: every delta carries a digest; retries deduplicate.
	seen := map[string]bool{}
	applied := map[string]bool{}
	for _, delta := range drill.Deltas {
		if strings.TrimSpace(delta.Ref) == "" || strings.TrimSpace(delta.Digest) == "" {
			rollback("DELTA_AMBIGUOUS", "delta without ref or digest blocks go-live")
			continue
		}
		if seen[delta.Ref] {
			report.Duplicates++
			continue
		}
		seen[delta.Ref] = true
		applied[delta.Ref] = true
	}
	report.Reconciliation = CutoverReconciliation{Applied: len(applied), Total: len(drill.Deltas)}
	// Stop/go thresholds.
	if in.ObservedRPO > drill.Thresholds.MaxRPO {
		rollback("RPO_EXCEEDED", fmt.Sprintf("observed RPO %s exceeds %s", in.ObservedRPO, drill.Thresholds.MaxRPO))
	}
	if in.ObservedRTO > drill.Thresholds.MaxRTO {
		rollback("RTO_EXCEEDED", fmt.Sprintf("observed RTO %s exceeds %s", in.ObservedRTO, drill.Thresholds.MaxRTO))
	}
	// Injected failures reach only allowed states.
	for _, failure := range in.Failures {
		allowed, known := allowedFailures[failure]
		if !known {
			rollback("FAILURE_DISALLOWED", "injected failure "+failure+" has no allowed state")
			continue
		}
		resolvesHold := false
		for _, state := range allowed {
			if state == CutoverHold {
				resolvesHold = true
			}
		}
		if in.FailForward && resolvesHold {
			hold("FAILURE_"+strings.ToUpper(strings.ReplaceAll(failure, "-", "_")), "injected failure "+failure+" held for fail-forward review")
		} else {
			rollback("FAILURE_"+strings.ToUpper(strings.ReplaceAll(failure, "-", "_")), "injected failure "+failure+" rolled back")
		}
	}
	if report.Decision == CutoverRollback {
		report.FailForward = false
	} else {
		report.FailForward = in.FailForward
	}
	if report.Decision != CutoverGoLive {
		report.ResumptionFence = "fence:" + drill.DrillID + ":held"
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Code != report.Findings[j].Code {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].Detail < report.Findings[j].Detail
	})
	report.Digest = digestCutover(report, drill)
	return report, nil
}

func digestCutover(report CutoverReport, drill CutoverDrill) string {
	parts := []string{"customer003-cutover", drill.DrillID, drill.TenantRef, drill.FreezeWatermark,
		string(report.Decision), report.RPO.String(), report.RTO.String(),
		fmt.Sprint(report.FailForward), report.ResumptionFence, report.HypercareOwner,
		fmt.Sprint(report.Reconciliation.Applied), fmt.Sprint(report.Reconciliation.Total),
		fmt.Sprint(report.Duplicates), drill.RollbackBoundary, drill.CustomerContact, drill.ManualContinuity}
	for _, delta := range drill.Deltas {
		parts = append(parts, delta.Ref+"\x00"+delta.Digest)
	}
	for _, transition := range drill.Transitions {
		parts = append(parts, transition.Name+"\x00"+transition.Owner)
	}
	for _, finding := range report.Findings {
		parts = append(parts, finding.Code+"\x00"+finding.Detail)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CutoverCell files drill reports keyed by drill ID. It is safe for
// concurrent use.
type CutoverCell struct {
	mu      sync.Mutex
	reports map[string]CutoverReport
}

// NewCutoverCell starts an empty cell.
func NewCutoverCell() *CutoverCell {
	return &CutoverCell{reports: map[string]CutoverReport{}}
}

// Record files one drill report exactly once per drill ID.
func (c *CutoverCell) Record(in CutoverInput) (CutoverReport, error) {
	report, err := ExecuteCutover(in)
	if err != nil {
		return CutoverReport{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, seen := c.reports[report.DrillID]; seen {
		return prior, nil
	}
	c.reports[report.DrillID] = report
	return report, nil
}
