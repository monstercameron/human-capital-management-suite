package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// VoiceBarredSource says where the person's text may not go to an outside
// service; Listen is absent there.
type VoiceBarredSource interface {
	VoiceListenBarred(ctx context.Context, tenant, home, person string) (workspace bool, conversations []string, err error)
}

// chatVoiceFeatureWriter holds the features response back so that the voice
// overlay can add its own fields before it is sent.
type chatVoiceFeatureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *chatVoiceFeatureWriter) Header() http.Header         { return w.header }
func (w *chatVoiceFeatureWriter) WriteHeader(status int)      { w.status = status }
func (w *chatVoiceFeatureWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

// OverlayChatVoiceFeatures adds "listen" and "listen_barred" to the Chat
// features answer (CHATVOICE-006): listen is true only when text to speech is
// composed and the workspace does not bar outside services, and listen_barred
// names the caller's conversations whose channel bars them. The page draws
// Listen only where it can work; there is no disabled Listen. Every other field
// is passed through as the features endpoint wrote it; a response that is not a
// JSON object is untouched.
func OverlayChatVoiceFeatures(next http.Handler, service VoiceService, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != integrate2FeaturesPath || r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		held := &chatVoiceFeatureWriter{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(held, r)
		for key, values := range held.header {
			w.Header()[key] = values
		}
		body := held.body.Bytes()
		var features map[string]json.RawMessage
		if held.status == http.StatusOK && json.Unmarshal(body, &features) == nil && features != nil {
			listen, barred := false, []string{}
			if service.Speaker != nil {
				listen = true
				if ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge}); denied != nil {
					listen = false
				} else if p, ok := trust.FromContext(ctx); !ok || p == nil || service.Barred == nil {
					listen = false
				} else if workspace, conversations, err := service.Barred.VoiceListenBarred(ctx, p.Tenant().String(), p.Tenant().String(), p.Subject()); err != nil || workspace {
					// A setting that cannot be read bars Listen, as it bars the call.
					listen = false
				} else {
					barred = conversations
				}
			}
			// Voice messages are composed when the switches can be read and kept; the
			// personal and channel switches are offered only then.
			features["voice"], _ = json.Marshal(service.Access != nil && service.SwitchValues != nil && service.Switches != nil)
			features["listen"], _ = json.Marshal(listen)
			features["listen_barred"], _ = json.Marshal(strings.Join(barred, ","))
			if merged, err := json.Marshal(features); err == nil {
				body = merged
				w.Header().Del("Content-Length")
			}
		}
		w.WriteHeader(held.status)
		_, _ = w.Write(body)
	})
}
