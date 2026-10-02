package chatsearch

import "testing"

func TestTodo_CHATSEARCH_002_Window(t *testing.T) {
	q := searchRequest()
	q.Limit = 1
	rows := []Row{{Kind: Saved, ID: "a", At: q.At}, {Kind: Saved, ID: "c", At: q.At}, {Kind: Saved, ID: "b", At: q.At}}
	got := WindowRows(rows, q)
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "b" || rows[0].ID != "a" {
		t.Fatalf("window mutated input or lost ordering: %+v", got)
	}
	q.BeforeAt, q.BeforeKey = q.At, rowKey(got[1])
	got = WindowRows(rows, q)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("keyset %+v", got)
	}
	q.BeforeKey = ""
	q.OpenIDs = []string{"b"}
	got = WindowRows(rows, q)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("recheck window %+v", got)
	}
}
