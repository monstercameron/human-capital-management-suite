package health

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// SnapshotProbe is the live health operation needed by the publishing seam.
type SnapshotProbe interface {
	Run(context.Context, uuid.UUID) Snapshot
}

// TenantResolver must return a tenant only after authenticating and
// authorizing the request for that tenant. Health output is tenant scoped.
type TenantResolver interface {
	ResolveHealthTenant(*http.Request) (uuid.UUID, bool)
}

// EndpointHandler publishes the typed store-health snapshot as JSON. The
// resolver is the security boundary and must be backed by the admin/ops
// authorization context; absent or invalid tenant identity fails closed.
func EndpointHandler(probe SnapshotProbe, resolver TenantResolver) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/ops/store-health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if probe == nil || resolver == nil {
			http.Error(w, "health unavailable", http.StatusServiceUnavailable)
			return
		}
		tenant, ok := resolver.ResolveHealthTenant(r)
		if !ok || tenant == uuid.Nil {
			http.Error(w, "tenant authorization required", http.StatusForbidden)
			return
		}
		snapshot := probe.Run(r.Context(), tenant)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if snapshot.State == StateUnknown {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	return mux
}
