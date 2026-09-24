package journey

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// ProposePromotionProcedure is the canonical procedure name shared by the
// native gRPC and Connect/HTTP transports.
const ProposePromotionProcedure = "/hcmnext.journey.v1.JourneyService/ProposePromotion"

// ProposeIntoManagementProcedure is the versioned semantic HTTP projection
// published for the typed promotion entry point. It deliberately points at
// the same handler as ProposePromotionProcedure: the route is a transport
// projection, not a second promotion implementation.
const ProposeIntoManagementProcedure = "/v1/promotions:proposeIntoManagement"

// CorrectWorkLoopProcedure is the served HTTP projection for a governed
// correction request. It resolves current revision authority from the
// journey engine and routes through app.RouteCorrection.
const CorrectWorkLoopProcedure = "/v1/work-loop:correct"

// NewProposePromotionHandler returns the typed HTTP handler for the
// no-effect promotion proposal. Admission is supplied by the edge through
// opts; this package intentionally does not inspect headers or authenticate
// a request a second time.
func NewProposePromotionHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	return newProposePromotionHandler(ProposePromotionProcedure, deps, opts...)
}

// NewProposeIntoManagementHandler returns the versioned HTTP projection for
// PromotionService.ProposeIntoManagement. The generated gRPC contract in this
// repository names the same operation JourneyService.ProposePromotion; both
// projections call the one application port and therefore have identical
// admission, validation, idempotency and no-effect semantics.
func NewProposeIntoManagementHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	return newProposePromotionHandler(ProposeIntoManagementProcedure, deps, opts...)
}

func newProposePromotionHandler(procedure string, deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	return connect.NewUnaryHandler(
		procedure,
		func(ctx context.Context, req *connect.Request[journeyv1.ProposePromotionRequest]) (*connect.Response[journeyv1.ProposePromotionResponse], error) {
			res, err := s.ProposePromotion(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(res), nil
		},
		opts...,
	)
}

// NewCorrectWorkLoopHandler returns the admitted HTTP route for a correction
// or repair request. Caller-supplied tenant, requester, and timestamps are
// intentionally absent: the handler binds those from admitted context and
// the server clock.
func NewCorrectWorkLoopHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	return connect.NewUnaryHandler(
		CorrectWorkLoopProcedure,
		func(ctx context.Context, req *connect.Request[structpb.Struct]) (*connect.Response[structpb.Struct], error) {
			principal, inv, refusal := trustedContext(ctx)
			if refusal != nil {
				return nil, refusal
			}
			input, err := correctionInput(req.Msg)
			if err != nil {
				return nil, envelope.New(envelope.CodeInvalidArgument, "journey.correct.input", "the request input is not acceptable").WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
			}
			eng, unavailable := s.engine(principal, inv, "correct")
			if unavailable != nil {
				return nil, unavailable
			}
			detail, err := eng.Inspect(ctx, input.IntentID)
			if err != nil {
				return nil, ownedError(err, principal, inv, "correct")
			}
			at := values.NewInstant(s.deps.nowFunc()())
			input.Tenant = principal.Tenant()
			input.RequestedBy = principal.Subject()
			input.RequestedAt = at
			decision, err := app.RouteCorrection(input, principal.Tenant(), detail.Summary.ProposalRevisionID, detail.Summary.MaterialDigest, at)
			if err != nil {
				if errors.Is(err, app.ErrCorrectionTampered) {
					return nil, envelope.New(envelope.CodeFailedPrecondition, "journey.correct.tampered", "the proposal content no longer matches its revision").WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
				}
				return nil, envelope.New(envelope.CodeInvalidArgument, "journey.correct.refused", "the correction request is not acceptable").WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
			}
			response, err := correctionResponse(decision)
			if err != nil {
				return nil, envelope.New(envelope.CodeUnspecified, "journey.correct.failed", "the request could not be completed").WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
			}
			return connect.NewResponse(response), nil
		},
		opts...,
	)
}

func correctionInput(msg *structpb.Struct) (app.CorrectionRequest, error) {
	if msg == nil {
		return app.CorrectionRequest{}, errors.New("request is required")
	}
	fields := msg.GetFields()
	allowed := map[string]bool{"intent_id": true, "proposal_revision_id": true, "proposal_digest": true, "kind": true, "reason": true}
	for name := range fields {
		if !allowed[name] {
			return app.CorrectionRequest{}, errors.New("unknown field: " + name)
		}
	}
	read := func(name string) (string, error) {
		field := fields[name]
		if field == nil || field.GetKind() == nil {
			return "", errors.New(name + " is required")
		}
		value, ok := field.GetKind().(*structpb.Value_StringValue)
		if !ok {
			return "", errors.New(name + " must be a string")
		}
		return value.StringValue, nil
	}
	var request app.CorrectionRequest
	var err error
	if request.IntentID, err = read("intent_id"); err != nil {
		return request, err
	}
	if request.ProposalRevisionID, err = read("proposal_revision_id"); err != nil {
		return request, err
	}
	if request.ProposalDigest, err = read("proposal_digest"); err != nil {
		return request, err
	}
	if request.Kind, err = read("kind"); err != nil {
		return request, err
	}
	if request.Reason, err = read("reason"); err != nil {
		return request, err
	}
	return request, nil
}

func correctionResponse(decision app.CorrectionDecision) (*structpb.Struct, error) {
	return structpb.NewStruct(map[string]any{
		"tenant": decision.Tenant.String(), "intent_id": decision.IntentID,
		"proposal_revision_id": decision.ProposalRevisionID, "proposal_digest": decision.ProposalDigest,
		"kind": decision.Kind, "route": decision.Route, "allowed": decision.Allowed,
		"decided_at": decision.DecidedAt.Time().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"digest":     decision.Digest,
	})
}
