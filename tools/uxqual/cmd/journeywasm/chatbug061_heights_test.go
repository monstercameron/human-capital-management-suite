package main

import (
	"strconv"
	"testing"
)

// The page keeps the height each answer card was drawn at, replaces a height
// that changed, ignores a card that is not laid out, and never grows without
// bound.
func TestTodo_CHATBUG_061(t *testing.T) {
	known, changed := chatbug061MergeHeights(nil, map[string]int{"question": 236, "hidden": 0, "": 90})
	if !changed || len(known) != 1 || known["question"] != 236 {
		t.Fatalf("first measurement kept %+v", known)
	}
	same, changed := chatbug061MergeHeights(known, map[string]int{"question": 236})
	if changed || len(same) != 1 {
		t.Fatalf("an unchanged measurement reported a change: %+v", same)
	}
	grown, changed := chatbug061MergeHeights(known, map[string]int{"question": 264, "other": 180})
	if !changed || grown["question"] != 264 || grown["other"] != 180 {
		t.Fatalf("a changed measurement kept %+v", grown)
	}
	if known["question"] != 236 {
		t.Fatal("a measurement wrote into the map a render may still be reading")
	}
	full := map[string]int{}
	for index := 0; index < chatbug061HeightLimit; index++ {
		full["post-"+strconv.Itoa(index)] = 200
	}
	capped, changed := chatbug061MergeHeights(full, map[string]int{"one-more": 210, "post-0": 220})
	if !changed || len(capped) != chatbug061HeightLimit || capped["post-0"] != 220 {
		t.Fatalf("the kept heights are not bounded: %d entries, post-0=%d", len(capped), capped["post-0"])
	}
	if _, held := capped["one-more"]; held {
		t.Fatal("a card beyond the limit was kept")
	}
}
