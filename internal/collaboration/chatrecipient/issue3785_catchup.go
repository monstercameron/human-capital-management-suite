package chatrecipient

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3785: "Catch me up". A private brief of what a person missed in one
// conversation or thread: the messages that ask them something, the ones that
// read like a decision, and the ones that point at work. Each line names the
// exact post it comes from.
//
// The brief is worked out on request from posts the person can read right now
// (active membership, history and post audience, as everywhere else), bounded
// to CatchUpLimit posts, and is never stored. Producing it never moves the
// person's read position and never writes a post. It is extractive: no model
// reads the messages, so a message's text is only ever shown back, as the
// words of the person who wrote it, and never acted on. A line is what someone
// said, not verified workflow state: a post that says "approved" is not an
// approval, and the brief says so by being attributed rather than by reporting
// status.
const (
	// CatchUpLimit is how many posts one brief reads. When more were missed, the
	// newest are read and the rest are counted as omitted.
	CatchUpLimit = 300
	// CatchUpLineLimit is how many lines one section lists; the rest are counted.
	CatchUpLineLimit = 8
	// CatchUpSnippetRunes is how much of a post a line shows.
	CatchUpSnippetRunes = 160

	CatchUpAsked     = "asked_you"
	CatchUpDecisions = "decisions"
	CatchUpWork      = "work"

	// CatchUpMethodExtractive says the brief was picked out of the messages by
	// rule, not written by a model.
	CatchUpMethodExtractive = "extractive"
)

// CatchUpPost is one post the person may read, as the store returns it.
type CatchUpPost struct {
	ID           string
	Sequence     uint64
	AuthorID     string
	Body         string
	ThreadRootID string
	At           time.Time
	// Mentioned is true when the post mentions the reader by name.
	Mentioned bool
	// Refs are the typed references the post carries.
	Refs []chat.Reference
}

// CatchUpRange is what the store captured, in one transaction, for one reader.
type CatchUpRange struct {
	ConversationName string
	// Cursor is the reader's read position when the range was captured.
	Cursor uint64
	// After is the sequence the range starts after: the read position, or just
	// before the post the reader asked to start from.
	After uint64
	// Head is the newest sequence the reader may read at the moment of capture.
	Head uint64
	// Total is how many readable posts fall in (After, Head].
	Total int
	// Posts are the newest of them, at most the limit asked, oldest first.
	Posts []CatchUpPost
}

// CatchUpStore is the optional storage side of the brief. It is a read.
type CatchUpStore interface {
	// CatchUpRange reads the posts after the reader's read position (or just
	// before anchorPostID when given) up to the head of the conversation, or of
	// the thread rooted at threadRootID. It refuses a reader who is not an active
	// member and an anchor or thread the reader may not read.
	CatchUpRange(ctx context.Context, host, home, subject, conversation, threadRootID, anchorPostID string, limit int) (CatchUpRange, error)
}

// CatchUpLine is one cited line of the brief.
type CatchUpLine struct {
	PostID       string
	Sequence     uint64
	ThreadRootID string
	AuthorID     string
	Text         string
	At           time.Time
}

// CatchUpSection is one kind of line. More counts the lines that did not fit.
type CatchUpSection struct {
	Kind  string
	Lines []CatchUpLine
	More  int
}

// CatchUpBrief is what the person is shown.
type CatchUpBrief struct {
	HostTenantID, ConversationID, ConversationName, ThreadRootID string
	// Cursor is the read position the brief was taken at. After and Through are
	// the sequences the covered range lies between, and CoveredFrom is the first
	// post actually read.
	Cursor, After, Through, CoveredFrom uint64
	// Total is how many posts were missed, Covered how many were read and Omitted
	// how many were not (the oldest, when more than CatchUpLimit).
	Total, Covered, Omitted int
	Truncated               bool
	Generated               time.Time
	Method                  string
	Sections                []CatchUpSection
}

