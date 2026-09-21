// Command hcmnext is a command, not a composition. It parses arguments,
// selects an application role and invokes that role's lifecycle; what a role
// is made of belongs to internal/application (ARCH-GO-020).
//
// With no arguments it prints its build identity and exits, which is what a
// deployment check calls.
//
//	hcmnext          print the build identity
//	hcmnext serve    run the P1A cell: gRPC on -grpc-listen, HTTP edge on -http-listen
//	hcmnext token    mint a bearer credential the -dev-hmac-key verifier accepts
//
// # serve
//
// serve selects internal/application's serve role
// (application.RoleServe) and hands the resulting bootstrap.Spec to
// internal/platform/bootstrap. That role composes one P1A cell
// (internal/intent/app.NewCell) over a PostgreSQL store and publishes it on
// both transports. The two are handed the same transport.Config, which is
// what makes their trusted context identical by construction rather than by
// review. The HTTP edge additionally serves the API-001 discovery document at
// /v1/discovery and, unless -workspace=false, the human-facing Promotion
// workspace at /workspace/promotion. The workspace is admitted by the same
// bearer credential as the API and reads through the same governed capability
// gateway; -workspace=false publishes the API surface alone, and the
// discovery document then advertises no workspace route.
//
// -profile=local-dev applies loopback-only defaults for the local PostgreSQL
// URL, development key and tenant, skips automatic migration, enables the
// development browser admission path and the executable promotion plan, and
// shortens graceful shutdown. It does not bypass authentication or
// authorization. Explicit flags and environment values still win.
//
// -dev-browser-login=true additionally serves a dev-only pasted-token sign-in
// form at /workspace/login: off by default, because a workspace that is
// reachable with an Authorization header must not grow a second, cookie-based
// way in unless an operator says so explicitly. When it is on, this command
// prints the exact URL to open once the listeners are up.
//
// -otel-exporter selects none (the default), stdout or otlphttp; none means
// this cell publishes no spans or metrics at all. otlphttp requires
// -otel-endpoint.
//
// -public-origin (or HCMNEXT_PUBLIC_ORIGIN) names the absolute http(s)
// origin browsers reach this cell at - for example
// https://hcm.example.com. It is needed only when a proxy between the
// browser and this listener terminates TLS or rewrites Host: the browser's
// Origin, the gRPC tunnel address the workspace shells emit and their
// connect-src policy then all have to name the public authority the request
// no longer carries. Left empty, localhost and direct-VPS deployments derive
// everything from the request itself, which is the default.
//
// The execution engine is on by default: this cell is composed with the
// execution authority (internal/intent/app.ExecutionAuthority) and the
// caller-driven promotion approval driver (internal/platform/execution.
// NewPromotionExecution), so IntentService.ExecuteIntent runs the
// promote_worker workflow for an approved proposal through the engine for
// a caller who additionally holds -execution-authority-role.
// -execution-authority-digest names the authority amendment this process
// asserts (carried through as evidence, never verified here) and stays
// required; -execution-authority-approver names who the workflow's
// approval WorkItem is routed to. -execution-authority=false opts back
// out to the refusing cell (then -scheduler must also be false), and
// -workflow-plan=prototype simulates promotions without effects.
//
// Process lifecycle is not this command's business and is not implemented
// here: configuration precedence, the build banner, signal handling, the
// STARTING/READY/DRAINING/STOPPED health machine, the database pool, the
// run-group and the ordered deadline-bounded shutdown all come from
// internal/platform/bootstrap. Which configuration the role accepts and which
// workloads it runs come from internal/application.
//
// The schema is applied before the listeners start, unless -migrate=false.
// This is the plain Goose apply; the DB-006 journaled apply - artifact digest,
// tool version, checksum verification, owner, start and finish - lives in
// cmd/migrate and is a separate deliberate step:
//
//	go run ./cmd/migrate up
//
// A release pipeline runs cmd/migrate and starts hcmnext with -migrate=false;
// -migrate exists so a developer can bring a scratch database up in one
// command. The Goose runner itself stays in this command rather than moving
// to internal/application because definitions/architecture/
// library-firewall.yaml (LIB-008) confines github.com/pressly/goose/v3 to the
// migrations and cmd roots; the application root declares a Migrator port and
// this command supplies the adapter.
//
// # Authentication
//
// P1A authenticates with the deterministic HMAC development verifier
// (internal/trust). -dev-hmac-key is the shared signing key and must be at
// least 32 bytes; there is no default, because a listener with a default
// signing key is a listener anyone can forge a principal against.
//
// # token
//
// token mints a bearer credential with the same internal/trust.HMACVerifier
// serve authenticates with, under the same -dev-hmac-key (or
// HCMNEXT_DEV_HMAC_KEY), and prints it to stdout. It is the development
// counterpart of an identity provider: something to hand a curl command, the
// dev browser sign-in form, or a test, without hand-rolling the token format.
// See "hcmnext token -h" for its flags.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's convention. It is the application root's constant, re-stated
// here only so this command's own help text and its token subcommand read the
// same name the serve role registers.
const EnvDatabaseURL = application.EnvDatabaseURL

// EnvDevHMACKey carries the development signing key, so it need not appear in
// a process listing.
const EnvDevHMACKey = application.EnvDevHMACKey

// defaultIssuer and defaultAudience are the serve role's own
// -issuer/-audience defaults. token shares these same constants for its own
// -issuer/-audience defaults, so a credential minted with no flags beyond
// -dev-hmac-key/-tenant/-subject verifies against a serve process started
// with no flags beyond its own -dev-hmac-key: the two commands cannot drift
// apart by one of them changing a literal the other did not.
const (
	defaultIssuer   = application.DefaultIssuer
	defaultAudience = application.DefaultAudience
)

// minimumHMACKeyBytes is the shortest development signing key this command
// will mint or start a listener with.
const minimumHMACKeyBytes = application.MinimumHMACKeyBytes

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		info := buildinfo.Current()
		fmt.Fprintf(os.Stdout, "hcmnext %s revision=%s modified=%t go=%s\n",
			info.Module, info.Revision, info.Modified, info.GoVersion)
		return
	}
	switch args[0] {
	case "serve":
		spec, err := application.SpecFor(application.RoleServe, args[1:],
			application.WithMigrator(migrateUp))
		if err != nil {
			fmt.Fprintf(os.Stderr, "hcmnext: %v\n", err)
			os.Exit(1)
		}
		os.Exit(bootstrap.Run(context.Background(), spec))
	case "token":
		os.Exit(runToken(args[1:], os.Stdout, os.Stderr, time.Now))
	case "workflow-version":
		os.Exit(runWorkflowVersion(args[1:], os.Stdout, os.Stderr, time.Now, openPostgresVersionRegistry))
	default:
		fmt.Fprintf(os.Stderr, "hcmnext: unknown command %q; usage: hcmnext [serve|token|workflow-version]\n", args[0])
		os.Exit(1)
	}
}
