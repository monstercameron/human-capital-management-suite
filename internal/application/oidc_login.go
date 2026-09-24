package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	oidcpg "github.com/monstercameron/human-capital-management-suite/internal/authn/oidc/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidcsession"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	appstore "github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
	sessionpg "github.com/monstercameron/human-capital-management-suite/internal/trust/session/pgstore"
)

func composeOIDCWorkspaceLogin(ctx context.Context, pool *pgxadapter.Pool, cfg ServeConfig, base trust.Verifier, now func() time.Time) (*oidc.Flow, *oidcsession.Authority, error) {
	if strings.TrimSpace(cfg.OIDCIssuerURL) == "" {
		return nil, nil, nil
	}
	if pool == nil {
		return nil, nil, fmt.Errorf("OIDC workspace login requires a database pool")
	}
	if base == nil {
		return nil, nil, fmt.Errorf("OIDC workspace login requires the configured federation verifier")
	}
	pairs, err := parseFederationIssuers(cfg.FederationIssuers)
	if err != nil {
		return nil, nil, err
	}
	tenant := values.TenantId(cfg.Tenant)
	pinned, err := loadFederationKeysFile(cfg.FederationKeysFile)
	if err != nil {
		return nil, nil, err
	}
	entry, ok := pinned[cfg.OIDCIssuerURL]
	if !ok {
		return nil, nil, fmt.Errorf("OIDC issuer %q has no pinned signing keys", cfg.OIDCIssuerURL)
	}
	allowed := false
	for _, issuer := range pairs[tenant] {
		if issuer == cfg.OIDCIssuerURL {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, nil, fmt.Errorf("OIDC issuer %q is not allow-listed for tenant %q", cfg.OIDCIssuerURL, tenant)
	}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	registry := issuerregistry.NewMemoryStore()
	// OIDC Core's ID-token audience is the OAuth client_id. The browser
	// access credential minted after the callback continues to use cfg.Audience.
	issuer, err := federationFileIssuerRecord(entry, cfg.OIDCClientID, clock().UTC())
	if err != nil {
		return nil, nil, err
	}
	if _, err := issuerregistry.Publish(registry, issuer); err != nil {
		return nil, nil, fmt.Errorf("publish OIDC issuer: %w", err)
	}
	if _, err := issuerregistry.Activate(registry, issuer.Ref(), issuerregistry.Evidence{ActedBy: federationActivator, Authority: federationAuthority, Reason: "pinned IdP configuration applied at serve composition", At: clock().UTC()}); err != nil {
		return nil, nil, fmt.Errorf("activate OIDC issuer: %w", err)
	}
	resolver, err := issuerregistry.NewResolver(registry, nil)
	if err != nil {
		return nil, nil, err
	}
	clients := oidc.NewStaticClientSource().WithClient(oidc.ClientRegistration{
		Tenant: tenant, IssuerURL: cfg.OIDCIssuerURL, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
		RedirectURI: cfg.OIDCRedirectURI, AuthorizationEndpoint: cfg.OIDCAuthorizationEndpoint,
		TokenEndpoint: cfg.OIDCTokenEndpoint, Scopes: []string{"openid", "profile", "email"},
	})
	flowKey := sha256.Sum256(append([]byte("hcmnext-oidc-flow-v1\x00"), []byte(cfg.OIDCSessionSigningKey)...))
	flow, err := oidc.NewFlow(oidc.FlowConfig{
		Registry: registry, Keys: resolver, Clients: clients,
		States: oidcpg.NewWithTenantMapper(pool, func(tenant values.TenantId) values.TenantId {
			return values.TenantId(appstore.TenantID(tenant.String()).String())
		}),
		Exchanger: oidc.HTTPTokenExchanger{}, Secret: flowKey[:],
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compose OIDC authorization flow: %w", err)
	}
	sessions, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: now, Store: sessionpg.New(pool)})
	if err != nil {
		return nil, nil, fmt.Errorf("compose OIDC session manager: %w", err)
	}
	authority, err := oidcsession.New(oidcsession.Config{
		Sessions: sessions, Fallback: base, Key: []byte(cfg.OIDCSessionSigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience,
		Now: now, Lifetime: 10 * time.Minute,
		SessionTenant: func(tenant values.TenantId) values.TenantId {
			return values.TenantId(appstore.TenantID(tenant.String()).String())
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compose OIDC session authority: %w", err)
	}
	return flow, authority, nil
}

type workspaceOIDCLogin struct {
	flow          *oidc.Flow
	tenant        values.TenantId
	issuerURL     string
	redirectURI   string
	sessionIssuer workspace.OIDCSessionIssuer
}

// composeWorkspaceOIDCLogin keeps the workspace's optional OIDC fields
// all-or-none. In particular, a nil concrete session authority must not be
// assigned to the interface field: that would produce a non-nil typed-nil
// interface and make the workspace reject the disabled login as partial.
func composeWorkspaceOIDCLogin(cfg ServeConfig, flow *oidc.Flow, sessions *oidcsession.Authority) workspaceOIDCLogin {
	if flow == nil || sessions == nil {
		return workspaceOIDCLogin{}
	}
	return workspaceOIDCLogin{
		flow: flow, tenant: values.TenantId(cfg.Tenant), issuerURL: cfg.OIDCIssuerURL,
		redirectURI: cfg.OIDCRedirectURI, sessionIssuer: sessions,
	}
}

func applyWorkspaceOIDCLogin(config *app.CellConfig, cfg ServeConfig, flow *oidc.Flow, sessions *oidcsession.Authority) {
	login := composeWorkspaceOIDCLogin(cfg, flow, sessions)
	config.OIDCFlow = login.flow
	config.OIDCTenant = login.tenant
	config.OIDCIssuerURL = login.issuerURL
	config.OIDCRedirectURI = login.redirectURI
	config.OIDCSessionIssuer = login.sessionIssuer
}
