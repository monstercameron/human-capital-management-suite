// Core document ID routing (HUB-002) at the application boundary. The
// route directory itself is documenthubstore's pure, in-memory reference
// implementation (or a durable equivalent a composition root supplies);
// this file only exposes it to callers who create documents and resolve
// their content, so a stale migrated copy or a foreign tenant's route is
// refused before any document read reaches the store.
package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// ErrNoRouteDirectory is returned when a caller asks for route registration
// or enforcement without a configured directory.
var ErrNoRouteDirectory = errors.New("document routes: no route directory configured")

// RegisterDocumentRoute registers, or idempotently confirms, the core route
// for one document under the given idempotency key.
func (s documentService) RegisterDocumentRoute(ctx context.Context, dir documenthubstore.DocumentRouteDirectory, documentID, tenantID, shardID, idempotencyKey string) (documenthubstore.DocumentRoute, error) {
	if dir == nil {
		return documenthubstore.DocumentRoute{}, ErrNoRouteDirectory
	}
	return dir.Register(ctx, documentID, tenantID, shardID, idempotencyKey)
}

// RequireDocumentRoute refuses a document read whose caller resolved a
// route earlier than the directory's current epoch, or under a tenant that
// does not own the document.
func (s documentService) RequireDocumentRoute(ctx context.Context, dir documenthubstore.DocumentRouteDirectory, route documenthubstore.DocumentRoute, tenantID string) error {
	return documenthubstore.RequireCurrentRoute(ctx, dir, route, tenantID)
}
