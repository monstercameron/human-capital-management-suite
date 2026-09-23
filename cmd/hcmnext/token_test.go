package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestMain brings up the ephemeral PostgreSQL this package's cell-composing
// tests share (internal/data/pgtest), the same way test/workspace and
// test/bootstrap do.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_CHAT_043_DevMachineToken(t *testing.T) {
	key := strings.Repeat("k", 32)
	at := time.Now().UTC()
	p, err := parseTokenArgs([]string{"-dev-hmac-key=" + key, "-tenant=tenant-a", "-subject=agent-a", "-subject-kind=agent", "-ttl=15m"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	token, err := mintDevToken(p, at)
	if err != nil {
		t.Fatal(err)
	}
	v, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(key), Issuer: p.issuer, Audience: p.audience, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := v.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil || identity.SubjectKind() != trust.SubjectKindAgent || identity.Subject() != "agent-a" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	if len(identity.Roles()) != 0 || len(identity.Purposes()) != 1 || identity.Purposes()[0] != "chat_integration" {
		t.Fatalf("machine roles=%v purposes=%v", identity.Roles(), identity.Purposes())
	}
	if _, err := parseTokenArgs([]string{"-dev-hmac-key=" + key, "-tenant=tenant-a", "-subject=agent-a", "-subject-kind=agent"}, io.Discard); err == nil {
		t.Fatal("long-lived machine credential accepted")
	}
	if _, err := parseTokenArgs([]string{"-dev-hmac-key=" + key, "-tenant=tenant-a", "-subject=agent-a", "-subject-kind=agent", "-ttl=15m", "-roles=comp_admin"}, io.Discard); err == nil {
		t.Fatal("machine human role accepted")
	}
}

// newTokenTestCell migrates a private schema and composes a real cell whose
// Verifier is the HMAC verifier under key, issuer and audience - exactly the
// shape "hcmnext serve" builds in buildServe. Tests in this file use it to
// prove a token minted by this package's own code is a token that cell's
// discovery route accepts.
func newTokenTestCell(t *testing.T, key, issuer, audience string, now time.Time) *app.Cell {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID("cell-token-test"))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(key),
		Issuer:   issuer,
		Audience: audience,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	cell, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    verifier,
		Audience:    audience,
		MaxDeadline: 30 * time.Second,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	return cell
}

