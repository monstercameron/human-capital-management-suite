package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATTONE_004_StoreSettings(t *testing.T) {
	s, schema := chatFixture(t)
	ctx := context.Background()
	styles := json.RawMessage(`[{"id":"professional","label":"Boardroom","instruction":"Write as if for a board paper.","register":"professional"}]`)
	if got, err := s.LoadWritingStyleSetting(ctx, "tenant-a"); err != nil || got.Found {
		t.Fatalf("a workspace with no row: %+v %v", got, err)
	}
	if rev, err := s.SaveWritingStyleSetting(ctx, "tenant-a", "admin-1", true, styles); err != nil || rev != 1 {
		t.Fatalf("first save: %d %v", rev, err)
	}
	got, err := s.LoadWritingStyleSetting(ctx, "tenant-a")
	var want, have []map[string]string
	_ = json.Unmarshal(styles, &want)
	_ = json.Unmarshal(got.Styles, &have)
	if err != nil || !got.Found || !got.Enabled || got.Revision != 1 || len(have) != 1 || have[0]["instruction"] != want[0]["instruction"] {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	// An administrator's "off" is a revision of the same row; the revision only moves forward.
	if rev, err := s.SaveWritingStyleSetting(ctx, "tenant-a", "admin-2", false, styles); err != nil || rev != 2 {
		t.Fatalf("second save: %d %v", rev, err)
	}
	if got, err := s.LoadWritingStyleSetting(ctx, "tenant-a"); err != nil || got.Enabled || got.Revision != 2 {
		t.Fatalf("off: %+v %v", got, err)
	}
	// Another workspace sees nothing of it, even by asking for the same key.
	if other, err := s.LoadWritingStyleSetting(ctx, "tenant-b"); err != nil || other.Found {
		t.Fatalf("a setting crossed workspaces: %+v %v", other, err)
	}
	if _, err := s.SaveWritingStyleSetting(ctx, "tenant-b", "admin-9", true, styles); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadWritingStyleSetting(ctx, "tenant-a"); got.Enabled {
		t.Fatal("another workspace's save changed this one")
	}
	// A row can be written only for the workspace the transaction is scoped to.
	role := createChatRLSRole(t, s, schema)
	err = runChatAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_workspace_setting (tenant_id, enabled, styles, updated_by) VALUES ('tenant-c', true, '[{}]', 'x')`)
		return err
	})
	if err == nil {
		t.Fatal("row level security allowed a write for another workspace")
	}
	for name, call := range map[string]func() error{
		"no tenant": func() error { _, err := s.SaveWritingStyleSetting(ctx, "", "a", true, styles); return err },
		"no writer": func() error { _, err := s.SaveWritingStyleSetting(ctx, "tenant-a", " ", true, styles); return err },
		"not json": func() error {
			_, err := s.SaveWritingStyleSetting(ctx, "tenant-a", "a", true, json.RawMessage(`{`))
			return err
		},
		"empty registry": func() error {
			_, err := s.SaveWritingStyleSetting(ctx, "tenant-a", "a", true, json.RawMessage(`[]`))
			return err
		},
		"not an array": func() error {
			_, err := s.SaveWritingStyleSetting(ctx, "tenant-a", "a", true, json.RawMessage(`{}`))
			return err
		},
		"load no tenant": func() error { _, err := s.LoadWritingStyleSetting(ctx, ""); return err },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	// A setting is not deleted by the application's role.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chattone_workspace_setting WHERE tenant_id='tenant-a'`)
		return err
	}); err != nil {
		t.Fatalf("a workspace's own setting cannot be removed by its own erasure: %v", err)
	}
}

func TestTodo_CHATTONE_004_StoreUsage(t *testing.T) {
	s, schema := chatFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := s.ReserveWritingStyleUse(ctx, "tenant-a", "alice", day, 3); err != nil {
			t.Fatalf("place %d: %v", i, err)
		}
	}
	if err := s.ReserveWritingStyleUse(ctx, "tenant-a", "alice", day.Add(time.Hour), 3); !errors.Is(err, ErrWritingStyleLimit) {
		t.Fatalf("fourth place: %v", err)
	}
	// The ceiling is per person, per workspace and per day.
	for name, call := range map[string]func() error{
		"another person":    func() error { return s.ReserveWritingStyleUse(ctx, "tenant-a", "bob", day, 3) },
		"another workspace": func() error { return s.ReserveWritingStyleUse(ctx, "tenant-b", "alice", day, 3) },
		"the next day":      func() error { return s.ReserveWritingStyleUse(ctx, "tenant-a", "alice", day.Add(24*time.Hour), 3) },
	} {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n, err := s.WritingStyleUsageToday(ctx, "tenant-a", "alice", day); err != nil || n != 3 {
		t.Fatalf("usage today %d %v", n, err)
	}
	if err := s.ReserveWritingStyleUse(ctx, "tenant-a", "alice", day, 0); !errors.Is(err, ErrWritingStyleLimit) {
		t.Fatalf("a zero limit admitted a call: %v", err)
	}

	// Concurrent presses cannot both take the last place.
	var granted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.ReserveWritingStyleUse(ctx, "tenant-a", "carol", day, 5); err == nil {
				granted.Add(1)
			} else if !errors.Is(err, ErrWritingStyleLimit) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if granted.Load() != 5 {
		t.Fatalf("a limit of 5 admitted %d concurrent calls", granted.Load())
	}

	if err := s.RecordWritingStyleUse(ctx, "tenant-a", "alice", day, "rewrite", 1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWritingStyleUse(ctx, "tenant-a", "alice", day, "meaning", 2, false); err != nil {
		t.Fatal(err)
	}
	// Outcome lines are not places.
	if n, _ := s.WritingStyleUsageToday(ctx, "tenant-a", "alice", day); n != 3 {
		t.Fatalf("an outcome line was counted as a place: %d", n)
	}
	for name, call := range map[string]func() error{
		"reserve is not an outcome": func() error { return s.RecordWritingStyleUse(ctx, "tenant-a", "alice", day, "reserve", 0, true) },
		"unknown operation":         func() error { return s.RecordWritingStyleUse(ctx, "tenant-a", "alice", day, "send", 0, true) },
		"attempt":                   func() error { return s.RecordWritingStyleUse(ctx, "tenant-a", "alice", day, "rewrite", 3, true) },
		"no person":                 func() error { return s.RecordWritingStyleUse(ctx, "tenant-a", "", day, "rewrite", 1, true) },
	} {
		if err := call(); !errors.Is(err, ErrWritingStyleInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// A usage line is never changed or removed.
	for _, statement := range []string{`UPDATE chattone_usage SET succeeded=NOT succeeded`, `DELETE FROM chattone_usage`} {
		err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, statement)
			return err
		})
		if err == nil {
			t.Fatalf("%q was allowed", statement)
		}
	}
	// And a workspace sees only its own lines.
	var lines int
	role := createChatRLSRole(t, s, schema)
	if err := runChatAsTenantRole(ctx, s, role, "tenant-b", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chattone_usage`).Scan(&lines)
	}); err != nil || lines != 1 {
		t.Fatalf("tenant-b sees %d lines (%v)", lines, err)
	}
}
