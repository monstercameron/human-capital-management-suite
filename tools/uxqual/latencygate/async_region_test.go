package latencygate

import "testing"

func TestTodo_UIPOLISH_009(t *testing.T) {
	region, err := NewRegion(CollectionRegion, 320)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != StateLoading || region.Presentation().ReservedHeight != 320 {
		t.Fatalf("initial region = %+v", region)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true})
	if err != nil || region.State != StateReady {
		t.Fatalf("ready transition = %+v, %v", region, err)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadStarted})
	if err != nil || region.State != StateRefreshing || !region.Presentation().RetainProjection {
		t.Fatalf("refresh transition = %+v, %v", region.Presentation(), err)
	}
}

func TestTodo_UIPOLISH_009_Golden(t *testing.T) {
	region, _ := NewRegion(FormRegion, 240)
	region = region.WithEnteredValues(true).WithNextAction("Invite a worker")
	for _, event := range []RegionEvent{{Type: EventLoadSucceeded, HasData: false}, {Type: EventSubmitStarted}} {
		var err error
		region, err = region.Transition(event)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := region.Presentation()
	want := Presentation{Variant: StateSubmitting, ReservedHeight: 240, PreserveInput: true, SubmissionsBlocked: true}
	if p != want {
		t.Fatalf("presentation = %+v, want %+v", p, want)
	}
}

func TestTodo_UIPOLISH_009_Browser(t *testing.T) {
	region, _ := NewRegion(DetailRegion, 180)
	region, _ = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: false})
	p := region.WithNextAction("Create a profile").Presentation()
	if p.Variant != StateEmpty || p.ReservedHeight != 180 || p.NextAction != "Create a profile" {
		t.Fatalf("actionable empty presentation = %+v", p)
	}
}

func TestTodo_UIPOLISH_009_Accessibility(t *testing.T) {
	region, _ := NewRegion(CollectionRegion, 100)
	region = region.WithNextAction("Retry people search")
	for _, event := range []RegionEvent{{Type: EventLoadFailed, Retryable: true}} {
		var err error
		region, err = region.Transition(event)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := region.Presentation()
	if p.Politeness != "polite" || p.NextAction != "Retry people search" {
		t.Fatalf("error announcement = %+v", p)
	}
}

func TestTodo_UIPOLISH_009_AccessibilityRequiresLocalizedErrorAction(t *testing.T) {
	region, _ := NewRegion(CollectionRegion, 100)
	region, _ = region.Transition(RegionEvent{Type: EventLoadFailed, Retryable: true})
	if action := region.Presentation().NextAction; action != "" {
		t.Fatalf("generic region supplied user-visible action %q", action)
	}
}

func TestTodo_UIPOLISH_009_FaultRetainsSafeProjectionAfterRefreshFailure(t *testing.T) {
	region, _ := NewRegion(CollectionRegion, 100)
	region, _ = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true})
	region, _ = region.Transition(RegionEvent{Type: EventLoadStarted})
	region, _ = region.Transition(RegionEvent{Type: EventLoadFailed, Retryable: true})
	if !region.Presentation().RetainProjection {
		t.Fatal("safe stale projection was dropped after refresh failure")
	}
}

func TestTodo_UIPOLISH_009_FaultDoesNotRetryNonRetryableMutation(t *testing.T) {
	region, _ := NewRegion(MutationRegion, 100)
	region, _ = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true})
	region, _ = region.Transition(RegionEvent{Type: EventSubmitStarted})
	region, _ = region.Transition(RegionEvent{Type: EventSubmitFailed, Retryable: false})
	if _, err := region.Transition(RegionEvent{Type: EventSubmitStarted}); err == nil {
		t.Fatal("non-retryable mutation error accepted a duplicate submission")
	}
}

func TestTodo_UIPOLISH_009_Fault(t *testing.T) {
	region, _ := NewRegion(MutationRegion, 100)
	region, _ = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true})
	region, _ = region.Transition(RegionEvent{Type: EventSubmitStarted})
	if _, err := region.Transition(RegionEvent{Type: EventSubmitStarted}); err == nil {
		t.Fatal("duplicate submission was accepted")
	}
	if _, err := region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true}); err == nil {
		t.Fatal("load completion interrupted mutation")
	}
}

func TestTodo_UIPOLISH_009_FaultRejectsLateLoadCompletion(t *testing.T) {
	region, err := NewRegion(CollectionRegion, 120)
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadStarted, OperationID: "load-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, OperationID: "load-old", HasData: true}); err == nil {
		t.Fatal("late load completion was accepted")
	}
	if _, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true}); err == nil {
		t.Fatal("uncorrelated load completion was accepted for a correlated operation")
	}
	if region.State != StateLoading || region.HasData {
		t.Fatalf("stale completion mutated region: %+v", region)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, OperationID: "load-a", HasData: false})
	if err != nil || region.State != StateEmpty {
		t.Fatalf("active load completion = %+v, %v", region, err)
	}
}

func TestTodo_UIPOLISH_009_FaultRejectsLateMutationCompletionAfterRetry(t *testing.T) {
	region, err := NewRegion(MutationRegion, 120)
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: true})
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventSubmitStarted, OperationID: "submit-a"})
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventSubmitFailed, OperationID: "submit-a", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventSubmitStarted, OperationID: "submit-b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = region.Transition(RegionEvent{Type: EventSubmitSuccess, OperationID: "submit-a", HasData: true}); err == nil {
		t.Fatal("late mutation completion was accepted after retry")
	}
	if region.State != StateSubmitting || region.ActiveOperationID != "submit-b" {
		t.Fatalf("stale mutation completion mutated region: %+v", region)
	}
}

func TestTodo_UIPOLISH_009_FaultRetryClearsFailedOperation(t *testing.T) {
	region, err := NewRegion(CollectionRegion, 120)
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadStarted, OperationID: "load-a"})
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadFailed, OperationID: "load-a", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	region, err = region.Transition(RegionEvent{Type: EventRetry})
	if err != nil || region.State != StateLoading || region.ActiveOperationID != "" {
		t.Fatalf("retry state = %+v, %v", region, err)
	}
}

func TestTodo_UIPOLISH_009_FaultRetryCorrelatesNewOperation(t *testing.T) {
	region, err := NewRegion(CollectionRegion, 120)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []RegionEvent{
		{Type: EventLoadStarted, OperationID: "first"},
		{Type: EventLoadFailed, OperationID: "first", Retryable: true},
		{Type: EventRetry, OperationID: "second"},
	} {
		region, err = region.Transition(event)
		if err != nil {
			t.Fatal(err)
		}
	}
	if region.ActiveOperationID != "second" || region.State != StateLoading {
		t.Fatalf("retry did not own the new load: %+v", region)
	}
	if _, err := region.Transition(RegionEvent{Type: EventLoadSucceeded, OperationID: "first", HasData: true}); err == nil {
		t.Fatal("failed operation settled the retry")
	}
	region, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, OperationID: "second", HasData: true})
	if err != nil || region.State != StateReady {
		t.Fatalf("retry completion = %+v, %v", region, err)
	}
}

func TestTodo_UIPOLISH_009_Performance(t *testing.T) {
	region, _ := NewRegion(CollectionRegion, 320)
	for i := 0; i < 10000; i++ {
		var err error
		region, err = region.Transition(RegionEvent{Type: EventLoadSucceeded, HasData: i%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		region, err = region.Transition(RegionEvent{Type: EventLoadStarted})
		if err != nil {
			t.Fatal(err)
		}
	}
	if region.Presentation().ReservedHeight != 320 {
		t.Fatal("reserved geometry changed during repeated refresh")
	}
}
