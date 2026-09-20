package journeyclient

import (
	"context"
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	xhtml "golang.org/x/net/html"
)

// REV-095-01: the edit-proposal dialog refuses a submit with blank required
// fields in the client, marks each one aria-invalid with a linked inline
// message and asks the browser adapter to focus the first one, without a
// request to the server.

func rev09501EditAction(p journey.Page) (journey.Action, bool) {
	if p.Detail == nil {
		return journey.Action{}, false
	}
	for _, action := range p.Detail.Actions {
		if action.ID == ActionEditProposal && !action.Disabled {
			return action, true
		}
	}
	return journey.Action{}, false
}

func rev09501Harness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.svc.edited = &journeyv1.EditProposalResponse{Journey: &journeyv1.Journey{IntentId: "01a0b18f-94ce-7560-b01f-71a6af3b5999"}}
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail with an edit action", func(p journey.Page) bool {
		_, ok := rev09501EditAction(p)
		return detailShown(p) && ok
	})
	return h
}

func rev09501Complete() map[string]string {
	return map[string]string{
		NameEditJobCode: "WRK-MGR", NameEditGrade: "M2", NameEditBase: "98000.00",
		NameEditEffective: "2026-12-01", NameEditBusinessReason: "Leads the regional operations team.",
		NameEditReason: "The grade in the original request was wrong.",
	}
}

func TestTodo_REV_095_01(t *testing.T) {
	h := rev09501Harness(t)
	values := rev09501Complete()
	for _, name := range []string{NameEditGrade, NameEditEffective, NameEditBusinessReason, NameEditReason} {
		values[name] = "   "
	}
	h.app.Submit(ActionEditProposal, values)

	p := h.awaitPage(t, "the edit refusal", func(p journey.Page) bool {
		action, ok := rev09501EditAction(p)
		return ok && p.FocusInvalidRevision != 0 && len(action.Fields) > 0 && action.Fields[1].Error != ""
	})
	action, _ := rev09501EditAction(p)
	invalid := map[string]bool{}
	for _, field := range action.Fields {
		if field.Error != "" {
			invalid[field.Name] = true
			if field.Error != "Complete this field." {
				t.Errorf("%s error = %q, want the localized required message", field.Name, field.Error)
			}
		}
	}
	for _, name := range []string{NameEditGrade, NameEditEffective, NameEditBusinessReason, NameEditReason} {
		if !invalid[name] {
			t.Errorf("blank %s was not marked invalid", name)
		}
	}
	for _, name := range []string{NameEditJobCode, NameEditBase} {
		if invalid[name] {
			t.Errorf("filled %s was marked invalid", name)
		}
	}
	if got := h.svc.called("EditProposal"); got != 0 {
		t.Fatalf("an incomplete edit reached the server %d times", got)
	}

	// The rendered dialog links each message and marks the control invalid;
	// the first invalid control in form order is target_grade.
	markup, err := journey.RenderToString(p)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var firstInvalid string
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && rev09501Attr(node, "aria-invalid") == "true" && firstInvalid == "" {
			firstInvalid = rev09501Attr(node, "id")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if firstInvalid != FieldEditGrade {
		t.Fatalf("first aria-invalid control = %q, want %q", firstInvalid, FieldEditGrade)
	}
	for _, id := range []string{FieldEditGrade, FieldEditEffective, FieldEditBusinessReason, FieldEditReason} {
		if !strings.Contains(markup, `id="`+id+`-error"`) {
			t.Errorf("%s has no visible inline message", id)
		}
		if !rev09501DescribedBy(root, id, id+"-error") {
			t.Errorf("%s is not linked to its message through aria-describedby", id)
		}
	}

	// Answering one field clears only its own message, without a new focus
	// request.
	revision := p.FocusInvalidRevision
	p.OnFieldChange(FieldEditGrade, "M2")
	after, _ := rev09501EditAction(h.store.Page())
	for _, field := range after.Fields {
		switch field.ID {
		case FieldEditGrade:
			if field.Error != "" {
				t.Errorf("answered grade still shows %q", field.Error)
			}
		case FieldEditReason:
			if field.Error == "" {
				t.Error("an unrelated field lost its message")
			}
		}
	}
	if got := h.store.Page().FocusInvalidRevision; got != revision {
		t.Fatalf("typing requested focus again: %d -> %d", revision, got)
	}

	// A repeated refusal asks for focus again.
	h.app.Submit(ActionEditProposal, values)
	h.awaitPage(t, "the repeated refusal", func(p journey.Page) bool { return p.FocusInvalidRevision > revision })

	// A complete submit passes through unchanged.
	h.app.Submit(ActionEditProposal, rev09501Complete())
	for deadline := time.Now().Add(3 * time.Second); h.svc.called("EditProposal") == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if got := h.svc.called("EditProposal"); got != 1 {
		t.Fatalf("a complete edit reached the server %d times, want 1", got)
	}
}

func rev09501Attr(node *xhtml.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func rev09501DescribedBy(root *xhtml.Node, id, target string) bool {
	found := false
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && rev09501Attr(node, "id") == id {
			for _, part := range strings.Fields(rev09501Attr(node, "aria-describedby")) {
				if part == target {
					found = true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}
