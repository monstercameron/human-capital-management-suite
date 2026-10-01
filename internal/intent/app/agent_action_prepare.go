package app

import (
	"context"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"go.opentelemetry.io/otel/trace"
)

type preparedAgentPromotion struct {
	principal  *trust.Principal
	worker     values.EntityRef
	baseline   journeyBaselineFacts
	definition intent.Definition
	payload    []byte
}

func (e *journeyEngine) prepareAgentPromotion(ctx context.Context, in workspace.ProposalInput) (*preparedAgentPromotion, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateProposalInput(in); err != nil {
		return nil, err
	}
	// The reference is resolved against both populations -- the fixed corpus
	// and this tenant's own created workers -- because a journey has to be
	// proposable for an employee somebody just made, not only for the four
	// this release ships with.
	subject, resolved, err := e.locate(ctx, principal.Tenant(), in.WorkerRef)
	if err != nil {
		return nil, err
	}
	if !resolved {
		return nil, journeyInputError("worker_ref", "no such worker in this workforce")
	}
	if journeySubjectIsViewer(principal, subject) {
		return nil, journeySelfPromotionError()
	}
	worker := subject.Ref

	baseline, err := journeyBaseline(in, subject)
	if err != nil {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.unavailable")
		return nil, err
	}
	if subject.Created != nil {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.durable_worker")
	} else {
		trace.SpanFromContext(ctx).AddEvent("promotion.baseline.declared_reference")
	}
	// WF-RUN-034: a worker whose earlier promotion has committed is proposed
	// from the pay the aggregates now record, not the pay journey_worker
	// froze when they were created. The placement half of the same fact
	// reaches currentPlacement through the governed read's own overlay.
	if subject.Created != nil {
		committed, found, committedErr := e.committedPay(ctx, principal, subject.Created.WorkerID.String())
		if committedErr != nil {
			return nil, committedErr
		}
		if found {
			baseline.currentBase, baseline.currency = committed.BasePay, committed.Currency
		}
	}
	current, err := e.currentPlacement(ctx, principal, worker, baseline.effective)
	if err != nil {
		return nil, err
	}
	ladder, err := e.ladderEdges(ctx, principal.Tenant())
	if err != nil {
		return nil, err
	}
	if err := validatePublishedPromotionPathFrom(ladder, current, in, baseline); err != nil {
		return nil, err
	}
	// An optional target position remains optional. The proposal must preserve
	// the proposer's choice; selecting a catalog vacancy here would bind a seat
	// the form deliberately left unnamed.

	def, ownedErr := e.svc.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1})
	if ownedErr != nil {
		return nil, journeyError(envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(ownedErr))
	}
	payload, err := journeyRequestPayload(in, subject.Key, current, baseline)
	if err != nil {
		return nil, err
	}
	return &preparedAgentPromotion{principal: principal, worker: worker, baseline: baseline, definition: def, payload: payload}, nil
}

// PrepareAgentPromotionRequest performs the owner's governed reads and validates
// the published promotion path without reserving a window or persisting a draft.
func (s *IntentService) PrepareAgentPromotionRequest(ctx context.Context, in workspace.ProposalInput) (*intentsv1.CreateIntentRequest, error) {
	e, ok := s.proposalDecisioner.(*journeyEngine)
	if !ok || e == nil {
		return nil, ErrAgentActionDraft
	}
	prepared, err := e.prepareAgentPromotion(ctx, in)
	if err != nil {
		return nil, err
	}
	def := prepared.definition
	return &intentsv1.CreateIntentRequest{Definition: &intentsv1.DefinitionReference{IntentTypeId: def.Ref.TypeID, Version: def.Ref.Version}, Subjects: journeySubjects(prepared.worker.Id, in.TargetPositionID), Request: &intentsv1.TypedPayload{Schema: &intentsv1.SchemaReference{SchemaId: def.InputSchema.SchemaID, Version: def.InputSchema.Version, ProtobufFullName: def.InputSchema.ProtobufFullName}, ProtobufWireBytes: prepared.payload}, ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE}, nil
}
