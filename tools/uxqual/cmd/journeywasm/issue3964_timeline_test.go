package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3964_QueryIsOneConversationAKindADayRangeAndOldestFirst(t *testing.T) {
	start := time.Date(2025, 11, 10, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 3)
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread} {
		query, err := timelineQuery("ops", kind, start, end)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := chatsearch.Parse(query)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		f := parsed.Filters
		if f.Conversation != "ops" || f.Kind != kind || f.Order != chatsearch.OrderOldest || f.After == "" || f.Before == "" || parsed.Text != "" {
			t.Fatalf("query %q parses to %+v", query, parsed)
		}
		if f.After >= start.UTC().Format("2006-01-02") || f.Before <= end.UTC().Format("2006-01-02") {
			t.Fatalf("query %q does not enclose the range", query)
		}
	}
	for _, bad := range []string{"", `a"b`, "a b", "a\x00b"} {
		if _, err := timelineQuery(bad, chatsearch.Message, start, end); err == nil {
			t.Fatalf("conversation %q must be refused, not spliced into the query", bad)
		}
	}
	if _, err := timelineQuery("ops", chatsearch.File, start, end); err == nil {
		t.Fatal("only messages and thread replies make a timeline")
	}
}

func TestIssue3964_ARowBecomesAReportOfThisConversationOnly(t *testing.T) {
	at := time.Date(2025, 11, 10, 10, 0, 0, 0, time.UTC)
	response := chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Rows: []chatsearch.Row{
		{Kind: chatsearch.Message, ID: "p1", Text: "Pump 3 is leaking", AuthorID: "maya", At: at, Edited: true, Target: chatsearch.Target{ConversationID: "ops", MessageID: "p1", Sequence: 4}},
		{Kind: chatsearch.Message, ID: "p2", Text: "another room", AuthorID: "maya", At: at, Target: chatsearch.Target{ConversationID: "hr", MessageID: "p2", Sequence: 1}},
		{Kind: chatsearch.Message, ID: "p3", Text: "restricted", AuthorID: "maya", At: at, Restricted: true, Target: chatsearch.Target{ConversationID: "ops", MessageID: "p3", Sequence: 6}},
		{Kind: chatsearch.Message, ID: "", Text: "no id", At: at, Target: chatsearch.Target{ConversationID: "ops", Sequence: 7}},
		{Kind: chatsearch.Thread, ID: "r1", Text: "reply", AuthorID: "malik", At: at, Target: chatsearch.Target{ConversationID: "ops", MessageID: "r1", Sequence: 9, ThreadID: "p1", ThreadSequence: 4}},
		{Kind: chatsearch.File, ID: "f1", Text: "diagram.png", At: at, Target: chatsearch.Target{ConversationID: "ops", MessageID: "p1", ItemID: "m1", Sequence: 4}},
	}}}, NextCursor: "next", Unavailable: []chatsearch.Kind{chatsearch.Todo}}
	entries, more, unavailable := timelineFromResponse(response, "ops")
	if unavailable || !more {
		t.Fatalf("unavailable=%v more=%v (an unrelated source being down does not stop a timeline)", unavailable, more)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want the three messages of ops", entries)
	}
	first := entries[0]
	if first.ID != "p1" || !first.Edited || first.Restricted || first.InThread || first.Sequence != 4 || first.AuthorID != "maya" || first.Text != "Pump 3 is leaking" {
		t.Fatalf("first entry = %+v", first)
	}
	if !strings.Contains(first.Href, "channel=ops") || !strings.Contains(first.Href, "message=p1") {
		t.Fatalf("an entry must carry the address of its message: %q", first.Href)
	}
	if !entries[1].Restricted {
		t.Fatal("a message addressed to part of the room must say so")
	}
	reply := entries[2]
	if !reply.InThread || reply.ThreadID != "p1" || reply.ThreadSequence != 4 || !strings.Contains(reply.Href, "thread=1") {
		t.Fatalf("a reply keeps its thread: %+v", reply)
	}
	down := chatsearch.Response{Unavailable: []chatsearch.Kind{chatsearch.Message}}
	if _, _, bad := timelineFromResponse(down, "ops"); !bad {
		t.Fatal("an unavailable message source must be reported, not read as an empty range")
	}
}

