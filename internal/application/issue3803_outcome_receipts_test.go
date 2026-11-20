package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// issue3803Source is the conversation-wide outcome read the receipts come from.
type issue3803Source struct {
	*personaSurfaceInvocationsFixture
	invocations []agentinvocationstore.OutcomeInvocation
	refusals    []agentinvocationstore.OutcomeRefusal
	err         error
}

func (s *issue3803Source) ListConversationOutcomeInvocations(_ context.Context, tenant, room string) ([]agentinvocationstore.OutcomeInvocation, error) {
	out := []agentinvocationstore.OutcomeInvocation{}
	for _, row := range s.invocations {
		if row.TenantID == tenant && row.ConversationID == room {
			out = append(out, row)
		}
	}
	return out, s.err
}

func (s *issue3803Source) ListConversationRefusals(_ context.Context, tenant, room string) ([]agentinvocationstore.OutcomeRefusal, error) {
	out := []agentinvocationstore.OutcomeRefusal{}
	for _, row := range s.refusals {
		if row.TenantID == tenant && row.ConversationID == room {
			out = append(out, row)
		}
	}
	return out, s.err
}

// issue3803Runs is a run store that holds one run per run identity.
type issue3803Runs struct{ runs map[string]runstate.Run }

func (s *issue3803Runs) Get(_ context.Context, id string) (runstate.Run, error) {
	if run, ok := s.runs[id]; ok {
		return run, nil
	}
	return runstate.Run{}, agentrunstate.ErrNotFound
}
func (s *issue3803Runs) Create(context.Context, runstate.Run) error       { return nil }
func (s *issue3803Runs) Save(context.Context, runstate.Run, uint64) error { return nil }

// issue3803Posts is the audience check: the posts it hides are the ones a
// restricted audience leaves the reader out of.
type issue3803Posts struct {
	hidden map[string]bool
	err    error
	asked  [][]string
}

func (p *issue3803Posts) PostsHiddenFrom(_ context.Context, _ chat.Principal, _ string, ids []string) (map[string]bool, error) {
	p.asked = append(p.asked, ids)
	return p.hidden, p.err
}

type issue3803Fixture struct {
	posts   *issue3803Posts
	surface *PersonaChatSurface
	ctx     context.Context
	room    *personaSurfaceChatFixture
	source  *issue3803Source
	runs    *issue3803Runs
	persona string
}

func issue3803Surface(t *testing.T) *issue3803Fixture {
	t.Helper()
	s, ctx, room, invocations, _ := personaSurfaceFixture(t)
	persona := invocations.rows[0].PersonaID
	invocations.rows = nil
	source := &issue3803Source{personaSurfaceInvocationsFixture: invocations}
	runs := &issue3803Runs{runs: map[string]runstate.Run{}}
	s.Invocations = source
	s.Executions = func(context.Context, string) (runstate.Store, error) { return runs, nil }
	posts := &issue3803Posts{hidden: map[string]bool{}}
	s.Posts = posts
	return &issue3803Fixture{posts: posts, surface: s, ctx: ctx, room: room, source: source, runs: runs, persona: persona}
}

// ask records a question by invoker under post, and the run state it reached
// (an empty state means no run exists for it).
func (f *issue3803Fixture) ask(t *testing.T, id, post, invoker string, state runstate.State) {
	t.Helper()
	row := agentinvocationstore.OutcomeInvocation{ID: id, TenantID: "tenant-a", ConversationID: "channel-a", ThreadID: post, PostID: post, InvokerID: invoker, PersonaID: f.persona, PersonaVersion: "1"}
	f.source.invocations = append([]agentinvocationstore.OutcomeInvocation{row}, f.source.invocations...)
	if state == "" {
		return
	}
	runID, err := personaSurfaceRunID(agentinvoke.Invocation{ID: id, TenantID: "tenant-a", PostID: post})
	if err != nil {
		t.Fatal(err)
	}
	f.runs.runs[runID] = runstate.Run{ID: runID, AdmissionID: runID, TenantID: "tenant-a", State: state, TerminalCode: "SECRET_INTERNAL_REASON"}
}

func (f *issue3803Fixture) outcomes(t *testing.T) map[string]personachat.OutcomeReceipt {
	t.Helper()
	progress, err := f.surface.Progress(f.ctx, "channel-a")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]personachat.OutcomeReceipt{}
	for _, receipt := range progress.Outcomes {
		if _, dup := out[receipt.PostID]; dup {
			t.Fatalf("two receipts under one message %s: %+v", receipt.PostID, progress.Outcomes)
		}
		out[receipt.PostID] = receipt
	}
	return out
}

