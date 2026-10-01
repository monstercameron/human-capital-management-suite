package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/machineauth"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// servedMachineAuth owns the one machine credential composition shared by the
// request verifier and the token endpoint. The registry is durable; signing
// keys are process-owned and rotatable through machine.Issuer.Rotate.
type servedMachineAuth struct {
	verifier *machine.Verifier
	issuer   *machine.Issuer
	registry *truststore.MachineRegistry
	now      func() time.Time
}

// composeServedVerifier builds the production credential boundary. Explicit
// verifier overrides remain a test/composition seam; the deployed path uses
// machine JWTs and only keeps the HMAC verifier in local development.
func composeServedVerifier(cfg ServeConfig, options Options, pool *pgxadapter.Pool) (trust.Verifier, *servedMachineAuth, error) {
	if options.NewVerifier != nil {
		verifier, err := composeVerifier(cfg, options)
		return verifier, nil, err
	}
	if pool == nil {
		return nil, nil, fmt.Errorf("application: the served machine-client registry requires the database pool")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	at := now().UTC()
	keyID, err := machineKeyID()
	if err != nil {
		return nil, nil, err
	}
	key, err := machine.GenerateServerKey(keyID, at.Add(24*time.Hour), at.Add(24*time.Hour+machine.MaxLifetime))
	if err != nil {
		return nil, nil, fmt.Errorf("application: generate machine signing key: %w", err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{key}, now)
	if err != nil {
		return nil, nil, fmt.Errorf("application: compose machine token issuer: %w", err)
	}
	verifier, err := machine.NewVerifier([]machine.ServerKey{key}, now)
	if err != nil {
		return nil, nil, fmt.Errorf("application: compose machine token verifier: %w", err)
	}
	registry := truststore.New(pool).Registry(func(tenant string) uuid.UUID { return pgstore.TenantID(tenant) })
	source := &servedMachineAuth{verifier: verifier, issuer: issuer, registry: registry, now: now}
	served := &trust.ServedVerifier{
		Machine:     verifier,
		Request:     machine.VerifyRequest{Issuer: cfg.Issuer, Audience: []string{cfg.Audience}},
		Revocations: registry,
		Now:         now,
	}

	if federationConfigured(cfg) {
		fallback, err := composeVerifier(cfg, options)
		if err != nil {
			return nil, nil, err
		}
		return &machineFallbackVerifier{machine: served, fallback: fallback}, source, nil
	}
	if cfg.Profile == ServeProfileLocalDev {
		fallback, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
			Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: now,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("application: build the local development HMAC verifier: %w", err)
		}
		// Local-dev persona and bearer tokens still carry their role bundle
		// (the workspace has not migrated to RBAC-RT-010 identity-only
		// tokens with server-resolved roles), so they are admitted through
		// the legacy HMAC Verify rather than ServedVerifier.Dev, whose
		// identity-only semantics would strip every persona's access.
		return &machineFallbackVerifier{machine: served, fallback: fallback}, source, nil
	}
	return served, source, nil
}

// machineFallbackVerifier admits either a machine token or the configured
// human federation assertion. It has no HMAC fallback outside local-dev.
type machineFallbackVerifier struct {
	machine  trust.Verifier
	fallback trust.Verifier
}

func (v *machineFallbackVerifier) Verify(ctx context.Context, credential trust.Credential) (*trust.Principal, error) {
	principal, err := v.machine.Verify(ctx, credential)
	if err == nil || v.fallback == nil {
		return principal, err
	}
	return v.fallback.Verify(ctx, credential)
}

func machineKeyID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("application: generate machine key id: %w", err)
	}
	return "serve-" + hex.EncodeToString(raw[:]), nil
}

func (s *servedMachineAuth) routes(cfg ServeConfig, address string) (http.Handler, error) {
	if s == nil || s.registry == nil || s.issuer == nil || s.verifier == nil {
		return nil, nil
	}
	origin := strings.TrimRight(cfg.PublicOrigin, "/")
	if origin == "" {
		origin = "http://" + address
	}
	h, err := machineauth.NewHandler(machineauth.Dependencies{
		Registry: s.registry, Issuer: s.issuer, Verifier: s.verifier,
		Audiences: []string{cfg.Audience}, TokenIssuer: cfg.Issuer,
		TokenURL: origin + "/oauth2/token", Now: s.now,
	})
	if err != nil {
		return nil, fmt.Errorf("application: compose machine-auth routes: %w", err)
	}
	return h.Routes(), nil
}

// mountMachineAuth adds the non-RPC machine credential endpoints without
// changing the existing edge's protocol admission or route behavior.
func mountMachineAuth(next http.Handler, routes http.Handler) http.Handler {
	if routes == nil {
		return next
	}
	mux := http.NewServeMux()
	mux.Handle("/oauth2/token", routes)
	mux.Handle("/.well-known/jwks.json", routes)
	mux.Handle("/", next)
	return mux
}
