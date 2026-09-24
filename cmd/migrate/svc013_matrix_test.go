package main

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestTodo_SVC_013_Golden(t *testing.T) {
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("migration command embeds no migration files")
	}
	if files[0].Version != 1 || files[0].Name == "" {
		t.Fatalf("first migration = %+v, want version 1 with a stable name", files[0])
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 {
		t.Fatalf("migration artifact digest has %d characters, want SHA-256 length", len(digest))
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		t.Fatal(err)
	}
	if target != files[len(files)-1].Version {
		t.Fatalf("target version %d != final embedded migration %d", target, files[len(files)-1].Version)
	}
}

func TestTodo_SVC_013_Race(t *testing.T) {
	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd, rest := splitCommand([]string{"up", "-database-url=postgres://user:secret@db/app"})
			if cmd != "up" || len(rest) != 1 || rest[0] != "-database-url=postgres://user:secret@db/app" {
				t.Errorf("splitCommand result = %q, %v", cmd, rest)
			}
			s := spec(cmd, rest)
			if s.HealthAddr != "" || s.DatabaseURLField != "" || s.DBPoolFactory != nil {
				t.Errorf("migrate unexpectedly declared serving DB lifecycle fields: health=%q db=%q factory=%t", s.HealthAddr, s.DatabaseURLField, s.DBPoolFactory != nil)
			}
		}()
	}
	wg.Wait()
}
