package agentqualification

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// MatrixProfiles names every workload profile a capacity matrix must measure
// before a scale claim is accepted. A single profile, however large, never
// proves that noisy tenants, quota pressure or scheduled bursts leave chat
// and workflow capacity intact.
var MatrixProfiles = []string{"tiny", "peak", "pooled", "dedicated"}

// Capacity decision actions recorded per profile.
const (
	DecisionHold         = "hold"
	DecisionScaleOut     = "scale-out"
	DecisionMove         = "move"
	DecisionShedOptional = "shed-optional"
)

// CapacityBudgets bounds one profile. Zero budgets are not unconstrained;
// every budget must be positive before a matrix may run.
type CapacityBudgets struct {
	MaxP99           time.Duration
	MaxQueueAge      time.Duration
	MaxPoolWait      time.Duration
	MaxTimerLateness time.Duration
	MaxCostMicros    int64
}

func validBudgets(budgets CapacityBudgets) bool {
	return budgets.MaxP99 > 0 && budgets.MaxQueueAge > 0 && budgets.MaxPoolWait > 0 && budgets.MaxTimerLateness > 0 && budgets.MaxCostMicros > 0
}

// TenantAdmission records fair-admission evidence per tenant: admitted work
// proves the tenant was served, shed work proves pressure was absorbed by
// shedding rather than by starving another tenant.
type TenantAdmission struct {
	Tenant         string
	Admitted, Shed int
}

// ScaleDecision records the scale-out/move threshold outcome for one profile.
type ScaleDecision struct {
	Action, Reason string
}

// ProfileCapacity is the measured capacity record for one profile. Worst-case
// pointers stay nil when the underlying value was never observed so an
// unobserved value differs from observed zero.
type ProfileCapacity struct {
	Profile                           string
	Budgets                           CapacityBudgets
	Report                            Report
	P50, P95, P99                     time.Duration
	KindP99                           map[string]time.Duration
	QueueAge, PoolWait, TimerLateness *time.Duration
	TotalCost                         *int64
	Admissions                        []TenantAdmission
	Decision                          ScaleDecision
}

// Matrix is the signed workload matrix across every qualification profile.
type Matrix struct {
	ID                    string
	Plans                 []SignedPlan
	StartedAt, FinishedAt time.Time
	Capacities            []ProfileCapacity
	Status                string
	Findings              []string
	Issuer                string
}

// SignedMatrix attests measured matrix bytes.
type SignedMatrix struct {
	Matrix    Matrix
	Signature []byte
}

// RunMatrix measures every qualification profile and records per-profile
// latency percentiles, queue age, fair admission, cost, pool wait, timer
// lateness and the scale-out/move threshold decision. Plans may arrive in any
// order but must cover every profile exactly once.
func RunMatrix(ctx context.Context, id string, plans []SignedPlan, budgets map[string]CapacityBudgets, keys map[string]ed25519.PublicKey, operations map[string]Operation) (Matrix, error) {
	started := time.Now().UTC()
	if ctx == nil || id == "" || len(plans) != len(MatrixProfiles) {
		return Matrix{}, ErrEvidence
	}
	seen := make(map[string]bool)
	for _, plan := range plans {
		profile := plan.Plan.Profile
		if seen[profile] || !validBudgets(budgets[profile]) {
			return Matrix{}, ErrEvidence
		}
		seen[profile] = true
	}
	for _, profile := range MatrixProfiles {
		if !seen[profile] {
			return Matrix{}, ErrEvidence
		}
	}
	matrix := Matrix{ID: id, Plans: append([]SignedPlan(nil), plans...), StartedAt: started}
	for _, plan := range plans {
		report, err := Run(ctx, plan, keys, operations)
		if err != nil {
			return Matrix{}, err
		}
		matrix.Capacities = append(matrix.Capacities, summarizeCapacity(plan.Plan.Profile, report, budgets[plan.Plan.Profile]))
	}
	matrix.FinishedAt = time.Now().UTC()
	matrix.Findings = assessMatrix(matrix)
	matrix.Status = "MEASURED"
	if len(matrix.Findings) != 0 {
		matrix.Status = "UNKNOWN"
	}
	return matrix, nil
}

