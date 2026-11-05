// WF-DATA-040: revalidate paused and waiting runs through the
// revalidation package, and undo a change as a new revision.
//
// A parameter change lands on running workflows: every paused or waiting
// run the impact preview names is fenced and revalidated against the
// live configuration through revalidation.Revalidate, and the report
// binds each decision to the preview. Undo never mutates history: it
// appends a new revision superseding the undone request, approved under
// the same separation of duties when the undone change was high-impact.
package promotion

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/revalidation"
)

// RunState is one affected-run state.
type RunState string

// Affected-run states.
const (
	RunStateRunning RunState = "RUNNING"
	RunStatePaused  RunState = "PAUSED"
	RunStateWaiting RunState = "WAITING"
)

// AffectedRun is one running instance that may need revalidation.
type AffectedRun struct {
	InstanceID string
	WorkflowID string
	TenantID   string
	State      RunState
}

// RunRevalidation is one run's revalidation outcome.
type RunRevalidation struct {
	InstanceID string
	Decision   revalidation.Decision
}

// RevalidationReport binds per-run revalidation decisions to the impact
// preview that named them.
type RevalidationReport struct {
	PreviewDigest string
	Runs          []RunRevalidation
	Digest        string
}

// RevalidatePausedRuns revalidates every paused or waiting run the
// preview names: each becomes a fenced revalidation workload over its
// pinned dependencies against the live configuration. Running runs and
// runs the preview does not name are skipped; a named run with no
// pinned dependencies fails closed.
func RevalidatePausedRuns(preview ImpactPreview, runs []AffectedRun, pinned map[string][]revalidation.Dependency, current []revalidation.Dependency, graph revalidation.Graph) (RevalidationReport, error) {
	if !validImpactPreview(preview) {
		return RevalidationReport{}, ErrGovernancePolicy
	}
	impacted := make(map[string]bool)
	for _, impact := range preview.Impacts {
		if impact.RunningInstanceID != "" {
			impacted[impact.RunningInstanceID] = true
		}
	}
	var out []RunRevalidation
	seen := make(map[string]bool)
	for _, run := range runs {
		if run.State != RunStatePaused && run.State != RunStateWaiting {
			continue
		}
		if !impacted[run.InstanceID] || seen[run.InstanceID] {
			continue
		}
		seen[run.InstanceID] = true
		workload := revalidation.Workload{
			ID: run.InstanceID, TenantID: run.TenantID,
			State: string(run.State), Dependencies: pinned[run.InstanceID],
		}
		decision, err := revalidation.Revalidate(workload, current, graph)
		if err != nil {
			return RevalidationReport{}, fmt.Errorf("config promotion: revalidate %s: %w", run.InstanceID, err)
		}
		out = append(out, RunRevalidation{InstanceID: run.InstanceID, Decision: decision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstanceID < out[j].InstanceID })
	parts := []string{"hcmnext.config.parameter-revalidation.v1", preview.Digest}
	for _, r := range out {
		parts = append(parts, r.InstanceID, string(r.Decision.Status), fmt.Sprint(r.Decision.FenceToken), r.Decision.EvidenceDigest)
	}
	return RevalidationReport{PreviewDigest: preview.Digest, Runs: out, Digest: digestStrings(parts...)}, nil
}

// UndoRevision is a new revision undoing one request. The undone request
// is never mutated; the revision supersedes it.
type UndoRevision struct {
	Revision         int
	UndoesDigest     string
	PackageID        string
	Author           string
	At               time.Time
	ApprovalRequired bool
	Digest           string
}

// UndoApproval is a distinct principal's approval of a high-impact undo.
type UndoApproval struct {
	RevisionDigest string
	Author         string
	Approver       string
	ApprovedAt     time.Time
	Digest         string
}

// UndoParameterChange appends the next revision undoing a request.
func UndoParameterChange(request ChangeRequest, priorRevision int, author string, at time.Time) (UndoRevision, error) {
	if !validChangeRequest(request) {
		return UndoRevision{}, ErrGovernancePolicy
	}
	if priorRevision < 1 {
		return UndoRevision{}, fmt.Errorf("%w: prior revision %d", ErrGovernancePolicy, priorRevision)
	}
	if strings.TrimSpace(author) == "" || at.IsZero() {
		return UndoRevision{}, ErrGovernancePolicy
	}
	rev := UndoRevision{
		Revision: priorRevision + 1, UndoesDigest: request.Digest,
		PackageID: request.PackageID, Author: author, At: at.UTC(),
		ApprovalRequired: len(request.HighImpactKeys) > 0,
	}
	rev.Digest = undoRevisionDigest(rev)
	return rev, nil
}

// ApproveUndo approves a high-impact undo under separation of duties:
// the approver must differ from the undo author.
func ApproveUndo(rev UndoRevision, approver string, at time.Time) (UndoApproval, error) {
	if !validUndoRevision(rev) {
		return UndoApproval{}, ErrStale
	}
	if !rev.ApprovalRequired {
		return UndoApproval{}, ErrApprovalRequired
	}
	if strings.TrimSpace(approver) == "" || approver == rev.Author {
		return UndoApproval{}, ErrSelfApproval
	}
	if at.IsZero() {
		return UndoApproval{}, ErrGovernancePolicy
	}
	approval := UndoApproval{RevisionDigest: rev.Digest, Author: rev.Author, Approver: approver, ApprovedAt: at.UTC()}
	approval.Digest = digestStrings("hcmnext.config.parameter-undo-approval.v1", approval.RevisionDigest, approval.Author, approval.Approver, approval.ApprovedAt.Format(time.RFC3339Nano))
	return approval, nil
}

func undoRevisionDigest(rev UndoRevision) string {
	return digestStrings("hcmnext.config.parameter-undo.v1", fmt.Sprint(rev.Revision), rev.UndoesDigest, rev.PackageID, rev.Author, rev.At.Format(time.RFC3339Nano), fmt.Sprint(rev.ApprovalRequired))
}

func validUndoRevision(rev UndoRevision) bool {
	return rev.Revision > 0 && rev.UndoesDigest != "" && rev.PackageID != "" && rev.Author != "" && !rev.At.IsZero() && rev.Digest != "" && rev.Digest == undoRevisionDigest(rev)
}
