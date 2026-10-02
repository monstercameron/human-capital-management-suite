package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// chatbug066Invocations is the invocation store with the ratings it holds.
type chatbug066Invocations struct {
	*personaSurfaceInvocationsFixture
	ratings                    map[string]agentinvocationstore.AnswerFeedback
	tenant, person, room, read string
	err                        error
}

func (s *chatbug066Invocations) ListAnswerFeedback(_ context.Context, tenant, person, room string) (map[string]agentinvocationstore.AnswerFeedback, error) {
	s.tenant, s.person, s.room = tenant, person, room
	return s.ratings, s.err
}

// The agent activity a page loads carries the asker's own stored rating for
// each answer, read for that person and that conversation, so the card is drawn
// rated from its first paint; an undone rating is not sent.
func TestTodo_CHATBUG_066(t *testing.T) {
	surface, ctx, _, invocations, _ := personaSurfaceFixture(t)
	store := &chatbug066Invocations{personaSurfaceInvocationsFixture: invocations}
	surface.Invocations = store

	progress, err := surface.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].Feedback != "" {
		t.Fatalf("an unrated answer: %+v %v", progress, err)
	}
	if store.tenant != "tenant-a" || store.person != "user-a" || store.room != "channel-a" {
		t.Fatalf("ratings were read for %q %q %q, want the caller in this conversation", store.tenant, store.person, store.room)
	}
	for _, tc := range []struct {
		rating agentinvocationstore.AnswerFeedback
		want   string
	}{
		{agentinvocationstore.AnswerFeedback{InvocationID: "invocation-a", Helpful: true, Active: true}, "helpful"},
		{agentinvocationstore.AnswerFeedback{InvocationID: "invocation-a", Helpful: false, Active: true}, "not-right"},
		{agentinvocationstore.AnswerFeedback{InvocationID: "invocation-a", Helpful: true, Active: false}, ""},
	} {
		store.ratings = map[string]agentinvocationstore.AnswerFeedback{"invocation-a": tc.rating, "somebody-elses-run": {Helpful: true, Active: true}}
		progress, err = surface.Progress(ctx, "channel-a")
		if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].Feedback != tc.want {
			t.Fatalf("rating %+v is sent as %q (%v), want %q", tc.rating, progress.Invocations[0].Feedback, err, tc.want)
		}
		encoded, _ := json.Marshal(progress)
		if tc.want != "" && !strings.Contains(string(encoded), `"feedback":"`+tc.want+`"`) {
			t.Fatalf("the rating is not in what the page reads: %s", encoded)
		}
		if tc.want == "" && strings.Contains(string(encoded), `"feedback"`) {
			t.Fatalf("an undone rating is still sent: %s", encoded)
		}
	}
	// A failed read of the ratings does not send a page that would show every
	// answer as unrated.
	store.err = errors.New("ratings store is down")
	if _, err = surface.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("a failed read of the ratings: %v", err)
	}
}
