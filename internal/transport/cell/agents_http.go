package cell

import (
	"net/http"

	"connectrpc.com/connect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportagents "github.com/monstercameron/human-capital-management-suite/internal/transport/agents"
)

// AgentHTTPOverlay mounts the agent Connect projection (AGENT-042) ahead of
// next, under the same pre-admission and decoded-message admission the chat
// and document overlays use, so an HTTP caller is authenticated, screened,
// quota-checked and validated exactly as a native RPC caller is. Every other
// route goes to next, and a nil setting leaves next untouched, matching
// [transportagents.Register].
func AgentHTTPOverlay(next http.Handler, cfg transport.Config, deps transportagents.Dependencies) http.Handler {
	if deps.Settings == nil {
		return next
	}
	handler := transportagents.NewHandler(
		deps,
		connect.WithInterceptors(chatAdmission{cfg: cfg}),
	)
	mux := http.NewServeMux()
	mux.Handle(transportagents.ProcedurePrefix, chatPreAdmission(cfg, handler))
	mux.Handle("/", next)
	return mux
}
