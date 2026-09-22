package chatrecords

import (
	"context"
	"errors"
	"testing"
	"time"
)

type allowAuth struct{}

func (allowAuth) Authorize(context.Context, string, string, string, string) (string, error) {
	return "policy:v1", nil
}
func service() *Service {
	return &Service{Repo: NewMemoryRepository(), Auth: allowAuth{}, Clock: func() time.Time { return time.Unix(100, 0).UTC() }}
}

func TestTodo_CHAT_047(t *testing.T) {
	s := service()
	e, err := s.AppendRecord(context.Background(), "operator", Record{TenantID: "tenant-a", ConversationID: "c", RecordID: "p1", Kind: KindPost, Revision: 1}, "membership.add", "approved")
	if err != nil {
		t.Fatal(err)
	}
	if e.ActorID != "operator" || e.PriorRevision != 1 || e.PolicyEvidence == "" || e.Digest == "" {
		t.Fatalf("incomplete audit event: %+v", e)
	}
	if len(e.Reason) == 0 {
		t.Fatal("reason omitted")
	}
}
func TestTodo_CHAT_047_Security(t *testing.T) {
	s := &Service{Repo: NewMemoryRepository(), Auth: allowAuth{}}
	_, err := s.AppendRecord(context.Background(), "", Record{TenantID: "tenant-a", RecordID: "p", Kind: KindPost}, "edit", "why")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_CHAT_047_Integration(t *testing.T) {
	s := service()
	_, err := s.AppendRecord(context.Background(), "a", Record{TenantID: "tenant-a", RecordID: "p", Kind: KindPost, Revision: 1}, "share.grant", "reason")
	if err != nil {
		t.Fatal(err)
	}
	events, _ := s.Repo.Events(context.Background(), "tenant-a")
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("events=%+v", events)
	}
}

func TestTodo_CHAT_048(t *testing.T) {
	s := service()
	if err := s.PlaceHold(context.Background(), "legal", Hold{TenantID: "tenant-a", HoldID: "h1", MatterRef: "m", Reason: "litigation", PlacedBy: "legal"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.AppendRecord(context.Background(), "author", Record{TenantID: "tenant-a", RecordID: "p", Kind: KindPost, HoldIDs: []string{"h1"}, Revision: 1}, "post", "reason")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Dispose(context.Background(), "legal", Record{TenantID: "tenant-a", RecordID: "p", HoldIDs: []string{"h1"}, Revision: 2}, "delete"); !errors.Is(err, ErrHeld) {
		t.Fatalf("dispose err=%v", err)
	}
	ex, err := s.Export(context.Background(), "legal", "tenant-a", "x1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.RecordIDs) != 1 || ex.RecordIDs[0] != "p" {
		t.Fatalf("export=%+v", ex)
	}
}
func TestTodo_CHAT_048_Integration(t *testing.T) {
	s := service()
	for i, k := range []Kind{KindPost, KindEdit, KindTombstone, KindReaction, KindFile, KindVoice, KindVideo, KindAgentOutput, KindDerived} {
		_, err := s.AppendRecord(context.Background(), "a", Record{TenantID: "t", RecordID: string(rune('a' + i)), Kind: k, Revision: 1}, "record", "retained")
		if err != nil {
			t.Fatal(err)
		}
	}
	ex, err := s.Export(context.Background(), "a", "t", "x")
	if err != nil || len(ex.RecordIDs) != 9 {
		t.Fatalf("export=%+v err=%v", ex, err)
	}
}
func TestTodo_CHAT_048_Security(t *testing.T) {
	s := service()
	if err := s.Report(context.Background(), "actor", Report{TenantID: "t", ReportID: "r", ReporterID: "other", TargetID: "p", Reason: "abuse"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_CHAT_049(t *testing.T) {
	s := service()
	_, err := s.AppendRecord(context.Background(), "a", Record{TenantID: "t", RecordID: "p", Kind: KindPost, Revision: 1}, "post", "r")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(context.Background(), "a", "t")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Restore(context.Background(), "a", snap)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Ready || r.Records != 1 || r.Events != 1 || r.Outbox != 1 {
		t.Fatalf("reconcile=%+v", r)
	}
}
func TestTodo_CHAT_049_Recovery(t *testing.T) {
	s := service()
	_, _ = s.AppendRecord(context.Background(), "a", Record{TenantID: "t", RecordID: "p", Kind: KindPost, Revision: 1}, "post", "r")
	snap, _ := s.Snapshot(context.Background(), "a", "t")
	snap.Digest = "bad"
	if _, err := s.Restore(context.Background(), "a", snap); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_CHAT_049_Fault(t *testing.T) {
	r := NewMemoryRepository()
	r.events["t"] = append(r.events["t"], AuditEvent{EventID: "e", Sequence: 1})
	r.outbox["t"] = nil
	got, _ := r.Reconcile(context.Background(), "t")
	if got.Ready || got.Events != 1 {
		t.Fatalf("got=%+v", got)
	}
}

func TestTodo_CHAT_050(t *testing.T) {
	s := service()
	if err := s.Report(context.Background(), "a", Report{TenantID: "t", ReportID: "r", ReporterID: "a", TargetID: "p", Reason: "harassment"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Moderate(context.Background(), "moderator", "t", "case", "block", "p", "policy", "evidence://r"); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_CHAT_050_Security(t *testing.T) {
	s := service()
	if err := s.Moderate(context.Background(), "m", "t", "case", "inspect", "p", "reason", ""); !errors.Is(err, ErrPrivateEvidence) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_CHAT_050_Integration(t *testing.T) {
	s := service()
	if err := s.Moderate(context.Background(), "m", "t", "case", "remove", "p", "policy", "evidence://r"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repo.Events(context.Background(), "t"); err != nil {
		t.Fatal(err)
	}
}
