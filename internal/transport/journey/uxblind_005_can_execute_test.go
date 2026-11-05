package journey_test

import "testing"

// UXBLIND-005: the wire detail carries the server's execute capability so the
// actions section can offer the start only to viewers the server would admit.
func TestTodo_UXBLIND_005_CanExecuteMapping(t *testing.T) {
	d := fixtureDetail()
	d.CanExecute = true
	if got := roundTripDetail(t, d); !got.GetCanExecute() {
		t.Fatal("CanExecute=true did not reach the wire detail")
	}
	d.CanExecute = false
	if got := roundTripDetail(t, d); got.GetCanExecute() {
		t.Fatal("CanExecute=false reached the wire detail as true")
	}
}
