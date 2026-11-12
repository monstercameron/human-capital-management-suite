package chatui

import (
	"strings"
	"sync"
)

// Issue 3827: scrolling a conversation flickered. The timeline's window is in
// virtual.go; this file holds the rest of what a scroll must leave alone.

// chatPhotoMemory is what the page has learned about portrait addresses this
// session. A row that is drawn again (the windowed timeline drops and redraws
// rows) asks it instead of the network: an address that failed is not put in
// the page again, so it is neither requested a second time nor shown for a
// moment before the initials come back.
type chatPhotoMemory struct {
	mu     sync.Mutex
	failed map[string]bool
}

var chatPhotos chatPhotoMemory

// chatPhotoResolved records how a portrait address ended: loaded, or failed
// and left to the initials beneath it.
func chatPhotoResolved(url string, loaded bool) {
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	chatPhotos.mu.Lock()
	defer chatPhotos.mu.Unlock()
	if loaded {
		delete(chatPhotos.failed, url)
		return
	}
	if chatPhotos.failed == nil {
		chatPhotos.failed = map[string]bool{}
	}
	chatPhotos.failed[url] = true
}

// chatPhotoURL is the portrait address to draw: the address itself, or nothing
// when it is known to have failed, which leaves the avatar its initials.
func chatPhotoURL(url string) string {
	url = strings.TrimSpace(url)
	chatPhotos.mu.Lock()
	defer chatPhotos.mu.Unlock()
	if chatPhotos.failed[url] {
		return ""
	}
	return url
}

// chatScrollSettleMs is how long after the last scroll event a timeline counts
// as still moving.
const chatScrollSettleMs = 200.0

// chatPointerGate holds back the row a resting mouse is over while a timeline
// scrolls beneath it. Each row that passed under the pointer used to become the
// hovered row, and that state belongs to the whole workspace: every one drew
// the composer, the command list and the sidebar again. The gate lets the row
// be named once, when the scroll has settled.
type chatPointerGate struct {
	scrolledAt float64
	scrolled   bool
}

// scroll notes a scroll event at now (milliseconds).
func (g *chatPointerGate) scroll(now float64) { g.scrolledAt, g.scrolled = now, true }

// wait is how many milliseconds a change of the hovered row still has to wait
// at now; zero means it applies at once.
func (g *chatPointerGate) wait(now float64) float64 {
	if !g.scrolled {
		return 0
	}
	return max(0, g.scrolledAt+chatScrollSettleMs-now)
}

// chatSendButtonStale reports whether the Send button has to be written for a
// composer that is ready or not. The page sweep that keeps fields in step runs
// after every change to the timeline, and it wrote both attributes each time.
func chatSendButtonStale(disabled bool, ariaDisabled string, ready bool) bool {
	return disabled == ready || ariaDisabled != boolString(!ready)
}
