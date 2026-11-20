package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestIssue3789_NamesTheCoveredPersonFromKnownPeopleOnly(t *testing.T) {
	items := []productui.ChatNotification{
		{ID: "a", Kind: productui.ChatNotificationCover, CoveringID: "worker-1"},
		{ID: "b", Kind: productui.ChatNotificationCover, CoveringID: "worker-9"},
		{ID: "c", Kind: productui.ChatNotificationCover, CoveringID: "worker-1", CoveringName: "Kept"},
	}
	people := []productui.Person{{ID: "p-1", WorkerID: "worker-1", Name: "Jo", PreferredName: "Joanna"}}
	nameChatNotificationAuthors(items, people)
	if items[0].CoveringName != "Joanna" || items[1].CoveringName != "" || items[2].CoveringName != "Kept" {
		t.Fatalf("names = %q %q %q", items[0].CoveringName, items[1].CoveringName, items[2].CoveringName)
	}
}
