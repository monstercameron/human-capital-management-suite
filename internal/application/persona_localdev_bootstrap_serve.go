package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const LocalDevPersonaBootstrapPath = "/v1/local-dev/personas/bootstrap"

// LocalDevPersonaBootstrapBrowserPath shares the product's authenticated,
// path-scoped persona command allowance and workspace session cookie path.
const LocalDevPersonaBootstrapBrowserPath = workspace.PathPersonaAdminData + "/local-setup"

type LocalDevPersonaBootstrapReceipt struct {
	Draft               PersonaDraft `json:"draft"`
	ConversationID      string       `json:"conversation_id"`
	RoomPoliciesCreated int          `json:"room_policies_created"`
	ReviewerID          string       `json:"reviewer_id,omitempty"`
}

type LocalDevPersonaBootstrapper interface {
	Bootstrap(context.Context) (LocalDevPersonaBootstrapReceipt, error)
}

type LocalDevPersonaBootstrapBrowserOptions struct {
	PublicOrigin string
	BrowserLogin bool
}

type localDevPersonaBootstrapSurface struct {
	drafts   *LocalDevPolicyHelperDraftBootstrap
	chat     *chatstore.Store
	profile  string
	reviewer *LocalDevPolicyHelperReviewerBootstrap
}

// localDevBootstrap requires the actual deployment's immutable reference
// source. Missing registered model policy or measured suite metadata leaves
// this local operator surface unavailable.
func (w *personaServeWiring) localDevBootstrap(profile string, core dbport.Beginner, agentDB *agentstore.Store, references LocalDevPersonaStarterReferenceResolver, chatDB *chatstore.Store, reviewDB ...dbport.Beginner) (LocalDevPersonaBootstrapper, error) {
	if w == nil || profile != ServeProfileLocalDev || core == nil || agentDB == nil || references == nil || chatDB == nil || w.adminSkills == nil || w.adminGrantStore == nil || w.store == nil || w.roles == nil || w.now == nil {
		return nil, ErrLocalDevPersonaChatBootstrap
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	sources, err := NewDatabasePersonaAdminValidationSources(agentDB, agentDB, w.adminSkills, w.adminGrantStore, mapper)
	if err != nil {
		return nil, err
	}
	drafts := &PersonaAdminDraftService{Store: personaAdminDraftStoreAdapter{store: w.store}, Authorizer: personaAdminCreateRoleAuthorizer{roles: w.roles}, Profiles: TenantPersonaProfileBuilder{Source: sources.Profiles}, Clock: personaAdminDraftClock{now: w.now}}
	bootstrap := &LocalDevPolicyHelperDraftBootstrap{Core: core, TenantUUID: mapper, Directory: NewPersonaAudienceDirectoryDB(NewAgentDirectoryDB(core, mapper)), Manifests: agentDB, References: references, Skills: w.adminSkills, Drafts: drafts, Existing: localDevPolicyHelperVersions{store: w.store}}
	surface := &localDevPersonaBootstrapSurface{drafts: bootstrap, chat: chatDB, profile: profile}
	if len(reviewDB) > 0 && reviewDB[0] != nil {
		surface.reviewer = &LocalDevPolicyHelperReviewerBootstrap{ReviewDB: reviewDB[0], Roles: w.roles, Directory: bootstrap.Directory, TenantUUID: mapper, Authorizer: drafts.Authorizer, Now: w.now}
	}
	return surface, nil
}

func (s *localDevPersonaBootstrapSurface) Bootstrap(ctx context.Context) (LocalDevPersonaBootstrapReceipt, error) {
	if s == nil || s.drafts == nil || s.chat == nil || s.profile != ServeProfileLocalDev {
		return LocalDevPersonaBootstrapReceipt{}, ErrLocalDevPersonaChatBootstrap
	}
	draft, err := s.drafts.CreateDraft(ctx, s.profile)
	if err != nil {
		return LocalDevPersonaBootstrapReceipt{}, fmt.Errorf("create local policy helper draft: %w", err)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return LocalDevPersonaBootstrapReceipt{}, ErrLocalDevPersonaChatBootstrap
	}
	conversation := localDevPersonaDemoConversationID(principal.Tenant().String(), "general")
	n, err := ProvisionLocalDevPersonaChatPolicy(ctx, s.chat, s.profile, principal.Tenant().String(), conversation, "general")
	if err != nil {
		return LocalDevPersonaBootstrapReceipt{}, fmt.Errorf("provision local room policy: %w", err)
	}
	reviewer := ""
	if s.reviewer != nil {
		reviewer, err = s.reviewer.Provision(ctx, s.profile)
		if err != nil {
			return LocalDevPersonaBootstrapReceipt{}, fmt.Errorf("provision independent local reviewer: %w", err)
		}
	}
	return LocalDevPersonaBootstrapReceipt{Draft: draft, ConversationID: conversation, RoomPoliciesCreated: n, ReviewerID: reviewer}, nil
}

type localDevPolicyHelperVersions struct{ store *agentpersonastore.Store }

func (s localDevPolicyHelperVersions) ListVersions(ctx context.Context, tenant values.TenantId, id string) ([]agentpersonastore.PersonaVersion, error) {
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return scoped.ListVersions(ctx, id)
}

func (s localDevPolicyHelperVersions) GetVersion(ctx context.Context, tenant values.TenantId, id string, version int64) (agentpersonastore.PersonaVersion, error) {
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, err
	}
	return scoped.GetVersion(ctx, id, version)
}
func (s localDevPolicyHelperVersions) Lifecycle(ctx context.Context, tenant values.TenantId, id string, version int64) (agentpersonastore.LifecycleState, error) {
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return "", err
	}
	return scoped.Lifecycle(ctx, id, version)
}

