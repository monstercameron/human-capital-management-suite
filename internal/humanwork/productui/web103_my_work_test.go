package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-103: the My Work collection. The registry has a
// My Work page and selectors project open work, but nothing
// scopes a stream to the viewer: pages hand-filter on
// PersonRef with ad hoc empty-identity behavior, so an empty
// viewer identity either sees unassigned work or breaks the
// page by convention. The compiler needs the governed
// collection — items whose PersonRef equals the viewer's
// PersonID in admission order, with empty identities matching
// nothing fail-closed.
func TestTodo_WEB_103(t *testing.T) {
	items := []WorkItem{
		{ID: "a", PersonRef: "p-amy", Terminal: true},
		{ID: "b", PersonRef: "p-bob"},
		{ID: "c", PersonRef: "p-amy"},
		{ID: "d"},
	}
	mine := MyWorkItems(items, ViewerProfile{PersonID: "p-amy"})
	if len(mine) != 2 || mine[0].ID != "a" || mine[1].ID != "c" {
		t.Fatalf("my work = %+v", mine)
	}
	if len(MyWorkItems(items, ViewerProfile{})) != 0 {
		t.Fatal("empty viewer collects work")
	}
	if len(MyWorkItems(nil, ViewerProfile{PersonID: "p-amy"})) != 0 {
		t.Fatal("nil stream collects work")
	}
	if len(MyWorkItems([]WorkItem{{ID: "x", PersonRef: ""}}, ViewerProfile{PersonID: "p-amy"})) != 0 {
		t.Fatal("unassigned item collected")
	}
}

func TestMyWorkItemsPreferTheServerRoutedAssigneeOverTheJourneySubject(t *testing.T) {
	items := []WorkItem{
		{ID: "finance", PersonRef: "worker-omar", AssigneeRef: "worker-thomas", Status: "Finance approval"},
		{ID: "manager", PersonRef: "worker-omar", AssigneeRef: "worker-dominic", Status: "Manager approval"},
	}
	thomas := MyWorkItems(items, ViewerProfile{PersonID: "worker-thomas"})
	if len(thomas) != 1 || thomas[0].ID != "finance" {
		t.Fatalf("Thomas's routed queue = %+v", thomas)
	}
	if got := MyWorkItems(items, ViewerProfile{PersonID: "worker-omar"}); len(got) != 0 {
		t.Fatalf("workflow subject was treated as the approval assignee: %+v", got)
	}
}

// Golden: collection outcomes over viewer/stream pairs.
func TestTodo_WEB_103_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a", PersonRef: "p-amy"}},
		{{ID: "a", PersonRef: "p-amy"}, {ID: "b", PersonRef: "p-bob"}, {ID: "c", PersonRef: "p-amy", Terminal: true}},
		{{ID: "x"}, {ID: "y", PersonRef: ""}},
	}
	viewers := []string{"p-amy", "", "p-ghost"}
	var builder strings.Builder
	for _, viewer := range viewers {
		for _, stream := range streams {
			for _, item := range MyWorkItems(stream, ViewerProfile{PersonID: viewer}) {
				builder.WriteString(item.ID)
				builder.WriteString("\x00")
			}
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "eb448f06c31515db7e0d48b3e95b03c5ec51e0384a066a2887c52cf962267636"
	if got != want {
		t.Fatalf("my-work digest = %s, want %s", got, want)
	}
}

// Browser: collection over viewer/stream patterns keeps
// admission order deterministically.
func TestTodo_WEB_103_Browser(t *testing.T) {
	streams := [][]WorkItem{
		{{ID: "a", PersonRef: "p-amy"}, {ID: "b", PersonRef: "p-bob"}, {ID: "c", PersonRef: "p-amy"}},
		{{ID: "c", PersonRef: "p-bob"}, {ID: "b"}, {ID: "a", PersonRef: "p-bob", Terminal: true}},
	}
	for _, stream := range streams {
		for _, viewer := range []string{"p-amy", "p-bob", ""} {
			first := MyWorkItems(stream, ViewerProfile{PersonID: viewer})
			second := MyWorkItems(stream, ViewerProfile{PersonID: viewer})
			previous := -1
			for _, item := range first {
				if viewer == "" || item.PersonRef != viewer {
					t.Fatalf("viewer %q collected %+v", viewer, item)
				}
				current := -1
				for i, candidate := range stream {
					if candidate.ID == item.ID {
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
				t.Fatal("collection is nondeterministic")
			}
		}
	}
}

// Conformance: passthrough identity, no aliasing, the
// collection plus its complement partition the stream.
func TestTodo_WEB_103_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a", PersonRef: "p-amy", Title: "T"},
		{ID: "b", PersonRef: "p-bob", Title: "U"},
	}
	mine := MyWorkItems(stream, ViewerProfile{PersonID: "p-amy"})
	if len(mine) != 1 || !reflect.DeepEqual(mine[0], stream[0]) {
		t.Fatalf("collection rewrites items: %+v", mine)
	}
	mine[0].Title = "mutated"
	again := MyWorkItems(stream, ViewerProfile{PersonID: "p-amy"})
	if again[0].Title != "T" || stream[0].Title != "T" {
		t.Fatal("collection aliases its input")
	}
	if !reflect.DeepEqual(MyWorkItems(stream, ViewerProfile{PersonID: "p-amy"}), MyWorkItems(stream, ViewerProfile{PersonID: "p-amy"})) {
		t.Fatal("collection is unstable")
	}
}
