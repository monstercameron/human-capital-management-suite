package journey_test

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestTodo_PROMOUX_008_Security is PROMOUX-008's SECURITY matrix test: the
// one that matters. It proves, by value rather than by flag, that a
// principal PROMOUX-008 does not authorize for diagnostics never receives
// the diagnostic-only identifiers in the InspectJourney payload at all --
// not merely hidden by a client that chooses not to render them -- and that
// the presence/count/layout of what such a principal does receive carries
// no information about whether the underlying journey actually has an
// instance, nodes, work items, a ledger write or evidence.
func TestTodo_PROMOUX_008_Security(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	withInstance := fixtureDetail()
	withoutInstance := fixtureDetail()
	withoutInstance.Summary.InstanceID = ""
	withoutInstance.Summary.InstanceVersion = 0
	withoutInstance.Summary.MaterialDigest = ""
	withoutInstance.Summary.CorrelationID = ""
	withoutInstance.Instance = nil
	withoutInstance.Nodes = nil
	withoutInstance.WorkItems = nil
	withoutInstance.Transitions = nil
	withoutInstance.Ledger = nil
	withoutInstance.EvidenceIDs = nil
	withoutInstance.PlannedWrites = nil

	t.Run("unauthorized viewer's payload contains none of the diagnostic identifiers", func(t *testing.T) {
		engine.setDetail(withInstance)
		resp, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		d := resp.GetDetail()
		j := d.GetJourney()
		if j.GetMaterialDigest() != "" {
			t.Errorf("material_digest = %q, want empty", j.GetMaterialDigest())
		}
		if j.GetCorrelationId() != "" {
			t.Errorf("correlation_id = %q, want empty", j.GetCorrelationId())
		}
		if j.GetInstanceId() != "" {
			t.Errorf("journey.instance_id = %q, want empty", j.GetInstanceId())
		}
		if j.GetInstanceVersion() != 0 {
			t.Errorf("journey.instance_version = %d, want 0", j.GetInstanceVersion())
		}
		if d.GetInstance() != nil {
			t.Errorf("instance = %v, want nil", d.GetInstance())
		}
		if len(d.GetPlannedWrites()) != 0 {
			t.Errorf("planned_writes = %v, want empty", d.GetPlannedWrites())
		}
		if d.GetLedger() != nil {
			t.Errorf("ledger = %v, want nil", d.GetLedger())
		}
		if len(d.GetEvidenceIds()) != 0 {
			t.Errorf("evidence_ids = %v, want empty", d.GetEvidenceIds())
		}
		if len(d.GetNodes()) != 0 {
			t.Errorf("nodes = %v, want empty", d.GetNodes())
		}
		if len(d.GetTransitions()) != 0 {
			t.Errorf("transitions = %v, want empty", d.GetTransitions())
		}
		if len(d.GetWorkItems()) == 0 {
			t.Fatal("work_items = 0, want the approval work item to remain (business-visible status/owner)")
		}
		for _, w := range d.GetWorkItems() {
			if w.GetWorkItemId() != "" {
				t.Errorf("work_items[].work_item_id = %q, want empty for an unauthorized viewer", w.GetWorkItemId())
			}
			if w.GetChosenOwner() == "" {
				t.Error("work_items[].chosen_owner was withheld too; the approval disposition needs it")
			}
		}
		// WorkerRef is a routing key (the person-profile link), not
		// diagnostic content, so it is expected to still be present.
		if j.GetWorkerRef() == "" {
			t.Error("worker_ref was withheld, but it is a navigation identifier ordinary business review needs")
		}
	})

	t.Run("presence and shape are identical for two unauthorized viewers regardless of the underlying journey's state", func(t *testing.T) {
		engine.setDetail(withInstance)
		hasInstance, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney (with instance): %v", err)
		}
		engine.setDetail(withoutInstance)
		noInstance, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney (without instance): %v", err)
		}
		a, b := hasInstance.GetDetail(), noInstance.GetDetail()
		if a.GetInstance() != nil || b.GetInstance() != nil {
			t.Fatalf("instance = %v / %v, want nil on both sides regardless of the underlying state", a.GetInstance(), b.GetInstance())
		}
		if len(a.GetNodes()) != 0 || len(b.GetNodes()) != 0 {
			t.Fatalf("nodes = %d / %d, want 0 on both sides", len(a.GetNodes()), len(b.GetNodes()))
		}
		if a.GetLedger() != nil || b.GetLedger() != nil {
			t.Fatalf("ledger = %v / %v, want nil on both sides", a.GetLedger(), b.GetLedger())
		}
		if len(a.GetEvidenceIds()) != 0 || len(b.GetEvidenceIds()) != 0 {
			t.Fatalf("evidence_ids = %v / %v, want empty on both sides", a.GetEvidenceIds(), b.GetEvidenceIds())
		}
		if len(a.GetPlannedWrites()) != 0 || len(b.GetPlannedWrites()) != 0 {
			t.Fatalf("planned_writes = %v / %v, want empty on both sides", a.GetPlannedWrites(), b.GetPlannedWrites())
		}
		if a.GetJourney().GetMaterialDigest() != b.GetJourney().GetMaterialDigest() {
			t.Fatalf("material_digest differs across underlying states for an equally unauthorized viewer: %q vs %q",
				a.GetJourney().GetMaterialDigest(), b.GetJourney().GetMaterialDigest())
		}
		if a.GetJourney().GetInstanceId() != b.GetJourney().GetInstanceId() {
			t.Fatalf("journey.instance_id differs across underlying states: %q vs %q", a.GetJourney().GetInstanceId(), b.GetJourney().GetInstanceId())
		}
		for i, wi := range a.GetWorkItems() {
			if wi.GetWorkItemId() != "" {
				t.Fatalf("work_items[%d].work_item_id = %q, want empty on the has-instance side too", i, wi.GetWorkItemId())
			}
		}
	})

	t.Run("authorized viewer receives the diagnostic identifiers", func(t *testing.T) {
		engine.setDetail(withInstance)
		resp, err := client.InspectJourney(authorizedContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		d := resp.GetDetail()
		if d.GetJourney().GetMaterialDigest() == "" {
			t.Error("authorized viewer did not receive material_digest")
		}
		if d.GetInstance().GetInstanceId() == "" {
			t.Error("authorized viewer did not receive the instance")
		}
		if len(d.GetPlannedWrites()) == 0 {
			t.Error("authorized viewer did not receive planned_writes")
		}
		if d.GetLedger() == nil {
			t.Error("authorized viewer did not receive the ledger")
		}
		if len(d.GetWorkItems()) == 0 || d.GetWorkItems()[0].GetWorkItemId() == "" {
			t.Error("authorized viewer did not receive the work item id")
		}
	})

	// REFACTOR: the business timeline and the diagnostic evidence are two
	// projections toDetail draws from the one workspace.JourneyDetail it is
	// called with, not two separately stored summaries that could drift
	// apart. Proof: calling it twice over the identical underlying detail,
	// once unauthorized and once authorized, must produce byte-identical
	// Timeline content either way (the business projection does not depend
	// on diagnostics authority at all) while the diagnostic content differs
	// by authorization alone -- exactly what one shared source gated by one
	// flag looks like, and exactly what two independently maintained
	// summaries would not reliably do.
	t.Run("REFACTOR: the business timeline is the same projection of the same detail regardless of diagnostics authority", func(t *testing.T) {
		engine.setDetail(withInstance)
		unauthorized, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney (unauthorized): %v", err)
		}
		authorized, err := client.InspectJourney(authorizedContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney (authorized): %v", err)
		}
		u, a := unauthorized.GetDetail().GetTimeline(), authorized.GetDetail().GetTimeline()
		if len(u) == 0 || len(u) != len(a) {
			t.Fatalf("timeline length = %d (unauthorized) vs %d (authorized), want equal and non-zero", len(u), len(a))
		}
		for i := range u {
			if u[i].GetTitle() != a[i].GetTitle() || u[i].GetDetail() != a[i].GetDetail() || u[i].GetActor() != a[i].GetActor() || u[i].GetKind() != a[i].GetKind() || u[i].GetRef() != a[i].GetRef() {
				t.Fatalf("timeline[%d] differs by authorization: %v vs %v", i, u[i], a[i])
			}
		}
		// Meanwhile the diagnostic projection of that same call genuinely
		// differs: this is the boundary, not an accident of two unrelated
		// projections happening to agree.
		if unauthorized.GetDetail().GetInstance() != nil || authorized.GetDetail().GetInstance() == nil {
			t.Fatal("diagnostic projection did not vary by authorization the way the business one must not")
		}
	})
}
