package agentstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestAgentUXProactive_Store_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("announcement"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	repository, err := NewAnnouncementStore(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	next := now.Add(4 * 24 * time.Hour)
	record := Announcement{TenantID: tenant, TenantKey: "tenant-a", ID: "holidays", InstallationID: "install-policy", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which company holidays are coming up.", Documents: []agentdocref.Reference{{DocumentID: "holiday-guide", VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}}, Cadence: "WEEKLY", Weekdays: []int16{1}, LocalTime: time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC), Zone: "America/New_York", State: AnnouncementActive, OwnerID: "owner", SchedulerID: "announcement:holidays", Revision: 1, NextRunAt: &next, CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, record); !errors.Is(err, ErrAnnouncementRevision) {
		t.Fatalf("duplicate create error = %v", err)
	}
	rows, err := repository.ListOwner(ctx, tenant, "owner")
	if err != nil || len(rows) != 1 || rows[0].Instruction != record.Instruction || rows[0].Documents[0].DocumentID != "holiday-guide" {
		t.Fatalf("owner announcements = %+v, %v", rows, err)
	}
	if otherRows, err := repository.ListOwner(ctx, other, "owner"); err != nil || len(otherRows) != 0 {
		t.Fatalf("other tenant rows = %+v, %v", otherRows, err)
	}
	record.State, record.Revision, record.UpdatedAt = AnnouncementPaused, 2, now.Add(time.Minute)
	record.NextRunAt = nil
	if err := repository.Save(ctx, record, 1); err != nil {
		t.Fatal(err)
	}
	occurrence := AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: record.ID, OccurrenceID: "occurrence-monday", Result: AnnouncementPosted, MessageID: "message-1", AttemptedAt: now.Add(time.Hour)}
	inserted, err := repository.RecordOccurrence(ctx, occurrence)
	if err != nil || !inserted {
		t.Fatalf("record occurrence = %t, %v", inserted, err)
	}
	inserted, err = repository.RecordOccurrence(ctx, occurrence)
	if err != nil || inserted {
		t.Fatalf("replay occurrence = %t, %v", inserted, err)
	}
	conflict := occurrence
	conflict.MessageID = "message-2"
	if _, err := repository.RecordOccurrence(ctx, conflict); !errors.Is(err, ErrAnnouncementRevision) {
		t.Fatalf("conflicting replay = %v", err)
	}
	got, err := repository.Get(ctx, tenant, record.ID)
	if err != nil || got.LastResult != AnnouncementPosted || got.LastMessageID != "message-1" || got.LastOccurrence != occurrence.OccurrenceID {
		t.Fatalf("stored result = %+v, %v", got, err)
	}
	if prior, found, err := repository.GetOccurrence(ctx, tenant, record.ID, occurrence.OccurrenceID); err != nil || !found || prior.MessageID != "message-1" {
		t.Fatalf("durable occurrence replay: %+v %t %v", prior, found, err)
	}
	if _, found, err := repository.GetOccurrence(ctx, other, record.ID, occurrence.OccurrenceID); err != nil || found {
		t.Fatalf("cross-tenant occurrence: %t %v", found, err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	if seen, err := repository.CheckCommand(ctx, tenant, "owner", "request-123", record.ID, digest); err != nil || seen {
		t.Fatalf("new command: %t %v", seen, err)
	}
	if err := repository.RecordCommand(ctx, tenant, "owner", "request-123", record.ID, digest); err != nil {
		t.Fatal(err)
	}
	if seen, err := repository.CheckCommand(ctx, tenant, "owner", "request-123", record.ID, digest); err != nil || !seen {
		t.Fatalf("command replay: %t %v", seen, err)
	}
	if _, err := repository.CheckCommand(ctx, tenant, "owner", "request-123", record.ID, "sha256:"+strings.Repeat("b", 64)); !errors.Is(err, ErrAnnouncementRevision) {
		t.Fatalf("changed replay: %v", err)
	}
	if seen, err := repository.CheckCommand(ctx, other, "owner", "request-123", record.ID, digest); err != nil || seen {
		t.Fatalf("cross-tenant command: %t %v", seen, err)
	}
	if err := repository.Save(ctx, record, 1); !errors.Is(err, ErrAnnouncementRevision) {
		t.Fatalf("stale revision: %v", err)
	}
	for _, field := range []string{"owner", "tenant", "schedule"} {
		changed := record
		changed.Revision = 3
		switch field {
		case "owner":
			changed.OwnerID = "other"
		case "tenant":
			changed.TenantKey = "other"
		case "schedule":
			changed.SchedulerID = "other"
		}
		if err := repository.Save(ctx, changed, 2); !errors.Is(err, ErrAnnouncementRevision) {
			t.Fatalf("immutable %s changed: %v", field, err)
		}
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 2)
	counter := 0
	for range 2 {
		wg.Go(func() {
			errorsCh <- repository.WithAnnouncementCommandFence(ctx, tenant, record.ID, "owner", "request-456", func() error {
				counter++
				_, err := repository.Get(ctx, tenant, record.ID)
				return err
			})
		})
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if counter != 2 {
		t.Fatalf("fence dropped a command: %d", counter)
	}
	for i, result := range []string{AnnouncementRefused, AnnouncementFailed} {
		reason := []string{"1 of the documents cannot be read by everyone in #general", "The agent could not write this announcement."}[i]
		if _, err := repository.RecordOccurrence(ctx, AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: record.ID, OccurrenceID: result, Result: result, Reason: reason, AttemptedAt: now.Add(time.Duration(i+2) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		rows, err := repository.ListOwner(ctx, tenant, "owner")
		if err != nil || len(rows) != 1 || rows[0].LastResult != result || rows[0].LastReason != reason || rows[0].LastMessageID != "" {
			t.Fatalf("%s projection leaked an old success: %+v %v", result, rows, err)
		}
	}
	if err := repository.WithAnnouncementFence(ctx, tenant, record.ID, func() error { return ErrAnnouncementRevision }); !errors.Is(err, ErrAnnouncementRevision) {
		t.Fatalf("fence callback error lost: %v", err)
	}
}
