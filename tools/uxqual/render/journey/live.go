package journey

import (
	"sync"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The journey page's product surface is a GoWebComponents client compiled to
// wasm: the server sends a shell, the client mounts this tree and talks to
// the engine over gRPC on a WebSocket. RenderToString and Document exist so
// the same tree can be asserted without a browser -- they are the test path,
// not a second product.
//
// This file is the bridge between "a Page is a value" and "the DOM is a
// living thing". A Store holds the current Page; the client replaces it
// whenever an RPC answers, and every mounted view re-renders. Everything
// here builds and behaves identically on native, so the whole live path is
// exercised by ordinary `go test`.

// Store holds the Page a mounted view renders and notifies its subscribers
// when it changes. It is safe to call from any goroutine: an RPC answer may
// arrive off the render loop even on wasm, where the callback runs on a
// different Go goroutine than the one that mounted.
type Store struct {
	mu          sync.RWMutex
	page        Page
	subscribers map[int]func()
	nextID      int
}

// NewStore returns a Store seeded with p.
func NewStore(p Page) *Store {
	return &Store{page: p, subscribers: make(map[int]func())}
}

// Page returns the current page.
func (s *Store) Page() Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.page
}

// Set replaces the page and re-renders every mounted view.
func (s *Store) Set(p Page) {
	s.mu.Lock()
	s.page = p
	s.mu.Unlock()
	s.notify()
}

// Update mutates the page in place and re-renders. It is the form every
// small change takes -- a keystroke, a notice, one journey's stage -- so a
// caller never has to rebuild a whole Page to change one field.
func (s *Store) Update(mutate func(*Page)) {
	if mutate == nil {
		return
	}
	s.mu.Lock()
	mutate(&s.page)
	s.mu.Unlock()
	s.notify()
}

// SetValue records one field's current value. It is what the renderer wires
// every controlled input to, so the client does not have to write that
// closure for each form.
func (s *Store) SetValue(fieldID, value string) {
	s.mu.Lock()
	if s.page.Values == nil {
		s.page.Values = make(map[string]string)
	}
	s.page.Values[fieldID] = value
	s.mu.Unlock()
	s.notify()
}

// Values returns a copy of the current field values.
func (s *Store) Values() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.page.Values))
	for k, v := range s.page.Values {
		out[k] = v
	}
	return out
}

