package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// personaAdminServeFactory binds one fully composed metadata-only client to
// an admitted request. The client itself re-authorizes every read; this outer
// check prevents an unverified transport context from receiving even a client
// value that could be mistaken for an available admin surface.
type personaAdminServeFactory struct {
	route PersonaAdminRoute
}

// ClientForRequest returns the catalog only for a verified human principal.
// Missing or non-human trust context fails closed and leaves the route
// unavailable.
func (f personaAdminServeFactory) ClientForRequest(ctx context.Context) productui.PersonaAdminClient {
	return f.route.ClientForRequest(ctx)
}

// newPersonaAdminServeFactory wraps a completely composed client for the
// workspace route. Callers must pass nil when any target, grant, skill, store,
// or permission dependency is absent; this helper never constructs fallbacks.
func newPersonaAdminServeFactory(client productui.PersonaAdminClient) interface {
	ClientForRequest(context.Context) productui.PersonaAdminClient
} {
	route, err := NewPersonaAdminRoute(client)
	if err != nil {
		return nil
	}
	return personaAdminServeFactory{route: route}
}
