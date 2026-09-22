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

func TestFingerprintStableAndContentBound(t *testing.T) {
	if fingerprint("hello") != fingerprint("hello") {
		t.Fatal("fingerprint is not stable")
	}
	if fingerprint("hello") == fingerprint("hello ") {
		t.Fatal("fingerprint ignored content change")
	}
}
