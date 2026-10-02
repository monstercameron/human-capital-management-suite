package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The four Ironridge people the roster gave no photograph are drawn with an
// image, not initials, in the members list and in Person details. The worker
// carries the URL the directory serves for them.
func TestChatPhotosSeededPeopleAreDrawnWithAnImage(t *testing.T) {
	people := []struct{ key, name, initials string }{
		{"ir-014-ben-whitaker", "Ben Whitaker", "BW"},
		{"ir-020-chris-yazzie", "Chris Yazzie", "CY"},
		{"ir-021-eddie-ramirez", "Eddie Ramirez", "ER"},
		{"ir-037-hector-salas", "Hector Salas", "HS"},
	}
	var workers []*journeyv1.Worker
	var members []chatui.Member
	for _, person := range people {
		entry, ok := demoworkforce.DirectoryEntryFor("ironridge-demo", person.key)
		if !ok || entry.PhotoURL == "" {
			t.Fatalf("%s has no directory photograph: %+v", person.name, entry)
		}
		workers = append(workers, &journeyv1.Worker{WorkerRef: person.key, SubjectId: person.key, LegalName: person.name, ProfilePhotoUrl: entry.PhotoURL})
		members = append(members, chatui.Member{ID: person.key, Name: person.name})
	}
	photos := chatPhotosFromWorkers(workers)
	render := func(m chatui.Model) string {
		t.Helper()
		markup, err := ui.RenderToString(chatui.Build(m))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}

	list := render(chatui.Model{State: chatui.StateReady, SelectedID: "room", CurrentUser: "viewer", ShowDetails: true, PhotoURLs: photos, Members: members,
		Conversations: []chatui.Conversation{{ID: "room", Name: "Room", Kind: chatui.PublicChannel}}})
	for _, person := range people {
		if !strings.Contains(list, `src="`+photos[person.key]+`"`) {
			t.Errorf("members list draws no image for %s", person.name)
		}
		if strings.Contains(list, ">"+person.initials+"</span>") {
			t.Errorf("members list draws initials %s for %s", person.initials, person.name)
		}
	}

	for _, person := range people {
		detail, ok := chatPersonDetailsFromWorkers(workers, person.key)
		if !ok {
			t.Fatalf("no details for %s", person.name)
		}
		panel := render(chatui.Model{State: chatui.StateReady, ShowPerson: true, PhotoURLs: photos, PersonDetails: &detail,
			Callbacks: chatui.Callbacks{OpenPerson: func(string) {}, ClosePerson: func() {}}})
		if !strings.Contains(panel, `src="`+photos[person.key]+`"`) {
			t.Errorf("Person details draws no image for %s", person.name)
		}
		if strings.Contains(panel, ">"+person.initials+"</span>") {
			t.Errorf("Person details draws initials %s for %s", person.initials, person.name)
		}
	}
}

// A unit the directory names is shown by that name wherever the department is
// printed, with the code turned into words only as a last resort.
func TestUnitDisplayNameUsesTheDirectoryName(t *testing.T) {
	worker := &journeyv1.Worker{OrgUnit: "safety-quality", OrgUnitName: "Safety & Quality"}
	if got := chatDepartmentName(worker); got != "Safety & Quality" {
		t.Fatalf("department = %q", got)
	}
}
