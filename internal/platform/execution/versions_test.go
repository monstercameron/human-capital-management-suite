package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type recordingRegistry struct {
	*version.Registry
	approvals  []workflowversionstore.Approval
	activated  []string
	supersede  []bool
	approveErr error
}

func (r *recordingRegistry) RecordApproval(_ context.Context, a workflowversionstore.Approval) error {
	r.approvals = append(r.approvals, a)
	return r.approveErr
}

func (r *recordingRegistry) ActivateApproved(_ context.Context, digest string, supersede bool) (version.CompiledVersion, error) {
	r.activated = append(r.activated, digest)
	r.supersede = append(r.supersede, supersede)
	return version.CompiledVersion{}, nil
}

func TestComposeVersionsActivatesOnlyDraftsOnARecordedReleaseApproval(t *testing.T) {
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	draft := version.CompiledVersion{WorkflowID: "wf", SemanticVersion: "1.0.0", CompiledPlanDigest: "sha256:draft", Status: version.StatusDraft}

	registry := &recordingRegistry{Registry: version.NewRegistry()}
	store, activate := composeVersions(PromotionExecutionConfig{Versions: registry}, at)
	if store != VersionRegistry(registry) {
		t.Fatal("a durable registry was not used as the composition's version store")
	}
	if err := activate(draft); err != nil {
		t.Fatalf("activate(draft): %v", err)
	}
	if len(registry.approvals) != 1 || registry.approvals[0].ApprovedBy != defaultVersionApprover ||
		registry.approvals[0].ApprovedBy == versionPublisher || registry.approvals[0].ReviewedPlanDigest != draft.CompiledPlanDigest {
		t.Fatalf("recorded approvals = %+v", registry.approvals)
	}
	if len(registry.activated) != 1 || !registry.supersede[0] {
		t.Fatalf("activations = %v supersede %v, want one superseding activation", registry.activated, registry.supersede)
	}
	first := registry.approvals[0].ApprovalID
	if err := activate(draft); err != nil || registry.approvals[1].ApprovalID != first {
		t.Fatalf("a second composition derived a different approval id (%v)", err)
	}

	for _, status := range []version.ActivationStatus{version.StatusActive, version.StatusQuarantined, version.StatusRetired} {
		v := draft
		v.Status = status
		before := len(registry.activated)
		if err := activate(v); err != nil || len(registry.activated) != before {
			t.Fatalf("activate(%s) = %v, activations %d -> %d; want it left alone", status, err, before, len(registry.activated))
		}
	}

	named := &recordingRegistry{Registry: version.NewRegistry(), approveErr: errors.New("refused")}
	_, activateNamed := composeVersions(PromotionExecutionConfig{Versions: named, VersionApprover: " principal:release-board "}, at)
	if err := activateNamed(draft); err == nil || len(named.activated) != 0 || named.approvals[0].ApprovedBy != "principal:release-board" {
		t.Fatalf("a refused approval = %v, activations %v, approvals %+v", err, named.activated, named.approvals)
	}
}

func TestComposeVersionsWithoutARegistryKeepsThePrivateInMemoryStore(t *testing.T) {
	store, activate := composeVersions(PromotionExecutionConfig{}, time.Now())
	if _, ok := store.(*version.Registry); !ok {
		t.Fatalf("store = %T, want the in-memory registry", store)
	}
	if err := activate(version.CompiledVersion{CompiledPlanDigest: "sha256:absent"}); version.CodeOf(err) != version.CodeUnknownRecord {
		t.Fatalf("activating an unpublished digest = %v, want %s", err, version.CodeUnknownRecord)
	}
}
