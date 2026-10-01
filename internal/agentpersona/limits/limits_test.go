package limits

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func testPolicy() Policy {
	return Policy{Version: "policy-7", InvokerPerPersona: RateLimit{Max: 2, Window: time.Hour}, InvokerConcurrency: 1, Conversation: RateLimit{Max: 3, Window: time.Hour}, PersonaDailySpendMicros: 100}
}

func testRequest() Request {
	return Request{TenantID: "tenant", InvokerID: "invoker", ConversationID: "conversation", PersonaID: "persona", PersonaVersion: 4, PolicyVersion: "policy-7", EstimatedSpendMicros: 40}
}

func testService(t *testing.T, at time.Time, store Store) *Service {
	t.Helper()
	s, err := New(testPolicy(), func() time.Time { return at }, store)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTodo_AGENTP_015(t *testing.T) {
	store := NewMemoryStore()
	s := testService(t, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), store)
	r, err := s.Reserve(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if r.Token().PolicyVersion != "policy-7" || r.Token().Estimate != 40 {
		t.Fatalf("token = %+v", r.Token())
	}
	if err := r.Settle(context.Background(), 25); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(context.Background(), 1); !errors.Is(err, ErrReservationClosed) {
		t.Fatalf("second settle = %v", err)
	}
}

func TestTodo_AGENTP_015_Golden(t *testing.T) {
	store := NewMemoryStore()
	p := testPolicy()
	p.InvokerPerPersona.Max = 10
	p.Conversation.Max = 10
	s, err := New(p, func() time.Time { return time.Date(2026, 9, 29, 23, 59, 59, 0, time.UTC) }, store)
	if err != nil {
		t.Fatal(err)
	}
	req := testRequest()
	first, err := s.Reserve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Refund(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := s.Reserve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Token().ID; got != "persona-reservation-00000002" {
		t.Fatalf("reservation id = %q", got)
	}
	if err := second.Settle(context.Background(), 40); err != nil {
		t.Fatal(err)
	}
	third, err := s.Reserve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := third.Settle(context.Background(), 40); err != nil {
		t.Fatal(err)
	}
	_, err = s.Reserve(context.Background(), req)
	var denial *Denial
	if !errors.As(err, &denial) || denial.Code != DenialPersonaSpend || denial.RetryAfter != time.Second {
		t.Fatalf("denial = %v", err)
	}
}

func TestTodo_AGENTP_015_Property(t *testing.T) {
	store := NewMemoryStore()
	s := testService(t, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), store)
	req := testRequest()
	for i := 0; i < 2; i++ {
		r, err := s.Reserve(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Settle(context.Background(), 40); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Reserve(context.Background(), req)
	var denial *Denial
	if !errors.As(err, &denial) || denial.Code != DenialInvokerRate || denial.Scope != ScopeInvoker {
		t.Fatalf("rate denial = %v", err)
	}
	other := req
	other.InvokerID = "other"
	other.EstimatedSpendMicros = 20
	otherRes, err := s.Reserve(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if err := otherRes.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENTP_015_Race(t *testing.T) {
	store := NewMemoryStore()
	p := testPolicy()
	p.InvokerPerPersona.Max = 100
	s, err := New(p, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }, store)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	var wg sync.WaitGroup
	var attempts sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	release := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		attempts.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Reserve(context.Background(), testRequest())
			if err != nil {
				attempts.Done()
				return
			}
			mu.Lock()
			admitted++
			mu.Unlock()
			attempts.Done()
			<-release
			_ = r.Release(context.Background())
		}()
	}
	attempts.Wait()
	close(release)
	wg.Wait()
	if admitted != 1 {
		t.Fatalf("admitted = %d, want one concurrency slot", admitted)
	}
}

func TestPolicyAndDenialBoundaries(t *testing.T) {
	if _, err := New(Policy{}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid policy = %v", err)
	}
	store := NewMemoryStore()
	s := testService(t, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), store)
	wrong := testRequest()
	wrong.PolicyVersion = "old"
	_, err := s.Reserve(context.Background(), wrong)
	var denial *Denial
	if !errors.As(err, &denial) || denial.Code != DenialPolicyVersion || !errors.Is(err, ErrDenied) {
		t.Fatalf("policy denial = %v", err)
	}
	bad := testRequest()
	bad.EstimatedSpendMicros = 40
	res, err := s.Reserve(context.Background(), bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Settle(context.Background(), 41); !errors.Is(err, ErrSpendExceeds) {
		t.Fatalf("overspend = %v", err)
	}
	if err := res.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConversationAndContextDenials(t *testing.T) {
	store := NewMemoryStore()
	p := testPolicy()
	p.InvokerConcurrency = 10
	p.InvokerPerPersona.Max = 10
	p.Conversation.Max = 1
	s, err := New(p, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }, store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Reserve(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	other := testRequest()
	other.InvokerID = "other"
	if _, err := s.Reserve(context.Background(), other); err == nil {
		t.Fatal("expected conversation denial")
	} else {
		var denial *Denial
		if !errors.As(err, &denial) || denial.Code != DenialConversationRate {
			t.Fatalf("conversation denial = %v", err)
		}
	}
	if err := first.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Reserve(ctx, testRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}
