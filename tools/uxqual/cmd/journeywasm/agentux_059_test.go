package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

func agentux059Response(status int, body string) func() *http.Response {
	return func() *http.Response {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
	}
}

// agentux059Visible is what the rating controls show for one answer, by the
// rule the card applies: a change the server refused shows what the server
// holds; otherwise the stored rating.
func agentux059Visible(model chatui.Model, invocation string) (rating string, unsaved bool) {
	if restored, failed := model.AgentFeedbackRestored[invocation]; failed {
		return restored, true
	}
	return model.AgentFeedbackSaved[invocation], false
}

// A rating reaches the server or the page says it did not: after a 503, a
// timeout and a 409 the control is back at the rating the server holds and the
// answer is marked as not saved; after a 200 it shows the new rating.
func TestTodo_AGENTUX_059_Fault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response func() *http.Response
		err      error
		failed   bool
	}{
		{"saved", agentux059Response(http.StatusOK, `{"invocation_id":"run","helpful":true,"active":true}`), nil, false},
		{"503", agentux059Response(http.StatusServiceUnavailable, `{"error":"unavailable"}`), nil, true},
		{"409", agentux059Response(http.StatusConflict, `{"error":"conflict"}`), nil, true},
		{"403", agentux059Response(http.StatusForbidden, `{"error":"denied"}`), nil, true},
		{"timeout", nil, context.DeadlineExceeded, true},
		{"no response", nil, nil, true},
		{"200 with a body that is not an answer", agentux059Response(http.StatusOK, `<html>`), nil, true},
	} {
		for _, previous := range []string{"", chatui.AgentFeedbackHelpful, chatui.AgentFeedbackNotRight} {
			var result personachat.FeedbackResult
			var response *http.Response
			if tc.response != nil {
				response = tc.response()
			}
			err := personaChatActionOutcome(response, tc.err, &result)
			if (err != nil) != tc.failed || (err != nil && !errors.Is(err, errPersonaChat)) {
				t.Fatalf("%s: outcome %v, want failed=%v", tc.name, err, tc.failed)
			}
			// What the client does with that outcome, for a person who held
			// `previous` and pressed Helpful.
			var ledger personaFeedbackLedger
			ledger.confirm("run", previous)
			model := chatui.Model{AgentFeedbackSaved: ledger.shown()}
			ledger.begin("run")
			if err != nil {
				model.AgentFeedbackRestored = personaFeedbackRestoredWith(model.AgentFeedbackRestored, "run", ledger.rating("run"))
			} else {
				ledger.confirm("run", personaFeedbackRating(true))
				model.AgentFeedbackSaved = ledger.shown()
			}
			ledger.end("run")
			rating, unsaved := agentux059Visible(model, "run")
			if tc.failed && (rating != previous || !unsaved) {
				t.Fatalf("%s after %q: the control shows %q unsaved=%v, want the previous rating marked as not saved", tc.name, previous, rating, unsaved)
			}
			if !tc.failed && (rating != chatui.AgentFeedbackHelpful || unsaved) {
				t.Fatalf("%s after %q: the control shows %q unsaved=%v, want Helpful, saved", tc.name, previous, rating, unsaved)
			}
			// Rating again clears the mark, so the next choice shows as chosen.
			model.AgentFeedbackRestored = personaFeedbackRestoredWithout(model.AgentFeedbackRestored, "run")
			if _, still := agentux059Visible(model, "run"); still {
				t.Fatalf("%s: the not-saved mark outlives the next rating", tc.name)
			}
		}
	}
}