func TestIssue3803_OutcomeReceipts_EveryEndedRunGetsOneReceiptForEveryMember(t *testing.T) {
	f := issue3803Surface(t)
	// Another person's runs: the viewer is user-a and asked none of these.
	f.ask(t, "i-done", "post-done", "grace", runstate.StateCompleted)
	f.ask(t, "i-failed", "post-failed", "grace", runstate.StateFailed)
	f.ask(t, "i-repair", "post-repair", "grace", runstate.StateNeedsRepair)
	f.ask(t, "i-cancel", "post-cancel", "grace", runstate.StateCancelled)
	f.ask(t, "i-expired", "post-expired", "grace", runstate.StateExpired)
	f.ask(t, "i-running", "post-running", "grace", runstate.StateRunning)
	f.ask(t, "i-waiting", "post-waiting", "grace", runstate.StateWaiting)
	f.ask(t, "i-pending", "post-pending", "grace", "")
	f.source.refusals = []agentinvocationstore.OutcomeRefusal{{TenantID: "tenant-a", InvokerID: "grace", ConversationID: "channel-a", ThreadID: "post-refused", PostID: "post-refused"}}
	got := f.outcomes(t)
	want := map[string]string{
		"post-done": personachat.OutcomeSucceeded, "post-failed": personachat.OutcomeFailed, "post-repair": personachat.OutcomeFailed,
		"post-cancel": personachat.OutcomeCancelled, "post-expired": personachat.OutcomeExpired, "post-refused": personachat.OutcomeRefused,
	}
	if len(got) != len(want) {
		t.Fatalf("receipts=%+v want %v", got, want)
	}
	for post, outcome := range want {
		receipt := got[post]
		if receipt.Outcome != outcome || receipt.InvokerID != "grace" || receipt.ConversationID != "channel-a" || receipt.ThreadID != post || receipt.ReceiptID == "" {
			t.Fatalf("receipt for %s=%+v want outcome %s", post, receipt, outcome)
		}
	}
	for _, post := range []string{"post-running", "post-waiting", "post-pending"} {
		if _, shown := got[post]; shown {
			t.Fatalf("a run still going has no receipt: %s", post)
		}
	}
	if got["post-done"].AgentName != "People Coach" {
		t.Fatalf("the receipt names the agent from the directory: %+v", got["post-done"])
	}
	// The viewer's own ended run is receipted the same way.
	f.ask(t, "i-mine", "post-mine", "user-a", runstate.StateCompleted)
	if mine := f.outcomes(t)["post-mine"]; mine.Outcome != personachat.OutcomeSucceeded || mine.InvokerID != "user-a" {
		t.Fatalf("own receipt=%+v", mine)
	}
}

func TestIssue3803_OutcomeReceipts_SameReceiptsAfterRestartAndOnReplay(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-done", "post-done", "grace", runstate.StateCompleted)
	f.source.refusals = []agentinvocationstore.OutcomeRefusal{{TenantID: "tenant-a", InvokerID: "grace", ConversationID: "channel-a", ThreadID: "post-refused", PostID: "post-refused"}}
	first, _ := json.Marshal(f.outcomes(t))
	// A restart is a new surface over the same stored rows; a replay is a read
	// again. Neither adds, drops or reorders a receipt.
	restarted := *f.surface
	f.surface = &restarted
	second, _ := json.Marshal(f.outcomes(t))
	third, _ := json.Marshal(f.outcomes(t))
	if string(first) != string(second) || string(second) != string(third) || len(f.outcomes(t)) != 2 {
		t.Fatalf("receipts moved between reads:\n%s\n%s\n%s", first, second, third)
	}
	// The refusal's receipt is keyed by its question, the run's by its run.
	ids := map[string]bool{}
	progress, _ := f.surface.Progress(f.ctx, "channel-a")
	for _, receipt := range progress.Outcomes {
		if ids[receipt.ReceiptID] {
			t.Fatalf("receipt identity repeated: %+v", progress.Outcomes)
		}
		ids[receipt.ReceiptID] = true
	}
}

func TestIssue3803_OutcomeReceipts_AQuestionAskedAgainShowsItsNewestAttempt(t *testing.T) {
	f := issue3803Surface(t)
	// Oldest first: ask puts the newest at the front.
	f.ask(t, "i-first", "post-a", "grace", runstate.StateFailed)
	f.ask(t, "i-second", "post-a", "grace", runstate.StateRunning)
	if got := f.outcomes(t); len(got) != 0 {
		t.Fatalf("the earlier failure must not show while the question is being answered again: %+v", got)
	}
	f.ask(t, "i-third", "post-a", "grace", runstate.StateCompleted)
	if got := f.outcomes(t)["post-a"]; got.Outcome != personachat.OutcomeSucceeded {
		t.Fatalf("newest attempt wins: %+v", got)
	}
	// A refusal for a question that has a run is the run's business.
	f.source.refusals = []agentinvocationstore.OutcomeRefusal{{TenantID: "tenant-a", InvokerID: "grace", ConversationID: "channel-a", ThreadID: "post-a", PostID: "post-a"}}
	if got := f.outcomes(t); len(got) != 1 || got["post-a"].Outcome != personachat.OutcomeSucceeded {
		t.Fatalf("refusal beside a run: %+v", got)
	}
}

