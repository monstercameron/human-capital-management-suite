// Package agentqualification measures agent workloads without substituting
// synthetic latency or inferred durability for observed application outcomes.
package agentqualification

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

var ErrEvidence = errors.New("agent qualification: invalid or untrusted evidence")

// Job pins a workload and tenant before measurement starts.
type Job struct {
	ID, Tenant, Kind string
}

// Plan is an internal qualification envelope, never a customer SLO claim.
// Limits and review precede observations; zero limits are not unconstrained.
type Plan struct {
	ID, Profile, Runtime, Issuer string
	IssuedAt, ExpiresAt          time.Time
	Jobs                         []Job
	Concurrency                  int
	P99Limit                     time.Duration
}

type SignedPlan struct {
	Plan      Plan
	Signature []byte
}

// Observation contains references returned by the real operation. Optional
// measurements use pointers so an unobserved value differs from observed zero.
type Observation struct {
	Resources                         []ResourceAdmission
	DurableRef, Provider, Model       string
	QueueAge, PoolWait, TimerLateness *time.Duration
	CostMicros                        *int64
}

// ResourceAdmission is one admission event emitted by the composed runtime.
type ResourceAdmission struct {
	Tenant, User, Task, Lane, Provider, Outcome string
	PoolWait                                    time.Duration
}

type Operation func(context.Context, Job) (Observation, error)

type Sample struct {
	Job                   Job
	StartedAt, FinishedAt time.Time
	Elapsed               time.Duration
	Observation           Observation
	Failure               string
}

type Summary struct {
	Tenant, Kind  string
	Count, Failed int
	P50, P95, P99 time.Duration
}

type Report struct {
	Plan                  SignedPlan
	Issuer                string
	StartedAt, FinishedAt time.Time
	Samples               []Sample
	Summaries             []Summary
	Status                string
	Findings              []string
}

type SignedReport struct {
	Report    Report
	Signature []byte
}

func SignPlan(plan Plan, key ed25519.PrivateKey) (SignedPlan, error) {
	if err := validatePlan(plan); err != nil || len(key) != ed25519.PrivateKeySize {
		return SignedPlan{}, ErrEvidence
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return SignedPlan{}, err
	}
	return SignedPlan{plan, ed25519.Sign(key, append([]byte("hcm-agent-capacity-plan-v1\x00"), data...))}, nil
}

func VerifyPlan(plan SignedPlan, keys map[string]ed25519.PublicKey, now time.Time) error {
	if err := validatePlan(plan.Plan); err != nil || now.Before(plan.Plan.IssuedAt) || !now.Before(plan.Plan.ExpiresAt) {
		return ErrEvidence
	}
	data, err := json.Marshal(plan.Plan)
	key := keys[plan.Plan.Issuer]
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte("hcm-agent-capacity-plan-v1\x00"), data...), plan.Signature) {
		return ErrEvidence
	}
	return nil
}

