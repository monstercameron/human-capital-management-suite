package chatui

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

// The composer's ticket and board autocomplete (issue 3747): "ticket:" lists
// tickets and "board:" lists project boards the reader can open, by key or
// title. The project service answers only for projects the reader may read,
// and picking one writes the canonical in-app address, which is the link
// ProjectTaskReferences unfurls into a card. It shares the document list's
// state, menu, keys and retry; Kind says which source is asked.

const (
	// SuggestKindTicket and SuggestKindBoard name the work-item pickers; the
	// empty kind is the document picker.
	SuggestKindTicket = "ticket"
	SuggestKindBoard  = "board"
)

const (
	KeyWorkSuggestTickets     = "chat.work_suggest_tickets"
	KeyWorkSuggestTicketsNone = "chat.work_suggest_tickets_none"
	KeyWorkSuggestBoards      = "chat.work_suggest_boards"
	KeyWorkSuggestBoardsNone  = "chat.work_suggest_boards_none"
	KeyWorkSuggestFailed      = "chat.work_suggest_failed"
	KeyWorkSuggestUnavailable = "chat.work_suggest_unavailable"
)

func init() {
	englishCopy[KeyWorkSuggestTickets] = "Tickets"
	englishCopy[KeyWorkSuggestTicketsNone] = "No tickets you can open match"
	englishCopy[KeyWorkSuggestBoards] = "Boards"
	englishCopy[KeyWorkSuggestBoardsNone] = "No boards you can open match"
	englishCopy[KeyWorkSuggestFailed] = "Tickets and boards could not be searched right now"
	englishCopy[KeyWorkSuggestUnavailable] = "Ticket and board search is not set up yet"
}

// WorkSuggestion is one readable ticket (kind ticket) or board (kind board)
// offered by the list; Insert is its canonical address.
func WorkSuggestion(kind, projectID, taskID, title, detail string) DocSuggestion {
	insert := ProjectBoardReferenceURL(projectID)
	id := projectID
	if kind == SuggestKindTicket {
		insert, id = ProjectTaskReferenceURL(projectID, taskID), projectID+"\x00"+taskID
	}
	return DocSuggestion{ID: kind + ":" + id, Title: title, Detail: detail, Insert: insert}
}

// workSuggestSource is the callback that answers kind, or nil when the
// host did not provide one. The empty kind is the document search.
func workSuggestSource(m Model, kind string) func(string, func([]DocSuggestion, DocSuggestStatus)) {
	switch kind {
	case SuggestKindTicket:
		return m.Callbacks.SuggestTasks
	case SuggestKindBoard:
		return m.Callbacks.SuggestProjects
	}
	return m.Callbacks.SuggestDocuments
}

// workTokenAt finds the "ticket:query" or "board:query" the caret sits at the
// end of. Like the document triggers it must start a word and stops at a
// newline or "]".
func workTokenAt(value string, caret int) (kind, query string, start int, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", "", 0, false
	}
	before := string(utf16.Decode(units[:caret]))
	best := -1
	for _, trigger := range []struct{ word, kind string }{{"ticket:", SuggestKindTicket}, {"board:", SuggestKindBoard}} {
		i := strings.LastIndex(before, trigger.word)
		if i < 0 || i < best {
			continue
		}
		if i > 0 {
			prev := []rune(before[:i])
			r := prev[len(prev)-1]
			if !unicode.IsSpace(r) && !strings.ContainsRune("([{\"'", r) {
				continue
			}
		}
		q := before[i+len(trigger.word):]
		if strings.ContainsAny(q, "]\n") || len([]rune(q)) > 60 {
			continue
		}
		kind, query, start, ok, best = trigger.kind, q, len(utf16.Encode([]rune(before[:i]))), true, i
	}
	return kind, query, start, ok
}

// suggestTokenAt is the open trigger at the caret, of whichever picker was
// typed last.
func suggestTokenAt(value string, caret int) (kind, query string, start int, ok bool) {
	dq, ds, dok := docTokenAt(value, caret)
	wk, wq, ws, wok := workTokenAt(value, caret)
	if wok && (!dok || ws > ds) {
		return wk, wq, ws, true
	}
	return "", dq, ds, dok
}

// workSuggestCopy picks the heading and the empty, failed and not-set-up
// sentences for a picker kind.
func workSuggestCopy(kind string) (title, none, failed, unavailable string) {
	switch kind {
	case SuggestKindTicket:
		return KeyWorkSuggestTickets, KeyWorkSuggestTicketsNone, KeyWorkSuggestFailed, KeyWorkSuggestUnavailable
	case SuggestKindBoard:
		return KeyWorkSuggestBoards, KeyWorkSuggestBoardsNone, KeyWorkSuggestFailed, KeyWorkSuggestUnavailable
	}
	return KeyDocSuggestTitle, KeyDocSuggestNone, KeyDocSuggestFailed, KeyDocSuggestUnavailable
}
