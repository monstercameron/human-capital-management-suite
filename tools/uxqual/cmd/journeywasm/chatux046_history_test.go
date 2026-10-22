package main

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatux046Page models what the browser does around the gate: the sheet is
// applied when its load answers, the router paints the page of an address it is
// given, and every paint of Chat records whether the sheet was applied at that
// moment. A paint of Chat with the sheet not applied is the flash this test
// exists to rule out.
type chatux046Page struct {
	sheetApplied bool
	events       []string
	chatPaints   []bool
	finishLoad   func(bool)
}

func (p *chatux046Page) load(done func(loaded bool)) {
	p.events = append(p.events, "load")
	p.finishLoad = func(ok bool) {
		p.sheetApplied = ok
		done(ok)
	}
}

// route is the router taking an address: it paints that page.
func (p *chatux046Page) route(href string) {
	page, _ := chatSheetPageForHref(href)
	p.events = append(p.events, "paint "+string(page))
	if productui.PageNeedsChatStylesheet(page) {
		p.chatPaints = append(p.chatPaints, p.sheetApplied)
	}
}

// popstate is the browser arriving at href by Back or Forward: the history
// listener sees the event first and either lets the router have it or holds it.
func (p *chatux046Page) popstate(gate *chatSheetGate, href string) {
	if gate.Arrive(href, func() { p.route(href) }) {
		p.events = append(p.events, "held "+href)
		return
	}
	p.route(href)
}

func home() string { return productui.Path(productui.PageHome) }

// Home, Chat, Back, Forward with the sheet loaded on the way into Chat (the
// ordinary click): neither history arrival is held, and Chat is painted with the
// sheet applied every time.
func TestTodo_CHATUX_046_Browser(t *testing.T) {
	page := &chatux046Page{}
	gate := newChatSheetGate(chatSheetNamed, page.load)

	gate.Navigate(chatHref(), func() { page.route(chatHref()) }) // Home -> Chat
	page.finishLoad(true)
	page.popstate(gate, home())     // Back
	page.popstate(gate, chatHref()) // Forward

	if want := []string{"load", "paint chat", "paint home", "paint chat"}; !reflect.DeepEqual(page.events, want) {
		t.Fatalf("events = %v, want %v", page.events, want)
	}
	if want := []bool{true, true}; !reflect.DeepEqual(page.chatPaints, want) {
		t.Fatalf("Chat was painted with the sheet applied = %v, want %v", page.chatPaints, want)
	}
}

// The same walk with a Chat entry the click path never saw (an address pushed
// by the router or the page itself), so the first arrival at Chat is by Back or
// Forward and the sheet is not in the page: the arrival is held, the router
// does not paint Chat until the sheet has loaded, and then it does, once.
func TestTodo_CHATUX_046_Browser_FirstArrivalByHistory(t *testing.T) {
	page := &chatux046Page{}
	gate := newChatSheetGate(chatSheetNamed, page.load)

	page.route(home())              // the document the person is on
	page.popstate(gate, home())     // Back to Home from some other entry
	page.popstate(gate, chatHref()) // Forward to a Chat entry: first arrival at Chat
	if want := []string{"paint home", "paint home", "load", "held " + chatHref()}; !reflect.DeepEqual(page.events, want) {
		t.Fatalf("before the sheet answers, events = %v, want %v", page.events, want)
	}
	if len(page.chatPaints) != 0 {
		t.Fatal("Chat was painted before the sheet was applied")
	}
	page.finishLoad(true)
	if want := []bool{true}; !reflect.DeepEqual(page.chatPaints, want) {
		t.Fatalf("Chat was painted with the sheet applied = %v, want %v", page.chatPaints, want)
	}

	// Back to Home and Forward again: the sheet stays, nothing is held.
	page.popstate(gate, home())
	page.popstate(gate, chatHref())
	if last := page.events[len(page.events)-1]; last != "paint chat" || len(page.chatPaints) != 2 || !page.chatPaints[1] {
		t.Fatalf("the second arrival was held or unstyled: events %v, paints %v", page.events, page.chatPaints)
	}
}

// A sheet that fails still lets the arrival through, so Back never strands the
// person; Chat then paints without its rules, which is the failure the gate
// reports rather than hides.
func TestTodo_CHATUX_046_Browser_FailedSheetStillArrives(t *testing.T) {
	page := &chatux046Page{}
	gate := newChatSheetGate(chatSheetNamed, page.load)
	page.popstate(gate, chatHref())
	page.finishLoad(false)
	if want := []bool{false}; !reflect.DeepEqual(page.chatPaints, want) {
		t.Fatalf("paints = %v, want one unstyled paint after a failed load", page.chatPaints)
	}
}

// An arrival at a page that is not Chat is never held, and neither is one at
// Chat's neighbour under the same path, the agent pages.
func TestTodo_CHATUX_046_ArrivalsAtOtherPagesAreNotHeld(t *testing.T) {
	page := &chatux046Page{}
	gate := newChatSheetGate(chatSheetNamed, page.load)
	for _, href := range []string{home(), productui.Path(productui.PageAgents), productui.Path(productui.PageDocs), "/somewhere/else", "%zz"} {
		if gate.Arrive(href, func() { t.Errorf("%s: release called for an arrival that was not held", href) }) {
			t.Errorf("%s was held", href)
		}
	}
	if page.events != nil {
		t.Fatalf("the sheet was asked for by arrivals that do not need it: %v", page.events)
	}
}
