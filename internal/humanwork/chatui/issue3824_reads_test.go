package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// TestIssue3824_ReadersAreDeduplicatedAndNamed: the same reader, purpose and day
// shows once, with the reader's full name.
func TestIssue3824_ReadersAreDeduplicatedAndNamed(t *testing.T) {
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	read := chatgate.ReadAudit{Person: "p1", Reader: "admin1", Purpose: "Gate answers view", At: at}
	v := GateView{Locale: "en-US", Names: map[string]string{"admin1": "Dana Whitfield"}, Reads: []chatgate.ReadAudit{read, read, read}}
	rows := gateReaderRows(v)
	if len(rows) != 1 || rows[0] != "Dana Whitfield · Gate answers view · 2026-11-12" {
		t.Fatalf("rows: %q", rows)
	}
	if strings.Contains(strings.Join(rows, ""), "Linh") {
		t.Fatal("own read listed")
	}
}

// TestIssue3824_AnswerCountPlural: one answer reads "1 answer".
func TestIssue3824_AnswerCountPlural(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "1 answer", "de-DE": "1 Antwort", "ar": "إجابة واحدة"} {
		if got := gateAnswerCount(locale, 1); got != want {
			t.Errorf("%s: %q want %q", locale, got, want)
		}
	}
	if got := gateAnswerCount("en-US", 2); got != "2 answers" {
		t.Errorf("got %q", got)
	}
}
