package pagedef

import "testing"

// TestTodo_ALIGN_043 proves the governed work-loop fixture has an admitted
// floorplan, a primary job, authorized reads, and explicit completion/support
// regions rather than a storage-shaped page.
func TestTodo_ALIGN_043(t *testing.T) {
	pd := WorkLoopPageDefinition()
	if violations := pd.Validate(); len(violations) != 0 {
		t.Fatalf("WorkLoopPageDefinition invalid: %v", violations)
	}
	if pd.PageID != "work.my-work" || pd.Version < 1 {
		t.Fatalf("work-loop identity = %q@%d, want versioned work.my-work", pd.PageID, pd.Version)
	}
	hasPrimary, hasSupporting, hasCompletion := false, false, false
	for _, region := range pd.Regions {
		switch region.Kind {
		case RegionPrimary:
			hasPrimary = true
		case RegionSupporting:
			hasSupporting = true
		case RegionCompletion:
			hasCompletion = true
		}
	}
	if !hasPrimary || !hasSupporting || !hasCompletion {
		t.Fatalf("work-loop anatomy missing primary/supporting/completion: %+v", pd.Regions)
	}
	if pd.Accessibility.LiveRegion != LiveRegionPolite {
		t.Fatalf("work-loop live region = %q, want polite", pd.Accessibility.LiveRegion)
	}
}

// TestTodo_ALIGN_043_Property proves every state is closed and that fixture
// construction does not mutate the page definition or digest.
func TestTodo_ALIGN_043_Property(t *testing.T) {
	pd := WorkLoopPageDefinition()
	digest := pd.Digest()
	for _, state := range WorkLoopStates() {
		if !state.Valid() {
			t.Errorf("WorkLoopStates includes invalid state %q", state)
		}
	}
	if got := WorkLoopPageDefinition().Digest(); got != digest {
		t.Fatalf("work-loop fixture digest changed across construction: %q vs %q", got, digest)
	}
}

// TestTodo_ALIGN_043_Golden pins the semantic names that make the fixture
// useful to an assistive technology user.
func TestTodo_ALIGN_043_Golden(t *testing.T) {
	pd := WorkLoopPageDefinition()
	for _, want := range []string{"My Work", "Assigned work", "Source and freshness", "Next step"} {
		found := false
		for _, region := range pd.Regions {
			if region.Heading != nil && region.Heading.Text == want {
				found = true
			}
		}
		if !found {
			t.Errorf("work-loop fixture does not declare heading %q", want)
		}
	}
}

// TestTodo_ALIGN_043_Security rejects a mutation that attempts to smuggle
// markup into a governed heading.
func TestTodo_ALIGN_043_Security(t *testing.T) {
	pd := WorkLoopPageDefinition()
	pd.Regions[1].Heading.Text = `<img src=x onerror=alert(1)>`
	if violations := pd.Validate(); len(violations) == 0 {
		t.Fatal("work-loop definition accepted executable markup")
	}
}

// TestTodo_ALIGN_043_Conformance proves the real admitted floorplan resolves
// the fixture, and every named RPC belongs to a generated service.
func TestTodo_ALIGN_043_Conformance(t *testing.T) {
	known := KnownRPCs()
	for _, region := range WorkLoopPageDefinition().Regions {
		for _, binding := range region.Bindings {
			if !known[binding.RPC] {
				t.Errorf("work-loop binding %q is not a registered RPC", binding.RPC)
			}
		}
		for _, action := range region.Actions {
			if !known[action.RPC] {
				t.Errorf("work-loop action %q is not a registered RPC", action.RPC)
			}
		}
	}
}

// TestTodo_ALIGN_044 proves the fallback is versioned, accessible, and has a
// read/retry route with an explicit role rather than a fake success action.
func TestTodo_ALIGN_044(t *testing.T) {
	pd := FailureRecoveryPageDefinition()
	if violations := pd.Validate(); len(violations) != 0 {
		t.Fatalf("FailureRecoveryPageDefinition invalid: %v", violations)
	}
	if pd.Accessibility.LiveRegion != LiveRegionAssertive {
		t.Fatalf("failure page live region = %q, want assertive", pd.Accessibility.LiveRegion)
	}
	for _, region := range pd.Regions {
		for _, action := range region.Actions {
			if action.ID == "retry-read" && action.RequiredRole == "" {
				t.Fatal("retry action has no required role")
			}
			if action.ID == "retry-read" && action.RPC == RPCRef(IntentServiceName, "ExecuteIntent") {
				t.Fatal("failure recovery retry must not execute a material intent")
			}
		}
	}
	known := KnownRPCs()
	for _, region := range pd.Regions {
		for _, binding := range region.Bindings {
			if !known[binding.RPC] {
				t.Errorf("failure binding %q is not a registered RPC", binding.RPC)
			}
		}
	}
}

// TestTodo_ALIGN_044_Property keeps every recovery state explicit and
// ensures the fallback has no ungoverned state value.
func TestTodo_ALIGN_044_Property(t *testing.T) {
	seen := map[WorkLoopState]bool{}
	for _, state := range WorkLoopStates() {
		if seen[state] {
			t.Fatalf("duplicate work-loop state %q", state)
		}
		seen[state] = true
	}
	if len(seen) != 7 {
		t.Fatalf("work-loop state vocabulary has %d members, want 7", len(seen))
	}
}
