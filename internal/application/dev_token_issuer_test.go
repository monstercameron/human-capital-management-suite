package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestDevTokenIssuerOfFindsTheServedVerifiersDevFallback guards the local-dev
// sign-in page: the INTAPI-001 ServedVerifier wraps the HMAC verifier as Dev
// and does not expose Issue itself, and every dev persona vanished when the
// persona composers only type-asserted the outer verifier.
func TestDevTokenIssuerOfFindsTheServedVerifiersDevFallback(t *testing.T) {
	hmac, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte("a-local-development-key-at-least-32-bytes"), Issuer: "https://issuer.test", Audience: "hcm-next-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if issuer, ok := devTokenIssuerOf(hmac); !ok || issuer == nil {
		t.Fatal("a bare HMAC verifier must be its own development issuer")
	}
	if issuer, ok := devTokenIssuerOf(&trust.ServedVerifier{Dev: hmac}); !ok || issuer != developmentTokenIssuer(hmac) {
		t.Fatal("a served verifier with a Dev fallback must yield that fallback as the development issuer")
	}
	if _, ok := devTokenIssuerOf(&trust.ServedVerifier{}); ok {
		t.Fatal("a served verifier without Dev (every non-local profile) must have no development issuer")
	}
	if issuer, ok := devTokenIssuerOf(&machineFallbackVerifier{machine: &trust.ServedVerifier{}, fallback: hmac}); !ok || issuer != developmentTokenIssuer(hmac) {
		t.Fatal("the local-dev machine+HMAC verifier must yield its HMAC fallback as the development issuer")
	}
}
