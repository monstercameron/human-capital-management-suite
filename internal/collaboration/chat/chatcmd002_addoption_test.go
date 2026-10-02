package chat

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_CHATCMD_002_AddOptionRules(t *testing.T) {
	card := Chatcmd002Card{Kind: "poll", Title: "Q?", Poll: &Chatcmd002Poll{Results: "always", AddOptions: "members", Options: []ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pho"}}}}
	next, err := card.AddOption("c", "  Salad ")
	if err != nil || len(next.Poll.Options) != 3 || next.Poll.Options[2].Text != "Salad" || len(card.Poll.Options) != 2 {
		t.Fatalf("add %+v %v; the original must not change (%+v)", next, err, card.Poll.Options)
	}
	for text, want := range map[string]error{" tacos": ErrConflict, "": ErrInvalidArgument, "line\x00break": ErrInvalidArgument} {
		if _, err := card.AddOption("d", text); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", text, err, want)
		}
	}
	if _, err := card.AddOption("", "Ramen"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("an option without an id: %v", err)
	}
	if _, err := (Chatcmd002Card{Kind: "todo", Title: "T"}).AddOption("e", "x"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("a list takes no options: %v", err)
	}
	now := time.Now()
	closed := now.Add(-time.Hour)
	if !card.CanAddOption(now, false) {
		t.Error("members may add to a poll that allows it")
	}
	card.Poll.AddOptions = "author"
	if card.CanAddOption(now, false) || !card.CanAddOption(now, true) {
		t.Error("an author-only poll takes options from its author only")
	}
	card.ClosedAt = &closed
	if card.CanAddOption(now, true) {
		t.Error("a closed poll takes none")
	}
	card.ClosedAt = nil
	for len(card.Poll.Options) < Chatcmd002MaxOptions {
		card.Poll.Options = append(card.Poll.Options, ChannelPollOption{ID: string(rune('A' + len(card.Poll.Options))), Text: string(rune('A' + len(card.Poll.Options)))})
	}
	if card.CanAddOption(now, true) {
		t.Error("a poll of twelve takes no more")
	}
}
