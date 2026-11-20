package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type issue3919Policy struct {
	refuse string
	seen   []string
}

func (p *issue3919Policy) CheckContent(_ context.Context, in ContentInput) error {
	p.seen = append(p.seen, in.Body)
	if p.refuse != "" && strings.Contains(in.Body, p.refuse) {
		return ErrPermissionDenied
	}
	return nil
}

func issue3919Rig(kind ConversationKind) (*fakeStore, *Service) {
	room := conversation()
	room.Kind = kind
	f := &fakeStore{conversation: room, membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u2", Body: "source", Revision: 3}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	return f, s
}

func issue3919Forward(note string) ForwardPostRequest {
	return ForwardPostRequest{Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1", DestinationTenantID: "t1", DestinationConversationID: "c1", IdempotencyKey: "note-1", Note: note}
}

// The note is the sharer's own words: kept beside the forwarded message, the
// message itself stays the original author's and the sharer's name is on the post.
func TestIssue3919_ForwardKeepsTheSharersNoteBesideTheOriginalMessage(t *testing.T) {
	for _, kind := range []ConversationKind{Direct, Group, PrivateChannel} {
		f, s := issue3919Rig(kind)
		if _, err := s.ForwardPost(context.Background(), issue3919Forward("  Can we talk about this?  ")); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		got := f.sent
		if got.Body != "source" || got.AuthorID != "u1" {
			t.Fatalf("%s: forwarded post = %+v, want the source body under the sharer", kind, got)
		}
		if got.SourceAttribution == nil || got.SourceAttribution.Note != "Can we talk about this?" || got.SourceAttribution.OriginalAuthorID != "u2" {
			t.Fatalf("%s: attribution = %+v, want the trimmed note and the original author", kind, got.SourceAttribution)
		}
	}
}

func TestIssue3919_ForwardWithoutANoteStoresNone(t *testing.T) {
	f, s := issue3919Rig(Direct)
	if _, err := s.ForwardPost(context.Background(), issue3919Forward("   ")); err != nil {
		t.Fatal(err)
	}
	if f.sent.SourceAttribution == nil || f.sent.SourceAttribution.Note != "" {
		t.Fatalf("attribution = %+v, want no note", f.sent.SourceAttribution)
	}
}

func TestIssue3919_ForwardRefusesATooLongNoteBeforeWriting(t *testing.T) {
	f, s := issue3919Rig(Direct)
	_, err := s.ForwardPost(context.Background(), issue3919Forward(strings.Repeat("é", MaxForwardNoteRunes+1)))
	if !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("err = %v, mutations = %d; want invalid argument and no write", err, f.mutations)
	}
	if _, err := s.ForwardPost(context.Background(), issue3919Forward(strings.Repeat("é", MaxForwardNoteRunes))); err != nil {
		t.Fatalf("a note of exactly the limit: %v", err)
	}
}

func TestIssue3919_ForwardNoteGoesThroughTheDestinationContentPolicy_Security(t *testing.T) {
	f, s := issue3919Rig(Group)
	policy := &issue3919Policy{refuse: "forbidden"}
	s.SetContentPolicy(policy)
	_, err := s.ForwardPost(context.Background(), issue3919Forward("this is forbidden"))
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("err = %v, mutations = %d; want the policy to refuse the note before any write", err, f.mutations)
	}
}

func TestIssue3919_ForwardToAConversationTheSharerCannotPostInIsRefused_Security(t *testing.T) {
	f, s := issue3919Rig(Group)
	s.SetAuthority(deniedReferenceAuthority{action: chatpolicy.ActionPost})
	_, err := s.ForwardPost(context.Background(), issue3919Forward("hello"))
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("err = %v, mutations = %d; want denial before a write", err, f.mutations)
	}
}

// A client cannot smuggle a note, or an author, in through the embedded
// attribution of the request: only the note field is read.
func TestIssue3919_ForwardIgnoresANoteSmuggledInTheEmbeddedAttribution_Security(t *testing.T) {
	f, s := issue3919Rig(Direct)
	r := issue3919Forward("")
	r.SourceAttribution = SourceAttribution{PostRevision: 3, Note: "forged", OriginalAuthorID: "someone-else"}
	if _, err := s.ForwardPost(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := f.sent.SourceAttribution; got.Note != "" || got.OriginalAuthorID != "u2" {
		t.Fatalf("attribution = %+v, want the server-derived values only", got)
	}
}
