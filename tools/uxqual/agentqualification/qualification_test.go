package agentqualification

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func qualificationPlan(t *testing.T) (SignedPlan, map[string]ed25519.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	plan := Plan{ID: "local-qualification-v1", Profile: "pooled", Runtime: "qualification-test", Issuer: "capacity-author", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Concurrency: 4, P99Limit: time.Second}
	for index, kind := range []string{"model", "chat", "workflow", "schedule"} {
		plan.Jobs = append(plan.Jobs, Job{ID: kind, Tenant: []string{"tiny", "noisy"}[index%2], Kind: kind})
	}
	signed, err := SignPlan(plan, private)
	if err != nil {
		t.Fatal(err)
	}
	return signed, map[string]ed25519.PublicKey{"capacity-author": public}
}

func completeObservation(job Job) Observation {
	zero := time.Duration(0)
	cost := int64(7)
	return Observation{DurableRef: "test-outcome:" + job.ID, Provider: "test-provider", Model: "test-model", QueueAge: &zero, PoolWait: &zero, TimerLateness: &zero, CostMicros: &cost, Resources: []ResourceAdmission{{Tenant: job.Tenant, User: "test-user", Task: job.ID, Lane: "interactive", Provider: "test-provider", Outcome: "ADMITTED"}}}
}