// Run starts actual operations concurrently and preserves every result,
// including failed operations. Missing kinds or metrics produce UNKNOWN.
func Run(ctx context.Context, plan SignedPlan, keys map[string]ed25519.PublicKey, operations map[string]Operation) (Report, error) {
	started := time.Now().UTC()
	if ctx == nil || VerifyPlan(plan, keys, started) != nil {
		return Report{}, ErrEvidence
	}
	for _, job := range plan.Plan.Jobs {
		if operations[job.Kind] == nil {
			return Report{}, fmt.Errorf("%w: operation %s missing", ErrEvidence, job.Kind)
		}
	}
	report := Report{Plan: plan, StartedAt: started, Samples: make([]Sample, len(plan.Plan.Jobs))}
	workers := plan.Plan.Concurrency
	if workers > len(plan.Plan.Jobs) {
		workers = len(plan.Plan.Jobs)
	}
	jobs := make(chan int)
	var done sync.WaitGroup
	for range workers {
		done.Add(1)
		go func() {
			defer done.Done()
			for index := range jobs {
				job := plan.Plan.Jobs[index]
				start := time.Now()
				var observation Observation
				err := ctx.Err()
				if err == nil && !start.Before(plan.Plan.ExpiresAt) {
					err = ErrEvidence
				}
				if err == nil {
					observation, err = operations[job.Kind](ctx, job)
				}
				finish := time.Now()
				sample := Sample{Job: job, StartedAt: start.UTC(), FinishedAt: finish.UTC(), Elapsed: finish.Sub(start), Observation: observation}
				if err != nil {
					sample.Failure = err.Error()
				}
				report.Samples[index] = sample
			}
		}()
	}
	for index := range plan.Plan.Jobs {
		jobs <- index
	}
	close(jobs)
	done.Wait()
	report.FinishedAt = time.Now().UTC()
	report.Summaries, report.Findings = assess(report)
	report.Status = "MEASURED"
	if len(report.Findings) != 0 {
		report.Status = "UNKNOWN"
	}
	return report, nil
}

// SignReport attests measured bytes using a reviewer distinct from the plan
// author. It cannot turn UNKNOWN measurements into an accepted qualification.
func SignReport(report Report, issuer string, key ed25519.PrivateKey) (SignedReport, error) {
	if issuer == "" || issuer == report.Plan.Plan.Issuer || len(key) != ed25519.PrivateKeySize || !validReport(report) {
		return SignedReport{}, ErrEvidence
	}
	report.Issuer = issuer
	data, err := json.Marshal(report)
	if err != nil {
		return SignedReport{}, err
	}
	return SignedReport{report, ed25519.Sign(key, append([]byte("hcm-agent-capacity-report-v1\x00"), data...))}, nil
}

func VerifyReport(report SignedReport, planKeys, reviewerKeys map[string]ed25519.PublicKey, now time.Time) error {
	r := report.Report
	if r.Issuer == "" || r.Issuer == r.Plan.Plan.Issuer || !validReport(r) || r.FinishedAt.After(now) || VerifyPlan(r.Plan, planKeys, now) != nil {
		return ErrEvidence
	}
	data, err := json.Marshal(r)
	key := reviewerKeys[r.Issuer]
	if err != nil || len(key) != ed25519.PublicKeySize || bytes.Equal(key, planKeys[r.Plan.Plan.Issuer]) || !ed25519.Verify(key, append([]byte("hcm-agent-capacity-report-v1\x00"), data...), report.Signature) {
		return ErrEvidence
	}
	return nil
}

func validatePlan(plan Plan) error {
	if plan.ID == "" || plan.Runtime == "" || plan.Issuer == "" || plan.IssuedAt.IsZero() || !plan.ExpiresAt.After(plan.IssuedAt) || plan.Concurrency < 2 || plan.P99Limit <= 0 || len(plan.Jobs) < 2 {
		return ErrEvidence
	}
	switch plan.Profile {
	case "tiny", "peak", "pooled", "dedicated":
	default:
		return ErrEvidence
	}
	seen := make(map[string]bool)
	for _, job := range plan.Jobs {
		if job.ID == "" || job.Tenant == "" || seen[job.ID] {
			return ErrEvidence
		}
		switch job.Kind {
		case "model", "chat", "workflow", "schedule":
		default:
			return ErrEvidence
		}
		seen[job.ID] = true
	}
	return nil
}

