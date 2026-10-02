package main

import (
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

type personaInstructionDocumentChoice struct {
	DocumentID       string
	Title            string
	Location         string
	VisibleLabel     string
	VersionMode      agentdocref.VersionMode
	PinnedVersion    uint64
	ExistingCaretPos int
}

type personaInstructionMentionState struct {
	Open  bool
	Start int
	Query string
}

// personaInstructionMentionAt owns the @ trigger decision independently of
// the browser. Spaces are valid document-title search text; a newline or more
// than 60 characters closes the menu.
func personaInstructionMentionAt(text string, caret int) personaInstructionMentionState {
	if caret < 0 || caret > len(text) || !utf8.ValidString(text) {
		return personaInstructionMentionState{}
	}
	lineStart := strings.LastIndex(text[:caret], "\n") + 1
	start := strings.LastIndex(text[lineStart:caret], "@")
	if start < 0 {
		return personaInstructionMentionState{}
	}
	start += lineStart
	query := text[start+1 : caret]
	if strings.ContainsAny(query, "\r\n") || len([]rune(query)) > 60 || strings.Contains(query, "]") {
		return personaInstructionMentionState{}
	}
	return personaInstructionMentionState{Open: true, Start: start, Query: query}
}

func personaInstructionChoiceLabel(choice personaInstructionDocumentChoice) string {
	if label := strings.TrimSpace(choice.VisibleLabel); label != "" {
		return label
	}
	title := strings.TrimSpace(choice.Title)
	if title == "" {
		title = choice.DocumentID
	}
	if location := strings.TrimSpace(choice.Location); location != "" {
		return title + " (" + location + ")"
	}
	return title
}

func personaInstructionInsertDocument(text string, caret int, refs []agentdocref.Reference, choice personaInstructionDocumentChoice) (string, int, []agentdocref.Reference) {
	if caret < 0 || caret > len(text) || !utf8.ValidString(text) {
		caret = len(text)
	}
	mention := "@[" + personaInstructionChoiceLabel(choice) + "]"
	prefixStart := -1
	if state := personaInstructionMentionAt(text, caret); state.Open {
		prefixStart = state.Start
	}
	insertStart := caret
	if prefixStart >= 0 {
		insertStart = prefixStart
	}
	next := text[:insertStart] + mention + text[caret:]
	if !strings.Contains(next, mention+" ") {
		next = text[:insertStart] + mention + " " + text[caret:]
	}
	if !personaInstructionHasReference(refs, choice.DocumentID) {
		label := personaInstructionChoiceLabel(choice)
		if label == "" {
			label = choice.DocumentID
		}
		pinned := choice.PinnedVersion
		if choice.VersionMode == agentdocref.ModeLatestPublished {
			pinned = 0
		}
		refs = append(refs, agentdocref.Reference{DocumentID: choice.DocumentID, VersionMode: choice.VersionMode, PinnedVersion: pinned, Label: label})
	}
	return next, insertStart + len(mention) + 1, refs
}

// personaInstructionRemoveReference takes one document out of the list and its
// mention out of the instructions. Everything else the administrator wrote is
// left as it is: line breaks, blank lines and indentation carry meaning in
// instructions. Documents that were attached without a mention stay attached.
func personaInstructionRemoveReference(text, documentID string, refs []agentdocref.Reference) (string, []agentdocref.Reference) {
	nextRefs := make([]agentdocref.Reference, 0, len(refs))
	for _, ref := range refs {
		if ref.DocumentID != documentID {
			nextRefs = append(nextRefs, ref)
			continue
		}
		if ref.Label != "" {
			text = personaInstructionRemoveMention(text, "@["+ref.Label+"]")
		}
	}
	return text, nextRefs
}

// personaInstructionRemoveMention deletes every occurrence of one mention. The
// space that separated the mention from its neighbour goes with it, so
// "Follow @[Policy] first" becomes "Follow first", and a line the mention
// ended keeps no trailing space.
func personaInstructionRemoveMention(text, mention string) string {
	for {
		at := strings.Index(text, mention)
		if at < 0 {
			return text
		}
		head, tail := text[:at], text[at+len(mention):]
		lineStart := head == "" || strings.HasSuffix(head, "\n")
		if strings.HasPrefix(tail, " ") && (lineStart || strings.HasSuffix(head, " ")) {
			tail = tail[1:]
		}
		if tail == "" || tail[0] == '\n' || tail[0] == '\r' {
			head = strings.TrimRight(head, " \t")
		}
		text = head + tail
	}
}

func personaInstructionReferencesForSubmit(text string, refs []agentdocref.Reference) []agentdocref.Reference {
	kept := make([]agentdocref.Reference, 0, len(refs))
	for _, ref := range refs {
		if strings.Contains(text, "@["+ref.Label+"]") {
			kept = append(kept, ref)
		}
	}
	return kept
}

func personaInstructionStoredForSubmit(text string, refs []agentdocref.Reference) (string, []agentdocref.Reference, []string) {
	byLabel := make(map[string][]agentdocref.Reference, len(refs))
	for _, ref := range refs {
		byLabel[ref.Label] = append(byLabel[ref.Label], ref)
	}
	unknown := make([]string, 0)
	var out strings.Builder
	for offset := 0; offset < len(text); {
		start := strings.Index(text[offset:], "@[")
		if start < 0 {
			out.WriteString(text[offset:])
			break
		}
		start += offset
		end := strings.Index(text[start+2:], "]")
		if end < 0 {
			out.WriteString(text[offset:])
			break
		}
		end += start + 2
		label := text[start+2 : end]
		out.WriteString(text[offset:start])
		matches := byLabel[label]
		if len(matches) != 1 {
			unknown = append(unknown, label)
			out.WriteString(text[start : end+1])
		} else {
			out.WriteString(agentdocref.Token(matches[0].DocumentID))
		}
		offset = end + 1
	}
	if len(unknown) > 0 {
		return out.String(), nil, unknown
	}
	// Every document still in the list is sent, mentioned or not: the server
	// allows a reference the instructions never name, and the list is where
	// the administrator removes one.
	return out.String(), append([]agentdocref.Reference(nil), refs...), unknown
}

func personaInstructionHasReference(refs []agentdocref.Reference, documentID string) bool {
	for _, ref := range refs {
		if ref.DocumentID == documentID {
			return true
		}
	}
	return false
}

func personaInstructionSetReferenceMode(refs []agentdocref.Reference, documentID string, mode agentdocref.VersionMode) []agentdocref.Reference {
	next := append([]agentdocref.Reference(nil), refs...)
	for index := range next {
		if next[index].DocumentID != documentID {
			continue
		}
		next[index].VersionMode = mode
		if mode == agentdocref.ModeLatestPublished {
			next[index].PinnedVersion = 0
		}
		break
	}
	return next
}

// personaInstructionDroppedMentions names the documents whose mention was in
// the instructions before an edit and is not in them after it.
func personaInstructionDroppedMentions(before, after string, refs []agentdocref.Reference) []string {
	dropped := make([]string, 0)
	for _, ref := range refs {
		if ref.Label == "" {
			continue
		}
		mention := "@[" + ref.Label + "]"
		if strings.Contains(before, mention) && !strings.Contains(after, mention) {
			dropped = append(dropped, ref.DocumentID)
		}
	}
	return dropped
}
