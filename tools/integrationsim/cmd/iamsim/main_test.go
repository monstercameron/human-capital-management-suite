package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/iamsim"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseOptionsDefaults(t *testing.T) {
	o, err := parseOptions(nil, envMap(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := options{
		listen: "127.0.0.1:8096", clientID: "hcm-next-local", clientSecret: defaultClientSecret,
		webhookSecret: defaultWebhookSecret, tokenTTL: 300 * time.Second, grantDelay: 1500 * time.Millisecond, maxAttempts: 10,
	}
	if o != want {
		t.Fatalf("defaults = %+v, want %+v", o, want)
	}
}

func TestParseOptionsEnvThenFlagPrecedence(t *testing.T) {
	env := envMap(map[string]string{
		"IAMSIM_LISTEN": "127.0.0.1:9000", "IAMSIM_CLIENT_ID": "env-id", "IAMSIM_CLIENT_SECRET": "env-secret",
		"IAMSIM_WEBHOOK_SECRET": "env-wh", "IAMSIM_CONTROL_TOKEN": "env-ctl", "IAMSIM_TOKEN_TTL": "30s",
		"IAMSIM_GRANT_DELAY": "0s", "IAMSIM_MAX_ATTEMPTS": "3",
	})
	o, err := parseOptions(nil, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.listen != "127.0.0.1:9000" || o.clientID != "env-id" || o.clientSecret != "env-secret" || o.webhookSecret != "env-wh" ||
		o.controlToken != "env-ctl" || o.tokenTTL != 30*time.Second || o.grantDelay != 0 || o.maxAttempts != 3 {
		t.Fatalf("env = %+v", o)
	}
	o, err = parseOptions([]string{"-listen", "127.0.0.1:9001", "-client-id", "flag-id", "-token-ttl", "1m", "-max-attempts", "5"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.listen != "127.0.0.1:9001" || o.clientID != "flag-id" || o.tokenTTL != time.Minute || o.maxAttempts != 5 || o.clientSecret != "env-secret" {
		t.Fatalf("flags = %+v", o)
	}
}

func TestParseOptionsRejectsBadValues(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  map[string]string
	}{
		"ttl too short":  {[]string{"-token-ttl", "500ms"}, nil},
		"negative delay": {[]string{"-grant-delay", "-1s"}, nil},
		"zero attempts":  {[]string{"-max-attempts", "0"}, nil},
		"empty secret":   {[]string{"-client-secret", ""}, nil},
		"empty webhook":  {[]string{"-webhook-secret", ""}, nil},
		"empty id":       {[]string{"-client-id", ""}, nil},
		"stray arg":      {[]string{"extra"}, nil},
		"unknown flag":   {[]string{"-nope"}, nil},
		"env ttl":        {nil, map[string]string{"IAMSIM_TOKEN_TTL": "soon"}},
		"env delay":      {nil, map[string]string{"IAMSIM_GRANT_DELAY": "later"}},
		"env attempts":   {nil, map[string]string{"IAMSIM_MAX_ATTEMPTS": "many"}},
	}
	for name, tc := range cases {
		if _, err := parseOptions(tc.args, envMap(tc.env), io.Discard); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := parseOptions([]string{"-h"}, envMap(nil), io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h = %v", err)
	}
}

// lockedBuffer is a goroutine-safe log sink.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestRunServesTwoStageFlowAndShutsDown boots the real listener on port 0,
// runs stage one and a stage-two read, then cancels and expects a clean stop
// with no secret or token in the logs.
func TestRunServesTwoStageFlowAndShutsDown(t *testing.T) {
	o, err := parseOptions([]string{"-listen", "127.0.0.1:0"}, envMap(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, o, logger, func(a string) { addrCh <- a }) }()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("listener never came up")
	}
	base := "http://" + addr

	resp, err := http.Get(base + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %v %v", resp, err)
	}
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodPost, base+"/oauth2/token", strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.clientID, o.clientSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tr iamsim.TokenResponse
	_ = json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || tr.ExpiresIn != 300 || tr.AccessToken == "" {
		t.Fatalf("token = %d %+v", resp.StatusCode, tr)
	}

	sreq, _ := http.NewRequest(http.MethodGet, base+"/v1/access-changes/iam:unknown", nil)
	sreq.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	resp, err = http.DefaultClient.Do(sreq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("authenticated status lookup = %d, want 404", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not stop")
	}
	out := logs.String()
	for _, want := range []string{"development client secret", "development webhook secret", "control endpoints are unauthenticated", "iamsim stopped", "token issued"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{o.clientSecret, o.webhookSecret, tr.AccessToken} {
		if strings.Contains(out, secret) {
			t.Fatalf("logs leak a secret or token:\n%s", out)
		}
	}
}

func TestRunListenFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	o, _ := parseOptions([]string{"-listen", ln.Addr().String(), "-client-secret", "s", "-webhook-secret", "w", "-control-token", "c"}, envMap(nil), io.Discard)
	logs := &lockedBuffer{}
	err = run(context.Background(), o, slog.New(slog.NewTextHandler(logs, nil)), nil)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("run on busy port = %v", err)
	}
	if strings.Contains(logs.String(), "development") || strings.Contains(logs.String(), "unauthenticated") {
		t.Fatalf("warned despite explicit secrets: %s", logs)
	}
}

func TestSplitListAndMultiSecretValidation(t *testing.T) {
	got, err := splitList("a, b")
	if err != nil || strings.Join(got, "|") != "a|b" {
		t.Fatalf("splitList = %v %v", got, err)
	}
	for _, bad := range []string{"a,,b", "a,", " ,a"} {
		if _, err := parseOptions([]string{"-client-secret", bad}, envMap(nil), io.Discard); err == nil {
			t.Errorf("-client-secret %q accepted", bad)
		}
	}
	o, err := parseOptions(nil, envMap(map[string]string{"IAMSIM_CLIENT_SECRET": "old,new"}), io.Discard)
	if err != nil || o.clientSecret != "old,new" {
		t.Fatalf("env list = %+v %v", o, err)
	}
}

// TestRunAcceptsEveryListedClientSecret boots the command with two client
// secrets and mints a token with each; the joined string is not a secret.
func TestRunAcceptsEveryListedClientSecret(t *testing.T) {
	o, err := parseOptions([]string{"-listen", "127.0.0.1:0", "-client-secret", "sec-old,sec-new", "-webhook-secret", "w", "-control-token", "c"}, envMap(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, o, slog.New(slog.NewTextHandler(logs, nil)), func(a string) { addrCh <- a }) }()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("listener never came up")
	}
	for secret, want := range map[string]int{"sec-old": http.StatusOK, "sec-new": http.StatusOK, "sec-old,sec-new": http.StatusUnauthorized} {
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/oauth2/token", strings.NewReader("grant_type=client_credentials"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(url.QueryEscape(o.clientID), url.QueryEscape(secret))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("secret %q = %d, want %d", secret, resp.StatusCode, want)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run = %v", err)
	}
	if out := logs.String(); strings.Contains(out, "sec-old") || strings.Contains(out, "development client secret") || !strings.Contains(out, "client_secrets=2") {
		t.Fatalf("logs:\n%s", out)
	}
}