// Subscribe registers fn to run after every change and returns the
// unsubscribe function. A view calls it from an effect, so the subscription
// is torn down with the view rather than outliving it.
func (s *Store) Subscribe(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	s.mu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[int]func())
	}
	s.nextID++
	id := s.nextID
	s.subscribers[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

// notify runs every subscriber outside the lock, so a subscriber that reads
// the page back (every one of them does) cannot deadlock.
func (s *Store) notify() {
	s.mu.RLock()
	fns := make([]func(), 0, len(s.subscribers))
	for _, fn := range s.subscribers {
		fns = append(fns, fn)
	}
	s.mu.RUnlock()
	for _, fn := range fns {
		fn()
	}
}

// SubscriberCount reports how many views are currently mounted on this
// store. It exists so a client (and this package's tests) can assert that
// unmounting actually released the subscription.
func (s *Store) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// LiveComponent returns the component ui.Render mounts: it draws the
// store's current Page and re-renders whenever the store changes.
//
// Its hook sequence is fixed at two calls, unconditionally, in one order --
// a UseState that holds the revision counter and a UseEffect that owns the
// subscription. GWC hooks are positional, so a conditional or
// data-dependent hook would corrupt the fiber's hook list on the next
// render; keeping them out of Build (which is data-shaped, and whose event
// handlers are created per node) is what makes that guarantee hold.
func LiveComponent(s *Store) ui.Node {
	return liveComponent(s, Build)
}

// LiveContentComponent is the embedded counterpart to LiveComponent. It
// subscribes to the same store and renders the same journey page body, but
// leaves application chrome and the main landmark to the product shell.
func LiveContentComponent(s *Store) ui.Node {
	return liveComponent(s, BuildContent)
}

func liveComponent(s *Store, build func(Page) ui.Node) ui.Node {
	if s == nil {
		return ui.CreateElement(func() ui.Node { return build(Page{}) })
	}
	return ui.CreateElement(func() ui.Node {
		revision := ui.UseState(0)
		// Subscribe before requesting a catch-up render. An RPC may finish
		// after Page() was read but before this effect is installed, so a
		// subscription alone can leave the initial loading snapshot stuck.
		// The store dependency also releases/rebinds when the source changes.
		ui.UseEffect(func() func() {
			refresh := func() {
				// Service answers can arrive while the product router is
				// committing a new leaf. Post the update to the framework's
				// frame inbox so it targets the committed fiber, not the leaf
				// being replaced.
				ui.PostAsync(func() {
					// The native review dialog owns its pending presentation.
					// Reconciling the page here strips the browser-managed open
					// state and hides the only visible progress control. Re-read
					// inside the queued frame as earlier updates can be pending.
					if reviewActionPending(s.Page()) {
						return
					}
					revision.Update(func(previous int) int { return previous + 1 })
				})
			}
			unsubscribe := s.Subscribe(refresh)
			refresh()
			return unsubscribe
		}, s)
		return build(s.Page())
	})
}

// The action ids Wire hands the client's submit callback for the two
// workforce operations. They are constants rather than literals scattered
// through the file so the client lane and this one cannot disagree about the
// spelling of a string neither compiler checks.
const (
	actionPropose       = "propose"
	actionProposeFor    = "propose-for"
	actionCreateWorker  = "create-worker"
	selectWorkerHrefPre = "#/journeys?worker="
)

// selectWorkerHref is the route Wire navigates to when a client supplied a
// nav but no SelectWorker: picking a person is a route change like any
// other, so the selection survives a reload and can be linked to.
func selectWorkerHref(ref string) string { return selectWorkerHrefPre + ref }

// WireOptions carries the live callbacks Wire cannot derive from the four
// arguments every client already passes. It exists so the People table could
// be wired without changing Wire's signature: the existing
// Wire(store, page, navigate, submit) call still compiles and still binds
// everything it used to.
type WireOptions struct {
	// SelectWorker is called with a WorkerCard.Ref when the reader picks a
	// row. When it is nil and nav is not, Wire falls back to navigating to
	// that row's selection route, so the People table works on a client that
	// supplied only the original two callbacks.
	SelectWorker func(ref string)
}

// WireOption configures WireOptions. A nil option is ignored rather than
// panicking, so a client may pass one conditionally.
type WireOption func(*WireOptions)

// WithSelectWorker binds the People table's row selection to fn instead of
// to a route change.
func WithSelectWorker(fn func(ref string)) WireOption {
	return func(o *WireOptions) { o.SelectWorker = fn }
}

// Wire returns a Page with every live callback the renderer understands
// already bound to s: field changes, navigation, opening a journey, picking
// an employee, and every kind of submission. The client supplies only the
// things it alone knows -- how to navigate and how to call the engine.
//
// It is the one place the plumbing lives, so a client never hand-wires a
// closure per field, per row or per action and cannot forget one.
//
// The People callbacks are bound as: OnSelect to SelectWorker (or, failing
// that, to nav on the row's selection route), OnPropose to submit with the
// action id "propose-for" and the single value worker_ref, and the New
// employee form to submit with the action id "create-worker".
func Wire(s *Store, p Page, nav func(href string), submit func(actionID string, values map[string]string), opts ...WireOption) Page {
	if s == nil {
		return p
	}
	var o WireOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	p.OnFieldChange = s.SetValue

	for i := range p.Nav {
		href := p.Nav[i].Href
		if nav != nil {
			p.Nav[i].OnNavigate = func() { nav(href) }
		}
	}
	if p.List != nil {
		for i := range p.List.Journeys {
			href := p.List.Journeys[i].Href
			if nav != nil {
				p.List.Journeys[i].OnOpen = func() { nav(href) }
			}
		}
		// The overview renders Groups when present, not Journeys. Bind the
		// displayed copies too or a click falls through to the fragment href
		// while the host history router remains on the list.
		for group := range p.List.Groups {
			for i := range p.List.Groups[group].Journeys {
				href := p.List.Groups[group].Journeys[i].Href
				if nav != nil {
					p.List.Groups[group].Journeys[i].OnOpen = func() { nav(href) }
				}
			}
		}
		if submit != nil {
			p.List.Form.OnSubmit = func(values map[string]string) { submit(actionPropose, values) }
		}
		wirePeople(p.List.People, nav, submit, o)
	}
	if p.Proposal != nil && submit != nil {
		p.Proposal.Form.OnSubmit = func(values map[string]string) { submit(actionPropose, values) }
	}
	if p.Proposal != nil && nav != nil {
		href := p.Proposal.JourneysLink.Href
		p.Proposal.JourneysLink.OnNavigate = func() { nav(href) }
	}
	if p.Detail != nil && submit != nil {
		for i := range p.Detail.Actions {
			id := p.Detail.Actions[i].ID
			p.Detail.Actions[i].OnSubmit = func(values map[string]string) { submit(id, values) }
		}
	}
	return p
}

// wirePeople binds the workforce panel. Each closure captures its own row's
// ref rather than the loop variable, which is the whole reason this is a
// function and not four lines inlined above.
func wirePeople(v *PeopleView, nav func(href string), submit func(actionID string, values map[string]string), o WireOptions) {
	if v == nil {
		return
	}
	for i := range v.Workers {
		ref := v.Workers[i].Ref
		switch {
		case o.SelectWorker != nil:
			selectWorker := o.SelectWorker
			v.Workers[i].OnSelect = func() { selectWorker(ref) }
		case nav != nil:
			href := selectWorkerHref(ref)
			v.Workers[i].OnSelect = func() { nav(href) }
		}
		if submit != nil {
			v.Workers[i].OnPropose = func() {
				submit(actionProposeFor, map[string]string{"worker_ref": ref})
			}
		}
	}
	if submit != nil {
		v.Form.OnSubmit = func(values map[string]string) { submit(actionCreateWorker, values) }
	}
}
