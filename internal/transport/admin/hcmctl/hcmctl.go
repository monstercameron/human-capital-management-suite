// Package hcmctl is the SVC-011/ADMIN-001 thin operator CLI: it parses
// flags, dials hcmnext.admin.v1.AdminService with the generated Go gRPC
// client (gen/go/hcmnext/admin/v1), attaches a bearer credential, calls
// exactly one method per invocation, and prints the typed response. It
// contains no business or store logic of any kind: every fact it prints
// came back from the server on the wire, and every write this package could
// conceivably attempt against workforce data does not exist, because
// AdminService publishes no mutating method (internal/operations/admin,
// internal/transport/admin). The onboarding subcommand advances a
// tenant-scoped onboarding run through the generated OnboardingService,
// which requires the operator role and records every transition.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: SVC-011,
// ADMIN-001.
//
// # Why this lives here and not at cmd/hcmctl
//
// ADMIN-001 names the binary "hcmctl"; SVC-011 names it "cmd/admin". Both
// would need a new top-level cmd/ directory, and the repository's own
// governing manifests currently forbid that: definitions/architecture/
// repository-layout.yaml's approved_commands.initial lists exactly
// {hcmnext, worker, projector, migrate}, and both it and definitions/
// architecture/process-roles.yaml explicitly reserve the "admin" process
// for P1B ("No cmd/admin directory is expected before P1B"; process-roles.yaml
// status: later). tools/policy/layout and tools/policy/processroles assert
// this with a real filesystem scan of cmd/*, so creating cmd/hcmctl or
// cmd/admin today would fail already-passing architecture policy tests.
// internal/transport is this package's actual home rather than
// internal/operations because it dials gRPC directly
// (definitions/architecture/dependency-roles.yaml's grpc/protobuf library
// firewall rows admit only internal/transport, gen, tools/gen and cmd as
// import roots for those libraries).
//
// This package is the whole of that CLI as a library: [Main] is the same
// three-line composition root a cmd/hcmctl/main.go would call
// (os.Exit(hcmctl.Main(os.Args[1:], os.Stdout, os.Stderr))). Promoting it to
// a real command is a one-file change once repository-layout.yaml and
// process-roles.yaml are widened - see this change's report for the exact
// rows to edit; this lane does not edit definitions/ itself.
//
// # JIT context
//
// An operator rarely holds a long-lived bearer token for this surface by
// design (OperatorRole is deliberately not an ordinary session role). The
// -mint-* flags mint a short-lived development/test credential just in time
// for one invocation via the same internal/trust.HMACVerifier.Issue a
// federation adapter's production equivalent will eventually replace; nothing
// this package mints is retained past the process, and the signing key and
// resulting token are never echoed back by [Main] (see redact.go).
package hcmctl

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// Dialer opens the client connection [Main] issues every call on. It exists
// so a test can substitute an in-process (bufconn or loopback) dialer
// without Main itself knowing the difference; the default used outside
// tests is [DialInsecure].
type Dialer func(ctx context.Context, addr string) (*grpc.ClientConn, error)

// DialInsecure dials addr over plaintext gRPC. It is exported so a caller
// wiring a real cmd/hcmctl main.go (once the repository layout is widened)
// can pass it as the default [Dialer] without depending on this package's
// unexported plumbing.
func DialInsecure(_ context.Context, addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// Main is the CLI's whole composition root: parse flags, resolve a
// credential, dial, call exactly one AdminService method, print the result.
// It returns a process exit code and never calls os.Exit itself, so a test
// (or a future cmd/hcmctl/main.go) controls the process boundary.
func Main(args []string, stdout, stderr io.Writer, dial Dialer) int {
	if dial == nil {
		dial = DialInsecure
	}
	cmd, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, redactError(err))
		fmt.Fprintln(stderr, usage())
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), cmd.global.timeout)
	defer cancel()

	token, err := cmd.global.resolveToken()
	if err != nil {
		fmt.Fprintln(stderr, redactError(err))
		return 2
	}

	conn, err := dial(ctx, cmd.global.addr)
	if err != nil {
		fmt.Fprintln(stderr, "dial:", redactError(err))
		return 1
	}
	defer conn.Close()

	client := adminv1.NewAdminServiceClient(conn)
	ctx = metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)

	if cmd.runOnboarding != nil {
		onboardClient := adminv1.NewOnboardingServiceClient(conn)
		result, err := cmd.runOnboarding(ctx, onboardClient)
		if err != nil {
			fmt.Fprintln(stderr, redactError(err))
			return 1
		}
		fmt.Fprint(stdout, result)
		return 0
	}
	result, err := cmd.run(ctx, client)
	if err != nil {
		fmt.Fprintln(stderr, redactError(err))
		return 1
	}
	fmt.Fprint(stdout, result)
	return 0
}

