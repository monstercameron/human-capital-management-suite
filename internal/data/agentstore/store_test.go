package agentstore

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func TestTodo_AGENT_008(t *testing.T) {
	t.Run("requires agent and core DSNs", func(t *testing.T) {
		_, err := New(context.Background(), Config{DSN: "postgres://agent:pw@127.0.0.1:5432/agent"})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("New error = %v, want ErrInvalidConfig", err)
		}
	})
	t.Run("rejects inconsistent pool bounds", func(t *testing.T) {
		_, err := withPoolSize("postgres://agent:pw@127.0.0.1:5432/agent", 2, 3)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("withPoolSize error = %v, want ErrInvalidConfig", err)
		}
	})
}

func TestTodo_AGENT_008_Security(t *testing.T) {
	core := "postgres://core:pw@db.example:5432/core"
	t.Run("rejects shared database despite distinct credentials", func(t *testing.T) {
		_, err := New(context.Background(), Config{
			DSN: "postgres://agent:other@db.example:5432/core", CoreDSN: core,
		})
		if !errors.Is(err, ErrSharedDatabase) {
			t.Fatalf("New error = %v, want ErrSharedDatabase", err)
		}
	})
	t.Run("rejects a shared login on the same server", func(t *testing.T) {
		_, err := New(context.Background(), Config{
			DSN:     "postgres://core:agent-password@localhost:5432/agents",
			CoreDSN: "postgres://core:core-password@127.0.0.1:5432/core",
		})
		if !errors.Is(err, ErrSharedCredential) {
			t.Fatalf("New error = %v, want ErrSharedCredential", err)
		}
	})
	if !sameDatabase("host=db.example port=5432 dbname=core user=agent", core) {
		t.Fatal("keyword and URL DSNs for one database were treated as isolated")
	}
}

func TestTodo_AGENT_008_PoolConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		max, min int32
		want     string
		wantErr  bool
	}{
		{name: "url adds bounds", dsn: "postgres://agent@db/agent", max: 4, min: 1, want: "postgres://agent@db/agent?pool_max_conns=4&pool_min_conns=1"},
		{name: "url enforces configured max", dsn: "postgres://agent@db/agent?sslmode=require&pool_max_conns=8", max: 4, min: 2, want: "postgres://agent@db/agent?pool_max_conns=4&pool_min_conns=2&sslmode=require"},
		{name: "keyword enforces configured max", dsn: "host=db dbname=agent pool_max_conns=100", max: 4, want: "host=db dbname=agent pool_max_conns=100 pool_max_conns=4"},
		{name: "keyword DSN adds bounds", dsn: "host=db dbname=agent", max: 4, min: 1, want: "host=db dbname=agent pool_max_conns=4 pool_min_conns=1"},
		{name: "no bounds leaves DSN intact", dsn: "host=db dbname=agent", want: "host=db dbname=agent"},
		{name: "min exceeds parsed max", dsn: "postgres://agent@db/agent?pool_max_conns=2", max: 0, min: 3, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withPoolSize(tc.dsn, tc.max, tc.min)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("withPoolSize error = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("withPoolSize = %q, %v; want %q", got, err, tc.want)
			}
			parsed, err := pgxpool.ParseConfig(got)
			if err != nil || (tc.max > 0 && parsed.MaxConns != tc.max) || (tc.min > 0 && parsed.MinConns != tc.min) {
				t.Fatalf("effective pool bounds were not enforced: config=%+v err=%v", parsed, err)
			}
		})
	}
}

func TestTodo_AGENT_008_UnavailableStore(t *testing.T) {
	var store *Store
	if _, err := store.Begin(context.Background()); err == nil {
		t.Fatal("nil store Begin succeeded")
	}
	if err := store.RunTx(context.Background(), func(dbport.Tx) error { return nil }); err == nil {
		t.Fatal("nil store RunTx succeeded")
	}
	if err := (&Store{}).RunTx(context.Background(), func(dbport.Tx) error { return nil }); err == nil {
		t.Fatal("uninitialized store RunTx succeeded")
	}
	if err := (&Store{}).RunTx(context.Background(), nil); err == nil {
		t.Fatal("nil callback RunTx succeeded")
	}
	if got := store.PoolStats(); got != (pgxadapter.Saturation{}) {
		t.Fatalf("nil store PoolStats = %+v", got)
	}
	store.Close()
}
