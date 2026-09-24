package journey

import (
	"context"
	"errors"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// InvalidationSource is the hub WatchPromotionInvalidations subscribes to.
// internal/humanwork/journeyinvalidation.Hub implements it.
type InvalidationSource interface {
	Subscribe(journeyinvalidation.SubscribeRequest) (*journeyinvalidation.Subscription, error)
}

// invalidationAccess is the page access a subscription is opened under and
// re-checked against before every delivery: the same pair ListJourneys
// requires, because every hint names a change to data that list backs.
var invalidationAccess = []featureAccessRequest{
	{pageID: "journeys", featureID: "journey_list"},
	{pageID: "work", featureID: "assigned_queue"},
}

// WatchPromotionInvalidations streams the caller's own authority-filtered
// promotion invalidation hints (REV-091-03). READ_ONLY.
//
// This handler decides no authority of its own beyond the page gate every
// journey read applies: the subscription (internal/humanwork/
// journeyinvalidation) filters each committed transition through the engine's
// visibility read under this caller's context and through the product-query
// authorization check, and numbers only what it delivers. The page gate is
// re-evaluated before every delivery, so access withdrawn mid-stream ends the
// stream as a revocation rather than letting it run to its ceiling.
//
// Like WatchJourney, the request reaches this handler without admission's
// structural validation (grpc-go decodes a stream's request after the
// interceptors run). That is safe here because the request carries nothing
// trusted: region is checked against a closed set and after_sequence only
// offsets the caller's own numbering, bounded by the hub.
func (s *server) WatchPromotionInvalidations(req *journeyv1.WatchPromotionInvalidationsRequest, stream journeyv1.JourneyService_WatchPromotionInvalidationsServer) error {
	ctx := stream.Context()
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return ctxErr
	}
	if err := s.requireAnyFeatureView(ctx, principal, inv, invalidationAccess...); err != nil {
		return err
	}
	eng, depErr := s.engine(principal, inv, "invalidations")
	if depErr != nil {
		return depErr
	}
	if s.deps.Invalidations == nil {
		return invalidationError(envelope.CodeUnavailable, "journey.invalidations.unconfigured",
			"live updates are not configured on this cell", principal, inv)
	}
	region := app.PromotionRegion(req.GetRegion())
	if _, err := region.Projection(); err != nil {
		return invalidationError(envelope.CodeInvalidArgument, "journey.invalidations.region",
			"the region is not one this service publishes", principal, inv).
			WithViolation("region", "use SHELL_COUNT, JOURNEYS, MY_WORK, PERSON or DETAIL", "journey.invalidations.region")
	}
	if req.GetAfterSequence() > journeyinvalidation.MaxAfterSequence {
		return invalidationError(envelope.CodeInvalidArgument, "journey.invalidations.after_sequence",
			"the resume position is out of range", principal, inv).
			WithViolation("after_sequence", "reconnect with the last sequence this client received", "journey.invalidations.after_sequence")
	}
	sub, err := s.deps.Invalidations.Subscribe(journeyinvalidation.SubscribeRequest{
		Principal: principal, Region: region, AfterSequence: req.GetAfterSequence(), Inspector: eng,
		Authorize: func(ctx context.Context) error {
			return s.requireAnyFeatureView(ctx, principal, inv, invalidationAccess...)
		},
	})
	if err != nil {
		if errors.Is(err, journeyinvalidation.ErrTooManySubscribers) {
			return invalidationError(envelope.CodeResourceExhausted, "journey.invalidations.capacity",
				"too many live update streams are open; try again shortly", principal, inv)
		}
		return ownedError(err, principal, inv, "invalidations")
	}
	defer sub.Close()

	// The same ceiling WatchJourney observes: the client reconnects carrying
	// its last sequence, and nothing in this process holds a stream forever.
	streamCtx, cancel := context.WithTimeout(ctx, watchMaxLifetime)
	defer cancel()
	for {
		raw, nextErr := sub.Next(streamCtx)
		switch {
		case nextErr == nil:
		case streamCtx.Err() != nil:
			// The client went away, or the ceiling elapsed: a completed
			// stream either way.
			return nil
		case errors.Is(nextErr, journeyinvalidation.ErrLagged):
			return invalidationError(envelope.CodeAborted, "journey.invalidations.lagged",
				"live updates fell behind; reconnect to refresh", principal, inv)
		case errors.Is(nextErr, journeyinvalidation.ErrRevoked):
			return revokedError(nextErr, principal, inv)
		default:
			return ownedError(nextErr, principal, inv, "invalidations")
		}
		if sendErr := stream.Send(&journeyv1.WatchPromotionInvalidationsResponse{Invalidation: raw}); sendErr != nil {
			return sendErr
		}
	}
}

func invalidationError(code envelope.Code, reason, message string, principal *trust.Principal, inv *transport.Invocation) *envelope.Error {
	out := envelope.New(code, reason, message)
	if inv != nil {
		out = out.WithCorrelation(inv.RequestID())
	}
	if principal != nil {
		out = out.WithEvidence(evidence(principal))
	}
	return out
}
