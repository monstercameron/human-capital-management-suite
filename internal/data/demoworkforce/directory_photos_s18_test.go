package demoworkforce

import (
	"testing"

	"github.com/google/uuid"
)

// TestDirectoryPhotos_NoImageIsShared fails when two people of one company
// wear the same photograph, and when a spare headshot named for someone is not
// a person the company employs.
func TestDirectoryPhotos_NoImageIsShared(t *testing.T) {
	for _, pack := range Packs() {
		t.Run(pack.Key, func(t *testing.T) {
			directory, err := pack.Directory()
			if err != nil {
				t.Fatal(err)
			}
			wearer := map[string]string{}
			for key, entry := range directory {
				if entry.PhotoURL == "" {
					continue
				}
				if other, dup := wearer[entry.PhotoURL]; dup {
					t.Errorf("%s and %s both wear %s", key, other, entry.PhotoURL)
				}
				wearer[entry.PhotoURL] = key
			}
			employees, err := pack.Plan(uuid.NewSHA1(demoNamespace, []byte("photos")))
			if err != nil {
				t.Fatal(err)
			}
			known := map[string]Employee{}
			for _, employee := range employees {
				known[employee.Row.WorkerKey] = employee
			}
			for key := range spareHeadshots[pack.Key] {
				employee, ok := known[key]
				if !ok {
					t.Errorf("spare headshot names %q, who %s does not employ", key, pack.Key)
				} else if employee.HasProfilePhoto {
					t.Errorf("%s already has a headshot in the plan and needs no spare", key)
				}
			}
		})
	}
}

// TestDirectoryPhotos_RosterRowsAreUnchanged keeps the append-only rows
// authoritative: the four Ironridge people without a photograph in the roster
// still have none there, and the directory lends them one.
func TestDirectoryPhotos_RosterRowsAreUnchanged(t *testing.T) {
	for _, key := range []string{"ir-014-ben-whitaker", "ir-020-chris-yazzie", "ir-021-eddie-ramirez", "ir-037-hector-salas"} {
		entry, ok := DirectoryEntryFor(IronridgeKey, key)
		if !ok || entry.PhotoURL == "" {
			t.Errorf("%s has no directory photograph: %+v", key, entry)
		}
	}
	employees, err := IronridgePack.Plan(uuid.NewSHA1(demoNamespace, []byte("rows")))
	if err != nil {
		t.Fatal(err)
	}
	for _, employee := range employees {
		switch employee.Row.WorkerKey {
		case "ir-014-ben-whitaker", "ir-020-chris-yazzie", "ir-021-eddie-ramirez", "ir-037-hector-salas":
			if employee.Row.ProfilePhotoProxyRef != "" {
				t.Errorf("%s: the plan row gained a photograph, which replays would reject", employee.Row.WorkerKey)
			}
		}
	}
}
