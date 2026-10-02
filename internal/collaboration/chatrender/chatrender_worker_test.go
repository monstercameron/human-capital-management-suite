package chatrender

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerFixture struct {
	job                                                        Job
	claimed, failed, completed, reserved, recorded, authorized int
	denyAt                                                     int
	produced                                                   Rendering
}

func (f *workerFixture) ClaimRendering(context.Context, string, time.Duration) (Job, error) {
	f.claimed++
	return f.job, nil
}
func (f *workerFixture) CompleteRendering(_ context.Context, _ Job, r Rendering) error {
	f.completed++
	f.produced = r
	return nil
}
func (f *workerFixture) FailRendering(context.Context, Job) error { f.failed++; return nil }
func (f *workerFixture) AuthorizeRenderingJob(context.Context, Job) error {
	f.authorized++
	if f.authorized == f.denyAt {
		return ErrDenied
	}
	return nil
}
func (f *workerFixture) ReserveRendering(context.Context, Job) (string, error) {
	f.reserved++
	return "cost:fixture", nil
}
func (f *workerFixture) RecordRendering(context.Context, string, Rendering) error {
	f.recorded++
	return nil
}
func testRenderingWorker(t *testing.T) {
	policy, pref, _ := renderingFixture()
	target := policy.Original
	target.Kinds = []Kind{Translate}
	target.Language = "en"
	registry, err := NewRegistry(Registration{Translate, FixtureProducer{Text: "translated", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &workerFixture{job: Job{Request: target, Lease: "lease", Attempts: 1}}
	worker := Worker{Jobs: fixture, Registry: registry, Authority: fixture, Usage: fixture}
	if err = worker.RunOne(context.Background(), "tenant", time.Minute); err != nil || fixture.completed != 1 || fixture.recorded != 1 || fixture.produced.CostReference != "cost:fixture" || fixture.authorized != 2 {
		t.Fatal(fixture, err)
	}
	policy.RequireMask = true
	policy.RequireReworded = true
	targets := RequestsForPolicy(policy, []Preference{pref, pref})
	if len(targets) != 1 || targets[0].Tone != Reworded || len(targets[0].Kinds) != 3 || targets[0].Kinds[0] != Mask {
		t.Fatal(targets)
	}
	policy.RequireReworded = false
	pref.Translate = false
	targets = RequestsForPolicy(policy, []Preference{pref})
	if len(targets) != 1 || targets[0].Tone != AsWritten || len(targets[0].Kinds) != 1 || targets[0].Kinds[0] != Mask {
		t.Fatal(targets)
	}
	ledger := FixtureUsageLedger{}
	reference, err := ledger.ReserveRendering(context.Background(), fixture.job)
	if err != nil || reference != "fixture:no-charge:post" {
		t.Fatal(reference, err)
	}
	if err = ledger.RecordRendering(context.Background(), reference, Rendering{}); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.ReserveRendering(context.Background(), Job{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err = ledger.RecordRendering(context.Background(), "", Rendering{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func testRenderingWorkerFault(t *testing.T) {
	policy, _, _ := renderingFixture()
	target := policy.Original
	target.Kinds = []Kind{Translate}
	registry, _ := NewRegistry(Registration{Translate, FixtureProducer{Text: "translated"}})
	for _, denyAt := range []int{1, 2} {
		f := &workerFixture{job: Job{Request: target}, denyAt: denyAt}
		worker := Worker{Jobs: f, Registry: registry, Authority: f, Usage: f}
		if err := worker.RunOne(context.Background(), "tenant", time.Minute); !errors.Is(err, ErrDenied) || f.failed != 1 || f.completed != 0 || f.reserved != denyAt-1 {
			t.Fatal(f, err)
		}
	}
	f := &workerFixture{job: Job{Request: target}}
	empty, _ := NewRegistry()
	worker := Worker{Jobs: f, Registry: empty, Authority: f, Usage: f}
	if err := worker.RunOne(context.Background(), "tenant", time.Minute); !errors.Is(err, ErrUnavailable) || f.failed != 1 || f.completed != 0 || f.recorded != 0 {
		t.Fatal(f, err)
	}
	if err := (Worker{}).RunOne(context.Background(), "tenant", time.Minute); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
