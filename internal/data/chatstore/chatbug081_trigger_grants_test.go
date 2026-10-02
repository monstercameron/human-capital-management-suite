package chatstore

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// chatbug081Write is one statement a trigger function runs against a table.
type chatbug081Write struct{ Verb, Table string }

// The three statements that need a privilege of their own. A table is named
// after INSERT INTO, DELETE FROM, or UPDATE ... SET; "ON CONFLICT DO UPDATE
// SET" names none and is covered by the INSERT it belongs to.
var (
	chatbug081Insert  = regexp.MustCompile(`(?i)\binsert\s+into\s+(?:only\s+)?"?([a-z_][a-z0-9_]*)"?`)
	chatbug081Delete  = regexp.MustCompile(`(?i)\bdelete\s+from\s+(?:only\s+)?"?([a-z_][a-z0-9_]*)"?`)
	chatbug081Update  = regexp.MustCompile(`(?i)\bupdate\s+(?:only\s+)?"?([a-z_][a-z0-9_]*)"?\s+(?:as\s+[a-z_][a-z0-9_]*\s+|[a-z_][a-z0-9_]*\s+)?set\b`)
	chatbug081Comment = regexp.MustCompile(`--[^\n]*`)
)

// chatbug081Writes lists the tables a function body writes to. It reads the
// body's static statements; a statement built at run time (EXECUTE) is not
// seen, and none of the chat triggers builds one.
func chatbug081Writes(body string) []chatbug081Write {
	body = chatbug081Comment.ReplaceAllString(body, "")
	var out []chatbug081Write
	for verb, pattern := range map[string]*regexp.Regexp{"INSERT": chatbug081Insert, "DELETE": chatbug081Delete, "UPDATE": chatbug081Update} {
		for _, match := range pattern.FindAllStringSubmatch(body, -1) {
			if table := strings.ToLower(match[1]); table != "set" {
				out = append(out, chatbug081Write{Verb: verb, Table: table})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Verb < out[j].Verb
	})
	return out
}

// chatbug081Refusals walks every trigger a role's own writes can fire and
// returns one line for each statement in a trigger function that the role has
// no privilege for. A trigger function runs as the caller unless it is
// SECURITY DEFINER, and a write it makes fires the triggers of the table it
// writes to, so the walk follows those as well.
func chatbug081Refusals(t *testing.T, db *pgtest.DB, role string) []string {
	t.Helper()
	ctx := context.Background()
	type trigger struct {
		Name, Function, Body string
		Events               int
		Definer              bool
	}
	triggers := map[string][]trigger{}
	rows, err := db.SQL.QueryContext(ctx, `SELECT c.relname,t.tgname,t.tgtype::int,p.proname,p.prosecdef,p.prosrc
 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_proc p ON p.oid=t.tgfoid
 WHERE NOT t.tgisinternal AND n.nspname=$1 ORDER BY c.relname,t.tgname`, db.Schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var item trigger
		if err := rows.Scan(&table, &item.Name, &item.Events, &item.Function, &item.Definer, &item.Body); err != nil {
			t.Fatal(err)
		}
		triggers[table] = append(triggers[table], item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// pg_trigger.tgtype: bit 2 INSERT, bit 3 DELETE, bit 4 UPDATE.
	eventBit := map[string]int{"INSERT": 1 << 2, "DELETE": 1 << 3, "UPDATE": 1 << 4}
	may := func(verb, table string) (exists, allowed bool) {
		qualified := db.Schema + "." + table
		if err := db.SQL.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, qualified).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			return false, false
		}
		if err := db.SQL.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,$3)`, role, qualified, verb).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		return true, allowed
	}
	var tables []string
	tableRows, err := db.SQL.QueryContext(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','p') ORDER BY c.relname`, db.Schema)
	if err != nil {
		t.Fatal(err)
	}
	defer tableRows.Close()
	for tableRows.Next() {
		var table string
		if err := tableRows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := tableRows.Err(); err != nil {
		t.Fatal(err)
	}
	var queue []chatbug081Write
	seen := map[chatbug081Write]bool{}
	push := func(write chatbug081Write) {
		if !seen[write] {
			seen[write] = true
			queue = append(queue, write)
		}
	}
	for _, table := range tables {
		for verb := range eventBit {
			if _, allowed := may(verb, table); allowed {
				push(chatbug081Write{Verb: verb, Table: table})
			}
		}
	}
	refused := map[string]bool{}
	for len(queue) > 0 {
		write := queue[0]
		queue = queue[1:]
		for _, item := range triggers[write.Table] {
			if item.Definer || item.Events&eventBit[write.Verb] == 0 {
				continue
			}
			for _, inner := range chatbug081Writes(item.Body) {
				exists, allowed := may(inner.Verb, inner.Table)
				if !exists {
					continue
				}
				if !allowed {
					refused[fmt.Sprintf("%s on %s fires %s (%s), which runs %s on %s", write.Verb, write.Table, item.Name, item.Function, inner.Verb, inner.Table)] = true
				}
				push(inner)
			}
		}
	}
	out := make([]string, 0, len(refused))
	for line := range refused {
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

// TestTodo_CHATBUG_081 builds the chat schema for a role that holds only the
// schema's default table privileges (SELECT, INSERT, UPDATE), the way the
// serving role of a cell with separated roles does, and fails when a write
// that role may make fires a trigger function that writes to a table the role
// may not write that way. The store tests run as the schema's owner and cannot
// see this; CHATBUG-070 was a delete that every one of them passed.
//
// It is run twice: stopped before migration 39 it must find the three refusals
// of CHATBUG-070, which is the proof that it can fail, and with every migration
// applied it must find none.
func TestTodo_CHATBUG_081(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chatbug081_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 38); err != nil {
		t.Fatal(err)
	}
	before := strings.Join(chatbug081Refusals(t, db, role), "\n")
	for _, table := range []string{"chatrender_rendering", "chatrender_job", "chatrender_report"} {
		if want := "UPDATE on chat_post fires chatrender_remove_views"; !strings.Contains(before, want) || !strings.Contains(before, "which runs DELETE on "+table) {
			t.Fatalf("before migration 39 the check does not find the refused DELETE on %s that CHATBUG-070 was:\n%s", table, before)
		}
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if refusals := chatbug081Refusals(t, db, role); len(refusals) != 0 {
		t.Fatalf("a trigger function writes to a table the serving role may not write; grant the privilege in the migration that adds the trigger or the table (see migrations 00028 and 00039):\n%s", strings.Join(refusals, "\n"))
	}

	t.Run("statements", func(t *testing.T) {
		body := `BEGIN
  -- DELETE FROM commented_out;
  INSERT INTO chat_outbox(id) VALUES (NEW.id) ON CONFLICT (id) DO UPDATE SET seen=true;
  UPDATE ONLY chat_conversation c SET revision=revision+1 WHERE c.id=NEW.conversation_id;
  DELETE FROM chatrender_job WHERE post_id=OLD.id;
  RETURN NEW; END`
		got := fmt.Sprint(chatbug081Writes(body))
		if want := "[{UPDATE chat_conversation} {INSERT chat_outbox} {DELETE chatrender_job}]"; got != want {
			t.Fatalf("statements read from a function body = %s, want %s", got, want)
		}
	})
}
