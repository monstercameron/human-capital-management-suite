package chatrender

import (
	"context"
	"time"
)

type Job struct {
	Request  Rendering
	Lease    string
	Attempts int
}
type JobStore interface {
	ClaimRendering(context.Context, string, time.Duration) (Job, error)
	CompleteRendering(context.Context, Job, Rendering) error
	FailRendering(context.Context, Job) error
}

// ProductionAuthority must recheck the original's current audience and provider
// disclosure policy immediately before reserving cost or invoking a producer.
type ProductionAuthority interface {
	AuthorizeRenderingJob(context.Context, Job) error
}

// UsageLedger is the narrow EXTCOST-002 boundary. Real providers may not run
// without a reservation; deterministic fixtures use FixtureUsageLedger.
type UsageLedger interface {
	ReserveRendering(context.Context, Job) (string, error)
	RecordRendering(context.Context, string, Rendering) error
}
type Worker struct {
	Jobs      JobStore
	Registry  *Registry
	Authority ProductionAuthority
	Usage     UsageLedger
}

func (w Worker) RunOne(ctx context.Context, tenant string, lease time.Duration) error {
	if w.Jobs == nil || w.Registry == nil || w.Authority == nil || w.Usage == nil {
		return ErrUnavailable
	}
	job, err := w.Jobs.ClaimRendering(ctx, tenant, lease)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		if err := w.Jobs.FailRendering(ctx, job); err != nil {
			return err
		}
		return cause
	}
	if err = w.Authority.AuthorizeRenderingJob(ctx, job); err != nil {
		return fail(err)
	}
	cost, err := w.Usage.ReserveRendering(ctx, job)
	if err != nil {
		return fail(err)
	}
	if cost == "" {
		return fail(ErrUnavailable)
	}
	out, err := w.Registry.Produce(ctx, job.Request, job.Request.Kinds)
	if err != nil {
		return fail(err)
	}
	out.CostReference = cost
	// Usage survives a failed write so a consumed provider call is not hidden.
	if err = w.Usage.RecordRendering(ctx, cost, out); err != nil {
		return fail(err)
	}
	if err = w.Authority.AuthorizeRenderingJob(ctx, job); err != nil {
		return fail(err)
	}
	return w.Jobs.CompleteRendering(ctx, job, out)
}

// FixtureUsageLedger records no financial side effect and makes its status
// explicit in the reference. It is only for deterministic fixture producers.
type FixtureUsageLedger struct{}

func (FixtureUsageLedger) ReserveRendering(_ context.Context, job Job) (string, error) {
	if job.Request.Message == "" {
		return "", ErrInvalid
	}
	return "fixture:no-charge:" + job.Request.Message, nil
}
func (FixtureUsageLedger) RecordRendering(_ context.Context, reference string, _ Rendering) error {
	if reference == "" {
		return ErrInvalid
	}
	return nil
}

// RequestsForPolicy includes transformations required by the channel even when
// the reader has not requested a change to tone or language.
func RequestsForPolicy(policy Policy, readers []Preference) []Rendering {
	var result []Rendering
	for _, pref := range readers {
		if policy.RequireReworded {
			pref.Tone = Reworded
		}
		if !hasKind(policy.AllowedKinds, Translate) {
			// The policy offers no translation here, so none is requested.
			pref.Translate, pref.SourceOverrides = false, nil
		}
		targets := RequestsForReaders(policy.Original, []Preference{pref})
		if policy.RequireMask && len(targets) == 0 {
			target := policy.Original
			target.Text = ""
			target.Tone = AsWritten
			target.Language = Language(target.SourceLanguage)
			targets = []Rendering{target}
		}
		for _, target := range targets {
			if policy.RequireMask {
				target.Kinds = append([]Kind{Mask}, target.Kinds...)
			}
			duplicate := false
			for _, prior := range result {
				if prior.Tone == target.Tone && prior.Language == target.Language {
					duplicate = true
				}
			}
			if !duplicate {
				result = append(result, target)
			}
		}
	}
	return result
}
