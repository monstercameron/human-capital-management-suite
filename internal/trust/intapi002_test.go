package trust

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

type intapi002Fixture struct {
	issuer   *machine.Issuer
	verifier *machine.Verifier
	served   *ServedVerifier
	source   *MemoryRevocationSource
	now      time.Time
}

func newIntapi002Fixture(t *testing.T) *intapi002Fixture {
	t.Helper()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	key, err := machine.GenerateServerKey("srv-2", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := machine.NewVerifier([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	source := NewMemoryRevocationSource(func() time.Time { return now })
	served := &ServedVerifier{
		Machine:     verifier,
		Request:     machine.VerifyRequest{Issuer: "https://cell.example", Audience: []string{"hcm-next-api"}},
		Revocations: source,
	}
	return &intapi002Fixture{issuer: issuer, verifier: verifier, served: served, source: source, now: now}
}

var intapi002Seq atomic.Int64

func (f *intapi002Fixture) mint(t *testing.T, client, session string) string {
	t.Helper()
	token, err := f.issuer.Issue(machine.IssueRequest{
		Issuer: "https://cell.example", Audience: []string{"hcm-next-api"},
		Subject: client, Client: client, Tenant: "tenant-a",
		Session: session, Assurance: "substantial",
		TokenID: fmt.Sprintf("tok-%d", intapi002Seq.Add(1)), Lifetime: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// countingSource wraps a RevocationSource and counts consultations, so
// tests prove a forgery never reaches revocation state.
type countingSource struct {
	inner RevocationSource
	calls atomic.Int64
}

func (c *countingSource) CheckRevocation(ctx context.Context, q RevocationQuery) error {
	c.calls.Add(1)
	return c.inner.CheckRevocation(ctx, q)
}

// TestTodo_INTAPI_002 is the PRIMARY: a fresh token admits once, its
// replay is refused, and revoked clients and sessions fail closed even
// for never-before-seen tokens.
func TestTodo_INTAPI_002(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh token admits exactly once", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		token := f.mint(t, "client-a", "mcs-1")
		if _, err := f.served.Verify(ctx, Credential{Token: token}); err != nil {
			t.Fatalf("first use: %v", err)
		}
		if _, err := f.served.Verify(ctx, Credential{Token: token}); !errors.Is(err, ErrTokenReplayed) {
			t.Fatalf("replay = %v, want ErrTokenReplayed", err)
		}
	})

	t.Run("revoked client fails closed before recording", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		f.source.RevokeClient("client-b")
		token := f.mint(t, "client-b", "mcs-2")
		if _, err := f.served.Verify(ctx, Credential{Token: token}); !errors.Is(err, ErrClientRevoked) {
			t.Fatalf("revoked client = %v, want ErrClientRevoked", err)
		}
	})

	t.Run("revoked session fails closed", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		token := f.mint(t, "client-c", "mcs-3")
		f.source.RevokeSession("mcs-3")
		if _, err := f.served.Verify(ctx, Credential{Token: token}); !errors.Is(err, ErrSessionRevoked) {
			t.Fatalf("revoked session = %v, want ErrSessionRevoked", err)
		}
	})

	t.Run("revocation after first use ends the token", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		first := f.mint(t, "client-d", "mcs-4")
		if _, err := f.served.Verify(ctx, Credential{Token: first}); err != nil {
			t.Fatalf("first use: %v", err)
		}
		second := f.mint(t, "client-d", "mcs-4")
		f.source.RevokeClient("client-d")
		if _, err := f.served.Verify(ctx, Credential{Token: second}); !errors.Is(err, ErrClientRevoked) {
			t.Fatalf("post-revocation use = %v, want ErrClientRevoked", err)
		}
	})
}

// TestTodo_INTAPI_002_Security proves a forgery never consults revocation
// state: signature, issuer, audience and lifetime are checked first, so a
// probe cannot learn which identifiers were seen.
func TestTodo_INTAPI_002_Security(t *testing.T) {
	ctx := context.Background()
	f := newIntapi002Fixture(t)
	counting := &countingSource{inner: f.source}
	f.served.Revocations = counting

	for _, token := range []string{
		"not-a-token",
		f.mint(t, "client-a", "mcs-1")[:20] + "AA",
	} {
		if _, err := f.served.Verify(ctx, Credential{Token: token}); err == nil {
			t.Fatalf("forgery %q admitted", token)
		}
	}
	if got := counting.calls.Load(); got != 0 {
		t.Fatalf("forgeries consulted the source %d times, want 0", got)
	}
	if _, err := f.served.Verify(ctx, Credential{Token: f.mint(t, "client-a", "mcs-9")}); err != nil {
		t.Fatalf("fresh token after forgeries: %v", err)
	}
	if got := counting.calls.Load(); got != 1 {
		t.Fatalf("source consultations = %d, want 1", got)
	}

	t.Run("development token above the dev maximum is refused", func(t *testing.T) {
		now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
		dev, err := NewHMACVerifier(HMACVerifierConfig{
			Key: []byte("01234567890123456789012345678901"), Issuer: "dev", Audience: "dev",
			Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatal(err)
		}
		mint := func(lifetime time.Duration) string {
			token, err := dev.Issue(Claims{
				Issuer: "dev", Audience: "dev", Subject: "dev-1", SubjectKind: "human",
				Tenant: "tenant-a", AuthenticationMethod: "bearer_token", Assurance: "substantial",
				SessionRef: "s-1", IssuedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(lifetime).Unix(),
			})
			if err != nil {
				t.Fatal(err)
			}
			return token
		}
		if _, err := dev.Verify(ctx, Credential{Token: mint(8 * time.Hour)}); err != nil {
			t.Fatalf("8h development token: %v", err)
		}
		if _, err := dev.Verify(ctx, Credential{Token: mint(48 * time.Hour)}); !errors.Is(err, ErrExpiredCredential) {
			t.Fatalf("48h development token = %v, want expiry refusal", err)
		}
	})
}

// TestTodo_INTAPI_002_Property proves the exactly-once property under
// concurrency and the never-after-revocation property in sequence: over N
// racing first presentations exactly one admits, and no presentation after
// revocation admits no matter how many race.
func TestTodo_INTAPI_002_Property(t *testing.T) {
	ctx := context.Background()

	t.Run("concurrent first presentations admit exactly one", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		token := f.mint(t, "client-p", "mcs-p")
		const racers = 16
		var admitted atomic.Int64
		var replayed atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.served.Verify(ctx, Credential{Token: token})
				switch {
				case err == nil:
					admitted.Add(1)
				case errors.Is(err, ErrTokenReplayed):
					replayed.Add(1)
				default:
					t.Errorf("unexpected refusal: %v", err)
				}
			}()
		}
		wg.Wait()
		if admitted.Load() != 1 || replayed.Load() != racers-1 {
			t.Fatalf("admitted=%d replayed=%d, want 1 and %d", admitted.Load(), replayed.Load(), racers-1)
		}
	})

	t.Run("no presentation admits after revocation", func(t *testing.T) {
		f := newIntapi002Fixture(t)
		f.source.RevokeClient("client-q")
		const attempts = 8
		for i := 0; i < attempts; i++ {
			token := f.mint(t, "client-q", "mcs-q")
			if _, err := f.served.Verify(ctx, Credential{Token: token}); !errors.Is(err, ErrClientRevoked) {
				t.Fatalf("attempt %d = %v, want ErrClientRevoked", i, err)
			}
		}
	})
}
