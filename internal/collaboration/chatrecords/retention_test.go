package chatrecords

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type retentionMemory struct {
	*MemoryRepository
	policy RetentionPolicy
	writes int
}

func (r *retentionMemory) GetRetentionPolicy(_ context.Context, tenant, kind string) (RetentionPolicy, bool, error) {
	if r.writes == 0 || r.policy.TenantID != tenant || r.policy.Kind != kind {
		return RetentionPolicy{}, false, nil
	}
	return r.policy, true, nil
}
func (r *retentionMemory) PutRetentionPolicy(_ context.Context, p RetentionPolicy, expected uint64) (RetentionPolicy, error) {
	if uint64(r.writes) != expected {
		return RetentionPolicy{}, ErrConflict
	}
	r.policy = p
	r.writes++
	return p, nil
}

type denyRetention struct{}

func (denyRetention) Authorize(context.Context, string, string, string, string) (string, error) {
	return "", ErrUnauthorized
}

func TestTodo_CHAT_048_RetentionConfiguration(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	repo := &retentionMemory{MemoryRepository: NewMemoryRepository()}
	s := &Service{Repo: repo, Auth: allowAuth{}, Clock: func() time.Time { return now }}
	_, found, err := s.GetRetentionPolicy(context.Background(), "admin", "tenant-a", "")
	if err != nil || found {
		t.Fatalf("unset retention found=%v err=%v", found, err)
	}
	p := RetentionPolicy{TenantID: "tenant-a", Mode: RetentionBudget, BudgetBytes: 1024}
	got, err := s.PutRetentionPolicy(context.Background(), "admin", p, 0)
	if err != nil || got.Revision != 1 || got.UpdatedBy != "admin" || repo.writes != 1 {
		t.Fatalf("configured=%+v writes=%d err=%v", got, repo.writes, err)
	}
	if _, err := s.PutRetentionPolicy(context.Background(), "admin", p, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, err := s.PutRetentionPolicy(context.Background(), "admin", RetentionPolicy{TenantID: "tenant-a", Mode: RetentionBudget}, 1); !errors.Is(err, ErrInvalid) || repo.writes != 1 {
		t.Fatalf("zero budget accepted: %v writes=%d", err, repo.writes)
	}
	s.Auth = denyRetention{}
	if _, err := s.PutRetentionPolicy(context.Background(), "admin", p, 1); !errors.Is(err, ErrUnauthorized) || repo.writes != 1 {
		t.Fatalf("denied write occurred: %v writes=%d", err, repo.writes)
	}
}

func TestTodo_CHAT_048_RetentionPolicy(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	items := []RetentionItem{
		{TenantID: "t", RecordID: "old", ConversationKind: "PUBLIC_CHANNEL", CreatedAt: now.AddDate(0, 0, -30), Bytes: 20, BytesKnown: true, Complete: true, HoldsKnown: true},
		{TenantID: "t", RecordID: "new", ConversationKind: "PUBLIC_CHANNEL", CreatedAt: now.AddDate(0, 0, -1), Bytes: 30, BytesKnown: true, Complete: true, HoldsKnown: true},
		{TenantID: "t", RecordID: "direct", ConversationKind: "DIRECT", CreatedAt: now.AddDate(0, 0, -30), Bytes: 10, BytesKnown: true, Complete: true, HoldsKnown: true},
	}
	for _, policy := range []RetentionPolicy{
		{TenantID: "t", Kind: "PUBLIC_CHANNEL", Mode: RetentionAge, AgeDays: 7, Revision: 1},
		{TenantID: "t", Kind: "PUBLIC_CHANNEL", Mode: RetentionDate, BeforeDate: now.AddDate(0, 0, -7), Revision: 1},
	} {
		preview, err := EvaluateRetention(policy, items, nil, now)
		if err != nil || len(preview.Candidates) != 1 || preview.Candidates[0].RecordID != "old" || len(preview.Blocked) != 0 {
			t.Fatalf("policy=%+v preview=%+v err=%v", policy, preview, err)
		}
	}
	budget := RetentionPolicy{TenantID: "t", Mode: RetentionBudget, BudgetBytes: 40, Revision: 1}
	preview, err := EvaluateRetention(budget, items, nil, now)
	if err != nil || len(preview.Candidates) != 2 || preview.Candidates[0].RecordID != "direct" || preview.Candidates[1].RecordID != "old" || preview.KnownBytes != 60 {
		t.Fatalf("budget preview=%+v err=%v", preview, err)
	}
}

func TestTodo_CHAT_048_Security_RetentionFailClosed(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	policy := RetentionPolicy{TenantID: "t", Mode: RetentionBudget, BudgetBytes: 1, Revision: 1}
	items := []RetentionItem{
		{TenantID: "t", RecordID: "held", CreatedAt: now.Add(-3 * time.Hour), Bytes: 20, BytesKnown: true, Complete: true, HoldsKnown: true, HoldIDs: []string{"h"}},
		{TenantID: "t", RecordID: "unknown-copy", CreatedAt: now.Add(-2 * time.Hour), Bytes: 20, BytesKnown: true, HoldsKnown: true},
		{TenantID: "t", RecordID: "unknown-size", CreatedAt: now.Add(-time.Hour), Complete: true, HoldsKnown: true},
	}
	preview, err := EvaluateRetention(policy, items, map[string]bool{"h": true}, now)
	if err != nil || len(preview.Candidates) != 0 || len(preview.Blocked) != 3 || preview.UnknownItems != 2 {
		t.Fatalf("unknown usage must block all: %+v err=%v", preview, err)
	}
	items = items[:1]
	for _, holds := range []map[string]bool{nil, {"h": true}} {
		preview, err = EvaluateRetention(policy, items, holds, now)
		if err != nil || len(preview.Candidates) != 0 || len(preview.Blocked) != 1 || preview.Blocked[0].Reason != "LEGAL_HOLD" {
			t.Fatalf("unresolved or active hold selected: %+v err=%v", preview, err)
		}
	}
	preview, err = EvaluateRetention(policy, items, map[string]bool{"h": false}, now)
	if err != nil || len(preview.Candidates) != 1 {
		t.Fatalf("released hold was not eligible: %+v err=%v", preview, err)
	}
	bad := policy
	bad.Kind = "DIRECT"
	if _, err := EvaluateRetention(bad, items, nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("kind-scoped disk budget accepted: %v", err)
	}
	bad = RetentionPolicy{TenantID: "t", Mode: RetentionAge, AgeDays: ^uint32(0), Revision: 1}
	if _, err := EvaluateRetention(bad, items, nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded age accepted: %v", err)
	}
	items = []RetentionItem{
		{TenantID: "t", RecordID: "one", CreatedAt: now.Add(-2 * time.Hour), Bytes: math.MaxInt64, BytesKnown: true, Complete: true, HoldsKnown: true},
		{TenantID: "t", RecordID: "two", CreatedAt: now.Add(-time.Hour), Bytes: 1, BytesKnown: true, Complete: true, HoldsKnown: true},
	}
	if _, err := EvaluateRetention(policy, items, nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("byte sum overflow accepted: %v", err)
	}
	items = items[:1]
	items[0].TenantID = "other"
	if _, err := EvaluateRetention(policy, items, nil, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-tenant inventory accepted: %v", err)
	}
}
