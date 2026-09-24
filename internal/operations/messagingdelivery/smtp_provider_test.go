package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type smtpTransportFunc func(context.Context, SMTPConfig, string, []string, []byte) error

type allowDomainGate struct{}

func (allowDomainGate) AuthorizeSendingDomain(context.Context, string, string) error { return nil }

type rejectDomainGate struct{}

func (rejectDomainGate) AuthorizeSendingDomain(context.Context, string, string) error {
	return ErrUnverifiedDomain
}

func (f smtpTransportFunc) Send(ctx context.Context, cfg SMTPConfig, from string, recipients []string, message []byte) error {
	return f(ctx, cfg, from, recipients, message)
}

func TestSMTPProviderFailsClosedWhenDomainIsUnverified(t *testing.T) {
	calls := 0
	provider, err := NewSMTPProvider(SMTPConfig{Host: "mail.example.test", From: "no-reply@example.test"}, smtpTransportFunc(func(context.Context, SMTPConfig, string, []string, []byte) error { calls++; return nil }), rejectDomainGate{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Send(context.Background(), Delivery{TenantID: "tenant-1", IdempotencyKey: "key-1", RecipientRef: "person@example.test", Purpose: "notice", Subject: "Notice"})
	if !errors.Is(err, ErrUnverifiedDomain) || calls != 0 {
		t.Fatalf("unverified sender reached SMTP: calls=%d err=%v", calls, err)
	}
}

func TestTodo_REV_011_02(t *testing.T) {
	calls := 0
	var captured []byte
	provider, err := NewSMTPProvider(SMTPConfig{Host: "mail.example.test", From: "no-reply@example.test"}, smtpTransportFunc(func(_ context.Context, _ SMTPConfig, from string, recipients []string, message []byte) error {
		calls++
		if from != "no-reply@example.test" || len(recipients) != 1 || recipients[0] != "person@example.test" {
			t.Fatalf("unexpected envelope from=%q recipients=%v", from, recipients)
		}
		captured = append([]byte(nil), message...)
		return nil
	}), allowDomainGate{})
	if err != nil {
		t.Fatal(err)
	}
	input := testIntent()
	input.RecipientRef = "person@example.test"
	dispatcher, err := NewDispatcher(provider, Policy{ProviderMaximumClassification: "INTERNAL"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempt.State != AcceptedByProvider || result.Attempt.ProviderRef == "" || calls != 1 {
		t.Fatalf("dispatch=%#v calls=%d", result, calls)
	}
	if !strings.Contains(string(captured), "Subject: Approval needed") || !strings.Contains(string(captured), "Open the secure task") || !strings.Contains(string(captured), "Message-ID:") {
		t.Fatalf("message missing expected headers or body: %q", captured)
	}
}

func TestTodo_REV_011_02_Integration(t *testing.T) {
	calls := 0
	messageIDs := []string{}
	provider, err := NewSMTPProvider(SMTPConfig{Host: "mail.example.test", From: "no-reply@example.test"}, smtpTransportFunc(func(_ context.Context, _ SMTPConfig, _ string, _ []string, message []byte) error {
		calls++
		for _, line := range strings.Split(string(message), "\r\n") {
			if strings.HasPrefix(line, "Message-ID: ") {
				messageIDs = append(messageIDs, strings.TrimPrefix(line, "Message-ID: "))
			}
		}
		return nil
	}), allowDomainGate{})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewDispatcher(provider, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	input := testIntent()
	input.RecipientRef = "person@example.test"
	first, err := dispatcher.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := dispatcher.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.Attempt.ID != second.Attempt.ID || len(messageIDs) != 1 || first.Attempt.ProviderRef == "" {
		t.Fatalf("first=%#v second=%#v calls=%d messageIDs=%v", first, second, calls, messageIDs)
	}
}
