package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestIssue3747_WorkTokenAt(t *testing.T) {
	cases := []struct {
		value string
		caret int
		kind  string
		query string
		start int
		ok    bool
	}{
		{"see ticket:fix lo", 17, SuggestKindTicket, "fix lo", 4, true},
		{"board:", 6, SuggestKindBoard, "", 0, true},
		{"(board:rel", 10, SuggestKindBoard, "rel", 1, true},
		{"myticket:x", 10, "", "", 0, false},
		{"ticket:x\nmore", 13, "", "", 0, false},
		{"hello", 5, "", "", 0, false},
	}
	for _, c := range cases {
		kind, query, start, ok := workTokenAt(c.value, c.caret)
		if kind != c.kind || query != c.query || start != c.start || ok != c.ok {
			t.Errorf("workTokenAt(%q,%d) = %q %q %d %v; want %q %q %d %v", c.value, c.caret, kind, query, start, ok, c.kind, c.query, c.start, c.ok)
		}
	}
}

func TestIssue3747_ApplyWorkSuggestionWritesCanonicalLink(t *testing.T) {
	ticket := WorkSuggestion(SuggestKindTicket, "p1", "t9", "Fix login", "T-T9")
	board := WorkSuggestion(SuggestKindBoard, "p1", "", "Core", "")
	for _, it := range []DocSuggestion{ticket, board} {
		refs := ProjectTaskReferences(it.Insert, "https://hcm.example")
		if len(refs) != 1 || refs[0].ProjectID != "p1" {
			t.Fatalf("insert %q does not unfurl: %+v", it.Insert, refs)
		}
	}
	if board.Insert != ProjectBoardReferenceURL("p1") || ticket.Insert != ProjectTaskReferenceURL("p1", "t9") {
		t.Fatalf("inserts = %q %q", ticket.Insert, board.Insert)
	}
}

func TestIssue3747_TicketPickerSearchesAndInserts(t *testing.T) {
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	var kinds, queries []string
	var done func([]DocSuggestion, DocSuggestStatus)
	m := Model{Callbacks: Callbacks{
		SuggestTasks: func(q string, d func([]DocSuggestion, DocSuggestStatus)) {
			kinds = append(kinds, "ticket")
			queries = append(queries, q)
			done = d
		},
		SuggestProjects: func(q string, d func([]DocSuggestion, DocSuggestStatus)) {
			kinds = append(kinds, "board")
			queries = append(queries, q)
			done = d
		},
	}}
	docSuggestRun(m, local, docSuggestState{Kind: SuggestKindTicket, Target: "chat-composer", Query: "fix", Open: true, Loading: true, seq: 1})
	if len(kinds) != 1 || kinds[0] != "ticket" || queries[0] != "fix" {
		t.Fatalf("calls = %v %v", kinds, queries)
	}
	done([]DocSuggestion{WorkSuggestion(SuggestKindTicket, "p1", "t9", "Fix login", "T-T9")}, DocSuggestOK)
	menu := renderNode(t, docSuggestMenu(m, local.get().docSuggest, "chat-composer"))
	if !strings.Contains(menu, "Fix login") || !strings.Contains(menu, "Tickets") || strings.Contains(menu, "Documents") {
		t.Fatalf("menu = %s", menu)
	}
	docSuggestRun(m, local, docSuggestState{Kind: SuggestKindBoard, Target: "chat-composer", Query: "", Open: true, Loading: true, seq: 2})
	if kinds[1] != "board" {
		t.Fatalf("board picker asked %v", kinds)
	}
}

func TestIssue3747_WorkPickerStatesUseWorkWords(t *testing.T) {
	menu := func(kind string, status DocSuggestStatus) string {
		return renderNode(t, docSuggestMenu(Model{Locale: "en-US"}, docSuggestState{Kind: kind, Target: "chat-composer", Open: true, Status: status}, "chat-composer"))
	}
	if got := menu(SuggestKindTicket, DocSuggestOK); !strings.Contains(got, "No tickets you can open match") {
		t.Fatalf("ticket none = %s", got)
	}
	if got := menu(SuggestKindBoard, DocSuggestOK); !strings.Contains(got, "No boards you can open match") {
		t.Fatalf("board none = %s", got)
	}
	failed := menu(SuggestKindTicket, DocSuggestFailed)
	if strings.Contains(failed, "Documents") || !strings.Contains(failed, "doc-suggest-retry") || !strings.Contains(failed, "could not be searched") {
		t.Fatalf("failed = %s", failed)
	}
	if got := menu(SuggestKindBoard, DocSuggestUnavailable); strings.Contains(got, "Document search") || !strings.Contains(got, "not set up") {
		t.Fatalf("unavailable = %s", got)
	}
}

func TestIssue3747_WorkPickerRetryAsksSameKind(t *testing.T) {
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	tasks, docs := 0, 0
	var done func([]DocSuggestion, DocSuggestStatus)
	m := Model{Callbacks: Callbacks{
		SuggestTasks:     func(q string, d func([]DocSuggestion, DocSuggestStatus)) { tasks++; done = d },
		SuggestDocuments: func(q string, d func([]DocSuggestion, DocSuggestStatus)) { docs++ },
	}}
	docSuggestRun(m, local, docSuggestState{Kind: SuggestKindTicket, Target: "chat-composer", Query: "x", Open: true, Loading: true, seq: 1})
	done(nil, DocSuggestFailed)
	docSuggestRetry(m, local)
	if tasks != 2 || docs != 0 {
		t.Fatalf("tasks=%d docs=%d", tasks, docs)
	}
}

func TestIssue3747_WorkPickerWithoutCallbackStaysClosed(t *testing.T) {
	if workSuggestSource(Model{Callbacks: Callbacks{SuggestDocuments: func(string, func([]DocSuggestion, DocSuggestStatus)) {}}}, SuggestKindTicket) != nil {
		t.Fatal("ticket source without a ticket callback")
	}
}

func TestIssue3747_WorkPickerDropsItemsWithoutCanonicalAddress(t *testing.T) {
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	var done func([]DocSuggestion, DocSuggestStatus)
	m := Model{Callbacks: Callbacks{SuggestTasks: func(q string, d func([]DocSuggestion, DocSuggestStatus)) { done = d }}}
	docSuggestRun(m, local, docSuggestState{Kind: SuggestKindTicket, Target: "chat-composer", Query: "x", Open: true, Loading: true, seq: 1})
	bad := WorkSuggestion(SuggestKindTicket, "p 1/../x", "t9", "Odd ids", "")
	if bad.Insert != "" {
		t.Fatalf("invalid ids produced an address %q", bad.Insert)
	}
	done([]DocSuggestion{bad, WorkSuggestion(SuggestKindTicket, "p1", "t9", "Fix login", "T-T9")}, DocSuggestOK)
	items := local.get().docSuggest.Items
	if len(items) != 1 || items[0].Title != "Fix login" {
		t.Fatalf("items = %+v", items)
	}
}
