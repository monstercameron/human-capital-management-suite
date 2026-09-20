package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseOptionsDefaults(t *testing.T) {
	o, err := parseOptions(nil, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := options{listen: defaultListen, secret: defaultSecret, apiKey: defaultAPIKey, applyDelay: 1500 * time.Millisecond, maxAttempts: 10}
	if o != want {
		t.Fatalf("defaults = %+v, want %+v", o, want)
	}
}

func TestParseOptionsFlagBeatsEnvBeatsDefault(t *testing.T) {
	e := env(map[string]string{
		"PAYROLLSIM_LISTEN":        "127.0.0.1:9000",
		"PAYROLLSIM_SECRET":        "env-secret",
		"PAYROLLSIM_API_KEY":       "env-key",
		"PAYROLLSIM_CONTROL_TOKEN": "env-token",
		"PAYROLLSIM_APPLY_DELAY":   "2s",
		"PAYROLLSIM_MAX_ATTEMPTS":  "4",
	})
	o, err := parseOptions([]string{"-listen", "127.0.0.1:9001", "-max-attempts", "7"}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := options{listen: "127.0.0.1:9001", secret: "env-secret", apiKey: "env-key", controlToken: "env-token", applyDelay: 2 * time.Second, maxAttempts: 7}
	if o != want {
		t.Fatalf("options = %+v, want %+v", o, want)
	}
}

func TestParseOptionsRejectsBadInput(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		env  map[string]string
	}{
		"bad env delay":     {env: map[string]string{"PAYROLLSIM_APPLY_DELAY": "soon"}},
		"bad env attempts":  {env: map[string]string{"PAYROLLSIM_MAX_ATTEMPTS": "many"}},
		"negative delay":    {args: []string{"-apply-delay", "-1s"}},
		"zero attempts":     {args: []string{"-max-attempts", "0"}},
		"empty secret":      {args: []string{"-secret", ""}},
		"empty api key":     {args: []string{"-api-key", ""}},
		"unknown flag":      {args: []string{"-nope"}},
		"stray positionals": {args: []string{"extra"}},
	} {
		if _, err := parseOptions(tc.args, env(tc.env), io.Discard); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// syncBuffer lets the test read logs the server goroutine is still writing.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunServesAndShutsDownGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logs := &syncBuffer{}
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-listen", "127.0.0.1:0"}, env(nil), logs, func(a string) { addrCh <- a })
	}()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Fatalf("healthz = %d %s", resp.StatusCode, body)
	}
	// The default API key is wired through: a keyless POST is refused.
	resp, err = http.Post("http://"+addr+"/v1/pay-changes", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("keyless intake = %d, want 401", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not shut down")
	}
	out := logs.String()
	for _, want := range []string{"development callback secret", "development API key", "control endpoints are unauthenticated", "payrollsim listening", "shutting down"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %q:\n%s", want, out)
		}
	}
}

func TestRunListenFailure(t *testing.T) {
	if err := run(context.Background(), []string{"-listen", "256.0.0.1:0"}, env(nil), io.Discard, nil); err == nil {
		t.Fatal("expected a listen error")
	}
}

func TestSplitListAndMultiKeyValidation(t *testing.T) {
	got, err := splitList(" a , b,c")
	if err != nil || strings.Join(got, "|") != "a|b|c" {
		t.Fatalf("splitList = %v %v", got, err)
	}
	for _, bad := range []string{"a,,b", "a,", ",a", " , "} {
		if _, err := splitList(bad); err == nil {
			t.Errorf("splitList(%q) accepted", bad)
		}
		if _, err := parseOptions([]string{"-api-key", bad}, env(nil), io.Discard); err == nil {
			t.Errorf("-api-key %q accepted", bad)
		}
	}
	o, err := parseOptions(nil, env(map[string]string{"PAYROLLSIM_API_KEY": "old,new"}), io.Discard)
	if err != nil || o.apiKey != "old,new" {
		t.Fatalf("env list = %+v %v", o, err)
	}
}

// TestRunAcceptsEveryListedAPIKey boots the command with two keys: both pass
// authentication (the empty body then fails validation with 400), any other
// key is 401, and no key value reaches the logs.
func TestRunAcceptsEveryListedAPIKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logs := &syncBuffer{}
	addrCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-listen", "127.0.0.1:0", "-api-key", "key-old,key-new", "-secret", "s", "-control-token", "c"}, env(nil), logs, func(a string) { addrCh <- a })
	}()
	var addr string
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server never became ready")
	}
	for key, want := range map[string]int{"key-old": http.StatusBadRequest, "key-new": http.StatusBadRequest, "key-old,key-new": http.StatusUnauthorized, "other": http.StatusUnauthorized} {
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/pay-changes", strings.NewReader("{}"))
		req.Header.Set("X-Api-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("key %q = %d, want %d", key, resp.StatusCode, want)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run = %v", err)
	}
	out := logs.String()
	if strings.Contains(out, "key-old") || strings.Contains(out, "development API key") || !strings.Contains(out, "api_keys=2") {
		t.Fatalf("logs:\n%s", out)
	}
}
