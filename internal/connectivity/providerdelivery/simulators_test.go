package providerdelivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/iamsim"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/oauthcc"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/payrollsim"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/transport"
)

// parkUntilShutdown stands in for the simulators' apply/grant delay so no
// callback is attempted during the test; Shutdown cancels it.
func parkUntilShutdown(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

func shutdownNow(t *testing.T, stop func(context.Context) error) {
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = stop(ctx)
	})
}

// loopbackClient is the production egress boundary with the local-simulator
// opt-in, proving the clients work through it.
func loopbackClient(t *testing.T, rawURL string) Doer {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	trust := transport.DestinationTrust{Schemes: []string{"http"}, Hosts: []string{u.Hostname()}, AllowLoopback: true}
	return transport.NewSafeHTTPClient(trust, nil)
}

func TestIntegrationPayrollClientAgainstPayrollSim(t *testing.T) {
	sim := payrollsim.New(payrollsim.Config{Secret: []byte("cb-secret"), APIKey: testAPIKey, Sleep: parkUntilShutdown})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	shutdownNow(t, sim.Shutdown)

	c := newPayroll(t, srv.URL, loopbackClient(t, srv.URL), 5*time.Second)
	first, err := c.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || first.Outcome != Delivered || first.Status != 202 || !strings.HasPrefix(first.ProviderRef, "PSIM-") {
		t.Fatalf("first: %+v err=%v", first, err)
	}
	replay, err := c.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || replay.Outcome != Delivered || replay.Status != 200 || replay.ProviderRef != first.ProviderRef {
		t.Fatalf("replay: %+v err=%v", replay, err)
	}
	changed := strings.Replace(payrollPayload, `"160000.00"`, `"170000.00"`, 1)
	conflict, err := c.Deliver(context.Background(), "chg-1", []byte(changed), callbackURL)
	if err != nil || conflict.Outcome != Rejected || conflict.Class != ClassRejectedConflict {
		t.Fatalf("conflict: %+v err=%v", conflict, err)
	}
	bad, err := NewPayrollClient(PayrollConfig{BaseURL: srv.URL, APIKey: "wrong-key", Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := bad.Deliver(context.Background(), "chg-2", []byte(strings.ReplaceAll(payrollPayload, "chg-1", "chg-2")), callbackURL); res.Outcome != Rejected || res.Class != ClassRejectedAuth || res.Reason != "invalid_api_key" {
		t.Fatalf("bad key: %+v", res)
	}
}

func TestIntegrationPayrollReverseAndStatusAgainstPayrollSim(t *testing.T) {
	sim := payrollsim.New(payrollsim.Config{Secret: []byte("cb-secret"), APIKey: testAPIKey, Sleep: parkUntilShutdown})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	shutdownNow(t, sim.Shutdown)
	c := newPayroll(t, srv.URL, loopbackClient(t, srv.URL), 5*time.Second)

	if st, err := c.Status(context.Background(), "chg-1"); err != nil || st.Known || st.Result.Status != 404 || st.Result.Outcome != Delivered {
		t.Fatalf("status before deliver: %+v err=%v", st, err)
	}
	if res, err := c.Reverse(context.Background(), "chg-1", "entered in error"); err != nil || res.Outcome != Rejected || res.Status != 404 {
		t.Fatalf("reverse unknown: %+v err=%v", res, err)
	}
	first, err := c.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || first.Outcome != Delivered {
		t.Fatalf("deliver: %+v err=%v", first, err)
	}
	st, err := c.Status(context.Background(), "chg-1")
	if err != nil || !st.Known || st.Status != "ACCEPTED" || st.ProviderRef != first.ProviderRef {
		t.Fatalf("status after deliver: %+v err=%v", st, err)
	}
	rev, err := c.Reverse(context.Background(), "chg-1", "entered in error")
	if err != nil || rev.Outcome != Delivered || rev.Class != ClassDelivered || rev.Status != 202 {
		t.Fatalf("reverse: %+v err=%v", rev, err)
	}
	replay, err := c.Reverse(context.Background(), "chg-1", "entered in error")
	if err != nil || replay.Outcome != Delivered || replay.Status != 200 {
		t.Fatalf("reverse replay: %+v err=%v", replay, err)
	}
	if res, _ := c.Reverse(context.Background(), "chg-1", "another reason"); res.Outcome != Rejected || res.Class != ClassRejectedConflict || res.Reason != "idempotency_key_reused" {
		t.Fatalf("reverse with other reason: %+v", res)
	}
	st, err = c.Status(context.Background(), "chg-1")
	if err != nil || !st.Known || (st.Status != "REVERSED" && st.Status != "REVERSAL_PENDING") {
		t.Fatalf("status after reverse: %+v err=%v", st, err)
	}
}

func TestIntegrationPayrollReverseRejectedChangeIsSettled(t *testing.T) {
	sc := payrollsim.DefaultScenario()
	sc.Mode, sc.ApplyDelayMS = payrollsim.ModeReject, 0
	// A zero apply delay settles at once; any other wait (callback retry
	// backoff) parks until shutdown.
	settleNow := func(ctx context.Context, d time.Duration) error {
		if d == 0 {
			return ctx.Err()
		}
		return parkUntilShutdown(ctx, d)
	}
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer cb.Close()
	sim := payrollsim.New(payrollsim.Config{Secret: []byte("cb-secret"), APIKey: testAPIKey, Scenario: &sc, Sleep: settleNow})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	shutdownNow(t, sim.Shutdown)
	c := newPayroll(t, srv.URL, srv.Client(), 5*time.Second)
	if _, err := c.Deliver(context.Background(), "chg-1", []byte(payrollPayload), cb.URL); err != nil {
		t.Fatal(err)
	}
	// The rejection is decided asynchronously; poll until the sim reports it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := c.Status(context.Background(), "chg-1")
		if err != nil {
			t.Fatal(err)
		}
		if st.Known && st.Status == "REJECTED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("change never rejected: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	res, err := c.Reverse(context.Background(), "chg-1", "entered in error")
	if err != nil || res.Outcome != Delivered || res.Class != ClassSettledNotReversible || res.Status != 409 {
		t.Fatalf("reverse of rejected change: %+v err=%v", res, err)
	}
}

func TestIntegrationAccessClientAgainstIAMSim(t *testing.T) {
	const id, secret = "hcm next:local", "s3cret+/:%&="
	sim := iamsim.New(iamsim.Config{ClientID: id, ClientSecret: secret, WebhookSecret: []byte("cb-secret"), Sleep: parkUntilShutdown})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	shutdownNow(t, sim.Shutdown)

	tokens, err := oauthcc.New(oauthcc.Config{TokenURL: srv.URL + "/oauth2/token", ClientID: id, ClientSecret: secret, Scope: iamsim.ScopeWrite, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAccessClient(AccessConfig{BaseURL: srv.URL, Tokens: tokens, Client: srv.Client(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || first.Outcome != Delivered || first.Status != 202 || first.ProviderRef == "" {
		t.Fatalf("first: %+v err=%v", first, err)
	}
	// Revoking every token makes the cached one invalid: the client must
	// refresh once and the replay must still resolve to the same change.
	if sim.RevokeAllTokens() == 0 {
		t.Fatal("no token was issued")
	}
	replay, err := c.Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || replay.Outcome != Delivered || replay.Status != 200 || replay.ProviderRef != first.ProviderRef {
		t.Fatalf("replay after revoke: %+v err=%v", replay, err)
	}

	wrong, err := oauthcc.New(oauthcc.Config{TokenURL: srv.URL + "/oauth2/token", ClientID: id, ClientSecret: "nope", Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := NewAccessClient(AccessConfig{BaseURL: srv.URL, Tokens: wrong, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := bad.Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL); res.Outcome != Rejected || res.Class != ClassRejectedAuth || res.Reason != "token_invalid_client" {
		t.Fatalf("bad client secret: %+v", res)
	}
}

func TestIntegrationAccessReverseAndStatusAgainstIAMSim(t *testing.T) {
	const id, secret = "hcm", "iam-client-secret"
	sim := iamsim.New(iamsim.Config{ClientID: id, ClientSecret: secret, WebhookSecret: []byte("cb-secret"), Sleep: parkUntilShutdown})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	shutdownNow(t, sim.Shutdown)
	tokens, err := oauthcc.New(oauthcc.Config{TokenURL: srv.URL + "/oauth2/token", ClientID: id, ClientSecret: secret, Scope: iamsim.ScopeWrite, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAccessClient(AccessConfig{BaseURL: srv.URL, Tokens: tokens, Client: srv.Client(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := c.Status(context.Background(), "acc-1"); err != nil || st.Known || st.Result.Status != 404 {
		t.Fatalf("status before deliver: %+v err=%v", st, err)
	}
	first, err := c.Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || first.Outcome != Delivered {
		t.Fatalf("deliver: %+v err=%v", first, err)
	}
	// A revoked token forces the single refresh on the status read too.
	sim.RevokeAllTokens()
	st, err := c.Status(context.Background(), "acc-1")
	if err != nil || !st.Known || st.Status != "ACCEPTED" || st.ProviderRef != first.ProviderRef {
		t.Fatalf("status after deliver: %+v err=%v", st, err)
	}
	sim.RevokeAllTokens()
	rev, err := c.Reverse(context.Background(), "acc-1", "role removed")
	if err != nil || rev.Outcome != Delivered || rev.Status != 202 {
		t.Fatalf("revoke: %+v err=%v", rev, err)
	}
	if replay, _ := c.Reverse(context.Background(), "acc-1", "role removed"); replay.Outcome != Delivered || replay.Status != 200 {
		t.Fatalf("revoke replay: %+v", replay)
	}
	st, err = c.Status(context.Background(), "acc-1")
	if err != nil || !st.Known || (st.Status != "REVOKED" && st.Status != "REVERSAL_PENDING") {
		t.Fatalf("status after revoke: %+v err=%v", st, err)
	}
}
