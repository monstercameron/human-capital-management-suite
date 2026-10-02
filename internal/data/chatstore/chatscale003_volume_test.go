package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// c001VerifyCounts proves, at the volume under measurement, that the first-open
// count (nothing cached, counted without reading bodies) and the cached read both
// equal the bounded history scan for every channel of the reader.
func c001VerifyCounts(t *testing.T, s *Store, channels int) {
	t.Helper()
	all := make([]string, channels)
	for i := range all {
		all[i] = c001Channel(i)
	}
	c001Run(t, s, `DELETE FROM chatscale_read_state WHERE tenant_id=$1`, chatscaleTenant)
	r := NewRecipientStateStore(s)
	start := time.Now()
	first, err := r.ChatscaleSidebarCounts(context.Background(), chatscaleTenant, chatscaleTenant, chatscaleReader, all)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("S22VERIFY first open of %d channels took %.1f ms wall", channels, float64(time.Since(start).Microseconds())/1000)
	want := chatscale003Oracle(t, s, all, countScanLimit)
	again, err := r.ChatscaleSidebarCounts(context.Background(), chatscaleTenant, chatscaleTenant, chatscaleReader, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, room := range all {
		if first[room] != want[room] || again[room] != want[room] {
			t.Fatalf("%s: first open %v, cached %v, history scan %v", room, first[room], again[room], want[room])
		}
	}
	if len(first) != len(want) {
		t.Fatalf("first open returned %d channels, scan %d", len(first), len(want))
	}
	t.Logf("S22VERIFY %d channels equal the bounded scan", len(want))
}

// c001ExplainCold prints the text plan of the first-open sidebar read when
// CHATSCALE003_EXPLAIN is set, inside a transaction that is rolled back.
func c001ExplainCold(t *testing.T, s *Store, channels int) {
	t.Helper()
	if c001Env("CHATSCALE003_EXPLAIN", "") == "" {
		return
	}
	all := make([]string, channels)
	for i := range all {
		all[i] = c001Channel(i)
	}
	ctx := context.Background()
	err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM chatscale_read_state WHERE tenant_id=$1`, chatscaleTenant); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) "+chatscaleCachedSQL, chatscaleTenant, chatscaleTenant, chatscaleReader, all, countScanLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			t.Logf("S22PLAN %s", line)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return c001Rollback
	})
	if err != nil && !errors.Is(err, c001Rollback) {
		t.Fatal(err)
	}
}

// c001ExplainTriggers lists, for the send statements, every trigger that ran with
// its time and call count (when CHATSCALE003_EXPLAIN is set), so a trigger's cost
// is attributed by name rather than as one sum.
func c001ExplainTriggers(t *testing.T, s *Store, channels int) {
	t.Helper()
	if c001Env("CHATSCALE003_EXPLAIN", "") == "" {
		return
	}
	ctx := context.Background()
	for _, stmt := range c001Statements(t, s, channels) {
		if stmt.Group != "send" {
			continue
		}
		var best map[string]float64
		var names []string
		for i := 0; i < 7; i++ {
			err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
				var raw string
				if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+stmt.SQL, stmt.Args...).Scan(&raw); err != nil {
					return err
				}
				var docs []map[string]any
				if err := json.Unmarshal([]byte(raw), &docs); err != nil {
					return err
				}
				times := map[string]float64{}
				var order []string
				list, _ := docs[0]["Triggers"].([]any)
				for _, item := range list {
					m, _ := item.(map[string]any)
					name := fmt.Sprintf("%s (%v calls)", c001Str(m, "Trigger Name"), m["Calls"])
					times[name] = c001Num(m, "Time")
					order = append(order, name)
				}
				if best == nil || len(order) > 0 && sumTimes(times) < sumTimes(best) {
					best, names = times, order
				}
				return c001Rollback
			})
			if err != nil && !errors.Is(err, c001Rollback) {
				t.Fatal(err)
			}
		}
		for _, name := range names {
			t.Logf("S22TRIGGER | %s | %s | %.3f ms", stmt.Name, name, best[name])
		}
	}
}

func sumTimes(m map[string]float64) float64 {
	var n float64
	for _, v := range m {
		n += v
	}
	return n
}
