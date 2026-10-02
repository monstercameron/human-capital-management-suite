package application

import (
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

const integrate2FeaturesPath = "/api/chat/features/v1"

func (s *agentServedAssembly) overlayChatFeatures(next http.Handler, admission transport.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != integrate2FeaturesPath {
			next.ServeHTTP(w, r)
			return
		}
		admitted, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if denied != nil {
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "denied"})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Filter management is mounted inside the conversation details panel.
		_ = json.NewEncoder(w).Encode(chatui.ChatFeatures{Gates: !isNilPersonaOutputPort(s.Gates), Renderings: !isNilPersonaOutputPort(s.Renderings), Status: !isNilPersonaOutputPort(s.ChannelStatus), Search: s.ChatSearch.Port != nil, Filters: s.Filters != nil, Locations: !isNilPersonaOutputPort(s.Locations), WritingStyles: chattoneFeatureOn(admitted, s.WritingStyles), Translating: chatlangTranslationFeature(admitted, s.Renderings), Translation: s.translationReady()})
	})
}
