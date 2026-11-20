package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// issue3690Form is what the Refine form submits when every control is as the
// page rendered it: the keys are always all present, so an empty text, an Any
// choice or an unticked box means "no such filter".
func issue3690Form(values map[string]string, checks map[string]bool) (map[string]string, map[string]bool) {
	v := map[string]string{"channel": "", "person": "", "kind": "", "written": "", "before": "", "after": "", "on": ""}
	c := map[string]bool{"file": false, "link": false, "reactions": false, "threads": false, "mentions": false, "agent": false, "mine": false, "voice": false}
	for k, x := range values {
		v[k] = x
	}
	for k, x := range checks {
		c[k] = x
	}
	return v, c
}

// TestIssue3690_EmptyControlsRemoveTheirFilter: emptying a field, choosing Any
// language or unticking a box and applying removes that filter from the query.
func TestIssue3690_EmptyControlsRemoveTheirFilter(t *testing.T) {
	query := `payroll in:announcements from:"Julian Miller" lang:en is:mine has:link`
	cases := []struct {
		name   string
		values map[string]string
		checks map[string]bool
		gone   func(f chatsearch.Filters) bool
	}{
		{"channel emptied", map[string]string{"person": "Julian Miller", "written": "en"}, map[string]bool{"mine": true, "link": true}, func(f chatsearch.Filters) bool { return f.Conversation == "" }},
		{"person emptied", map[string]string{"channel": "announcements", "written": "en"}, map[string]bool{"mine": true, "link": true}, func(f chatsearch.Filters) bool { return f.Person == "" }},
		{"any language", map[string]string{"channel": "announcements", "person": "Julian Miller"}, map[string]bool{"mine": true, "link": true}, func(f chatsearch.Filters) bool { return f.Language == "" }},
		{"mine unticked", map[string]string{"channel": "announcements", "person": "Julian Miller", "written": "en"}, map[string]bool{"link": true}, func(f chatsearch.Filters) bool { return !f.Mine }},
		{"link unticked", map[string]string{"channel": "announcements", "person": "Julian Miller", "written": "en"}, map[string]bool{"mine": true}, func(f chatsearch.Filters) bool { return !f.Link }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values, checks := issue3690Form(tc.values, tc.checks)
			got, err := chatsearchControlQuery(query, values, checks)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := chatsearch.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.gone(parsed.Filters) {
				t.Fatalf("filter survived: %q", got)
			}
			if parsed.Text != "payroll" {
				t.Fatalf("search words changed: %q", got)
			}
		})
	}
}

// TestIssue3690_FormMirroringTheQueryIsANoOp: applying the controls exactly as
// rendered keeps every filter and adds none.
func TestIssue3690_FormMirroringTheQueryIsANoOp(t *testing.T) {
	query := `payroll in:announcements lang:en is:mine`
	values, checks := issue3690Form(map[string]string{"channel": "announcements", "written": "en"}, map[string]bool{"mine": true})
	got, err := chatsearchControlQuery(query, values, checks)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := chatsearch.Parse(got)
	f := parsed.Filters
	if f.Conversation != "announcements" || f.Language != "en" || !f.Mine || f.Link || f.File || f.Person != "" {
		t.Fatalf("filters changed: %q %+v", got, f)
	}
}

// TestIssue3403_DefaultControlsKeepTheQuery: Apply with All kinds, Any language
// and blank fields keeps a valid query and adds no kind or language.
func TestIssue3403_DefaultControlsKeepTheQuery(t *testing.T) {
	values, checks := issue3690Form(nil, nil)
	got, err := chatsearchControlQuery("budget", values, checks)
	if err != nil || got != "budget" {
		t.Fatalf("got %q %v", got, err)
	}
	// Date-only and checkbox-only filters with both selectors unrestricted.
	values, checks = issue3690Form(map[string]string{"after": "2026-11-01"}, map[string]bool{"link": true})
	got, err = chatsearchControlQuery("budget", values, checks)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := chatsearch.Parse(got)
	if parsed.Filters.After != "2026-11-01" || !parsed.Filters.Link || parsed.Filters.Kind != "" || parsed.Filters.Language != "" {
		t.Fatalf("got %q %+v", got, parsed.Filters)
	}
}

// TestIssue3690_UnquotedNameIsNotLeftBehindAsSearchWords: applying the controls
// unchanged to a query with an unquoted two-word name keeps the name in the
// person filter; its second word must not become a search word.
func TestIssue3690_UnquotedNameIsNotLeftBehindAsSearchWords(t *testing.T) {
	model := chatui.Model{SearchDirectory: []chatui.SearchPerson{{ID: "hc-026-julian-miller", Name: "Julian Miller"}}}
	typed := `from:@Julian Miller payroll`
	values, checks := issue3690Form(map[string]string{"person": "Julian Miller"}, nil)
	got, err := chatsearchControlQuery(chatsearchControlBase(model, typed), values, checks)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := chatsearch.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Text != "payroll" || parsed.Filters.Person == "" {
		t.Fatalf("applying the unchanged controls gave %q (text %q, person %q)", got, parsed.Text, parsed.Filters.Person)
	}
	// Clearing the person removes the whole name, not just its first word.
	values, checks = issue3690Form(nil, nil)
	got, err = chatsearchControlQuery(chatsearchControlBase(model, typed), values, checks)
	if err != nil || got != "payroll" {
		t.Fatalf("clearing the person gave %q (%v), want payroll", got, err)
	}
}

// TestIssue3690_ClearedDateIsNotJudgedAsStillApplied: a date field submitted
// empty removes its date, so it must not be compared against the old one.
func TestIssue3690_ClearedDateIsNotJudgedAsStillApplied(t *testing.T) {
	query := `budget before:2026-12-01`
	values, checks := issue3690Form(map[string]string{"after": "2026-12-05"}, nil)
	got, err := chatsearchControlQuery(query, values, checks)
	if err != nil {
		t.Fatalf("clearing Before and setting a later After was refused: %v", err)
	}
	if parsed, _ := chatsearch.Parse(got); parsed.Filters.Before != "" || parsed.Filters.After != "2026-12-05" {
		t.Fatalf("got %q", got)
	}
	if _, _, reversed := chatsearchReversedRange(query, values); reversed {
		t.Fatal("a cleared Before was still compared with the query's old Before")
	}
}
