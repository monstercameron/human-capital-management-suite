package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type stage3728Links struct {
	rows []chatstore.WorkLink
	err  error
	asks []string
}

func (l *stage3728Links) ListWorkLinksBySource(_ context.Context, tenant, kind, source string, _ int) ([]chatstore.WorkLink, error) {
	l.asks = append(l.asks, tenant+"|"+kind+"|"+source)
	return l.rows, l.err
}

type stage3728Chat struct {
	posts []chat.JourneyStagePost
	fail  map[string]error
}

func (c *stage3728Chat) PostJourneyStage(_ context.Context, r chat.JourneyStagePost) (chat.Post, error) {
	if err := c.fail[r.ConversationID]; err != nil {
		return chat.Post{}, err
	}
	c.posts = append(c.posts, r)
	return chat.Post{ID: "p"}, nil
}

func stage3728Rows(conversations ...string) []chatstore.WorkLink {
	out := make([]chatstore.WorkLink, 0, len(conversations))
	for _, c := range conversations {
		out = append(out, chatstore.WorkLink{TenantID: "t1", ConversationID: c, Kind: chatstore.WorkLinkJourneyShare, SourceID: "j1", ProposedByID: "u1"})
	}
	return out
}

func TestIssue3728_DecisionIsAnnouncedInEveryConversationTheJourneyWasSharedInto_Integration(t *testing.T) {
	links := &stage3728Links{rows: stage3728Rows("c1", "c2", "c3")}
	poster := &stage3728Chat{fail: map[string]error{"c2": chat.ErrNotFound}}
	a := &journeyStageAnnouncer{Chat: poster, Links: links}
	ctx := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "approver"))
	a.AnnounceJourneyDecision(ctx, workspace.JourneyDecisionEvent{IntentID: "j1", Approve: true, Stage: workspace.JourneyStageManagerApproval, ProposalRevisionID: "rev-1"})
	// One conversation is gone; the others still hear about it.
	if len(poster.posts) != 2 || poster.posts[0].ConversationID != "c1" || poster.posts[1].ConversationID != "c3" {
		t.Fatalf("posts %+v", poster.posts)
	}
	got := poster.posts[0]
	if got.TenantID != "t1" || got.JourneyID != "j1" || got.Outcome != chat.JourneyStageApproved || got.Decider.SubjectID != "approver" || got.Decider.TenantID != "t1" {
		t.Fatalf("post %+v", got)
	}
	if got.IdempotencyKey != "journey-stage:j1:approved:MANAGER_APPROVAL:rev-1" {
		t.Fatalf("key %q", got.IdempotencyKey)
	}
	if len(links.asks) != 1 || links.asks[0] != "t1|JOURNEY_SHARE|j1" {
		t.Fatalf("asked %v", links.asks)
	}
}

func TestIssue3728_OutcomeIsMappedFromTheDecisionAndTheStageReached(t *testing.T) {
	for _, tc := range []struct {
		approve bool
		stage   workspace.JourneyStage
		want    string
	}{
		{true, workspace.JourneyStageFinanceApproval, chat.JourneyStageApproved},
		{true, workspace.JourneyStageCompleted, chat.JourneyStageApproved},
		{false, workspace.JourneyStageRejected, chat.JourneyStageRejected},
		{true, workspace.JourneyStageBlocked, chat.JourneyStageBlocked},
		{true, workspace.JourneyStageRepairRequired, chat.JourneyStageBlocked},
		{true, workspace.JourneyStageFailed, chat.JourneyStageBlocked},
		{false, workspace.JourneyStageAwaitingApproval, chat.JourneyStageRejected},
	} {
		if got := journeyStageOutcome(workspace.JourneyDecisionEvent{Approve: tc.approve, Stage: tc.stage}); got != tc.want {
			t.Errorf("approve=%v stage=%s: %q, want %q", tc.approve, tc.stage, got, tc.want)
		}
	}
}

// Nothing is announced without a verified person who decided, and a failing
// link read or a panicking poster never escapes into the decision.
func TestIssue3728_AnnouncerFailsQuietlyAndNeverWithoutTheDecider_Security(t *testing.T) {
	links := &stage3728Links{rows: stage3728Rows("c1")}
	poster := &stage3728Chat{}
	a := &journeyStageAnnouncer{Chat: poster, Links: links}
	e := workspace.JourneyDecisionEvent{IntentID: "j1", Approve: true, Stage: workspace.JourneyStageCompleted}
	a.AnnounceJourneyDecision(context.Background(), e)
	if len(poster.posts) != 0 || len(links.asks) != 0 {
		t.Fatalf("announced with no decider: %+v %v", poster.posts, links.asks)
	}
	// An agent is not a person who decides.
	agent, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "t1", Subject: "agent-1", SubjectKind: trust.SubjectKindAgent, Roles: []string{"hcm_admin"}, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "s", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "sha256:x"})
	if err != nil {
		t.Fatal(err)
	}
	a.AnnounceJourneyDecision(trust.WithPrincipal(context.Background(), agent), e)
	if len(poster.posts) != 0 {
		t.Fatal("an agent decision was announced as a person decision")
	}
	ctx := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "approver"))
	links.err = errors.New("store down")
	a.AnnounceJourneyDecision(ctx, e)
	links.err = nil
	a.Chat = panicChat{}
	a.AnnounceJourneyDecision(ctx, e)
	// A journey nobody shared announces nothing, and a blank id never reads links.
	links.rows = nil
	a.Chat = poster
	a.AnnounceJourneyDecision(ctx, e)
	before := len(links.asks)
	a.AnnounceJourneyDecision(ctx, workspace.JourneyDecisionEvent{IntentID: " ", Approve: true})
	if len(poster.posts) != 0 || len(links.asks) != before {
		t.Fatalf("posts %+v asks %v", poster.posts, links.asks)
	}
	var nilAnnouncer *journeyStageAnnouncer
	nilAnnouncer.AnnounceJourneyDecision(ctx, e)
}

type panicChat struct{}

func (panicChat) PostJourneyStage(context.Context, chat.JourneyStagePost) (chat.Post, error) {
	panic("chat exploded")
}

// The store the served chat service runs on can write a decision line; if a
// wrapper ever hid the capability the announcement would fail closed and quiet.
var (
	_ chat.SystemLineWriter = (*chatstore.ModeratedAdapter)(nil)
	_ chat.SystemLineWriter = (*chatstore.RenderingAdapter)(nil)
	_ journeyStageLinks     = (*chatstore.Store)(nil)
	_ journeyStageChat      = (*chat.Service)(nil)
)
