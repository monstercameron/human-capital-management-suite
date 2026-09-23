package hcmctl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
)

func mintFlagsForTest() globalFlags {
	return globalFlags{
		addr:        "127.0.0.1:0",
		mint:        true,
		mintProfile: devprofile.Name,
		signingKey:  "01234567890123456789012345678901",
		issuer:      "test-issuer",
		audience:    "test-audience",
		tenant:      "tenant-a",
		subject:     "operator-1",
		ttl:         15 * time.Minute,
	}
}

func mintVerifierForTest(t *testing.T, g globalFlags) *trust.HMACVerifier {
	t.Helper()
	v, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(g.signingKey),
		Issuer:   g.issuer,
		Audience: g.audience,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_RBAC_RT_010_Mint proves the operator tool no longer mints
// authority: -mint issues an identity-only development credential carrying
// subject, tenant, session and substantial assurance and nothing else, and
// development minting is refused outside the local development profile and
// against a non-loopback cell.
func TestTodo_RBAC_RT_010_Mint(t *testing.T) {
	t.Run("minted credential is identity only", func(t *testing.T) {
		g := mintFlagsForTest()
		token, err := mintToken(g)
		if err != nil {
			t.Fatalf("mintToken: %v", err)
		}
		p, err := mintVerifierForTest(t, g).VerifyIdentity(context.Background(), trust.Credential{Token: token})
		if err != nil {
			t.Fatalf("minted credential fails identity verification: %v", err)
		}
		if p.Subject() != "operator-1" || string(p.Tenant()) != "tenant-a" || p.SessionRef() != "hcmctl-jit-session" {
			t.Fatalf("minted identity = %s", p)
		}
		if p.Assurance() != trust.AssuranceSubstantial {
			t.Fatalf("minted assurance = %v, want substantial (a development mint never achieves high)", p.Assurance())
		}
		if len(p.Roles()) != 0 || len(p.Purposes()) != 0 || len(p.AuthorityRefs()) != 0 ||
			len(p.DelegationRefs()) != 0 || p.OrganizationScopeID() != "" {
			t.Fatalf("minted credential carries authority: roles=%v purposes=%v org=%q authority=%v delegations=%v",
				p.Roles(), p.Purposes(), p.OrganizationScopeID(), p.AuthorityRefs(), p.DelegationRefs())
		}
	})

	t.Run("minting is refused outside the local development profile", func(t *testing.T) {
		for _, profile := range []string{"", "standard", "production"} {
			g := mintFlagsForTest()
			g.mintProfile = profile
			if _, err := mintToken(g); err == nil || !strings.Contains(err.Error(), devprofile.Name) {
				t.Fatalf("mintToken with profile %q = %v, want a refusal naming %q", profile, err, devprofile.Name)
			}
		}
	})

	t.Run("minting is refused against a non-loopback cell", func(t *testing.T) {
		for _, addr := range []string{"0.0.0.0:8443", "cell.example:8443", "10.0.0.9:8443"} {
			g := mintFlagsForTest()
			g.addr = addr
			if _, err := mintToken(g); err == nil || !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("mintToken with addr %q = %v, want a loopback refusal", addr, err)
			}
		}
	})

	t.Run("minting still requires issuer, key, tenant and subject", func(t *testing.T) {
		g := mintFlagsForTest()
		g.signingKey = ""
		if _, err := mintToken(g); err == nil {
			t.Fatal("mintToken without a signing key succeeded")
		}
		g = mintFlagsForTest()
		g.subject = ""
		if _, err := mintToken(g); err == nil {
			t.Fatal("mintToken without a subject succeeded")
		}
	})
}
