// Command payrollsim runs the payroll-provider simulator
// (internal/connectivity/payrollsim) as a plain process on its own port, so
// the promotion workflow's payroll hand-off can be integration-tested against
// a real HTTP peer. It is deliberately not containerised: the simulator is a
// dev and test tool, started beside the HCM server with `go run`.
//
// This command only parses flags (each with an environment fallback), wires
// the library and owns the process lifecycle; every behaviour it exposes is
// implemented and tested in the library.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/payrollsim"
)

const (
	defaultListen = "127.0.0.1:8095"
	// The dev defaults are recognisable on sight so one leaking into a shared
	// environment is obvious in any log or header dump.
	defaultSecret = "payrollsim-local-dev-secret-only"
	defaultAPIKey = "payrollsim-local-dev-key-only"
	drainTimeout  = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "payrollsim:", err)
		os.Exit(1)
	}
}

// options is the resolved command configuration.
type options struct {
	listen       string
	secret       string
	apiKey       string
	controlToken string
	applyDelay   time.Duration
	maxAttempts  int
}

// parseOptions resolves flags over environment over defaults. Environment
// values are applied as flag defaults, so an explicit flag always wins.
func parseOptions(args []string, getenv func(string) string, stderr io.Writer) (options, error) {
	envOr := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	applyDelay := time.Duration(payrollsim.DefaultScenario().ApplyDelayMS) * time.Millisecond
	if v := getenv("PAYROLLSIM_APPLY_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return options{}, fmt.Errorf("PAYROLLSIM_APPLY_DELAY: %w", err)
		}
		applyDelay = d
	}
	maxAttempts := 10
	if v := getenv("PAYROLLSIM_MAX_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return options{}, fmt.Errorf("PAYROLLSIM_MAX_ATTEMPTS: %w", err)
		}
		maxAttempts = n
	}

	var o options
	fs := flag.NewFlagSet("payrollsim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.listen, "listen", envOr("PAYROLLSIM_LISTEN", defaultListen), "listen address (env PAYROLLSIM_LISTEN)")
	fs.StringVar(&o.secret, "secret", envOr("PAYROLLSIM_SECRET", defaultSecret), "callback HMAC secret (env PAYROLLSIM_SECRET)")
	fs.StringVar(&o.apiKey, "api-key", envOr("PAYROLLSIM_API_KEY", defaultAPIKey), "comma-separated X-Api-Key values accepted on pay-change endpoints; list two to rotate (env PAYROLLSIM_API_KEY)")
	fs.StringVar(&o.controlToken, "control-token", getenv("PAYROLLSIM_CONTROL_TOKEN"), "X-Sim-Control-Token for /v1/_control; empty leaves control open (env PAYROLLSIM_CONTROL_TOKEN)")
	fs.DurationVar(&o.applyDelay, "apply-delay", applyDelay, "initial scenario apply delay (env PAYROLLSIM_APPLY_DELAY)")
	fs.IntVar(&o.maxAttempts, "max-attempts", maxAttempts, "callback attempts per event (env PAYROLLSIM_MAX_ATTEMPTS)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if o.applyDelay < 0 {
		return options{}, errors.New("-apply-delay must not be negative")
	}
	if o.maxAttempts < 1 {
		return options{}, errors.New("-max-attempts must be at least 1")
	}
	if o.secret == "" {
		return options{}, errors.New("-secret must not be empty")
	}
	if o.apiKey == "" {
		return options{}, errors.New("-api-key must not be empty")
	}
	if _, err := splitList(o.apiKey); err != nil {
		return options{}, fmt.Errorf("-api-key: %w", err)
	}
	return o, nil
}

// splitList parses a comma-separated credential list. Entries are trimmed;
// an empty entry ("a,,b" or a trailing comma) is an error rather than being
// skipped, because it almost always means a key was lost in templating.
func splitList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
		if parts[i] == "" {
			return nil, errors.New("empty entry in comma-separated list")
		}
	}
	return parts, nil
}

// run serves until ctx is cancelled, then stops the listener and drains
// in-flight callbacks for up to drainTimeout. ready, when non-nil, receives
// the bound address (tests listen on port 0).
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer, ready func(addr string)) error {
	o, err := parseOptions(args, getenv, stderr)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if o.secret == defaultSecret {
		logger.Warn("using the built-in development callback secret; set -secret or PAYROLLSIM_SECRET outside local dev")
	}
	apiKeys, _ := splitList(o.apiKey) // validated by parseOptions
	if slices.Contains(apiKeys, defaultAPIKey) {
		logger.Warn("using the built-in development API key; set -api-key or PAYROLLSIM_API_KEY outside local dev")
	}
	if o.controlToken == "" {
		logger.Warn("control endpoints are unauthenticated; set -control-token or PAYROLLSIM_CONTROL_TOKEN outside local dev")
	}

	scenario := payrollsim.DefaultScenario()
	scenario.ApplyDelayMS = o.applyDelay.Milliseconds()
	sim := payrollsim.New(payrollsim.Config{
		Secret:       []byte(o.secret),
		APIKeys:      apiKeys,
		ControlToken: o.controlToken,
		Scenario:     &scenario,
		MaxAttempts:  o.maxAttempts,
		Logger:       logger,
	})

	ln, err := net.Listen("tcp", o.listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: sim, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("payrollsim listening", "addr", ln.Addr().String(), "apply_delay", o.applyDelay, "max_attempts", o.maxAttempts, "api_keys", len(apiKeys))
	if ready != nil {
		ready(ln.Addr().String())
	}

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	logger.Info("payrollsim shutting down", "drain_timeout", drainTimeout)
	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	httpErr := srv.Shutdown(drainCtx)
	simErr := sim.Shutdown(drainCtx)
	if simErr != nil {
		logger.Warn("callbacks abandoned at drain timeout", "err", simErr)
	}
	return errors.Join(httpErr, simErr)
}
