package chatstore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var chatTenantTables = []string{
	"chat_app_event",
	"chat_app_event_seen",
	"chat_app_installation",
	"chat_audit_event",
	"chat_channel_policy",
	"chat_channel_poll",
	"chat_channel_poll_revision",
	"chat_channel_poll_vote",
	"chat_channel_todo",
	"chat_channel_todo_revision",
	"chat_channel_widget",
	"chat_channel_widget_revision",
	"chat_conversation",
	"chat_conversation_idempotency",
	"chat_cursor",
	"chat_idempotency",
	"chat_membership",
	"chat_moderation_action",
	"chat_moderation_report",
	"chat_outbox",
	"chat_outbox_cursor",
	"chat_outbox_receipt",
	"chat_personal_sidebar",
	"chat_pin",
	"chat_post",
	"chat_post_revision",
	"chat_preference",
	"chat_quiet_hours",
	"chat_reaction",
	"chat_record_export",
	"chat_record_hold",
	"chat_record_inventory",
	"chat_retention_policy",
	"chat_share_grant",
	"chat_thread_follow",
}

// TestTodo_CHAT_004 proves every chat-owned table has forced
// row security and a tenant predicate for both reads and writes.
func TestTodo_CHAT_004(t *testing.T) {
	s, schema := chatFixture(t)
	want := make(map[string]bool, len(chatTenantTables))
	for _, table := range chatTenantTables {
		want[table] = true
	}
	got := make(map[string]bool, len(chatTenantTables))
	err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
COALESCE((SELECT string_agg(pg_get_expr(p.polqual,p.polrelid),' ') FROM pg_policy p WHERE p.polrelid=c.oid),''),
COALESCE((SELECT string_agg(pg_get_expr(p.polwithcheck,p.polrelid),' ') FROM pg_policy p WHERE p.polrelid=c.oid),'')
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=$1 AND c.relkind='r' AND left(c.relname,5)='chat_' ORDER BY c.relname`, schema)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table, using, check string
			var enabled, forced bool
			if err := rows.Scan(&table, &enabled, &forced, &using, &check); err != nil {
				return err
			}
			got[table] = true
			if !want[table] {
				return fmt.Errorf("unexpected chat-owned table %q; add it to the isolation contract", table)
			}
			if !enabled || !forced || !tenantPolicyExpr(using) || !tenantPolicyExpr(check) {
				return fmt.Errorf("%s RLS enabled=%t forced=%t using=%q check=%q", table, enabled, forced, using, check)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range chatTenantTables {
		if !got[table] {
			t.Errorf("chat-owned table %s is missing from the migrated schema", table)
		}
	}
}

func tenantPolicyExpr(expr string) bool {
	return strings.Contains(expr, "tenant_id") && strings.Contains(expr, "hcmnext.tenant_id")
}

// TestTodo_CHAT_004_Integration enters a role with no BYPASSRLS privilege.
// It proves that setting tenant-a cannot read tenant-b records and that the
// WITH CHECK policy rejects tenant-b writes for the core tenant-owned rows.
func TestTodo_CHAT_004_Integration(t *testing.T) {
	s, schema := chatFixture(t)
	role := createChatRLSRole(t, s, schema)
	ctx := context.Background()
	seedConversation(t, s, "tenant-a")
	seedConversationRow(t, s, Conversation{ID: "tenant-b-conversation", TenantID: "tenant-b", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-1", Role: "manager", State: "active"}})
	postA, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "tenant-a-post", Body: "tenant a"})
	if err != nil {
		t.Fatal(err)
	}
	postB, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-b", ConversationID: "tenant-b-conversation", AuthorID: "u-1", ClientKey: "tenant-b-post", Body: "tenant b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence) VALUES('tenant-b','tenant-b','u-1','tenant-b-conversation',1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES('tenant-b:conversation:app','tenant-b','tenant-b-conversation','app',1,'{}','{}','ACTIVE','u-1',1,now(),now())`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	err = runChatAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
		for _, table := range []string{"chat_conversation", "chat_post", "chat_post_revision", "chat_membership", "chat_cursor", "chat_app_installation"} {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id='tenant-b'`).Scan(&count); err != nil {
				return fmt.Errorf("read %s: %w", table, err)
			}
			if count != 0 {
				return fmt.Errorf("tenant-a read %d tenant-b rows from %s", count, table)
			}
		}
		var ownPostCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post WHERE tenant_id='tenant-a' AND id=$1`, postA.ID).Scan(&ownPostCount); err != nil {
			return err
		}
		if ownPostCount != 1 {
			return fmt.Errorf("tenant-a cannot read own post %s", postA.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	foreignWrites := []struct {
		table string
		sql   string
	}{
		{"chat_conversation", `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('forged','tenant-b','PRIVATE_CHANNEL','','attacker')`},
		{"chat_post", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) VALUES('forged-post','tenant-b','tenant-b-conversation','attacker',99,'forged')`},
		{"chat_post_revision", `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body) VALUES('tenant-b',$1,99,'attacker','forged')`},
		{"chat_membership", `INSERT INTO chat_membership(tenant_id,conversation_id,member_id) VALUES('tenant-b','tenant-b-conversation','attacker')`},
		{"chat_cursor", `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence) VALUES('tenant-b','tenant-b','attacker','tenant-b-conversation',99)`},
		{"chat_app_installation", `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES('forged-app','tenant-b','tenant-b-conversation','attacker',1,'{}','{}','ACTIVE','attacker',1,now(),now())`},
	}
	for i, write := range foreignWrites {
		t.Run(write.table, func(t *testing.T) {
			err := runChatAsTenantRole(ctx, s, role, "tenant-a", func(tx dbport.Tx) error {
				args := []any{}
				if write.table == "chat_post_revision" {
					args = append(args, postB.ID)
				}
				_, err := tx.Exec(ctx, write.sql, args...)
				return err
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
				t.Fatalf("foreign insert %d into %s error = %v; want row-level security rejection", i, write.table, err)
			}
		})
	}
}

func createChatRLSRole(t *testing.T, s *Store, schema string) string {
	t.Helper()
	ctx := context.Background()
	role := "chat_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedRole, quotedSchema := quoteChatIdentifier(role), quoteChatIdentifier(schema)
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+quotedRole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+quotedSchema+` TO `+quotedRole); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+quotedSchema+` TO `+quotedRole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+quotedSchema+` TO `+quotedRole)
		return err
	}); err != nil {
		t.Fatalf("create restricted chat role: %v", err)
	}
	t.Cleanup(func() {
		if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+quotedRole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+quotedRole)
			return err
		}); err != nil {
			t.Errorf("drop restricted chat role: %v", err)
		}
	})
	return role
}

func runChatAsTenantRole(ctx context.Context, s *Store, role, tenantID string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteChatIdentifier(role)); err != nil {
			return err
		}
		if err := tenant(ctx, tx, tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func quoteChatIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
