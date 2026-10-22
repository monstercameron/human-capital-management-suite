package main

import (
	"encoding/json"
	"testing"
)

// TestTodo_CHATGATE_014_Client: the page reads the server's labels into what the
// details panel draws, drops a label nobody holds or without a name, and keeps
// what it had when the answer cannot be read.
func TestTodo_CHATGATE_014_Client(t *testing.T) {
	got, ok := chatgateLabels(json.RawMessage(`[{"name":"On call","people":["ana","ben"]},{"name":"Empty","people":[]},{"name":"","people":["cy"]}]`))
	if !ok || len(got) != 1 || got[0].Name != "On call" || len(got[0].People) != 2 || got[0].People[1] != "ben" {
		t.Fatalf("labels %+v %v", got, ok)
	}
	if got, ok := chatgateLabels(json.RawMessage(`[]`)); !ok || len(got) != 0 {
		t.Fatalf("an empty list is a list with nothing in it: %+v %v", got, ok)
	}
	for _, raw := range []string{``, `null`, `{"name":"x"}`, `not json`} {
		if got, ok := chatgateLabels(json.RawMessage(raw)); ok && raw != `null` {
			t.Errorf("%q was read as %+v", raw, got)
		}
	}
}
