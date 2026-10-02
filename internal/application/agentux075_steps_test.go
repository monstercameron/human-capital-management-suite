package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A run reports each step as a typed kind with the name of the thing, the board
// keeps the latest per run, and the context carries it to the run.
func TestTodo_AGENTUX_075_Steps(t *testing.T) {
	now := time.Unix(1000, 0)
	board := newPersonaRunStepBoard(func() time.Time { return now })
	board.Note("tenant-a", "inv-1", personaStepSearching, "carry-over\n\tpolicy")
	kind, subject, ok := board.Current("tenant-a", "inv-1")
	if !ok || kind != personaStepSearching || subject != "carry-over policy" {
		t.Fatalf("current step = %q %q %v, want the search with a one-line subject", kind, subject, ok)
	}
	if _, _, ok := board.Current("tenant-b", "inv-1"); ok {
		t.Fatal("another tenant read this run's step")
	}
	board.Note("tenant-a", "inv-1", personaStepWriting, "")
	if kind, subject, _ = board.Current("tenant-a", "inv-1"); kind != personaStepWriting || subject != "" {
		t.Fatalf("a later note did not replace the earlier one: %q %q", kind, subject)
	}
	now = now.Add(personaRunStepTTL + time.Second)
	if _, _, ok := board.Current("tenant-a", "inv-1"); ok {
		t.Fatal("a step that nothing has updated for a quarter of an hour is still shown")
	}
	// A subject is one short line.
	long := strings.Repeat("holiday ", 40)
	board.Note("tenant-a", "inv-2", personaStepReadingDocument, long)
	if _, subject, _ := board.Current("tenant-a", "inv-2"); len([]rune(subject)) > personaRunStepSubjectRunes {
		t.Fatalf("subject of %d runes kept", len([]rune(subject)))
	}
	// The board is bounded.
	for i := 0; i < personaRunStepCap+10; i++ {
		board.Note("tenant-a", "run-"+string(rune('a'+i%26))+strings.Repeat("x", i/26), personaStepWriting, "")
	}
	if len(board.steps) > personaRunStepCap {
		t.Fatalf("board holds %d steps, cap is %d", len(board.steps), personaRunStepCap)
	}

	// What a tool call and its result are.
	query, _ := json.Marshal(map[string]string{"query": "carry over"})
	if kind, subject := personaStepForTool(personaDocumentSearchTool, query); kind != personaStepSearching || subject != "carry over" {
		t.Errorf("a document search is %q %q", kind, subject)
	}
	if kind, subject := personaStepForTool("some_internal_tool", query); kind != personaStepWorking || subject != "" {
		t.Errorf("an unknown tool is %q %q, and its name must never be shown", kind, subject)
	}
	one := []personaQualitySearchedDocument{{DocumentID: "d1", Title: "2026 holiday guide"}, {DocumentID: "d1", Title: "2026 holiday guide", SectionTitle: "Dates"}}
	if kind, subject := personaStepForDocuments(one); kind != personaStepReadingDocument || subject != "2026 holiday guide" {
		t.Errorf("one document is %q %q", kind, subject)
	}
	two := append(one, personaQualitySearchedDocument{DocumentID: "d2", Title: "Handbook"})
	if kind, subject := personaStepForDocuments(two); kind != personaStepReadingMany || subject != "" {
		t.Errorf("two documents are %q %q", kind, subject)
	}

	// With no note (a restart), the checkpoints say how far the run is, and never a name.
	for _, test := range []struct {
		checkpoints []runstate.Checkpoint
		want        string
	}{
		{nil, personaStepReadingQuestion},
		{[]runstate.Checkpoint{{Phase: runstate.PhaseModelCall, Attempt: 1}}, personaStepReadingQuestion},
		{[]runstate.Checkpoint{{Phase: runstate.PhaseModelCall, Attempt: 1}, {Phase: runstate.PhaseToolCall, Attempt: 2}}, personaStepReadingMany},
		{[]runstate.Checkpoint{{Phase: runstate.PhaseToolCall, Attempt: 2}, {Phase: runstate.PhaseModelCall, Attempt: 2}}, personaStepWriting},
		{[]runstate.Checkpoint{{Phase: runstate.PhaseValidation, Attempt: 1}}, personaStepWriting},
	} {
		if got := personaStepFromRun(runstate.Run{Checkpoints: test.checkpoints}); got != test.want {
			t.Errorf("checkpoints %+v read as %q, want %q", test.checkpoints, got, test.want)
		}
	}

	// The context carries the board to the run; without it the run notes nothing.
	admission := agentrun.Record{Request: agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-a", Key: "inv-3"}}}
	personaRunStepNoter(context.Background(), admission)(personaStepSearching, "x")
	personaRunStepNoter(withPersonaRunSteps(context.Background(), board), admission)(personaStepReadingQuestion, "")
	if kind, _, ok := board.Current("tenant-a", "inv-3"); !ok || kind != personaStepReadingQuestion {
		t.Fatalf("the run's note did not reach the board through its context: %q %v", kind, ok)
	}
}

// The progress read puts the run's step on its invocation row for the asker,
// from the run's own note when there is one, else from its checkpoints, and not
// at all once the run is over.
func TestTodo_AGENTUX_075_StepsProgress(t *testing.T) {
	s, ctx, _, invocations, execution := personaSurfaceFixture(t)
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 {
		t.Fatalf("progress=%+v %v", progress, err)
	}
	if got := progress.Invocations[0]; got.StepKind != personaStepReadingQuestion || got.StepSubject != "" {
		t.Fatalf("a run with only a first model checkpoint reads %q %q", got.StepKind, got.StepSubject)
	}
	s.Steps = newPersonaRunStepBoard(s.Now)
	s.Steps.Note("tenant-a", "invocation-a", personaStepReadingDocument, "2026 holiday guide")
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || progress.Invocations[0].StepKind != personaStepReadingDocument || progress.Invocations[0].StepSubject != "2026 holiday guide" {
		t.Fatalf("the run's own note is not on the row: %+v %v", progress.Invocations, err)
	}
	if raw, _ := json.Marshal(progress.Invocations[0]); !strings.Contains(string(raw), `"step_kind":"reading_document"`) || !strings.Contains(string(raw), `"step_subject":"2026 holiday guide"`) {
		t.Fatalf("the typed step is not in the wire row: %s", raw)
	}
	execution.run.State, execution.run.TerminalCode = runstate.StateFailed, "MODEL_UNAVAILABLE"
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || progress.Invocations[0].StepKind != "" || progress.Invocations[0].StepSubject != "" {
		t.Fatalf("a finished run still reports a step: %+v %v", progress.Invocations, err)
	}
	if invocations.owner != "user-a" {
		t.Fatalf("the read was not the asker's: %+v", invocations)
	}
}

type agentUX075Committer struct {
	mu        sync.Mutex
	reactions []chatcore.AgentQuestionReaction
	err       error
}

func (c *agentUX075Committer) CommitAgentQuestionReaction(_ context.Context, r chatcore.AgentQuestionReaction) (chatcore.Reaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reactions = append(c.reactions, r)
	return chatcore.Reaction{PostID: r.PostID, Emoji: r.Emoji}, c.err
}

type agentUX075Switch map[string]bool

func (s agentUX075Switch) AgentReactionsEnabled(_ context.Context, _, _, agent string) bool {
	on, set := s[agent]
	return !set || on
}

type agentUX075Chooser struct{ pick string }

func (c agentUX075Chooser) ChooseOutcomeEmoji(context.Context, string, []string) (string, bool) {
	return c.pick, c.pick != ""
}

// AGENTUX-052: the reaction that fits the outcome takes the first one's place,
// from a closed table and without a model call; a model's choice counts only for
// an answer and only from the closed set. The live model-chosen part needs a run.
func TestTodo_AGENTUX_052_OutcomeReaction(t *testing.T) {
	failure := func(code string) error { return &PersonaRunFailure{Code: code} }
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"answered", nil, "✅"},
		{"found nothing", failure("NO_RESULTS"), "🤷"},
		{"not allowed", failure("OUT_OF_SCOPE"), "🤷"},
		{"refused at admission", errPersonaReferenceInactive, "🤷"},
		{"model down", failure("MODEL_UNAVAILABLE"), chatcore.AgentOutcomeFailed()},
		{"no model on the server", errPersonaModelNotConfigured, chatcore.AgentOutcomeFailed()},
		{"a failure nobody classified", errors.New("a database detail nobody should read"), chatcore.AgentOutcomeFailed()},
		{"stopped by the asker", failure("CANCELLED"), ""},
	} {
		if got := agentUX075OutcomeEmoji(test.err); got != test.want {
			t.Errorf("%s: outcome emoji %q, want %q", test.name, got, test.want)
		}
		if got := agentUX075OutcomeEmoji(test.err); got != "" && !chatcore.ValidAgentQuestionReactionEmoji(got) {
			t.Errorf("%s: %q is outside the closed set", test.name, got)
		}
	}

	post := chatcore.Post{ID: "question", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", Body: "Show me all the holidays"}
	asker := chatcore.Principal{TenantID: "t1", SubjectID: "u1"}
	committer := &agentUX075Committer{}
	reactor := servedPersonaQuestionReactor{chat: committer, switches: agentUX075Switch{"quiet-agent": false}}
	reactor.ReactToOutcome(context.Background(), post, asker, "assistant", chatcore.AgentOutcomeAnswered())
	if len(committer.reactions) != 1 {
		t.Fatalf("outcome reactions: %+v", committer.reactions)
	}
	got := committer.reactions[0]
	if got.Emoji != "✅" || got.Replaces != agentUX075QuestionEmoji(post.Body) || got.Replaces != "🔎" || got.AgentSubjectID != "assistant" || got.Asker.SubjectID != asker.SubjectID || got.Asker.TenantID != asker.TenantID {
		t.Fatalf("outcome reaction %+v, want ✅ in the place of the first one (🔎) by the agent for the asker", got)
	}
	// The owner's switch silences the outcome reaction too.
	reactor.ReactToOutcome(context.Background(), post, asker, "quiet-agent", chatcore.AgentOutcomeAnswered())
	reactor.ReactToOutcome(context.Background(), post, asker, "assistant", "")
	if len(committer.reactions) != 1 {
		t.Fatalf("a switched-off agent or an empty outcome reacted: %+v", committer.reactions)
	}

	// A fake model may choose for an answer, from the closed set only.
	reactor.chooser = agentUX075Chooser{pick: "🙌"}
	reactor.ReactToOutcome(context.Background(), post, asker, "assistant", chatcore.AgentOutcomeAnswered())
	reactor.chooser = agentUX075Chooser{pick: "💣"}
	reactor.ReactToOutcome(context.Background(), post, asker, "assistant", chatcore.AgentOutcomeAnswered())
	reactor.chooser = agentUX075Chooser{pick: "🙌"}
	reactor.ReactToOutcome(context.Background(), post, asker, "assistant", chatcore.AgentOutcomeFailed())
	var emojis []string
	for _, reaction := range committer.reactions[1:] {
		emojis = append(emojis, reaction.Emoji)
	}
	if strings.Join(emojis, " ") != "🙌 ✅ "+chatcore.AgentOutcomeFailed() {
		t.Fatalf("model-chosen reactions = %q: the choice must count only for an answer and only from the closed set", emojis)
	}
}

