package journeyinvalidation

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
)

// EmitForSubscriber authorizes an invalidation before minting its private,
// gap-free delivery number. A filtered transition never advances the number,
// so a viewer cannot infer another worker's activity from missing positions.
func EmitForSubscriber(s *promotion.SubscriberSequencer, subscriberKey string, region promotion.Region, req productquery.InvalidationRequest) (productquery.InvalidationMessage, bool, error) {
	if s == nil || subscriberKey == "" {
		// Advance preserves the domain's classified refusal for invalid inputs.
		_, _, err := s.Advance(subscriberKey, region)
		return productquery.InvalidationMessage{}, false, err
	}
	projection, err := region.Projection()
	if err != nil {
		return productquery.InvalidationMessage{}, false, err
	}
	req.Projection = projection
	message, ok, err := productquery.EmitInvalidation(req)
	if err != nil || !ok {
		return productquery.InvalidationMessage{}, false, err
	}
	local, watermark, err := s.Advance(subscriberKey, region)
	if err != nil {
		return productquery.InvalidationMessage{}, false, err
	}
	message.SourceSequence, message.Watermark = local, watermark
	if err := message.Validate(); err != nil {
		return productquery.InvalidationMessage{}, false, err
	}
	return message, true, nil
}
