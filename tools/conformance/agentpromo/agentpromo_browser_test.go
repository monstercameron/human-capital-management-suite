package agentpromo

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENT2_024_Browser pins the browser-visible journey contract: the
// manager's ask, the confirmed plan with its live step, the pending batch
// approval with its exact digest and tainted sources, and the submitted
// intents with their owner statuses. Nothing raw — no reference, no taint
// code, no secret — reaches the page.
func TestTodo_AGENT2_024_Browser(t *testing.T) {
	ctx := context.Background()

	parked := newPromoWorld(t, promoTaskBrowser, promoDirectory(t))
	parked.runReadPhase()
	parked.runDraftPhase()
	parked.runApprovalPhase()
	parked.parkSubmitStep()
	reader := agentclient.New(promoRunners{store: parked.store})
	snapshot, err := reader.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: "harborcare-demo", Principal: promoManager})
	if err != nil || len(snapshot.Tasks) != 1 {
		t.Fatalf("snapshot=%+v err=%v, want the manager's single task", snapshot.Tasks, err)
	}
	task := snapshot.Tasks[0]
	if task.ID != promoTaskBrowser || task.Goal != promoGoal || task.Title != promoGoal {
		t.Fatalf("task=%+v, want the manager's ask as goal and title", task)
	}
	if len(task.Steps) != 10 {
		t.Fatalf("steps=%+v, want the ten confirmed plan steps", task.Steps)
	}
	if task.Steps[7].Name != promoSkillSubmit || task.Steps[7].Tier != "T3" || task.Steps[7].State != "awaiting_approval" {
		t.Fatalf("submit step=%+v, want the parked T3 submission", task.Steps[7])
	}
	if len(task.Approvals) != 1 {
		t.Fatalf("approvals=%+v, want the single pending batch", task.Approvals)
	}
	approval := task.Approvals[0]
	if approval.ID != promoTaskBrowser+"/submit" || approval.Digest == "" || approval.Summary != promoSkillSubmit {
		t.Fatalf("approval=%+v, want the submit step with its exact digest", approval)
	}
	if got, want := strings.Join(approval.Sources, "|"), "document:Q2 calibration notes|records|request"; got != want {
		t.Fatalf("approval sources=%q, want %q", got, want)
	}
	if approval.Taint == "" || strings.Contains(approval.Taint, "EXTERNAL_UNTRUSTED") {
		t.Fatalf("approval taint=%q, want summarized labels without the raw taint code", approval.Taint)
	}
	if len(task.SubmittedIntents) != 1 || task.SubmittedIntents[0].Name != promoSkillSubmit {
		t.Fatalf("submitted intents=%+v, want the parked submission", task.SubmittedIntents)
	}

	for locale, checkpoints := range map[string][]string{
		"en-US": {"You confirmed the plan", "Finished: promo.calibration_read", "Waiting for your approval", "Private draft", "Approval reference", "Document: Q2 calibration notes"},
		"de-DE": {"Sie haben den Plan bestätigt", "Wartet auf Ihre Zustimmung", "Privater Entwurf", "Zustimmungsreferenz", "Dokument: Q2 calibration notes"},
		"ar":    {"أكّدتَ الخطة", "بانتظار موافقتك", "مسودة خاصة", "مرجع الموافقة", "مستند: Q2 calibration notes"},
	} {
		page := promoRenderPage(t, snapshot, promoManager, promoTaskBrowser, locale)
		for _, text := range checkpoints {
			if !strings.Contains(page, text) {
				t.Errorf("%s: the task view does not say %q", locale, text)
			}
		}
		for _, markup := range []string{
			`data-task-view="` + promoTaskBrowser + `"`,
			`data-approval-id="` + promoTaskBrowser + `/submit"`,
			approval.Digest,
			`href="/workspace/app/docs?document=` + promoDocID + `"`,
		} {
			if !strings.Contains(page, markup) {
				t.Errorf("%s: the task view is missing %s", locale, markup)
			}
		}
		for _, code := range []string{">awaiting_approval<", "EXTERNAL_UNTRUSTED", "AGENT_DERIVED", "draft:promo-batch-1", "promo-submit-1", ">records<", "document:Q2"} {
			if strings.Contains(page, code) {
				t.Errorf("%s: the task view shows the code %q", locale, code)
			}
		}
	}

	completed := newPromoWorld(t, promoTask, promoDirectory(t))
	completed.driveFull()
	doneReader := agentclient.New(promoRunners{store: completed.store})
	done, err := doneReader.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: "harborcare-demo", Principal: promoManager})
	if err != nil || len(done.Tasks) != 1 {
		t.Fatalf("completed snapshot=%+v err=%v", done.Tasks, err)
	}
	finished := done.Tasks[0]
	if finished.State != productui.AgentTaskCompleted {
		t.Fatalf("completed state=%q, want completed", finished.State)
	}
	if len(finished.Approvals) != 0 {
		t.Fatalf("completed approvals=%+v, want none pending", finished.Approvals)
	}
	var kinds []string
	for _, artifact := range finished.Artifacts {
		kinds = append(kinds, artifact.Kind)
	}
	for _, want := range []string{productui.AgentArtifactReport, productui.AgentArtifactDraft, productui.AgentArtifactDocument} {
		found := false
		for _, kind := range kinds {
			found = found || kind == want
		}
		if !found {
			t.Fatalf("completed artifacts=%+v, want the report, the draft and the document", finished.Artifacts)
		}
	}
	if len(finished.SubmittedIntents) != 1 || finished.SubmittedIntents[0].Status != productui.AgentIntentExecuting {
		t.Fatalf("completed intents=%+v, want the submitted promotion carried out", finished.SubmittedIntents)
	}
	page := promoRenderPage(t, done, promoManager, promoTask, "en-US")
	for _, text := range []string{"Report", "Private draft", "Q2 calibration notes", "Being carried out", "Finished: promo.outcome_report"} {
		if !strings.Contains(page, text) {
			t.Errorf("the completed view does not say %q", text)
		}
	}
}
