package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Shared files (#3917), the part that needs no browser. The list is the Chat
// search's File kind scoped to the open conversation, so the server applies
// membership, history, deletion and message access exactly as it does for a
// search; this file builds that request and reads its answer.

// sharedFilesPageSize is how many files one page asks for.
const sharedFilesPageSize = 20

// sharedFilesQuery is the search that lists one conversation's files, narrowed
// by words of a file name. Text typed into the name box is only ever words: its
// quotes are dropped and a colon is a space, so an operator typed there (in:,
// from:, kind:) cannot change which conversation or kind the list reads.
func sharedFilesQuery(conversationID, name string) (string, error) {
	if conversationID == "" || strings.ContainsAny(conversationID, "\"\x00 \t\n") {
		return "", chatsearch.ErrInvalid
	}
	query := `in:"` + conversationID + `" kind:file`
	cleaned := strings.NewReplacer(`"`, " ", ":", " ", "\x00", " ").Replace(name)
	if words := strings.Fields(cleaned); len(words) > 0 {
		if len(words) > 8 {
			words = words[:8]
		}
		query += " " + strings.Join(words, " ")
	}
	if len(query) > 400 {
		return "", chatsearch.ErrInvalid
	}
	return query, nil
}

// sharedFilesFromResponse reads a page of the answer. Rows of another
// conversation, and rows that name no attachment, are dropped; unavailable is
// true when the file source itself could not answer.
func sharedFilesFromResponse(response chatsearch.Response, conversationID string) (files []chatui.SharedFile, more, unavailable bool) {
	for _, kind := range response.Unavailable {
		if kind == chatsearch.File {
			unavailable = true
		}
	}
	for _, group := range response.Groups {
		if group.Kind != chatsearch.File {
			continue
		}
		for _, row := range group.Rows {
			if row.Target.ConversationID != conversationID || row.Target.MessageID == "" || row.Target.ItemID == "" {
				continue
			}
			files = append(files, chatui.SharedFile{
				ID: row.ID, PostID: row.Target.MessageID, MediaID: row.Target.ItemID, Name: row.Text, SenderID: row.AuthorID, At: row.At,
				Sequence: row.Target.Sequence, ThreadID: row.Target.ThreadID, ThreadSequence: row.Target.ThreadSequence,
			})
		}
	}
	return files, response.NextCursor != "", unavailable
}

// sharedFilesMerge appends a page to the files already read, once each.
func sharedFilesMerge(have, page []chatui.SharedFile) []chatui.SharedFile {
	seen := make(map[string]bool, len(have)+len(page))
	out := make([]chatui.SharedFile, 0, len(have)+len(page))
	for _, list := range [][]chatui.SharedFile{have, page} {
		for _, file := range list {
			if !seen[file.ID] {
				seen[file.ID] = true
				out = append(out, file)
			}
		}
	}
	return out
}

// sharedFileByID is a row of the open conversation's list.
func sharedFileByID(model chatui.Model, id string) (chatui.SharedFile, bool) {
	if model.SharedFiles.ConversationID != model.SelectedID || id == "" {
		return chatui.SharedFile{}, false
	}
	for _, file := range model.SharedFiles.Files {
		if file.ID == id {
			return file, true
		}
	}
	return chatui.SharedFile{}, false
}

// sharedFileName is the name of an attachment the list shows, for a download
// of a file whose message is not on screen.
func sharedFileName(model chatui.Model, postID, mediaID string) string {
	if file, ok := sharedFileByID(model, postID+":"+mediaID); ok {
		return file.Name
	}
	return ""
}
