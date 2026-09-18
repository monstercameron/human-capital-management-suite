package journey_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// roundTripDetail sends d through the wire conversion and back by way of a
// real gRPC call, which is the only route the exported surface offers: the
// conversion functions are unexported on purpose, because nothing outside
// this package may build a JourneyDetail by hand and claim it came from the
// engine.
func roundTripDetail(t *testing.T, d workspace.JourneyDetail) *journeyv1.JourneyDetail {
	t.Helper()
	engine := newFakeEngine()
	engine.setDetail(d)
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	// This helper's whole point is that the conversion is total -- every
	// field the port set survives the wire round trip -- so it calls in as
	// PROMOUX-008's diagnostics-authorized fixture identity rather than the
	// ordinary manager token, which now has some of those fields withheld
	// by design.
	resp, err := client.InspectJourney(authorizedContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
	if err != nil {
		t.Fatalf("InspectJourney: %v", err)
	}
	return resp.GetDetail()
}

// TestJourneyConversionCarriesEveryPortField walks a fully populated
// JourneyDetail through the wire and asserts, field by field, that nothing
// was dropped. A conversion that silently loses a field is the defect a
// handler test cannot see - the RPC still succeeds, the page just shows less
// than the engine knows - so this test compares values, not shapes.
func TestJourneyConversionCarriesEveryPortField(t *testing.T) {
	src := fixtureDetail()
	got := roundTripDetail(t, src)

	t.Run("Summary", func(t *testing.T) {
		j, want := got.GetJourney(), src.Summary
		checks := []struct {
			field string
			got   any
			want  any
		}{
			{"intent_id", j.GetIntentId(), want.IntentID},
			{"correlation_id", j.GetCorrelationId(), want.CorrelationID},
			{"worker_ref", j.GetWorkerRef(), want.Worker.String()},
			{"worker_name", j.GetWorkerName(), want.WorkerName},
			{"current_base", j.GetCurrentBase(), want.CurrentBase},
			{"proposed_base", j.GetProposedBase(), want.ProposedBase},
			{"currency", j.GetCurrency(), want.Currency},
			{"effective_date", j.GetEffectiveDate(), want.EffectiveDate},
			{"business_reason", j.GetBusinessReason(), want.BusinessReason},
			{"proposal_revision_id", j.GetProposalRevisionId(), want.ProposalRevisionID},
			{"material_digest", j.GetMaterialDigest(), want.MaterialDigest},
			{"instance_id", j.GetInstanceId(), want.InstanceID},
			{"instance_version", j.GetInstanceVersion(), want.InstanceVersion},
			{"current.job_code", j.GetCurrent().GetJobCode(), want.Current.JobCode},
			{"current.grade", j.GetCurrent().GetGrade(), want.Current.Grade},
			{"current.position_id", j.GetCurrent().GetPositionId(), want.Current.PositionID},
			{"current.org_unit", j.GetCurrent().GetOrgUnit(), want.Current.OrgUnit},
			{"current.pay_zone", j.GetCurrent().GetPayZone(), want.Current.PayZone},
			{"target.job_code", j.GetTarget().GetJobCode(), want.Target.JobCode},
			{"target.grade", j.GetTarget().GetGrade(), want.Target.Grade},
			{"target.position_id", j.GetTarget().GetPositionId(), want.Target.PositionID},
			{"target.org_unit", j.GetTarget().GetOrgUnit(), want.Target.OrgUnit},
			{"target.pay_zone", j.GetTarget().GetPayZone(), want.Target.PayZone},
			{"created_at", j.GetCreatedAt().AsTime(), want.CreatedAt},
			{"updated_at", j.GetUpdatedAt().AsTime(), want.UpdatedAt},
		}
		for _, c := range checks {
			if !reflect.DeepEqual(c.got, c.want) {
				t.Errorf("journey.%s = %v, want %v", c.field, c.got, c.want)
			}
		}
		if j.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
			t.Errorf("journey.stage = %v, want AWAITING_APPROVAL", j.GetStage())
		}
	})

	t.Run("Findings", func(t *testing.T) {
		if len(got.GetFindings()) != len(src.Findings) {
			t.Fatalf("findings = %d, want %d", len(got.GetFindings()), len(src.Findings))
		}
		for i, w := range src.Findings {
			f := got.GetFindings()[i]
			if f.GetSeverity() != w.Severity || f.GetCode() != w.Code || f.GetMessage() != w.Message {
				t.Errorf("findings[%d] = %v, want %+v", i, f, w)
			}
		}
	})

	t.Run("PlannedWritesAndEvidence", func(t *testing.T) {
		if !reflect.DeepEqual(got.GetPlannedWrites(), src.PlannedWrites) {
			t.Errorf("planned_writes = %v, want %v", got.GetPlannedWrites(), src.PlannedWrites)
		}
		if !reflect.DeepEqual(got.GetEvidenceIds(), src.EvidenceIDs) {
			t.Errorf("evidence_ids = %v, want %v", got.GetEvidenceIds(), src.EvidenceIDs)
		}
		if got.GetApprover() != src.Approver {
			t.Errorf("approver = %q, want %q", got.GetApprover(), src.Approver)
		}
	})

	t.Run("Instance", func(t *testing.T) {
		i, want := got.GetInstance(), src.Instance
		if i.GetInstanceId() != want.InstanceID ||
			i.GetInstanceVersion() != want.InstanceVersion ||
			i.GetWorkflowId() != want.WorkflowID ||
			i.GetWorkflowVersion() != want.WorkflowVersion ||
			i.GetPlanDigest() != want.PlanDigest ||
			i.GetStatus() != want.Status ||
			i.GetCorrelationId() != want.CorrelationID {
			t.Errorf("instance = %v, want %+v", i, want)
		}
		if !reflect.DeepEqual(i.GetCurrentNodeIds(), want.CurrentNodeIDs) {
			t.Errorf("instance.current_node_ids = %v, want %v", i.GetCurrentNodeIds(), want.CurrentNodeIDs)
		}
		if !i.GetCreatedAt().AsTime().Equal(want.CreatedAt) {
			t.Errorf("instance.created_at = %v, want %v", i.GetCreatedAt().AsTime(), want.CreatedAt)
		}
		if !i.GetStartedAt().AsTime().Equal(*want.StartedAt) {
			t.Errorf("instance.started_at = %v, want %v", i.GetStartedAt().AsTime(), *want.StartedAt)
		}
		if i.GetCompletedAt() != nil {
			t.Errorf("instance.completed_at = %v, want unset for a nil port value", i.GetCompletedAt())
		}
	})

	t.Run("Nodes", func(t *testing.T) {
		n, want := got.GetNodes()[0], src.Nodes[0]
		if n.GetNodeId() != want.NodeID || int(n.GetAttempt()) != want.Attempt ||
			n.GetStepType() != want.StepType || n.GetStatus() != want.Status || n.GetTraceId() != want.TraceID {
			t.Errorf("nodes[0] = %v, want %+v", n, want)
		}
		if !n.GetRecordedAt().AsTime().Equal(want.RecordedAt) {
			t.Errorf("nodes[0].recorded_at = %v, want %v", n.GetRecordedAt().AsTime(), want.RecordedAt)
		}
		if n.GetCompletedAt() != nil {
			t.Error("nodes[0].completed_at must stay unset for a nil port value")
		}
	})

	t.Run("WorkItems", func(t *testing.T) {
		w, want := got.GetWorkItems()[0], src.WorkItems[0]
		if w.GetWorkItemId() != want.WorkItemID.String() ||
			w.GetKind() != string(want.Kind) ||
			w.GetStatus() != string(want.Status) ||
			w.GetWorkType() != want.WorkType ||
			w.GetNodeId() != want.NodeID ||
			w.GetOwnerRef() != want.OwnerRef ||
			w.GetChosenOwner() != want.Assignment.ChosenOwner ||
			w.GetClaimedBy() != want.ClaimedBy ||
			w.GetCompletedBy() != want.CompletedBy ||
			w.GetItemVersion() != want.ItemVersion {
			t.Errorf("work_items[0] = %v, want %+v", w, want)
		}
		if !w.GetClaimedAt().AsTime().Equal(*want.ClaimedAt) {
			t.Errorf("work_items[0].claimed_at = %v, want %v", w.GetClaimedAt().AsTime(), *want.ClaimedAt)
		}
		if !w.GetClaimExpiresAt().AsTime().Equal(*want.ClaimExpiresAt) {
			t.Errorf("work_items[0].claim_expires_at = %v, want %v", w.GetClaimExpiresAt().AsTime(), *want.ClaimExpiresAt)
		}
		if !w.GetDeadlineAt().AsTime().Equal(want.DeadlineAt) {
			t.Errorf("work_items[0].deadline_at = %v, want %v", w.GetDeadlineAt().AsTime(), want.DeadlineAt)
		}
		if !w.GetCreatedAt().AsTime().Equal(want.CreatedAt) {
			t.Errorf("work_items[0].created_at = %v, want %v", w.GetCreatedAt().AsTime(), want.CreatedAt)
		}
		if w.GetCompletedAt() != nil {
			t.Error("work_items[0].completed_at must stay unset for a nil port value")
		}
	})

	t.Run("Transitions", func(t *testing.T) {
		tr, want := got.GetTransitions()[0], src.Transitions[0]
		if tr.GetWorkItemId() != want.WorkItemID || tr.GetFrom() != want.From || tr.GetTo() != want.To ||
			tr.GetActor() != want.Actor || tr.GetReason() != want.Reason {
			t.Errorf("transitions[0] = %v, want %+v", tr, want)
		}
		if !tr.GetAt().AsTime().Equal(want.At) {
			t.Errorf("transitions[0].at = %v, want %v", tr.GetAt().AsTime(), want.At)
		}
	})

	t.Run("Ledger", func(t *testing.T) {
		l, want := got.GetLedger(), src.Ledger
		if l.GetStreamKey() != want.StreamKey || l.GetSequence() != want.Sequence ||
			l.GetSchemaRef() != want.SchemaRef || l.GetDigest() != want.Digest ||
			l.GetIdempotencyKey() != want.IdempotencyKey {
			t.Errorf("ledger = %v, want %+v", l, want)
		}
		if !l.GetOccurredAt().AsTime().Equal(want.OccurredAt) ||
			!l.GetEffectiveAt().AsTime().Equal(want.EffectiveAt) ||
			!l.GetRecordedAt().AsTime().Equal(want.RecordedAt) {
			t.Errorf("ledger instants = %v/%v/%v, want %v/%v/%v",
				l.GetOccurredAt().AsTime(), l.GetEffectiveAt().AsTime(), l.GetRecordedAt().AsTime(),
				want.OccurredAt, want.EffectiveAt, want.RecordedAt)
		}
	})

	t.Run("Timeline", func(t *testing.T) {
		e, want := got.GetTimeline()[0], src.Timeline[0]
		if e.GetActor() != want.Actor || e.GetKind() != want.Kind ||
			e.GetTitle() != want.Title || e.GetDetail() != want.Detail || e.GetRef() != want.Ref {
			t.Errorf("timeline[0] = %v, want %+v", e, want)
		}
		if !e.GetAt().AsTime().Equal(want.At) {
			t.Errorf("timeline[0].at = %v, want %v", e.GetAt().AsTime(), want.At)
		}
	})
}

