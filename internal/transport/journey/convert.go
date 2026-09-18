package journey

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

var errUnknownJourneyStage = errors.New("journey: unknown journey stage")

// This file is the whole translation layer between the workspace journey
// port's plain Go types and the hcmnext.journey.v1 wire messages. It is a
// separate file, and every function in it is total, because a conversion
// that silently drops a field is the one defect a handler test cannot see:
// the RPC still succeeds, the page just shows less than the engine knows.
// convert_test.go therefore round-trips fully populated values rather than
// spot-checking a few fields.

// toTimestamp renders t, treating the zero time as "unset" rather than as
// year 1: an absent instant must arrive as an absent field, not as a
// timestamp the page would render.
func toTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// toOptionalTimestamp renders a pointer instant the port already models as
// optional.
func toOptionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return toTimestamp(*t)
}

// fromTimestamp is [toTimestamp]'s inverse: an unset field is the zero time.
func fromTimestamp(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// stageToProto maps a port stage token onto the wire enum. An unrecognized
// token maps to JOURNEY_STAGE_UNSPECIFIED rather than being guessed at,
// which is what makes a schema drift visible instead of silently wrong.
func stageToProto(s workspace.JourneyStage) journeyv1.JourneyStage {
	switch s {
	case workspace.JourneyStageProposed:
		return journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED
	case workspace.JourneyStageBlocked:
		return journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED
	case workspace.JourneyStageAwaitingApproval:
		return journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL
	case workspace.JourneyStageCompleted:
		return journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED
	case workspace.JourneyStageRejected:
		return journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED
	case workspace.JourneyStageFailed:
		return journeyv1.JourneyStage_JOURNEY_STAGE_FAILED
	case workspace.JourneyStageFinanceApproval:
		return journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL
	case workspace.JourneyStageManagerApproval:
		return journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL
	case workspace.JourneyStageWaitingEffectiveDate:
		return journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE
	case workspace.JourneyStageRevalidation:
		return journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION
	case workspace.JourneyStageReapproval:
		return journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL
	case workspace.JourneyStageExecuted:
		return journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED
	case workspace.JourneyStageObservingEffects:
		return journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS
	case workspace.JourneyStageRecorded:
		return journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED
	case workspace.JourneyStageRepairRequired:
		return journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED
	case workspace.JourneyStageAwaitingAcknowledgement:
		return journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT
	default:
		return journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED
	}
}

// JourneyStageFromProto is stageToProto's strict inverse. An unspecified or
// future wire value cannot be represented by the workspace port, so it is
// refused instead of being collapsed into a valid-looking stage.
func JourneyStageFromProto(s journeyv1.JourneyStage) (workspace.JourneyStage, error) {
	switch s {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return workspace.JourneyStageProposed, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return workspace.JourneyStageBlocked, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return workspace.JourneyStageAwaitingApproval, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return workspace.JourneyStageCompleted, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED:
		return workspace.JourneyStageRejected, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return workspace.JourneyStageFailed, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return workspace.JourneyStageFinanceApproval, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return workspace.JourneyStageManagerApproval, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return workspace.JourneyStageWaitingEffectiveDate, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION:
		return workspace.JourneyStageRevalidation, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return workspace.JourneyStageReapproval, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED:
		return workspace.JourneyStageExecuted, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return workspace.JourneyStageObservingEffects, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED:
		return workspace.JourneyStageRecorded, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
		return workspace.JourneyStageRepairRequired, nil
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT:
		return workspace.JourneyStageAwaitingAcknowledgement, nil
	default:
		return "", errUnknownJourneyStage
	}
}

// toPlacement renders one side of the placement change.
func toPlacement(p workspace.JourneyPlacement) *journeyv1.Placement {
	return &journeyv1.Placement{
		JobCode:    p.JobCode,
		Grade:      p.Grade,
		PositionId: p.PositionID,
		OrgUnit:    p.OrgUnit,
		PayZone:    p.PayZone,
	}
}

// fromInterventionKind maps the wire intervention kind onto the port's own
// closed set. An unrecognized or unspecified value maps to the empty
// string, which the port's own validation (never this conversion) refuses.
func fromInterventionKind(k journeyv1.JourneyInterventionKind) workspace.JourneyInterventionKind {
	switch k {
	case journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_WITHDRAW:
		return workspace.JourneyInterventionWithdraw
	case journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL:
		return workspace.JourneyInterventionCancel
	case journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_REPAIR:
		return workspace.JourneyInterventionRepair
	default:
		return ""
	}
}

// toInterventionOutcome maps the port's own outcome vocabulary onto the
// shared hcmnext.common.v1.InterventionOutcome wire enum. An unrecognized
// port value maps to UNSPECIFIED rather than being guessed at.
func toInterventionOutcome(o workspace.JourneyInterventionOutcome) commonv1.InterventionOutcome {
	switch o {
	case workspace.InterventionApplied:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_APPLIED
	case workspace.InterventionPendingSafePoint:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_PENDING_SAFE_POINT
	case workspace.InterventionDenied:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_DENIED
	case workspace.InterventionTooLate:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_TOO_LATE
	case workspace.InterventionRepairRequired:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_REPAIR_REQUIRED
	case workspace.InterventionIndeterminate:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_INDETERMINATE
	default:
		return commonv1.InterventionOutcome_INTERVENTION_OUTCOME_UNSPECIFIED
	}
}

// toInterventionPreview renders one [workspace.JourneyInterventionPreview].
func toInterventionPreview(p workspace.JourneyInterventionPreview) *journeyv1.PreviewJourneyInterventionResponse {
	return &journeyv1.PreviewJourneyInterventionResponse{
		Available:                p.Available,
		UnavailableReasonRef:     p.UnavailableReasonRef,
		ConsequenceSummary:       p.ConsequenceSummary,
		LikelyOutcome:            toInterventionOutcome(p.LikelyOutcome),
		CurrentGovernanceVersion: p.CurrentGovernanceVersion,
		RequiresDualControl:      p.RequiresDualControl,
		RequiresSimulation:       p.RequiresSimulation,
		AuthorityRoleRefs:        append([]string(nil), p.AuthorityRoleRefs...),
	}
}

// fromPlacement is [toPlacement]'s inverse. A nil message is the zero
// placement, so a request that omits the field is not a decoding failure.
func fromPlacement(p *journeyv1.Placement) workspace.JourneyPlacement {
	return workspace.JourneyPlacement{
		JobCode:    p.GetJobCode(),
		Grade:      p.GetGrade(),
		PositionID: p.GetPositionId(),
		OrgUnit:    p.GetOrgUnit(),
		PayZone:    p.GetPayZone(),
	}
}

// toJourney renders one summary. The worker reference travels as its
// canonical text encoding (values.EntityRef.String), which is empty for a
// reference the kernel would not accept - an invalid reference must not be
// re-rendered into a shape a client could echo back as if it were valid.
// WorkerRef and IntentId always travel: both are routing keys ordinary
// business navigation needs (the person profile link, this journey's own
// address), never displayed as diagnostic content on their own. diagAuthor-
// ized (PROMOUX-008) instead gates the fields with no business rendering
// path at all -- the material/plan digest, the correlation id, and the
// workflow instance identity -- by omitting them from the wire message
// itself for a principal [server.diagnosticsAuthorized] denies, so an
// unauthorized viewer never has them to withhold from the view; they are
// simply not in the payload.
func toJourney(s workspace.JourneySummary, diagAuthorized bool) *journeyv1.Journey {
	out := &journeyv1.Journey{
		IntentId:           s.IntentID,
		WorkerRef:          s.Worker.String(),
		WorkerName:         s.WorkerName,
		Current:            toPlacement(s.Current),
		Target:             toPlacement(s.Target),
		CurrentBase:        s.CurrentBase,
		ProposedBase:       s.ProposedBase,
		Currency:           s.Currency,
		EffectiveDate:      s.EffectiveDate,
		BusinessReason:     s.BusinessReason,
		Stage:              stageToProto(s.Stage),
		ProposalRevisionId: s.ProposalRevisionID,
		CreatedAt:          toTimestamp(s.CreatedAt),
		UpdatedAt:          toTimestamp(s.UpdatedAt),
		GovernanceVersion:  s.GovernanceVersion,
	}
	out.CurrentWorkItem = toJourneyWorkItemSummary(s.CurrentWorkItem)
	out.Viewer = toJourneyViewerProjection(s.Viewer)
	if diagAuthorized {
		out.CorrelationId = s.CorrelationID
		out.MaterialDigest = s.MaterialDigest
		out.InstanceId = s.InstanceID
		out.InstanceVersion = s.InstanceVersion
	}
	return out
}

// toJourneyWorkItemSummary renders the engine's viewer-scoped work item
// summary. The engine has already applied the work item read rules; this is a
// field copy, and nil stays nil.
func toJourneyWorkItemSummary(s *workspace.JourneyWorkItemSummary) *journeyv1.JourneyWorkItemSummary {
	if s == nil {
		return nil
	}
	out := &journeyv1.JourneyWorkItemSummary{
		Kind:                   s.Kind,
		Status:                 s.Status,
		AssigneePrincipalId:    s.AssigneePrincipalID,
		AssigneeDisplayName:    s.AssigneeDisplayName,
		ViewerPermittedActions: append([]string(nil), s.ViewerPermittedActions...),
		ViewerMembership:       s.ViewerMembership,
	}
	if !s.DueAt.IsZero() {
		out.DueAt = toTimestamp(s.DueAt)
	}
	return out
}

// toJourneyViewerProjection renders the engine's PROMOUX-012 viewer
// projection. It is a token-to-enum copy: the engine resolved every value, and
// an unresolved projection (empty responsibility) stays unset on the wire.
func toJourneyViewerProjection(v workspace.JourneyViewerProjection) *journeyv1.JourneyViewerProjection {
	if v.Responsibility == "" {
		return nil
	}
	out := &journeyv1.JourneyViewerProjection{
		Responsibility: journeyv1.JourneyViewerResponsibility(journeyv1.JourneyViewerResponsibility_value["JOURNEY_VIEWER_RESPONSIBILITY_"+string(v.Responsibility)]),
		NextStep:       journeyv1.JourneyNextStep(journeyv1.JourneyNextStep_value["JOURNEY_NEXT_STEP_"+string(v.NextStep)]),
		NextStepOwner:  journeyv1.JourneyStepOwner(journeyv1.JourneyStepOwner_value["JOURNEY_STEP_OWNER_"+string(v.NextStepOwner)]),
		AwaitsPerson:   v.AwaitsPerson,
		Closed:         v.Closed,
	}
	for _, relationship := range v.Relationships {
		if value, ok := journeyv1.JourneyViewerRelationship_value["JOURNEY_VIEWER_RELATIONSHIP_"+string(relationship)]; ok && value != 0 {
			out.Relationships = append(out.Relationships, journeyv1.JourneyViewerRelationship(value))
		}
	}
	return out
}

// toFinding renders one simulation finding.
func toFinding(f workspace.JourneyFinding) *journeyv1.Finding {
	return &journeyv1.Finding{Severity: f.Severity, Code: f.Code, Message: f.Message}
}

// toInstance renders the workflow instance, or nil when the journey has not
// been executed or the caller is not [server.diagnosticsAuthorized]. The
// instance is diagnostic-only end to end (no page has ever rendered its
// fields for a business purpose), so an unauthorized caller gets nil
// regardless of whether the journey actually has one -- the same absent
// shape either way, never a hint that distinguishes the two.
func toInstance(i *workspace.JourneyInstance, diagAuthorized bool) *journeyv1.Instance {
	if i == nil || !diagAuthorized {
		return nil
	}
	return &journeyv1.Instance{
		InstanceId:      i.InstanceID,
		InstanceVersion: i.InstanceVersion,
		WorkflowId:      i.WorkflowID,
		WorkflowVersion: i.WorkflowVersion,
		PlanDigest:      i.PlanDigest,
		Status:          i.Status,
		CurrentNodeIds:  append([]string(nil), i.CurrentNodeIDs...),
		CorrelationId:   i.CorrelationID,
		CreatedAt:       toTimestamp(i.CreatedAt),
		StartedAt:       toOptionalTimestamp(i.StartedAt),
		CompletedAt:     toOptionalTimestamp(i.CompletedAt),
	}
}

// toNode renders one durable node-execution row.
func toNode(n workspace.JourneyNode) *journeyv1.NodeExecution {
	return &journeyv1.NodeExecution{
		NodeId:      n.NodeID,
		Attempt:     int32(n.Attempt),
		StepType:    n.StepType,
		Status:      n.Status,
		TraceId:     n.TraceID,
		StartedAt:   toOptionalTimestamp(n.StartedAt),
		CompletedAt: toOptionalTimestamp(n.CompletedAt),
		RecordedAt:  toTimestamp(n.RecordedAt),
	}
}

// toWorkItem projects one durable human-work row down to what the journey
// page shows. This is deliberately lossy in one direction only: the page
// needs to know who owes the decision and where the item stands, not the
// whole governance frame hcmnext.admin.v1.WorkItemProfile renders. The uuid
// identifiers travel as their canonical strings because
// definitions/architecture/dependency-roles.yaml does not admit
// internal/transport as an import root for github.com/google/uuid.
//
// WorkItemId is withheld unless diagAuthorized: PROMOUX-008 names "work-item
// UUIDs" as an internal the ordinary approver never needs, and no RPC this
// service exposes accepts a work item id back as input (DecideJourney acts
// on the intent id) -- so it is pure diagnostic identity, while every other
// field here (status, owner, claim, deadline) is what the approval
// disposition card actually renders and must keep carrying regardless of
// diagnostics authority.
func toWorkItem(w workitem.WorkItem, diagAuthorized bool) *journeyv1.WorkItem {
	workItemID := ""
	if diagAuthorized {
		workItemID = w.WorkItemID.String()
	}
	return &journeyv1.WorkItem{
		WorkItemId:     workItemID,
		Kind:           string(w.Kind),
		Status:         string(w.Status),
		WorkType:       w.WorkType,
		NodeId:         w.NodeID,
		OwnerRef:       w.OwnerRef,
		ChosenOwner:    w.Assignment.ChosenOwner,
		ClaimedBy:      w.ClaimedBy,
		ClaimedAt:      toOptionalTimestamp(w.ClaimedAt),
		ClaimExpiresAt: toOptionalTimestamp(w.ClaimExpiresAt),
		CompletedBy:    w.CompletedBy,
		CompletedAt:    toOptionalTimestamp(w.CompletedAt),
		DeadlineAt:     toTimestamp(w.DeadlineAt),
		CreatedAt:      toTimestamp(w.CreatedAt),
		ItemVersion:    w.ItemVersion,
	}
}

// toTransition renders one immutable WorkItem transition.
func toTransition(t workspace.JourneyTransition) *journeyv1.WorkItemTransition {
	return &journeyv1.WorkItemTransition{
		WorkItemId: t.WorkItemID,
		From:       t.From,
		To:         t.To,
		Actor:      t.Actor,
		Reason:     t.Reason,
		At:         toTimestamp(t.At),
	}
}

// toLedger renders the one governed business write, or nil when the END
// node's terminal write has not been recorded.
func toLedger(l *workspace.JourneyLedgerEvent) *journeyv1.LedgerEvent {
	if l == nil {
		return nil
	}
	return &journeyv1.LedgerEvent{
		StreamKey:      l.StreamKey,
		Sequence:       l.Sequence,
		SchemaRef:      l.SchemaRef,
		Digest:         l.Digest,
		IdempotencyKey: l.IdempotencyKey,
		OccurredAt:     toTimestamp(l.OccurredAt),
		EffectiveAt:    toTimestamp(l.EffectiveAt),
		RecordedAt:     toTimestamp(l.RecordedAt),
	}
}

// toTimelineEvent renders one entry of the engine-composed timeline.
func toTimelineEvent(e workspace.JourneyEvent) *journeyv1.TimelineEvent {
	return &journeyv1.TimelineEvent{
		At:     toTimestamp(e.At),
		Actor:  e.Actor,
		Kind:   e.Kind,
		Title:  e.Title,
		Detail: e.Detail,
		Ref:    e.Ref,
	}
}

// toDetail renders the whole detail and stamps its change-detection digest.
// The digest is computed last, over the finished message, so it covers every
// field the page can see.
//
// diagAuthorized (PROMOUX-008) is the one boolean [server.diagnosticsAuthor
// ized] computes for the caller; it decides which of the two projections
// over this single, already-fetched d a caller receives. Timeline is the
// business projection -- already human-readable history, sent unconditionally
// to every caller who could reach InspectJourney at all. PlannedWrites (raw
// SET operations), Instance (workflow/instance internals), Nodes (node
// executions and trace ids) and Ledger (the stream key, schema ref and
// digest of the terminal write) and EvidenceIds are the diagnostic
// projection: every one of them is withheld -- nil or empty, never merely
// hidden -- for a caller this todo does not authorize, because nothing this
// service or any page built on it renders from them for an ordinary
// reviewer. WorkItems is not part of either list: it stays populated either
// way because the approval disposition an ordinary approver needs comes
// from it (see toWorkItem); only its WorkItemId is diagnostic-only and is
// blanked per item instead.
func toDetail(d workspace.JourneyDetail, diagAuthorized bool) *journeyv1.JourneyDetail {
	out := &journeyv1.JourneyDetail{
		Journey:              toJourney(d.Summary, diagAuthorized),
		Instance:             toInstance(d.Instance, diagAuthorized),
		Approver:             d.Approver,
		DiagnosticsAvailable: d.DiagnosticsAvailable,
		CanDecide:            d.CanDecide,
	}
	if diagAuthorized {
		out.PlannedWrites = append([]string(nil), d.PlannedWrites...)
		out.Ledger = toLedger(d.Ledger)
		out.EvidenceIds = append([]string(nil), d.EvidenceIDs...)
	}
	for _, f := range d.Findings {
		out.Findings = append(out.Findings, toFinding(f))
	}
	if diagAuthorized {
		for _, n := range d.Nodes {
			out.Nodes = append(out.Nodes, toNode(n))
		}
		for _, t := range d.Transitions {
			out.Transitions = append(out.Transitions, toTransition(t))
		}
	}
	for _, w := range d.WorkItems {
		out.WorkItems = append(out.WorkItems, toWorkItem(w, diagAuthorized))
	}
	for _, e := range d.Timeline {
		out.Timeline = append(out.Timeline, toTimelineEvent(e))
	}
	out.DetailDigest = detailDigest(out)
	return out
}

// digestPrefix labels the hash algorithm, matching how every other digest in
// this repository is written on the wire.
const digestPrefix = "sha256:"

// detailDigest is the SHA-256 of d's deterministically marshalled canonical
// Protobuf bytes with detail_digest itself cleared, so the digest is a
// function of the content only and never of a previously stamped digest.
// Marshalling is deterministic rather than merely stable-in-practice:
// proto.Marshal makes no ordering guarantee across processes, and a change
// detector that reports spurious changes is worse than none.
//
// It is not a governance artifact and never travels as one. The material
// proposal digest the simulation minted is on Journey; this is a cache key.
func detailDigest(d *journeyv1.JourneyDetail) string {
	if d == nil {
		return ""
	}
	subject := d
	if d.GetDetailDigest() != "" {
		subject = proto.Clone(d).(*journeyv1.JourneyDetail)
		subject.DetailDigest = ""
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(subject)
	if err != nil {
		// A message built entirely from in-memory Go values cannot fail to
		// marshal. Returning an empty digest rather than panicking keeps a
		// read surface answering; an empty digest never equals a client's
		// since_digest, so the only consequence is that a watch answers
		// immediately instead of waiting.
		return ""
	}
	sum := sha256.Sum256(raw)
	return digestPrefix + hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Workforce
// ---------------------------------------------------------------------------

// toWorker renders one worker listing row. Every field is copied, including
// the empty compensation fields a corpus worker carries: an absent baseline is
// a fact about that worker, and filling it in here would be this service
// asserting a salary it was never told.
func toWorker(w workspace.WorkerSummary) *journeyv1.Worker {
	return &journeyv1.Worker{
		WorkerRef:           w.WorkerRef,
		WorkerId:            w.WorkerID,
		SubjectRevision:     w.SubjectRevision,
		LegalName:           w.LegalName,
		PreferredName:       w.PreferredName,
		WorkerNumber:        w.WorkerNumber,
		JobCode:             w.JobCode,
		JobTitle:            w.JobTitle,
		Grade:               w.Grade,
		OrgUnit:             w.OrgUnit,
		PositionId:          w.PositionID,
		Location:            w.Location,
		PayZone:             w.PayZone,
		BasePay:             w.BasePay,
		Currency:            w.Currency,
		BonusTarget:         w.BonusTarget,
		HireDate:            w.HireDate,
		Source:              w.Source,
		CreatedAt:           toTimestamp(w.CreatedAt),
		ManagerRef:          w.ManagerRef,
		ManagerRelationship: toManagerRelationship(w.ManagerDisposition, w.ManagerWorkerRef),
		ProfilePhotoUrl:     w.ProfilePhotoURL,
	}
}

func toManagerRelationship(disposition, managerWorkerRef string) *journeyv1.ManagerRelationshipProjection {
	if disposition == "" && managerWorkerRef == "" {
		return nil
	}
	value := journeyv1.ManagerRelationshipProjection_DISPOSITION_UNSPECIFIED
	switch disposition {
	case workspace.ManagerRelationshipRoot:
		value = journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT
	case workspace.ManagerRelationshipVisible:
		value = journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE
	case workspace.ManagerRelationshipWithheld:
		value = journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD
	case workspace.ManagerRelationshipOrphan:
		value = journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN
	}
	return &journeyv1.ManagerRelationshipProjection{Disposition: value, ManagerWorkerRef: managerWorkerRef}
}

// fromWorker is [toWorker]'s inverse.
func fromWorker(w *journeyv1.Worker) workspace.WorkerSummary {
	return workspace.WorkerSummary{
		WorkerRef:          w.GetWorkerRef(),
		WorkerID:           w.GetWorkerId(),
		SubjectRevision:    w.GetSubjectRevision(),
		LegalName:          w.GetLegalName(),
		PreferredName:      w.GetPreferredName(),
		WorkerNumber:       w.GetWorkerNumber(),
		JobCode:            w.GetJobCode(),
		JobTitle:           w.GetJobTitle(),
		Grade:              w.GetGrade(),
		OrgUnit:            w.GetOrgUnit(),
		PositionID:         w.GetPositionId(),
		Location:           w.GetLocation(),
		PayZone:            w.GetPayZone(),
		BasePay:            w.GetBasePay(),
		Currency:           w.GetCurrency(),
		BonusTarget:        w.GetBonusTarget(),
		HireDate:           w.GetHireDate(),
		Source:             w.GetSource(),
		CreatedAt:          fromTimestamp(w.GetCreatedAt()),
		ManagerRef:         w.GetManagerRef(),
		ManagerDisposition: fromManagerRelationship(w.GetManagerRelationship()),
		ManagerWorkerRef:   managerWorkerRef(w.GetManagerRelationship()),
		ProfilePhotoURL:    w.GetProfilePhotoUrl(),
	}
}

func fromManagerRelationship(value *journeyv1.ManagerRelationshipProjection) string {
	if value == nil {
		return ""
	}
	return fromManagerRelationshipDisposition(value.GetDisposition())
}

func managerWorkerRef(value *journeyv1.ManagerRelationshipProjection) string {
	if value == nil {
		return ""
	}
	return value.GetManagerWorkerRef()
}

func fromManagerRelationshipDisposition(value journeyv1.ManagerRelationshipProjection_Disposition) string {
	switch value {
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT:
		return workspace.ManagerRelationshipRoot
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE:
		return workspace.ManagerRelationshipVisible
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD:
		return workspace.ManagerRelationshipWithheld
	case journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN:
		return workspace.ManagerRelationshipOrphan
	default:
		return ""
	}
}

// toWorkforceOptions renders the closed placement set.
func toWorkforceOptions(o workspace.WorkforceOptions) *journeyv1.WorkforceOptions {
	out := &journeyv1.WorkforceOptions{
		JobCodes:  o.JobCodes,
		Grades:    o.Grades,
		OrgUnits:  o.OrgUnits,
		PayZones:  o.PayZones,
		Positions: o.Positions,
		Currency:  o.Currency,
	}
	for _, placement := range o.Placements {
		out.Placements = append(out.Placements, &journeyv1.WorkforcePlacementOption{
			JobCode: placement.JobCode, Grade: placement.Grade,
			PayZone: placement.PayZone, Currency: placement.Currency,
		})
	}
	for _, path := range o.PromotionPaths {
		out.PromotionPaths = append(out.PromotionPaths, &journeyv1.PromotionPathOption{
			PathRef: path.PathRef, Revision: path.Revision,
			SourceProfileRef: path.SourceProfileRef,
			SourceJobCode:    path.SourceJobCode, SourceGrade: path.SourceGrade,
			TargetProfileRef: path.TargetProfileRef,
			TargetJobCode:    path.TargetJobCode, TargetGrade: path.TargetGrade,
			TargetTitle: path.TargetTitle, Kind: path.Kind,
			MinimumBaseIncrease:   path.MinimumBaseIncrease,
			MaximumBaseIncrease:   path.MaximumBaseIncrease,
			CompensationPolicyRef: path.CompensationPolicyRef,
			BenefitRuleRefs:       append([]string(nil), path.BenefitRuleRefs...),
		})
	}
	// UXLIVE-011: the vacancy list crosses whole. Reference is the one field
	// a proposal binds to, and it is carried verbatim -- this transport
	// never mints, shortens or re-derives it.
	for _, vacancy := range o.PositionVacancies {
		out.PositionVacancies = append(out.PositionVacancies, &journeyv1.PositionVacancyOption{
			Reference: vacancy.Reference, Title: vacancy.Title,
			Organization: vacancy.Organization, Manager: vacancy.Manager,
			Location: vacancy.Location, JobCode: vacancy.JobCode, OrgUnit: vacancy.OrgUnit,
			VacancyEnd: vacancy.VacancyEndISO, ReservationState: vacancy.ReservationState,
		})
	}
	return out
}

// fromWorkforceOptions is [toWorkforceOptions]'s inverse. A nil message is the
// zero options, so a response that omits the field is not a decoding failure.
func fromWorkforceOptions(o *journeyv1.WorkforceOptions) workspace.WorkforceOptions {
	out := workspace.WorkforceOptions{
		JobCodes:  o.GetJobCodes(),
		Grades:    o.GetGrades(),
		OrgUnits:  o.GetOrgUnits(),
		PayZones:  o.GetPayZones(),
		Positions: o.GetPositions(),
		Currency:  o.GetCurrency(),
	}
	for _, placement := range o.GetPlacements() {
		out.Placements = append(out.Placements, workspace.WorkforcePlacementOption{
			JobCode: placement.GetJobCode(), Grade: placement.GetGrade(),
			PayZone: placement.GetPayZone(), Currency: placement.GetCurrency(),
		})
	}
	for _, path := range o.GetPromotionPaths() {
		out.PromotionPaths = append(out.PromotionPaths, workspace.PromotionPathOption{
			PathRef: path.GetPathRef(), Revision: path.GetRevision(),
			SourceProfileRef: path.GetSourceProfileRef(),
			SourceJobCode:    path.GetSourceJobCode(), SourceGrade: path.GetSourceGrade(),
			TargetProfileRef: path.GetTargetProfileRef(),
			TargetJobCode:    path.GetTargetJobCode(), TargetGrade: path.GetTargetGrade(),
			TargetTitle: path.GetTargetTitle(), Kind: path.GetKind(),
			MinimumBaseIncrease:   path.GetMinimumBaseIncrease(),
			MaximumBaseIncrease:   path.GetMaximumBaseIncrease(),
			CompensationPolicyRef: path.GetCompensationPolicyRef(),
			BenefitRuleRefs:       append([]string(nil), path.GetBenefitRuleRefs()...),
		})
	}
	for _, vacancy := range o.GetPositionVacancies() {
		out.PositionVacancies = append(out.PositionVacancies, workspace.PositionVacancyOption{
			Reference: vacancy.GetReference(), Title: vacancy.GetTitle(),
			Organization: vacancy.GetOrganization(), Manager: vacancy.GetManager(),
			Location: vacancy.GetLocation(), JobCode: vacancy.GetJobCode(), OrgUnit: vacancy.GetOrgUnit(),
			VacancyEndISO: vacancy.GetVacancyEnd(), ReservationState: vacancy.GetReservationState(),
		})
	}
	return out
}

// fromCreateWorkerRequest reads the create form off the wire. It trims
// nothing and defaults nothing: which fields may be empty, and what an empty
// one means, is the engine's rule, and a transport that pre-filled them would
// be deciding it twice.
func fromCreateWorkerRequest(req *journeyv1.CreateWorkerRequest) workspace.WorkerInput {
	return workspace.WorkerInput{
		LegalName:     req.GetLegalName(),
		PreferredName: req.GetPreferredName(),
		JobCode:       req.GetJobCode(),
		Grade:         req.GetGrade(),
		OrgUnit:       req.GetOrgUnit(),
		PositionID:    req.GetPositionId(),
		Location:      req.GetLocation(),
		PayZone:       req.GetPayZone(),
		BasePay:       req.GetBasePay(),
		Currency:      req.GetCurrency(),
		BonusTarget:   req.GetBonusTarget(),
		HireDate:      req.GetHireDate(),
		ManagerRef:    req.GetManagerRef(),
	}
}

// toCreateWorkerRequest is [fromCreateWorkerRequest]'s inverse, so a client
// built on this package's own types produces the same message a page does.
func toCreateWorkerRequest(in workspace.WorkerInput) *journeyv1.CreateWorkerRequest {
	return &journeyv1.CreateWorkerRequest{
		LegalName:     in.LegalName,
		PreferredName: in.PreferredName,
		JobCode:       in.JobCode,
		Grade:         in.Grade,
		OrgUnit:       in.OrgUnit,
		PositionId:    in.PositionID,
		Location:      in.Location,
		PayZone:       in.PayZone,
		BasePay:       in.BasePay,
		Currency:      in.Currency,
		BonusTarget:   in.BonusTarget,
		HireDate:      in.HireDate,
		ManagerRef:    in.ManagerRef,
	}
}
