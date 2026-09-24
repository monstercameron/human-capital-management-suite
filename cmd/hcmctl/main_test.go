package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin/hcmctl"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_RECOVERY_001_HcmctlMatrixCommand proves the actual operator
// binary composition routes to recovery policy without dialing a server.
func TestTodo_RECOVERY_001_HcmctlMatrixCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	dialCalled := false
	dial := func(context.Context, string) (*grpc.ClientConn, error) {
		dialCalled = true
		return nil, errors.New("matrix command unexpectedly dialed a server")
	}
	if code := run([]string{"recovery", "matrix"}, &stdout, &stderr, dial); code != 0 {
		t.Fatalf("hcmctl recovery matrix exit = %d, stderr=%q", code, stderr.String())
	}
	var document struct {
		Version   int               `json:"version"`
		Contracts []json.RawMessage `json:"contracts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("hcmctl recovery matrix output is not JSON: %v\n%s", err, stdout.String())
	}
	if document.Version == 0 || len(document.Contracts) != 10 {
		t.Fatalf("hcmctl returned incomplete recovery matrix: version=%d contracts=%d", document.Version, len(document.Contracts))
	}
	if dialCalled {
		t.Fatal("read-only matrix command dialed the application server")
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"recovery", "restore-tenant"}, &stdout, &stderr, dial); code != 2 {
		t.Fatalf("hcmctl exposed an unapproved recovery action: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if dialCalled {
		t.Fatal("rejected recovery action dialed the application server")
	}
}

// startCommandFixtureServer boots a real AdminService - the same
// registration this binary's server side (internal/transport/cell.NewGRPCServer)
// performs, minus the rest of the cell - behind the shared trusted-request
// interceptor chain, on a loopback listener chosen by the OS, verified by a
// real internal/trust.HMACVerifier rather than a stub. That verifier is
// also what mints the JIT credential below, so this test exercises the same
// signing/verification pair -mint uses against a live server in production.
func startCommandFixtureServer(t *testing.T, verifier trust.Verifier) (addr string, cleanup func()) {
	t.Helper()
	cfg := transport.Config{Verifier: verifier}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, admin.Dependencies{})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), func() {
		srv.Stop()
		_ = lis.Close()
	}
}

// TestHcmctlCommandRunsAgainstAnInProcessAdminServer is ADMIN-001/SVC-011's
// command-level smoke test for the promoted cmd/hcmctl binary. It starts a
// real AdminService in-process on a loopback listener (mirroring
// internal/transport/admin's own integration-test pattern in
// admin_integration_test.go), then drives hcmctl.Main with main's own real
// dialer (hcmctl.DialInsecure) - not a test double - proving the exact
// composition this file's main() wires (os.Args, os.Stdout, os.Stderr,
// hcmctl.DialInsecure) reaches a live server end to end.
//
// Two invocations share the one server: an issuer-signed operator fixture
// credential succeeds and prints the required evidence line (ADMIN-001's
// "evidence IDs printed on every call"), while the identity-only development
// mint is rejected for lacking operator authority. Neither the signing key
// nor a bearer credential appears in stdout or stderr.
func TestHcmctlCommandRunsAgainstAnInProcessAdminServer(t *testing.T) {
	const signingKey = "hcmctl-command-smoke-test-signing-key-0123456789"
	const issuer = "hcmctl-smoke-issuer"
	const audience = "hcmctl-smoke-audience"

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(signingKey),
		Issuer:   issuer,
		Audience: audience,
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}

	addr, cleanup := startCommandFixtureServer(t, verifier)
	defer cleanup()

	mintArgs := func() []string {
		return []string{
			"-addr", addr,
			"-timeout", "10s",
			"-mint",
			"-mint-profile", "local-dev",
			"-mint-key", signingKey,
			"-mint-issuer", issuer,
			"-mint-audience", audience,
			"-mint-tenant", "acme-corp",
			"-mint-subject", "operator-smoke",
			"release-manifest",
		}
	}

	now := time.Now()
	operatorToken, err := verifier.Issue(trust.Claims{
		Issuer: issuer, Audience: audience, Subject: "operator-smoke",
		SubjectKind: "human", Tenant: "acme-corp",
		Roles: []string{admin.OperatorRole}, AuthenticationMethod: "bearer_token",
		Assurance: "high", SessionRef: "operator-smoke-session",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue operator fixture credential: %v", err)
	}

	t.Run("authorized_token_call_prints_evidence", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		args := []string{"-addr", addr, "-timeout", "10s", "-token", operatorToken, "release-manifest"}
		code := hcmctl.Main(args, &stdout, &stderr, hcmctl.DialInsecure)
		if code != 0 {
			t.Fatalf("hcmctl.Main exit code = %d, stderr = %s", code, stderr.String())
		}

		out := stdout.String()
		if !strings.Contains(out, "manifest_digest:") {
			t.Fatalf("output missing manifest_digest:\n%s", out)
		}
		if !strings.Contains(out, "evidence:") {
			t.Fatalf("output missing the required evidence line:\n%s", out)
		}

		assertNoLeakedCredential(t, signingKey, stdout.String()+stderr.String())
	})

	t.Run("identity_only_mint_is_rejected_without_leaking_the_credential", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := hcmctl.Main(mintArgs(), &stdout, &stderr, hcmctl.DialInsecure)
		if code == 0 {
			t.Fatalf("expected a non-zero exit code for identity without operator authority; stdout = %s", stdout.String())
		}

		assertNoLeakedCredential(t, signingKey, stdout.String()+stderr.String())
	})
}

// assertNoLeakedCredential fails t if combined (stdout+stderr from one
// hcmctl.Main invocation) contains the mint signing key or an unredacted
// "Bearer <token>" credential. hcmctl mints a fresh JWT-shaped token per
// call, so this checks the redaction contract by shape (redact.go's
// bearerPattern) and by the one secret this test itself supplied, rather
// than by a specific token value it does not control.
func assertNoLeakedCredential(t *testing.T, signingKey, combined string) {
	t.Helper()
	if strings.Contains(combined, signingKey) {
		t.Fatalf("output leaked the mint signing key:\n%s", combined)
	}
	lower := strings.ToLower(combined)
	if idx := strings.Index(lower, "bearer "); idx != -1 && !strings.HasPrefix(lower[idx:], "bearer [redacted]") {
		t.Fatalf("output contains an unredacted bearer credential:\n%s", combined)
	}
}