// TestJourneyConversionLeavesUnexecutedSectionsUnset proves the other half
// of the contract: the sections the port models as nil until a journey has
// been executed must arrive unset, not as populated-but-empty messages a
// page would render as "an instance exists with no id".
func TestJourneyConversionLeavesUnexecutedSectionsUnset(t *testing.T) {
	src := workspace.JourneyDetail{Summary: fixtureSummary()}
	src.Summary.Stage = workspace.JourneyStageProposed
	src.Summary.InstanceID = ""
	src.Summary.InstanceVersion = 0

	got := roundTripDetail(t, src)
	if got.GetInstance() != nil {
		t.Errorf("instance = %v, want unset", got.GetInstance())
	}
	if got.GetLedger() != nil {
		t.Errorf("ledger = %v, want unset", got.GetLedger())
	}
	if len(got.GetNodes()) != 0 || len(got.GetWorkItems()) != 0 || len(got.GetTransitions()) != 0 {
		t.Errorf("nodes/work_items/transitions = %d/%d/%d, want all empty",
			len(got.GetNodes()), len(got.GetWorkItems()), len(got.GetTransitions()))
	}
	if got.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Errorf("stage = %v, want PROPOSED", got.GetJourney().GetStage())
	}
	if got.GetJourney().GetCreatedAt() == nil {
		t.Error("created_at must be set: the summary carries a non-zero instant")
	}
}

