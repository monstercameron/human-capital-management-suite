package agentclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// agent2017Task is a task as the runtime stores it part way through governed
// work: the plan is confirmed, a read and a draft are finished, a submission
// waits for the owner's approval and a verification has not started. Its
// ledger and inputs hold identifiers and references the page must not show.
func agent2017Task() agentrun.AgentTask {
	started := fixedNow.Add(-10 * time.Minute)
	return agentrun.AgentTask{
		ID: "task-promote", TenantID: tenantKey, UserID: "hc-051-linh-tran", Goal: "Prepare the promotion request for Ana Flores\nUse the calibration notes.",
		State: agentrun.StateAwaitingApproval, Version: 7, CurrentStep: 2, CreatedAt: started, UpdatedAt: fixedNow,
		FailureDetail: "internal failure detail tenant-secret",
		Wake:          &agentrun.WakeCondition{Kind: agentrun.WakeApproval, Key: "approval:wake-key-secret"},
		LastWake:      &agentrun.WakeEvent{ID: "wake-1", Kind: agentrun.WakeSignal, Key: "signal:key-secret", OccurredAt: fixedNow.Add(-4 * time.Minute)},
		Plan: agentrun.AgentPlan{
			Revision: 1, Confirmed: true, ConfirmedBy: "hc-051-linh-tran", ConfirmedAt: started.Add(time.Minute), Digest: "sha256:plan",
			DocumentReferences: []agentdocref.Reference{{DocumentID: "doc-calibration", Label: "Calibration notes", VersionMode: agentdocref.ModeLatestPublished}},
			Steps: []agentrun.PlanStep{
				{ID: "read", Type: agentrun.StepRead, SkillID: "agent.read_handbook", SkillVersion: 1, Tier: agentrun.TierRead, State: agentrun.StepCompleted, ResultRef: "result:read-ref-secret", StartedAt: started.Add(2 * time.Minute), FinishedAt: started.Add(3 * time.Minute)},
				{ID: "draft", Type: agentrun.StepDraft, SkillID: "agent.summarize_request", SkillVersion: 1, Tier: agentrun.TierPrivateDraft, State: agentrun.StepCompleted, ResultRef: "draft:draft-ref-secret", StartedAt: started.Add(4 * time.Minute), FinishedAt: started.Add(5 * time.Minute)},
				{ID: "submit", Type: agentrun.StepSubmit, SkillID: "people.promote_worker", SkillVersion: 1, Tier: agentrun.TierSubmitGoverned, State: agentrun.StepAwaitingApproval, ApprovalDigest: "sha256:approve-submit", Destination: "queue:destination-secret",
					Inputs: []agentrun.InputRef{
						{Name: "draft", Ref: "draft:draft-ref-secret"},
						{Name: "goal", Ref: "task:task-promote", Taint: []string{"USER_AUTHORED"}},
						{Name: "peer", Ref: "chat:peer-ref-secret", SourceID: "worker:other-tenant:salary-band", Taint: []string{"UNTRUSTED_PEER"}},
					}},
				{ID: "verify", Type: agentrun.StepVerify, SkillID: "people.read_promotion", SkillVersion: 1, Tier: agentrun.TierRead, State: agentrun.StepPending, VerificationRef: "owner:verify-ref-secret"},
			},
		},
		Ledger: agentrun.TaskLedger{Entries: []agentrun.LedgerEntry{
			{Sequence: 1, Kind: "USER_GOAL", Ref: "task:task-promote", Taint: []string{"USER_AUTHORED"}},
			{Sequence: 2, Kind: "STEP_RESULT", Ref: "result:read-ref-secret", SourceIDs: []string{"agent-document-usage:v1", "document:doc-calibration/version:3"}, Taint: []string{"TOOL_DERIVED"}},
			{Sequence: 3, Kind: "STEP_RESULT", Ref: "draft:draft-ref-secret", SourceIDs: []string{"document:doc-calibration/version:3", "worker:42:record-secret"}, Taint: []string{"AGENT_DERIVED"}},
			{Sequence: 4, Kind: "ARTIFACT", Ref: "document:doc-calibration", Digest: "sha256:artifact"},
			{Sequence: 5, Kind: "ARTIFACT", Ref: "report:revoked-ref-secret", Revoked: true},
		}},
	}
}

