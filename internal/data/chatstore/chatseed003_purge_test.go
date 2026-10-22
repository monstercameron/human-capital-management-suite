package chatstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type catalogueForeignKey struct{ child, parent, onDelete string }

// readChatCatalogue lists the base tables of the migrated schema and every
// foreign key between them.
func readChatCatalogue(t *testing.T, s *Store) (tables []string, keys []catalogueForeignKey) {
	t.Helper()
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "catalogue", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname=current_schema() AND c.relkind IN ('r','p') AND NOT c.relispartition ORDER BY 1`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		fks, err := tx.Query(ctx, `SELECT conrelid::regclass::text, confrelid::regclass::text, confdeltype::text FROM pg_constraint
			WHERE contype='f' AND connamespace=(SELECT oid FROM pg_namespace WHERE nspname=current_schema()) ORDER BY 1,2`)
		if err != nil {
			return err
		}
		defer fks.Close()
		for fks.Next() {
			var key catalogueForeignKey
			if err := fks.Scan(&key.child, &key.parent, &key.onDelete); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return fks.Err()
	}); err != nil {
		t.Fatalf("read catalogue: %v", err)
	}
	return tables, keys
}

// TestTodo_CHATSEED_003_PurgeCoversEveryChatTable walks the schema a real
// migrated database has. A table that is neither purged nor named as cascaded,
// append-only or kept fails it, and so does a foreign key that would make a
// conversation delete fail, which is how a reset broke when migrations 17 to 48
// added tables the purge did not know.
func TestTodo_CHATSEED_003_PurgeCoversEveryChatTable(t *testing.T) {
	s, _ := chatFixture(t)
	tables, keys := readChatCatalogue(t, s)

	purgeIndex := map[string]int{}
	for i, spec := range seedPurgeOrder {
		if _, dup := purgeIndex[spec.table]; dup {
			t.Errorf("%s is purged twice", spec.table)
		}
		purgeIndex[spec.table] = i
	}
	where := map[string]string{}
	claim := func(table, list string) {
		if previous, taken := where[table]; taken {
			t.Errorf("%s is in both %s and %s", table, previous, list)
		}
		where[table] = list
	}
	for table := range purgeIndex {
		claim(table, "seedPurgeOrder")
	}
	for _, table := range seedPurgeCascade {
		claim(table, "seedPurgeCascade")
	}
	for _, table := range seedPurgeImmutable {
		claim(table, "seedPurgeImmutable")
	}
	for table := range seedPurgeKept {
		claim(table, "seedPurgeKept")
	}

	present := map[string]bool{}
	var missing []string
	for _, table := range tables {
		present[table] = true
		if _, ok := where[table]; !ok {
			missing = append(missing, table)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tables neither purged nor listed as cascaded, append-only or kept (add each to seed_support.go): %s", strings.Join(missing, ", "))
	}
	var stale []string
	for table := range where {
		if !present[table] {
			stale = append(stale, table)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("seed_support.go names tables the schema does not have: %s", strings.Join(stale, ", "))
	}

	// A table that holds a foreign key into a room or post, directly or through a
	// parent that is itself removed, has to go before its parent, cascade with
	// it, or be named as append-only so the purge can refuse up front.
	removed := func(table string) bool {
		list := where[table]
		return list == "seedPurgeOrder" || list == "seedPurgeCascade"
	}
	for _, key := range keys {
		if !removed(key.parent) || key.child == key.parent {
			continue
		}
		switch where[key.child] {
		case "seedPurgeOrder":
			if key.onDelete != "c" && purgeIndex[key.child] > purgeIndex[key.parent] {
				t.Errorf("%s is purged after %s, which it references without a cascade", key.child, key.parent)
			}
		case "seedPurgeCascade":
			if key.onDelete != "c" {
				t.Errorf("%s is listed as cascaded but its foreign key to %s is not ON DELETE CASCADE", key.child, key.parent)
			}
		case "seedPurgeImmutable":
			if key.onDelete == "c" {
				t.Errorf("%s is listed as blocking but cascades from %s", key.child, key.parent)
			}
		default:
			t.Errorf("%s is kept, yet references %s: deleting the parent fails or silently takes it too", key.child, key.parent)
		}
	}
	for _, table := range seedPurgeImmutable {
		if !strings.Contains(seedImmutableHistoryQuery, "'"+table+"' ") && !strings.Contains(seedImmutableHistoryQuery, "FROM "+table+" ") {
			t.Errorf("seedImmutableHistoryQuery does not check %s", table)
		}
		referencesRoom := false
		for _, key := range keys {
			if key.child == table && key.parent == "chat_conversation" && key.onDelete != "c" {
				referencesRoom = true
			}
		}
		if !referencesRoom {
			t.Errorf("%s is listed as blocking but has no non-cascading foreign key to chat_conversation", table)
		}
	}
}

// TestTodo_CHATSEED_003_PurgeRemovesEveryDependentRow puts a row in each table
// that points at a room without a cascade and checks the purge empties them,
// leaves another room alone and refuses a room that holds append-only history.
func TestTodo_CHATSEED_003_PurgeRemovesEveryDependentRow(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	const tenant = "tenant-a"
	seedConversationRow(t, s, Conversation{ID: "reset-room", TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	seedConversationRow(t, s, Conversation{ID: "other-room", TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	seedConversationRow(t, s, Conversation{ID: "gated-room", TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)

	count := func(table, room string) int {
		t.Helper()
		var n int
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id=$1 AND conversation_id=$2`, table), tenant, room).Scan(&n)
		}); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	exec := func(statement string, args ...any) {
		t.Helper()
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, statement, args...)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	for _, room := range []string{"reset-room", "other-room"} {
		exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,sequence,body,author_id,author_home_tenant_id,created_at) VALUES($1,$2,$3,1,'hello','alice',$2,now())`, "post-"+room, tenant, room)
		exec(`INSERT INTO chat_persona_source_classification(tenant_id,conversation_id,post_id,body_digest,data_class) VALUES($1,$2,$3,$4,'INTERNAL')`, tenant, room, "post-"+room, "sha256:"+strings.Repeat("b", 64))
		exec(`INSERT INTO chat_persona_channel_policy(tenant_id,conversation_id,revision,policy_json) VALUES($1,$2,1,'{}')`, tenant, room)
		exec(`INSERT INTO chat_public_audience_policy(tenant_id,conversation_id,revision,classification) VALUES($1,$2,1,'PUBLIC')`, tenant, room)
		exec(`INSERT INTO chat_public_audience_principal(tenant_id,conversation_id,home_tenant_id,subject_id) VALUES($1,$2,$1,'alice')`, tenant, room)
		exec(`INSERT INTO chat_gate_state(tenant_id,conversation_id,revision,state_json) VALUES($1,$2,1,'{}')`, tenant, room)
	}
	dependents := []string{"chat_post", "chat_persona_source_classification", "chat_persona_channel_policy", "chat_public_audience_policy", "chat_public_audience_principal", "chat_gate_state"}
	for _, table := range dependents {
		if count(table, "reset-room") != 1 {
			t.Fatalf("fixture row missing in %s", table)
		}
	}

	if err := s.PurgeConversations(ctx, tenant, []string{"reset-room"}); err != nil {
		t.Fatalf("purge a room with dependent rows: %v", err)
	}
	for _, table := range dependents {
		if n := count(table, "reset-room"); n != 0 {
			t.Errorf("%s still holds %d rows for the purged room", table, n)
		}
		if n := count(table, "other-room"); n != 1 {
			t.Errorf("%s holds %d rows for the room that was not purged, want 1", table, n)
		}
	}

	exec(`INSERT INTO chat_gate_version(tenant_id,conversation_id,semantic_version,digest,definition_json) VALUES($1,$2,'1.0.0',$3,'{}')`, tenant, "gated-room", "sha256:"+strings.Repeat("a", 64))
	err := s.PurgeConversations(ctx, tenant, []string{"gated-room"})
	if !errors.Is(err, ErrSeedPurgeBlocked) || !strings.Contains(err.Error(), "chat_gate_version") {
		t.Fatalf("purge of a room with gate history = %v, want ErrSeedPurgeBlocked naming chat_gate_version", err)
	}
	if exists, existsErr := s.ConversationExists(ctx, tenant, "gated-room"); existsErr != nil || !exists {
		t.Fatalf("a refused purge removed the room: exists=%t err=%v", exists, existsErr)
	}
}