// TestJourneyStageEnumIsTotal walks every stage the port declares and
// asserts each one arrives as its own wire value, and that an unknown stage
// token arrives as the UNSPECIFIED zero rather than being guessed at.
func TestJourneyStageEnumIsTotal(t *testing.T) {
	cases := []struct {
		stage workspace.JourneyStage
		want  journeyv1.JourneyStage
	}{
		{workspace.JourneyStageProposed, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED},
		{workspace.JourneyStageBlocked, journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED},
		{workspace.JourneyStageAwaitingApproval, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL},
		{workspace.JourneyStageCompleted, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED},
		{workspace.JourneyStageRejected, journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED},
		{workspace.JourneyStageFailed, journeyv1.JourneyStage_JOURNEY_STAGE_FAILED},
		{workspace.JourneyStageFinanceApproval, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL},
		{workspace.JourneyStageManagerApproval, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL},
		{workspace.JourneyStageWaitingEffectiveDate, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE},
		{workspace.JourneyStageRevalidation, journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION},
		{workspace.JourneyStageReapproval, journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL},
		{workspace.JourneyStageExecuted, journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED},
		{workspace.JourneyStageObservingEffects, journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS},
		{workspace.JourneyStageRecorded, journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED},
		{workspace.JourneyStageRepairRequired, journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED},
		{workspace.JourneyStageAwaitingAcknowledgement, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT},
		{workspace.JourneyStage("SOMETHING_NEW"), journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED},
		{workspace.JourneyStage(""), journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED},
	}

	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := testContext(t)
	for _, c := range cases {
		t.Run(string(c.stage), func(t *testing.T) {
			d := fixtureDetail()
			d.Summary.Stage = c.stage
			engine.setDetail(d)
			resp, err := client.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
			if err != nil {
				t.Fatalf("InspectJourney: %v", err)
			}
			if resp.GetDetail().GetJourney().GetStage() != c.want {
				t.Fatalf("stage %q = %v, want %v", c.stage, resp.GetDetail().GetJourney().GetStage(), c.want)
			}
			if c.want == journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED {
				if _, err := journey.JourneyStageFromProto(c.want); err == nil {
					t.Fatalf("wire stage %v was accepted as a workspace stage", c.want)
				}
				return
			}
			back, err := journey.JourneyStageFromProto(c.want)
			if err != nil {
				t.Fatalf("stageFromProto(%v): %v", c.want, err)
			}
			if back != c.stage {
				t.Fatalf("stage round trip = %q, want %q", back, c.stage)
			}
		})
	}
}

