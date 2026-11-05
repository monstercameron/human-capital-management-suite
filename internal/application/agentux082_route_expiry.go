package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// agentUX082RouteWindowReader is the deployment history behind a route reader:
// the latest deployment for the exact manifest regardless of its window.
type agentUX082RouteWindowReader interface {
	LatestPersonaModelRouteDeployment(context.Context, uuid.UUID, string, string, int64, int64, string) (agentstore.PersonaModelRoutePolicy, error)
}

// agentUX082RouteExpired reports whether a route lookup that found no current
// policy failed because the deployment lapsed (true: tell the asker the setup
// expired and who can renew it) rather than because none was ever published
// (false: the generic unavailable sentence). A reader without deployment
// history never reports expiry, so legacy readers keep their behavior.
func agentUX082RouteExpired(ctx context.Context, routes PersonaModelRoutePolicyReader, tenant uuid.UUID, entity string, ref agentmanifest.Reference, agentDigest string, now time.Time) bool {
	window, ok := routes.(agentUX082RouteWindowReader)
	if !ok {
		return false
	}
	latest, err := window.LatestPersonaModelRouteDeployment(ctx, tenant, entity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), agentDigest)
	if err != nil || latest.EffectiveUntil == nil {
		return false
	}
	return !now.Before(*latest.EffectiveUntil)
}
