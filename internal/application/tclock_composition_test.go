package application

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

func TestComposeClock_DisabledIsNilAndDoesNotOpenAnything(t *testing.T) {
	called := false
	runtime, err := ComposeClock(context.Background(), nil, nil, nil, ClockDependencies{
		OpenStore: func(context.Context, ClockRuntimeConfig) (*timestore.Store, error) {
			called = true
			return nil, errors.New("must not be called")
		},
	})
	if err != nil || runtime != nil {
		t.Fatalf("disabled composition = (%v, %v), want (nil, nil)", runtime, err)
	}
	if called {
		t.Fatal("disabled composition opened a store")
	}
}

func TestComposeClock_EnabledFailsClosedBeforeOpeningWithoutAuthority(t *testing.T) {
	cfg := validClockRuntimeConfigForComposition(t)
	called := false
	_, err := ComposeClock(context.Background(), &cfg, &pgxadapter.Pool{}, func() time.Time { return compositionNow }, ClockDependencies{
		OpenStore: func(context.Context, ClockRuntimeConfig) (*timestore.Store, error) {
			called = true
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "authoritative roster") {
		t.Fatalf("error = %v, want roster authority failure", err)
	}
	if called {
		t.Fatal("composition opened a store before validating authority")
	}
}

func TestFixedClockRegistry_RejectsUnknownTenantAndExpiredRegistry(t *testing.T) {
	r := fixedClockRegistry{registry: clockdomain.VerifiedProfileRegistry{
		ValidFrom: compositionNow.Add(-time.Hour), ValidUntil: compositionNow.Add(time.Hour),
	}}
	if _, err := r.Resolve(context.Background(), "", compositionNow); err == nil {
		t.Fatal("empty tenant unexpectedly resolved")
	}
	if _, err := r.Resolve(context.Background(), "tenant", compositionNow.Add(2*time.Hour)); err == nil {
		t.Fatal("expired registry unexpectedly resolved")
	}
}

func TestFixedClockRegistry_ReturnsCopiedProfiles(t *testing.T) {
	profile := clockdomain.IntegrationProfile{
		Class: clockdomain.SourceDevicePushHTTP, Transport: "HTTPS_PUSH", Authentication: "DEVICE_CERT",
		TrustCeiling: clockdomain.TrustCeilingMedium, PermittedMethods: []clockdomain.IdentificationMethod{clockdomain.MethodBadge},
		OfflineLimit: time.Hour, ClockTrustRequired: true, FirstPartner: "partner", Version: "v1",
	}
	registry, err := clockdomain.NewProfileRegistry([]clockdomain.IntegrationProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	source := fixedClockRegistry{registry: clockdomain.VerifiedProfileRegistry{Revision: "r1", ValidFrom: compositionNow.Add(-time.Hour), ValidUntil: compositionNow.Add(time.Hour), Registry: registry}}
	first, err := source.Resolve(context.Background(), "tenant", compositionNow)
	if err != nil {
		t.Fatal(err)
	}
	first.Registry.Profiles[0].PermittedMethods[0] = clockdomain.MethodPIN
	second, err := source.Resolve(context.Background(), "tenant", compositionNow)
	if err != nil {
		t.Fatal(err)
	}
	if second.Registry.Profiles[0].PermittedMethods[0] != clockdomain.MethodBadge {
		t.Fatal("registry source exposed mutable profile state")
	}
}

var compositionNow = time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)

func validClockRuntimeConfigForComposition(t *testing.T) ClockRuntimeConfig {
	t.Helper()
	return ClockRuntimeConfig{
		TimeDatabaseURL: "postgres://clock:clock-secret@127.0.0.1:5432/clock",
		TimeSchema:      "hcmnext_time", CoreDatabaseURL: "postgres://core:core-secret@127.0.0.1:5432/core",
		WorkerTokenKey: []byte(strings.Repeat("worker-key-", 4)), SignedRegistryPath: "registry.json",
		SignedRegistryKey: make([]byte, ed25519.PublicKeySize), SignedRegistryPinnedRevision: "r1",
	}
}