// SignMatrix attests measured matrix bytes using a reviewer distinct from
// every plan author. It cannot repair incomplete or unmeasured profiles.
func SignMatrix(matrix Matrix, issuer string, key ed25519.PrivateKey) (SignedMatrix, error) {
	if issuer == "" || len(key) != ed25519.PrivateKeySize || !validMatrix(matrix) {
		return SignedMatrix{}, ErrEvidence
	}
	for _, plan := range matrix.Plans {
		if issuer == plan.Plan.Issuer {
			return SignedMatrix{}, ErrEvidence
		}
	}
	matrix.Issuer = issuer
	data, err := json.Marshal(matrix)
	if err != nil {
		return SignedMatrix{}, err
	}
	return SignedMatrix{matrix, ed25519.Sign(key, append([]byte("hcm-agent-capacity-matrix-v1\x00"), data...))}, nil
}

// VerifyMatrix checks the reviewer signature, the plan trust anchors and that
// the recorded capacities still match the measured reports.
func VerifyMatrix(matrix SignedMatrix, planKeys, reviewerKeys map[string]ed25519.PublicKey, now time.Time) error {
	m := matrix.Matrix
	if m.Issuer == "" || !validMatrix(m) || m.FinishedAt.After(now) {
		return ErrEvidence
	}
	for _, plan := range m.Plans {
		if m.Issuer == plan.Plan.Issuer || VerifyPlan(plan, planKeys, now) != nil {
			return ErrEvidence
		}
		if key := reviewerKeys[m.Issuer]; len(key) == ed25519.PublicKeySize && bytes.Equal(key, planKeys[plan.Plan.Issuer]) {
			return ErrEvidence
		}
	}
	data, err := json.Marshal(m)
	key := reviewerKeys[m.Issuer]
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte("hcm-agent-capacity-matrix-v1\x00"), data...), matrix.Signature) {
		return ErrEvidence
	}
	return nil
}

