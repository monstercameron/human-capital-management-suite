package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestAgentUXAmbient_ReadIsAskedOncePerOpen: Chat refreshes the agent directory
// of the open conversation several times while it loads, and the ambient read
// beside it was asked each time. It is asked once while its answer is fresh,
// never twice at the same moment, and asked again for a stale answer, a
// conversation that was cut short and another person.
func TestAgentUXAmbient_ReadIsAskedOncePerOpen(t *testing.T) {
	var cache ambientReadCache
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	if _, have, fetch := cache.begin("general", now); have || !fetch {
		t.Fatal("the first read of a conversation was not asked")
	}
	if _, _, fetch := cache.begin("general", now); fetch {
		t.Fatal("a second read was asked while the first was in flight")
	}
	answer := ambientagents.Snapshot{Grants: []ambientagents.Grant{{Agent: "task-catcher", Enabled: true}}}
	cache.finish("general", answer, nil, now)
	got, have, fetch := cache.begin("general", now.Add(time.Second))
	if fetch || !have || len(got.Grants) != 1 {
		t.Fatalf("a fresh answer was not reused: %+v %v %v", got, have, fetch)
	}
	if _, _, fetch := cache.begin("random", now); !fetch {
		t.Fatal("another conversation was served from this one's answer")
	}
	if _, _, fetch := cache.begin("general", now.Add(ambientReadFreshness+time.Second)); !fetch {
		t.Fatal("a stale answer was not asked again")
	}
	// A failure keeps the last good answer and is not repeated at once.
	cache.finish("general", ambientagents.Snapshot{}, errors.New("not found"), now.Add(time.Minute))
	if got, have, fetch := cache.begin("general", now.Add(time.Minute+time.Second)); fetch || !have || len(got.Grants) != 1 {
		t.Fatalf("a failed read blanked the last good answer or was repeated at once: %+v %v %v", got, have, fetch)
	}
	// A read that was cut short is not an answer.
	cache.finish("random", ambientagents.Snapshot{}, context.Canceled, now)
	if _, _, fetch := cache.begin("random", now.Add(time.Second)); !fetch {
		t.Fatal("a cancelled read was treated as an answer")
	}
	if ambientReadKey(journeyclient.Config{Tenant: "t1", Subject: "alice"}, "general") == ambientReadKey(journeyclient.Config{Tenant: "t1", Subject: "bob"}, "general") {
		t.Fatal("two people share one cached answer")
	}
}
