package chatapps

import (
	"context"
	"errors"
	"testing"
)

type cardCallbackSpy struct{ calls int }

func (s *cardCallbackSpy) Call(_ context.Context, _ Installation, cb Callback) (CallbackResult, error) {
	s.calls++
	if cb.Card == nil || cb.Card.Kind != "notice" || cb.Actor.Principal == "" {
		return CallbackResult{}, errors.New("callback lost typed card or invoker context")
	}
	return CallbackResult{Accepted: true, Message: "saved"}, nil
}

func TestTodo_CHAT_041(t *testing.T) {
	s, actor := fixture()
	spy := &cardCallbackSpy{}
	s.Callback = spy

	installation, err := s.Repo.Get(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	installation.Manifest.Cards = []CardType{{Kind: "notice", Fields: []Field{
		{Name: "text", Type: "string", Required: true},
		{Name: "detail", Type: "string"},
	}}}
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}

	valid := Card{Kind: "notice", Values: map[string]string{"text": "Ready", "detail": "For this conversation"}}
	result, err := s.Invoke(context.Background(), actor, installation.ID, Callback{Command: "ping", IdempotencyKey: "card-valid", Card: &valid})
	if err != nil || !result.Accepted || result.Message != "saved" || spy.calls != 1 {
		t.Fatalf("valid card result=%+v err=%v callback calls=%d", result, err, spy.calls)
	}

	for _, card := range []Card{
		{Kind: "notice", Values: map[string]string{"text": "Ready", "hcm:write": "true"}},
		{Kind: "notice", Values: map[string]string{"detail": "missing required text"}},
	} {
		if _, err := s.Invoke(context.Background(), actor, installation.ID, Callback{Command: "ping", IdempotencyKey: "card-invalid", Card: &card}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid card reached callback: card=%+v err=%v", card, err)
		}
	}
	installation.Manifest.Cards[0].Fields[0].Type = "number"
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Invoke(context.Background(), actor, installation.ID, Callback{Command: "ping", IdempotencyKey: "card-unsupported", Card: &valid}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported schema type reached callback: %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("invalid typed payload invoked callback %d times", spy.calls)
	}
}

// TestTodo_CHAT_041_Golden is the CHAT-041 GOLDEN matrix test: the
// server-owned card rendering's exact escaped bytes are pinned, so a
// regression that lets a value reach the DOM unescaped (or reorders schema
// fields) is caught by a byte diff, not a fuzzy assertion.
func TestTodo_CHAT_041_Golden(t *testing.T) {
	manifest := Manifest{
		AppID: "leave-request", Version: 1,
		Cards: []CardType{{Kind: "notice", Fields: []Field{
			{Name: "text", Type: "string", Required: true},
			{Name: "detail", Type: "string"},
		}}},
	}
	card := Card{Kind: "notice", Values: map[string]string{
		"text":   "Ready <b>now</b>",
		"detail": `"quoted" & escaped`,
	}}
	got, err := RenderCard(manifest, card)
	if err != nil {
		t.Fatal(err)
	}
	const want = `<div class="chatapp-card" data-card-kind="notice"><div data-field="text">Ready &lt;b&gt;now&lt;/b&gt;</div><div data-field="detail">&#34;quoted&#34; &amp; escaped</div></div>`
	if got != want {
		t.Fatalf("rendered card bytes changed:\n got %s\nwant %s", got, want)
	}

	if _, err := RenderCard(manifest, Card{Kind: "script", Values: map[string]string{"text": "<script>alert(1)</script>"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("undeclared card kind rendered: %v", err)
	}
}
