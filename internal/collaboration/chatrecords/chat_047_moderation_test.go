package chatrecords

import (
	"context"
	"errors"
	"testing"
	"time"
)

type rejectModerationAuth struct{}

func (rejectModerationAuth) Authorize(context.Context, string, string, string, string) (string, error) {
	return "", ErrUnauthorized
}

func TestTodo_CHAT_047_ModerationAuditIsBodyFree(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	at := time.Unix(1700000000, 0).UTC()
	s := &Service{Repo: repo, Auth: allowAuth{}, Clock: func() time.Time { return at }}
	event, err := s.ModerateAudited(ctx, "moderator", "tenant-a", "conv-3", "case-7", "remove", "post-9", "policy violation", "evidence://case-7", 12)
	if err != nil {
		t.Fatal(err)
	}
	if event.ActorID != "moderator" || event.TargetID != "post-9" || event.PriorRevision != 12 || event.Reason != "policy violation" || event.PolicyEvidence != "policy:v1" || event.Digest == "" {
		t.Fatalf("audit event omitted required evidence: %+v", event)
	}
	events, err := repo.Events(ctx, "tenant-a")
	if err != nil || len(events) != 1 || events[0] != event {
		t.Fatalf("persisted audit events = %+v, %v", events, err)
	}
	reconcile, err := repo.Reconcile(ctx, "tenant-a")
	if err != nil || !reconcile.Ready || reconcile.Events != 1 || reconcile.Outbox != 1 {
		t.Fatalf("moderation projection not atomic: %+v, %v", reconcile, err)
	}
	if len(repo.actions["tenant-a"]) != 1 || repo.actions["tenant-a"][0].CaseID != "case-7" {
		t.Fatalf("moderation action missing: %+v", repo.actions["tenant-a"])
	}
}

func TestTodo_CHAT_047_Security_ModerationAuditFailsClosed(t *testing.T) {
	repo := NewMemoryRepository()
	s := &Service{Repo: repo, Auth: allowAuth{}}
	if _, err := s.ModerateAudited(context.Background(), "m", "t", "c", "case", "inspect", "post", "reason", "", 1); !errors.Is(err, ErrPrivateEvidence) {
		t.Fatalf("missing evidence ref = %v", err)
	}
	if _, err := s.ModerateAudited(context.Background(), "m", "t", "c", "case", "inspect", "post", "", "evidence://case", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing reason = %v", err)
	}
	if events, _ := repo.Events(context.Background(), "t"); len(events) != 0 || len(repo.actions["t"]) != 0 {
		t.Fatalf("invalid action changed state: events=%+v actions=%+v", events, repo.actions["t"])
	}
	s.Auth = rejectModerationAuth{}
	if _, err := s.ModerateAudited(context.Background(), "m", "t", "c", "case", "inspect", "post", "reason", "evidence://case", 1); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized action = %v", err)
	}
	if events, _ := repo.Events(context.Background(), "t"); len(events) != 0 || len(repo.actions["t"]) != 0 {
		t.Fatalf("unauthorized action changed state: events=%+v actions=%+v", events, repo.actions["t"])
	}
}

func TestTodo_CHAT_047_Golden_ModerationAuditDigest(t *testing.T) {
	event := AuditEvent{TenantID: "tenant-a", EventID: "moderation:case-7:1", Sequence: 1, ActorID: "moderator", Action: "moderation.remove", TargetType: "chat_post", TargetID: "post-9", PriorRevision: 12, Reason: "policy violation", PolicyEvidence: "policy:v1", At: time.Unix(1700000000, 0).UTC()}
	got := DigestEvent(event)
	const want = "sha256:6ba34d1600441f531c6cd7c11650e168ec060f9f3b458d9df518c0a886d628ce"
	if got != want {
		t.Fatalf("moderation audit digest = %q, want %q", got, want)
	}
}
