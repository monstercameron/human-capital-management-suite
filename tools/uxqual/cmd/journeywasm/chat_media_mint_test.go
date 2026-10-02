package main

import (
	"testing"
	"time"
)

// TestTodo_CHATBUG_009_MintClaim: a re-render replaces the element whose
// request was minting a grant and aborts it. The claim must end with the abort,
// or every later request for the artifact finds the claim held, waits, and
// reports the preview as failed.
func TestTodo_CHATBUG_009_MintClaim(t *testing.T) {
	cache := newChatMediaGrants()
	cache.SetWanted([]string{"gif"})
	now := time.Now()

	epoch, claimed := cache.ClaimEpoch("gif", now)
	if !claimed {
		t.Fatal("first request could not claim the artifact")
	}
	if _, ok := cache.MintUnderClaim("gif", epoch, func() (string, time.Time, bool) { return "", time.Time{}, false }); ok {
		t.Fatal("an aborted mint produced a grant")
	}
	epoch, claimed = cache.ClaimEpoch("gif", now)
	if !claimed {
		t.Fatal("the claim of an aborted mint is still held: the replacement element can never fetch")
	}
	grant, ok := cache.MintUnderClaim("gif", epoch, func() (string, time.Time, bool) { return "token", now.Add(time.Hour), true })
	if !ok || grant.Token != "token" {
		t.Fatalf("replacement element's mint = %+v, %v", grant, ok)
	}
	if _, claimed = cache.ClaimEpoch("gif", now); claimed {
		t.Fatal("a cached grant was claimed again: the preview would be fetched twice")
	}
	if got, ok := cache.Get("gif", now); !ok || got.Token != "token" {
		t.Fatalf("grant was not cached: %+v, %v", got, ok)
	}
}

// A mint that resolves after the artifact left the visible set must not leave
// a claim behind either.
func TestTodo_CHATBUG_009_MintClaimReleasedWhenNotCached(t *testing.T) {
	cache := newChatMediaGrants()
	cache.SetWanted([]string{"gif"})
	epoch, claimed := cache.ClaimEpoch("gif", time.Now())
	if !claimed {
		t.Fatal("could not claim")
	}
	cache.SetWanted([]string{"other"})
	if _, ok := cache.MintUnderClaim("gif", epoch, func() (string, time.Time, bool) { return "token", time.Time{}, true }); ok {
		t.Fatal("a grant for an artifact no longer visible was cached")
	}
	cache.SetWanted([]string{"gif"})
	if _, claimed = cache.ClaimEpoch("gif", time.Now()); !claimed {
		t.Fatal("artifact is unclaimable after it came back into view")
	}
}
