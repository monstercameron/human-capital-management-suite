package application

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Listen to a thread (CHATVOICE-006): the root and its replies, in order, each
// led by its author's name. The posts are read as the person who asked, so a
// reply they cannot read is never spoken, and each is filtered and shaped like a
// single message (VoiceSpeaker.spokenPost).

const (
	// voiceThreadScanPages bounds the fallback walk of a conversation's history.
	voiceThreadScanPages = 30
	voiceThreadNameRunes = 60
)

// threadSnapshotReader is what the chat service offers for reading one thread at
// once; a store that only pages history leaves it unavailable.
type threadSnapshotReader interface {
	CaptureThreadSnapshot(context.Context, chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error)
}

// spokenPost is what a person would read of one post: the author's own words
// with masked text shown as it is shown, formatting marks dropped, a link as
// "link to" and a citation as "source".
func (s VoiceSpeaker) spokenPost(ctx context.Context, p chat.Principal, post chat.Post) (string, error) {
	body := chatui.AuthoredReaderText(post.Body)
	if s.Filter != nil {
		post.Body = body
		var err error
		if body, err = s.Filter.MaskedBody(ctx, post, p); err != nil {
			return "", err
		}
	}
	return VoiceSpokenText(body), nil
}

// threadText reads the thread and returns what is spoken for it.
func (s VoiceSpeaker) threadText(ctx context.Context, p chat.Principal, r VoiceSpeakRequest) (string, error) {
	posts, err := s.threadPosts(ctx, p, r)
	if err != nil {
		return "", err
	}
	var parts []string
	total := 0
	for _, post := range posts {
		spoken, err := s.spokenPost(ctx, p, post)
		if err != nil {
			return "", err
		}
		if spoken == "" {
			continue
		}
		if name := voiceSpokenName(r.Names[post.AuthorID]); name != "" {
			spoken = name + ". " + spoken
		}
		parts = append(parts, spoken)
		if total += len([]rune(spoken)) + 1; total > agentmodel.AudioMaxSpeechCharacters {
			return "", ErrVoiceTooLong
		}
	}
	return strings.Join(parts, " "), nil
}

// threadPosts returns the visible, undeleted root and replies in the order they
// were posted.
func (s VoiceSpeaker) threadPosts(ctx context.Context, p chat.Principal, r VoiceSpeakRequest) ([]chat.Post, error) {
	var posts []chat.Post
	keep := func(post chat.Post) {
		if post.TenantID == r.TenantID && post.ConversationID == r.ConversationID && !post.Deleted && (post.ID == r.ThreadID || post.ParentID == r.ThreadID) {
			posts = append(posts, post)
		}
	}
	if reader, ok := s.Reader.(threadSnapshotReader); ok {
		snapshot, err := reader.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: p, TenantID: r.TenantID, ConversationID: r.ConversationID, ThreadID: r.ThreadID, InvokingPostID: r.ThreadID, Limit: chat.MaxThreadSnapshotPosts})
		if err == nil {
			for _, post := range snapshot.Posts {
				keep(post)
			}
		}
		if len(posts) > 0 {
			sort.SliceStable(posts, func(i, j int) bool { return posts[i].Sequence < posts[j].Sequence })
			return posts, nil
		}
	}
	// The history walk: a thread has no listing of its own without the snapshot.
	page := chat.Page{PageSize: 200}
	seen := map[string]bool{}
	for i := 0; i < voiceThreadScanPages; i++ {
		result, err := s.Reader.ListPosts(ctx, chat.ListPostsRequest{Principal: p, TenantID: r.TenantID, ConversationID: r.ConversationID, Page: page})
		if err != nil {
			return nil, err
		}
		for _, post := range result.Posts {
			keep(post)
		}
		if result.NextCursor == "" || seen[result.NextCursor] {
			break
		}
		seen[result.NextCursor] = true
		page.Cursor = result.NextCursor
	}
	root := false
	for _, post := range posts {
		root = root || post.ID == r.ThreadID
	}
	if !root {
		return nil, chat.ErrNotFound
	}
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].Sequence < posts[j].Sequence })
	return posts, nil
}

// voiceSpokenName cleans a display name the page sent: no control characters,
// no markup marks, and a bounded length. It is only ever spoken.
func voiceSpokenName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("[]()<>*_`~#@", r) {
			return -1
		}
		return r
	}, name)
	runes := []rune(strings.Join(strings.Fields(name), " "))
	if len(runes) > voiceThreadNameRunes {
		runes = runes[:voiceThreadNameRunes]
	}
	return string(runes)
}
