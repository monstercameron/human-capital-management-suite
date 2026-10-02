package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// chatlangDetected is CHATLANG-008's second half of "detected when it is sent and,
// for older messages, the first time a reader opens the conversation". A message
// that has no recorded language (one written before detection existed, or before
// this revision was annotated) is detected now with the same local detector, and
// the language is recorded so the next read starts from it. A text that really
// has no language of its own ("ok", a name) stays "no language" and is not
// written again. A correction by the writer always stands.
func (s integrate2RenderingPolicy) chatlangDetected(ctx context.Context, scope chatstore.RenderingScope, id string, revision uint64, body string, stored chatrender.Detection) chatrender.Detection {
	if stored.Corrected {
		return stored
	}
	if language := chatrender.Language(stored.Language); language != "" && language != "und" {
		return stored
	}
	fresh := chatrender.Detect(body)
	if language := chatrender.Language(fresh.Language); language == "" || language == "und" {
		return stored
	}
	// Recording is best effort: a failure leaves the message detected for this
	// read and detected again for the next.
	_ = s.Store.RecordRevisionLanguage(ctx, scope.Tenant, id, revision)
	return fresh
}

// chatlangOwn reports whether the reader wrote the post.
func chatlangOwn(reader chat.Principal, post chat.Post) bool {
	return post.AuthorID != "" && post.AuthorID == reader.SubjectID && (post.AuthorHomeTenantID == "" || post.AuthorHomeTenantID == reader.TenantID)
}
