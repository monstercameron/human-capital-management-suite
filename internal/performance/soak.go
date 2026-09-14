// Staged soak harness: PERF-003 runs a 24-virtual-hour load shape —
// baseline, ramp, peak, double burst, restart and drain — through the
// real deterministic admission engine, sampling real pilot-path
// latency every virtual hour.
//
// Virtual hours compress wall time: offered units are modeled counts
// at a declared command scale, while every admission verdict comes
// from admission.Decide and every hourly p99 comes from measured
// operations. A measured breach, an undersized peak model, a leak, a
// duplicate effect or hour-1-to-24 deterioration beyond 10% rejects
// with PERF_003_REJECTED naming field, state and version. The engine
// is pure: plans are never aliased and nothing is persisted.
package performance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// SoakRejectedCode is the stable machine-readable refusal code.
const SoakRejectedCode = "PERF_003_REJECTED"

// SoakHours is the required virtual-hour span of every soak.
const SoakHours = 24

// DeteriorationNumerator/DeteriorationDenominator bound hour-1-to-24
// growth: hour 24 must not exceed hour 1 by more than 10%.
const (
	DeteriorationNumerator   = 11
	DeteriorationDenominator = 10
)

// SoakRejectedError names the offending field, state and vocabulary
// version of a refused soak.
type SoakRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *SoakRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsSoakRejected unwraps a PERF_003_REJECTED refusal.
func AsSoakRejected(err error) (*SoakRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	var rejected *SoakRejectedError
	if errors.As(err, &rejected) && rejected.Code == SoakRejectedCode {
		return rejected, true
	}
	return nil, false
}

func soakReject(field, state string) *SoakRejectedError {
	return &SoakRejectedError{Code: SoakRejectedCode, Field: field, State: state, Version: SchemaVersion}
}

// SoakStage is one named load step. Burst doubles the offered units:
// a spike beyond the declared rate. Restart bumps the placement
// epoch mid-soak: queued work carries across, leases persist.
type SoakStage struct {
	Name         string
	VirtualHours int
	OfferedUnits int
	Burst        bool
	Restart      bool
}

// SoakPlan is the fully declared soak. DeclaredPeakCommandsPerSec
// must clear the published PEAK envelope floor: an undersized peak
// model rejects instead of passing quietly. UnitCommandsPerSec
// documents the compression scale one modeled unit represents.
type SoakPlan struct {
	Tenant                     string
	Cell                       string
	QuotaLimit                 int
	CapacityPerHour            int
	RetryAllowance             int
	DeclaredPeakCommandsPerSec int
	UnitCommandsPerSec         int
	Stages                     []SoakStage
	P99Ceiling                 time.Duration
	MaxErrorRate               float64
	OpsPerHour                 int
}

func (p SoakPlan) hourlyRate(stage SoakStage) int {
	if stage.Burst {
		return stage.OfferedUnits * 2
	}
	return stage.OfferedUnits
}

// Validate implements validation. Every refusal carries
// PERF_003_REJECTED with the offending field and state.
func (p SoakPlan) Validate() error {
	if strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Cell) == "" {
		return soakReject("tenant/cell", "missing")
	}
	if p.QuotaLimit <= 0 || p.CapacityPerHour <= 0 {
		return soakReject("quota/capacity", "non-positive")
	}
	if p.RetryAllowance < 0 || p.UnitCommandsPerSec < 1 {
		return soakReject("retry/scale", "non-positive")
	}
	// Hourly sampling needs at least two batches for a distribution.
	if p.OpsPerHour < 2*soakBatchOps {
		return soakReject("ops_per_hour", "too-few-for-distribution")
	}
	// Ten batches per hour keeps the hourly p99 a distribution, not
	// one sample.
	if p.OpsPerHour < 10*soakBatchOps {
		return soakReject("ops_per_hour", "too-few-for-p99")
	}
	if p.P99Ceiling <= 0 || p.MaxErrorRate < 0 || p.MaxErrorRate > 1 {
		return soakReject("latency-budget", "invalid")
	}
	if len(p.Stages) < 3 {
		return soakReject("stages", "fewer-than-3")
	}
	if p.DeclaredPeakCommandsPerSec < Peak.CommandsPerSecond {
		return soakReject("declared_peak_commands_per_sec", "undersized")
	}
	names := make(map[string]struct{}, len(p.Stages))
	var hours, floor, ceiling int
	var burst, restart bool
	for i, stage := range p.Stages {
		if strings.TrimSpace(stage.Name) == "" {
			return soakReject(fmt.Sprintf("stages/%d/name", i), "missing")
		}
		if _, ok := names[stage.Name]; ok {
			return soakReject(fmt.Sprintf("stages/%d/name", i), "duplicate")
		}
		names[stage.Name] = struct{}{}
		if stage.VirtualHours < 1 || stage.OfferedUnits < 0 {
			return soakReject(fmt.Sprintf("stages/%d", i), "non-positive")
		}
		rate := p.hourlyRate(stage)
		if i == 0 || rate < floor {
			floor = rate
		}
		if i == 0 || rate > ceiling {
			ceiling = rate
		}
		hours += stage.VirtualHours
		burst = burst || stage.Burst
		restart = restart || stage.Restart
	}
	if hours != SoakHours {
		return soakReject("stages", "not-24-virtual-hours")
	}
	if !burst {
		return soakReject("stages", "no-burst")
	}
	if !restart {
		return soakReject("stages", "no-restart")
	}
	if floor <= 0 || ceiling < 2*floor {
		return soakReject("stages", "no-2x-peak")
	}
	return nil
}

