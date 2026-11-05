package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// TestTodo_AGENTP_025_Browser drives the document surface end to end through
// the served document service: a reader's comment mentioning a persona starts
// exactly one run through the shared AGENTP-008 resolver, a plain comment
// starts nothing, and the answer's audience is decided by the one AGENTP-012
// evaluator with the approvals of AGENTP-014. A render-level run through the
// document page awaits persona UI on that page; this test proves the journey
// the page will render at the same service boundary the RPC server calls.
func TestTodo_AGENTP_025_Browser(t *testing.T) {
	harness, _ := newSurfaceHarness(t)
	svc := withDocumentPersonaMentions(documentServiceFixture(t), harness.bridge).(documentService)

	const tenant, owner, reader = "tenant-a", "owner", "alice"
	ctx := context.Background()
	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Leave policy", "# Leave policy\n\nSubmit requests two weeks ahead.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, id, reader, ""); err != nil {
		t.Fatal(err)
	}
	readerCtx := trust.WithPrincipal(ctx, mustBridgePrincipal(t, reader, tenant))

	// A passage-anchored question: the quote anchors the comment to the
	// version under discussion; it is the normal case, not a quoted post.
	comment, err := svc.AddDocumentComment(readerCtx, tenant, reader, id, versionID,
		"@policy-helper how much notice do I need?", &transportdocument.CommentAnchor{Quote: "Submit requests two weeks ahead."}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(harness.runs.requests) != 1 {
		t.Fatalf("runs started = %d, want the one persona mention", len(harness.runs.requests))
	}
	run := harness.runs.requests[0]
	kind, resource, version, ok := parseSurfaceChannel(run.ConversationID)
	if !ok || kind != chatrecipient.SurfaceHubDocument || resource != id || version != versionID {
		t.Fatalf("run channel %q is not the viewed document version", run.ConversationID)
	}
	if run.InvokerID != reader || run.InvokingPostID != comment.ID || run.Mode != agentinvoke.OnBehalfOf {
		t.Fatalf("run is not the reader's on-behalf-of invocation: %+v", run)
	}
	// A comment without a persona mention starts nothing.
	if _, err := svc.AddDocumentComment(readerCtx, tenant, reader, id, versionID, "Thanks, this is clear.", nil, ""); err != nil {
		t.Fatal(err)
	}
	if len(harness.runs.requests) != 1 {
		t.Fatalf("runs started = %d, want no run for a plain comment", len(harness.runs.requests))
	}
	// The answer's audience is the document's readers. An owner who may not
	// read the cited part keeps the answer private to the invoker.
	authority := &surfaceFloorAuthority{
		snapshot: chatrecipient.AudienceSnapshot{TenantID: tenant, Surface: chatrecipient.SurfaceRef{TenantID: tenant, Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, Revision: 3}, Revision: 3,
			CurrentMembers: []chatrecipient.AudiencePrincipal{{TenantID: tenant, SubjectID: reader}, {TenantID: tenant, SubjectID: owner}}, Complete: true, GuestAndExternalComplete: true},
		denied: map[string]bool{owner: true},
	}
	decision := chatrecipient.EvaluateAudienceFloor(readerCtx, chatrecipient.AudienceFloorRequest{
		Surface:     &chatrecipient.SurfaceRef{TenantID: tenant, Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, Revision: 3},
		Disclosures: []chatrecipient.Disclosure{{SourceID: id, RecordID: versionID, Field: "section-2", CitationTitle: "Section 2", DataClass: dlp.ClassCompensation}},
	}, authority)
	if decision.Route != chatrecipient.RoutePrivate || decision.Reason != chatrecipient.AudienceFloorUnauthorized {
		t.Fatalf("document floor decision = %+v, want the private route", decision)
	}
	// A governed effect from the surface run raises the same AGENTP-014
	// card, bound to the surface invocation and decided in the task view.
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	approvals, err := agentapproval.New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval, err := approvals.Create(agentapproval.CreateRequest{
		ID: "approval-surface-doc", TenantID: tenant, TaskID: "task-surface-doc", AssignedUserID: reader, AgentID: "persona-policy",
		OriginChain: []agentapproval.ActorRef{{Kind: agentapproval.ActorAgent, ID: "persona-policy"}},
		Items: []agentapproval.ActionItem{{
			ID: "item-1", Kind: agentapproval.ItemGovernedIntent, Tier: agentapproval.TierSubmitGoverned,
			IntentDefinitionID: "PromoteWorker", IntentDefinitionVersion: "v3",
			Subjects:       []agentapproval.Subject{{ID: "worker-7", Kind: "worker"}},
			MaterialFields: []agentapproval.MaterialField{{Path: "job.level", Before: "L4", After: "L5"}},
			Sources:        []agentapproval.Source{{Ref: id + "/" + versionID, Taint: "TRUSTED_INTERNAL"}},
			Uncertainty:    "source is current as of the task checkpoint", RiskClass: "low",
		}},
		TaskExpiresAt: now.Add(48 * time.Hour), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	card, err := agentapproval.BuildPersonaChatCard(approval, run.InvocationID, reader, now)
	if err != nil || card.InvocationID != run.InvocationID || card.TaskViewHref == "" {
		t.Fatalf("surface approval card = %+v err=%v", card, err)
	}
	if _, err := agentapproval.ResolvePersonaChatCardCallback(card, agentapproval.PersonaChatCardCallback{Action: "approve", ActorID: reader, InvocationID: card.InvocationID, Now: now, Current: approval}); !errors.Is(err, agentapproval.ErrChatApprovalSurface) {
		t.Fatalf("surface card decided off the task view: %v", err)
	}
}
