package productui

import (
	"strings"
	"testing"
)

func TestLegacyUXBLIND_018(t *testing.T) {
	view := testView(PagePerson)
	view.SelectedPerson = "worker-avery"

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="/workspace/app/people"`) {
		t.Fatal("person profile lost its safe directory destination")
	}
	if !strings.Contains(doc, "Return to the directory") {
		t.Fatal("person profile does not honestly label its directory fallback")
	}
	if strings.Contains(doc, "Back to People") || strings.Contains(doc, "return=") {
		t.Fatal("person profile exposes a misleading back label or an unsafe return URL")
	}
}
