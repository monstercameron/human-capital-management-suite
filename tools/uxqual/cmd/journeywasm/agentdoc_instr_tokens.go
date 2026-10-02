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

func personaInstructionRemoveReference(text, documentID string, refs []agentdocref.Reference) (string, []agentdocref.Reference) {
	label := ""
	for _, ref := range refs {
		if ref.DocumentID == documentID {
			label = ref.Label
			break
		}
	}
	if label != "" {
		text = strings.ReplaceAll(text, "@["+label+"]", "")
	}
	nextRefs := refs[:0]
	for _, ref := range refs {
		if ref.DocumentID != documentID && strings.Contains(text, "@["+ref.Label+"]") {
			nextRefs = append(nextRefs, ref)
		}
	}
	return strings.Join(strings.Fields(text), " "), nextRefs
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
	used := make(map[string]bool, len(refs))
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
			used[matches[0].DocumentID] = true
		}
		offset = end + 1
	}
	kept := make([]agentdocref.Reference, 0, len(refs))
	for _, ref := range refs {
		if used[ref.DocumentID] {
			kept = append(kept, ref)
		}
	}
	return out.String(), kept, unknown
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