func agent2017Page(t *testing.T, snapshot productui.AgentSnapshot, user, taskID, locale string) string {
	t.Helper()
	view := productui.NewView(productui.PageAgents, tenantKey, user, "")
	view = productui.ApplyAgentsAvailability(view, productui.AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	view = productui.ApplyLocale(productui.ApplyRequest(view, productui.PageRequest{Page: productui.PageAgents, AgentTaskID: taskID}), productui.ResolveProductLocale(locale))
	markup, err := ui.RenderToString(productui.BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("&#39;", "'", "&amp;", "&", "&quot;", `"`).Replace(markup)
}

// TestTodo_AGENT2_017 reads a stored task through the served read adapter and
// checks every section of the task view the adapter fills: the saved progress
// in order, what the task produced with a link to its owning page, the
// pending approval with its exact digest and what it rests on, and the status
// of each submission. Nothing here is put on the page by the test.
func TestTodo_AGENT2_017(t *testing.T) {
	client := New(fixedRunners{reader: leakyReader{tasks: []agentrun.AgentTask{agent2017Task()}}})
	snapshot, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil || len(snapshot.Tasks) != 1 {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	task := snapshot.Tasks[0]

	var kinds []string
	for _, checkpoint := range task.Checkpoints {
		kinds = append(kinds, checkpoint.Kind+"/"+checkpoint.Step)
		if _, err := time.Parse(time.RFC3339Nano, checkpoint.At); err != nil {
			t.Errorf("checkpoint %q carries no instant: %q", checkpoint.Kind, checkpoint.At)
		}
	}
	if got, want := strings.Join(kinds, " "), "plan_confirmed/ step_finished/agent.read_handbook step_finished/agent.summarize_request resumed/ waiting_approval/"; got != want {
		t.Fatalf("checkpoints = %q, want %q", got, want)
	}
	if len(task.Artifacts) != 2 ||
		task.Artifacts[0] != (productui.AgentArtifact{Kind: productui.AgentArtifactDocument, Name: "Calibration notes", Href: "/workspace/app/docs?document=doc-calibration"}) ||
		task.Artifacts[1] != (productui.AgentArtifact{Kind: productui.AgentArtifactDraft}) {
		t.Fatalf("artifacts = %+v, want the attached document with its link and the private draft; the revoked entry is not listed", task.Artifacts)
	}
	if len(task.Approvals) != 1 {
		t.Fatalf("approvals = %+v", task.Approvals)
	}
	approval := task.Approvals[0]
	if approval.ID != "task-promote/submit" || approval.Digest != "sha256:approve-submit" || approval.Summary != "people.promote_worker" {
		t.Fatalf("approval = %+v", approval)
	}
	if got, want := strings.Join(approval.Sources, "|"), "document:Calibration notes|records|request"; got != want {
		t.Errorf("approval sources = %q, want %q", got, want)
	}
	if got, want := approval.Taint, "AGENT_DERIVED,USER_AUTHORED,UNTRUSTED_PEER"; got != want {
		t.Errorf("approval labels = %q, want %q", got, want)
	}
	if len(task.SubmittedIntents) != 1 || task.SubmittedIntents[0] != (productui.AgentIntentStatus{Name: "people.promote_worker", Status: productui.AgentIntentAwaitingApproval}) {
		t.Fatalf("submitted intents = %+v", task.SubmittedIntents)
	}
	if task.Goal != agent2017Task().Goal || task.Title != "Prepare the promotion request for Ana Flores" {
		t.Fatalf("goal %q, title %q", task.Goal, task.Title)
	}

	// The submission's status follows the record.
	for name, tc := range map[string]struct {
		change func(*agentrun.AgentTask)
		want   string
	}{
		"not reached":      {func(task *agentrun.AgentTask) { task.Plan.Steps[2].State = agentrun.StepPending }, productui.AgentIntentDraft},
		"running":          {func(task *agentrun.AgentTask) { task.Plan.Steps[2].State = agentrun.StepRunning }, productui.AgentIntentExecuting},
		"sent, unverified": {func(task *agentrun.AgentTask) { task.Plan.Steps[2].State = agentrun.StepCompleted }, productui.AgentIntentExecuting},
		"sent, verified": {func(task *agentrun.AgentTask) {
			task.Plan.Steps[2].State, task.Plan.Steps[3].State = agentrun.StepCompleted, agentrun.StepCompleted
		}, productui.AgentIntentObserved},
		"failed":    {func(task *agentrun.AgentTask) { task.Plan.Steps[2].State = agentrun.StepFailed }, productui.AgentIntentFailed},
		"ambiguous": {func(task *agentrun.AgentTask) { task.FailureCode = "AMBIGUOUS_EFFECT" }, productui.AgentIntentNeedsRepair},
	} {
		stored := agent2017Task()
		tc.change(&stored)
		if got := taskSubmittedIntents(stored); len(got) != 1 || got[0].Status != tc.want {
			t.Errorf("%s: submission status = %+v, want %q", name, got, tc.want)
		}
	}
}

// TestTodo_AGENT2_017_Browser renders the served task as its owner's page in
// three languages: each section is present, said in the reader's language,
// and the controls are one click each.
func TestTodo_AGENT2_017_Browser(t *testing.T) {
	client := NewWithControls(fixedRunners{reader: policyReaderFixture{leakyReader{tasks: []agentrun.AgentTask{agent2017Task()}}}}, fixedAgentSetting{enabled: true}, inertController{})
	snapshot, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil {
		t.Fatal(err)
	}
	for locale, want := range map[string][]string{
		"en-US": {"You confirmed the plan", "Finished: Read handbook", "Finished: Wrote a draft only you can see", "Picked up again", "Stopped to wait for your approval",
			"Calibration notes", "Private draft", "Kept with this task", "Approval reference", "Document: Calibration notes", "Records the agent read for you", "Your request",
			"Written by the agent", "Written by you", "From other people's messages, not verified", "Waiting for your approval", "Use the calibration notes."},
		"de-DE": {"Sie haben den Plan bestätigt", "Wieder aufgenommen", "Angehalten, bis Sie zustimmen", "Privater Entwurf", "Zustimmungsreferenz", "Dokument: Calibration notes", "Vom Agenten geschrieben", "Wartet auf Ihre Zustimmung"},
		"ar":    {"أكّدتَ الخطة", "استؤنفت", "توقفت بانتظار موافقتك", "مسودة خاصة", "مرجع الموافقة", "مستند: Calibration notes", "كتبه الوكيل", "بانتظار موافقتك"},
	} {
		page := agent2017Page(t, snapshot, "hc-051-linh-tran", "task-promote", locale)
		for _, text := range want {
			if !strings.Contains(page, text) {
				t.Errorf("%s: the task view does not say %q", locale, text)
			}
		}
		for _, markup := range []string{
			`data-task-view="task-promote"`, `data-detail="agents.checkpoints"`, `data-detail="agents.artifacts"`, `data-detail="agents.approvals"`, `data-detail="agents.submitted_intents"`,
			`href="/workspace/app/docs?document=doc-calibration"`, `data-approval-id="task-promote/submit"`, `sha256:approve-submit`,
			`data-task-action="pause"`, `data-task-action="cancel"`, `data-task-version="7"`,
		} {
			if !strings.Contains(page, markup) {
				t.Errorf("%s: the task view is missing %s", locale, markup)
			}
		}
		// No code the server sent is shown as text.
		for _, code := range []string{">plan_confirmed<", ">step_finished<", ">waiting_approval<", ">awaiting_approval<", "AGENT_DERIVED", "UNTRUSTED_PEER", ">records<", "document:Calibration"} {
			if strings.Contains(page, code) {
				t.Errorf("%s: the task view shows the code %q", locale, code)
			}
		}
	}
}

// policyReaderFixture lets the fixture task offer its controls: the page
// shows a control only when the server's policy allows it.
type policyReaderFixture struct{ leakyReader }

func (policyReaderFixture) TaskPolicy(context.Context, agentrun.AgentTask, string) agentsystem.TaskActionPolicy {
	return agentsystem.TaskActionPolicy{Pause: true, Cancel: true}
}

// TestTodo_AGENT2_017_Security holds identifiers, references and another
// person's task in the store and proves none of it reaches the owner's
// snapshot, the page document or the page.
func TestTodo_AGENT2_017_Security(t *testing.T) {
	theirs := agent2017Task()
	theirs.ID, theirs.UserID, theirs.Goal = "task-theirs", "hc-050-rafael-torres", "Rafael private goal"
	client := New(fixedRunners{reader: leakyReader{tasks: []agentrun.AgentTask{agent2017Task(), theirs}}})
	snapshot, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	page := agent2017Page(t, snapshot, "hc-051-linh-tran", "task-promote", "en-US")
	for _, forbidden := range []string{
		"secret", "other-tenant", "salary-band", "worker:42", "result:", "draft:draft", "chat:peer", "queue:", "owner:verify", "approval:wake", "signal:key",
		"version:3", "agent-document-usage", "task-theirs", "Rafael private goal", "hc-050-rafael-torres", "report:",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the snapshot carries %q: %s", forbidden, encoded)
		}
		if strings.Contains(page, forbidden) {
			t.Errorf("the page shows %q", forbidden)
		}
	}
	// The other person's task opens nothing for this owner, by id or by link.
	if other := agent2017Page(t, snapshot, "hc-051-linh-tran", "task-theirs", "en-US"); strings.Contains(other, `data-task-view=`) || strings.Contains(other, "Approval reference") {
		t.Errorf("asking for another person's task opened a task view")
	}
	// The other person reads their own, and not this one.
	rafael, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-050-rafael-torres"})
	if err != nil || len(rafael.Tasks) != 1 || rafael.Tasks[0].ID != "task-theirs" {
		t.Fatalf("the other owner's snapshot = %+v, %v", rafael.Tasks, err)
	}
	// A reader that is not the owner gets no plan history from the runtime.
	platform := newPlatform(t)
	startTask(t, platform, tenantKey, "task-linh", "hc-051-linh-tran", "Summarise my open onboarding requests", true)
	runner, err := platform.ForTenant(context.Background(), tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := runner.PreviousPlan(context.Background(), "task-linh", "hc-050-rafael-torres"); err == nil || found {
		t.Errorf("another person read the plan history: found=%v err=%v", found, err)
	}
}

// TestTodo_AGENT2_017_PlanRevision replans a real task so the new revision
// needs the owner's confirmation, and checks the served view names what the
// revision adds and drops, next to the control that confirms it.
func TestTodo_AGENT2_017_PlanRevision(t *testing.T) {
	ctx := context.Background()
	platform := newPlatform(t)
	startTask(t, platform, tenantKey, "task-linh", "hc-051-linh-tran", "Summarise my open onboarding requests", true)
	runner, err := platform.ForTenant(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	client := FromPlatformWithControls(platform, fixedAgentSetting{enabled: true}, inertController{})
	before, err := client.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil || len(before.Tasks) != 1 || len(before.Tasks[0].PlanChanges) != 0 {
		t.Fatalf("a task on its first plan reports changes: %+v, %v", before.Tasks, err)
	}
	if _, found, err := runner.PreviousPlan(ctx, "task-linh", "hc-051-linh-tran"); err != nil || found {
		t.Fatalf("a first plan has a previous plan: found=%v err=%v", found, err)
	}
	stored, err := runner.Runtime.GetTask(ctx, "task-linh")
	if err != nil {
		t.Fatal(err)
	}
	// The new plan drops the lookup and reads the handbook instead. That is a
	// skill the confirmed plan did not hold, so the runtime will not run it
	// until the owner confirms the revision.
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{
		{ID: "handbook", Type: agentrun.StepRead, SkillID: "agent.read_handbook", SkillVersion: 1, ExpectedOutput: "policy text", Tier: agentrun.TierRead, Inputs: []agentrun.InputRef{{Name: "query", Ref: "task:task-linh:query-ref-secret"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	replanned, err := runner.Runtime.Replan(ctx, stored.ID, stored.Version, plan, fixedNow.Add(time.Minute))
	if err != nil || replanned.State != agentrun.StateAwaitingPlanConfirmation || replanned.Plan.Revision != 2 {
		t.Fatalf("replan = %+v, %v", replanned, err)
	}
	previous, found, err := runner.PreviousPlan(ctx, "task-linh", "hc-051-linh-tran")
	if err != nil || !found || previous.Revision != 1 || len(previous.Steps) != 1 || previous.Steps[0].SkillID != "skill.lookup" {
		t.Fatalf("previous plan = %+v found=%v err=%v", previous, found, err)
	}
	after, err := client.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil || len(after.Tasks) != 1 {
		t.Fatalf("snapshot = %+v, %v", after, err)
	}
	task := after.Tasks[0]
	if len(task.PlanChanges) != 2 ||
		task.PlanChanges[0].Change != productui.AgentPlanChangeAdded || task.PlanChanges[0].Step.Name != "agent.read_handbook" || task.PlanChanges[0].Step.Tier != "T0" ||
		task.PlanChanges[1].Change != productui.AgentPlanChangeRemoved || task.PlanChanges[1].Step.Name != "skill.lookup" {
		t.Fatalf("plan changes = %+v", task.PlanChanges)
	}
	if encoded, _ := json.Marshal(after); strings.Contains(string(encoded), "query-ref-secret") {
		t.Fatalf("a step input reference left the server: %s", encoded)
	}
	page := agent2017Page(t, after, "hc-051-linh-tran", "task-linh", "en-US")
	for _, want := range []string{"Plan revision 2", "What changed in this plan", "Added: Read handbook", "Removed: skill.lookup", "Review the changes, then start the plan to continue.", `data-task-action="confirm-plan"`, `data-plan-change="added"`, `data-plan-change="removed"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the revision view is missing %q", want)
		}
	}
	if changes, confirm := strings.Index(page, "What changed in this plan"), strings.Index(page, `data-task-action="confirm-plan"`); changes < 0 || confirm < 0 || changes > confirm {
		t.Errorf("the changes are not shown before the control that confirms them")
	}
}