// SoakHour is the modeled and measured account of one virtual hour.
type SoakHour struct {
	Hour           int
	Stage          string
	Offered        int
	Admitted       int
	Degraded       int
	Queued         int
	Rejected       int
	Retried        int
	Dropped        int
	QueueDepth     int
	EffectsApplied int
	// P99 is the hourly tail, gated against the absolute ceiling.
	// Median drives the deterioration trend: tails swing with
	// single hiccups, medians move only with systematic shifts.
	P99    time.Duration
	Median time.Duration
	Errors int
}

// SoakVerdict is the deterministic soak account. The digest covers
// modeled counts only — measured latencies are reported, never
// digested — so identical plans replay to identical digests.
type SoakVerdict struct {
	PlanDigest       string
	Hours            []SoakHour
	TotalOffered     int
	TotalAdmitted    int
	TotalRejected    int
	TotalRetried     int
	TotalDropped     int
	TotalEffects     int
	LeasesGranted    int
	LeasesReleased   int
	DuplicateEffects int
	// BaselineP99 and ClosingP99 are the window medians of hourly
	// medians for hours 1-3 and 22-24, reported for diagnosis. The
	// gate fits a Theil-Sen trend over all 24 hourly medians:
	// hiccups cannot move either layer, while systematic drift
	// moves most pairwise slopes.
	BaselineP99 time.Duration
	ClosingP99  time.Duration
	Digest      string
}

// HourP99 returns the sampled p99 of virtual hour n (1-based).
func (v SoakVerdict) HourP99(n int) time.Duration {
	if n < 1 || n > len(v.Hours) {
		return 0
	}
	return v.Hours[n-1].P99
}

// QueueDepth returns the closing queue depth of virtual hour n.
func (v SoakVerdict) QueueDepth(n int) int {
	if n < 1 || n > len(v.Hours) {
		return -1
	}
	return v.Hours[n-1].QueueDepth
}

type queuedUnit struct {
	id    string
	lease bool
}

type soakUnit struct {
	id       string
	critical admission.Criticality
	retry    bool
}

// RunSoak executes one soak plan. It is pure: the plan is copied on
// entry and nothing is persisted.
func RunSoak(plan SoakPlan) (SoakVerdict, error) {
	if err := plan.Validate(); err != nil {
		return SoakVerdict{}, err
	}
	stages := append([]SoakStage(nil), plan.Stages...)
	engine := &soakEngine{plan: plan, epoch: 1}
	for _, stage := range stages {
		rate := plan.hourlyRate(stage)
		for h := 0; h < stage.VirtualHours; h++ {
			if stage.Restart && h == 0 {
				engine.epoch++
			}
			if err := engine.runHour(stage.Name, rate); err != nil {
				return SoakVerdict{}, err
			}
		}
	}
	return engine.finish()
}

type soakEngine struct {
	plan       SoakPlan
	epoch      uint64
	hour       int
	seq        int
	served     int
	queue      []queuedUnit
	retries    []soakUnit
	granted    int
	released   int
	applied    map[string]struct{}
	duplicates int
	hours      []SoakHour
}

func (e *soakEngine) nextID() string {
	e.seq++
	return fmt.Sprintf("%s/%s/e%d/h%02d/u%05d", e.plan.Tenant, e.plan.Cell, e.epoch, e.hour, e.seq)
}

