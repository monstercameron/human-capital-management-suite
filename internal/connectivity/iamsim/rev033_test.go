package iamsim

import (
	"context"
	"testing"
)

func TestTodo_REV_033_02_NoDefaultCallbackClient(t *testing.T) {
	s := New(Config{WebhookSecret: []byte("test secret")})
	if s.client != nil {
		t.Fatal("simulator constructed an outbound client without an injected port or egress gateway")
	}
	change := &change{req: AccessChangeRequest{CallbackURL: "https://receiver.example.test/callback", Tenant: "tenant-1"}}
	if _, err := s.attempt(context.Background(), change, &delivery{}, "event-1", EventGranted, []byte("{}")); err == nil {
		t.Fatal("callback without an outbound port unexpectedly succeeded")
	}
}
