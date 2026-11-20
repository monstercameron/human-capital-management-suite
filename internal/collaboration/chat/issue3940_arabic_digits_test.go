package chat

import (
	"testing"
	"time"
)

// TestIssue3940_ArabicIndicDigitsReadAsDates: a date written with Arabic-Indic
// (or Persian) digits means the same as the Western-digit date, the task keeps
// its words, and unreadable figures are reported rather than guessed.
func TestIssue3940_ArabicIndicDigitsReadAsDates(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ western, native string }{
		{"مراجعة الملف 9 أكتوبر", "مراجعة الملف ٩ أكتوبر"},
		{"مراجعة الملف 9 أكتوبر 2027", "مراجعة الملف ٩ أكتوبر ٢٠٢٧"},
		{"مراجعة الملف بحلول 2027-03-13", "مراجعة الملف بحلول ٢٠٢٧-٠٣-١٣"},
		{"مراجعة الملف بحلول 9 أكتوبر", "مراجعة الملف بحلول ۹ أكتوبر"},
		{"مراجعة الملف أكتوبر 9", "مراجعة الملف أكتوبر ٩"},
	} {
		want, err := Chatcmd004ParseTodo(tc.western, nil, "me", now, Chatcmd004ResolveDate)
		if err != nil || want.Card.Todo.Items[0].DueAt == nil {
			t.Fatalf("control %q: %+v %v", tc.western, want, err)
		}
		got, err := Chatcmd004ParseTodo(tc.native, nil, "me", now, Chatcmd004ResolveDate)
		if err != nil || got.Card.Todo.Items[0].DueAt == nil {
			t.Fatalf("%q: no due date: %+v %v", tc.native, got.Card.Todo.Items, err)
		}
		if !got.Card.Todo.Items[0].DueAt.Equal(*want.Card.Todo.Items[0].DueAt) || got.Card.Todo.Items[0].Text != "مراجعة الملف" {
			t.Fatalf("%q: got %v %q, want %v", tc.native, got.Card.Todo.Items[0].DueAt, got.Card.Todo.Items[0].Text, want.Card.Todo.Items[0].DueAt)
		}
	}
	// A day that does not exist stays in the text and is reported, not invented.
	d, _ := Chatcmd004ParseTodo("مراجعة الملف بحلول ٣٠ فبراير", nil, "me", now, Chatcmd004ResolveDate)
	if d.Card.Todo.Items[0].DueAt != nil || d.Card.Todo.Items[0].Text != "مراجعة الملف بحلول ٣٠ فبراير" || len(d.Issues) != 1 || d.Issues[0] != "date" {
		t.Fatalf("impossible date: %+v", d)
	}
}
