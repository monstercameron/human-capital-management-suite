package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestTodo_CHATSEARCH_002_Security_Files: an attachment's name is found by the
// people who may read the message it is on, and by nobody else. Every file
// used to be withheld from everyone.
func TestTodo_CHATSEARCH_002_Security_Files(t *testing.T) {
	reader := &integrate2ReaderFixture{post: chat.Post{ID: "post", TenantID: "tenant-a", ConversationID: "room", AuthorID: "bob", Body: "Budget attached", Revision: 1}}
	authority := integrate2SearchRowAuthority(reader)
	actor := chatsearch.Actor{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "alice"}
	file := chatsearch.Row{Kind: chatsearch.File, ID: "post:artifact", TenantID: "tenant-a", Text: "budget-2026.xlsx", HasFile: true, Target: chatsearch.Target{ConversationID: "room", MessageID: "post", ItemID: "artifact", Sequence: 4}}
	if ok, err := authority(t.Context(), actor, file); err != nil || !ok {
		t.Fatalf("a reader of the message cannot find its file: %v %v", ok, err)
	}
	// The conversation check that precedes text matching carries no row.
	if ok, err := authority(t.Context(), actor, chatsearch.Row{Kind: chatsearch.File, Target: chatsearch.Target{ConversationID: "room"}}); err != nil || !ok {
		t.Fatalf("the conversation check: %v %v", ok, err)
	}
	// A message the reader can no longer read takes its file with it.
	reader.denied = true
	if ok, err := authority(t.Context(), actor, file); err != nil || ok {
		t.Fatalf("a file was found on a message its reader may not read: %v %v", ok, err)
	}
	reader.denied = false
	// So does a deleted message, and a message that is not there.
	reader.post.Deleted = true
	if ok, err := authority(t.Context(), actor, file); err != nil || ok {
		t.Fatalf("a file was found on a deleted message: %v %v", ok, err)
	}
	reader.post.Deleted = false
	elsewhere := file
	elsewhere.Target.MessageID = "another-post"
	if ok, err := authority(t.Context(), actor, elsewhere); err != nil || ok {
		t.Fatalf("a file was found on a message that is not in the conversation: %v %v", ok, err)
	}
	// A file that names no message has nothing to be checked against.
	loose := file
	loose.Target.MessageID = ""
	if ok, err := authority(t.Context(), actor, loose); err != nil || ok {
		t.Fatalf("a file with no message was found: %v %v", ok, err)
	}
	// A cited document is still not found on a message's say-so.
	source := file
	source.Kind = chatsearch.SourceTitle
	if ok, err := authority(t.Context(), actor, source); err != nil || ok {
		t.Fatalf("a source title was found: %v %v", ok, err)
	}
}

// chatsearchSpokenSource is a voice source that hands over the sentences of
// the one transcript it holds.
type chatsearchSpokenSource struct {
	row    chatsearch.Row
	spoken []string
}

func (s chatsearchSpokenSource) Search(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
	rows, _, err := s.SearchSentences(ctx, q)
	return rows, err
}

func (s chatsearchSpokenSource) SearchSentences(context.Context, chatsearch.Request) ([]chatsearch.Row, map[string][]string, error) {
	return []chatsearch.Row{s.row}, map[string][]string{s.row.ID: s.spoken}, nil
}

func (chatsearchSpokenSource) CanOpen(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) {
	return true, nil
}

// TestTodo_CHATSEARCH_002_VoiceSentence: a voice result names the sentence
// that holds the searched words in what its reader is shown, so opening it
// seeks there; a word a filter hides never decides the sentence; and the
// recheck, which carries no words, still lets the result through.
func TestTodo_CHATSEARCH_002_VoiceSentence(t *testing.T) {
	reader := &integrate2ReaderFixture{post: chat.Post{ID: "post", TenantID: "tenant-a", ConversationID: "room", AuthorID: "bob", AuthorHomeTenantID: "tenant-a", Body: "Voice", Revision: 1}}
	spoken := []string{"Good morning everyone.", "The quartz budget is late.", "The budget review is on Friday."}
	row := chatsearch.Row{Kind: chatsearch.Voice, ID: "post:voice", TenantID: "tenant-a", Text: "Good morning everyone. The quartz budget is late. The budget review is on Friday.", HasVoice: true, Target: chatsearch.Target{ConversationID: "room", MessageID: "post", ItemID: "voice", Sequence: 9}}
	store := &integrate2SearchFilterStore{chatfilterHTTPStore: chatfilterHTTPStore{defs: []chatfilter.Definition{{ID: "rule", Version: "1.0.0", Name: "Rule", Kind: "words", Match: []string{"quartz"}, Action: "mask"}}}}
	policy := &chat.FilterContentPolicy{Filters: &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry()}}
	source := integrate2VoiceSource{Source: chatsearchSpokenSource{row: row, spoken: spoken}, Reader: reader, Filter: policy}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "alice"}, At: time.Now()}

	for words, want := range map[string]int{"friday": 3, "budget": 2, "budget review": 3, "morning": 1, "*": 0, "": 0} {
		q.Query = words
		rows, err := source.Search(t.Context(), q)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%q: rows=%+v err=%v", words, rows, err)
		}
		if rows[0].Target.Sentence != want {
			t.Fatalf("%q seeks to sentence %d, want %d", words, rows[0].Target.Sentence, want)
		}
		if words == "friday" {
			if allowed, err := source.CanOpen(t.Context(), q.Actor, rows[0]); err != nil || !allowed {
				t.Fatalf("a result that names a sentence is refused at the recheck: %v %v", allowed, err)
			}
			moved := rows[0]
			moved.Target.MessageID = "another-post"
			if allowed, _ := source.CanOpen(t.Context(), q.Actor, moved); allowed {
				t.Fatal("a result for another message passed the recheck")
			}
		}
	}
	// The masked word is not found, so it names no sentence either.
	q.Query = "quartz"
	if rows, err := source.Search(t.Context(), q); err != nil || len(rows) != 0 {
		t.Fatalf("the masked word matched: %+v %v", rows, err)
	}
	// A source that hands over no sentences (a correction) seeks nowhere.
	source.Source = chatsearchSpokenSource{row: row}
	q.Query = "friday"
	if rows, err := source.Search(t.Context(), q); err != nil || len(rows) != 1 || rows[0].Target.Sentence != 0 {
		t.Fatalf("a transcript with no timed sentences: %+v %v", rows, err)
	}
}
