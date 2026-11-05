package journey

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// The pure row merge joins on the worker ref, then the subject id; a unit
// code the namer knows gains its display name; a photograph already on the
// row wins; contact fields are set only for the chat projection; nothing
// else moves.
func TestTodo_JOURNEY_ApplyDirectoryEntries(t *testing.T) {
	namer := func(code string) string {
		if code == "ops" {
			return "Operations"
		}
		return ""
	}
	entries := map[string]directoryEntry{
		"w-1": {PhotoURL: "/w1.jpg", Email: "w1@example.com", Phone: "111"},
		"s-2": {PhotoURL: "/s2.jpg", Email: "s2@example.com", Phone: "222"},
	}
	workers := []*journeyv1.Worker{
		{WorkerRef: "w-1", SubjectId: "w-1", OrgUnit: "ops"},
		{WorkerRef: "unknown-ref", SubjectId: "s-2", OrgUnit: "ops"},
		{WorkerRef: "w-1", SubjectId: "w-1", OrgUnit: "ops", ProfilePhotoUrl: "/kept.jpg"},
		{WorkerRef: "nobody", SubjectId: "nobody", OrgUnit: "never"},
		nil,
	}
	applyDirectoryEntries(workers, namer, entries, true)
	if got := workers[0]; got.OrgUnitName != "Operations" || got.ProfilePhotoUrl != "/w1.jpg" || got.WorkEmail != "w1@example.com" || got.WorkPhone != "111" {
		t.Fatalf("ref join = %+v", got)
	}
	if got := workers[1]; got.ProfilePhotoUrl != "/s2.jpg" || got.WorkEmail != "s2@example.com" {
		t.Fatalf("subject join = %+v", got)
	}
	if got := workers[2]; got.ProfilePhotoUrl != "/kept.jpg" || got.WorkEmail != "w1@example.com" {
		t.Fatalf("row photo must win, contacts still merge = %+v", got)
	}
	if got := workers[3]; got.OrgUnitName != "" || got.WorkEmail != "" || got.ProfilePhotoUrl != "" {
		t.Fatalf("unknown worker moved: %+v", got)
	}

	plain := []*journeyv1.Worker{{WorkerRef: "w-1", SubjectId: "w-1", OrgUnit: "ops"}}
	applyDirectoryEntries(plain, namer, entries, false)
	if got := plain[0]; got.OrgUnitName != "Operations" || got.ProfilePhotoUrl != "/w1.jpg" || got.WorkEmail != "" || got.WorkPhone != "" {
		t.Fatalf("listing projection = %+v", got)
	}

	unnamed := []*journeyv1.Worker{{WorkerRef: "w-1", SubjectId: "w-1", OrgUnit: "ops"}}
	applyDirectoryEntries(unnamed, nil, entries, true)
	if got := unnamed[0]; got.OrgUnitName != "" || got.WorkEmail != "w1@example.com" {
		t.Fatalf("nil namer = %+v", got)
	}
}