type agentUX075Refs struct {
	persona string
	err     error
}

func (r agentUX075Refs) ResolvePersonaMentions(_ context.Context, _, _ string, refs []chatcore.Reference) ([]agentinvoke.Mention, error) {
	if r.err != nil || len(refs) != 1 {
		return nil, r.err
	}
	return []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: r.persona, Canonical: true}}, nil
}

type agentUX075Settings struct {
	off map[string]bool
	err error
}

func (s agentUX075Settings) PersonaReactionsEnabled(_ context.Context, _, persona string) (bool, error) {
	if s.err != nil {
		return true, s.err
	}
	return !s.off[persona], nil
}

// The owner's per-agent switch is read for the persona the agent is: an agent
// switched off does not react, one with no choice does, and a switch that cannot
// be read leaves the reaction on (AGENTUX-075).
func TestTodo_AGENTUX_075_ReactionSwitch(t *testing.T) {
	ctx := context.Background()
	off := agentUX075Settings{off: map[string]bool{"persona-quiet": true}}
	if got := newStoredPersonaReactionSwitch(agentUX075Refs{persona: "persona-quiet"}, off).AgentReactionsEnabled(ctx, "t1", "c1", "assistant"); got {
		t.Error("an agent its owner switched off still reacts")
	}
	if got := newStoredPersonaReactionSwitch(agentUX075Refs{persona: "persona-loud"}, off).AgentReactionsEnabled(ctx, "t1", "c1", "assistant"); !got {
		t.Error("an agent with no stored choice does not react")
	}
	if got := newStoredPersonaReactionSwitch(agentUX075Refs{persona: "persona-quiet"}, agentUX075Settings{err: errors.New("down")}).AgentReactionsEnabled(ctx, "t1", "c1", "assistant"); !got {
		t.Error("an unreadable setting silenced the agent")
	}
	if got := newStoredPersonaReactionSwitch(agentUX075Refs{err: errors.New("gone")}, off).AgentReactionsEnabled(ctx, "t1", "c1", "assistant"); !got {
		t.Error("an agent that cannot be resolved was silenced")
	}
	if newStoredPersonaReactionSwitch(nil, off) != nil || newStoredPersonaReactionSwitch(agentUX075Refs{}, nil) != nil {
		t.Error("a switch was built without its ports")
	}
	// Through the reactor: the switched-off agent's reaction is not stored.
	committer := &agentUX075Committer{}
	reactor := servedPersonaQuestionReactor{chat: committer, switches: newStoredPersonaReactionSwitch(agentUX075Refs{persona: "persona-quiet"}, off)}
	post := chatcore.Post{ID: "q", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", Body: "hello"}
	reactor.ReactToQuestion(ctx, post, chatcore.Principal{TenantID: "t1", SubjectID: "u1"}, "assistant")
	reactor.ReactToOutcome(ctx, post, chatcore.Principal{TenantID: "t1", SubjectID: "u1"}, "assistant", chatcore.AgentOutcomeAnswered())
	if len(committer.reactions) != 0 {
		t.Errorf("a switched-off agent reacted: %+v", committer.reactions)
	}
}

// leasedChat stands for the routed service: it stores a reaction only when the
// call reaches it through the chain, and says so.
type agentUX075Routed struct {
	chatcore.ConversationService
	calls []chatcore.AgentQuestionReaction
}

func (r *agentUX075Routed) CommitAgentQuestionReaction(_ context.Context, request chatcore.AgentQuestionReaction) (chatcore.Reaction, error) {
	r.calls = append(r.calls, request)
	return chatcore.Reaction{PostID: request.PostID, Emoji: request.Emoji}, nil
}

// The reaction travels the served chain (streaming, then the audit decorator,
// then the routed service that holds the route lease), and does not go around it
// to the core service, which refused it on a routed server (AGENTUX-075).
func TestTodo_AGENTUX_075_ReactionTravelsTheServedChain(t *testing.T) {
	routed := &agentUX075Routed{}
	audited := &auditedChatService{ConversationService: routed}
	served := &streamingChatService{ConversationService: audited, masker: nil}
	committer := servedAgentQuestionReactions(served)
	if committer != chatcore.AgentQuestionReactionCommitter(served) {
		t.Fatalf("the served service did not take the reaction: %T", committer)
	}
	request := chatcore.AgentQuestionReaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", AgentSubjectID: "assistant", Asker: chatcore.Principal{TenantID: "t1", SubjectID: "u1"}, Emoji: "👀"}
	if _, err := committer.CommitAgentQuestionReaction(context.Background(), request); err != nil || len(routed.calls) != 1 || routed.calls[0].PostID != "p1" {
		t.Fatalf("the reaction did not reach the routed service: %v %+v", err, routed.calls)
	}
	// A chain that cannot carry it says so; it is never dropped silently.
	bare := &streamingChatService{ConversationService: struct{ chatcore.ConversationService }{}}
	if got := servedAgentQuestionReactions(bare); got != nil {
		t.Errorf("a chain with no reaction port was given one: %T", got)
	}
	if _, err := bare.CommitAgentQuestionReaction(context.Background(), request); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Errorf("a chain with no reaction port: %v", err)
	}
}
