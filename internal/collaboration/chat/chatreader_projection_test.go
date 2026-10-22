package chat

import (
	"context"
	"testing"
)

// hidingProjector replaces every post of a writer it is told to, and answers
// with the wrong number of entries when it is told to.
type hidingProjector struct {
	hide    string
	crooked bool
}

func (p hidingProjector) ProjectReaderBodies(_ context.Context, _ Principal, posts []Post) []ReaderBody {
	if p.crooked {
		return nil
	}
	out := make([]ReaderBody, len(posts))
	for i, post := range posts {
		if post.AuthorID == p.hide {
			out[i] = ReaderBody{Text: "stand-in", Replaced: true}
		}
	}
	return out
}

func TestReaderBodyProjection(t *testing.T) {
	ctx := context.Background()
	reader := Principal{TenantID: "t", SubjectID: "reader"}
	posts := func() []Post {
		return []Post{{ID: "a", AuthorID: "heated", Body: "the original"}, {ID: "b", AuthorID: "calm", Body: "as written"}, {ID: "c", AuthorID: "heated", Deleted: true}}
	}

	// No projector: nothing is hidden, and nothing is asked.
	plain := &Service{}
	if got := plain.maskReaderBodies(ctx, reader, posts()); len(got) != 3 || got[0] || got[1] || got[2] {
		t.Fatalf("with no projector: %v", got)
	}
	if !plain.readerMayReadAsWritten(ctx, reader, Post{ID: "a", AuthorID: "heated", Body: "x"}) {
		t.Fatal("with no projector a reader may read as written")
	}

	s := &Service{}
	s.SetReaderBodyProjector(hidingProjector{hide: "heated"})
	list := posts()
	replaced := s.maskReaderBodies(ctx, reader, list)
	if !replaced[0] || list[0].Body != "stand-in" || replaced[1] || list[1].Body != "as written" {
		t.Fatalf("a list: %v %+v", replaced, list)
	}
	if got := s.HideUnreadableOriginal(ctx, reader, Post{ID: "a", AuthorID: "heated", Body: "the original"}); got.Body != "stand-in" {
		t.Fatalf("a post read by its identity: %q", got.Body)
	}
	if got := s.HideUnreadableOriginal(ctx, reader, Post{ID: "b", AuthorID: "calm", Body: "as written"}); got.Body != "as written" {
		t.Fatalf("a post the reader may read: %q", got.Body)
	}
	if s.readerMayReadAsWritten(ctx, reader, Post{ID: "a", AuthorID: "heated", Body: "the original"}) || !s.readerMayReadAsWritten(ctx, reader, Post{ID: "b", AuthorID: "calm", Body: "as written"}) {
		t.Fatal("the copy check does not follow the projector")
	}

	// An answer that does not line up with the posts cannot be trusted for any:
	// every post with a body is replaced rather than passed through.
	bad := &Service{}
	bad.SetReaderBodyProjector(hidingProjector{crooked: true})
	if got := bad.projectReaderBodies(ctx, reader, posts()); len(got) != 3 || !got[0].Replaced || !got[1].Replaced || got[2].Replaced {
		t.Fatalf("a crooked answer: %+v", got)
	}
}

func TestSearchWordsShown(t *testing.T) {
	for _, tc := range []struct {
		shown, query string
		want         bool
	}{
		{"The deploy broke prod again.", "deploy", true},
		{"The deploy broke prod again.", "DEPLOY prod", true},
		{"The deploy broke prod again.", "idiot", false},
		{"The deploy broke prod again.", "deploy idiot", false},
		{"The deploy broke prod again.", "prod.", true},
		{"anything", "   ", false},
	} {
		if got := searchWordsShown(tc.shown, tc.query); got != tc.want {
			t.Errorf("%q in %q = %v, want %v", tc.query, tc.shown, got, tc.want)
		}
	}
}
