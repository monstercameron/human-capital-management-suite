package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

const AgentVersionRolloutPath = "/api/agents/rollouts"

// OverlayAgentVersionRolloutHTTP admits identity through the existing edge
// verifier before calling the application command port.
func OverlayAgentVersionRolloutHTTP(next http.Handler, service *AgentVersionRolloutService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AgentVersionRolloutPath {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		var request AgentVersionRolloutCommand
		if decoder.Decode(&request) != nil {
			http.Error(w, "invalid rollout request", http.StatusBadRequest)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, "invalid rollout request", http.StatusBadRequest)
			return
		}
		result, err := service.Execute(ctx, request)
		if err != nil {
			status := http.StatusServiceUnavailable
			switch {
			case errors.Is(err, ErrAgentVersionRolloutDenied), errors.Is(err, ErrPersonaAdminCommandUnavailable), errors.Is(err, ErrPersonaAdminLifecycleUnavailable):
				status = http.StatusForbidden
			case errors.Is(err, agentrollout.ErrInvalid), errors.Is(err, agentpersonastore.ErrInvalid):
				status = http.StatusBadRequest
			case errors.Is(err, agentrollout.ErrPreviewStale), errors.Is(err, agentrollout.ErrReviewRequired), errors.Is(err, agentrollout.ErrApprovalRequired), errors.Is(err, agentpersonastore.ErrConflict):
				status = http.StatusConflict
			case errors.Is(err, agentpersonastore.ErrNotFound):
				status = http.StatusNotFound
			}
			http.Error(w, "rollout request refused", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}
