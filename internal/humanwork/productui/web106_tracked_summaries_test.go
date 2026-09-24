package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-106: tracked-request summaries. UF-005 lets a
// participant track a long-running intent, but nothing
// projects the tracking surface: pages hand-pick status, due,
// and open/closed per row, so the status summary disagrees
// with the governed lists by convention. The compiler needs
// the derived-only projection — one entry per item in
// admission order carrying exactly the tracking fields, with
// openness derived from terminality — adding no truth.
func TestTodo_WEB_106(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Status: "In progress", Due: "2026-09-30"},
		{ID: "b", Status: "Awaiting approval", Due: "2026-10-01"},
		{ID: "c", Status: "Done", Terminal: true},
	}
	summaries := SummarizeTracked(items)
	if len(summaries) != 3 {
		t.Fatalf("summaries = %+v", summaries)
	}
	if summaries[0].ID != "a" || summaries[0].Status != "In progress" || summaries[0].Due != "2026-09-30" || !summaries[0].Open {
		t.Fatalf("tracking summary = %+v", summaries[0])
	}
	if summaries[2].Open {
		t.Fatalf("terminal item tracked open: %+v", summaries[2])
	}
	if len(SummarizeTracked(nil)) != 0 {
		t.Fatal("nil stream tracks requests")
	}

	// Openness reconciles with the governed lists.
	var open, closed int
	for _, summary := range summaries {
		if summary.Open {
			open++
		} else {
			closed++
		}
	}
	if open != len(OpenWorkItems(items)) || closed != len(RecentWork(items)) {
		t.Fatalf("tracking %d/%d vs lists %d/%d", open, closed, len(OpenWorkItems(items)), len(RecentWork(items)))
	}
}

// Golden: tracking outcomes over admitted streams.
func TestTodo_WEB_106_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a", Status: "In progress", Due: "2026-09-30"}},
		{{ID: "a", Status: "Awaiting approval"}, {ID: "b", Status: "Blocked", Due: "2026-10-02"}},
		{{ID: "x", Status: "Done", Terminal: true}, {ID: "y"}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, summary := range SummarizeTracked(stream) {
			fmt.Fprintf(&builder, "%s|%s|%s|%t\x00", summary.ID, summary.Status, summary.Due, summary.Open)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "dfc6caa2559a5c8a0b29af80767df01965df4339b143114cc86dc3eceda9cfbf"
	if got != want {
		t.Fatalf("tracking digest = %s, want %s", got, want)
	}
}

// Browser: tracking over stream patterns keeps admission
// order deterministically.
func TestTodo_WEB_106_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a", Status: "In progress"}, {ID: "b", Status: "Done", Terminal: true}},
		{{ID: "c", Status: "Blocked", Due: "2026-11-01"}, {ID: "d"}},
	}
	for _, stream := range patterns {
		first := SummarizeTracked(stream)
		second := SummarizeTracked(stream)
		if len(first) != len(stream) {
			t.Fatalf("tracking drops items: %+v from %+v", first, stream)
		}
		for i, summary := range first {
			if summary.ID != stream[i].ID || summary.Open == stream[i].Terminal {
				t.Fatalf("tracking misprojects %+v", summary)
			}
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("tracking is nondeterministic")
		}
	}
}

// Conformance: one entry per item, fields pass through,
// openness reconciles, stability.
func TestTodo_WEB_106_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a", Status: "S", Due: "D"},
		{ID: "b", Status: "T", Due: "E", Terminal: true},
	}
	summaries := SummarizeTracked(stream)
	if len(summaries) != len(stream) {
		t.Fatal("tracking drops or doubles items")
	}
	if summaries[0].Status != "S" || summaries[0].Due != "D" || !summaries[0].Open {
		t.Fatalf("tracking rewrites items: %+v", summaries[0])
	}
	var open, closed int
	for _, summary := range summaries {
		if summary.Open {
			open++
		} else {
			closed++
		}
	}
	if open != len(OpenWorkItems(stream)) || closed != len(RecentWork(stream)) {
		t.Fatal("tracking disagrees with the governed lists")
	}
	if !reflect.DeepEqual(summaries, SummarizeTracked(stream)) {
		t.Fatal("tracking is unstable")
	}
}