func (e *soakEngine) snapshot(admitted, retried int) admission.Snapshot {
	return admission.Snapshot{
		TenantID: e.plan.Tenant, CellID: e.plan.Cell, PlacementEpoch: e.epoch,
		Quota:      admission.Quota{Known: true, Version: "soak-v1", Limit: e.plan.QuotaLimit, Consumed: admitted, Pending: len(e.queue)},
		Capacity:   e.plan.CapacityPerHour - e.served,
		ReservedP0: 0, RetryRemaining: e.plan.RetryAllowance - retried,
	}
}

func (e *soakEngine) request(unit soakUnit) admission.Request {
	retry := 0
	if unit.retry {
		retry = 1
	}
	return admission.Request{
		TenantID: e.plan.Tenant, CellID: e.plan.Cell, PlacementEpoch: e.epoch,
		Criticality: unit.critical, EstimatedCost: 1,
		RetryBudgetID: "soak-retry", RetryAttempt: retry,
	}
}

// applyEffect records one applied effect; a repeated id is a
// duplicate-effect defect, counted rather than hidden.
func (e *soakEngine) applyEffect(id string) {
	if e.applied == nil {
		e.applied = make(map[string]struct{})
	}
	if _, ok := e.applied[id]; ok {
		e.duplicates++
		return
	}
	e.applied[id] = struct{}{}
	e.released++
}

func (e *soakEngine) runHour(stage string, offered int) error {
	e.hour++
	report := SoakHour{Hour: e.hour, Stage: stage}
	// Drain queued work first, oldest first.
	drained := 0
	for len(e.queue) > 0 && drained < e.plan.CapacityPerHour {
		unit := e.queue[0]
		e.queue = e.queue[1:]
		e.applyEffect(unit.id)
		drained++
		report.EffectsApplied++
	}
	e.served = drained
	admitted := 0
	retried := 0
	arrivals := make([]soakUnit, 0, offered+len(e.retries))
	arrivals = append(arrivals, e.retries...)
	e.retries = nil
	for i := 0; i < offered; i++ {
		unit := soakUnit{id: e.nextID(), critical: soakCriticality(i)}
		arrivals = append(arrivals, unit)
	}
	report.Offered = len(arrivals)
	for _, unit := range arrivals {
		decision := admission.Decide(e.request(unit), e.snapshot(admitted, retried), admission.Policy{})
		switch decision.Outcome {
		case admission.Admit:
			admitted++
			e.served++
			e.granted++
			e.applyEffect(unit.id)
			report.Admitted++
			report.EffectsApplied++
		case admission.Degrade:
			// The admission receipt reserves nothing for degraded
			// work (reservation 0): it neither consumes quota nor
			// reserved capacity, so served/admitted stay untouched.
			e.granted++
			e.applyEffect(unit.id)
			report.Degraded++
			report.EffectsApplied++
		case admission.Queue:
			e.granted++
			e.queue = append(e.queue, queuedUnit{id: unit.id, lease: true})
			report.Queued++
		case admission.Defer, admission.Reject, admission.Shed:
			report.Rejected++
			if decision.Outcome == admission.Reject && strings.Contains(decision.Reason, "INVALID") {
				return fmt.Errorf("performance: admission refused valid soak unit: %s", decision.Reason)
			}
			if unit.critical == admission.P4 || retried >= e.plan.RetryAllowance {
				report.Dropped++
				continue
			}
			unit.retry = true
			e.retries = append(e.retries, unit)
			report.Retried++
		default:
			return fmt.Errorf("performance: unknown admission outcome %q", decision.Outcome)
		}
	}
	// Sample real pilot-path latency for this virtual hour. An
	// unsampled hour fails closed: latency without a clock proves
	// no bound.
	median, p99, errs, ok := sampleHourOps(e.plan.OpsPerHour)
	if !ok {
		return soakReject(fmt.Sprintf("hours/%d/latency", e.hour), "unsampled")
	}
	report.Median = median
	report.P99 = p99
	report.Errors = errs
	if err := checkHourBudget(e.hour, p99, errs, e.plan.OpsPerHour, e.plan.P99Ceiling, e.plan.MaxErrorRate); err != nil {
		return err
	}
	report.QueueDepth = len(e.queue)
	e.hours = append(e.hours, report)
	return nil
}

// checkHourBudget is the pure hourly gate: error-rate excess or a
// p99 over the ceiling rejects with the offending hour.
func checkHourBudget(hour int, p99 time.Duration, errs, ops int, ceiling time.Duration, maxRate float64) error {
	if ops <= 0 {
		return soakReject(fmt.Sprintf("hours/%d/latency", hour), "unsampled")
	}
	if rate := float64(errs) / float64(ops); rate > maxRate {
		return soakReject(fmt.Sprintf("hours/%d/errors", hour), "budget-breach")
	}
	if p99 > ceiling {
		return soakReject(fmt.Sprintf("hours/%d/p99", hour), "budget-breach")
	}
	return nil
}

