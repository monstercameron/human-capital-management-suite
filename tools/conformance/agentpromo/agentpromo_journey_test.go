package agentpromo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
)

// TestTodo_AGENT2_024 walks the reference promotion journey: a manager asks
// the agent to prepare promotions for their team from a calibration
// document, and every effect stays inside the manager's team, fields and
// approval path.
func TestTodo_AGENT2_024(t *testing.T) {
	ctx := context.Background()
	world := newPromoWorld(t, promoTask, promoDirectory(t))

	t.Run("manager confirms the ten-step promotion plan", func(t *testing.T) {
		task := world.get()
		if task.State != agentrun.StateRunning || !task.Plan.Confirmed || task.Plan.ConfirmedBy != promoManager {
			t.Fatalf("task state=%s confirmed=%v by=%q", task.State, task.Plan.Confirmed, task.Plan.ConfirmedBy)
		}
		want := []struct {
			id   string
			typ  agentrun.StepType
			tier agentrun.Tier
		}{
			{"read-doc", agentrun.StepRead, agentrun.TierRead},
			{"resolve-reports", agentrun.StepRead, agentrun.TierRead},
			{"read-bands", agentrun.StepRead, agentrun.TierRead},
			{"simulate", agentrun.StepAnalyze, agentrun.TierRead},
			{"draft", agentrun.StepDraft, agentrun.TierPrivateDraft},
			{"verify-drafts", agentrun.StepVerify, agentrun.TierRead},
			{"request-approval", agentrun.StepAskUser, agentrun.TierPrivateDraft},
			{"submit", agentrun.StepSubmit, agentrun.TierSubmitGoverned},
			{"wait-decision", agentrun.StepWait, agentrun.TierRead},
			{"report", agentrun.StepCommunicate, agentrun.TierCommunicate},
		}
		if len(task.Plan.Steps) != len(want) {
			t.Fatalf("plan steps=%d, want %d", len(task.Plan.Steps), len(want))
		}
		for i, step := range task.Plan.Steps {
			if step.ID != want[i].id || step.Type != want[i].typ || step.Tier != want[i].tier {
				t.Fatalf("step %d=%+v, want %s/%s/T%d", i, step, want[i].id, want[i].typ, want[i].tier)
			}
		}
		if !strings.HasPrefix(task.Plan.Digest, "sha256:") {
			t.Fatalf("plan digest=%q is not pinned", task.Plan.Digest)
		}
	})

	t.Run("read and resolve refuse the non-report and the injected instruction", func(t *testing.T) {
		world.runReadPhase()
		if len(world.extract.Values) != 1 || world.extract.Values[0].Name != "candidates" {
			t.Fatalf("extraction=%+v, want the single typed candidates field", world.extract.Values)
		}
		value := world.extract.Values[0]
		if len(value.Citations) != 1 || value.Citations[0].SourceID != promoDocID {
			t.Fatalf("extraction is not cited to the calibration doc: %+v", value.Citations)
		}
		raw, err := json.Marshal(world.extract.Values)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), promoInjectionMarker) {
			t.Fatalf("the injected instruction crossed into the typed extraction: %s", raw)
		}
		for _, worker := range []string{promoWorkerOmar, promoWorkerNoor, promoWorkerJane} {
			if !strings.Contains(string(raw), worker) {
				t.Fatalf("extraction lost candidate %s: %s", worker, raw)
			}
		}
		planningRaw, err := json.Marshal(world.planning)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(planningRaw), promoInjectionMarker) {
			t.Fatalf("the injected instruction reached the planning context: %s", planningRaw)
		}
		if world.planning.UserGoal != promoGoal || world.planning.ConfirmedPlanDigest != world.task.Plan.Digest {
			t.Fatalf("planning context=%+v, want the user goal and the confirmed plan digest", world.planning)
		}
		injected, err := agentsecurity.DefaultInstructionDetector(promoCalibrationDoc())
		if err != nil || !injected {
			t.Fatalf("detector on the raw document=%v,%v, want a flagged injection", injected, err)
		}
		clean, err := agentsecurity.DefaultInstructionDetector(string(raw))
		if err != nil || clean {
			t.Fatalf("detector on the typed extraction=%v,%v, want no instruction", clean, err)
		}
		joined := strings.Join(world.journal.Refusals, "\n")
		if !strings.Contains(joined, promoWorkerJane) || !strings.Contains(joined, promoInjectionMarker) {
			t.Fatalf("refusal reasons do not name both the non-report and the injection: %q", joined)
		}
	})

	t.Run("bands simulate and drafts validate", func(t *testing.T) {
		world.runDraftPhase()
		for _, worker := range []string{promoWorkerOmar, promoWorkerNoor} {
			preflight, ok := world.preflight[worker]
			if !ok {
				t.Fatalf("no preflight for %s", worker)
			}
			if preflight.Status != promotion.StatusReady || len(preflight.Blocking()) != 0 {
				t.Fatalf("%s preflight=%s findings=%+v, want READY without blocking findings", worker, preflight.Status, preflight.Findings)
			}
			if preflight.IntentType != promotion.IntentType || preflight.IntentVersion != promotion.IntentVersion {
				t.Fatalf("%s intent=%s/%s, want %s/%s", worker, preflight.IntentType, preflight.IntentVersion, promotion.IntentType, promotion.IntentVersion)
			}
			if preflight.Band.State != rewards.BandResultEvaluated {
				t.Fatalf("%s band=%s, want an evaluated band read", worker, preflight.Band.State)
			}
		}
		if world.card.Digest == "" || len(world.card.Arguments) != 4 {
			t.Fatalf("write card=%+v, want four bound arguments with a digest", world.card)
		}
		if rebuilt := world.rebuildCard(); rebuilt.Digest != world.card.Digest {
			t.Fatalf("rebuilt card digest=%s, want the exact card digest %s", rebuilt.Digest, world.card.Digest)
		}
		stale := append(append([]agentsecurity.WriteArgument(nil), world.submitArgs()...), agentsecurity.WriteArgument{
			Name: "subject:" + promoWorkerJane, Value: promoWorkerJane, Role: agentsecurity.WriteSubject,
			Taint:      []agentsecurity.TaintLabel{agentsecurity.TaintExternal},
			Provenance: []string{promoDocID},
			Citations:  []agentsecurity.Citation{{SourceID: promoDocID, Location: "candidates:3", Digest: promoContentDigest(promoCalibrationDoc())}},
		})
		staleCard, err := agentsecurity.BuildWriteApprovalCard(stale)
		if err != nil {
			t.Fatal(err)
		}
		_, err = agentsecurity.BindWriteArguments(ctx, agentsecurity.TierSubmitGoverned, stale, &staleCard, world.owner)
		var refusal *agentsecurity.Refusal
		if !errors.As(err, &refusal) || refusal.Code != agentsecurity.RefusalAuthorityExpansion {
			t.Fatalf("stale bind=%v, want an AGENT_AUTHORITY_EXPANSION refusal for the non-report", err)
		}
	})

	t.Run("manager approves the listed batch with step-up", func(t *testing.T) {
		world.runApprovalPhase()
		batch, err := world.approvals.Get(world.batch.ID)
		if err != nil || batch.State != agentapproval.StateApproved {
			t.Fatalf("batch=%+v err=%v, want the approved card", batch, err)
		}
		if len(batch.Items) != 2 || batch.Items[0].ID != "item-noor-haddad" || batch.Items[1].ID != "item-omar-reyes" {
			t.Fatalf("batch items=%+v, want the two listed promotions in server order", batch.Items)
		}
		for _, item := range batch.Items {
			if item.Kind != agentapproval.ItemGovernedIntent || item.Tier != agentapproval.TierSubmitGoverned {
				t.Fatalf("item=%+v, want a governed T3 intent", item)
			}
			if item.IntentDefinitionID != promotion.IntentType || item.IntentDefinitionVersion != promotion.IntentVersion {
				t.Fatalf("item=%+v, want %s/%s", item, promotion.IntentType, promotion.IntentVersion)
			}
		}
		if agentapproval.Digest(batch) != batch.Digest {
			t.Fatal("the approved digest does not recompute from the listed batch")
		}
	})

	t.Run("submit carries the agent-for-manager chain exactly once", func(t *testing.T) {
		world.runSubmitPhase()
		if len(world.journal.Intents) != 2 {
			t.Fatalf("submitted intents=%+v, want exactly the two listed promotions", world.journal.Intents)
		}
		seen := map[string]bool{}
		for _, intent := range world.journal.Intents {
			seen[intent.Subject] = true
			if intent.IntentType != promotion.IntentType || intent.IntentVersion != promotion.IntentVersion {
				t.Fatalf("intent=%+v, want %s/%s", intent, promotion.IntentType, promotion.IntentVersion)
			}
			if intent.Digest != world.batch.Digest || intent.IdempotencyKey != "promo-submit-1" {
				t.Fatalf("intent=%+v, want the batch digest and the journey idempotency key", intent)
			}
			if intent.ActorSubject != promoManager {
				t.Fatalf("intent=%+v, want the manager as the delegation subject", intent)
			}
			if intent.Actor.AgentVersion != promoAgentVersion || intent.Actor.InstallationID != promoInstall || intent.Actor.RunID != promoTask || intent.Actor.StepID != "submit" {
				t.Fatalf("intent actor=%+v, want the agent-for-manager run and step", intent.Actor)
			}
		}
		if !seen[promoWorkerOmar] || !seen[promoWorkerNoor] || seen[promoWorkerJane] {
			t.Fatalf("submitted subjects=%v, want only the two eligible reports", seen)
		}
	})

	t.Run("wait routes decides and reports owner-observed outcomes", func(t *testing.T) {
		world.runWaitPhase()
		world.runReportPhase()
		task := world.get()
		if task.State != agentrun.StateCompleted {
			t.Fatalf("task state=%s, want COMPLETED", task.State)
		}
		if len(world.journal.Decisions) != 1 {
			t.Fatalf("decisions=%+v, want the single observed HR decision", world.journal.Decisions)
		}
		decision := world.journal.Decisions[0]
		if decision.Approver != promoHR || decision.Decision != "APPROVE" {
			t.Fatalf("decision=%+v, want the HR approver's approval", decision)
		}
		for _, want := range []string{promoWorkerOmar, promoWorkerNoor, promoHR, promoWorkerJane, promoInjectionMarker} {
			if !strings.Contains(world.journal.Report, want) {
				t.Fatalf("report is missing %q:\n%s", want, world.journal.Report)
			}
		}
	})

	t.Run("second run drops the worker lost mid-task", func(t *testing.T) {
		second := newPromoWorld(t, promoTaskRun2, promoDirectory(t))
		second.runReadPhase()
		second.runDraftPhase()
		if _, ok := second.preflight[promoWorkerNoor]; !ok {
			t.Fatal("the second run never drafted the worker it later loses")
		}
		second.directory = promoDirectoryNoorMoved(t)
		second.owner.directory = second.directory
		second.runApprovalPhase()
		if len(second.batch.Items) != 1 || second.batch.Items[0].Subjects[0].ID != promoWorkerOmar {
			t.Fatalf("second batch=%+v, want only the remaining report", second.batch.Items)
		}
		second.runSubmitPhase()
		if len(second.journal.Intents) != 1 || second.journal.Intents[0].Subject != promoWorkerOmar {
			t.Fatalf("second intents=%+v, want only the remaining report submitted", second.journal.Intents)
		}
		if joined := strings.Join(second.journal.Drops, "\n"); !strings.Contains(joined, promoWorkerNoor) {
			t.Fatalf("second run recorded no visible drop: %q", joined)
		}
		stale := []agentsecurity.WriteArgument{{
			Name: "subject:" + promoWorkerNoor, Value: promoWorkerNoor, Role: agentsecurity.WriteSubject,
			Taint:      []agentsecurity.TaintLabel{agentsecurity.TaintExternal},
			Provenance: []string{promoDocID},
			Citations:  []agentsecurity.Citation{{SourceID: promoDocID, Location: "candidates:2", Digest: promoContentDigest(promoCalibrationDoc())}},
		}}
		staleCard, err := agentsecurity.BuildWriteApprovalCard(stale)
		if err != nil {
			t.Fatal(err)
		}
		_, err = agentsecurity.BindWriteArguments(ctx, agentsecurity.TierSubmitGoverned, stale, &staleCard, second.owner)
		var refusal *agentsecurity.Refusal
		if !errors.As(err, &refusal) || refusal.Code != agentsecurity.RefusalAuthorityExpansion {
			t.Fatalf("stale bind=%v, want the lost report refused before any submission", err)
		}
	})
}
