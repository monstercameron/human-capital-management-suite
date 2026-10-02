package workspace

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATBUG-014: every workspace document read the viewer's agent snapshot (the
// agents they may ask and all of their own tasks), which took 0.4 to 0.7 s on
// the review machine and is drawn by one page: Agents. Every other page needs
// only the tenant setting and whether the viewer administers it, which decide
// the navigation entry.
//
// A document for another page now reads only those two and marks the snapshot
// as deferred. The client is a single-page application, so somebody who opens
// Chat and then goes to Agents never asks for the Agents document; the client
// reads the snapshot from PathAgentSnapshot when that page opens instead.

// PathAgentSnapshot serves the signed-in viewer's own agent snapshot, in the
// form the document's configuration carries it. It sits under the persona
// administration prefix because the page policy already admits requests there.
const PathAgentSnapshot = PathPersonaAdminData + "/viewer-agents"

// resolveAgentsSetting is the part of the agents projection every page needs:
// whether the viewer administers the setting and whether the tenant has agents
// on. readable reports that the agent service can be asked for a snapshot.
func (h *Handler) resolveAgentsSetting(ctx context.Context, principal *trust.Principal, access productAccess) (config *AgentsConfig, readable bool) {
	if principal == nil {
		return nil, false
	}
	config = &AgentsConfig{ViewerIsAdmin: agentsAdmin(access)}
	if h.agentSettings != nil {
		enabled, err := h.agentSettings.AgentsEnabled(ctx, principal.Tenant())
		config.Enabled = err == nil && enabled
	}
	if !config.Enabled {
		if config.ViewerIsAdmin {
			config.Reason = productui.AgentsReasonTenantDisabled
			config.SettingsHref = productui.AgentsSettingsHref()
		}
		return config, false
	}
	config.Service = agentServiceUnavailable
	return config, h.agents != nil
}

// agentSnapshotPage reports whether page draws the agent snapshot.
func agentSnapshotPage(page productui.PageID) bool {
	profile, _, ok := productui.PageProfiles(page)
	return ok && profile == productui.RouteProfileAgents
}

// resolveAgentsForPage is resolveAgents for a page that draws the snapshot and
// the setting alone for every other page, which leaves the snapshot to the
// client.
func (h *Handler) resolveAgentsForPage(ctx context.Context, principal *trust.Principal, access productAccess, page productui.PageID) *AgentsConfig {
	if agentSnapshotPage(page) {
		return h.resolveAgents(ctx, principal, access)
	}
	config, readable := h.resolveAgentsSetting(ctx, principal, access)
	if readable {
		// The service was not asked, so the document does not say whether
		// it answers.
		config.Service, config.Deferred = "", true
	}
	return config
}

// serveAgentSnapshot answers the client's read of a deferred snapshot. The
// viewer and their authority come from the admitted credential and the stored
// role policy, exactly as they do for the document.
func (h *Handler) serveAgentSnapshot(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil || principal == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{}`))
		return
	}
	_ = json.NewEncoder(w).Encode(h.resolveAgents(admitted.Context(), principal, access))
}
