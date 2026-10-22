package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// recorder logs what the gate does, in order, so a test reads the sequence.
type chatSheetRecorder struct {
	events []string
	done   func(bool)
	loads  int
}

func (r *chatSheetRecorder) load(done func(loaded bool)) {
	r.loads++
	r.events = append(r.events, "load")
	r.done = done
}

func (r *chatSheetRecorder) navigate(label string) func() {
	return func() { r.events = append(r.events, "navigate "+label) }
}

func chatHref() string { return productui.Path(productui.PageChat) }

// From a page that does not link Chat's sheet, going to Chat asks for the sheet
// first and navigates only once it has loaded: Chat is never painted without
// its rules.
func TestChatSheetGateHoldsNavigationUntilTheSheetLoads(t *testing.T) {
	r := &chatSheetRecorder{}
	gate := newChatSheetGate(chatSheetNamed, r.load)

	gate.Navigate(chatHref()+"?channel=general", r.navigate("chat"))
	if want := []string{"load"}; !reflect.DeepEqual(r.events, want) {
		t.Fatalf("before the sheet loads, events = %v, want %v", r.events, want)
	}
	if gate.State() != chatSheetLoading {
		t.Fatalf("state = %v, want loading", gate.State())
	}
	r.done(true)
	if want := []string{"load", "navigate chat"}; !reflect.DeepEqual(r.events, want) {
		t.Fatalf("events = %v, want %v", r.events, want)
	}
	if gate.State() != chatSheetReady {
		t.Fatalf("state = %v, want ready", gate.State())
	}

	// Once it is in the page, later trips to Chat are not held.
	gate.Navigate(chatHref(), r.navigate("again"))
	if r.loads != 1 || r.events[len(r.events)-1] != "navigate again" {
		t.Fatalf("a second trip to Chat asked for the sheet again or was held: loads %d, events %v", r.loads, r.events)
	}
}

// Only the pages that draw Chat's rules wait for them.
func TestChatSheetGateOnlyHoldsPagesThatNeedTheSheet(t *testing.T) {
	for _, test := range []struct {
		name string
		href string
		held bool
	}{
		{"chat", productui.Path(productui.PageChat), true},
		{"chat with a channel", productui.Path(productui.PageChat) + "#channel=c1", true},
		{"chat settings", productui.Path(productui.PageChatSettings), true},
		{"home", productui.Path(productui.PageHome), false},
		{"agents sit under Chat but style themselves", productui.Path(productui.PageAgents), false},
		{"docs", productui.Path(productui.PageDocs), false},
		{"an address that is not a page", "/somewhere/else", false},
		{"an address that does not parse", "%zz", false},
	} {
		r := &chatSheetRecorder{}
		gate := newChatSheetGate(chatSheetNamed, r.load)
		gate.Navigate(test.href, r.navigate(test.name))
		held := r.loads == 1 && len(r.events) == 1
		if held != test.held {
			t.Errorf("%s: held = %v, want %v (events %v)", test.name, held, test.held, r.events)
		}
		if !test.held && !reflect.DeepEqual(r.events, []string{"navigate " + test.name}) {
			t.Errorf("%s: events = %v, want an immediate navigation", test.name, r.events)
		}
	}
}

// A document that is itself a Chat page links the sheet already, and a document
// that says nothing about it has nothing to wait for.
func TestChatSheetGateWithNothingToLoad(t *testing.T) {
	for _, state := range []chatSheetState{chatSheetReady, chatSheetUnnamed} {
		r := &chatSheetRecorder{}
		gate := newChatSheetGate(state, r.load)
		gate.Navigate(chatHref(), r.navigate("chat"))
		if r.loads != 0 || !reflect.DeepEqual(r.events, []string{"navigate chat"}) {
			t.Errorf("state %v: loads %d, events %v", state, r.loads, r.events)
		}
		gate.Wait(nil)
	}
}

// Navigations that arrive while the sheet is loading share the one request and
// go ahead in the order they came.
func TestChatSheetGateSharesOneLoad(t *testing.T) {
	r := &chatSheetRecorder{}
	gate := newChatSheetGate(chatSheetNamed, r.load)
	gate.Navigate(chatHref(), r.navigate("first"))
	gate.Navigate(productui.Path(productui.PageChatSettings), r.navigate("second"))
	if r.loads != 1 {
		t.Fatalf("%d loads for two navigations, want 1", r.loads)
	}
	r.done(true)
	r.done(false)
	want := []string{"load", "navigate first", "navigate second"}
	if !reflect.DeepEqual(r.events, want) || gate.State() != chatSheetReady {
		t.Fatalf("events = %v, state %v; want %v and ready (a late second report changes nothing)", r.events, gate.State(), want)
	}
}

// A sheet that fails or never answers does not trap the person: the navigation
// goes ahead and the gate does not ask again.
func TestChatSheetGateGivesUpOnAFailedLoad(t *testing.T) {
	r := &chatSheetRecorder{}
	gate := newChatSheetGate(chatSheetNamed, r.load)
	gate.Navigate(chatHref(), r.navigate("chat"))
	r.done(false)
	if want := []string{"load", "navigate chat"}; !reflect.DeepEqual(r.events, want) || gate.State() != chatSheetFailed {
		t.Fatalf("events = %v, state %v; want %v and failed", r.events, gate.State(), want)
	}
	gate.Navigate(chatHref(), r.navigate("retry"))
	if r.loads != 1 || r.events[len(r.events)-1] != "navigate retry" {
		t.Fatalf("a failed sheet was asked for again or held a navigation: loads %d, events %v", r.loads, r.events)
	}
}

// A route loader reached without going through Navigate (history, a link the
// router handled) waits too: Wait starts the load when nothing has, returns when
// the sheet is in, and returns on cancellation instead of hanging.
func TestChatSheetGateWaitHoldsTheLoader(t *testing.T) {
	r := &chatSheetRecorder{}
	gate := newChatSheetGate(chatSheetNamed, r.load)
	returned := make(chan struct{})
	go func() {
		gate.Wait(nil)
		close(returned)
	}()
	deadline := time.After(2 * time.Second)
	for {
		if gate.State() == chatSheetLoading {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Wait did not ask for the sheet")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-returned:
		t.Fatal("Wait returned before the sheet loaded")
	case <-time.After(20 * time.Millisecond):
	}
	r.done(true)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return once the sheet loaded")
	}

	cancelled := make(chan struct{})
	slow := newChatSheetGate(chatSheetNamed, (&chatSheetRecorder{}).load)
	close(cancelled)
	slow.Wait(cancelled)
}

// A gate with no way to load (no page to ask) fails closed to going ahead.
func TestChatSheetGateWithoutALoaderProceeds(t *testing.T) {
	gate := newChatSheetGate(chatSheetNamed, nil)
	ran := false
	gate.Navigate(chatHref(), func() { ran = true })
	if !ran || gate.State() != chatSheetFailed {
		t.Fatalf("ran %v, state %v", ran, gate.State())
	}
}
