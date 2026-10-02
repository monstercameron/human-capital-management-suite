package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// The person who asked may ask again after any ending that left the question
// unanswered, however the run ended; a run still going, or one that answered,
// may not be asked again. Asking again sends the original question, with the
// one agent it was put to, as a reply in the question's own thread. No model is
// involved: the chat and the stores are fixtures.
func TestTodo_CHATBUG_054(t *testing.T) {
	for _, tc := range []struct {
		state runstate.State
		code  string
		want  error
	}{
		{runstate.StateFailed, "MODEL_UNAVAILABLE", nil},
		{runstate.StateFailed, "OUTPUT_REJECTED", nil},
		{runstate.StateFailed, "DAILY_LIMIT_REACHED", nil},
		{runstate.StateExpired, "EXPIRED", nil},
		{runstate.StateCancelled, "CANCELLED", nil},
		{runstate.StateNeedsRepair, "DELIVERY_FAILED", nil},
		{runstate.StateRunning, "", personachat.ErrConflict},
		{runstate.StateWaiting, "", personachat.ErrConflict},
		{runstate.StateCompleted, "", personachat.ErrConflict},
	} {
		surface, ctx, room, _, execution := personaSurfaceFixture(t)
		execution.run.State, execution.run.TerminalCode = tc.state, tc.code
		result, err := surface.Retry(ctx, "invocation-a", "ask-again-key")
		if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Fatalf("%s %s: asking again = %v, want %v", tc.state, tc.code, err, tc.want)
		}
		if tc.want != nil {
			if len(room.sends) != 0 || len(room.reattempts) != 0 {
				t.Fatalf("%s: a refused Ask again still sent %d messages and admitted %d", tc.state, len(room.sends), len(room.reattempts))
			}
			continue
		}
		// CHATBUG-047: no message is posted; the question that stands, with its one
		// agent, is admitted again as the first retry.
		if result.PostID != "post-a" || len(room.sends) != 0 || len(room.reattempts) != 1 {
			t.Fatalf("%s %s: result=%+v sends=%d reattempts=%d", tc.state, tc.code, result, len(room.sends), len(room.reattempts))
		}
		asked := room.reattempts[0]
		if asked.post.Body != "Help with policy" || asked.post.ID != "post-a" || asked.post.ConversationID != "channel-a" || asked.attempt != 1 {
			t.Fatalf("%s: asked again with %+v, want the stored question, attempt one", tc.state, asked)
		}
	}
}

// A failure stored for a message with no run, and a question the server holds
// nothing for at all, can both be asked again by the person who asked; a run
// that stands behind the question still decides.
func TestTodo_CHATBUG_054_Integration(t *testing.T) {
	// A stored failure the server marked as not worth retrying.
	surface, ctx, room, invocations, execution := personaSurfaceFixture(t)
	invocations.rows = nil
	execution.err = agentrunstate.ErrNotFound
	surface.Failures = &personaSurfaceFailuresFixture{failures: []agentinvocationstore.PostFailure{{TenantID: "tenant-a", InvokerID: "user-a", PostID: "post-a", ConversationID: "channel-a", ThreadID: "post-a", Code: "INVOCATION_FAILED", Retryable: false}}}
	if result, err := surface.Retry(ctx, "post-failure:post-a", "ask-again-key"); err != nil || result.PostID != "post-a" || len(room.sends) != 0 || len(room.reattempts) != 1 {
		t.Fatalf("a stored failure cannot be asked again: %+v %v sends=%d", result, err, len(room.sends))
	}

	// A question named by its own message, with nothing stored for it.
	surface, ctx, room, invocations, execution = personaSurfaceFixture(t)
	invocations.rows = nil
	execution.err = agentrunstate.ErrNotFound
	result, err := surface.Retry(ctx, "question:channel-a:post-a", "ask-again-key")
	if err != nil || result.PostID != "post-a" || len(room.sends) != 0 || len(room.reattempts) != 1 || room.reattempts[0].post.Body != "Help with policy" || room.reattempts[0].attempt != 0 {
		t.Fatalf("a question with no run cannot be asked again: %+v %v reattempts=%+v", result, err, room.reattempts)
	}

	// The same name while a run for that question is still going: refused.
	surface, ctx, room, _, _ = personaSurfaceFixture(t)
	if _, err := surface.Retry(ctx, "question:channel-a:post-a", "ask-again-key"); !errors.Is(err, personachat.ErrConflict) || len(room.sends) != 0 {
		t.Fatalf("a question whose run is still going was asked again: %v sends=%d", err, len(room.sends))
	}

	// A message that is not the caller's, or not there, is refused.
	surface, ctx, room, invocations, execution = personaSurfaceFixture(t)
	invocations.rows = nil
	execution.err = agentrunstate.ErrNotFound
	room.posts[0].AuthorID = "somebody-else"
	if _, err := surface.Retry(ctx, "question:channel-a:post-a", "ask-again-key"); !errors.Is(err, personachat.ErrDenied) || len(room.sends) != 0 {
		t.Fatalf("somebody else's question was asked again: %v", err)
	}
	if _, err := surface.Retry(ctx, "question:channel-a:no-such-post", "ask-again-key"); !errors.Is(err, personachat.ErrDenied) || len(room.sends) != 0 {
		t.Fatalf("a question that is not in the conversation was asked again: %v", err)
	}
	for _, malformed := range []string{"question:", "question:channel-a", "question:channel-a:", "question::post-a"} {
		if _, err := surface.Retry(ctx, malformed, "ask-again-key"); !errors.Is(err, personachat.ErrDenied) {
			t.Fatalf("%q: %v, want a refusal", malformed, err)
		}
	}
	// A message that names no agent has nothing to ask again.
	room.posts[0].AuthorID, room.posts[0].References = "user-a", nil
	if _, err := surface.Retry(ctx, "question:channel-a:post-a", "ask-again-key"); !errors.Is(err, personachat.ErrDenied) || len(room.sends) != 0 {
		t.Fatalf("an ordinary message was put to an agent: %v", err)
	}

	if conversation, post, ok := personaRetryQuestion("question:room:with:colons:post-1"); !ok || conversation != "room:with:colons" || post != "post-1" {
		t.Fatalf("question name split into %q %q %v", conversation, post, ok)
	}
	if _, _, ok := personaRetryQuestion("invocation-a"); ok {
		t.Fatal("an invocation id was read as a question name")
	}
}