// TestJourneyWorkerRefIsCanonicalAndParsable proves the worker reference is
// a value a client can echo back rather than a display string: it is exactly
// values.EntityRef's canonical text encoding, and an EntityRef the kernel
// would reject renders as empty rather than as a plausible-looking
// reference.
func TestJourneyWorkerRefIsCanonicalAndParsable(t *testing.T) {
	t.Run("a valid reference travels as its canonical encoding", func(t *testing.T) {
		got := roundTripDetail(t, fixtureDetail())
		want := fixtureWorkerRef().String()
		if want == "" {
			t.Fatal("the fixture worker reference does not validate; the fixture is wrong, not the code")
		}
		if got.GetJourney().GetWorkerRef() != want {
			t.Fatalf("worker_ref = %q, want %q", got.GetJourney().GetWorkerRef(), want)
		}
	})

	t.Run("an invalid reference renders as empty", func(t *testing.T) {
		d := fixtureDetail()
		d.Summary.Worker.Id = "not-a-uuid"
		got := roundTripDetail(t, d)
		if got.GetJourney().GetWorkerRef() != "" {
			t.Fatalf("worker_ref = %q, want empty for a reference the kernel rejects", got.GetJourney().GetWorkerRef())
		}
	})
}

// TestDetailDigestIsDeterministicAndContentAddressed pins the three
// properties WatchJourney's whole change detection rests on: the digest of
// equal content is equal across calls and across processes (deterministic
// marshalling, not incidental map ordering), different content digests
// differently, and the digest never covers itself.
func TestDetailDigestIsDeterministicAndContentAddressed(t *testing.T) {
	first := roundTripDetail(t, fixtureDetail())
	second := roundTripDetail(t, fixtureDetail())

	t.Run("equal content digests equally", func(t *testing.T) {
		if first.GetDetailDigest() != second.GetDetailDigest() {
			t.Fatalf("digest differs across two conversions of equal content: %q vs %q",
				first.GetDetailDigest(), second.GetDetailDigest())
		}
		if !strings.HasPrefix(first.GetDetailDigest(), "sha256:") {
			t.Fatalf("digest = %q, want a sha256: prefix", first.GetDetailDigest())
		}
	})

	t.Run("a change anywhere in the detail changes the digest", func(t *testing.T) {
		mutations := map[string]func(*workspace.JourneyDetail){
			"summary stage":      func(d *workspace.JourneyDetail) { d.Summary.Stage = workspace.JourneyStageCompleted },
			"summary timestamp":  func(d *workspace.JourneyDetail) { d.Summary.UpdatedAt = fixtureTime().Add(2 * time.Hour) },
			"a finding":          func(d *workspace.JourneyDetail) { d.Findings[0].Message = "revised" },
			"a planned write":    func(d *workspace.JourneyDetail) { d.PlannedWrites = append(d.PlannedWrites, "audit.trail") },
			"the instance":       func(d *workspace.JourneyDetail) { d.Instance.Status = "COMPLETED" },
			"a node":             func(d *workspace.JourneyDetail) { d.Nodes[0].Status = "COMPLETED" },
			"a work item status": func(d *workspace.JourneyDetail) { d.WorkItems[0].Status = "COMPLETED" },
			"a transition":       func(d *workspace.JourneyDetail) { d.Transitions[0].To = "CLAIMED" },
			"the ledger":         func(d *workspace.JourneyDetail) { d.Ledger.Sequence = 13 },
			"an evidence id":     func(d *workspace.JourneyDetail) { d.EvidenceIDs = append(d.EvidenceIDs, "ev:3") },
			"a timeline entry":   func(d *workspace.JourneyDetail) { d.Timeline[0].Title = "Promotion approved" },
			"the approver":       func(d *workspace.JourneyDetail) { d.Approver = "approver-kim" },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				d := fixtureDetail()
				mutate(&d)
				got := roundTripDetail(t, d)
				if got.GetDetailDigest() == first.GetDetailDigest() {
					t.Fatalf("mutating %s left the digest unchanged (%q)", name, got.GetDetailDigest())
				}
			})
		}
	})

	t.Run("the digest is not part of what it covers", func(t *testing.T) {
		// Recomputing the digest over the received message with the field
		// cleared must reproduce it exactly. If the stamped digest were
		// itself hashed, this would be impossible for any client to verify.
		clone := proto.Clone(first).(*journeyv1.JourneyDetail)
		stamped := clone.GetDetailDigest()
		clone.DetailDigest = ""
		if journey.DetailDigestForTest(clone) != stamped {
			t.Fatalf("recomputed digest %q does not match the stamped %q",
				journey.DetailDigestForTest(clone), stamped)
		}
	})
}
