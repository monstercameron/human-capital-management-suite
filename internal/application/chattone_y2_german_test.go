package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chattoneY2Reader drives the served rendering reader endpoint the page reads
// for a conversation's messages, as one person.
func chattoneY2Reader(t *testing.T, rig *chatlangRig, subject string, ids ...string) (int, map[string]chatui.ReaderSelection) {
	t.Helper()
	admission, bearer := integrate1Admission(t, "host", subject, time.Now)
	handler := (&agentServedAssembly{Renderings: rig.chat.renderings}).Overlay(http.NotFoundHandler(), admission)
	query := url.Values{"conversation": {rig.room}, "tenant": {"host"}, "message": ids}
	request := httptest.NewRequest(http.MethodGet, ChatRenderingPath+"/reader?"+query.Encode(), nil)
	request.Header.Set("Authorization", bearer)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var out map[string]chatui.ReaderSelection
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatalf("reader answer: %v: %s", err, response.Body.String())
		}
	}
	return response.Code, out
}

// TestChattoneY2GermanReaderCalmPost: a reader who reads German, a calm English
// post, the fixture translator and the reword producer registered. The reader
// answer is 200 with a pending or ready selection in every reword setting, and a
// message that does not exist answers 404, not 503.
func TestChattoneY2GermanReaderCalmPost(t *testing.T) {
	rig, setting := chattoneY2Rig(t, chattoneRewordReplayFixture(t))
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	calm := rig.send("calm-de", w10Calm)
	for _, mode := range []string{chatstore.RewordModeOff, chatstore.RewordModeOffered, chatstore.RewordModeOn} {
		for _, view := range []bool{true, false} {
			setting(mode, view)
			code, out := chattoneY2Reader(t, rig, "bruno", calm.ID)
			if code != http.StatusOK {
				t.Fatalf("mode %s view %v: reader answered %d", mode, view, code)
			}
			got, ok := out[calm.ID]
			if !ok || (got.Mark.State != "pending" && got.Mark.State != "ready" && got.Mark.State != "original" && got.Mark.State != "fallback") {
				t.Fatalf("mode %s view %v: %+v", mode, view, got)
			}
			rig.drain()
			code, out = chattoneY2Reader(t, rig, "bruno", calm.ID)
			if code != http.StatusOK || out[calm.ID].Mark.State != "ready" {
				t.Fatalf("mode %s view %v after the worker ran: %d %+v", mode, view, code, out[calm.ID])
			}
		}
	}
	if code, _ := chattoneY2Reader(t, rig, "bruno", "no-such-post"); code != http.StatusNotFound {
		t.Fatalf("a message that does not exist answered %d", code)
	}

	// One message that is gone does not take the others with it: a removed message
	// among the page's messages is left out of the answer and the rest read as
	// usual, so the page keeps what it has and asks nothing again.
	gone := rig.send("gone-de", "A message that is removed after the page listed it.")
	if _, err := rig.chat.service.DeletePost(rig.as("alice"), chatcore.DeletePostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: rig.room, PostID: gone.ID, ExpectedRevision: gone.Revision}); err != nil {
		t.Fatal(err)
	}
	code, out := chattoneY2Reader(t, rig, "bruno", calm.ID, gone.ID, "no-such-post")
	if code != http.StatusOK || out[calm.ID].Mark.State != "ready" || len(out) != 1 {
		t.Fatalf("a batch with messages that are gone: %d %+v", code, out)
	}
	if code, _ := chattoneY2Reader(t, rig, "bruno", gone.ID); code != http.StatusNotFound {
		t.Fatalf("a removed message answered %d", code)
	}
}