func TestIssue3964_MergeKeepsTimeOrderCutsAtTheLimitAndNeverSkipsAHole(t *testing.T) {
	start := time.Date(2025, 11, 10, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 2)
	e := func(id string, hours int) chatui.TimelineEntry {
		return chatui.TimelineEntry{ID: id, At: start.Add(time.Duration(hours) * time.Hour)}
	}
	ids := func(list []chatui.TimelineEntry) string {
		var out []string
		for _, x := range list {
			out = append(out, x.ID)
		}
		return strings.Join(out, ",")
	}
	got, truncated := timelineMerge([]timelineKindRead{{entries: []chatui.TimelineEntry{e("m1", 1), e("m3", 5)}}, {entries: []chatui.TimelineEntry{e("r1", 2), e("r2", 3)}}}, start, end)
	if ids(got) != "m1,r1,r2,m3" || truncated {
		t.Fatalf("merge = %s truncated=%v", ids(got), truncated)
	}
	// Messages the search put outside the exact range (it works in UTC days) are dropped.
	got, _ = timelineMerge([]timelineKindRead{{entries: []chatui.TimelineEntry{e("before", -1), e("in", 1), e("after", 49)}}}, start, end)
	if ids(got) != "in" {
		t.Fatalf("range = %s", ids(got))
	}
	// One kind ran out of pages at hour 3: nothing later than that is shown from
	// the other kind either, or the timeline would silently skip that kind's gap.
	got, truncated = timelineMerge([]timelineKindRead{{entries: []chatui.TimelineEntry{e("m1", 1), e("m9", 40)}}, {entries: []chatui.TimelineEntry{e("r1", 2), e("r2", 3)}, more: true}}, start, end)
	if ids(got) != "m1,r1,r2" || !truncated {
		t.Fatalf("cut merge = %s truncated=%v, want everything up to the last row read of the cut kind", ids(got), truncated)
	}
	// The overall limit.
	many := make([]chatui.TimelineEntry, 0, chatui.TimelineMaxEntries+5)
	for i := 0; i < chatui.TimelineMaxEntries+5; i++ {
		many = append(many, chatui.TimelineEntry{ID: "m" + strconv.Itoa(i), At: start.Add(time.Duration(i) * time.Minute)})
	}
	got, truncated = timelineMerge([]timelineKindRead{{entries: many}}, start, end)
	if len(got) != chatui.TimelineMaxEntries || !truncated {
		t.Fatalf("limit: %d entries truncated=%v", len(got), truncated)
	}
}

func TestIssue3964_ChangedEntriesAreCountedBeforeAnythingIsShared_Security(t *testing.T) {
	at := time.Date(2025, 11, 10, 10, 0, 0, 0, time.UTC)
	shown := []chatui.TimelineEntry{
		{ID: "same", Text: "a", At: at}, {ID: "reworded", Text: "b", At: at}, {ID: "gone", Text: "c", At: at}, {ID: "edited", Text: "d", At: at}, {ID: "narrowed", Text: "e", At: at},
	}
	fresh := []chatui.TimelineEntry{
		{ID: "same", Text: "a", At: at}, {ID: "reworded", Text: "b2", At: at}, {ID: "edited", Text: "d", At: at, Edited: true}, {ID: "narrowed", Text: "e", At: at, Restricted: true}, {ID: "new", Text: "z", At: at},
	}
	if got := timelineChanged([]string{"same", "reworded", "gone", "edited", "narrowed"}, shown, fresh); got != 4 {
		t.Fatalf("changed = %d, want the reworded, the gone, the newly edited and the newly narrowed", got)
	}
	if got := timelineChanged([]string{"same"}, shown, fresh); got != 0 {
		t.Fatalf("an unchanged entry counted as changed: %d", got)
	}
	if got := timelineChanged([]string{"never-shown"}, shown, fresh); got != 1 {
		t.Fatalf("an id the timeline never showed must not pass: %d", got)
	}
}

// A busy stretch before the range (the search works in whole UTC days) must not
// use up the pages meant for the range: paging goes on past it, stops at the end
// of the range, at the limit of messages inside it, and at the page cap.
func TestIssue3964_PagingSpendsItsBudgetInsideTheRangeNotBeforeIt(t *testing.T) {
	start := time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	page := func(from time.Time, n int) []chatui.TimelineEntry {
		out := make([]chatui.TimelineEntry, n)
		for i := range out {
			out[i] = chatui.TimelineEntry{ID: strconv.Itoa(i), At: from.Add(time.Duration(i) * time.Minute)}
		}
		return out
	}
	before := page(start.Add(-20*time.Hour), timelinePageSize)
	keep, inRange := timelineKeepReading(true, before, 0, timelineMaxPages, start, end)
	if !keep || inRange != 0 {
		t.Fatalf("a full page before the range ended the reading: keep=%v inRange=%d", keep, inRange)
	}
	if keep, _ = timelineKeepReading(true, before, 0, timelineMaxPages+timelineLeadPages, start, end); keep {
		t.Fatal("the page cap must end the reading")
	}
	inside := page(start.Add(time.Hour), timelinePageSize)
	if keep, inRange = timelineKeepReading(true, inside, chatui.TimelineMaxEntries-timelinePageSize, 2, start, end); keep || inRange != chatui.TimelineMaxEntries {
		t.Fatalf("the limit of messages inside the range must end the reading: keep=%v inRange=%d", keep, inRange)
	}
	past := page(end.Add(time.Minute), 3)
	if keep, _ = timelineKeepReading(true, past, 0, 1, start, end); keep {
		t.Fatal("a page that reached the end of the range must end the reading")
	}
	if keep, _ = timelineKeepReading(false, inside, 0, 1, start, end); keep {
		t.Fatal("no next page, nothing more to read")
	}
}