func TestIssue3803_OutcomeReceipts_CarryNoReasonAnswerOrTaskName_Security(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-failed", "post-failed", "grace", runstate.StateFailed)
	f.ask(t, "i-done", "post-done", "grace", runstate.StateCompleted)
	progress, err := f.surface.Progress(f.ctx, "channel-a")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(progress.Outcomes)
	for _, leaked := range []string{"SECRET_INTERNAL_REASON", "task", "failure", "output", "digest", "i-failed", "i-done"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(leaked)) {
			t.Fatalf("a receipt leaks %q: %s", leaked, raw)
		}
	}
}

func TestIssue3803_OutcomeReceipts_OnlyForAReaderOfTheConversation_Security(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-done", "post-done", "grace", runstate.StateCompleted)
	if len(f.outcomes(t)) != 1 {
		t.Fatal("a member sees the receipt")
	}
	// Access is checked on every read: a person whose membership ended gets no
	// receipt and no hint that the conversation exists.
	f.room.err = chat.ErrPermissionDenied
	progress, err := f.surface.Progress(f.ctx, "channel-a")
	if err == nil || len(progress.Outcomes) != 0 {
		t.Fatalf("a non-member read=%+v %v", progress, err)
	}
	if !errors.Is(err, personachat.ErrDenied) && !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("refusal shape=%v", err)
	}
	// A receipt of another conversation or tenant never travels here.
	f.room.err = nil
	f.source.invocations = append(f.source.invocations,
		agentinvocationstore.OutcomeInvocation{ID: "i-x", TenantID: "tenant-b", ConversationID: "channel-a", ThreadID: "post-x", PostID: "post-x", InvokerID: "grace", PersonaID: f.persona},
		agentinvocationstore.OutcomeInvocation{ID: "i-y", TenantID: "tenant-a", ConversationID: "channel-z", ThreadID: "post-y", PostID: "post-y", InvokerID: "grace", PersonaID: f.persona})
	if got := f.outcomes(t); len(got) != 1 {
		t.Fatalf("foreign receipts reached the page: %+v", got)
	}
}

func TestIssue3803_OutcomeReceipts_ReadFailureLeavesTheActivityIntact(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-done", "post-done", "grace", runstate.StateCompleted)
	f.source.err = errors.New("store down")
	progress, err := f.surface.Progress(f.ctx, "channel-a")
	if err != nil || len(progress.Outcomes) != 0 {
		t.Fatalf("a failed receipt read must not break the activity: %+v %v", progress, err)
	}
}

func TestIssue3803_OutcomeReceipts_NotUnderAMessageTheReaderCannotRead_Security(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-open", "post-open", "grace", runstate.StateCompleted)
	f.ask(t, "i-secret", "post-secret", "grace", runstate.StateFailed)
	f.ask(t, "i-mine", "post-mine", "user-a", runstate.StateCompleted)
	f.source.refusals = []agentinvocationstore.OutcomeRefusal{{TenantID: "tenant-a", InvokerID: "grace", ConversationID: "channel-a", ThreadID: "post-secret-refused", PostID: "post-secret-refused"}}
	f.posts.hidden = map[string]bool{"post-secret": true, "post-secret-refused": true}
	got := f.outcomes(t)
	if len(got) != 2 || got["post-open"].Outcome == "" || got["post-mine"].Outcome == "" {
		t.Fatalf("a message addressed to others must carry no receipt for this reader: %+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "post-secret") {
		t.Fatalf("the existence of a message the reader cannot read leaks: %s", raw)
	}
	for _, asked := range f.posts.asked {
		for _, id := range asked {
			if id == "post-mine" {
				t.Fatalf("the reader's own question needs no audience check: %v", asked)
			}
		}
	}
}

func TestIssue3803_OutcomeReceipts_NoAudienceAnswerShowsOnlyTheReadersOwn_Security(t *testing.T) {
	f := issue3803Surface(t)
	f.ask(t, "i-other", "post-other", "grace", runstate.StateCompleted)
	f.ask(t, "i-mine", "post-mine", "user-a", runstate.StateCompleted)
	f.posts.err = errors.New("audience store down")
	if got := f.outcomes(t); len(got) != 1 || got["post-mine"].Outcome == "" {
		t.Fatalf("a failed audience check must hide other people's receipts: %+v", got)
	}
	f.posts.err = nil
	f.surface.Posts = nil
	if got := f.outcomes(t); len(got) != 1 || got["post-mine"].Outcome == "" {
		t.Fatalf("without an audience check only the reader's own receipt may show: %+v", got)
	}
}
