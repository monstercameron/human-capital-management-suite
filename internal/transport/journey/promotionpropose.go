package journey

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// The wire half of the intent-only `promotion.propose` contract (PROMO-007).
//
// hcmnext.journey.v1.ProposePromotionRequest has no field a caller could use
// to assert a server-owned fact - no current pay, no current manager, no
// position vacancy, no budget authority - and no field for selecting a
// principal, tenant, session, locale or legal context, because the
// trusted-request boundary derives all of those from the verified credential.
// That leaves exactly one way to try: append bytes the message does not
// define and hope somebody reads them.
//
// Nothing in this file has to refuse those bytes, and that is deliberate
// rather than an omission. Strict decoding is part of the shared
// trusted-request boundary every method on this server is admitted through
// (internal/transport.Validate walks the decoded message and reports
// strict_decoding.unknown_field), so a request carrying an undefined field is
// already refused INVALID_ARGUMENT before any handler runs - identically on
// direct gRPC and on the GoGRPCBridge tunnel, because it is the same
// interceptor chain on the same server. Repeating the check here would add a
// second place for the answer to be, and the two could disagree.
//
// internal/intent/app refuses the same bytes again on the application side.
// Those two are not redundant with each other: the boundary protects the wire,
// and the application service protects itself from an in-process caller that
// never crossed one.

// PromotionProposer is the application port behind ProposePromotion.
//
// It is an optional extension of the composed journey engine rather than a
// field of its own on [Dependencies]: the cell publishes exactly one Promotion
// application service, [Dependencies.Engine] is that service, and a second
// wiring point would be a second way to reach the domains - which is the thing
// this contract exists to prevent. An engine that does not implement it makes
// ProposePromotion answer UNAVAILABLE, the same way a nil engine does.
//
// It exchanges the generated messages rather than plain Go values because the
// response is entirely server-derived: there is no page-shaped port type for
// it to be projected from, and inventing one would put a second definition of
// the contract in the tree.
type PromotionProposer interface {
	ProposePromotion(context.Context, *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error)
}

// ProposePromotion serves the intent-only promotion.propose contract.
//
// Effect class: governed write through the intent service. The handler admits
// the caller through the shared trusted-request boundary and forwards to the
// one Promotion application service, which runs CapabilityGateway -> the
// governed worker read -> IntentService -> preflight. It decides no business
// rule and reads no table.
func (s *server) ProposePromotion(
	ctx context.Context, req *journeyv1.ProposePromotionRequest,
) (*journeyv1.ProposePromotionResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "journeys", "promotion_request", roleaccess.ActionCreate); err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "propose_promotion")
	if depErr != nil {
		return nil, depErr
	}
	proposer, ok := eng.(PromotionProposer)
	if !ok {
		// A cell whose engine predates this contract is told so, rather than
		// being answered with the generated service's Unimplemented: the
		// surface exists, and the client's next move depends on knowing that
		// it is this deployment and not the API that cannot act.
		return nil, envelope.New(envelope.CodeUnavailable,
			"journey.propose_promotion.engine_unconfigured",
			"the journey engine is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(evidence(principal))
	}

	res, err := proposer.ProposePromotion(ctx, req)
	if err != nil {
		return nil, ownedError(err, principal, inv, "propose_promotion")
	}
	return res, nil
}
