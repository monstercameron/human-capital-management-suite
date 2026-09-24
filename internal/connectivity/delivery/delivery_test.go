package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

var deliveryAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	claims        int
	observations  []delivery.Observation
	duplicate     bool
	cancelOnClaim context.CancelFunc
}

func TestGovernedRegistryFencesRunnerReplay(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{}
	registry := idempotency.NewRegistry()
	runner := delivery.Runner{Store: store, Transport: transport, Idempotency: registry, Clock: func() time.Time { return deliveryAt }}
	first, err := runner.Deliver(context.Background(), validEnvelope())
	if err != nil || first.State != delivery.StateSubmitted || transport.calls != 1 {
		t.Fatalf("first delivery = %+v, %v; calls=%d", first, err, transport.calls)
	}
	replay, err := runner.Deliver(context.Background(), validEnvelope())
	if err != nil || replay.State != delivery.StateAlreadyObserved || replay.ProviderRef != "provider-1" || transport.calls != 1 || store.claims != 2 {
		t.Fatalf("registry replay = %+v, %v; calls=%d claims=%d", replay, err, transport.calls, store.claims)
	}
}

func (s *fakeStore) Claim(_ context.Context, _ delivery.Envelope, attempt int) (delivery.Claim, error) {
	s.claims++
	if s.cancelOnClaim != nil {
		s.cancelOnClaim()
	}
	return delivery.Claim{AttemptID: "attempt-" + string(rune('0'+attempt)), Attempt: attempt, AlreadyObserved: s.duplicate}, nil
}
func (s *fakeStore) Observe(_ context.Context, _ delivery.Envelope, _ delivery.Claim, observation delivery.Observation) error {
	s.observations = append(s.observations, observation)
	return nil
}

type fakeTransport struct {
	calls int
	err   error
}

type failOnceTransport struct{ calls int }

func (t *failOnceTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	t.calls++
	if t.calls == 1 {
		return delivery.ProviderResult{}, &delivery.ProviderError{Err: errors.New("temporary"), Retryable: true}
	}
	return delivery.ProviderResult{ProviderRef: "provider-recovered"}, nil
}

type fakeRetryAdmission struct {
	calls      int
	err        error
	attempts   []int
	identities []delivery.RetryIdentity
}

func (a *fakeRetryAdmission) AdmitRetry(_ context.Context, identity delivery.RetryIdentity) error {
	a.calls++
	a.attempts = append(a.attempts, identity.NextAttempt)
	a.identities = append(a.identities, identity)
	return a.err
}

func (t *fakeTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	t.calls++
	return delivery.ProviderResult{ProviderRef: "provider-1"}, t.err
}

func validEnvelope() delivery.Envelope {
	return delivery.Envelope{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "recipient-1", EndpointRef: "endpoint-1", TemplateRef: "template-1", ParametersRef: "params-1", Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "intent-1:endpoint-1", CorrelationID: "correlation-1", ContentDigest: "sha256:abc", ExpiresAt: deliveryAt.Add(time.Hour)}
}

func TestGovernedRegistryAllowsDistinctAdmittedRetryAttempt(t *testing.T) {
	store := &fakeStore{}
	transport := &failOnceTransport{}
	runner := delivery.Runner{Store: store, Transport: transport, Idempotency: idempotency.NewRegistry(), Clock: func() time.Time { return deliveryAt }, MaxAttempts: 2}
	got, err := runner.Deliver(context.Background(), validEnvelope())
	if err != nil || got.State != delivery.StateSubmitted || got.ProviderRef != "provider-recovered" || transport.calls != 2 || len(store.observations) != 2 {
		t.Fatalf("retry with governed registry = %+v, %v; calls=%d observations=%d", got, err, transport.calls, len(store.observations))
	}
}

func TestTodo_SVC_010(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{}
	runner := delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }}
	observation, err := runner.Deliver(context.Background(), validEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != delivery.StateSubmitted || transport.calls != 1 || len(store.observations) != 1 {
		t.Fatalf("observation=%+v calls=%d records=%d", observation, transport.calls, len(store.observations))
	}
}

func TestTodo_SVC_010_Integration(t *testing.T) {
	store := &fakeStore{duplicate: true}
	transport := &fakeTransport{}
	observation, err := (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }}).Deliver(context.Background(), validEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != delivery.StateAlreadyObserved || transport.calls != 0 {
		t.Fatalf("duplicate observation=%+v provider calls=%d", observation, transport.calls)
	}
}

func TestTodo_SVC_010_Fault(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{err: &delivery.ProviderError{Err: errors.New("provider unavailable"), Retryable: true}}
	observation, err := (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 2}).Deliver(context.Background(), validEnvelope())
	if err == nil || observation.State != delivery.StateFailed || transport.calls != 2 || len(store.observations) != 2 {
		t.Fatalf("fault observation=%+v err=%v calls=%d records=%d", observation, err, transport.calls, len(store.observations))
	}
	transport.err = &delivery.ProviderError{Err: errors.New("accepted but response lost"), Ambiguous: true}
	transport.calls = 0
	store.observations = nil
	observation, err = (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 3}).Deliver(context.Background(), validEnvelope())
	if err == nil || observation.State != delivery.StateReviewRequired || transport.calls != 1 {
		t.Fatalf("ambiguous observation=%+v err=%v calls=%d", observation, err, transport.calls)
	}
}

