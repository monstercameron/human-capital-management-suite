package chatstore

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var chatscaleResumeSchema = flag.String("chatscale.resume-schema", "", "resume an orphaned, exclusively chatscale-owned pgtest corpus")

func chatscaleVolumeFixture(t *testing.T) (*Store, bool) {
	t.Helper()
	schema := *chatscaleResumeSchema
	if schema == "" {
		s, _ := chatFixture(t)
		return s, false
	}
	if len(schema) != 34 || !strings.HasPrefix(schema, "t_") {
		t.Fatal("resume requires a pgtest schema name")
	}
	for _, r := range schema[2:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			t.Fatal("invalid resume schema")
		}
	}
	u, err := url.Parse(os.Getenv("HCMNEXT_TEST_DATABASE_URL"))
	if err != nil || u.Host != "127.0.0.1:18540" {
		t.Fatal("resume is restricted to the shared test PostgreSQL on port 18540")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := New(context.Background(), Config{DSN: u.String(), CoreDSN: "postgres://core:pw@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctx := context.Background()
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		var count, foreign int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE id LIKE 'chatscale-p-%'),count(*) FILTER(WHERE tenant_id<>$1 OR
 (id NOT LIKE 'chatscale-p-%' AND NOT(author_id=$2 AND
 (client_key LIKE 'chatscale-mixed-indexed-%' OR client_key LIKE 'chatscale-budget-%')))) FROM chat_post`, chatscaleTenant, chatscaleReader).Scan(&count, &foreign); err != nil {
			return err
		}
		if count != chatscaleSize() || foreign != 0 {
			return fmt.Errorf("resume corpus posts=%d foreign=%d want %d exclusively owned posts", count, foreign, chatscaleSize())
		}
		var rooms, people int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM chat_conversation WHERE tenant_id=$1),(SELECT count(DISTINCT member_id) FROM chat_membership WHERE tenant_id=$1)`, chatscaleTenant).Scan(&rooms, &people); err != nil {
			return err
		}
		if (rooms != 200 && rooms != 300) || people != 2000 {
			return fmt.Errorf("resume corpus rooms=%d people=%d", rooms, people)
		}
		if err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM chat_conversation WHERE tenant_id<>$1)+
 (SELECT count(*) FROM chat_membership WHERE tenant_id<>$1)+
 (SELECT count(*) FROM chat_outbox WHERE tenant_id<>$1)`, chatscaleTenant).Scan(&foreign); err != nil {
			return err
		}
		if foreign != 0 {
			return fmt.Errorf("resume schema contains %d unowned rows", foreign)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The first attempt's interrupted measurements may have dropped indexes.
	// Restore the exact current definitions before taking a new baseline.
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		raw, err := Migrations.ReadFile("migrations/00034_chatscale_hot_reads.sql")
		if err != nil {
			return err
		}
		for _, part := range strings.Split(strings.Split(string(raw), "-- +goose Down")[0], ";") {
			start := strings.Index(part, "CREATE INDEX CONCURRENTLY ")
			if start < 0 {
				continue
			}
			sql := strings.TrimSpace(part[start:])
			name := strings.Fields(sql)[3]
			if _, err := tx.Exec(ctx, "DROP INDEX IF EXISTS "+name); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, strings.Replace(sql, "CONCURRENTLY ", "", 1)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `CREATE INDEX IF NOT EXISTS chatscale_receipt_parent ON chat_outbox_receipt(outbox_id)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE INDEX IF NOT EXISTS chatscale_post_visibility ON chat_post(tenant_id,conversation_id,id) WHERE NOT tombstoned`); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var malformed int
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND
 (payload->>'ConversationID' IS NULL OR payload->>'TargetID' IS NULL OR payload->'Value' IS NULL)`, chatscaleTenant).Scan(&malformed)
	}); err != nil {
		t.Fatal(err)
	}
	if malformed == 0 {
		t.Logf("resumed exclusively owned corpus %s with %d seeded posts", schema, chatscaleSize())
		return s, true
	}
	// An older generator used lower-case envelope keys. Recreate only this
	// isolated synthetic outbox, using the current generator's canonical keys.
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		// This is an exclusively owned synthetic schema, validated above. Avoid
		// millions of individual foreign-key cascade checks during fixture repair.
		if _, err := tx.Exec(ctx, `TRUNCATE chat_outbox_receipt,chat_outbox RESTART IDENTITY`); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chatscaleSeedOutbox(t, s, chatscaleSize())
	t.Logf("resumed exclusively owned corpus %s with %d posts", schema, chatscaleSize())
	return s, true
}
