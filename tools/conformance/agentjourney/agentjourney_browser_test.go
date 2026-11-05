package agentjourney

import (
	"strings"
	"testing"
)

// TestTodo_AGENT_047_Browser pins the browser-visible journey contract. While
// the submit step waits, the agents page shows the confirmed plan, the
// pending approval with its exact reference digest, and its sources in
// English, German, and Arabic. After the carry, the page shows the submitted
// intent as done and checked with no pending approval. Nothing raw — no
// state code, no taint label, no internal reference — reaches the page, the
// task title stays keyboard-focusable, and Arabic renders right-to-left.
func TestTodo_AGENT_047_Browser(t *testing.T) {
	world := NewWorld(t)
	world.SelectProvider()
	world.PublishAgent()
	world.InstallAgent()
	world.AnswerDM()
	world.ParkForApproval()

	parked := map[string][]string{
		"en-US": {"Brief the team and file the approved promotion", "You confirmed the plan", "Waiting for your approval", "Approval reference", "Document: Team handbook", "Written by you", "Written by the agent", "Private draft", "Your request"},
		"de-DE": {"Sie haben den Plan bestätigt", "Wartet auf Ihre Zustimmung", "Zustimmungsreferenz", "Dokument: Team handbook", "Von Ihnen geschrieben", "Vom Agenten geschrieben", "Privater Entwurf", "Ihre Anfrage"},
		"ar":    {"أكّدتَ الخطة", "بانتظار موافقتك", "مرجع الموافقة", "مستند: Team handbook", "كتبتَه أنت", "كتبه الوكيل", "مسودة خاصة", "طلبك"},
	}
	for locale, texts := range parked {
		page := world.RenderAgentsPage(locale)
		for _, text := range texts {
			if !strings.Contains(page, text) {
				t.Errorf("%s: the parked page does not say %q", locale, text)
			}
		}
		for _, markup := range []string{
			`data-task-view="` + journeyTask + `"`,
			`data-approval-id="` + journeyTask + `/submit"`,
			world.StepApprovalDigest(),
			`id="agents-task-title" tabindex="-1"`,
			`dir="auto"`,
		} {
			if markup == "" || !strings.Contains(page, markup) {
				t.Errorf("%s: the parked page is missing %s", locale, markup)
			}
		}
		for _, code := range []string{">awaiting_approval<", "AWAITING_APPROVAL", "EXTERNAL_UNTRUSTED", "AGENT_DERIVED", "USER_AUTHORED", "pinv_", "arreq_", "earlier_step", ">records<", ">request<"} {
			if strings.Contains(page, code) {
				t.Errorf("%s: the parked page shows the code %q", locale, code)
			}
		}
	}
	if rtl := world.RenderAgentsPage("ar"); !strings.Contains(rtl, "rtl") {
		t.Error("ar: the agents page carries no right-to-left marker")
	}

	world.CarryHCMAction()

	driven := map[string][]string{
		"en-US": {"team.promotion_submit", "Done and checked"},
		"de-DE": {"team.promotion_submit", "Erledigt und geprüft"},
		"ar":    {"team.promotion_submit", "تمّ وجرى التحقق"},
	}
	for locale, texts := range driven {
		page := world.RenderAgentsPage(locale)
		for _, text := range texts {
			if !strings.Contains(page, text) {
				t.Errorf("%s: the driven page does not say %q", locale, text)
			}
		}
		if strings.Contains(page, "data-approval-id=") {
			t.Errorf("%s: the driven page still shows a pending approval", locale)
		}
	}
}
