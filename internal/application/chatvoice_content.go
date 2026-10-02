package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// VoiceRecordsReader finds the voice records of posts for a reader who may
// read them; a post the reader cannot read has no record.
type VoiceRecordsReader interface {
	VoiceRecordsForPosts(ctx context.Context, p chat.Principal, tenant, conversation string, posts []string) (map[string]chat.VoiceRecord, error)
}

// VoiceAwareThreadReader gives agents a voice message as its transcript and
// nothing else (CHATVOICE-005). The transcript arrives fenced as untrusted
// text, in the one message content reader (chat.MessageContentText) that typed
// messages go through; the audio attachment is dropped from the post, so no
// agent, to-do or reminder agent or tool call is ever handed audio. A transcript
// that cannot be read stops the read: an agent never sees a half-read thread.
type VoiceAwareThreadReader struct {
	Next  agentinvoke.ThreadReader
	Voice VoiceRecordsReader
}

var _ agentinvoke.ThreadReader = VoiceAwareThreadReader{}

func (r VoiceAwareThreadReader) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	if r.Next == nil {
		return nil, chat.ErrVoiceUnavailable
	}
	posts, err := r.Next.ReadThread(ctx, req)
	if err != nil || r.Voice == nil || len(posts) == 0 {
		return posts, err
	}
	ids := make([]string, 0, len(posts))
	for _, post := range posts {
		ids = append(ids, post.ID)
	}
	records, err := r.Voice.VoiceRecordsForPosts(ctx, chat.Principal{TenantID: req.TenantID, SubjectID: req.InvokerID}, req.TenantID, req.ConversationID, ids)
	if err != nil {
		return nil, err
	}
	for i, post := range posts {
		record, ok := records[post.ID]
		if !ok {
			continue
		}
		posts[i].Body = chat.MessageContentText(post.Body, &record)
		posts[i].Files = withoutArtifact(post.Files, record.ArtifactID)
		posts[i].Embeds = withoutArtifact(post.Embeds, record.ArtifactID)
	}
	return posts, nil
}

func withoutArtifact(files []agentinvoke.ThreadAttachment, artifact string) []agentinvoke.ThreadAttachment {
	var kept []agentinvoke.ThreadAttachment
	for _, f := range files {
		if f.ID != artifact {
			kept = append(kept, f)
		}
	}
	return kept
}

// voiceAwareThreads wraps the agents' thread reader when voice is composed.
func voiceAwareThreads(next agentinvoke.ThreadReader, runtime composedChat) agentinvoke.ThreadReader {
	if runtime.moderationStore == nil {
		return next
	}
	return VoiceAwareThreadReader{Next: next, Voice: runtime.moderationStore}
}
