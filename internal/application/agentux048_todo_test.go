package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

// s15Proof names one existing proof that a todo's test cites. The todo tests of
// lane S15 are the clause tables of AGENTUX-048..070: each runs the proofs that
// carry its clauses, so a clause that loses its test fails under the todo's name.
type s15Proof struct {
	name string
	run  func(*testing.T)
}

func s15Run(t *testing.T, proofs ...s15Proof) {
	t.Helper()
	for _, proof := range proofs {
		t.Run(proof.name, proof.run)
	}
}

// TestTodo_AGENTUX_048 covers the owner's side of an agent that posts first:
// the schedule (time zone, weekdays, month day), the exact-message preview that
// posts nothing, post now and a saved schedule, idempotent occurrences, pause,
// and an occurrence that is refused with a plain reason when a cited document is
// not readable by everyone in the conversation.
func TestTodo_AGENTUX_048(t *testing.T) {
	s15Run(t,
		s15Proof{"schedule math", TestAgentUXProactive_ScheduleMath},
		s15Proof{"occurrence is idempotent", TestAgentUXProactive_IdempotentOccurrence},
		s15Proof{"controls and replay", TestAgentUXProactive_ControlsAndReplay},
		s15Proof{"preview posts nothing and refusal is plain", TestAgentUXProactive_PreviewRefusalAndPause},
		s15Proof{"preview matches the post", TestAgentUXProactive_PreviewMatchesPost},
		s15Proof{"concurrent replay posts once", TestAgentUXProactive_ConcurrentOccurrenceReplay},
		s15Proof{"native schedule pause and misfire", TestAgentUXProactive_NativeSchedulePauseAndMisfire},
		s15Proof{"upcoming holidays with a fake model", TestAgentUXProactiveLive_PostedMessage},
	)
}

// TestTodo_AGENTUX_048_Security: a member who cannot read a document, a
// placement that was revoked, a member added later and a document of another
// tenant never produce a public post; the service identity and the requester are
// bound to the tenant; the model sees no personal context.
func TestTodo_AGENTUX_048_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"public only when every member may read every source", TestAgentUXProactive_PublicRule_Security},
		s15Proof{"sponsored body class", TestAgentUXProactive_SponsoredBodyClass_Security},
		s15Proof{"mixed document classes", TestAgentUXProactive_MixedDocumentClasses_Security},
		s15Proof{"owner must read the documents", TestAgentUXProactive_PreviewRequiresOwnerDocumentAccess_Security},
		s15Proof{"withdrawn placement refuses the occurrence", TestAgentUXProactive_WithdrawnPlacementRefusesOccurrence_Security},
		s15Proof{"owner and HTTP admission", TestAgentUXProactive_OwnerAndHTTPAdmission_Security},
		s15Proof{"another tenant's names", TestAgentUXProactive_CatalogNames_Security},
		s15Proof{"citation binding", TestAgentUXProactive_CitationBinding_Security},
		s15Proof{"sealed public sources", TestAgentUXProactive_SealedPublicSources_Security},
		s15Proof{"requester and tenant binding", TestAgentUXProactive_RequesterAndTenantBinding_Security},
		s15Proof{"prompt has no personal context", TestAgentUXProactive_PromptHasNoPersonalContext_Security},
		s15Proof{"a member added later", TestAgentUXProactive_MemberAddedAfterPublicReply_Security_Integration},
		s15Proof{"a failed edit restores the document scope", TestAgentUXProactive_FailedEditRestoresDocumentScope_Security},
	)
}

// TestTodo_AGENTUX_048_Integration runs the served path against the test
// database with a fake model: post now, preview then post, the weekly tick,
// pause before a tick, a queued occurrence that is paused, the failure
// projection and the document authority.
func TestTodo_AGENTUX_048_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"post now", TestAgentUXProactive_PostNow_Integration},
		s15Proof{"preview then post", TestAgentUXProactive_PreviewPost_Integration},
		s15Proof{"weekly tick", TestAgentUXProactive_WeeklyTick_Integration},
		s15Proof{"pause before the tick", TestAgentUXProactive_PauseBeforeTick_Integration},
		s15Proof{"pause a queued occurrence", TestAgentUXProactive_PauseQueuedOccurrence_Integration},
		s15Proof{"failure projection", TestAgentUXProactive_FailureProjection_Integration},
		s15Proof{"document authority", TestAgentUXProactive_DocumentAuthority_Integration},
		s15Proof{"preview and post through the served assembly", TestAgentUXProactiveLive_PreviewAndPost_Integration},
	)
}

// TestTodo_AGENTUX_048_OwnerIsNamed pins what the page shows beside a saved
// schedule and what the post is made with: the owner who set it, by name, and
// one post for one Post now however often the command is sent.
func TestTodo_AGENTUX_048_OwnerIsNamed(t *testing.T) {
	s, _, _, _, _, delivery, draft := proactiveControls(t)
	ctx := context.Background()
	if _, err := s.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	reply, err := s.ListAnnouncements(ctx)
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].OwnerName != "Alex Example" {
		t.Fatalf("the list does not name who set the schedule: %+v %v", reply, err)
	}
	row := reply.Snapshot.Rows[0]
	for range 2 {
		if _, err := s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, Action: "POST_NOW", ExpectedRevision: row.Revision, IdempotencyKey: "post-now-048a"}); err != nil {
			t.Fatal(err)
		}
	}
	if delivery.calls != 1 {
		t.Fatalf("Post now sent twice posted %d messages", delivery.calls)
	}
}