// soakCriticality mixes tiers deterministically: mostly P1-P3 pressure
// traffic with every 25th unit a sheddable P4 probe.
func soakCriticality(i int) admission.Criticality {
	if i%25 == 24 {
		return admission.P4
	}
	return []admission.Criticality{admission.P1, admission.P2, admission.P2, admission.P3}[i%4]
}

// soakBatchOps groups light probes into batches large enough that
// absolute-time hiccups (scheduler quanta, GC assists) dilute to
// negligible fractions of each batch total. Per-op means derive from
// exact division of clean batch totals, so sub-tick probes still
// yield accurate magnitudes without heat-generating busywork.
const soakBatchOps = 2000

// soakLightProbe alternates the two pilot-path micro-operations. Both
// are infallible by construction; failures would surface through
// injected faults, never here.
func soakLightProbe(i int) error {
	if i%2 == 0 {
		return Small.Validate()
	}
	return digestPayload()
}

// soakSpareBatchAttempts bounds frozen-clock resampling per hour:
// 90 spare batches ride through multi-hundred-millisecond clock
// freezes without stretching a healthy hour.
const soakSpareBatchAttempts = 90

// sampleHourOps times ops light probes in batches and returns the
// hourly median and nearest-rank p99 of batch per-op means with the
// error count. ok is false when a full quorum of genuine batches
// could not be collected: the hour is unsampled and fails closed,
// never reported with a fabricated distribution.
//
// GC stays suppressed inside the sampling window: stop-the-world
// pauses are millisecond-scale events that would otherwise dominate
// microsecond batch means at random hours. The gate measures mutator
// latency; GC behavior is sized separately (PERF-005), and collection
// runs normally outside sampling.
//
// A zero batch mean is a frozen-clock sample, never a fast batch:
// soakBatchOps probes include soakBatchOps/2 SHA-256/KiB hashes, so a
// batch total under soakBatchOps nanoseconds — under one nanosecond
// per probe — is physically impossible. Such samples are discarded
// and the batch is retried; glitch batches are not errors, so they
// never touch errs or the error-rate budget.
func sampleHourOps(ops int) (median, p99 time.Duration, errs int, ok bool) {
	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)
	batches := (ops + soakBatchOps - 1) / soakBatchOps
	means := make([]time.Duration, 0, batches)
	completed := 0
	for attempts := 0; completed < batches && attempts < batches+soakSpareBatchAttempts; attempts++ {
		n := soakBatchOps
		if remaining := ops - completed*soakBatchOps; remaining < n {
			n = remaining
		}
		start := time.Now()
		for i := 0; i < n; i++ {
			if err := soakLightProbe(i); err != nil {
				errs++
			}
		}
		if mean := time.Since(start) / time.Duration(n); mean > 0 {
			means = append(means, mean)
			completed++
		}
	}
	if completed < batches {
		return 0, 0, errs, false
	}
	sort.Slice(means, func(i, j int) bool { return means[i] < means[j] })
	rank := len(means)*99 + 99
	rank /= 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(means) {
		rank = len(means)
	}
	return means[len(means)/2], means[rank-1], errs, true
}

func (e *soakEngine) finish() (SoakVerdict, error) {
	verdict := SoakVerdict{PlanDigest: digestSoakPlan(e.plan), Hours: e.hours}
	for _, h := range e.hours {
		verdict.TotalOffered += h.Offered
		verdict.TotalAdmitted += h.Admitted + h.Degraded
		verdict.TotalRejected += h.Rejected
		verdict.TotalRetried += h.Retried
		verdict.TotalDropped += h.Dropped
		verdict.TotalEffects += h.EffectsApplied
	}
	verdict.LeasesGranted = e.granted
	verdict.LeasesReleased = e.released
	verdict.DuplicateEffects = e.duplicates
	// Leftover retries at soak end are dropped work, counted loudly.
	verdict.TotalDropped += len(e.retries)
	if len(e.queue) > 0 {
		return SoakVerdict{}, soakReject("queue", "undrained")
	}
	if verdict.DuplicateEffects != 0 {
		return SoakVerdict{}, soakReject("effects", "duplicates")
	}
	if verdict.LeasesGranted != verdict.LeasesReleased {
		return SoakVerdict{}, soakReject("leases", "drift")
	}
	if verdict.TotalRetried > verdict.TotalRejected {
		return SoakVerdict{}, soakReject("retries", "amplified")
	}
	verdict.BaselineP99 = medianHourly(e.hours[0], e.hours[1], e.hours[2])
	verdict.ClosingP99 = medianHourly(e.hours[len(e.hours)-3], e.hours[len(e.hours)-2], e.hours[len(e.hours)-1])
	if theilSenBreach(e.hours, verdict.BaselineP99) {
		return SoakVerdict{}, soakReject("deterioration/p99", "beyond-10pct")
	}
	firstQueue, lastQueue := e.hours[0].QueueDepth, e.hours[len(e.hours)-1].QueueDepth
	if firstQueue == 0 {
		if lastQueue != 0 {
			return SoakVerdict{}, soakReject("deterioration/queue", "beyond-10pct")
		}
	} else if lastQueue*DeteriorationDenominator > firstQueue*DeteriorationNumerator {
		return SoakVerdict{}, soakReject("deterioration/queue", "beyond-10pct")
	}
	verdict.Digest = digestVerdict(verdict)
	return verdict, nil
}

