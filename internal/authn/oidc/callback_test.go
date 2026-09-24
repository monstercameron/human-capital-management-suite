package oidc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

func rsaFixture(t *testing.T) (issuerFixture, *testKeys, string) {
	t.Helper()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, time.Minute,
	)
	return fixture, keys, kid
}

func TestFlow_HandleCallback_MalformedParams(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)

	cases := []struct {
		name   string
		params oidc.CallbackParams
	}{
		{"no state", oidc.CallbackParams{Code: "code-1", Now: baseTime}},
		{"no code", oidc.CallbackParams{State: "state-1", Now: baseTime}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := flow.HandleCallback(context.Background(), tc.params)
			if !errors.Is(err, oidc.ErrCallbackMalformed) {
				t.Fatalf("HandleCallback error = %v, want ErrCallbackMalformed", err)
			}
		})
	}
}

func TestFlow_HandleCallback_AuthorizationDenied(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)

	_, ev, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{
		Error: "access_denied", ErrorDescription: "user cancelled", Now: baseTime,
	})
	if !errors.Is(err, oidc.ErrAuthorizationDenied) {
		t.Fatalf("HandleCallback error = %v, want ErrAuthorizationDenied", err)
	}
	if ev.Outcome != oidc.OutcomeDenied {
		t.Fatalf("evidence outcome = %q, want DENIED", ev.Outcome)
	}
}

func TestFlow_HandleCallback_UnknownState(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)

	_, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: "never-issued", Code: "code-1", Now: baseTime})
	if !errors.Is(err, oidc.ErrReplayedOrUnknownState) {
		t.Fatalf("HandleCallback error = %v, want ErrReplayedOrUnknownState", err)
	}
}

func TestFlow_HandleCallback_ExpiredPendingAuthorization(t *testing.T) {
	t.Parallel()
	fixture, keys, kid := rsaFixture(t)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, code := beginAndIssueCode(t, flow, idp, baseTime, "code-1", "user-1", nil, keys, trustfederation.AlgRS256, kid)

	// The default TTL is 10 minutes; arriving 11 minutes later must be
	// refused even though the state is still the first (and only) use.
	_, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(11 * time.Minute)})
	if !errors.Is(err, oidc.ErrVerifierExpired) {
		t.Fatalf("HandleCallback error = %v, want ErrVerifierExpired", err)
	}
}

func TestFlow_HandleCallback_MissingIDToken(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, accessToken: "at-1"}) // no idToken

	_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
	if !errors.Is(err, oidc.ErrMissingIDToken) {
		t.Fatalf("HandleCallback error = %v, want ErrMissingIDToken", err)
	}
}

func TestFlow_HandleCallback_TransportFailure(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	flow := newFlow(t, fixture, newClientSource(), errExchanger{err: errors.New("network unreachable")}, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)}); err == nil {
		t.Fatalf("HandleCallback: got nil error for a transport failure, want one")
	}
}

func TestFlow_HandleCallback_ClientDeregisteredBetweenBeginAndCallback(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	idp := newFakeIdP()
	clients := newSwappableClientSource(oidc.NewStaticClientSource().WithClient(validClient()))
	flow := newFlow(t, fixture, clients, idp, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	// Simulate the registration disappearing (e.g. deprovisioned) between
	// BeginAuthorization and the callback landing: HandleCallback must
	// re-check the client registration, not trust that BeginAuthorization
	// already found one.
	clients.swap(oidc.NewStaticClientSource())

	if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)}); !errors.Is(err, oidc.ErrClientNotRegistered) {
		t.Fatalf("HandleCallback error = %v, want ErrClientNotRegistered", err)
	}
}

func TestFlow_HandleCallback_RefusesClientIDChangedBetweenBeginAndCallback(t *testing.T) {
	t.Parallel()
	fixture, _, _ := rsaFixture(t)
	clients := newSwappableClientSource(oidc.NewStaticClientSource().WithClient(validClient()))
	flow := newFlow(t, fixture, clients, newFakeIdP(), nil)
	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	changed := validClient()
	changed.ClientID = "attacker-client"
	clients.swap(oidc.NewStaticClientSource().WithClient(changed))
	if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)}); !errors.Is(err, oidc.ErrClientNotRegistered) {
		t.Fatalf("HandleCallback with changed client id = %v, want ErrClientNotRegistered", err)
	}
}
