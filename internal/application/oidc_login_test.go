package application

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidcsession"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func validOIDCConfig() ServeConfig {
	return ServeConfig{
		Tenant: "tenant-a", Workspace: true, Profile: ServeProfileLocalDev,
		FederationIssuers: "tenant-a=https://idp.example/tenant-a",
		OIDCIssuerURL:     "https://idp.example/tenant-a", OIDCClientID: "client-a",
		OIDCAuthorizationEndpoint: "http://localhost:8081/authorize", OIDCTokenEndpoint: "http://localhost:8081/token",
		OIDCRedirectURI: "http://localhost:8080/workspace/login/oidc/callback", OIDCSessionSigningKey: strings.Repeat("k", 32),
	}
}

func TestTodo_REV_033_01_ConfigValidation(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		if err := (ServeConfig{}).validateOIDCLogin(); err != nil {
			t.Fatalf("validateOIDCLogin with no OIDC config: %v", err)
		}
	})
	t.Run("configured flow is accepted", func(t *testing.T) {
		if err := validOIDCConfig().validateOIDCLogin(); err != nil {
			t.Fatalf("valid OIDC configuration rejected: %v", err)
		}
	})
	t.Run("partial config fails closed", func(t *testing.T) {
		cfg := validOIDCConfig()
		cfg.OIDCSessionSigningKey = ""
		if err := cfg.validateOIDCLogin(); err == nil {
			t.Fatal("partial OIDC configuration was accepted")
		}
	})
	t.Run("issuer must be tenant allow-listed", func(t *testing.T) {
		cfg := validOIDCConfig()
		cfg.OIDCIssuerURL = "https://attacker.example/"
		if err := cfg.validateOIDCLogin(); err == nil {
			t.Fatal("unlisted issuer was accepted")
		}
	})
	t.Run("production rejects loopback http", func(t *testing.T) {
		cfg := validOIDCConfig()
		cfg.Profile = "production"
		if err := cfg.validateOIDCLogin(); err == nil {
			t.Fatal("production accepted loopback HTTP endpoints")
		}
	})
	t.Run("signing key has minimum entropy length", func(t *testing.T) {
		cfg := validOIDCConfig()
		cfg.OIDCSessionSigningKey = strings.Repeat("x", 31)
		if err := cfg.validateOIDCLogin(); err == nil {
			t.Fatal("short signing key was accepted")
		}
	})
}

func TestTodo_REV_033_01_WorkspaceOIDCOptionsAreAllOrNone(t *testing.T) {
	flow := &oidc.Flow{}
	sessions := &oidcsession.Authority{}
	cfg := validOIDCConfig()
	for _, tc := range []struct {
		name     string
		flow     *oidc.Flow
		sessions *oidcsession.Authority
	}{
		{name: "disabled"},
		{name: "missing session authority", flow: flow},
		{name: "missing flow", sessions: sessions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := composeWorkspaceOIDCLogin(cfg, tc.flow, tc.sessions)
			if got.flow != nil || got.tenant != "" || got.issuerURL != "" || got.redirectURI != "" || got.sessionIssuer != nil {
				t.Fatalf("incomplete login options were retained: %+v", got)
			}
		})
	}

	got := composeWorkspaceOIDCLogin(cfg, flow, sessions)
	if got.flow != flow || got.tenant != values.TenantId(cfg.Tenant) || got.issuerURL != cfg.OIDCIssuerURL || got.redirectURI != cfg.OIDCRedirectURI || got.sessionIssuer == nil {
		t.Fatalf("complete OIDC login options = %+v", got)
	}
}

func TestTodo_REV_033_01_LocalDevCellConfigComposesWorkspaceWithoutOIDC(t *testing.T) {
	args := []string{"-profile=local-dev"}
	parsed, err := bootstrap.ParseConfig(args, func(string) (string, bool) { return "", false }, ServeConfigFieldsForArgs(args))
	if err != nil {
		t.Fatalf("parse local-dev serve defaults: %v", err)
	}
	cfg, err := ServeConfigFromValues(parsed)
	if err != nil {
		t.Fatalf("load local-dev serve defaults: %v", err)
	}
	if cfg.Tenant != LocalDevTenant || !cfg.Workspace || !cfg.DevBrowserLogin {
		t.Fatalf("local-dev workspace defaults = tenant:%q workspace:%t dev-login:%t", cfg.Tenant, cfg.Workspace, cfg.DevBrowserLogin)
	}
	workspaceEnabled := cfg.Workspace
	cellConfig := app.CellConfig{
		Store: &stubStore{}, Verifier: stubVerifier{}, Audience: cfg.Audience,
		Workspace: &workspaceEnabled, DevBrowserLogin: cfg.DevBrowserLogin,
	}
	applyWorkspaceOIDCLogin(&cellConfig, cfg, nil, nil)
	if cellConfig.OIDCSessionIssuer != nil {
		t.Fatalf("disabled OIDC session issuer is a non-nil interface: %#v", cellConfig.OIDCSessionIssuer)
	}
	cell, err := app.NewCell(cellConfig)
	if err != nil {
		t.Fatalf("compose local-dev application cell: %v", err)
	}
	h, err := transportcell.NewEdgeHandler(cell)
	if err != nil {
		t.Fatalf("compose local-dev workspace through transport cell: %v", err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("GET", workspace.PathLogin, nil))
	if response.Code != 200 {
		t.Fatalf("dev login page status = %d, want 200", response.Code)
	}
}

type oidcFallbackVerifier struct{}

func (oidcFallbackVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, trust.ErrInvalidCredential
}

func TestTodo_REV_033_01_ServedOIDCComposition(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "https://idp.example/tenant-a"
	file := struct {
		Issuers []any `json:"issuers"`
	}{Issuers: []any{map[string]any{
		"tenant": "tenant-a", "issuer": issuer,
		"keys": []any{map[string]string{"kid": "key-a", "alg": "RS256", "public_key": base64.StdEncoding.EncodeToString(der)}},
	}}}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "federation-keys.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := validOIDCConfig()
	cfg.Profile = ServeProfileStandard
	cfg.Issuer = "hcmnext-test"
	cfg.Audience = "workspace"
	cfg.FederationKeysFile = path
	cfg.OIDCAuthorizationEndpoint = "https://idp.example/authorize"
	cfg.OIDCTokenEndpoint = "https://idp.example/token"
	cfg.OIDCRedirectURI = "https://app.example/workspace/login/oidc/callback"
	flow, sessions, err := composeOIDCWorkspaceLogin(context.Background(), &pgxadapter.Pool{}, cfg, oidcFallbackVerifier{}, func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatalf("compose configured OIDC login: %v", err)
	}
	if flow == nil || sessions == nil {
		t.Fatalf("served OIDC composition returned flow=%v session authority=%v", flow, sessions)
	}
}
