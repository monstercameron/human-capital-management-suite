package chatui

import "testing"

// TestIssue3902_MinimalEditIsTheOneReplacement: a toolbar action's new draft is
// handed to the field as one replacement of the smallest range, in UTF-16 units.
func TestIssue3902_MinimalEditIsTheOneReplacement(t *testing.T) {
	for _, tc := range []struct {
		name, old, updated string
		start, end         int
		insert             string
	}{
		{"bold", "Please review this draft", "Please **review** this draft", 7, 13, "**review**"},
		{"emoji", "Test ", "Test 🍕", 5, 5, "🍕"},
		{"unwrap", "a **b** c", "a b c", 2, 7, "b"},
		{"same", "same", "same", 4, 4, ""},
		{"empty", "", "x", 0, 0, "x"},
	} {
		start, end, insert := composerMinimalEdit(tc.old, tc.updated)
		if tc.name == "unwrap" {
			// the edge may sit anywhere inside the repeated asterisks; what matters is the result
			if got := applyUTF16Edit(tc.old, start, end, insert); got != tc.updated {
				t.Errorf("%s: edit (%d,%d,%q) gives %q, want %q", tc.name, start, end, insert, got, tc.updated)
			}
			continue
		}
		if start != tc.start || end != tc.end || insert != tc.insert {
			t.Errorf("%s: got (%d,%d,%q), want (%d,%d,%q)", tc.name, start, end, insert, tc.start, tc.end, tc.insert)
		}
	}
}

// TestIssue3902_MinimalEditNeverSplitsASurrogatePair: replacing one emoji with
// another sharing a high surrogate must not cut between the halves.
func TestIssue3902_MinimalEditNeverSplitsASurrogatePair(t *testing.T) {
	old, updated := "hi 😀 there", "hi 😁 there"
	start, end, insert := composerMinimalEdit(old, updated)
	if start != 3 || end != 5 || insert != "😁" {
		t.Fatalf("got (%d,%d,%q), want (3,5,%q)", start, end, insert, "😁")
	}
	if got := applyUTF16Edit(old, start, end, insert); got != updated {
		t.Fatalf("applied edit gives %q, want %q", got, updated)
	}
}

func applyUTF16Edit(value string, start, end int, insert string) string {
	out, _ := composerInsertAt(value, insert, start, end)
	return out
}
