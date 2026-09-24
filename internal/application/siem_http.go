package application

import (
	"errors"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/siemstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/subscriptionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/siemhttp"
)

// composeSIEMHTTP adds the governed customer feed when durable storage and an
// explicit signing credential resolver are both available.
func composeSIEMHTTP(next http.Handler, config transport.Config, rings SIEMRingResolver, pool *pgxadapter.Pool) http.Handler {
	if pool == nil || rings == nil {
		return next
	}
	feed := SIEMFeedService{
		Governance: subscriptionstore.New(pool),
		Feed:       siemstore.New(pool),
	}
	handler := siemhttp.Handler{
		Config: config,
		Reader: feed,
		Rings:  rings,
		ClassifyReadError: func(err error) int {
			if errors.Is(err, siem.ErrInvalidCursor) {
				return http.StatusConflict
			}
			if errors.Is(err, ErrSIEMFeedGovernance) {
				return http.StatusForbidden
			}
			return http.StatusServiceUnavailable
		},
	}
	return siemhttp.Overlay(next, handler)
}
