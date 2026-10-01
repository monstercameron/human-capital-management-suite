package application

import "testing"

func TestSelfClockCompositionRequiresAuthoritativePorts(t *testing.T) {
	if _, err := (SelfClockComposition{}).WorkerSelfService(); err == nil {
		t.Fatal("expected missing authoritative ports error")
	}
}
