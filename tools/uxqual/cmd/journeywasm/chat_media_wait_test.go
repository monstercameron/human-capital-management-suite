package main

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestTodo_CHATBUG_031_MediaWait: the wait for the visible-message sync (and for
// a shared grant) ends when the cache changes, uses one timer for its limit, and
// stops when cancelled. The review browser pane stretches every timer it fires,
// so eighty chained 25 ms sleeps took long enough to outlive the loader's
// watchdog and the tile was marked failed.
func TestTodo_CHATBUG_031_MediaWait(t *testing.T) {
	cache := newChatMediaGrants()
	never := func() bool { return false }

	// A change the cache announces ends the wait at once, long before the limit.
	started := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cache.SetWanted([]string{"gif"})
	}()
	if !cache.WaitUntil(func() bool { return cache.Wanted("gif") }, 5*time.Second, never) {
		t.Fatal("the wait did not see the sync that arrived")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the wait ran %v although the sync announced itself after 20ms", elapsed)
	}

	// A grant put by another instance ends a grant wait the same way.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cache.Put("gif", chatMediaGrant{Token: "t"})
	}()
	if !cache.WaitUntil(func() bool { _, ok := cache.Get("gif", time.Now()); return ok }, 5*time.Second, never) {
		t.Fatal("the wait did not see the grant that was put")
	}

	// Nothing changes: the limit ends it, with the condition's last answer.
	started = time.Now()
	if cache.WaitUntil(func() bool { return cache.Wanted("absent") }, 60*time.Millisecond, never) {
		t.Fatal("the wait invented a sync")
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("the limit took %v, want about 60ms", elapsed)
	}

	// A condition already true returns without waiting; a cancelled wait returns false.
	var checks atomic.Int32
	if !cache.WaitUntil(func() bool { checks.Add(1); return true }, time.Hour, never) || checks.Load() != 1 {
		t.Fatalf("a true condition was checked %d times", checks.Load())
	}
	if cache.WaitUntil(func() bool { return false }, time.Hour, func() bool { return true }) {
		t.Fatal("a cancelled wait reported success")
	}
}