func summarizeCapacity(profile string, report Report, budgets CapacityBudgets) ProfileCapacity {
	capacity := ProfileCapacity{Profile: profile, Budgets: budgets, Report: report, KindP99: make(map[string]time.Duration)}
	durations := make([]time.Duration, len(report.Samples))
	byKind := make(map[string][]time.Duration)
	admitted, shed := make(map[string]int), make(map[string]int)
	tenants := make(map[string]bool)
	var queueAge, poolWait, timerLateness time.Duration
	var queueSeen, poolSeen, timerSeen, costSeen bool
	var totalCost int64
	for index, sample := range report.Samples {
		durations[index] = sample.Elapsed
		byKind[sample.Job.Kind] = append(byKind[sample.Job.Kind], sample.Elapsed)
		tenants[sample.Job.Tenant] = true
		observation := sample.Observation
		for _, resource := range observation.Resources {
			if resource.Outcome == "ADMITTED" {
				admitted[sample.Job.Tenant]++
			} else if resource.Outcome != "" {
				shed[sample.Job.Tenant]++
			}
		}
		if observation.QueueAge != nil {
			queueSeen = true
			queueAge = max(queueAge, *observation.QueueAge)
		}
		if observation.PoolWait != nil {
			poolSeen = true
			poolWait = max(poolWait, *observation.PoolWait)
		}
		if observation.TimerLateness != nil {
			timerSeen = true
			timerLateness = max(timerLateness, *observation.TimerLateness)
		}
		if observation.CostMicros != nil {
			costSeen = true
			totalCost += *observation.CostMicros
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	capacity.P50, capacity.P95, capacity.P99 = percentile(durations, .50), percentile(durations, .95), percentile(durations, .99)
	for kind, samples := range byKind {
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		capacity.KindP99[kind] = percentile(samples, .99)
	}
	if queueSeen {
		capacity.QueueAge = &queueAge
	}
	if poolSeen {
		capacity.PoolWait = &poolWait
	}
	if timerSeen {
		capacity.TimerLateness = &timerLateness
	}
	if costSeen {
		capacity.TotalCost = &totalCost
	}
	names := make([]string, 0, len(tenants))
	for tenant := range tenants {
		names = append(names, tenant)
	}
	sort.Strings(names)
	for _, tenant := range names {
		capacity.Admissions = append(capacity.Admissions, TenantAdmission{Tenant: tenant, Admitted: admitted[tenant], Shed: shed[tenant]})
	}
	capacity.Decision = decideCapacity(profile, capacity)
	return capacity
}

// decideCapacity applies the capacity thresholds. Optional model inference
// sheds before chat or workflow critical-path capacity grows; pool pressure
// on the shared profile moves tenants to dedicated capacity instead.
func decideCapacity(profile string, capacity ProfileCapacity) ScaleDecision {
	if len(capacity.Report.Findings) != 0 {
		return ScaleDecision{Action: DecisionHold, Reason: "measurement unknown; no capacity decision"}
	}
	budgets := capacity.Budgets
	for _, kind := range []string{"chat", "workflow", "schedule"} {
		if capacity.KindP99[kind] > budgets.MaxP99 {
			return ScaleDecision{Action: DecisionScaleOut, Reason: fmt.Sprintf("%s p99 %s exceeds budget %s", kind, capacity.KindP99[kind], budgets.MaxP99)}
		}
	}
	if capacity.QueueAge != nil && *capacity.QueueAge > budgets.MaxQueueAge {
		return ScaleDecision{Action: DecisionScaleOut, Reason: fmt.Sprintf("queue age %s exceeds budget %s", *capacity.QueueAge, budgets.MaxQueueAge)}
	}
	if capacity.TimerLateness != nil && *capacity.TimerLateness > budgets.MaxTimerLateness {
		return ScaleDecision{Action: DecisionScaleOut, Reason: fmt.Sprintf("timer lateness %s exceeds budget %s", *capacity.TimerLateness, budgets.MaxTimerLateness)}
	}
	if capacity.PoolWait != nil && *capacity.PoolWait > budgets.MaxPoolWait {
		if profile == "pooled" {
			return ScaleDecision{Action: DecisionMove, Reason: fmt.Sprintf("pool wait %s exceeds shared budget %s; move tenant to dedicated capacity", *capacity.PoolWait, budgets.MaxPoolWait)}
		}
		return ScaleDecision{Action: DecisionScaleOut, Reason: fmt.Sprintf("pool wait %s exceeds budget %s", *capacity.PoolWait, budgets.MaxPoolWait)}
	}
	if capacity.KindP99["model"] > budgets.MaxP99 {
		return ScaleDecision{Action: DecisionShedOptional, Reason: fmt.Sprintf("model p99 %s exceeds budget %s; shed optional inference first", capacity.KindP99["model"], budgets.MaxP99)}
	}
	if capacity.TotalCost != nil && *capacity.TotalCost > budgets.MaxCostMicros {
		return ScaleDecision{Action: DecisionShedOptional, Reason: fmt.Sprintf("cost %d micros exceeds budget %d; shed optional inference first", *capacity.TotalCost, budgets.MaxCostMicros)}
	}
	return ScaleDecision{Action: DecisionHold, Reason: "all profiles within signed budgets"}
}

func assessMatrix(matrix Matrix) []string {
	findings := make([]string, 0)
	for _, capacity := range matrix.Capacities {
		for _, finding := range capacity.Report.Findings {
			findings = append(findings, capacity.Profile+": "+finding)
		}
	}
	return findings
}

func validMatrix(matrix Matrix) bool {
	if matrix.ID == "" || len(matrix.Plans) != len(MatrixProfiles) || len(matrix.Capacities) != len(MatrixProfiles) || matrix.StartedAt.IsZero() || matrix.FinishedAt.Before(matrix.StartedAt) {
		return false
	}
	seen := make(map[string]bool)
	for index, plan := range matrix.Plans {
		capacity := matrix.Capacities[index]
		if validatePlan(plan.Plan) != nil || !validReport(capacity.Report) || !validBudgets(capacity.Budgets) {
			return false
		}
		if capacity.Profile != plan.Plan.Profile || seen[capacity.Profile] {
			return false
		}
		if capacity.Report.Plan.Plan.ID != plan.Plan.ID || !bytes.Equal(capacity.Report.Plan.Signature, plan.Signature) {
			return false
		}
		seen[capacity.Profile] = true
		expected := summarizeCapacity(capacity.Profile, capacity.Report, capacity.Budgets)
		left, _ := json.Marshal(expected)
		right, _ := json.Marshal(capacity)
		if string(left) != string(right) {
			return false
		}
	}
	for _, profile := range MatrixProfiles {
		if !seen[profile] {
			return false
		}
	}
	findings := assessMatrix(matrix)
	left, _ := json.Marshal(findings)
	right, _ := json.Marshal(matrix.Findings)
	wantStatus := "MEASURED"
	if len(findings) > 0 {
		wantStatus = "UNKNOWN"
	}
	return string(left) == string(right) && matrix.Status == wantStatus
}
