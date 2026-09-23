package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_HUB_001_DocumentMigrateCommand(t *testing.T) {
	command, rest := splitCommand([]string{"document", "up", "-document-database-url=postgres://u@db/docs"})
	if command != "document up" || len(rest) != 1 || rest[0] != "-document-database-url=postgres://u@db/docs" {
		t.Fatalf("splitCommand = (%q, %v)", command, rest)
	}
	if command, rest = splitCommand([]string{"document"}); command != documentCommandPrefix || rest != nil {
		t.Fatalf("bare document = (%q, %v)", command, rest)
	}
	if documentSubcommand("document status") != "status" || documentSubcommand("chat status") != "" {
		t.Fatal("document command namespace is ambiguous")
	}
	core := "postgres://core@db:5432/core"
	chat := "postgres://chat@db:5432/chat"
	if err := validateDocumentCommand("up", "", core, chat); err == nil {
		t.Fatal("missing document DSN accepted")
	}
	if err := validateDocumentCommand("up", "postgres://docs@db:5432/docs", "", chat); err == nil {
		t.Fatal("missing core DSN accepted")
	}
	if err := validateDocumentCommand("down", "postgres://docs@db/docs", core, chat); err == nil {
		t.Fatal("document down accepted")
	}
	if err := validateDocumentCommand("up", "host=db port=5432 dbname=core user=docs", core, chat); !errors.Is(err, ErrDocumentSharesDatabase) {
		t.Fatalf("shared core database accepted: %v", err)
	}
	if err := validateDocumentCommand("up", "postgres://docs@db:5432/chat", core, chat); !errors.Is(err, ErrDocumentSharesDatabase) {
		t.Fatalf("shared chat database accepted: %v", err)
	}
	if err := validateDocumentCommand("up", "postgres://docs@db:5432/docs", core, chat); err != nil {
		t.Fatalf("independent database refused: %v", err)
	}
	if _, err := openDocumentMigrateDB(context.Background(), "://invalid"); err == nil {
		t.Fatal("malformed document DSN opened")
	}
}

func TestTodo_HUB_001_DocumentMigrateCommand_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	var first bytes.Buffer
	if err := runDocumentMigrateCommand(ctx, "up", db.SQL, &first); err != nil {
		t.Fatalf("document up: %v", err)
	}
	if !strings.Contains(first.String(), "applied ") || !strings.Contains(first.String(), "document schema version") {
		t.Fatalf("document up output = %q", first.String())
	}
	var again bytes.Buffer
	if err := runDocumentMigrateCommand(ctx, "up", db.SQL, &again); err != nil {
		t.Fatalf("document up rerun: %v", err)
	}
	if strings.Contains(again.String(), "applied ") {
		t.Fatalf("document up re-applied migrations: %q", again.String())
	}
	var status bytes.Buffer
	if err := runDocumentMigrateCommand(ctx, "status", db.SQL, &status); err != nil {
		t.Fatalf("document status: %v", err)
	}
	if !strings.Contains(status.String(), "applied") || !strings.Contains(status.String(), "document schema version") {
		t.Fatalf("document status output = %q", status.String())
	}
	var tables int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('document','document_outbox','document_grant')`).Scan(&tables); err != nil || tables != 3 {
		t.Fatalf("document schema incomplete: tables=%d err=%v", tables, err)
	}
	if err := runDocumentMigrateCommand(ctx, "down", db.SQL, &status); err == nil {
		t.Fatal("document down accepted")
	}
}
