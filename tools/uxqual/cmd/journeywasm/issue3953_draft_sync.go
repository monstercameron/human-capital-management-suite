package main

import "strings"

// Issues 3953 and 3950: what the page knows about its drafts compared with the
// account's copy.
//
// A draft is written to the person's account, so two tabs can write the same
// conversation, and a write can fail while the backend is away. Until now the
// later write replaced the earlier text without a word (3953), and edits made
// during an outage stayed on screen but never reached the account, with
// nothing telling the person (3950).
//
// base is the text the account is known to hold for each conversation, as of
// the last time this session read or wrote it. A different text that arrives
// from the account while this session has its own is another tab's version, and
// it is kept in conflicts, shown beside the composer, until the person settles
// it. Nothing is replaced or sent on its own. unsaved is set while a write has
// failed and the edits are still only on this page.
type draftSyncState struct {
	base      map[string]string
	conflicts map[string]string
	unsaved   bool
}

// noteServerDraftLocked compares the account's copy of one conversation's draft
// with this session's own. A copy this session already knew (base) is its own
// older write, not news; a blank copy is another tab that sent or cleared the
// draft, which never needs a choice.
func (s *chatState) noteServerDraftLocked(id, server string) {
	if s.draftSync.base == nil {
		s.draftSync.base = make(map[string]string)
	}
	known, seen := s.draftSync.base[id]
	s.draftSync.base[id] = server
	if (seen && known == server) || strings.TrimSpace(server) == "" || server == s.drafts[id] {
		return
	}
	s.recordDraftConflictLocked(id, server)
}

// noteAdoptedDraftLocked records a draft this session took from the account as
// the text the account holds.
func (s *chatState) noteAdoptedDraftLocked(id, body string) {
	if s.draftSync.base == nil {
		s.draftSync.base = make(map[string]string)
	}
	s.draftSync.base[id] = body
}

// keepReplacedDraftLocked is for a conversation this tab is not showing, whose
// saved text the account's newer copy just replaced: the replaced text is kept
// as the other version, so opening the conversation offers it back.
func (s *chatState) keepReplacedDraftLocked(id, replaced string) {
	if strings.TrimSpace(replaced) != "" {
		s.recordDraftConflictLocked(id, replaced)
	}
}

// recordDraftConflictLocked keeps another tab's version. A second different
// version for the same conversation is added to the first, so none is lost
// before the person chooses.
func (s *chatState) recordDraftConflictLocked(id, other string) {
	if strings.TrimSpace(other) == "" || other == s.drafts[id] {
		return
	}
	if s.draftSync.conflicts == nil {
		s.draftSync.conflicts = make(map[string]string)
	}
	switch kept := s.draftSync.conflicts[id]; {
	case kept == "":
		s.draftSync.conflicts[id] = other
	case strings.Contains(kept, other):
	default:
		s.draftSync.conflicts[id] = kept + "\n\n" + other
	}
	s.syncDraftSyncModelLocked()
}

func (s *chatState) syncDraftSyncModelLocked() {
	s.model.DraftConflicts = copyChatDrafts(s.draftSync.conflicts)
	s.model.DraftUnsaved = s.draftSync.unsaved
}

// notePersistedLocked records what the account now holds for every draft the
// write covered. A draft with no edit since the write is exactly what was
// written; a blank one was cleared.
func (s *chatState) notePersistedLocked(covered map[string]bool) {
	if s.draftSync.base == nil {
		s.draftSync.base = make(map[string]string)
	}
	for id := range covered {
		if body := s.drafts[id]; strings.TrimSpace(body) == "" {
			delete(s.draftSync.base, id)
		} else {
			s.draftSync.base[id] = body
		}
	}
}

// resolveDraftConflict forgets the other version once the person settled it.
func (s *chatState) resolveDraftConflict(conversationID string) {
	s.mu.Lock()
	delete(s.draftSync.conflicts, conversationID)
	s.syncDraftSyncModelLocked()
	s.mu.Unlock()
}

// noteDraftSaveFailed is called when a write of the drafts failed. It reports
// whether there are edits that exist only on this page; only then is the person
// told, because a failed write of nothing new loses nothing.
func (s *chatState) noteDraftSaveFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.draftRevs) == 0 {
		return false
	}
	s.draftSync.unsaved = true
	s.syncDraftSyncModelLocked()
	return true
}

// draftsUnsaved reports whether edits are still waiting for a write.
func (s *chatState) draftsUnsaved() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.draftRevs) > 0
}

// clearDraftSaveFailure is called when a write reached the account. It reports
// whether it withdrew the "not saved yet" line, so the page knows to redraw.
func (s *chatState) clearDraftSaveFailure() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.draftSync.unsaved || len(s.draftRevs) != 0 {
		return false
	}
	s.draftSync.unsaved = false
	s.syncDraftSyncModelLocked()
	return true
}
