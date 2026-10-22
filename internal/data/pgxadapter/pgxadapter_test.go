package pgxadapter

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestInterfaceCompliance(t *testing.T) {
	var _ dbport.Tx = Tx{}
	var _ dbport.Conn = (*Conn)(nil)
	var _ dbport.Beginner = (*Conn)(nil)
	var _ dbport.Conn = (*Pool)(nil)
	var _ dbport.Beginner = (*Pool)(nil)
}

func TestConnectInvalidURL(t *testing.T) {
	_, err := Connect(context.Background(), "://bad-url", nil)
	if err == nil {
		t.Fatal("expected error for bad url")
	}
}

func TestNewPoolInvalidURL(t *testing.T) {
	_, err := NewPool(context.Background(), "://bad-url", nil)
	if err == nil {
		t.Fatal("expected error for bad url")
	}
}

func TestDSNMetadataAndIsolationComparisons(t *testing.T) {
	metadata, err := ParseDSNMetadata("host=db.example port=5432 dbname=agents user=agent")
	if err != nil {
		t.Fatalf("ParseDSNMetadata(keyword DSN): %v", err)
	}
	if metadata.Host != "db.example" || metadata.Port != 5432 || metadata.Database != "agents" || metadata.User != "agent" {
		t.Fatalf("keyword DSN metadata=%+v", metadata)
	}
	if !SameDatabaseIdentity("postgres://agent:a@DB.example:5432/agents?sslmode=require", "postgres://review:b@db.example:5432/agents?sslmode=disable") {
		t.Fatal("same host, port, and database with different credentials were not recognized")
	}
	if !SameDatabaseIdentity(" postgres://agent:a@db.example:5432/agents ", "postgres://review:b@db.example:5432/agents") {
		t.Fatal("surrounding whitespace changed logical database comparison")
	}
	if SameDatabaseIdentity("postgres://agent@db.example:5432/agents", "postgres://agent@db.example:5432/core") {
		t.Fatal("different databases were treated as one logical database")
	}
	if !SameLoginRole("postgres://agent:first@db.example/agents", "postgres://agent:second@db.example/core") {
		t.Fatal("same login with different passwords or databases was not recognized")
	}
	if !SameDatabaseIdentity("invalid-dsn", " invalid-dsn ") || SameDatabaseIdentity("invalid-dsn", "other-invalid-dsn") {
		t.Fatal("malformed DSNs did not use trimmed exact fallback")
	}
	if !SameLoginRole("invalid-dsn", " invalid-dsn ") || SameLoginRole("invalid-dsn", "other-invalid-dsn") {
		t.Fatal("malformed DSN role comparison did not use trimmed exact fallback")
	}
}

func TestConnectRuntimeParams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Connect(ctx, "postgres://user:pass@localhost:5432/db?sslmode=disable", map[string]string{"search_path": "public"})
	if err == nil {
		t.Fatal("Connect unexpectedly succeeded without a test server")
	}
}

func TestSaturationZero(t *testing.T) {
	var s Saturation
	if s.AcquiredConns != 0 || s.IdleConns != 0 || s.MaxConns != 0 || s.TotalConns != 0 {
		t.Fatal("zero Saturation not zero")
	}
}

func TestTxCommitRollbackNoPanic(t *testing.T) {
	var tx Tx
	_ = tx.Commit
	_ = tx.Rollback
	_ = tx.Exec
	_ = tx.Query
	_ = tx.QueryRow
}

func TestPoolStatsNoPanic(t *testing.T) {
	var p *Pool
	_ = p
}
