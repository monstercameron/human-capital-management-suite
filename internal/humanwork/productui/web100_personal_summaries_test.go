package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-100: personal-essential summaries. Sections need
// a summaries slot, but nothing derives the per-person rollup
// from the admitted stream: callers hand-count active and
// completed items per person, and nothing reconciles those
// counts with the attention (WEB-098) and recent-work
// (WEB-099) lists. The compiler needs the derived-only rollup
// — one entry per person present, in order of first
// appearance, with active and completed counts — so the
// summaries slot has governed content that adds no new truth.
func TestTodo_WEB_100(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Person: "amy"},
		{ID: "b", Person: "bob", Terminal: true},
		{ID: "c", Person: "amy", Terminal: true},
		{ID: "d", Person: "bob"},
		{ID: "e", Person: "amy"},
	}
	summaries := SummarizePersonal(items)
	if len(summaries) != 2 {
		t.Fatalf("summaries = %+v", summaries)
	}
	if summaries[0].Person != "amy" || summaries[0].Active != 2 || summaries[0].Completed != 1 {
		t.Fatalf("amy summary = %+v", summaries[0])
	}
	if summaries[1].Person != "bob" || summaries[1].Active != 1 || summaries[1].Completed != 1 {
		t.Fatalf("bob summary = %+v", summaries[1])
	}
	if len(SummarizePersonal(nil)) != 0 {
		t.Fatal("nil stream summarizes people")
	}

	// The rollup reconciles with the governed lists.
	var active, completed int
	for _, summary := range summaries {
		active += summary.Active
		completed += summary.Completed
	}
	if active != len(OpenWorkItems(items)) || completed != len(RecentWork(items)) {
		t.Fatalf("rollup %d/%d vs lists %d/%d", active, completed, len(OpenWorkItems(items)), len(RecentWork(items)))
	}
}

// Golden: summary outcomes over admitted streams.
func TestTodo_WEB_100_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a", Person: "amy"}},
		{{ID: "a", Person: "amy"}, {ID: "b", Person: "amy", Terminal: true}, {ID: "c", Person: "bob"}},
		{{ID: "x", Person: "zed", Terminal: true}, {ID: "y", Person: "zed", Terminal: true}},
		{{ID: "solo"}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, summary := range SummarizePersonal(stream) {
			fmt.Fprintf(&builder, "%s:%d:%d\x00", summary.Person, summary.Active, summary.Completed)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "80b8ce89872a7da7d525d879ce6e3498c9c04728f33ac4fc41a3acb121c895a7"
	if got != want {
		t.Fatalf("summary digest = %s, want %s", got, want)
	}
}

// Browser: summaries over admitted-stream patterns preserve
// first-appearance order deterministically.
func TestTodo_WEB_100_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a", Person: "amy"}, {ID: "b", Person: "bob"}, {ID: "c", Person: "amy", Terminal: true}},
		{{ID: "c", Person: "bob"}, {ID: "b", Person: "amy"}, {ID: "a", Person: "bob", Terminal: true}},
		{{ID: "p", Person: ""}, {ID: "q"}},
	}
	for _, stream := range patterns {
		first := SummarizePersonal(stream)
		second := SummarizePersonal(stream)
		seen := map[string]bool{}
		for _, summary := range first {
			if seen[summary.Person] {
				t.Fatalf("person listed twice: %+v", first)
			}
			seen[summary.Person] = true
		}
		for _, item := range stream {
			if !seen[item.Person] {
				t.Fatalf("person %q missing from %+v", item.Person, first)
			}
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("summaries are nondeterministic")
		}
	}
}

// Conformance: totals reconcile item-for-item with the
// attention and recent-work lists; stability.
func TestTodo_WEB_100_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a", Person: "amy"},
		{ID: "b", Person: "bob", Terminal: true},
		{ID: "c", Person: "amy", Terminal: true},
	}
	var active, completed int
	for _, summary := range SummarizePersonal(stream) {
		active += summary.Active
		completed += summary.Completed
		if summary.Active < 0 || summary.Completed < 0 || summary.Active+summary.Completed == 0 {
			t.Fatalf("empty summary: %+v", summary)
		}
	}
	if active+completed != len(stream) {
		t.Fatal("summaries drop or double items")
	}
	if active != len(OpenWorkItems(stream)) || completed != len(RecentWork(stream)) {
		t.Fatal("summaries disagree with the governed lists")
	}
	if !reflect.DeepEqual(SummarizePersonal(stream), SummarizePersonal(stream)) {
		t.Fatal("summaries are unstable")
	}
}
