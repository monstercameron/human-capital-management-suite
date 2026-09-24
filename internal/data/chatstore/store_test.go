package chatstore

import "testing"

func TestDatabaseIsolationComparesLogicalDatabase(t *testing.T) {
	if !sameDatabase("postgres://chat:pw@db.example/chat", "postgres://core:pw@db.example/chat") {
		t.Fatal("same logical database with different credentials was accepted")
	}
	if sameDatabase("postgres://chat:pw@db.example/chat", "postgres://core:pw@db.example/core") {
		t.Fatal("different database names were treated as shared")
	}
}

// TestDatabaseIsolationComparesKeywordDSNs covers the libpq keyword spelling.
// The comparison used to parse only URLs, so a keyword DSN never matched
// anything and a composition that spelled the two DSNs differently could point
// chat at the core database.
func TestDatabaseIsolationComparesKeywordDSNs(t *testing.T) {
	if !sameDatabase("host=db.example port=5432 dbname=chat user=chat password=pw", "postgres://core:pw@db.example:5432/chat") {
		t.Fatal("keyword and URL spellings of one database were treated as separate")
	}
	if sameDatabase("host=db.example port=5432 dbname=chat user=chat", "host=db.example port=5432 dbname=core user=core") {
		t.Fatal("different keyword databases were treated as shared")
	}
	if sameDatabase("host=db.example port=5432 dbname=chat", "host=other.example port=5432 dbname=chat") {
		t.Fatal("different hosts were treated as shared")
	}
	if sameDatabase("postgres://chat:pw@db.example:5432/chat", "postgres://chat:pw@db.example:6432/chat") {
		t.Fatal("different ports were treated as shared")
	}
}

// TestTodo_CHAT_003 proves the storage composition rejects reuse of the core
// login role even when chat has its own database. The guard must run before
// network access so an unsafe deployment configuration fails closed.
func TestTodo_CHAT_003(t *testing.T) {
	_, err := New(t.Context(), Config{
		DSN:     "postgres://shared:pw@127.0.0.1:1/chat",
		CoreDSN: "postgres://shared:pw@127.0.0.1:1/workflow",
	})
	if err != ErrCoreCredential {
		t.Fatalf("New with separate database but shared role = %v, want %v", err, ErrCoreCredential)
	}
	if sameDatabaseCredential("postgres://chat_role:pw@db.example/chat", "postgres://workflow_role:pw@db.example/workflow") {
		t.Fatal("distinct database roles were treated as one credential")
	}
	if sameDatabaseCredential("postgres://shared:pw@chat-db.example/chat", "postgres://shared:pw@core-db.example/workflow") {
		t.Fatal("same role label on independent database endpoints was treated as shared credential")
	}
	_, err = New(t.Context(), Config{
		DSN:     "postgres://shared:pw@chat-db.example:1/chat",
		CoreDSN: "postgres://shared:pw@core-db.example:1/workflow",
	})
	if err == ErrCoreCredential {
		t.Fatal("New rejected the same role label on independent endpoints as a shared credential")
	}
}

// TestTodo_CHAT_003_Security checks that varying only the secret does not
// disguise reuse of the same PostgreSQL authorization role.
func TestTodo_CHAT_003_Security(t *testing.T) {
	if !sameDatabaseCredential("postgres://chat_role:first@db.example/chat", "postgres://chat_role:second@db.example/workflow") {
		t.Fatal("same database role with rotated passwords was treated as distinct")
	}
	if _, err := New(t.Context(), Config{
		DSN:     "postgres://chat_role:chat@127.0.0.1:1/chat",
		CoreDSN: "postgres://chat_role:core@127.0.0.1:1/workflow",
	}); err != ErrCoreCredential {
		t.Fatalf("New with a shared role and different passwords = %v, want %v", err, ErrCoreCredential)
	}
}

func TestTodo_CHAT_003_RejectsLoopbackHostAliases(t *testing.T) {
	const role = "postgres://shared:pw@"
	aliases := [][2]string{
		{role + "localhost:5432/chat", role + "127.0.0.1:5432/workflow"},
		{role + "localhost:5432/chat", role + "[::1]:5432/workflow"},
		{"host=localhost port=5432 dbname=chat user=shared", role + "127.0.0.1:5432/workflow"},
	}
	for _, pair := range aliases {
		if !sameDatabaseCredential(pair[0], pair[1]) {
			t.Errorf("loopback alias pair was treated as separate credentials:\n  %s\n  %s", pair[0], pair[1])
		}
	}
	if sameDatabaseCredential(role+"chat-db.example/chat", role+"core-db.example/workflow") {
		t.Fatal("distinct named endpoints with the same role were treated as shared")
	}
}

func TestFingerprintStableAndContentBound(t *testing.T) {
	if fingerprint("hello") != fingerprint("hello") {
		t.Fatal("fingerprint is not stable")
	}
	if fingerprint("hello") == fingerprint("hello ") {
		t.Fatal("fingerprint ignored content change")
	}
}
