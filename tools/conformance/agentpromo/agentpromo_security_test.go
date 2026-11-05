package agentpromo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// TestTodo_AGENT2_024_Security proves the journey boundaries refuse the
// hostile cases: the injected instruction, the non-report subject, a foreign
// approver, an agent decision, a tampered digest, a revoked grant and a
// replayed token.
func TestTodo_AGENT2_024_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("injected instruction never reaches planning or write arguments", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		raw, err := json.Marshal(world.planning)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), promoInjectionMarker) {
			t.Fatalf("planning context kept the injection: %s", raw)
		}
		for _, value := range world.extract.Values {
			if len(value.Taint) == 0 {
				t.Fatalf("extraction value %q lost its taint", value.Name)
			}
		}
		destination := []agentsecurity.WriteArgument{{
			Name: "destination:channel", Value: "#salaries", Role: agentsecurity.WriteDestination,
			Taint:      []agentsecurity.TaintLabel{agentsecurity.TaintExternal},
			Provenance: []string{promoDocID},
			Citations:  []agentsecurity.Citation{{SourceID: promoDocID, Location: "body:9", Digest: promoContentDigest(promoCalibrationDoc())}},
		}}
		card, err := agentsecurity.BuildWriteApprovalCard(destination)
		if err != nil {
			t.Fatal(err)
		}
		_, err = agentsecurity.BindWriteArguments(ctx, agentsecurity.TierSubmitGoverned, destination, &card, world.owner)
		var refusal *agentsecurity.Refusal
		if !errors.As(err, &refusal) || refusal.Code != agentsecurity.RefusalAuthorityExpansion {
			t.Fatalf("tainted destination bind=%v, want the egress refusal", err)
		}
	})

	t.Run("non-report never reaches the batch card or the journal", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.driveFull()
		for _, item := range world.batch.Items {
			for _, subject := range item.Subjects {
				if subject.ID == promoWorkerJane {
					t.Fatalf("batch lists the non-report: %+v", world.batch.Items)
				}
			}
		}
		for _, arg := range world.submitArgs() {
			if arg.Value == promoWorkerJane {
				t.Fatalf("write arguments carry the non-report: %+v", world.submitArgs())
			}
		}
		for _, intent := range world.journal.Intents {
			if intent.Subject == promoWorkerJane {
				t.Fatalf("journal submitted the non-report: %+v", world.journal.Intents)
			}
		}
	})

	t.Run("only the manager approves in the product surface", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		batch := world.createBatch("approval-hostile-1")
		presented := []string{batch.Items[0].ID, batch.Items[1].ID}
		origin := []agentapproval.ActorRef{{Kind: agentapproval.ActorHuman, ID: promoManager}}

		other := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoHR, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: batch.Digest, PresentedItemIDs: presented, DecisionOrigin: []agentapproval.ActorRef{{Kind: agentapproval.ActorHuman, ID: promoHR}}, StepUpProof: world.stepupProof(batch.ID, batch.Digest)}
		if err := world.approvals.Approve(ctx, other, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrUnauthorized) {
			t.Fatalf("foreign approval=%v, want ErrUnauthorized", err)
		}
		agent := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: batch.Digest, PresentedItemIDs: presented, DecisionOrigin: []agentapproval.ActorRef{{Kind: agentapproval.ActorAgent, ID: promoAgentID}}, StepUpProof: world.stepupProof(batch.ID, batch.Digest)}
		if err := world.approvals.Approve(ctx, agent, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrAgentCannotApprove) {
			t.Fatalf("agent approval=%v, want ErrAgentCannotApprove", err)
		}
		chat := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: "CHAT_TEXT", ExpectedDigest: batch.Digest, PresentedItemIDs: presented, DecisionOrigin: origin, StepUpProof: world.stepupProof(batch.ID, batch.Digest)}
		if err := world.approvals.Approve(ctx, chat, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrWrongSurface) {
			t.Fatalf("chat approval=%v, want ErrWrongSurface", err)
		}
		hidden := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: batch.Digest, PresentedItemIDs: presented[:1], DecisionOrigin: origin, StepUpProof: world.stepupProof(batch.ID, batch.Digest)}
		if err := world.approvals.Approve(ctx, hidden, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrDigestMismatch) {
			t.Fatalf("hidden-item approval=%v, want ErrDigestMismatch", err)
		}
		tampered := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: "sha256:tampered", PresentedItemIDs: presented, DecisionOrigin: origin, StepUpProof: world.stepupProof(batch.ID, batch.Digest)}
		if err := world.approvals.Approve(ctx, tampered, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrDigestMismatch) {
			t.Fatalf("tampered approval=%v, want ErrDigestMismatch", err)
		}
		nostep := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: batch.Digest, PresentedItemIDs: presented, DecisionOrigin: origin}
		if err := world.approvals.Approve(ctx, nostep, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrStepUpRequired) {
			t.Fatalf("approval without step-up=%v, want ErrStepUpRequired", err)
		}
		forged := world.stepupProof(batch.ID, batch.Digest)
		forged.Signature = []byte("forged-signature")
		withForged := agentapproval.ApproveRequest{ApprovalID: batch.ID, UserID: promoManager, Surface: agentapproval.SurfaceProductTaskView, ExpectedDigest: batch.Digest, PresentedItemIDs: presented, DecisionOrigin: origin, StepUpProof: forged}
		if err := world.approvals.Approve(ctx, withForged, world.stepupVerifier()); !errors.Is(err, agentapproval.ErrStepUpRequired) {
			t.Fatalf("forged step-up approval=%v, want ErrStepUpRequired", err)
		}
		if stored, err := world.approvals.Get(batch.ID); err != nil || stored.State != agentapproval.StatePending {
			t.Fatalf("hostile batch=%+v err=%v, want it still pending", stored, err)
		}
	})

	t.Run("revocation stops exchange and wakes", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		world.runApprovalPhase()
		world.runSubmitPhase()
		world.authority.deactivate()
		_, err := world.delegate.Exchange(agentdelegation.ExchangeRequest{
			SubjectToken: world.grant.GrantID, SubjectTokenType: agentdelegation.DelegationGrantTokenType,
			RunID: world.taskID, StepID: "wait-decision", Skill: promoSkillSubmit,
			Scope: []string{promoCapability}, Audience: promoAudience, Sender: promoSender,
		})
		if !errors.Is(err, agentdelegation.ErrUserInactive) {
			t.Fatalf("exchange after deactivation=%v, want ErrUserInactive", err)
		}
		if err := world.delegate.RevokeGrant(world.grant.GrantID, "manager departed"); err != nil {
			t.Fatal(err)
		}
		_, err = world.delegate.Exchange(agentdelegation.ExchangeRequest{
			SubjectToken: world.grant.GrantID, SubjectTokenType: agentdelegation.DelegationGrantTokenType,
			RunID: world.taskID, StepID: "wait-decision", Skill: promoSkillSubmit,
			Scope: []string{promoCapability}, Audience: promoAudience, Sender: promoSender,
		})
		if !errors.Is(err, agentdelegation.ErrGrantRevoked) && !errors.Is(err, agentdelegation.ErrUserInactive) {
			t.Fatalf("exchange after revoke=%v, want the grant refused", err)
		}
	})

	t.Run("delegated token replays nowhere else", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		credential := world.exchangeCredential(world.taskID, "read-doc")
		if _, err := world.delegate.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: "foreign-server", Sender: promoSender, At: world.now}); !errors.Is(err, agentdelegation.ErrTokenAudienceMismatch) {
			t.Fatalf("foreign verify=%v, want ErrTokenAudienceMismatch", err)
		}
		if _, err := world.delegate.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: promoAudience, Sender: "workload/impostor", At: world.now}); !errors.Is(err, agentdelegation.ErrTokenSenderMismatch) {
			t.Fatalf("impostor verify=%v, want ErrTokenSenderMismatch", err)
		}
		claims, err := world.delegate.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: promoAudience, Sender: promoSender, At: world.now})
		if err != nil || claims.Subject != promoManager || claims.Actor.RunID != world.taskID {
			t.Fatalf("home verify=%+v err=%v, want the manager subject and the journey run", claims, err)
		}
	})

	t.Run("unapproved submit step never executes", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		world.runApprovalPhase()
		intents := len(world.journal.Intents)
		if _, err := world.runtime.ExecuteNext(ctx, world.taskID, world.get().Version, world, world, world.beat()); !errors.Is(err, agentrun.ErrApprovalRequired) {
			t.Fatalf("unapproved submit=%v, want ErrApprovalRequired", err)
		}
		if len(world.journal.Intents) != intents {
			t.Fatalf("the parked submit step submitted: %+v", world.journal.Intents)
		}
	})
}
