package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// InvalidationStream is the receiving half of one
// WatchPromotionInvalidations call (REV-091-03), narrowed to the one method
// the product client uses.
type InvalidationStream interface {
	Recv() (*journeyv1.WatchPromotionInvalidationsResponse, error)
}

// InvalidationService is the optional live-update extension the production
// gRPC client implements. It is separate from [Service] for the same reason
// [PreferenceService] is: the workflow App's test seam stays narrowly about
// journey execution, and an embedding without live updates simply does not
// offer it.
type InvalidationService interface {
	WatchPromotionInvalidations(ctx context.Context, in *journeyv1.WatchPromotionInvalidationsRequest) (InvalidationStream, error)
}

var _ InvalidationService = (*grpcService)(nil)

func (s *grpcService) WatchPromotionInvalidations(ctx context.Context, in *journeyv1.WatchPromotionInvalidationsRequest) (InvalidationStream, error) {
	stream, err := s.client.WatchPromotionInvalidations(ctx, in)
	if err != nil {
		return nil, err
	}
	return stream, nil
}