func validReport(report Report) bool {
	if validatePlan(report.Plan.Plan) != nil || len(report.Samples) != len(report.Plan.Plan.Jobs) || report.StartedAt.IsZero() || report.StartedAt.Before(report.Plan.Plan.IssuedAt) || report.FinishedAt.Before(report.StartedAt) {
		return false
	}
	for index, sample := range report.Samples {
		if sample.Job != report.Plan.Plan.Jobs[index] || sample.Elapsed < 0 || sample.StartedAt.Before(report.StartedAt) || sample.FinishedAt.After(report.FinishedAt) || sample.FinishedAt.Before(sample.StartedAt) {
			return false
		}
	}
	summaries, findings := assess(report)
	left, _ := json.Marshal(struct {
		Summaries []Summary
		Findings  []string
	}{summaries, findings})
	right, _ := json.Marshal(struct {
		Summaries []Summary
		Findings  []string
	}{report.Summaries, report.Findings})
	wantStatus := "MEASURED"
	if len(findings) > 0 {
		wantStatus = "UNKNOWN"
	}
	return string(left) == string(right) && report.Status == wantStatus
}

func assess(report Report) ([]Summary, []string) {
	groups := make(map[string][]Sample)
	kinds, tenants := make(map[string]bool), make(map[string]bool)
	findings := make([]string, 0)
	for _, sample := range report.Samples {
		kinds[sample.Job.Kind], tenants[sample.Job.Tenant] = true, true
		groups[sample.Job.Tenant+"\x00"+sample.Job.Kind] = append(groups[sample.Job.Tenant+"\x00"+sample.Job.Kind], sample)
		if sample.Failure != "" {
			findings = append(findings, sample.Job.ID+": operation failed")
		}
		o := sample.Observation
		for _, resource := range o.Resources {
			if resource.Tenant != sample.Job.Tenant || resource.User == "" || resource.Task == "" || resource.Lane == "" || resource.Outcome == "" || resource.PoolWait < 0 {
				findings = append(findings, sample.Job.ID+": invalid resource admission binding")
			}
		}
		if o.DurableRef == "" {
			findings = append(findings, sample.Job.ID+": durable outcome unobserved")
		}
		if o.QueueAge == nil || o.PoolWait == nil {
			findings = append(findings, sample.Job.ID+": queue age or pool wait unobserved")
		}
		if (o.QueueAge != nil && *o.QueueAge < 0) || (o.PoolWait != nil && *o.PoolWait < 0) || (o.TimerLateness != nil && *o.TimerLateness < 0) || (o.CostMicros != nil && *o.CostMicros < 0) {
			findings = append(findings, sample.Job.ID+": invalid measurement")
		}
		if sample.Job.Kind == "model" && (o.Provider == "" || o.Provider == "fake" || o.Model == "" || o.CostMicros == nil) {
			findings = append(findings, sample.Job.ID+": live model cost or route unobserved")
		}
		if (sample.Job.Kind == "workflow" || sample.Job.Kind == "schedule") && o.TimerLateness == nil {
			findings = append(findings, sample.Job.ID+": timer lateness unobserved")
		}
	}
	for _, kind := range []string{"chat", "model", "schedule", "workflow"} {
		if !kinds[kind] {
			findings = append(findings, kind+": workload absent")
		}
	}
	if len(tenants) < 2 {
		findings = append(findings, "mixed tenants absent")
	}
	if report.FinishedAt.After(report.Plan.Plan.ExpiresAt) {
		findings = append(findings, "measurement exceeded plan window")
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summaries := make([]Summary, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		durations := make([]time.Duration, len(group))
		s := Summary{Tenant: group[0].Job.Tenant, Kind: group[0].Job.Kind, Count: len(group)}
		for i, sample := range group {
			durations[i] = sample.Elapsed
			if sample.Failure != "" {
				s.Failed++
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		s.P50, s.P95, s.P99 = percentile(durations, .50), percentile(durations, .95), percentile(durations, .99)
		if s.P99 > report.Plan.Plan.P99Limit {
			findings = append(findings, key+": p99 exceeds internal qualification limit")
		}
		summaries = append(summaries, s)
	}
	return summaries, findings
}

func percentile(samples []time.Duration, quantile float64) time.Duration {
	return samples[int(math.Ceil(float64(len(samples))*quantile))-1]
}
