package app

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

// These bindings are the served application seam for the Leave domain. The
// domain package remains the owner of all lifecycle rules; this package only
// supplies immutable capability metadata and adapts typed Go calls to the
// existing gateway used by the shipped cell.
const (
	LeaveRequestCapabilityID = "hcmnext.workforce.leave_request"
)

// LeaveDeterminationInvocation is the typed application payload for the
// determination capability. Assessment is produced by the messaging plane;
// the adapter never treats provider acceptance as delivery.
type LeaveDeterminationInvocation struct {
	Input            leave.DeterminationInput
	Assessment       delivery.NoticeAssessment
	ProviderAccepted bool
	Acknowledged     bool
}

// LeaveEvidenceReviewInvocation is the typed application payload for one
// restricted review turn. A nil Resume only requests more information; a
// non-nil Resume carries one already governed evidence reference.
type LeaveEvidenceReviewInvocation struct {
	Loop    leave.ReviewLoop
	Finding workreview.Finding
	Resume  *leave.EvidenceRef
}

func leaveSchema(id string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         "hcmnext.workforce.v1." + id,
		Version:          1,
		ProtobufFullName: "hcmnext.workforce.v1." + id,
	}
}

func leaveCapabilityDefinition(id, request, response string, effect capability.EffectClass, scope, testRef string, reads []string) capability.Definition {
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: "workforce.leave",
		RequestSchema: leaveSchema(request), ResponseSchema: leaveSchema(response), ErrorSchema: leaveSchema("LeaveError"),
		EffectClass: effect, ReadData: capability.DataDomainFieldSet{DataDomains: reads},
		RiskClass: "R2", IdempotencyPolicyRef: "idempotency.tenant-canonical-request.v1",
		AuthZScopeRef: scope, LegalBasisRef: "legal.leave.hypothetical.v1",
		EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1",
		TestRef: testRef,
	}
}

func leaveCapabilityBindings() []struct {
	definition capability.Definition
} {
	return []struct {
		definition capability.Definition
	}{
		{
			definition: leaveCapabilityDefinition(LeaveRequestCapabilityID, "RequestLeaveRequest", "RequestLeaveResult", capability.EffectReadOnly, "scope:workforce.leave.request", "conformance:LEAVE-016/served/v1", []string{"worker", "leave", "legal", "messaging", "evidence", "workflow"}),
		},
	}
}

// LeaveRequestCapabilityResult is the result of the idempotent process
// anchor. It records no workforce mutation or external effect.
type LeaveRequestCapabilityResult struct {
	Record  leave.AnchorRecord
	Created bool
}

func serveLeaveDetermination(ctx context.Context, payload any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call, ok := payload.(LeaveDeterminationInvocation)
	if !ok {
		return nil, fmt.Errorf("app: leave determination expects LeaveDeterminationInvocation, got %T", payload)
	}
	determination, err := leave.RenderDetermination(call.Input)
	if err != nil {
		return nil, err
	}
	return leave.DeliverDetermination(determination, call.Input, call.Assessment, call.ProviderAccepted, call.Acknowledged)
}

func serveLeaveEvidenceReview(ctx context.Context, payload any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call, ok := payload.(LeaveEvidenceReviewInvocation)
	if !ok {
		return nil, fmt.Errorf("app: leave evidence review expects LeaveEvidenceReviewInvocation, got %T", payload)
	}
	loop, err := leave.RequestMoreInfo(call.Loop, call.Finding)
	if err != nil || call.Resume == nil {
		return loop, err
	}
	return leave.ResumeEvidence(loop, *call.Resume)
}

func serveLeaveMedicalTrace(ctx context.Context, payload any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, ok := payload.(leave.TraceInput)
	if !ok {
		return nil, fmt.Errorf("app: medical leave trace expects leave.TraceInput, got %T", payload)
	}
	return leave.TraceMedicalLeave(input, nil)
}