func (s *Service) catchUpStore() (CatchUpStore, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(CatchUpStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// CatchUp works out the caller's brief for one conversation, or for one thread
// when threadRootID is set. The conversation is admitted before anything is
// read, and the store reads only what the caller may read now.
func (s *Service) CatchUp(ctx context.Context, p chat.Principal, host, conversation, threadRootID, anchorPostID string, now time.Time) (CatchUpBrief, error) {
	store, err := s.catchUpStore()
	if err != nil {
		return CatchUpBrief{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || conversation == "" {
		return CatchUpBrief{}, chat.ErrInvalidArgument
	}
	if _, err := s.admit(ctx, p, host, conversation); err != nil {
		return CatchUpBrief{}, err
	}
	rng, err := store.CatchUpRange(ctx, host, p.TenantID, p.SubjectID, conversation, threadRootID, anchorPostID, CatchUpLimit)
	if err != nil {
		return CatchUpBrief{}, err
	}
	return buildCatchUp(rng, p, host, conversation, threadRootID, now), nil
}

func buildCatchUp(rng CatchUpRange, p chat.Principal, host, conversation, threadRootID string, now time.Time) CatchUpBrief {
	b := CatchUpBrief{
		HostTenantID: host, ConversationID: conversation, ConversationName: rng.ConversationName, ThreadRootID: threadRootID,
		Cursor: rng.Cursor, After: rng.After, Through: rng.Head, Total: rng.Total, Covered: len(rng.Posts),
		Generated: now.UTC(), Method: CatchUpMethodExtractive,
	}
	if b.Covered > b.Total {
		b.Total = b.Covered
	}
	b.Omitted = b.Total - b.Covered
	b.Truncated = b.Omitted > 0
	if len(rng.Posts) > 0 {
		b.CoveredFrom = rng.Posts[0].Sequence
	}
	sections := map[string]*CatchUpSection{}
	order := []string{CatchUpAsked, CatchUpDecisions, CatchUpWork}
	add := func(kind string, post CatchUpPost) {
		section := sections[kind]
		if section == nil {
			section = &CatchUpSection{Kind: kind}
			sections[kind] = section
		}
		if len(section.Lines) >= CatchUpLineLimit {
			section.More++
			return
		}
		section.Lines = append(section.Lines, CatchUpLine{PostID: post.ID, Sequence: post.Sequence, ThreadRootID: post.ThreadRootID, AuthorID: post.AuthorID,
			Text: catchUpSnippet(post.Body), At: post.At.UTC()})
	}
	for _, post := range rng.Posts {
		// What the reader wrote themselves is not something they missed.
		if post.AuthorID == p.SubjectID || post.ID == "" || post.Sequence == 0 || catchUpSnippet(post.Body) == "" {
			continue
		}
		switch {
		case post.Mentioned:
			add(CatchUpAsked, post)
		case catchUpDecisionCue(post.Body):
			add(CatchUpDecisions, post)
		case catchUpPointsAtWork(post.Refs):
			add(CatchUpWork, post)
		}
	}
	for _, kind := range order {
		if section := sections[kind]; section != nil {
			b.Sections = append(b.Sections, *section)
		}
	}
	return b
}

// catchUpSnippet is the first part of a post on one line.
func catchUpSnippet(body string) string {
	text := strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(text) <= CatchUpSnippetRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:CatchUpSnippetRunes])) + "…"
}

// catchUpDecisionCues are plain phrases that mark a message as one that states
// a decision. They are cues for a reader to look at, not a ruling: the brief
// lists such a message as something someone said.
var catchUpDecisionCues = []string{
	"we decided", "i decided", "decision:", "decided to", "we agreed", "agreed to", "let's go with", "we will go with", "going with",
	"wir haben beschlossen", "beschlossen", "wir haben entschieden", "entschieden", "wir haben uns geeinigt", "vereinbart",
	"قررنا", "تقرر", "اتفقنا", "القرار:",
}

func catchUpDecisionCue(body string) bool {
	lower := strings.ToLower(body)
	for _, cue := range catchUpDecisionCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// catchUpPointsAtWork is true for a post that carries a typed reference to a
// workflow, a journey or a project task. The reference says what the author
// pointed at; whether that work has moved is for its owning service to say.
func catchUpPointsAtWork(refs []chat.Reference) bool {
	for _, ref := range refs {
		switch ref.Kind {
		case chat.JourneyReference, chat.ProjectWorkReference, chat.WorkflowPageReferenceKind:
			return true
		}
	}
	return false
}
