package cell

import (
	"net/http"

	"connectrpc.com/connect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// DocumentHTTPOverlay mounts the document Connect projection (HUB-041) ahead
// of next, under the same pre-admission and decoded-message admission the chat
// overlay uses, so an HTTP caller is authenticated, screened and validated
// exactly as a native RPC caller is. Every other route goes to next, and a nil
// service leaves next untouched.
func DocumentHTTPOverlay(next http.Handler, cfg transport.Config, service transportdocument.Service, cursorKey []byte) http.Handler {
	if service == nil {
		return next
	}
	handler := transportdocument.NewHandler(
		transportdocument.Dependencies{Service: service, CursorKey: cursorKey},
		connect.WithInterceptors(chatAdmission{cfg: cfg}),
	)
	mux := http.NewServeMux()
	mux.Handle(transportdocument.ProcedurePrefix, chatPreAdmission(cfg, handler))
	mux.Handle("/", next)
	return mux
}