func TestTodo_SVC_010_Security(t *testing.T) {
	envelope := validEnvelope()
	envelope.ContentRef = ""
	if err := envelope.Validate(deliveryAt); err != nil {
		t.Fatal(err)
	}
	// The type itself has no rendered payload, destination address or provider credential.
	if _, err := (delivery.Runner{Store: &fakeStore{}, Transport: &fakeTransport{}, Clock: func() time.Time { return deliveryAt }}).Deliver(context.Background(), delivery.Envelope{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "recipient-1", EndpointRef: "endpoint-1", Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "k", CorrelationID: "c", ContentDigest: "d", ExpiresAt: deliveryAt.Add(time.Hour)}); err == nil {
		t.Fatal("envelope without content/template accepted")
	}
}

func TestDeliveryRetryAdmissionDeniedStopsProviderCall(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{err: &delivery.ProviderError{Err: errors.New("temporary"), Retryable: true}}
	gate := &fakeRetryAdmission{err: errors.New("budget exhausted")}
	obs, err := (delivery.Runner{Store: store, Transport: transport, RetryAdmission: gate, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 3}).Deliver(context.Background(), validEnvelope())
	if !errors.Is(err, delivery.ErrRetryAdmission) || transport.calls != 1 || gate.calls != 1 || store.claims != 1 || len(store.observations) != 1 || obs.State != delivery.StateFailed {
		t.Fatalf("obs=%+v err=%v calls=%d gate=%d", obs, err, transport.calls, gate.calls)
	}
}

func TestDeliveryRetryAdmissionSkipsAmbiguousAndFinal(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{err: &delivery.ProviderError{Err: errors.New("unknown"), Ambiguous: true}}
	gate := &fakeRetryAdmission{}
	if _, err := (delivery.Runner{Store: store, Transport: transport, RetryAdmission: gate, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 3}).Deliver(context.Background(), validEnvelope()); err == nil || gate.calls != 0 || transport.calls != 1 {
		t.Fatalf("ambiguous err=%v gate=%d calls=%d", err, gate.calls, transport.calls)
	}
	transport.err = &delivery.ProviderError{Err: errors.New("temporary"), Retryable: true}
	transport.calls = 0
	gate.calls = 0
	if _, err := (delivery.Runner{Store: &fakeStore{}, Transport: transport, RetryAdmission: gate, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 1}).Deliver(context.Background(), validEnvelope()); err == nil || gate.calls != 0 || transport.calls != 1 {
		t.Fatalf("final err=%v gate=%d calls=%d", err, gate.calls, transport.calls)
	}
}

func TestDeliveryRetryAdmissionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := &fakeTransport{err: &delivery.ProviderError{Err: errors.New("temporary"), Retryable: true}}
	gate := &fakeRetryAdmission{}
	_, err := (delivery.Runner{Store: &fakeStore{}, Transport: transport, RetryAdmission: gate, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 2}).Deliver(ctx, validEnvelope())
	if !errors.Is(err, context.Canceled) || transport.calls != 0 || gate.calls != 0 {
		t.Fatalf("err=%v transport=%d gate=%d", err, transport.calls, gate.calls)
	}
}

func TestDeliveryCancellationAfterClaimNeverCallsProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeStore{cancelOnClaim: cancel}
	transport := &fakeTransport{}
	_, err := (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }}).Deliver(ctx, validEnvelope())
	if !errors.Is(err, context.Canceled) || store.claims != 1 || transport.calls != 0 || len(store.observations) != 0 {
		t.Fatalf("err=%v claims=%d transport=%d observations=%d", err, store.claims, transport.calls, len(store.observations))
	}
}

func TestDeliveryRetryAdmissionPreservesCauseAndExactIdentityAcrossCalls(t *testing.T) {
	cause := errors.New("budget exhausted")
	gate := &fakeRetryAdmission{err: cause}
	runner := delivery.Runner{Store: &fakeStore{}, Transport: &fakeTransport{err: &delivery.ProviderError{Err: errors.New("temporary"), Retryable: true}}, RetryAdmission: gate, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 2}
	for range 2 {
		_, err := runner.Deliver(context.Background(), validEnvelope())
		if !errors.Is(err, delivery.ErrRetryAdmission) || !errors.Is(err, cause) {
			t.Fatalf("admission error lost cause: %v", err)
		}
	}
	if gate.calls != 2 || len(gate.identities) != 2 {
		t.Fatalf("gate calls/identities=%d/%d", gate.calls, len(gate.identities))
	}
	for _, identity := range gate.identities {
		if identity.TenantID != "tenant-1" || identity.IntentID != "intent-1" || identity.IdempotencyKey != "intent-1:endpoint-1" || identity.FailedAttemptID != "attempt-1" || identity.FailedAttempt != 1 || identity.NextAttempt != 2 {
			t.Fatalf("retry identity=%+v", identity)
		}
	}
}
