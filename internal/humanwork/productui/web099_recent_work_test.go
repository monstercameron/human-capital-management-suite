package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-099: recent-work continuity. Terminal items have
// no governed list for the recent-work slot: completed work
// scatters by caller convention while attention (WEB-098)
// covers only the active side. The lifecycle needs the mirror
// filter — terminal admitted items in admission order — so the
// admitted stream partitions exactly: attention plus recent
// work, nothing lost, nothing shared. Recency stays
// server-ranked like attention order.
func TestTodo_WEB_099(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Title: "Active one"},
		{ID: "b", Title: "Done one", Terminal: true},
		{ID: "c", Title: "Active two"},
		{ID: "d", Title: "Rejected one", Terminal: true},
	}
	recent := RecentWork(items)
	if len(recent) != 2 || recent[0].ID != "b" || recent[1].ID != "d" {
		t.Fatalf("recent work = %+v", recent)
	}
	if len(RecentWork(nil)) != 0 {
		t.Fatal("nil stream lists recent work")
	}
	if len(RecentWork([]WorkItem{{ID: "x"}})) != 0 {
		t.Fatal("active-only stream lists recent work")
	}

	// The admitted stream partitions exactly.
	attention := OpenWorkItems(items)
	seen := map[string]bool{}
	for _, item := range attention {
		seen[item.ID] = true
	}
	for _, item := range recent {
		if seen[item.ID] {
			t.Fatalf("item %q listed twice", item.ID)
		}
		seen[item.ID] = true
	}
	for _, item := range items {
		if !seen[item.ID] {
			t.Fatalf("item %q listed nowhere", item.ID)
		}
	}

	recent[0].Title = "mutated"
	again := RecentWork(items)
	if again[0].Title != "Done one" || items[1].Title != "Done one" {
		t.Fatal("recent work aliases its input")
	}
}

// Golden: recent-work outcomes over admitted streams.
func TestTodo_WEB_099_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a"}, {ID: "b", Terminal: true}, {ID: "c"}},
		{{ID: "x", Terminal: true}, {ID: "y", Terminal: true}},
		{{ID: "solo"}},
		{{ID: "dup", Terminal: true}, {ID: "dup", Terminal: true}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, item := range RecentWork(stream) {
			builder.WriteString(item.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "7bc97e45c3050bfb14818ff6d538f3b5d0c3e5944e1ef3508ab81d389f4fafaf"
	if got != want {
		t.Fatalf("recent digest = %s, want %s", got, want)
	}
}

// Browser: recent work over admitted-stream patterns preserves
// admission order deterministically.
func TestTodo_WEB_099_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a", Terminal: true}, {ID: "b"}, {ID: "c", Terminal: true}},
		{{ID: "c"}, {ID: "b", Terminal: true}, {ID: "a"}},
		{{ID: "z", Terminal: true}, {ID: "y"}, {ID: "x", Terminal: true}, {ID: "w"}, {ID: "v", Terminal: true}},
	}
	for _, stream := range patterns {
		first := RecentWork(stream)
		second := RecentWork(stream)
		previous := -1
		for _, item := range first {
			if !item.Terminal {
				t.Fatalf("active item in recent work: %+v", item)
			}
			current := -1
			for i, candidate := range stream {
				if candidate.ID == item.ID && candidate.Terminal {
					current = i
					break
				}
			}
			if current < 0 || current < previous {
				t.Fatalf("admission order broken: %+v from %+v", first, stream)
			}
			previous = current
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("recent work is nondeterministic")
		}
	}
}

// Conformance: the partition holds item-for-item, fields pass
// through, stability.
func TestTodo_WEB_099_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a", Title: "T", Person: "P", CompletedAt: "C"},
		{ID: "b", Title: "U", Person: "Q", CompletedAt: "D", Terminal: true},
	}
	if len(OpenWorkItems(stream))+len(RecentWork(stream)) != len(stream) {
		t.Fatal("attention plus recent drops or doubles items")
	}
	recent := RecentWork(stream)
	if len(recent) != 1 || !reflect.DeepEqual(recent[0], stream[1]) {
		t.Fatalf("recent work rewrites items: %+v", recent)
	}
	first := RecentWork(stream)
	second := RecentWork(stream)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("recent work is unstable")
	}
}
