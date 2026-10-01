package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

const (
	AgentPortableCatalogPath = "/api/agents/portable/catalog"
	AgentPortableExportPath  = "/api/agents/portable/export"
	AgentPortableImportPath  = "/api/agents/portable/import"
	AgentPortableDraftPath   = "/api/agents/portable/draft"
)

// OverlayAgentPortableHTTP mounts authenticated JSON routes on an existing
// served edge. Authentication is performed by the same transport admission
// configuration used by the rest of the HTTP surface.
func OverlayAgentPortableHTTP(next http.Handler, service *AgentPortableService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
			return
		}
		if service == nil {
			http.Error(w, "portable service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == AgentPortableCatalogPath {
			var request struct{}
			if decodePortableRequest(w, r, &request, 64<<10) != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			catalog, err := service.Catalog(ctx)
			if err != nil {
				http.Error(w, "portable catalog unavailable", portableHTTPErrorStatus(err))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(catalog)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == AgentPortableExportPath {
			var req AgentPortableExportRequest
			if err := decodePortableRequest(w, r, &req, 64<<10); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			data, err := service.Export(ctx, req)
			if err != nil {
				http.Error(w, "portable export unavailable", portableHTTPErrorStatus(err))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == AgentPortableImportPath {
			var request AgentPortableImportRequest
			if err := decodePortableRequest(w, r, &request, 2<<20); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			draft, err := service.ImportMapped(ctx, request)
			if err != nil {
				http.Error(w, "portable import unavailable", portableHTTPErrorStatus(err))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(draft)
			return
		}
		if r.URL.Path == AgentPortableDraftPath {
			var request struct {
				DefinitionID string `json:"definition_id"`
			}
			if decodePortableRequest(w, r, &request, 64<<10) != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			draft, err := service.ReadDraft(ctx, request.DefinitionID)
			if err != nil {
				http.Error(w, "portable draft unavailable", portableHTTPErrorStatus(err))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(draft)
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle(AgentPortableExportPath, endpoint)
	mux.Handle(AgentPortableCatalogPath, endpoint)
	mux.Handle(AgentPortableImportPath, endpoint)
	mux.Handle(AgentPortableDraftPath, endpoint)
	mux.Handle("/", next)
	return mux
}

func portableHTTPErrorStatus(err error) int {
	switch {
	case errors.Is(err, ErrAgentPortableDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrAgentPortableInvalid), errors.Is(err, agentportable.ErrInvalidDefinition), errors.Is(err, agentportable.ErrMappingRequired):
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}

func decodePortableRequest(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