// discoveryStatus GETs the composed cell's own discovery route with token as
// the bearer credential and returns the HTTP status.
func discoveryStatus(t *testing.T, cell *app.Cell, token string) int {
	t.Helper()
	handler, err := transportcell.NewEdgeHandler(cell)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+app.DiscoveryPath, nil)
	if err != nil {
		t.Fatalf("build discovery request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET discovery: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()
	return res.StatusCode
}

// TestTokenCommandMintsACredentialTheServerAccepts is the token subcommand's
// primary conformance test: a credential minted by this package's own code,
// under the server's signing key, is admitted by a real composed cell's
// discovery route; the identical mint under any other key is refused. This is
// what "the same HMAC dev verifier the server uses" means in practice - not a
// second implementation of the token format that could quietly drift from
// internal/trust.HMACVerifier.
func TestTokenCommandMintsACredentialTheServerAccepts(t *testing.T) {
	t.Parallel()

	const (
		serverKey = "hcmnext-token-test-server-signing-key-32+"
		wrongKey  = "hcmnext-token-test-a-different-key-32-byt"
	)
	tenant := string(fixtures.Tenant)
	baseTime := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	cell := newTokenTestCell(t, serverKey, defaultIssuer, defaultAudience, baseTime)

	mint := func(t *testing.T, key string) string {
		t.Helper()
		token, err := mintDevToken(tokenParams{
			hmacKey:  key,
			issuer:   defaultIssuer,
			audience: defaultAudience,
			tenant:   tenant,
			subject:  "user-token-cli-test",
			roles:    []string{"comp_admin"},
			purpose:  "compensation_review",
			ttl:      time.Hour,
		}, baseTime)
		if err != nil {
			t.Fatalf("mintDevToken: %v", err)
		}
		return token
	}

	t.Run("minted with the server's own key, the discovery route admits it", func(t *testing.T) {
		token := mint(t, serverKey)
		if status := discoveryStatus(t, cell, token); status != http.StatusOK {
			t.Fatalf("discovery status = %d, want 200", status)
		}
	})

	t.Run("minted with a different key, the same route refuses it", func(t *testing.T) {
		token := mint(t, wrongKey)
		if status := discoveryStatus(t, cell, token); status != http.StatusUnauthorized {
			t.Fatalf("discovery status = %d, want 401", status)
		}
	})

	t.Run("runToken end to end prints a token the same route admits", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := runToken([]string{
			"-dev-hmac-key=" + serverKey,
			"-issuer=" + defaultIssuer,
			"-audience=" + defaultAudience,
			"-tenant=" + tenant,
			"-subject=user-token-cli-e2e",
		}, &stdout, &stderr, func() time.Time { return baseTime })
		if code != 0 {
			t.Fatalf("runToken exit = %d, stderr = %q", code, stderr.String())
		}
		token := strings.TrimSpace(stdout.String())
		if token == "" {
			t.Fatal("runToken printed no token to stdout")
		}
		if status := discoveryStatus(t, cell, token); status != http.StatusOK {
			t.Fatalf("discovery status for the CLI-minted token = %d, want 200", status)
		}
	})

	t.Run("a signing key shorter than 32 bytes is refused before minting anything", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := runToken([]string{
			"-dev-hmac-key=too-short",
			"-tenant=" + tenant,
			"-subject=user-token-cli-short-key",
		}, &stdout, &stderr, func() time.Time { return baseTime })
		if code == 0 {
			t.Fatalf("runToken with a short key exited 0; stdout=%q", stdout.String())
		}
		if stdout.String() != "" {
			t.Errorf("runToken with a short key wrote to stdout: %q", stdout.String())
		}
		if stderr.String() == "" {
			t.Error("runToken with a short key printed no explanation to stderr")
		}
	})

	t.Run("a missing tenant or subject is refused before minting anything", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := runToken([]string{
			"-dev-hmac-key=" + serverKey,
		}, &stdout, &stderr, func() time.Time { return baseTime })
		if code == 0 {
			t.Fatalf("runToken with no -tenant/-subject exited 0; stdout=%q", stdout.String())
		}
		if stdout.String() != "" {
			t.Errorf("runToken with no -tenant/-subject wrote to stdout: %q", stdout.String())
		}
	})

	t.Run("HCMNEXT_DEV_HMAC_KEY supplies the key when -dev-hmac-key is omitted", func(t *testing.T) {
		// Not t.Setenv: the parent test is t.Parallel, and t.Setenv panics
		// under a parallel ancestor. This subtest itself never calls
		// t.Parallel, so a plain os.Setenv/restore is race-free here.
		old, had := os.LookupEnv(EnvDevHMACKey)
		if err := os.Setenv(EnvDevHMACKey, serverKey); err != nil {
			t.Fatalf("Setenv: %v", err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(EnvDevHMACKey, old)
			} else {
				_ = os.Unsetenv(EnvDevHMACKey)
			}
		})
		var stdout, stderr strings.Builder
		code := runToken([]string{
			"-issuer=" + defaultIssuer,
			"-audience=" + defaultAudience,
			"-tenant=" + tenant,
			"-subject=user-token-cli-env-key",
		}, &stdout, &stderr, func() time.Time { return baseTime })
		if code != 0 {
			t.Fatalf("runToken with the key from %s exit = %d, stderr = %q", EnvDevHMACKey, code, stderr.String())
		}
		token := strings.TrimSpace(stdout.String())
		if status := discoveryStatus(t, cell, token); status != http.StatusOK {
			t.Fatalf("discovery status for the env-keyed token = %d, want 200", status)
		}
	})
}

// TestTokenCommandCarriesTheOrganizationScope pins the -org-scope flag: the
// kernel refuses to create an intent whose initiator carries no
// organization_scope_id (internal/intent.Instance.Validate), so a credential
// minted without one can read the workspace but never propose. The claim
// must round-trip through the same verifier serve authenticates with.
func TestTokenCommandCarriesTheOrganizationScope(t *testing.T) {
	t.Parallel()

	const key = "hcmnext-token-test-org-scope-signing-key-32+"
	baseTime := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	params, err := parseTokenArgs([]string{
		"-dev-hmac-key=" + key, "-tenant=" + string(fixtures.Tenant), "-subject=user-org-scope",
		"-org-scope= org:harborcare-demo:people-ops ",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseTokenArgs: %v", err)
	}
	if params.orgScope != "org:harborcare-demo:people-ops" {
		t.Fatalf("orgScope = %q, want the trimmed flag value", params.orgScope)
	}

	token, err := mintDevToken(params, baseTime)
	if err != nil {
		t.Fatalf("mintDevToken: %v", err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(key), Issuer: defaultIssuer, Audience: defaultAudience,
		Now: func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := principal.OrganizationScopeID(); got != "org:harborcare-demo:people-ops" {
		t.Fatalf("OrganizationScopeID = %q, want the minted scope", got)
	}

	// The default stays empty: a scope is an assertion about where the
	// subject acts, not something this CLI should invent.
	bare, err := parseTokenArgs([]string{"-dev-hmac-key=" + key, "-tenant=t", "-subject=s"}, io.Discard)
	if err != nil {
		t.Fatalf("parseTokenArgs (bare): %v", err)
	}
	if bare.orgScope != "" {
		t.Fatalf("default orgScope = %q, want empty", bare.orgScope)
	}
}
