package operation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

func TestOperation_PublicErrorValuesRemainUnambiguous(t *testing.T) {
	var provider *operation.ProviderError
	if provider.Error() != "<nil>" || provider.Unwrap() != nil {
		t.Fatalf("nil ProviderError = %q / %v", provider.Error(), provider.Unwrap())
	}
	provider = &operation.ProviderError{Result: operation.ResponseFailure, Cause: operation.ErrInvalid}
	if provider.Error() == "" || !errors.Is(provider, operation.ErrInvalid) {
		t.Fatalf("provider error = %q", provider.Error())
	}
	var rejection *operation.CredentialRejection
	if rejection.Error() != "<nil>" || rejection.Unwrap() != nil || !rejection.Is(operation.ErrCredentialRejected) {
		t.Fatalf("nil CredentialRejection = %q / %v", rejection.Error(), rejection.Unwrap())
	}
	rejection = &operation.CredentialRejection{Code: operation.CredentialRejectedCode, Field: "expiry", State: operation.StateLeased, Version: 1, Cause: operation.ErrLeaseExpired}
	if !errors.Is(rejection, operation.ErrCredentialRejected) || !errors.Is(rejection, operation.ErrLeaseExpired) {
		t.Fatalf("credential rejection did not preserve sentinels: %v", rejection)
	}
}

func TestOperation_ContextAndTenantValidationFailClosed(t *testing.T) {
	j := newJournal()
	//lint:ignore SA1012 deliberate nil context: this hardening test proves a nil context fails closed with ErrInvalid. owner=platform-connectivity expires=2027-03-24
	if _, err := j.Get(nil, "tenant-promotion", testUUID()); !errors.Is(err, operation.ErrInvalid) {
		t.Fatalf("nil context get = %v", err)
	}
	if _, err := j.Get(context.Background(), "", testUUID()); !errors.Is(err, operation.ErrInvalid) {
		t.Fatalf("empty tenant get = %v", err)
	}
	if _, err := j.Queue(context.Background(), "", testUUID()); !errors.Is(err, operation.ErrInvalid) {
		t.Fatalf("empty tenant queue = %v", err)
	}
	if _, err := j.List(context.Background(), ""); !errors.Is(err, operation.ErrInvalid) {
		t.Fatalf("empty tenant list = %v", err)
	}
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "", OperationID: testUUID(), WorkerID: "worker", Duration: time.Minute, Revalidate: confirmed}); !errors.Is(err, operation.ErrInvalid) {
		t.Fatalf("empty tenant lease = %v", err)
	}
	if _, err := j.RecordObservation(context.Background(), operation.Observation{OperationID: testUUID(), ObservationID: testUUID(), ExternalResourceKey: "resource", Verdict: operation.ObservationUnknown, ObservedAt: testNow}); !errors.Is(err, operation.ErrObservationRequired) {
		t.Fatalf("empty tenant observation = %v", err)
	}
}

func testUUID() uuid.UUID { return uuid.MustParse("00000000-0000-0000-0000-000000000001") }
