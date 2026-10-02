package chat

import (
	"testing"
	"time"
)

// A typed address can be shared with no position at all: the lookup port may
// be unavailable, and the words are still a useful message.
func TestTodo_CHATMAP_004_TypedAddress(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	typed := LocationPlace{Source: LocationAddress, Address: "12 Quay Street, back gate", Precision: "exact", CapturedAt: now}
	got, err := PrepareLocation(typed, now)
	if err != nil || got.Position != nil || got.Address != typed.Address {
		t.Fatal("typed address without a position", got, err)
	}
	for name, change := range map[string]func(*LocationPlace){
		"empty text":  func(p *LocationPlace) { p.Address = "  " },
		"approximate": func(p *LocationPlace) { p.Precision = "approximate"; p.ApproximateRadius = 500 },
		"device":      func(p *LocationPlace) { p.Source = LocationDevice },
		"pin":         func(p *LocationPlace) { p.Source = LocationPin },
	} {
		p := typed
		change(&p)
		if _, err := PrepareLocation(p, now); err == nil {
			t.Fatal("accepted without a position:", name)
		}
	}
	// It never appears on the crew map: that is for live device positions.
	f := newLiveFixture()
	f.repo.v = LocationShare{ID: "x", TenantID: "tt", ConversationID: "c", PostID: "m", SharerID: "alice", SharerTenantID: "tt", Place: got}
	f.store.post.References = []Reference{{Kind: LocationReference, TenantID: "tt", ID: "x"}}
	view, err := f.live.LiveMap(t.Context(), f.alice, "tt", "c")
	if err != nil || len(view.Shares) != 0 {
		t.Fatal("an address appeared on the crew map", view, err)
	}
}
