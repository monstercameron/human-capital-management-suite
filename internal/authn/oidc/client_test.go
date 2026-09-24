package oidc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// validClient returns a syntactically valid registration for tenantAcme /
// issuerAcme, ready for a test to mutate one field before use.
func validClient() oidc.ClientRegistration {
	return oidc.ClientRegistration{
		Tenant:                tenantAcme,
		IssuerURL:             issuerAcme,
		ClientID:              clientIDAcme,
		RedirectURI:           redirectURI,
		AuthorizationEndpoint: authEndpoint,
		TokenEndpoint:         tokenEndpoint,
		Scopes:                []string{"openid"},
	}
}

func TestStaticClientSource_LookupClient(t *testing.T) {
	t.Parallel()
	src := oidc.NewStaticClientSource().WithClient(validClient())

	got, found, err := src.LookupClient(tenantAcme, issuerAcme)
	if err != nil {
		t.Fatalf("LookupClient: %v", err)
	}
	if !found {
		t.Fatalf("LookupClient: found = false, want true")
	}
	if got.ClientID != clientIDAcme {
		t.Fatalf("LookupClient client id = %q, want %q", got.ClientID, clientIDAcme)
	}

	_, found, err = src.LookupClient(tenantAcme, "https://unregistered.invalid/")
	if err != nil {
		t.Fatalf("LookupClient (unregistered issuer): %v", err)
	}
	if found {
		t.Fatalf("LookupClient (unregistered issuer): found = true, want false")
	}
}

func TestFlow_BeginAuthorization_RefusesUnregisteredClient(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	empty := oidc.NewStaticClientSource() // no registrations at all
	flow := newFlow(t, fixture, empty, newFakeIdP(), nil)

	_, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if !errors.Is(err, oidc.ErrClientNotRegistered) {
		t.Fatalf("BeginAuthorization error = %v, want ErrClientNotRegistered", err)
	}
}

func TestFlow_BeginAuthorization_RefusesInvalidClientRegistration(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	bad := validClient()
	bad.Scopes = nil // missing "openid"
	clients := oidc.NewStaticClientSource().WithClient(bad)
	flow := newFlow(t, fixture, clients, newFakeIdP(), nil)

	_, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if !errors.Is(err, oidc.ErrInvalidClientRegistration) {
		t.Fatalf("BeginAuthorization error = %v, want ErrInvalidClientRegistration", err)
	}
}

func TestFlow_BeginAuthorization_RefusesClientIDThatDiffersFromIssuerAudience(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	client := validClient()
	client.ClientID = "different-oauth-client"
	flow := newFlow(t, fixture, oidc.NewStaticClientSource().WithClient(client), newFakeIdP(), nil)
	if _, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime); !errors.Is(err, oidc.ErrWrongAudience) {
		t.Fatalf("BeginAuthorization mismatched client id error = %v, want ErrWrongAudience", err)
	}
}
