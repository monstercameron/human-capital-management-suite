package main

import (
	"errors"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestChattoneY2Reads: a reworded view that is on its way is asked about again
// by its own rule and a translation by its own; a failing read is asked a few
// times and then given up, and a message that is not there is not asked again.
func TestChattoneY2Reads(t *testing.T) {
	reword := []chatrender.Kind{chatrender.Reword}
	selections := map[string]chatui.ReaderSelection{
		"reword-pending":     {Mark: chatrender.Mark{State: "pending", Wanted: reword}},
		"reword-unavailable": {Mark: chatrender.Mark{State: "unavailable", Wanted: reword}},
		"reword-fallback":    {Mark: chatrender.Mark{State: "fallback", CanShowOriginal: true, Wanted: reword}},
		"reword-ready":       {Mark: chatrender.Mark{State: "ready", Kinds: reword}},
		"others-see":         {Mark: chatrender.Mark{State: "original"}, OthersSee: &chatui.ReaderOthersSee{State: "pending"}},
		"translate":          {Mark: chatrender.Mark{State: "pending", CanShowOriginal: true, Wanted: []chatrender.Kind{chatrender.Translate}}},
		"both":               {Mark: chatrender.Mark{State: "pending", Wanted: []chatrender.Kind{chatrender.Reword, chatrender.Translate}}},
	}
	got := chattoneUnsettled(selections)
	sort.Strings(got)
	if len(got) != 3 || got[0] != "both" || got[1] != "others-see" || got[2] != "reword-pending" {
		t.Fatalf("rewordings on their way = %v", got)
	}
	all := readerUnsettled(selections)
	sort.Strings(all)
	if len(all) != 4 || all[3] != "translate" {
		t.Fatalf("everything asked about again (once each) = %v", all)
	}

	attempts := 0
	retried := boundedRead(readerRetryLimit, func() bool { attempts++; return false })
	for i := 1; i < 100; i++ {
		if retried() {
			if i != readerRetryLimit || attempts != readerRetryLimit {
				t.Fatalf("a failing read stopped after %d asks (%d attempts)", i, attempts)
			}
			break
		}
		if i == 99 {
			t.Fatal("a failing read is asked again for ever")
		}
	}
	if !boundedRead(readerRetryLimit, func() bool { return true })() {
		t.Fatal("a read that answered was asked again")
	}

	// A message that is not there is the server's answer; an outage is not.
	if !chatux012HTTPFinal(errPersonaChatNotFound) || !errors.Is(errPersonaChatNotFound, errPersonaChat) {
		t.Fatal("not found is asked again, or is no longer an unavailable read to its callers")
	}
	if chatux012HTTPFinal(errPersonaChat) {
		t.Fatal("an outage was taken for an answer")
	}

	// A read that fails adds nothing: the text stays as written.
	chosen := map[string]chatui.ReaderSelection{}
	integrate2ApplyReaderAnswer(chosen, []chatui.Message{{ID: "m1", Revision: 1}}, map[string]chatui.ReaderSelection{"m1": {Mark: chatrender.Mark{State: "pending"}}}, errPersonaChatNotFound)
	if len(chosen) != 0 {
		t.Fatalf("a failed read recorded a mark: %+v", chosen)
	}
}
