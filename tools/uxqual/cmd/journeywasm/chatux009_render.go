package main

import (
	"sync"
	"time"
)

// chatux009RenderGrace bounds how long a first render is treated as on its way.
// A page that is left before it mounts must not leave later changes unable to
// reach the route.
const chatux009RenderGrace = 15 * time.Second

var chatux009Render struct {
	sync.Mutex
	since time.Time
}

// chatux009BeginRender records that a route load for Chat has started, so the
// first render of the page is on its way.
func chatux009BeginRender() {
	chatux009Render.Lock()
	chatux009Render.since = time.Now()
	chatux009Render.Unlock()
}

// chatux009EndRender records that the page is on screen.
func chatux009EndRender() {
	chatux009Render.Lock()
	chatux009Render.since = time.Time{}
	chatux009Render.Unlock()
}

// chatux009Hold marks the one open that is the page's first, so that open reads
// the channel's to-do list, widgets and poll after its messages instead of
// beside them.
var chatux009Hold struct {
	sync.Mutex
	id string
}

func chatux009HoldFor(id string) {
	chatux009Hold.Lock()
	chatux009Hold.id = id
	chatux009Hold.Unlock()
}

// chatux009TakeHold reports whether the open of conversation id is the first
// open of the page, and clears the mark.
func chatux009TakeHold(id string) bool {
	chatux009Hold.Lock()
	defer chatux009Hold.Unlock()
	if id == "" || chatux009Hold.id != id {
		return false
	}
	chatux009Hold.id = ""
	return true
}

// chatux009RenderPending reports whether the page's first render has been
// started and has not happened yet.
func chatux009RenderPending() bool {
	chatux009Render.Lock()
	defer chatux009Render.Unlock()
	return !chatux009Render.since.IsZero() && time.Since(chatux009Render.since) < chatux009RenderGrace
}
