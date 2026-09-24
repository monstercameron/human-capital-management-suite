package chatui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// localUI is presentation state the chat client never needs to know about:
// which inline channel tray is open and the create dialog's kind and picked
// members. Like mentionBox it lives in a ref, so a handler always reads its own
// latest write even when events outrun renders; the tick only asks for one.
type localUI struct {
	room       string
	tray       string
	createKind ConversationKind
	pickQuery  string
	picked     []mentionCandidate
	pickActive int
	// composerNotice explains a composer command that could not run, such as
	// /giphy without a configured GIPHY key. The next keystroke clears it.
	composerNotice string
	// docSuggest is the composer's "[[" / "doc:" list (chat_doc_suggest.go).
	docSuggest docSuggestState
	seq        uint64
}

type localStore struct {
	box  *localUI
	tick ui.State[uint64]
}

func (s localStore) get() localUI { return *s.box }

func (s localStore) update(change func(*localUI)) {
	change(s.box)
	s.box.seq++
	s.tick.Set(s.box.seq)
}

// forRoom drops room-scoped state (the open tray) when the conversation
// changes, without asking for another render: the caller is rendering already.
func (s localStore) forRoom(room string) {
	if s.box.room != room {
		s.box.room = room
		s.box.tray = ""
	}
}

// resetCreate clears the create dialog's picker for its next opening.
func (s localStore) resetCreate() {
	s.box.createKind = ""
	s.box.pickQuery = ""
	s.box.picked = nil
	s.box.pickActive = 0
}

// pickCandidates lists people the viewer can add to a new conversation: the
// directory, the current room's members and anyone already on screen, minus
// the viewer and whoever is already picked, best prefix matches first.
func pickCandidates(m Model, query string, picked []mentionCandidate) []mentionCandidate {
	q := strings.ToLower(strings.TrimSpace(query))
	skip := map[string]bool{m.CurrentUser: true}
	for _, p := range picked {
		skip[p.ID] = true
		skip["name:"+strings.ToLower(strings.TrimSpace(p.Name))] = true
	}
	type scored struct {
		c    mentionCandidate
		rank int
	}
	var out []scored
	add := func(id, name string) {
		name = strings.TrimSpace(name)
		nameKey := "name:" + strings.ToLower(name)
		if id == "" || name == "" || name == id || skip[id] || skip[nameKey] {
			return
		}
		skip[nameKey] = true
		lower := strings.ToLower(name)
		rank := -1
		if q == "" || strings.HasPrefix(lower, q) {
			rank = 0
		} else {
			for _, word := range strings.Fields(lower)[1:] {
				if strings.HasPrefix(word, q) {
					rank = 1
					break
				}
			}
		}
		if rank < 0 {
			return
		}
		skip[id] = true
		out = append(out, scored{mentionCandidate{ID: id, Name: name, Member: true}, rank})
	}
	for _, p := range m.SearchDirectory {
		add(p.ID, p.Name)
	}
	for _, p := range m.Members {
		add(p.ID, p.Name)
	}
	for _, msg := range m.Messages {
		add(msg.AuthorID, msg.Author)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].c.Name) < strings.ToLower(out[j].c.Name)
	})
	if len(out) > 6 {
		out = out[:6]
	}
	result := make([]mentionCandidate, len(out))
	for i := range out {
		result[i] = out[i].c
	}
	return result
}

// pickedIDs is the members field's submitted value: stable subject IDs, never
// display names, so two people with one name cannot be confused.
func pickedIDs(picked []mentionCandidate) string {
	ids := make([]string, 0, len(picked))
	for _, p := range picked {
		ids = append(ids, p.ID)
	}
	return strings.Join(ids, ",")
}
