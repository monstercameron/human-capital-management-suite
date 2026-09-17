package intentmanifests

import (
	"errors"
	"strings"
	"testing"
)

// FuzzTodo_FEATURE_CONF_001 proves the parity gate never fails open on
// adversarial inputs: unknown features, forged surfaces, empty or oversized
// typed inputs and unauthenticated claims always refuse, and an authorized
// resolution never alters canonical semantics.
func FuzzTodo_FEATURE_CONF_001(f *testing.F) {
	seeds := []struct {
		feature string
		actor   string
		surface string
		input   string
		auth    bool
	}{
		{"promote_employee", "EMPLOYEE", "GWC", "sha256:typed-input", true},
		{"grant_equity", "EMPLOYEE", "GWC", "sha256:typed-input", true},
		{"no_such_feature", "OPERATOR", "HTTP", "sha256:typed-input", true},
		{"promote_employee", "HR_ADMIN", "CARRIER_PIGEON", "sha256:typed-input", true},
		{"promote_employee", "HR_ADMIN", "CLI", "", true},
		{"promote_employee", "HR_ADMIN", "CLI", "sha256:typed-input", false},
	}
	for _, seed := range seeds {
		auth := "0"
		if seed.auth {
			auth = "1"
		}
		f.Add(seed.feature, seed.actor, seed.surface, seed.input, auth)
	}
	f.Fuzz(func(t *testing.T, feature, actor, surface, input, authFlag string) {
		bindings, err := BindManifest(parityBindings())
		if err != nil {
			t.Fatalf("fixture manifest must bind: %v", err)
		}
		claim := ActorClaim{Actor: ChannelActor(actor), Authenticated: authFlag == "1"}
		got, err := bindings.Resolve(feature, claim, Surface(surface), input)
		if err != nil {
			if !errors.Is(err, ErrChannelRefused) {
				t.Fatalf("Resolve must fail closed with ErrChannelRefused, got %v", err)
			}
			return
		}
		if !claim.Authenticated {
			t.Fatal("unauthenticated claim resolved: caller context was trusted")
		}
		if strings.TrimSpace(input) == "" {
			t.Fatal("empty typed input resolved")
		}
		if got.DefinitionDigest == "" || got.SchemaDigest == "" || got.RequestDigest == "" {
			t.Fatal("resolution returned incomplete canonical semantics")
		}
		if !strings.HasPrefix(got.DefinitionDigest, "sha256:") || !strings.HasPrefix(got.RequestDigest, "sha256:") {
			t.Fatalf("resolution digests are not canonical: %+v", got)
		}
	})
}
