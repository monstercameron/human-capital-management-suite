package application

import (
	"errors"
	"testing"
	"time"

	paymethoddomain "github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedPayMethodInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(values.NewInstant(servedPayMethodTime()))
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func servedPayMethodTime() time.Time {
	return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
}

func servedPayMethodDestination(t *testing.T) paymethoddomain.Destination {
	t.Helper()
	destination, err := paymethoddomain.NewDestination(paymethoddomain.Destination{
		DestinationID:      "destination-served",
		WorkerRef:          "worker-served",
		Rail:               paymethoddomain.RailACH,
		Risk:               paymethoddomain.RiskMedium,
		GovernedRef:        "vault-token:served",
		DisplayHint:        "****1234",
		Currency:           "USD",
		CountryCode:        "US",
		Verification:       paymethoddomain.VerificationVerified,
		VerificationDigest: "sha256:served-verification",
		Effective:          servedPayMethodInterval(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

// TestTodo_PAYMETHOD_001_Served proves classified destinations and split
// validation are reachable from the application dependency closure.
func TestTodo_PAYMETHOD_001_Served(t *testing.T) {
	surface := NewServedPayMethodSurface()
	if surface.Version == nil || surface.NewDestination == nil || surface.NewSplitPlan == nil {
		t.Fatal("served payment-method surface is incomplete")
	}
	if surface.Version() <= 0 {
		t.Fatal("payment-method schema version must be positive")
	}
	destination := servedPayMethodDestination(t)
	if _, err := surface.NewDestination(destination); err != nil {
		t.Fatalf("served destination: %v", err)
	}
	destination.AccountNumber = "4111111111111111"
	if _, err := surface.NewDestination(destination); !errors.Is(err, paymethoddomain.ErrRawBankDetailProhibited) {
		t.Fatalf("raw destination error = %v, want plaintext refusal", err)
	}
}

// TestTodo_PAYMETHOD_002_Served proves the authorization gate is reachable
// through the same application boundary as the direct-deposit service.
func TestTodo_PAYMETHOD_002_Served(t *testing.T) {
	surface := NewServedPayMethodSurface()
	if surface.AuthorizeChange == nil || surface.NewChangeService == nil || surface.AuthorizeAndActivate == nil {
		t.Fatal("served payment-method authorization surface is incomplete")
	}
	if _, err := surface.AuthorizeChange(paymethoddomain.ChangeAuthorizationRequest{}); !errors.Is(err, paymethoddomain.ErrInvalidAuthorization) {
		t.Fatalf("malformed authorization error = %v, want fail-closed refusal", err)
	}
}

// TestTodo_PAYMETHOD_003_Served proves prenote and settlement observations
// remain external, validated records at the serving boundary.
func TestTodo_PAYMETHOD_003_Served(t *testing.T) {
	surface := NewServedPayMethodSurface()
	if surface.RecordPrenoteObservation == nil || surface.RecordSettlementSubmission == nil || surface.ReconcileSettlement == nil {
		t.Fatal("served payment-method reconciliation surface is incomplete")
	}
	if _, err := surface.RecordSettlementSubmission(paymethoddomain.SettlementSubmission{}, nil); !errors.Is(err, paymethoddomain.ErrInvalidReconciliation) {
		t.Fatalf("malformed settlement submission error = %v, want observation refusal", err)
	}
}

func TestServedPayMethodNilApplication(t *testing.T) {
	var app *App
	if surface := app.PayMethod(); surface.Version != nil || surface.AuthorizeChange != nil || surface.ReconcileSettlement != nil {
		t.Fatal("nil application exposed payment-method capabilities")
	}
}