// OverlayLocalDevPersonaBootstrap admits a real verified local administrator
// before any provisioning. The operator supplies no tenant, actor, pass flag,
// policy reference, or publication instruction in the request body.
func OverlayLocalDevPersonaBootstrap(next http.Handler, surface LocalDevPersonaBootstrapper, admission transport.Config, browser ...LocalDevPersonaBootstrapBrowserOptions) http.Handler {
	options := LocalDevPersonaBootstrapBrowserOptions{}
	if len(browser) > 0 {
		options = browser[0]
	}
	policy := edge.BrowserPolicyOptions{}
	if origin, err := url.Parse(options.PublicOrigin); err == nil && origin.Scheme != "" && origin.Host != "" {
		policy.AllowedOrigins = []string{options.PublicOrigin}
		policy.SecureCookies = origin.Scheme == "https"
	}
	mux := http.NewServeMux()
	handler := edge.BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if surface == nil {
			http.Error(w, "local persona bootstrap unavailable", http.StatusServiceUnavailable)
			return
		}
		metadata := workspace.AdmissionMetadata(r, options.BrowserLogin)
		if len(r.Header.Values("Authorization")) == 0 && len(metadata.Get(transport.AuthorizationMetadataKey)) > 0 {
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
		receipt, bootstrapErr := surface.Bootstrap(ctx)
		if bootstrapErr != nil {
			slog.WarnContext(ctx, "local persona bootstrap refused", "error", bootstrapErr)
			status := http.StatusConflict
			if errors.Is(bootstrapErr, ErrPersonaDraftDenied) || errors.Is(bootstrapErr, ErrLocalDevPersonaChatBootstrap) {
				status = http.StatusForbidden
			}
			http.Error(w, "local persona bootstrap refused", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(receipt)
	}), policy)
	mux.Handle(LocalDevPersonaBootstrapPath, handler)
	mux.Handle(LocalDevPersonaBootstrapBrowserPath, handler)
	mux.Handle("/", next)
	return mux
}
