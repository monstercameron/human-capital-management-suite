package main

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3958. The Saved panel's meeting brief. The selection and the brief live
// only on the panel's own object, in memory: nothing about them is written to
// browser storage, sent to the service or kept after the panel is closed.

// chatsaveBriefState is one open brief.
type chatsaveBriefState struct {
	stage         string // setup or draft
	checking      bool
	err           string
	purpose, when string
	includeNotes  bool
	text          string
	included      []chatui.SavedMessageRow
	notesInText   bool
	target        string
	shareNotes    bool
	problems      []chatui.MeetingBriefProblem
	status        string
	draftedIn     string
	// rows are the selected messages as last read, for the page that shows the
	// selection when the list cannot be read again.
	rows []chatui.SavedMessageRow
}

func chatsaveKeyParts(key string) (host, conversationID, postID string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 {
		return "", "", key
	}
	return parts[0], parts[1], parts[2]
}

// chatsaveToggleSelection flips one message in the selection.
func chatsaveToggleSelection(selected map[string]bool, key string) map[string]bool {
	next := map[string]bool{}
	for k, on := range selected {
		if on {
			next[k] = true
		}
	}
	if next[key] {
		delete(next, key)
	} else {
		next[key] = true
	}
	return next
}

// chatsaveBriefRows turns the selection into rows as the service reads them
// now. A selected message the list no longer holds (taken off Saved here or on
// another device) becomes a "missing" row, so the brief can say so; its text
// is never kept anywhere else.
func chatsaveBriefRows(page chat.SavedPage, selected map[string]bool, cfg journeyclient.Config, model chatui.Model) []chatui.SavedMessageRow {
	found := chat.SavedPage{}
	seen := map[string]bool{}
	for _, item := range page.Items {
		key := chatui.SavedItemKey(item.TenantID, item.ConversationID, item.PostID)
		if selected[key] {
			found.Items = append(found.Items, item)
			seen[key] = true
		}
	}
	rows := chatsaveRows(found, cfg, model)
	var missing []string
	for key, on := range selected {
		if on && !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		host, conversationID, postID := chatsaveKeyParts(key)
		rows = append(rows, chatui.SavedMessageRow{TenantID: host, ConversationID: conversationID, PostID: postID, Availability: "missing"})
	}
	return rows
}

// chatsaveBriefSources is the preview list: every selected message, and for
// each one that cannot be used, why.
func chatsaveBriefSources(rows []chatui.SavedMessageRow) (sources []chatui.SavedBriefSource, used int) {
	for _, row := range rows {
		reason := chatui.MeetingBriefSkipReason(row)
		if reason == "" {
			used++
		}
		sources = append(sources, chatui.SavedBriefSource{Row: row, Reason: reason})
	}
	return sources, used
}

// chatsaveBriefView is the brief page's view.
func (s *chatsaveBriefState) view(rows []chatui.SavedMessageRow, model chatui.Model) *chatui.SavedBriefView {
	sources, used := chatsaveBriefSources(rows)
	return &chatui.SavedBriefView{Stage: s.stage, Checking: s.checking, Error: s.err, Purpose: s.purpose, When: s.when, IncludeNotes: s.includeNotes,
		Sources: sources, UsedCount: used, LeftCount: len(sources) - used, Text: s.text, NotesInText: s.notesInText,
		Targets: chatui.SavedBriefTargets(model.Conversations), Target: s.target, ShareNotes: s.shareNotes, Problems: s.problems, Status: s.status, DraftedIn: s.draftedIn}
}

// chatsaveBriefDraftText is the composer text after a brief is put in a draft:
// whatever the person had already typed stays, and the brief follows it.
func chatsaveBriefDraftText(existing, brief string) string {
	brief = strings.TrimRight(brief, "\n\r\t ")
	if strings.TrimSpace(existing) == "" {
		return brief
	}
	return strings.TrimRight(existing, "\n\r\t ") + "\n\n" + brief
}

// chatsaveBriefTargetName is the name of a conversation the brief can go to,
// or "" when the person is not allowed to pick it.
func chatsaveBriefTargetName(model chatui.Model, id string) string {
	for _, target := range chatui.SavedBriefTargets(model.Conversations) {
		if target.ID == id {
			return target.Name
		}
	}
	return ""
}
