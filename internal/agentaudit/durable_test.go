package agentaudit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENT2_014_DurableHelpers(t *testing.T) {
	entry := testEntry("event:h", "tenant:a", "user:a", "task:h", EventSkillCall)
	if err := ValidateEntry(entry); err != nil {
		t.Fatalf("ValidateEntry: %v", err)
	}
	bad := entry
	bad.Action = ""
	if err := ValidateEntry(bad); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("ValidateEntry err = %v, want ErrInvalidEntry", err)
	}
	if err := ValidateQuery(Query{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ValidateQuery(empty) err = %v, want ErrUnauthorized", err)
	}
	viewer := Viewer{TenantID: "tenant:a", UserID: "u", Role: ViewerAuditor}
	if err := ValidateQuery(Query{Viewer: viewer, Kinds: []EventKind{"NOPE"}}); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("ValidateQuery(kind) err = %v, want ErrInvalidEntry", err)
	}
	if err := ValidateQuery(Query{Viewer: viewer, Kinds: []EventKind{EventApproval}}); err != nil {
		t.Fatalf("ValidateQuery: %v", err)
	}

	first := SealRecord(entry, 1, "", time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC))
	second := SealRecord(testEntry("event:i", "tenant:a", "user:a", "task:h", EventSkillCall), 2, first.ChainHash, time.Now())
	if !first.Created || first.ChainHash == "" || first.ChainHash == second.ChainHash {
		t.Fatalf("unexpected sealed records %+v %+v", first, second)
	}
	if err := VerifyChain([]Record{first, second}); err != nil {
		t.Fatalf("sealed chain does not verify: %v", err)
	}
	// SealRecord must match what MemoryStore produces.
	mem := NewMemoryStore()
	got, _ := mem.Append(context.Background(), entry)
	if got.ChainHash != first.ChainHash {
		t.Fatalf("SealRecord hash %s != MemoryStore hash %s", first.ChainHash, got.ChainHash)
	}
	view := ProjectRecord(first, viewer)
	if len(view.Fields) != 2 || !view.Fields[1].Redacted || view.Fields[0].Value != "worker-7" {
		t.Fatalf("ProjectRecord view = %+v", view.Fields)
	}
	if !SameEntry(entry, cloneEntry(entry)) || SameEntry(entry, bad) {
		t.Fatal("SameEntry did not separate identical from changed entries")
	}
}
