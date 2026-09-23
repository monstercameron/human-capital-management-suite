package chatrecordstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

func TestTodo_CHAT_048_Integration_RetentionPolicy(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, found, err := s.GetRetentionPolicy(ctx, "tenant-a", "")
	if err != nil || found {
		t.Fatalf("unset policy found=%v err=%v", found, err)
	}
	p := chatrecords.RetentionPolicy{TenantID: "tenant-a", Mode: chatrecords.RetentionBudget, BudgetBytes: 1024, Revision: 1, UpdatedBy: "admin", UpdatedAt: time.Unix(100, 0).UTC()}
	got, err := s.PutRetentionPolicy(ctx, p, 0)
	if err != nil || got.BudgetBytes != 1024 {
		t.Fatalf("put=%+v err=%v", got, err)
	}
	if _, err := s.PutRetentionPolicy(ctx, p, 0); !errors.Is(err, chatrecords.ErrConflict) {
		t.Fatalf("stale create err=%v", err)
	}
	p.Revision = 2
	p.BudgetBytes = 2048
	if _, err := s.PutRetentionPolicy(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	got, found, err = s.GetRetentionPolicy(ctx, "tenant-a", "")
	if err != nil || !found || got.Revision != 2 || got.BudgetBytes != 2048 {
		t.Fatalf("read=%+v found=%v err=%v", got, found, err)
	}
	_, found, err = s.GetRetentionPolicy(ctx, "tenant-b", "")
	if err != nil || found {
		t.Fatalf("cross tenant read found=%v err=%v", found, err)
	}
	_, found, err = s.GetRetentionPolicy(ctx, "tenant-a", "DIRECT")
	if err != nil || found {
		t.Fatalf("wrong kind read found=%v err=%v", found, err)
	}
	for _, scoped := range []chatrecords.RetentionPolicy{
		{TenantID: "tenant-a", Kind: "DIRECT", Mode: chatrecords.RetentionAge, AgeDays: 30, Revision: 1, UpdatedBy: "admin", UpdatedAt: time.Unix(100, 0).UTC()},
		{TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", Mode: chatrecords.RetentionDate, BeforeDate: time.Unix(50, 0).UTC(), Revision: 1, UpdatedBy: "admin", UpdatedAt: time.Unix(100, 0).UTC()},
	} {
		if _, err := s.PutRetentionPolicy(ctx, scoped, 0); err != nil {
			t.Fatalf("put scoped %+v: %v", scoped, err)
		}
		read, found, err := s.GetRetentionPolicy(ctx, "tenant-a", scoped.Kind)
		if err != nil || !found || read.Mode != scoped.Mode || read.AgeDays != scoped.AgeDays || !read.BeforeDate.Equal(scoped.BeforeDate) {
			t.Fatalf("read scoped=%+v found=%v err=%v", read, found, err)
		}
	}
}