// trendSlopeFloorPerHour is the absolute noise floor under the
// deterioration allowance, in nanoseconds of hourly drift. Shared
// hosts jitter microsecond batch means by single-digit nanoseconds
// per hour with no systematic drift (measured Theil-Sen medians of
// ±9ns/h across clean soaks against a ~8ns/h relative allowance),
// so the relative term alone flips on noise. The floor keeps the
// gate's verdict about systematic drift, not host jitter, while
// staying 4x below the level that would move any pinned trend case
// (flat, +9% pass, +15% breach, spike immunity at 100µs anchors).
const trendSlopeFloorPerHour = float64(100 * time.Nanosecond)

// theilSenBreach fits a Theil-Sen trend — the median of all pairwise
// hourly slopes — over the 24 hourly medians and reports whether it
// exceeds the hour-1-to-24 allowance: 10% of the anchor across the
// span, or the host-jitter floor, whichever is larger. Fewer than
// half the hours can be transients without moving the median, so
// hiccups (even multi-hour ones) cannot fabricate a breach, while
// systematic drift shifts most pairs and always trips it.
func theilSenBreach(hours []SoakHour, anchor time.Duration) bool {
	if len(hours) < 2 || anchor <= 0 {
		return len(hours) >= 2
	}
	slopes := make([]float64, 0, len(hours)*(len(hours)-1)/2)
	for i := 0; i < len(hours); i++ {
		for j := i + 1; j < len(hours); j++ {
			slopes = append(slopes, float64(hours[j].Median-hours[i].Median)/float64(j-i))
		}
	}
	sort.Float64s(slopes)
	median := slopes[len(slopes)/2]
	allowance := 0.10 * float64(anchor) / float64(len(hours)-1)
	if allowance < trendSlopeFloorPerHour {
		allowance = trendSlopeFloorPerHour
	}
	return median > allowance*(1+1e-9)
}

// medianHourly is the middle hourly median of three hours.
func medianHourly(hours ...SoakHour) time.Duration {
	sorted := []time.Duration{hours[0].Median, hours[1].Median, hours[2].Median}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[1]
}

func digestSoakPlan(plan SoakPlan) string {
	parts := []string{"perf003-plan", plan.Tenant, plan.Cell,
		fmt.Sprint(plan.QuotaLimit, plan.CapacityPerHour, plan.RetryAllowance,
			plan.DeclaredPeakCommandsPerSec, plan.UnitCommandsPerSec, plan.OpsPerHour)}
	for _, stage := range plan.Stages {
		parts = append(parts, fmt.Sprintf("%s/%d/%d/b%v/r%v",
			stage.Name, stage.VirtualHours, stage.OfferedUnits, stage.Burst, stage.Restart))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestVerdict(verdict SoakVerdict) string {
	parts := []string{"perf003-verdict", verdict.PlanDigest,
		fmt.Sprint(verdict.TotalOffered, verdict.TotalAdmitted, verdict.TotalRejected,
			verdict.TotalRetried, verdict.TotalDropped, verdict.TotalEffects,
			verdict.LeasesGranted, verdict.LeasesReleased, verdict.DuplicateEffects)}
	for _, h := range verdict.Hours {
		parts = append(parts, fmt.Sprintf("%d/%s/%d/%d/%d/%d/%d/%d/%d/%d",
			h.Hour, h.Stage, h.Offered, h.Admitted, h.Degraded, h.Queued,
			h.Rejected, h.Retried, h.Dropped, h.EffectsApplied))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
