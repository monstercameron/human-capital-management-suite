package program

import (
	"strings"
	"testing"
	"time"
)

func TestTodo_PROGRAM_002(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	if rev.Digest == "" {
		t.Fatal("revision carries no digest")
	}
	got, err := c.ResolveRevision(ResolveContext{
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		At: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ResolveRevision: %v", err)
	}
	if got.Digest != rev.Digest || got.Version != 1 {
		t.Fatalf("resolver returned version %d, want 1", got.Version)
	}
	// No ambient latest: outside every interval fails.
	_, err = c.ResolveRevision(ResolveContext{
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		At: time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("resolution outside every interval fell back to latest")
	}
}

func TestTodo_PROGRAM_002_Property(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	// Successive non-overlapping revisions resolve exactly per instant.
	for v, year := range []int{2026, 2027, 2028} {
		_, err := c.AppendRevision(testCaller, Revision{
			ProgramID: def.ID, Version: uint64(v + 1), Tenant: "tenant-acme",
			Org: "org:acme", Jurisdiction: "US-CA",
			From: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("AppendRevision v%d: %v", v+1, err)
		}
	}
	for v, year := range []int{2026, 2027, 2028} {
		got, err := c.ResolveRevision(ResolveContext{
			Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
			At: time.Date(year, 7, 1, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("resolve %d: %v", year, err)
		}
		if got.Version != uint64(v+1) {
			t.Fatalf("year %d resolved to version %d", year, got.Version)
		}
	}
}

func TestTodo_PROGRAM_002_Fault(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	mustRevision(t, c, def.ID)
	// A failing backing store reaches a typed refusal with zero effects.
	c.SetRevisionStore(failStore{})
	_, err := c.ResolveRevision(ResolveContext{
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		At: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("store fault silently resolved")
	}
	if !strings.Contains(err.Error(), "revision store") {
		t.Fatalf("fault is not a typed store refusal: %v", err)
	}
	// Define + AppendRevision journalled two entries; the fault adds none.
	if len(c.Journal()) != 2 {
		t.Fatalf("fault left journal effects: %d entries", len(c.Journal()))
	}
}

func FuzzTodo_PROGRAM_002(f *testing.F) {
	f.Add([]byte("tenant-acme"), []byte("US-CA"), int64(1750000000))
	f.Fuzz(func(t *testing.T, tenant, jurisdiction []byte, at int64) {
		ctx := ResolveContext{
			Tenant: string(tenant), Org: "org:acme",
			Jurisdiction: string(jurisdiction),
			At:           time.Unix(at, 0).UTC(),
		}
		// Must never panic; empty tenants must never resolve.
		c := NewCatalog()
		if _, err := c.ResolveRevision(ctx); err == nil && len(tenant) == 0 {
			t.Fatal("empty tenant resolved")
		}
	})
}

func TestTodo_PROGRAM_002_Security(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	mustRevision(t, c, def.ID)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	_, err := c.AppendRevision(rival, Revision{
		ProgramID: def.ID, Version: 2, Tenant: "tenant-acme",
		Org: "org:acme", Jurisdiction: "US-CA",
		From: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("cross-tenant revision append accepted")
	}
	if strings.Contains(err.Error(), "tenant-acme") {
		t.Fatalf("denial leaks tenant existence: %v", err)
	}
}

func TestTodo_PROGRAM_002_Mutation(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	mustRevision(t, c, def.ID)
	// Mutant A: overlapping revision must be killed.
	_, err := c.AppendRevision(testCaller, Revision{
		ProgramID: def.ID, Version: 2, Tenant: "tenant-acme",
		Org: "org:acme", Jurisdiction: "US-CA",
		From: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("overlapping-revision mutant survived")
	}
	// Mutant B: tampered digest on append must be killed.
	tampered := Revision{
		ProgramID: def.ID, Version: 2, Tenant: "tenant-acme",
		Org: "org:acme", Jurisdiction: "US-CA",
		From:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC),
		Digest: "sha256:" + strings.Repeat("f", 64),
	}
	if _, err := c.AppendRevision(testCaller, tampered); err == nil {
		t.Fatal("tampered-digest mutant survived")
	}
	// Mutant C: wrong-jurisdiction resolution must not return the revision.
	_, err = c.ResolveRevision(ResolveContext{
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-NY",
		At: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("cross-jurisdiction mutant survived")
	}
}
