package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func notePage(busy bool) journey.Page {
	return journey.Page{Detail: &journey.DetailView{Notes: &journey.NotesView{Composer: &journey.NoteComposer{Busy: busy}}}}
}

func TestNoteFocusReturnsOnlyWhenASubmissionSettles(t *testing.T) {
	var tracker noteFocusTracker
	steps := []struct {
		page journey.Page
		want bool
	}{
		{notePage(false), false}, // idle: typing must never move focus
		{notePage(false), false},
		{notePage(true), false}, // sending
		{notePage(true), false},
		{notePage(false), true}, // settled: recorded or refused
		{notePage(false), false},
		{journey.Page{}, false}, // navigated away mid-send
		{notePage(true), false},
		{journey.Page{}, false},
		{notePage(false), false}, // a new detail page is not a settled send
	}
	for i, step := range steps {
		if got := tracker.observe(step.page); got != step.want {
			t.Fatalf("step %d: observe = %v, want %v", i, got, step.want)
		}
	}
}
