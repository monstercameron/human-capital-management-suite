package trust

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

func servedMachineKeys(t *testing.T, now time.Time) []machine.ServerKey {
	t.Helper()
	key, err := machine.GenerateServerKey("srv-1", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return []machine.ServerKey{key}
}

// TestServedVerifierComposite proves the INTAPI-001 served verifier admits
// machine tokens as integration identity with no authority, still admits
// HMAC development tokens when the composition supplies the dev verifier,
// and refuses HMAC tokens outright when it does not.
func TestServedVerifierComposite(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	keys := servedMachineKeys(t, now)
	issuer, err := machine.NewIssuer(keys, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	machineVerifier, err := machine.NewVerifier(keys, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	req := machine.VerifyRequest{Issuer: "https://cell.example", Audience: []string{"hcm-next-api"}}
	ctx := context.Background()

	token, err := issuer.Issue(machine.IssueRequest{
		Issuer: "https://cell.example", Audience: []string{"hcm-next-api"},
		Subject: "service-subject-a", Client: "client-a", Tenant: "tenant-a",
		Session: "mcs-1", Assurance: "substantial", TokenID: "tok-1", Lifetime: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("machine token admits as authority-free integration identity", func(t *testing.T) {
		v := &ServedVerifier{Machine: machineVerifier, Request: req}
		p, err := v.Verify(ctx, Credential{Token: token})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if p.Subject() != "service-subject-a" || p.ClientID() != "client-a" || string(p.Tenant()) != "tenant-a" {
			t.Fatalf("principal = %s", p)
		}
		if p.SubjectKind() != SubjectKindIntegration {
			t.Fatalf("kind = %v, want integration", p.SubjectKind())
		}
		if len(p.Roles()) != 0 || len(p.Purposes()) != 0 || len(p.AuthorityRefs()) != 0 || len(p.DelegationRefs()) != 0 {
			t.Fatalf("principal carries authority: %s", p)
		}
	})

	t.Run("hmac is refused without a dev verifier", func(t *testing.T) {
		v := &ServedVerifier{Machine: machineVerifier, Request: req}
		dev, err := NewHMACVerifier(HMACVerifierConfig{
			Key: []byte("01234567890123456789012345678901"), Issuer: "dev", Audience: "dev", Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatal(err)
		}
		hmacToken, err := dev.IssueIdentity(Claims{
			Issuer: "dev", Audience: "dev", Subject: "dev-1", SubjectKind: "human",
			Tenant: "tenant-a", AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "s-1", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(ctx, Credential{Token: hmacToken}); err == nil {
			t.Fatal("HMAC token admitted without a dev verifier")
		}
	})

	t.Run("hmac admits through the dev verifier only", func(t *testing.T) {
		dev, err := NewHMACVerifier(HMACVerifierConfig{
			Key: []byte("01234567890123456789012345678901"), Issuer: "dev", Audience: "dev", Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatal(err)
		}
		v := &ServedVerifier{Machine: machineVerifier, Request: req, Dev: dev}
		hmacToken, err := dev.IssueIdentity(Claims{
			Issuer: "dev", Audience: "dev", Subject: "dev-1", SubjectKind: "human",
			Tenant: "tenant-a", AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "s-1", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		p, err := v.Verify(ctx, Credential{Token: hmacToken})
		if err != nil {
			t.Fatalf("dev HMAC through dev verifier: %v", err)
		}
		if p.Subject() != "dev-1" || p.SubjectKind() != SubjectKindHuman {
			t.Fatalf("principal = %s", p)
		}
		// An authority-bearing HMAC token is refused even with the dev
		// verifier: identity-only semantics apply to both paths.
		wide, err := dev.Issue(Claims{
			Issuer: "dev", Audience: "dev", Subject: "dev-1", SubjectKind: "human",
			Tenant: "tenant-a", Roles: []string{"hcm_admin"},
			AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "s-1", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(ctx, Credential{Token: wide}); !errors.Is(err, ErrCallerSelectedAuthority) {
			t.Fatalf("authority-bearing HMAC = %v, want ErrCallerSelectedAuthority", err)
		}
	})
}
