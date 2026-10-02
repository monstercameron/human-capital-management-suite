package chatsearch

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_CHATSEARCH_002_Sentence: the sentence a voice result opens at is
// the first that holds every searched word, else the first that holds any,
// and none when the words are in no sentence or there are no words.
func TestTodo_CHATSEARCH_002_Sentence(t *testing.T) {
	spoken := []string{"Good morning everyone.", "The budget is late.", "The budget review is on Friday."}
	for words, want := range map[string]int{
		"budget":         2,
		"BUDGET review":  3,
		"friday budget":  3,
		"morning friday": 1,
		"payroll":        0,
		"":               0,
		"*":              0,
		"* friday":       3,
	} {
		if got := Sentence(spoken, words); got != want {
			t.Errorf("Sentence(%q) = %d, want %d", words, got, want)
		}
	}
	if got := Sentence(nil, "budget"); got != 0 {
		t.Errorf("a transcript with no sentences names sentence %d", got)
	}
	a := Target{ConversationID: "room", MessageID: "post", ItemID: "voice", Sequence: 4, Sentence: 3}
	b := a
	b.Sentence = 0
	if !SameTarget(a, b) || a == b {
		t.Fatal("two results for one recording differ by more than their sentence")
	}
	b.MessageID = "another"
	if SameTarget(a, b) {
		t.Fatal("results for two messages are taken for one")
	}
}

// TestTodo_CHATSEARCH_003_Declarations: the agent kinds each state what text
// is found, who may find it and where it opens; they are no part of the Chat
// content registry until a composition registers them, and once registered
// they are named among the kinds an answer can hold and can be asked for.
func TestTodo_CHATSEARCH_003_Declarations(t *testing.T) {
	declared := map[Kind]bool{}
	for _, d := range Declarations() {
		declared[d.Kind] = true
	}
	want := []Kind{Agent, AgentTask, AgentAnnouncement}
	got := AgentDeclarations()
	if len(got) != len(want) {
		t.Fatalf("agent kinds = %+v", got)
	}
	r := NewRegistry()
	none := SourceFuncs{SearchRows: func(context.Context, Request) ([]Row, error) { return nil, nil }, OpenRow: func(context.Context, Actor, Row) (bool, error) { return true, nil }}
	q := searchRequest()
	for i, d := range got {
		if d.Kind != want[i] || d.Text == "" || d.Audience == "" || d.Opens == "" {
			t.Fatalf("declaration %d = %+v", i, d)
		}
		if declared[d.Kind] {
			t.Fatalf("%s is also a Chat content kind", d.Kind)
		}
		if same, ok := AgentDeclaration(d.Kind); !ok || same != d {
			t.Fatalf("AgentDeclaration(%s) = %+v %v", d.Kind, same, ok)
		}
		// Not searchable, and not a kind a query may name, before it is registered.
		q.Filters.Kind = d.Kind
		if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
			t.Fatalf("kind %s was accepted before any source declared it: %v", d.Kind, err)
		}
		if !errors.Is(r.RegisterSource(d.Kind, none), ErrRegistry) {
			t.Fatalf("%s was registered without its declaration", d.Kind)
		}
		if err := r.Register(d, none); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Search(context.Background(), q); err != nil {
			t.Fatalf("kind %s after registration: %v", d.Kind, err)
		}
	}
	if _, ok := AgentDeclaration(Message); ok {
		t.Fatal("a Chat kind is reported as an agent kind")
	}
	q.Filters.Kind = ""
	answer, err := r.Search(context.Background(), q)
	if err != nil || len(answer.Kinds) != len(want) {
		t.Fatalf("kinds = %v err = %v", answer.Kinds, err)
	}
}
