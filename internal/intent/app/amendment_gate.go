package app

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/authority"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// AmendmentGatedExecutor is a [ProposalExecutor] that admits every planned
// write in an executed revision through the cell's bound P1B authority
// amendment before the driver runs (REV-059-01, NEXT-006's consumer
// follow-up: "conditional admission of local command implementations from
// the amendment").
//
// ExecuteIntent calls exactly this port to start an approved revision, and
// Resume/ResumeTimer re-present the same bound revision, so gating here
// covers every path by which a P1B write reaches the runtime. Each
// [intent.PlannedWrite] passes [authority.Amendment.AdmitLocalWrite] with
// the revision's own material tenant; the first refusal fails the call
// with the amendment's own typed error and the inner driver never runs,
// so the write never lands. A nil amendment passes everything through:
// cells composed without a bound amendment behave exactly as before.
type AmendmentGatedExecutor struct {
	inner     ProposalExecutor
	amendment *authority.Amendment
	grant     string
	now       func() time.Time
}

// NewAmendmentGatedExecutor wraps inner with the amendment gate. A nil
// inner stays nil so a cell composed without a driver keeps refusing as
// unavailable; a nil amendment returns inner itself, unwrapped. A nil now
// means the process clock in UTC.
func NewAmendmentGatedExecutor(inner ProposalExecutor, amendment *authority.Amendment, grant string, now func() time.Time) ProposalExecutor {
	if inner == nil {
		return nil
	}
	if amendment == nil {
		return inner
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &AmendmentGatedExecutor{inner: inner, amendment: amendment, grant: grant, now: now}
}

func (g *AmendmentGatedExecutor) admit(start runtime.StartRequest) error {
	revision := start.Proposal.Revision
	at := g.now().UTC()
	for _, write := range revision.Writes {
		if err := g.amendment.AdmitLocalWrite(write.FieldPath, string(write.Operation), at, revision.Tenant, g.grant); err != nil {
			return err
		}
	}
	return nil
}

// Execute admits the bound revision's writes, then starts the driver.
func (g *AmendmentGatedExecutor) Execute(ctx context.Context, start runtime.StartRequest) (ExecutionResult, error) {
	if err := g.admit(start); err != nil {
		return ExecutionResult{}, err
	}
	return g.inner.Execute(ctx, start)
}

// Resume admits the resumed revision's writes, then resumes the driver.
func (g *AmendmentGatedExecutor) Resume(ctx context.Context, req ExecutionResumeRequest) (ExecutionResult, error) {
	if err := g.admit(req.Start); err != nil {
		return ExecutionResult{}, err
	}
	return g.inner.Resume(ctx, req)
}

// ResumeTimer admits the resumed revision's writes, then resumes the driver.
func (g *AmendmentGatedExecutor) ResumeTimer(ctx context.Context, req ExecutionTimerResumeRequest) (ExecutionResult, error) {
	if err := g.admit(req.Start); err != nil {
		return ExecutionResult{}, err
	}
	return g.inner.ResumeTimer(ctx, req)
}

var _ ProposalExecutor = (*AmendmentGatedExecutor)(nil)
