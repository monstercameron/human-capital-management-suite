package delivery

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// validEnvelope passes Validate at now; each test breaks one field.
func validEnvelope() Envelope {
	return Envelope{
		TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "contact-1",
		EndpointRef: "endpoint-1", ContentRef: "content-1", Purpose: "NOTICE",
		Classification: "INTERNAL", Channel: ChannelEmail, IdempotencyKey: "idem-1",
		CorrelationID: "corr-1", ContentDigest: "sha256:abc", ExpiresAt: now.Add(time.Hour),
	}
}

func TestValidateAcceptsAWellFormedEnvelope(t *testing.T) {
	if err := validEnvelope().Validate(now); err != nil {
		t.Fatalf("Validate(valid) = %v, want nil", err)
	}
	// A template reference satisfies the content requirement on its own.
	e := validEnvelope()
	e.ContentRef, e.TemplateRef = "", "template-1"
	if err := e.Validate(now); err != nil {
		t.Fatalf("Validate with template_ref = %v, want nil", err)
	}
}

func TestValidateRefusesEachBrokenRule(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Envelope)
		now    time.Time
	}{
		"blank tenant":           {func(e *Envelope) { e.TenantID = "   " }, now},
		"blank digest":           {func(e *Envelope) { e.ContentDigest = "" }, now},
		"no content or template": {func(e *Envelope) { e.ContentRef = "" }, now},
		"unknown channel":        {func(e *Envelope) { e.Channel = Channel("FAX") }, now},
		"no expiry":              {func(e *Envelope) { e.ExpiresAt = time.Time{} }, now},
		"already expired":        {func(e *Envelope) { e.ExpiresAt = now }, now},
	}
	for name, c := range cases {
		e := validEnvelope()
		c.mutate(&e)
		if err := e.Validate(c.now); !errors.Is(err, ErrInvalidEnvelope) {
			t.Errorf("%s: Validate = %v, want ErrInvalidEnvelope", name, err)
		}
	}
}

func TestProviderErrorUnwrapsItsCause(t *testing.T) {
	cause := errors.New("connection reset")
	pe := &ProviderError{Err: fmt.Errorf("send: %w", cause), Ambiguous: true}
	if !errors.Is(pe, cause) {
		t.Fatal("ProviderError does not unwrap to its cause")
	}
	if pe.Error() != "send: connection reset" {
		t.Fatalf("Error() = %q, want the cause message", pe.Error())
	}
}

func TestProviderErrorNilAndEmptyAreSafe(t *testing.T) {
	var nilErr *ProviderError
	if nilErr.Unwrap() != nil {
		t.Fatal("nil ProviderError unwrapped to a non-nil cause")
	}
	if nilErr.Error() != "delivery: provider error" {
		t.Fatalf("nil Error() = %q", nilErr.Error())
	}
	if (&ProviderError{}).Error() != "delivery: provider error" {
		t.Fatal("empty ProviderError did not report the generic message")
	}
}

func TestChannelAndStateValuesAreWireStable(t *testing.T) {
	if ChannelInbox != "INBOX" || ChannelEmail != "EMAIL" || StateReviewRequired != "REVIEW_REQUIRED" || StateAlreadyObserved != "ALREADY_OBSERVED" {
		t.Fatal("a channel or state constant changed its wire value")
	}
}
