package productui

import (
	"errors"
	"strings"
	"testing"
)

func TestTodo_WEB_193_Security(t *testing.T) {
	view := testView(PageHelpHub)
	// A nonempty projection makes the denial authoritative: an empty legacy
	// projection retains the component-preview compatibility behavior.
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PageHome, View: true}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/workspace/app/help/knowledge-search", "/workspace/app/help/hr-service-request", "/workspace/app/help/confidential-case", "/workspace/app/help/case-status"} {
		if strings.Contains(doc, route) {
			t.Errorf("help hub disclosed destination without a page grant: %s", route)
		}
	}
	if !strings.Contains(doc, "Employee Help hub") {
		t.Fatal("help hub failed to retain its safe support fallback")
	}
}

func TestTodo_WEB_194_Security(t *testing.T) {
	view := testView(PageKnowledgeSearch)
	view.Query = "restricted leave"
	view.SearchKnowledge = func(string) ([]KnowledgeSearchResult, error) {
		return nil, errors.New("restricted-secret: database password=canary")
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"restricted-secret", "database password", "canary"} {
		if strings.Contains(doc, secret) {
			t.Fatalf("knowledge search exposed backend failure detail %q", secret)
		}
	}
	if !strings.Contains(doc, "Authorized knowledge search is not published yet") {
		t.Fatal("knowledge search did not render its nondisclosing failure state")
	}
}

func TestTodo_WEB_195_Security(t *testing.T) {
	view := testView(PageHRServiceRequest)
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PageHRServiceRequest, View: true}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "disabled") {
		t.Fatal("service request submit remained enabled without create permission")
	}
	if strings.Contains(doc, "confidential-case") {
		t.Fatal("service request exposed confidential escalation without its page grant")
	}
}

func TestTodo_WEB_196_Security(t *testing.T) {
	view := testView(PageConfidentialCase)
	view.Query = "sealed-case-canary"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Query) {
		t.Fatal("unpublished confidential intake rendered submitted case data")
	}
	if !strings.Contains(doc, "not published yet") {
		t.Fatal("confidential intake did not fail closed to the governed-service state")
	}
}

func TestTodo_WEB_197_Security(t *testing.T) {
	view := testView(PageCaseStatus)
	view.Query = "case-id-canary"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Query) || strings.Contains(doc, "case number") || strings.Contains(doc, "status: ") {
		t.Fatal("safe participant status disclosed an unverified case identifier or status")
	}
	if !PageVisible(PageCaseStatus, []string{"worker_self"}) || PageVisible(PageCaseStatus, []string{"unknown_role"}) {
		t.Fatal("participant case status role boundary is not enforced")
	}
}

func TestTodo_WEB_198_Security(t *testing.T) {
	view := testView(PageCaseMessaging)
	view.Query = "private-message-canary"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Query) || strings.Contains(doc, "<textarea") || strings.Contains(doc, "Send message") {
		t.Fatal("unpublished case messaging exposed private content or an active composer")
	}
	if !PageVisible(PageCaseMessaging, []string{"worker_self"}) || PageVisible(PageCaseMessaging, []string{"unknown_role"}) {
		t.Fatal("case messaging role boundary is not enforced")
	}
}

func TestTodo_WEB_199_Security(t *testing.T) {
	view := testView(PageCaseCenter)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"Jordan Lee", "Avery Patel", "intent-1", "intent-2"} {
		if strings.Contains(doc, data) {
			t.Errorf("unpublished Case Center leaked unrelated fixture data %q", data)
		}
	}
	if !PageVisible(PageCaseCenter, []string{"hr_partner"}) || PageVisible(PageCaseCenter, []string{"worker_self"}) {
		t.Fatal("specialist Case Center role boundary is not enforced")
	}
}

func TestTodo_WEB_200_Security(t *testing.T) {
	view := testView(PageCaseAssignment)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"Assign to", "Recuse", "worker-jordan", "intent-1"} {
		if strings.Contains(doc, data) {
			t.Errorf("unpublished assignment surface exposed actionable or identifying data %q", data)
		}
	}
	if !PageVisible(PageCaseAssignment, []string{"hr_partner"}) || PageVisible(PageCaseAssignment, []string{"worker_self"}) {
		t.Fatal("case assignment role boundary is not enforced")
	}
}

func TestTodo_WEB_201_Security(t *testing.T) {
	view := testView(PageCaseEvidence)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"evidence-canary", "confidential attachment", "Download evidence"} {
		if strings.Contains(doc, data) {
			t.Errorf("unpublished evidence review exposed evidence content or action %q", data)
		}
	}
	if !PageVisible(PageCaseEvidence, []string{"hr_partner"}) || PageVisible(PageCaseEvidence, []string{"worker_self"}) {
		t.Fatal("case evidence role boundary is not enforced")
	}
}

func TestTodo_WEB_202_Security(t *testing.T) {
	view := testView(PageCaseDisposition)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"finding-canary", "substantiated", "Dismiss case"} {
		if strings.Contains(doc, data) {
			t.Errorf("unpublished disposition surface exposed a finding or action %q", data)
		}
	}
	if !PageVisible(PageCaseDisposition, []string{"hr_partner"}) || PageVisible(PageCaseDisposition, []string{"worker_self"}) {
		t.Fatal("case disposition role boundary is not enforced")
	}
}

func TestTodo_WEB_203_Security(t *testing.T) {
	view := testView(PageCaseAppeal)
	view.Query = "appeal-canary"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Query) || strings.Contains(doc, "Appeal submitted") || strings.Contains(doc, "finding-canary") {
		t.Fatal("unpublished appeal surface disclosed an appeal or finding state")
	}
	if !PageVisible(PageCaseAppeal, []string{"worker_self"}) || PageVisible(PageCaseAppeal, []string{"unknown_role"}) {
		t.Fatal("case appeal role boundary is not enforced")
	}
}

func TestTodo_WEB_204_Security(t *testing.T) {
	view := testView(PageCaseRedaction)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"audit trail clean", "12 fields redacted", "unredacted-canary", "checked by"} {
		if strings.Contains(strings.ToLower(doc), strings.ToLower(claim)) {
			t.Errorf("unpublished redaction surface made an unsupported audit claim %q", claim)
		}
	}
	if !PageVisible(PageCaseRedaction, []string{"hr_partner"}) || PageVisible(PageCaseRedaction, []string{"worker_self"}) {
		t.Fatal("case redaction role boundary is not enforced")
	}
}
