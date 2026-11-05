package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestMessagingSendCapability(t *testing.T) {
	defs := Definitions()
	if len(defs) != 1 || defs[0].ID != CapSend {
		t.Fatalf("definitions = %+v", defs)
	}
	if defs[0].EffectClass != capability.EffectExternalMutation {
		t.Fatalf("send effect = %s, want EXTERNAL_MUTATION", defs[0].EffectClass)
	}
	reg := capability.NewRegistry()
	if err := reg.Register(defs[0], func(_ context.Context, payload any) (any, error) { return nil, nil }); err != nil {
		t.Fatalf("register send: %v", err)
	}
	rec, ok := reg.Lookup(capability.Key{ID: CapSend, Version: 1})
	if !ok || rec.Digest == "" {
		t.Fatal("messaging.send not resolvable with a digest")
	}
}

func sendInputs() map[string]string {
	return map[string]string{
		"tenant_id": "acme", "organization_scope": "acme/eng",
		"audience_expression": "team:eng", "audience_resolution_policy": "frozen",
		"purpose": "NOTICE", "classification": "INTERNAL", "urgency": "normal",
		"delivery_requirement": "DELIVERED", "reply_mode": "NOT_ALLOWED",
		"template_ref": "team_notice/v3", "content_ref": "", "parameters_ref": "params:1",
		"correlation_id": "corr-1", "expires_at": "2000000000", "notice_key": "n-1",
	}
}

func TestMessagingSend(t *testing.T) {
	sender := &recordingSender{}
	out, err := Send(context.Background(), sender, sendInputs())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d", len(sender.sent))
	}
	if sender.sent[0].TemplateRef != "team_notice/v3" || sender.sent[0].AudienceExpression != "team:eng" {
		t.Fatalf("intent = %+v", sender.sent[0])
	}
	if out.MessageID == "" || out.Digest == "" {
		t.Fatalf("outcome = %+v", out)
	}
	if out.Digest != DigestIntent(sender.sent[0]) {
		t.Fatal("outcome digest does not match the sent intent")
	}
	again, err := Send(context.Background(), sender, sendInputs())
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	if again.MessageID != out.MessageID {
		t.Fatal("same notice key produced a different message id")
	}
}

func TestMessagingSendRefusals(t *testing.T) {
	sender := &recordingSender{}
	bad := sendInputs()
	bad["audience_expression"] = ""
	if _, err := Send(context.Background(), sender, bad); err == nil {
		t.Fatal("intent without audience accepted")
	}
	bad = sendInputs()
	bad["expires_at"] = "soon"
	if _, err := Send(context.Background(), sender, bad); err == nil {
		t.Fatal("non-integer expiry accepted")
	}
	bad = sendInputs()
	delete(bad, "notice_key")
	if _, err := Send(context.Background(), sender, bad); err == nil {
		t.Fatal("notice without idempotency key accepted")
	}
	failing := &recordingSender{err: errors.New("provider down")}
	if _, err := Send(context.Background(), failing, sendInputs()); err == nil {
		t.Fatal("provider failure swallowed")
	}
	if _, err := Send(context.Background(), nil, sendInputs()); err == nil {
		t.Fatal("nil sender accepted")
	}
}

type recordingSender struct {
	sent []MessageIntent
	err  error
}

func (s *recordingSender) Send(_ context.Context, intent MessageIntent) (Receipt, error) {
	if s.err != nil {
		return Receipt{}, s.err
	}
	s.sent = append(s.sent, intent)
	return Receipt{MessageID: "msg-" + intent.CorrelationID}, nil
}
