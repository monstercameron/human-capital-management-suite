package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// TestChatSeedAdminPersonaResolves pins the signed-in persona against the
// generated workforce.
//
// The seeder matches it by numeric prefix because the name half of a worker key
// comes from the generated corpus and is not the seeder's to assume. That match
// has to land on somebody: when it does not, the seed silently writes no read
// state at all and every room shows unread, which is a demo defect nothing else
// in the suite would catch.
func TestChatSeedAdminPersonaResolves(t *testing.T) {
	employees, err := demoworkforce.Plan(pgstore.TenantID("harborcare-demo"))
	if err != nil {
		t.Fatal(err)
	}
	people := make([]string, 0, len(employees))
	for _, e := range employees {
		people = append(people, e.Row.WorkerKey)
	}
	index := adminIndex(people)
	if index < 0 {
		t.Fatalf("no worker matches %q; the seeded read state would go nowhere", chatSeedAdminPrefix)
	}
	if got := people[index]; got != "hc-050-rafael-torres" {
		t.Fatalf("admin persona = %q, want hc-050-rafael-torres", got)
	}
	// Exactly one, or the prefix is not an identity.
	matches := 0
	for _, p := range people {
		if len(p) >= len(chatSeedAdminPrefix) && p[:len(chatSeedAdminPrefix)] == chatSeedAdminPrefix {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("%d workers match %q", matches, chatSeedAdminPrefix)
	}
}
