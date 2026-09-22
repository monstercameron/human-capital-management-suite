package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_CHAT_003_MigrateChatCommand pins the command surface: "chat" is a
// namespace whose action is the second token, an unknown action or a missing
// chat DSN fails before any connection is opened, and a chat DSN that resolves
// to the core database is refused outright.
func TestTodo_CHAT_003_MigrateChatCommand(t *testing.T) {
	command, rest := splitCommand([]string{"chat", "up", "-chat-database-url=postgres://x"})
	if command != "chat up" || len(rest) != 1 || rest[0] != "-chat-database-url=postgres://x" {
		t.Fatalf("splitCommand = (%q, %v)", command, rest)
	}
	if command, rest = splitCommand([]string{"chat"}); command != chatCommandPrefix || rest != nil {
		t.Fatalf("bare chat = (%q, %v)", command, rest)
	}
	if got := chatSubcommand("chat status"); got != "status" {
		t.Fatalf("chatSubcommand = %q", got)
	}
	if got := chatSubcommand("up"); got != "" {
		t.Fatalf("core command read as chat: %q", got)
	}
	if err := validateChatCommand("up", "", "postgres://core@db/core"); err == nil {
		t.Fatal("chat up accepted without a chat DSN")
	}
	if err := validateChatCommand("down", "postgres://chat@db/chat", ""); err == nil {
		t.Fatal("chat down accepted; only up and status exist")
	}
	if err := validateChatCommand("", "postgres://chat@db/chat", ""); err == nil {
		t.Fatal("bare chat accepted")
	}
	if err := validateChatCommand("up", "postgres://chat@db:5432/hcm?sslmode=disable", "postgres://core@db:5432/hcm"); !errors.Is(err, ErrChatSharesCoreDatabase) {
		t.Fatalf("shared database = %v, want ErrChatSharesCoreDatabase", err)
	}
	if err := validateChatCommand("up", "host=db port=5432 dbname=hcm user=chat", "postgres://core@db:5432/hcm"); !errors.Is(err, ErrChatSharesCoreDatabase) {
		t.Fatalf("shared database across DSN spellings = %v", err)
	}
	if err := validateChatCommand("up", "postgres://chat@db:5432/hcm_chat", "postgres://core@db:5432/hcm"); err != nil {
		t.Fatalf("independent chat database refused: %v", err)
	}
	if _, err := openChatMigrateDB(context.Background(), "://nonsense"); err == nil {
		t.Fatal("malformed chat DSN opened")
	}
}

// TestTodo_CHAT_003_MigrateChatCommand_Integration applies the chat migration
// set to a real, empty database and proves status reports it, a rerun is
// idempotent, and a chat table the application depends on exists afterwards.
func TestTodo_CHAT_003_MigrateChatCommand_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()

	var first bytes.Buffer
	if err := runChatMigrateCommand(ctx, "up", db.SQL, &first); err != nil {
		t.Fatalf("chat up: %v", err)
	}
	if !strings.Contains(first.String(), "applied ") || !strings.Contains(first.String(), "chat schema version") {
		t.Fatalf("chat up output = %q", first.String())
	}

	var again bytes.Buffer
	if err := runChatMigrateCommand(ctx, "up", db.SQL, &again); err != nil {
		t.Fatalf("chat up rerun: %v", err)
	}
	if strings.Contains(again.String(), "applied ") {
		t.Fatalf("chat up re-applied a migration: %q", again.String())
	}

	var status bytes.Buffer
	if err := runChatMigrateCommand(ctx, "status", db.SQL, &status); err != nil {
		t.Fatalf("chat status: %v", err)
	}
	if !strings.Contains(status.String(), "applied") || !strings.Contains(status.String(), "chat schema version") {
		t.Fatalf("chat status output = %q", status.String())
	}

	var conversations int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation`).Scan(&conversations); err != nil {
		t.Fatalf("chat_conversation after migrate: %v", err)
	}
	if conversations != 0 {
		t.Fatalf("chat_conversation rows = %d, want an empty applied schema", conversations)
	}

	if err := runChatMigrateCommand(ctx, "down", db.SQL, &status); err == nil {
		t.Fatal("chat down accepted; the chat set only supports up and status")
	}
}
