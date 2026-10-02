package chat

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// chatbug081PgError has the fields and the method of the database driver's
// error, which this package may not import.
type chatbug081PgError struct {
	Code, Message, Detail, Hint, Where     string
	SchemaName, TableName, ColumnName      string
	ConstraintName, InternalQuery, Routine string
}

func (e *chatbug081PgError) Error() string    { return e.Code + ": " + e.Message }
func (e *chatbug081PgError) SQLState() string { return e.Code }

// TestTodo_CHATBUG_081 holds the log line of a refused database call to what
// the database named and to nothing a row holds.
func TestTodo_CHATBUG_081(t *testing.T) {
	const secret = "walt.brennan@example.com"
	logged := func(err error) string {
		var out bytes.Buffer
		logDatabaseFailure(slog.New(slog.NewTextHandler(&out, nil)), err)
		return out.String()
	}

	t.Run("the delete refusal of CHATBUG-070", func(t *testing.T) {
		// What PostgreSQL answered when chatrender_remove_views deleted from a
		// table the serving role had no DELETE on, wrapped as the store wraps it.
		denied := &chatbug081PgError{
			Code: "42501", Message: "permission denied for table chatrender_rendering",
			Where:   "SQL statement \"DELETE FROM chatrender_rendering WHERE tenant_id=NEW.tenant_id AND post_id=NEW.id\"\nPL/pgSQL function chatrender_remove_views() line 4 at SQL statement",
			Routine: "aclcheck_error",
		}
		line := logged(fmt.Errorf("delete post: %w", denied))
		for _, want := range []string{"hcmnext.chat.database_failure", "sqlstate=42501", "table=chatrender_rendering", "function=chatrender_remove_views"} {
			if !strings.Contains(line, want) {
				t.Errorf("the log line lacks %q: %s", want, line)
			}
		}
		if strings.Contains(line, "DELETE FROM") || strings.Contains(line, "tenant_id") {
			t.Errorf("the log line carries statement text: %s", line)
		}
	})

	t.Run("a constraint is named and its row is not", func(t *testing.T) {
		duplicate := &chatbug081PgError{
			Code: "23503", Message: "insert or update on table \"chat_post\" violates foreign key constraint \"chat_post_conversation_fk\"",
			Detail:     "Key (conversation_id)=(" + secret + ") is not present in table \"chat_conversation\".",
			Hint:       "check " + secret,
			SchemaName: "public", TableName: "chat_post", ConstraintName: "chat_post_conversation_fk",
			InternalQuery: "INSERT INTO chat_post VALUES ('" + secret + "')",
		}
		line := logged(duplicate)
		for _, want := range []string{"sqlstate=23503", "schema=public", "table=chat_post", "constraint=chat_post_conversation_fk"} {
			if !strings.Contains(line, want) {
				t.Errorf("the log line lacks %q: %s", want, line)
			}
		}
		if strings.Contains(line, secret) || strings.Contains(line, "Key (") {
			t.Fatalf("the log line carries a row value: %s", line)
		}
	})

	t.Run("a value in a message or a name field is never logged", func(t *testing.T) {
		for name, err := range map[string]*chatbug081PgError{
			"a message with a value":              {Code: "22P02", Message: "invalid input syntax for type uuid: \"" + secret + "\""},
			"a privilege message with more text":  {Code: "42501", Message: "permission denied for table chat_post " + secret},
			"a value in a name field":             {Code: "23505", TableName: secret + " x", ConstraintName: "a b"},
			"a value in the context":              {Code: "22P02", Where: "COPY chat_post, line 1, column body: \"" + secret + "\""},
			"an object message for another state": {Code: "22P02", Message: "permission denied for table chat_post"},
		} {
			line := logged(err)
			if !strings.Contains(line, "sqlstate="+err.Code) {
				t.Errorf("%s: the SQLSTATE is not logged: %s", name, line)
			}
			if strings.Contains(line, secret) || strings.Contains(line, "table=") || strings.Contains(line, "constraint=") || strings.Contains(line, "function=") {
				t.Errorf("%s: the log line carries more than the SQLSTATE: %s", name, line)
			}
		}
	})

	t.Run("only a database error is logged, once, from the error mapping", func(t *testing.T) {
		if line := logged(errors.New("boom")); line != "" {
			t.Errorf("an error that is not the database's was logged: %s", line)
		}
		if line := logged(chatcore.ErrPermissionDenied); line != "" {
			t.Errorf("a refusal the service owns was logged as a database failure: %s", line)
		}
		var out bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
		defer slog.SetDefault(previous)
		denied := &chatbug081PgError{Code: "42501", Message: "permission denied for table chatrender_job"}
		if err := callErr(fmt.Errorf("delete post: %w", denied)); err == nil {
			t.Fatal("the mapped error is nil")
		}
		if got := strings.Count(out.String(), "hcmnext.chat.database_failure"); got != 1 || !strings.Contains(out.String(), "table=chatrender_job") {
			t.Errorf("mapping a database refusal wrote %d lines, want one naming the table: %s", got, out.String())
		}
		out.Reset()
		if err := callErr(chatcore.ErrConflict); err == nil || out.Len() != 0 {
			t.Errorf("mapping a conflict logged a database failure: %s", out.String())
		}
	})
}
