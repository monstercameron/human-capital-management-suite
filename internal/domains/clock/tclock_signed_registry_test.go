package clock

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func signedRegistryPayload() SignedRegistryPayload {
	return SignedRegistryPayload{Schema: ProfileRegistryVersion, Revision: "r1", ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ValidUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Profiles: []SignedProfile{{Class: SourceDevicePushHTTP, Transport: "HTTPS_PUSH", Authentication: "DEVICE_CERT", TrustCeiling: TrustCeilingMedium, PermittedMethods: []IdentificationMethod{MethodBadge}, OfflineLimit: "24h", ClockTrustRequired: true, FirstPartner: "ZKTeco PUSH/ADMS", Version: "v1"}}}
}

func TestVerifySignedProfileRegistry_acceptsPinnedEd25519Payload(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data, err := SignSignedRegistry(signedRegistryPayload(), priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifySignedProfileRegistry(data, pub, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != "r1" || got.Registry.Profiles[0].FirstPartner != "ZKTeco PUSH/ADMS" {
		t.Fatalf("unexpected verified registry: %+v", got)
	}
	got.Registry.Profiles[0].PermittedMethods[0] = MethodPIN
	again, err := VerifySignedProfileRegistry(data, pub, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "r1")
	if err != nil || again.Registry.Profiles[0].PermittedMethods[0] != MethodBadge {
		t.Fatal("verified registry was not cloned")
	}
}

func TestVerifySignedProfileRegistry_rejectsWrongKeyRevisionAndTime(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data, _ := SignSignedRegistry(signedRegistryPayload(), priv, "test")
	cases := []struct {
		name string
		key  ed25519.PublicKey
		now  time.Time
		pin  string
		want error
	}{
		{"wrong key", make(ed25519.PublicKey, ed25519.PublicKeySize), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "r1", ErrSignedRegistrySignature},
		{"wrong revision", pub, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "r2", ErrSignedRegistryRevision},
		{"expired", pub, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "r1", ErrSignedRegistryValidity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := VerifySignedProfileRegistry(data, tc.key, tc.now, tc.pin)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
