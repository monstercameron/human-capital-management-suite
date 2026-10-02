package chatstore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func TestTodo_CHATSCALE_001_Performance_Contention(t *testing.T) {
	s, schema := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id)
 SELECT $1,$2,$1,'chatscale-u-'||g FROM generate_series(1,1000) g ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleRoom); err != nil {
		t.Fatal(err)
	}
	conn, err := pgxadapter.Connect(ctx, os.Getenv("HCMNEXT_TEST_DATABASE_URL"), map[string]string{"search_path": schema})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	type result struct {
		seq int64
		ms  float64
		err error
	}
	results := make(chan result, 1000)
	start := make(chan struct{})
	for i := 1; i <= 1000; i++ {
		go func(i int) {
			<-start
			begin := time.Now()
			p, err := s.sendPostRaw(ctx, SendRequest{TenantID: chatscaleTenant, ConversationID: chatscaleRoom, AuthorID: fmt.Sprintf("chatscale-u-%d", i), ClientKey: fmt.Sprintf("chatscale-contended-%d", i), Body: "Concurrent payroll follow-up", References: []byte("[]")})
			results <- result{p.Sequence, float64(time.Since(begin).Microseconds()) / 1000, err}
		}(i)
	}
	begin := time.Now()
	close(start)
	// This independent connection excludes Go pool acquisition. Its SELECT
	// measures database round-trip plus waiting on the same conversation row
	// held by sequence allocation; the send samples include pool queues too.
	var lockSamples []float64
	for i := 0; i < 20; i++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := tenant(ctx, tx, chatscaleTenant); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		lockStart := time.Now()
		var seq int64
		err = tx.QueryRow(ctx, `SELECT post_sequence FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, chatscaleTenant, chatscaleRoom).Scan(&seq)
		lockSamples = append(lockSamples, float64(time.Since(lockStart).Microseconds())/1000)
		if err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var sendSamples []float64
	seen := map[int64]bool{}
	for i := 0; i < 1000; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("contended send: %v", r.err)
			continue
		}
		if seen[r.seq] {
			t.Errorf("duplicate sequence %d", r.seq)
		}
		seen[r.seq] = true
		sendSamples = append(sendSamples, r.ms)
	}
	if len(seen) != 1000 {
		t.Fatalf("successful unique sends=%d want 1000", len(seen))
	}
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		var head int64
		if err := tx.QueryRow(ctx, `SELECT post_sequence FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, chatscaleRoom).Scan(&head); err != nil {
			return err
		}
		if head != 1075 {
			return fmt.Errorf("head=%d want 1075", head)
		}
		for seq := int64(76); seq <= head; seq++ {
			if !seen[seq] {
				return fmt.Errorf("lost sequence %d", seq)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("CONTENTION senders=1000 elapsed=%.3fms sends/sec=%.1f send-with-pool p50=%.3fms p95=%.3fms p99=%.3fms", float64(time.Since(begin).Microseconds())/1000, 1000/time.Since(begin).Seconds(), chatscalePercentile(sendSamples, .5), chatscalePercentile(sendSamples, .95), chatscalePercentile(sendSamples, .99))
	t.Logf("CONVERSATION LOCK roundtrip-plus-wait p50=%.3fms p95=%.3fms p99=%.3fms samples=20", chatscalePercentile(lockSamples, .5), chatscalePercentile(lockSamples, .95), chatscalePercentile(lockSamples, .99))
}
