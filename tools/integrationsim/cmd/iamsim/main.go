// Command iamsim runs the identity/access provider simulator
// (internal/connectivity/iamsim) as a plain process on its own port, so the
// promotion workflow's access hand-off can be exercised end to end over real
// HTTP without Docker or a vendor sandbox.
//
// Every setting is a flag with an IAMSIM_* environment fallback (flag >
// environment > default). The client secret and webhook secret have local
// development defaults so the simulator starts with zero setup; a warning is
// logged whenever a default secret is in use, because those values are public
// in this source file. Secrets and access tokens are never logged.
//
// On SIGINT/SIGTERM the listener stops, then in-flight grant processing and
// callback retries are given a drain window before being abandoned.
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

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/iamsim"
)

const (
	defaultListen        = "127.0.0.1:8096"
	defaultClientSecret  = "iamsim-local-dev-client-secret-only"
	defaultWebhookSecret = "iamsim-local-dev-webhook-secret-only"
	defaultTokenTTL      = 300 * time.Second
	defaultGrantDelay    = 1500 * time.Millisecond
	defaultMaxAttempts   = 10
	drainTimeout         = 30 * time.Second
)

// options is the resolved command configuration.
type options struct {
	listen        string
	clientID      string
	clientSecret  string
	webhookSecret string
	controlToken  string
	tokenTTL      time.Duration
	grantDelay    time.Duration
	maxAttempts   int
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	opts, err := parseOptions(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("invalid configuration", "err", err)
		os.Exit(2)
	}
	if err := run(ctx, opts, logger, nil); err != nil {
		logger.Error("iamsim failed", "err", err)
		os.Exit(1)
	}
}

// parseOptions resolves flags over environment over defaults. Environment
// values seed the flag defaults, so an explicit flag always wins.
func parseOptions(args []string, getenv func(string) string, output io.Writer) (options, error) {
	envOr := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	envDuration := func(key string, def time.Duration) (time.Duration, error) {
		v := getenv(key)
		if v == "" {
			return def, nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		return d, nil
	}
	ttl, err := envDuration("IAMSIM_TOKEN_TTL", defaultTokenTTL)
	if err != nil {
		return options{}, err
	}
	delay, err := envDuration("IAMSIM_GRANT_DELAY", defaultGrantDelay)
	if err != nil {
		return options{}, err
	}
	attempts := defaultMaxAttempts
	if v := getenv("IAMSIM_MAX_ATTEMPTS"); v != "" {
		if attempts, err = strconv.Atoi(v); err != nil {
			return options{}, fmt.Errorf("IAMSIM_MAX_ATTEMPTS: %w", err)
		}
	}

	var o options
	fs := flag.NewFlagSet("iamsim", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&o.listen, "listen", envOr("IAMSIM_LISTEN", defaultListen), "listen address (IAMSIM_LISTEN)")
	fs.StringVar(&o.clientID, "client-id", envOr("IAMSIM_CLIENT_ID", iamsim.DefaultClientID), "OAuth client id (IAMSIM_CLIENT_ID)")
	fs.StringVar(&o.clientSecret, "client-secret", envOr("IAMSIM_CLIENT_SECRET", defaultClientSecret), "comma-separated OAuth client secrets, any of which authenticates; list two to rotate (IAMSIM_CLIENT_SECRET)")
	fs.StringVar(&o.webhookSecret, "webhook-secret", envOr("IAMSIM_WEBHOOK_SECRET", defaultWebhookSecret), "callback HMAC secret (IAMSIM_WEBHOOK_SECRET)")
	fs.StringVar(&o.controlToken, "control-token", getenv("IAMSIM_CONTROL_TOKEN"), "token for /v1/_control endpoints; empty leaves them open (IAMSIM_CONTROL_TOKEN)")
	fs.DurationVar(&o.tokenTTL, "token-ttl", ttl, "access token lifetime (IAMSIM_TOKEN_TTL)")
	fs.DurationVar(&o.grantDelay, "grant-delay", delay, "delay before a change is granted or rejected (IAMSIM_GRANT_DELAY)")
	fs.IntVar(&o.maxAttempts, "max-attempts", attempts, "callback attempts per delivery (IAMSIM_MAX_ATTEMPTS)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	switch {
	case o.clientID == "":
		return options{}, errors.New("client-id must not be empty")
	case o.clientSecret == "":
		return options{}, errors.New("client-secret must not be empty")
	case o.webhookSecret == "":
		return options{}, errors.New("webhook-secret must not be empty")
	case o.tokenTTL < time.Second:
		return options{}, errors.New("token-ttl must be at least 1s")
	case o.grantDelay < 0:
		return options{}, errors.New("grant-delay must not be negative")
	case o.maxAttempts < 1:
		return options{}, errors.New("max-attempts must be at least 1")
	}
	if _, err := splitList(o.clientSecret); err != nil {
		return options{}, fmt.Errorf("client-secret: %w", err)
	}
	return o, nil
}

// splitList parses a comma-separated credential list. Entries are trimmed;
// an empty entry ("a,,b" or a trailing comma) is an error rather than being
// skipped, because it almost always means a secret was lost in templating.
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

// run serves the simulator until ctx is cancelled, then shuts down: stop the
// listener, then drain callbacks. ready, when non-nil, receives the bound
// address once the listener is up (tests bind port 0).
func run(ctx context.Context, o options, logger *slog.Logger, ready func(addr string)) error {
	clientSecrets, _ := splitList(o.clientSecret) // validated by parseOptions
	if slices.Contains(clientSecrets, defaultClientSecret) {
		logger.Warn("using the built-in development client secret; set IAMSIM_CLIENT_SECRET outside local development")
	}
	if o.webhookSecret == defaultWebhookSecret {
		logger.Warn("using the built-in development webhook secret; set IAMSIM_WEBHOOK_SECRET outside local development")
	}
	if o.controlToken == "" {
		logger.Warn("control endpoints are unauthenticated; set IAMSIM_CONTROL_TOKEN to guard them")
	}
	sc := iamsim.DefaultScenario()
	sc.TokenTTLSeconds = int64(o.tokenTTL / time.Second)
	sc.GrantDelayMS = o.grantDelay.Milliseconds()
	sim := iamsim.New(iamsim.Config{
		ClientID:      o.clientID,
		ClientSecrets: clientSecrets,
		WebhookSecret: []byte(o.webhookSecret),
		ControlToken:  o.controlToken,
		Scenario:      &sc,
		MaxAttempts:   o.maxAttempts,
		Logger:        logger,
	})

	ln, err := net.Listen("tcp", o.listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", o.listen, err)
	}
	srv := &http.Server{Handler: sim, ReadHeaderTimeout: 10 * time.Second}
	logger.Info("iamsim listening", "addr", ln.Addr().String(), "client_id", o.clientID, "client_secrets", len(clientSecrets), "token_ttl", o.tokenTTL, "grant_delay", o.grantDelay, "max_attempts", o.maxAttempts)
	if ready != nil {
		ready(ln.Addr().String())
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down; draining callbacks", "timeout", drainTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	httpErr := srv.Shutdown(shutdownCtx)
	simErr := sim.Shutdown(shutdownCtx)
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	if httpErr != nil {
		return fmt.Errorf("http shutdown: %w", httpErr)
	}
	if simErr != nil {
		return fmt.Errorf("callback drain: %w", simErr)
	}
	logger.Info("iamsim stopped")
	return nil
}
