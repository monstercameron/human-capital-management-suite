package execution

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// recordingRegistry is a durable-shaped registry over the in-memory store
// that records every approval and activation composition asks for.
type recordingRegistry struct {
	*version.Registry
	approvals []workflowversionstore.Approval
	activated []string
}

func (r *recordingRegistry) RecordApproval(_ context.Context, a workflowversionstore.Approval) error {
	r.approvals = append(r.approvals, a)
	return nil
}

func (r *recordingRegistry) ActivateApproved(_ context.Context, digest string, _ bool) (version.CompiledVersion, error) {
	r.activated = append(r.activated, digest)
	v, _, err := r.GetByDigest(digest)
	return v, err
}

// TestTodo_WF_COMP_006_ServeBootNeverApproves proves composition over a
// durable registry publishes both shipped workflows as DRAFT with their
// declared fixtures and tool versions, records no approval and activates
// nothing, and is idempotent across recomposition.
func TestTodo_WF_COMP_006_ServeBootNeverApproves(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	registry := &recordingRegistry{Registry: version.NewRegistry()}
	for range 2 {
		store, err := composeVersions(PromotionExecutionConfig{Versions: registry}, at)
		if err != nil {
			t.Fatalf("composeVersions: %v", err)
		}
		if store != version.Store(registry) {
			t.Fatal("a durable registry was not used as the composition's version store")
		}
	}
	if len(registry.approvals) != 0 || len(registry.activated) != 0 {
		t.Fatalf("composition recorded approvals %+v and activations %v, want none", registry.approvals, registry.activated)
	}
	for _, workflowID := range []string{prototype.ApprovalWorkflowID, promotionexec.WorkflowID} {
		versions, err := registry.List(workflowID)
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 1 {
			t.Fatalf("%s: %d published versions after two compositions, want 1", workflowID, len(versions))
		}
		v := versions[0]
		if v.Status != version.StatusDraft || len(v.Approvals) != 0 || len(v.FixtureRefs) == 0 || v.ToolVersions["go"] == "" || v.PublishedBy != versionPublisher {
			t.Fatalf("%s published as %+v, want an unapproved DRAFT declaring fixtures and tool versions", workflowID, v)
		}
		if _, found, err := registry.GetActiveForWorkflow(workflowID); err != nil || found {
			t.Fatalf("%s has an ACTIVE version after composition (%v)", workflowID, err)
		}
	}
}

// TestComposeVersionsWithoutARegistryActivatesOnFixtureRuns proves the
// unit-only in-memory registry activates each version on its own in-process
// fixture run, under an approver that is not the publisher.
func TestComposeVersionsWithoutARegistryActivatesOnFixtureRuns(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	store, err := composeVersions(PromotionExecutionConfig{}, at)
	if err != nil {
		t.Fatalf("composeVersions: %v", err)
	}
	if _, ok := store.(*version.Registry); !ok {
		t.Fatalf("store = %T, want the in-memory registry", store)
	}
	for _, workflowID := range []string{prototype.ApprovalWorkflowID, promotionexec.WorkflowID} {
		active, found, err := store.GetActiveForWorkflow(workflowID)
		if err != nil || !found || len(active.Approvals) != 1 || active.Approvals[0].ApprovedBy != unitCompositionApprover {
			t.Fatalf("%s = %+v (found %v, %v), want ACTIVE on the unit-composition fixture run", workflowID, active, found, err)
		}
	}
	registry := version.NewRegistry()
	published, err := PublishShippedVersions(registry, at)
	if err != nil {
		t.Fatal(err)
	}
	stripped := published[0]
	stripped.FixtureRefs = nil
	if err := activateInMemory(registry, stripped, at); err == nil {
		t.Fatal("a version whose record no longer verifies was activated in memory")
	}
}