// globalFlags are accepted before the subcommand name.
type globalFlags struct {
	addr    string
	token   string
	timeout time.Duration

	mint       bool
	signingKey string
	issuer     string
	audience   string
	tenant     string
	subject    string
	roles      string
	purpose    string
	ttl        time.Duration
}

// resolveToken returns the bearer credential for this invocation: the
// -token flag verbatim, or a freshly minted JIT credential when -mint is
// set. It never logs or returns the signing key.
func (g globalFlags) resolveToken() (string, error) {
	if !g.mint {
		if g.token == "" {
			return "", fmt.Errorf("hcmctl: -token is required unless -mint is set")
		}
		return g.token, nil
	}
	return mintToken(g)
}

func newGlobalFlagSet() (*flag.FlagSet, *globalFlags) {
	g := &globalFlags{}
	fs := flag.NewFlagSet("hcmctl", flag.ContinueOnError)
	fs.StringVar(&g.addr, "addr", "127.0.0.1:0", "AdminService gRPC address (host:port)")
	fs.StringVar(&g.token, "token", "", "bearer credential to present (mutually exclusive with -mint)")
	fs.DurationVar(&g.timeout, "timeout", 15*time.Second, "per-invocation deadline")
	fs.BoolVar(&g.mint, "mint", false, "mint a JIT development credential instead of using -token")
	fs.StringVar(&g.signingKey, "mint-key", "", "HMAC signing key for -mint (never printed)")
	fs.StringVar(&g.issuer, "mint-issuer", "", "issuer claim for -mint")
	fs.StringVar(&g.audience, "mint-audience", "", "audience claim for -mint")
	fs.StringVar(&g.tenant, "mint-tenant", "", "tenant claim for -mint")
	fs.StringVar(&g.subject, "mint-subject", "", "subject claim for -mint")
	fs.StringVar(&g.roles, "mint-roles", "hcmnext.trust.role.operator", "comma-separated roles claim for -mint")
	fs.StringVar(&g.purpose, "mint-purpose", "operator_diagnostics", "purpose claim for -mint")
	fs.DurationVar(&g.ttl, "mint-ttl", 15*time.Minute, "validity window for -mint")
	return fs, g
}

// usage documents every subcommand. It carries no business rule; it exists
// only so -h and a parse error tell the operator what exists.
func usage() string {
	return `usage: hcmctl [global flags] <subcommand> [subcommand flags]

subcommands:
  list-intents           list BusinessIntent instances
  release-manifest       render the endpoint/capability/intent-definition discovery document
  list-capabilities      list registered capability profiles
  explain-transaction    governed read-only transaction chronology
  worker-state           governed read-only worker/employment/assignment facts
  instance <id>          governed read-only workflow execution inspector
  onboarding             operator onboarding-pipeline runs (REV-036-01)
  explorer               ledger/provenance explorer and AuthZ simulator (REV-037-01)

global flags: -addr -token -timeout -mint -mint-key -mint-issuer -mint-audience
              -mint-tenant -mint-subject -mint-roles -mint-purpose -mint-ttl`
}
