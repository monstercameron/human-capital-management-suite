// Recovery game days: RECOVERY-004 executes failover, failback and
// recovery scenarios with decision owners, safe degraded modes,
// cutover/failback fences and reconciled outcomes.
//
// Each scenario meets its declared RPO/RTO or produces a deterministic
// failed gate naming the miss, with incident, customer advisory, repair
// and post-game review evidence. Failure injection cannot escape
// test/recovery cells: a fence naming production is an error, never a
// drill.
package recovery

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

// LostAsset is the closed set of game-day scenario assets.
type LostAsset string

// The game-day scenario assets.
const (
	LostDatabase    LostAsset = "database"
	LostRegion      LostAsset = "region"
	LostLedgerShard LostAsset = "ledger-shard"
	LostSigningKey  LostAsset = "signing-key"
	LostAdmin       LostAsset = "admin"
	LostIdP         LostAsset = "identity-provider"
	LostProvider    LostAsset = "provider"
)

// GameDayStatus is the closed scenario outcome.
type GameDayStatus string

// The game-day outcomes.
const (
	GameDayPass       GameDayStatus = "PASS"
	GameDayFailedGate GameDayStatus = "FAILED_GATE"
)

// GameDayScenario is one declared failover/failback drill.
type GameDayScenario struct {
	DrillID       string
	Asset         LostAsset
	Owner         string
	DegradedMode  string
	BudgetRPO     time.Duration
	BudgetRTO     time.Duration
	Fence         string
	CutoverFence  string
	FailbackFence string
}

// GameDayEvidence carries the four required evidence artifacts.
type GameDayEvidence struct {
	Incident   string
	Advisory   string
	Repair     string
	PostReview string
}

// GameDayInput is the complete execution envelope.
type GameDayInput struct {
	Scenario    GameDayScenario
	ObservedRPO time.Duration
	ObservedRTO time.Duration
	Evidence    GameDayEvidence
	FailedBack  bool
}

// GameDayFinding names one gate defect.
type GameDayFinding struct {
	Code   string
	Detail string
}

// GameDayReport is the deterministic drill receipt.
type GameDayReport struct {
	DrillID     string
	Asset       LostAsset
	Status      GameDayStatus
	ObservedRPO time.Duration
	ObservedRTO time.Duration
	FailedBack  bool
	Findings    []GameDayFinding
	Digest      string
}

func validLostAsset(asset LostAsset) bool {
	switch asset {
	case LostDatabase, LostRegion, LostLedgerShard, LostSigningKey, LostAdmin, LostIdP, LostProvider:
		return true
	}
	return false
}

// ExecuteGameDay runs one scenario. Malformed envelopes and production
// fences are errors; budget misses and missing evidence fence the
// report as a deterministic failed gate.
func ExecuteGameDay(in GameDayInput) (GameDayReport, error) {
	scenario := in.Scenario
	if strings.TrimSpace(scenario.DrillID) == "" || strings.TrimSpace(scenario.DrillID) != scenario.DrillID {
		return GameDayReport{}, errors.New("recovery: drill id is required exact, without padding")
	}
	if !validLostAsset(scenario.Asset) {
		return GameDayReport{}, fmt.Errorf("recovery: unknown lost asset %q", scenario.Asset)
	}
	if strings.TrimSpace(scenario.Owner) == "" {
		return GameDayReport{}, errors.New("recovery: decision owner is required")
	}
	if strings.TrimSpace(scenario.DegradedMode) == "" {
		return GameDayReport{}, errors.New("recovery: safe degraded mode is required")
	}
	if scenario.BudgetRPO <= 0 || scenario.BudgetRTO <= 0 {
		return GameDayReport{}, errors.New("recovery: RPO and RTO budgets must be positive")
	}
	for _, fence := range []string{scenario.Fence, scenario.CutoverFence, scenario.FailbackFence} {
		if strings.TrimSpace(fence) == "" {
			return GameDayReport{}, errors.New("recovery: cutover, failback and drill fences are required")
		}
		if isProductionDestination(fence) {
			return GameDayReport{}, fmt.Errorf("recovery: failure injection cannot target production: %s", fence)
		}
	}
	if in.ObservedRPO < 0 || in.ObservedRTO < 0 {
		return GameDayReport{}, errors.New("recovery: observed RPO and RTO cannot be negative")
	}
	report := GameDayReport{
		DrillID: scenario.DrillID, Asset: scenario.Asset,
		ObservedRPO: in.ObservedRPO, ObservedRTO: in.ObservedRTO, FailedBack: in.FailedBack,
		Status: GameDayPass,
	}
	fail := func(code, detail string) {
		report.Status = GameDayFailedGate
		report.Findings = append(report.Findings, GameDayFinding{Code: code, Detail: detail})
	}
	if in.ObservedRPO > scenario.BudgetRPO {
		fail("RPO_BUDGET_MISSED", fmt.Sprintf("observed RPO %s exceeds budget %s", in.ObservedRPO, scenario.BudgetRPO))
	}
	if in.ObservedRTO > scenario.BudgetRTO {
		fail("RTO_BUDGET_MISSED", fmt.Sprintf("observed RTO %s exceeds budget %s", in.ObservedRTO, scenario.BudgetRTO))
	}
	for _, artifact := range []struct{ name, value string }{
		{"incident", in.Evidence.Incident}, {"advisory", in.Evidence.Advisory},
		{"repair", in.Evidence.Repair}, {"post-review", in.Evidence.PostReview},
	} {
		if strings.TrimSpace(artifact.value) == "" {
			fail("EVIDENCE_MISSING", artifact.name+" evidence is required")
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Code != report.Findings[j].Code {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].Detail < report.Findings[j].Detail
	})
	report.Digest = digestGameDay(report, scenario, in.Evidence)
	return report, nil
}

func digestGameDay(report GameDayReport, scenario GameDayScenario, evidence GameDayEvidence) string {
	parts := []string{"recovery004-gameday", scenario.DrillID, string(scenario.Asset), scenario.Owner,
		scenario.DegradedMode, scenario.BudgetRPO.String(), scenario.BudgetRTO.String(),
		scenario.Fence, scenario.CutoverFence, scenario.FailbackFence,
		string(report.Status), report.ObservedRPO.String(), report.ObservedRTO.String(),
		fmt.Sprint(report.FailedBack), evidence.Incident, evidence.Advisory, evidence.Repair, evidence.PostReview}
	for _, finding := range report.Findings {
		parts = append(parts, finding.Code+"\x00"+finding.Detail)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GameDayCell records executed drill IDs so a restarted game-day never
// double-counts a drill. It is safe for concurrent use.
type GameDayCell struct {
	mu     sync.Mutex
	drills map[string]GameDayReport
}

// NewGameDayCell starts an empty drill cell.
func NewGameDayCell() *GameDayCell {
	return &GameDayCell{drills: map[string]GameDayReport{}}
}

// Record executes one drill and files its report exactly once per drill ID.
func (c *GameDayCell) Record(in GameDayInput) (GameDayReport, error) {
	report, err := ExecuteGameDay(in)
	if err != nil {
		return GameDayReport{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, seen := c.drills[report.DrillID]; seen {
		return prior, nil
	}
	c.drills[report.DrillID] = report
	return report, nil
}
