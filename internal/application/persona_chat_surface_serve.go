package application

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

func (w *personaServeWiring) chatSurface(service chat.ConversationService, db *agentstore.Store) (*PersonaChatSurface, error) {
	if w == nil || service == nil || db == nil || w.refs == nil || w.avail == nil || w.skill == nil {
		return nil, personachat.ErrUnavailable
	}
	invocations, err := agentinvocationstore.NewWithTenantUUID(db, pgstore.TenantID)
	if err != nil {
		return nil, err
	}
	executions, err := agentrunstate.New(db, func(tenant string) uuid.UUID { return pgstore.TenantID(tenant) })
	if err != nil {
		return nil, err
	}
	return &PersonaChatSurface{Chat: service, References: w.refs, Personas: w.avail, Skills: w.skill, Invocations: invocations, Failures: invocations, Receipts: invocations, Executions: func(_ context.Context, tenant string) (runstate.Store, error) { return executions.ForTenant(tenant) },
		ChannelAlwaysPrivate: func(ctx context.Context, tenant string, facts personaReferenceFacts) (bool, error) {
			if w.store == nil {
				return false, personachat.ErrUnavailable
			}
			store, err := w.store.Scoped(values.TenantId(tenant))
			if err != nil {
				return false, err
			}
			version, placement, err := store.ReadCurrentPersonaAuthority(ctx, facts.ConversationID, facts.PersonaID)
			if err != nil || placement.InstallationID != facts.InstallationID || uint64(version.Version) != facts.PersonaVersion {
				return false, personachat.ErrUnavailable
			}
			return placement.ChannelPolicy.AlwaysPrivate, nil
		}, Now: w.now}, nil
}

// PersonaChatBrowserOptions comes from the serving cell's origin and login policy.
type PersonaChatBrowserOptions struct {
	PublicOrigin string
	BrowserLogin bool
}

// OverlayPersonaChatSurface puts the persona HTTP routes through the same
// authenticated admission boundary as the remaining serving edge.
func OverlayPersonaChatSurface(next http.Handler, surface personachat.Surface, admission transport.Config, browser ...PersonaChatBrowserOptions) http.Handler {
	options := PersonaChatBrowserOptions{}
	if len(browser) > 0 {
		options = browser[0]
	}
	policy := edge.BrowserPolicyOptions{}
	if origin, err := url.Parse(options.PublicOrigin); err == nil && origin.Scheme != "" && origin.Host != "" {
		policy.AllowedOrigins = []string{options.PublicOrigin}
		policy.SecureCookies = origin.Scheme == "https"
	}
	handler := personachat.Handler{Surface: surface}
	endpoint := edge.BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata := workspace.AdmissionMetadata(r, options.BrowserLogin)
		if len(r.Header.Values("Authorization")) == 0 && len(metadata.Get(transport.AuthorizationMetadataKey)) > 0 && agentServedMutation(r.Method) {
			cookie, err := r.Cookie(edge.BrowserCSRFCookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				http.Error(w, "browser proof required", http.StatusForbidden)
				return
			}
		}
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: metadata, Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			http.Error(w, "request denied", err.HTTPStatus())
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	}), policy)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == personachat.Path || strings.HasPrefix(r.URL.Path, personachat.Path+"/") {
			endpoint.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