// Unit operations deliberately identify test evidence; the integration test
// supplies durable application operations and keeps incomplete evidence UNKNOWN.
func TestTodo_AGENT_048_Conformance(t *testing.T) {
	plan, keys := qualificationPlan(t)
	var active, peak atomic.Int64
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	op := func(ctx context.Context, job Job) (Observation, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for current := peak.Load(); n > current; current = peak.Load() {
			if peak.CompareAndSwap(current, n) {
				break
			}
		}
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return Observation{}, ctx.Err()
		}
		return completeObservation(job), nil
	}
	operations := map[string]Operation{"model": op, "chat": op, "workflow": op, "schedule": op}
	type result struct {
		report Report
		err    error
	}
	completed := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { r, err := Run(ctx, plan, keys, operations); completed <- result{r, err} }()
	for range 4 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	r := <-completed
	if r.err != nil || r.report.Status != "MEASURED" || len(r.report.Samples) != 4 || peak.Load() != 4 {
		t.Fatalf("run=%+v peak=%d", r, peak.Load())
	}
	for _, sample := range r.report.Samples {
		if sample.Elapsed < 0 || sample.Observation.DurableRef == "" {
			t.Fatalf("unmeasured operation: %+v", sample)
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignReport(r.report, "independent-reviewer", private)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReport(signed, keys, map[string]ed25519.PublicKey{"independent-reviewer": public}, time.Now()); err != nil {
		t.Fatal(err)
	}
	signed.Report.Samples[0].Observation.DurableRef = "tampered"
	if err := VerifyReport(signed, keys, map[string]ed25519.PublicKey{"independent-reviewer": public}, time.Now()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("tampered report error=%v", err)
	}
}

func TestTodo_AGENT_048_Fault(t *testing.T) {
	plan, keys := qualificationPlan(t)
	op := func(_ context.Context, job Job) (Observation, error) {
		if job.Kind == "model" {
			return Observation{}, errors.New("provider quota exhausted")
		}
		return Observation{DurableRef: job.ID}, nil
	}
	report, err := Run(context.Background(), plan, keys, map[string]Operation{"model": op, "chat": op, "workflow": op, "schedule": op})
	if err != nil || report.Status != "UNKNOWN" || len(report.Samples) != 4 {
		t.Fatalf("failed measurement report=%+v err=%v", report, err)
	}
	if report.Samples[0].Failure != "provider quota exhausted" || report.Summaries[0].Count < 1 {
		t.Fatalf("failure or samples lost: %+v", report)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignReport(report, "reviewer", private)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReport(signed, keys, map[string]ed25519.PublicKey{"reviewer": public}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := SignReport(report, report.Plan.Plan.Issuer, private); !errors.Is(err, ErrEvidence) {
		t.Fatalf("self review accepted: %v", err)
	}
	selfPlan, err := SignPlan(report.Plan.Plan, private)
	if err != nil {
		t.Fatal(err)
	}
	selfReport := report
	selfReport.Plan = selfPlan
	selfSigned, err := SignReport(selfReport, "reviewer", private)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReport(selfSigned, map[string]ed25519.PublicKey{report.Plan.Plan.Issuer: public}, map[string]ed25519.PublicKey{"reviewer": public}, time.Now()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("same key under different reviewer identities accepted: %v", err)
	}
	if err := VerifyReport(signed, keys, map[string]ed25519.PublicKey{"reviewer": public}, report.FinishedAt.Add(-time.Nanosecond)); !errors.Is(err, ErrEvidence) {
		t.Fatalf("future measurement accepted: %v", err)
	}
	report.Status = "MEASURED"
	if _, err := SignReport(report, "reviewer", private); !errors.Is(err, ErrEvidence) {
		t.Fatalf("status promotion error=%v", err)
	}
	report.Status = "UNKNOWN"
	report.Summaries[0].P99 += time.Hour
	if _, err := SignReport(report, "reviewer", private); !errors.Is(err, ErrEvidence) {
		t.Fatalf("forged percentile error=%v", err)
	}
}

func TestQualificationRejectsUntrustedOrIncompletePlan(t *testing.T) {
	plan, keys := qualificationPlan(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Profile = "unknown" },
		func(p *Plan) { p.Concurrency = 1 },
		func(p *Plan) { p.P99Limit = 0 },
		func(p *Plan) { p.Jobs[1].ID = p.Jobs[0].ID },
		func(p *Plan) { p.Jobs[0].Kind = "synthetic" },
	} {
		p := plan.Plan
		p.Jobs = append([]Job(nil), p.Jobs...)
		mutate(&p)
		if _, err := SignPlan(p, private); !errors.Is(err, ErrEvidence) {
			t.Fatalf("invalid plan accepted: %+v err=%v", p, err)
		}
	}
	if err := VerifyPlan(plan, map[string]ed25519.PublicKey{"capacity-author": public}, time.Now()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unknown signature accepted: %v", err)
	}
	if err := VerifyPlan(plan, keys, plan.Plan.ExpiresAt); !errors.Is(err, ErrEvidence) {
		t.Fatalf("expired plan accepted: %v", err)
	}
	var called atomic.Bool
	op := func(context.Context, Job) (Observation, error) { called.Store(true); return Observation{}, nil }
	if _, err := Run(context.Background(), plan, keys, map[string]Operation{"chat": op}); !errors.Is(err, ErrEvidence) || called.Load() {
		t.Fatalf("missing op invoked another operation: err=%v called=%v", err, called.Load())
	}
	if _, err := Run(nil, plan, keys, nil); !errors.Is(err, ErrEvidence) {
		t.Fatalf("nil context accepted: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Run(cancelled, plan, keys, map[string]Operation{"model": op, "chat": op, "workflow": op, "schedule": op})
	if err != nil || report.Status != "UNKNOWN" || called.Load() {
		t.Fatalf("cancelled run dispatched work: status=%s err=%v called=%v", report.Status, err, called.Load())
	}
	for _, sample := range report.Samples {
		if sample.Failure != context.Canceled.Error() {
			t.Fatalf("cancellation evidence lost: %+v", sample)
		}
	}
	if got := percentile([]time.Duration{1, 2, 3, 4}, .95); got != 4 {
		t.Fatalf("p95=%d", got)
	}
	if !reflect.DeepEqual(plan.Plan.Jobs[0], Job{"model", "tiny", "model"}) {
		t.Fatal("invalid-plan tests mutated their shared input")
	}
}

func TestQualificationRecordsResourceTenantSubstitutionAsUnknown(t *testing.T) {
	plan, keys := qualificationPlan(t)
	op := func(_ context.Context, job Job) (Observation, error) {
		observation := completeObservation(job)
		if job.Kind == "model" {
			observation.Resources[0].Tenant = "foreign-tenant"
		}
		return observation, nil
	}
	report, err := Run(context.Background(), plan, keys, map[string]Operation{"model": op, "chat": op, "workflow": op, "schedule": op})
	if err != nil || report.Status != "UNKNOWN" {
		t.Fatalf("tenant substitution accepted: status=%s err=%v", report.Status, err)
	}
	found := false
	for _, finding := range report.Findings {
		if finding == "model: invalid resource admission binding" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tenant substitution evidence missing: %v", report.Findings)
	}
}
