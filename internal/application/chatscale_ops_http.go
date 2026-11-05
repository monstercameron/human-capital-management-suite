package application

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatscaleOpsPath is the operations view's chat scale figure: table-growth
// and slow-query figures for the chat tables with the CHATSCALE-001 budget
// thresholds (CHATSCALE-003 GREEN). It sits beside /admin/ops/store-health.
const ChatscaleOpsPath = "/admin/ops/chatscale"

// ChatscaleOpsPort is the chat store's operations figure. *chatstore.Store
// implements it; a deployment without a composed chat store leaves the route
// to answer unavailable.
type ChatscaleOpsPort interface {
	ChatscaleOpsView(context.Context) (chatstore.ChatscaleOpsView, error)
}

// OverlayChatscaleOps mounts the chat scale figure with the same admission
// as the neighbouring Chat surfaces, plus the operator role the other
// /admin/ops routes require. A nil port leaves the path mounted and
// answering unavailable.
func OverlayChatscaleOps(next http.Handler, port ChatscaleOpsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatscaleOpsPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatscaleOpsError(w, "unauthenticated", denied.HTTPStatus())
			return
		}
		principal, ok := trust.FromContext(ctx)
		if !ok || principal == nil {
			chatscaleOpsError(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		if err := adminpolicy.RequireOperator(principal); err != nil {
			chatscaleOpsError(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			chatscaleOpsError(w, "method_not_allowed", http.StatusMethodNotAllowed)
			return
		}
		if port == nil {
			chatscaleOpsError(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		view, err := port.ChatscaleOpsView(r.Context())
		if err != nil {
			chatscaleOpsError(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(view)
	})
}

func chatscaleOpsError(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}
