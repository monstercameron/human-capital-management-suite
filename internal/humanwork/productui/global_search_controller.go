package productui

import (
	"sync"
	"time"
)

// globalSearchDebounceDelay is how long typing must pause before results are
// recomputed. The field itself never waits: every keystroke lands in the
// browser-owned edit buffer immediately.
const globalSearchDebounceDelay = 250 * time.Millisecond

// globalSearchController is the shared search controller (UXLIVE-029). It
// keeps three things apart that used to be one controlled value:
//
//   - the query: the local edit buffer, which is authoritative while the
//     reader types and which nothing but the reader's own input may change;
//   - the generation: a counter bumped by every edit, which fences results;
//   - the results: the answer for exactly one generation.
//
// A results answer is accepted only for the generation and exact query that
// requested it. It never writes the query, so a late or out-of-order answer
// cannot take a character back out of the field -- the "Amara" -> "Amaa"
// defect was a rerender writing an older value into the input.
type globalSearchController struct {
	mu                sync.Mutex
	buffer            string
	generation        uint64
	results           []GlobalSearchItem
	resultsQuery      string
	resultsGeneration uint64
	resolved          bool
	// remote holds what Chat answered for remoteQuery (CHATSEARCH-002). It is
	// listed after the local results of the same query and never without them.
	remote        []GlobalSearchItem
	remoteQuery   string
	remoteVersion int
}

func newGlobalSearchController(seed string) *globalSearchController {
	return &globalSearchController{buffer: seed}
}

// Edit records the reader's current field contents and returns the new
// generation. It is the only way the query changes, apart from Clear.
func (controller *globalSearchController) Edit(value string) uint64 {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if value == controller.buffer {
		return controller.generation
	}
	controller.buffer = value
	controller.generation++
	return controller.generation
}

// Clear empties the query after a destination was chosen.
func (controller *globalSearchController) Clear() uint64 {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.buffer = ""
	controller.generation++
	controller.results, controller.resultsQuery, controller.resolved = nil, "", false
	controller.remote, controller.remoteQuery = nil, ""
	return controller.generation
}

// ResolveRemote offers Chat's results for one generation, under the same fence
// as Resolve: an answer for a query the reader has since changed is dropped.
// It returns a number that changes with every accepted answer, for the caller
// to render again with, and whether this one was accepted.
func (controller *globalSearchController) ResolveRemote(generation uint64, query string, results []GlobalSearchItem) (int, bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if generation != controller.generation || query != controller.buffer {
		return controller.remoteVersion, false
	}
	controller.remote = append([]GlobalSearchItem(nil), results...)
	controller.remoteQuery = query
	controller.remoteVersion++
	return controller.remoteVersion, true
}

// Query returns the edit buffer.
func (controller *globalSearchController) Query() string {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.buffer
}

// Request returns the generation and query a results computation must answer.
func (controller *globalSearchController) Request() (uint64, string) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.generation, controller.buffer
}

// Resolve offers results for one generation. It reports whether they were
// accepted: an answer for a superseded generation, or for a query that is not
// exactly the current buffer, is discarded and the previous results stay.
func (controller *globalSearchController) Resolve(generation uint64, query string, results []GlobalSearchItem) bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if generation != controller.generation || query != controller.buffer {
		return false
	}
	controller.results = append([]GlobalSearchItem(nil), results...)
	controller.resultsQuery = query
	controller.resultsGeneration = generation
	controller.resolved = true
	return true
}

// Results returns the last accepted results and whether they answer the
// current generation. Stale results stay visible (marked busy) until the
// current generation's answer arrives, so the list does not flash empty.
func (controller *globalSearchController) Results() (results []GlobalSearchItem, query string, current bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	results = append([]GlobalSearchItem(nil), controller.results...)
	// Chat's results follow the local results they were asked with. While the
	// reader types on, both stay as they are until the new query is answered.
	if controller.resolved && controller.remoteQuery == controller.resultsQuery {
		results = append(results, controller.remote...)
	}
	return results, controller.resultsQuery,
		controller.resolved && controller.resultsGeneration == controller.generation
}
