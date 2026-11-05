// WF-DATA-040 extension RED: paused and waiting runs are never
// revalidated, and undo has no revision shape.
package promotion

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/revalidation"
)

var gov040Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func gov040Graph() revalidation.Graph {
	return revalidation.Graph{SchemaVersion: revalidation.Version()}
}

func gov040Current() []revalidation.Dependency {
	return []revalidation.Dependency{{
		Key: "people.promotion.threshold", Kind: revalidation.KindRule,
		Version: "v2", Digest: "sha256:after", Active: true,
	}}
}

func gov040Pinned() map[string][]revalidation.Dependency {
	return map[string][]revalidation.Dependency{
		"run-17": {{
			Key: "people.promotion.threshold", Kind: revalidation.KindRule,
			Version: "v1", Digest: "sha256:before", Active: true,
		}},
	}
}

// TestTodo_WF_DATA_040_Revalidation proves paused and waiting runs named
// by an impact preview are revalidated through the revalidation package.
func TestTodo_WF_DATA_040_Revalidation(t *testing.T) {
	_, preview := wfData040Request(t)
	report, err := RevalidatePausedRuns(preview, []AffectedRun{
		{InstanceID: "run-17", WorkflowID: "workflow:promotion", TenantID: "tenant-a", State: RunStatePaused},
		{InstanceID: "run-18", WorkflowID: "workflow:promotion", TenantID: "tenant-a", State: RunStateRunning},
		{InstanceID: "run-99", WorkflowID: "workflow:other", TenantID: "tenant-a", State: RunStateWaiting},
	}, gov040Pinned(), gov040Current(), gov040Graph())
	if err != nil {
		t.Fatalf("RevalidatePausedRuns: %v", err)
	}
	if len(report.Runs) != 1 || report.Runs[0].InstanceID != "run-17" {
		t.Fatalf("revalidated = %+v, want exactly run-17", report.Runs)
	}
	dec := report.Runs[0].Decision
	if dec.WorkloadID != "run-17" || dec.EvidenceDigest == "" {
		t.Fatalf("decision = %+v, want bound evidence", dec)
	}
	if report.PreviewDigest != preview.Digest || report.Digest == "" {
		t.Fatalf("report = %+v, want preview-bound digest", report)
	}
	if _, err := RevalidatePausedRuns(ImpactPreview{}, nil, gov040Pinned(), gov040Current(), gov040Graph()); !errors.Is(err, ErrGovernancePolicy) {
		t.Fatalf("invalid preview err = %v, want ErrGovernancePolicy", err)
	}
}

// TestTodo_WF_DATA_040_Undo proves undoing a change is a new revision:
// the original request is never mutated, and a high-impact undo needs a
// different principal's approval.
func TestTodo_WF_DATA_040_Undo(t *testing.T) {
	request, _ := wfData040Request(t)
	before := request.Digest
	rev, err := UndoParameterChange(request, 1, "author-c", gov040Now)
	if err != nil {
		t.Fatalf("UndoParameterChange: %v", err)
	}
	if rev.Revision != 2 || rev.UndoesDigest != request.Digest || !rev.ApprovalRequired || rev.Digest == "" {
		t.Fatalf("revision = %+v", rev)
	}
	if request.Digest != before {
		t.Fatal("undo mutated the original request")
	}
	if _, err := ApproveUndo(rev, "author-c", gov040Now); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-approve err = %v, want ErrSelfApproval", err)
	}
	approval, err := ApproveUndo(rev, "reviewer-d", gov040Now)
	if err != nil {
		t.Fatalf("ApproveUndo: %v", err)
	}
	if approval.RevisionDigest != rev.Digest || approval.Approver != "reviewer-d" || approval.Digest == "" {
		t.Fatalf("undo approval = %+v", approval)
	}
}

func TestTodo_WF_DATA_040_UndoLowImpact(t *testing.T) {
	diff, definition := wfData040Diff(t)
	definition.HighImpact = false
	request, err := AuthorizeParameterChanges("package-2", "sha256:package-2", "author-a", diff,
		[]config.ParameterDefinition{definition}, []ParameterPermission{{Key: "people.promotion.threshold", Editors: []string{"author-a"}}})
	if err != nil {
		t.Fatalf("AuthorizeParameterChanges: %v", err)
	}
	rev, err := UndoParameterChange(request, 3, "author-a", gov040Now)
	if err != nil {
		t.Fatalf("UndoParameterChange: %v", err)
	}
	if rev.Revision != 4 || rev.ApprovalRequired {
		t.Fatalf("revision = %+v, want revision 4 with no approval", rev)
	}
	if _, err := ApproveUndo(rev, "reviewer-d", gov040Now); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("approve-unneeded err = %v, want ErrApprovalRequired", err)
	}
}
