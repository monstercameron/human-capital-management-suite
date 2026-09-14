package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// PROMOUX-012: the fixtures carry the server's viewer projection, and the
// approval item is a manager or finance approval rather than the literal
// "Awaiting approval" label, which the review view no longer matches on.
//
// RED for WEB-104: task and approval filtering. The work
// collection filters through an unexported string matcher:
// tab names travel as raw strings from request to provider,
// so a typo'd filter fails closed by accident rather than by
// contract, and nothing names the task/approval split the
// tabs present. The compiler needs the typed contract — one
// enum covering the all-tasks, approvals, blocked, and
// completed views with unknown filters matching nothing —
// governing the real provider path by delegation.
func TestTodo_WEB_104(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Status: "In progress", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "start_approval"},
		{ID: "b", Status: "Manager approval", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "manager_decision"},
		{ID: "c", Status: "Blocked", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "correct_proposal"},
		{ID: "d", Status: "Done", Terminal: true},
	}
	if got := FilterWorkCollection(items, WorkCollectionAll); len(got) != 3 {
		t.Fatalf("all tasks = %+v", got)
	}
	if got := FilterWorkCollection(items, WorkCollectionReview); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("approvals = %+v", got)
	}
	if got := FilterWorkCollection(items, WorkCollectionBlocked); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("blocked = %+v", got)
	}
	if got := FilterWorkCollection(items, WorkCollectionComplete); len(got) != 1 || got[0].ID != "d" {
		t.Fatalf("completed = %+v", got)
	}
	if got := FilterWorkCollection(items, WorkCollectionFilter(0)); len(got) != 0 {
		t.Fatalf("unknown filter matches: %+v", got)
	}

	// The request strings parse to the typed contract.
	if ParseWorkCollectionFilter("") != WorkCollectionAll {
		t.Fatal("empty request is not the tasks view")
	}
	if ParseWorkCollectionFilter("review") != WorkCollectionReview {
		t.Fatal("review does not parse to approvals")
	}
	if ParseWorkCollectionFilter("blocked") != WorkCollectionBlocked {
		t.Fatal("blocked does not parse")
	}
	if ParseWorkCollectionFilter("complete") != WorkCollectionComplete {
		t.Fatal("complete does not parse")
	}
	if ParseWorkCollectionFilter("bogus") != WorkCollectionFilter(0) {
		t.Fatal("bogus filter parses to a view")
	}

	// The provider path delegates: identical outcomes.
	for _, raw := range []string{"", "review", "blocked", "complete", "bogus"} {
		if !reflect.DeepEqual(filterWork(items, raw), FilterWorkCollection(items, ParseWorkCollectionFilter(raw))) {
			t.Fatalf("provider path ungoverned for %q", raw)
		}
	}
}

// Golden: typed outcomes over filter values.
func TestTodo_WEB_104_Golden(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Status: "In progress", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "start_approval"},
		{ID: "b", Status: "Manager approval", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "manager_decision"},
		{ID: "c", Status: "Blocked", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "correct_proposal"},
		{ID: "d", Status: "Done", Terminal: true},
	}
	filters := []WorkCollectionFilter{WorkCollectionAll, WorkCollectionReview, WorkCollectionBlocked, WorkCollectionComplete, WorkCollectionFilter(0)}
	var builder strings.Builder
	for _, filter := range filters {
		for _, item := range FilterWorkCollection(items, filter) {
			builder.WriteString(item.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "161cf090d1f17677d4a32587f18597c2f094d5a8c86529c2c2e2e90c321f9d69"
	if got != want {
		t.Fatalf("filter digest = %s, want %s", got, want)
	}
}

// Browser: filtering over item patterns is deterministic and
// keeps admission order within each view.
func TestTodo_WEB_104_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a", Status: "Awaiting approval"}, {ID: "b", Status: "In progress"}, {ID: "c", Status: "Blocked", Terminal: true}},
		{{ID: "x", Status: "Blocked"}, {ID: "y", Status: "Awaiting approval"}, {ID: "z", Status: "Done", Terminal: true}},
	}
	filters := []WorkCollectionFilter{WorkCollectionAll, WorkCollectionReview, WorkCollectionBlocked, WorkCollectionComplete}
	for _, stream := range patterns {
		for _, filter := range filters {
			first := FilterWorkCollection(stream, filter)
			second := FilterWorkCollection(stream, filter)
			previous := -1
			for _, item := range first {
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
				t.Fatal("filtering is nondeterministic")
			}
		}
	}
}

// Conformance: the four views partition open work plus
// completed; passthrough; stability.
func TestTodo_WEB_104_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a", Status: "In progress", Title: "T", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "start_approval"},
		{ID: "b", Status: "Finance approval", Title: "U", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "finance_decision"},
		{ID: "c", Status: "Blocked", Title: "V", ViewerResponsibility: "ACTION_REQUIRED", NextStep: "correct_proposal"},
		{ID: "d", Status: "Done", Title: "W", Terminal: true},
	}
	all := FilterWorkCollection(stream, WorkCollectionAll)
	complete := FilterWorkCollection(stream, WorkCollectionComplete)
	if len(all)+len(complete) != len(stream) {
		t.Fatal("views drop or double items")
	}
	if !reflect.DeepEqual(all[0], stream[0]) {
		t.Fatalf("filtering rewrites items: %+v", all[0])
	}
	all[0].Title = "mutated"
	if FilterWorkCollection(stream, WorkCollectionAll)[0].Title != "T" || stream[0].Title != "T" {
		t.Fatal("filtering aliases its input")
	}
	if !reflect.DeepEqual(FilterWorkCollection(stream, WorkCollectionReview), FilterWorkCollection(stream, WorkCollectionReview)) {
		t.Fatal("filtering is unstable")
	}
}
