package application

import (
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

func OverlayAgentAnnouncements(next http.Handler, surface agentcontrols.AnnouncementSurface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := agentcontrols.AnnouncementHandler{Surface: surface}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentcontrols.AnnouncementPath && !strings.HasPrefix(r.URL.Path, agentcontrols.AnnouncementPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
