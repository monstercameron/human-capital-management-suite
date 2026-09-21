package execute

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestServesPinnedInstance proves which published version a live instance may
// keep advancing on: the ACTIVE resolved plan, or the exact version the
// instance pinned when a later activation superseded it -- never a
// superseded version the request did not pin, a governed quarantine, a
// retired version, or a version that is not the resolver's plan.
func TestServesPinnedInstance(t *testing.T) {
	plan, err := promotionexec.CompileV1_0()
	if err != nil {
		t.Fatal(err)
	}
	selection := runtime.WorkflowSelection{WorkflowID: plan.WorkflowID, Plan: plan}
	digest := plan.Digest()
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	record := func(status version.ActivationStatus, reason string) version.CompiledVersion {
		v := version.CompiledVersion{WorkflowID: plan.WorkflowID, CompiledPlanDigest: digest, Status: status}
		if reason != "" {
			v.Approvals = []version.ApprovalRecord{
				{ApprovedBy: "a", Result: version.StatusActive, ApprovedAt: at},
				{ApprovedBy: "b", Reason: reason, Result: status, ApprovedAt: at.Add(time.Hour)},
			}
		}
		return v
	}
	pinned := runtime.StartRequest{PinnedCompiledPlanDigest: digest}
	superseded := record(version.StatusQuarantined, version.SupersededReasonPrefix+"sha256:next")
	for _, tc := range []struct {
		name      string
		start     runtime.StartRequest
		published version.CompiledVersion
		want      bool
	}{
		{"active, unpinned", runtime.StartRequest{}, record(version.StatusActive, ""), true},
		{"active, pinned", pinned, record(version.StatusActive, ""), true},
		{"superseded, pinned", pinned, superseded, true},
		{"superseded, unpinned", runtime.StartRequest{}, superseded, false},
		{"superseded, pinned to another digest", runtime.StartRequest{PinnedCompiledPlanDigest: "sha256:other"}, superseded, false},
		{"governed quarantine, pinned", pinned, record(version.StatusQuarantined, "incident INC-1"), false},
		{"retired, pinned", pinned, record(version.StatusRetired, "end of life"), false},
		{"draft, pinned", pinned, record(version.StatusDraft, ""), false},
	} {
		if got := servesPinnedInstance(tc.start, tc.published, selection); got != tc.want {
			t.Errorf("%s: servesPinnedInstance = %t, want %t", tc.name, got, tc.want)
		}
	}
	other := superseded
	other.CompiledPlanDigest = "sha256:not-the-plan"
	if servesPinnedInstance(runtime.StartRequest{PinnedCompiledPlanDigest: other.CompiledPlanDigest}, other, selection) {
		t.Error("a version that is not the resolver's plan was served")
	}
}
