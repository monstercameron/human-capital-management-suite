package app

import (
	"strings"
	"testing"

	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func TestAttestedAckVerifierAdmitsOnlyUnsignedHRISSignals(t *testing.T) {
	verifier := attestedAckVerifier{source: acknowledgementEventSource}
	if err := verifier.Verify(stepSignal.Signal{Source: acknowledgementEventSource}); err != nil {
		t.Fatalf("Verify(attested HRIS signal) = %v, want nil", err)
	}
}

func TestAttestedAckVerifierRefusesForeignSource(t *testing.T) {
	verifier := attestedAckVerifier{source: acknowledgementEventSource}
	err := verifier.Verify(stepSignal.Signal{Source: "hcmnext.integrations.webhook"})
	if err == nil || !strings.Contains(err.Error(), "not the attested") {
		t.Fatalf("Verify(foreign source) = %v, want attested-source refusal", err)
	}
}

func TestAttestedAckVerifierRefusesSignedSignal(t *testing.T) {
	verifier := attestedAckVerifier{source: acknowledgementEventSource}
	err := verifier.Verify(stepSignal.Signal{Source: acknowledgementEventSource, Signature: []byte("webhook-bytes")})
	if err == nil || !strings.Contains(err.Error(), "no external signature") {
		t.Fatalf("Verify(signed signal) = %v, want external-signature refusal", err)
	}
}
