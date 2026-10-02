package application

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// OverlayAgentUXAmbient mounts the ambient agents' read on the served assembly.
// Chat asks it once for every conversation it opens; where the feature is not
// turned on (no surface), the read still answers 200 with nothing in it, so a
// workspace without those agents is not told "not found" on every load. A
// command sent to a surface that is not there is answered as unavailable.
func OverlayAgentUXAmbient(next http.Handler, surface ambientagents.Surface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ambientagents.Handler{Surface: surface}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ambientagents.Path && !strings.HasPrefix(r.URL.Path, ambientagents.Path+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		r = r.WithContext(ctx)
		if surface == nil && r.Method == http.MethodGet && r.URL.Path == ambientagents.Path {
			writeAgentUXAmbientEmpty(w)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// writeAgentUXAmbientEmpty answers the read of a workspace without ambient agents:
// no cards, no agents reading, nothing to opt out of.
func writeAgentUXAmbientEmpty(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{}, Grants: []ambientagents.Grant{}, Tasks: []ambientagents.Task{}})
}
