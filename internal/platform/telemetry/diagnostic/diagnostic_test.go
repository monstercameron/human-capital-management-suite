package diagnostic

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRequest() Request {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	return Request{Revision: 1, Actor: "operator-a", Approver: "operator-b", Purpose: "incident-123", Scope: "tenant:opaque-a", Level: LevelDebug, VolumeBudget: 2, StartsAt: start, ExpiresAt: start.Add(30 * time.Minute), Signature: strings.Repeat("s", 64)}
}

func TestDiagnosticElevationRequiresScopedExpiringGovernedConfiguration(t *testing.T) {
	request := testRequest()
	if err := Validate(request); err != nil {
		t.Fatal(err)
	}
	controller := NewController()
	snapshot, err := controller.Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest != Digest(request) || snapshot.Scope != request.Scope {
		t.Fatalf("snapshot=%+v does not bind request", snapshot)
	}
	if decision := controller.Decide(request.Scope, LevelDebug, request.StartsAt.Add(time.Minute)); !decision.Allowed {
		t.Fatalf("active scoped decision=%+v", decision)
	}
	if decision := controller.Decide(request.Scope, LevelDebug, request.ExpiresAt); decision.Allowed || decision.Reason != "EXPIRED_OR_NOT_ACTIVE" {
		t.Fatalf("expired decision=%+v", decision)
	}
}

func TestTodo_OBS_018_Property(t *testing.T) {
	controller := NewController()
	request := testRequest()
	request.VolumeBudget = 1
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	when := request.StartsAt.Add(time.Minute)
	if !controller.Decide(request.Scope, LevelInfo, when).Allowed {
		t.Fatal("first diagnostic record was denied")
	}
	if got := controller.Decide(request.Scope, LevelInfo, when); got.Allowed || got.Reason != "VOLUME_BUDGET_EXHAUSTED" {
		t.Fatalf("second decision=%+v", got)
	}
}

func TestTodo_OBS_018_Golden(t *testing.T) {
	request := testRequest()
	snapshot, err := NewController().Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	want := "diagnostic revision=1 scope=tenant:opaque-a level=3 expires=2026-09-02T12:30:00Z"
	if got := snapshot.Explain(); got != want {
		t.Fatalf("Explain=%q, want %q", got, want)
	}
}

func TestTodo_OBS_018_Race(t *testing.T) {
	controller := NewController()
	request := testRequest()
	request.VolumeBudget = 3
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	allowed := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision := controller.Decide(request.Scope, LevelInfo, request.StartsAt.Add(time.Minute))
			allowed <- decision.Allowed
		}()
	}
	wg.Wait()
	close(allowed)
	count := 0
	for ok := range allowed {
		if ok {
			count++
		}
	}
	if count != request.VolumeBudget {
		t.Fatalf("concurrent diagnostic grants=%d, want exact budget %d", count, request.VolumeBudget)
	}
}

func TestTodo_OBS_018_Security(t *testing.T) {
	request := testRequest()
	request.Scope = "tenant:opaque-a *"
	if !errors.Is(Validate(request), ErrInvalidRequest) {
		t.Fatal("wildcard diagnostic scope was accepted")
	}
	request = testRequest()
	request.Actor = request.Approver
	if !errors.Is(Validate(request), ErrInvalidRequest) {
		t.Fatal("self-approved elevation was accepted")
	}
}

func TestTodo_OBS_018_Conformance(t *testing.T) {
	request := testRequest()
	controller := NewController()
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	if decision := controller.Decide("correlation:other", LevelDebug, request.StartsAt.Add(time.Minute)); decision.Allowed || decision.Reason != "SCOPE_MISMATCH" {
		t.Fatalf("cross-scope decision=%+v", decision)
	}
}

func TestTodo_OBS_018_Mutation(t *testing.T) {
	request := testRequest()
	controller := NewController()
	snapshot, err := controller.Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Scope = "tenant:changed"
	if snapshot.Scope == request.Scope {
		t.Fatal("snapshot shares mutable request state")
	}
}
