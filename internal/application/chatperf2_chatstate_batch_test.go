package application

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// chatperf2BatchFixture answers each conversation with its own snapshot or its
// own refusal, and records who asked for what.
type chatperf2BatchFixture struct {
	chatstateHTTPFixture
	refused  map[string]error
	requests []chat.GetConversationRequest
}

func (f *chatperf2BatchFixture) GetChannelStatusSnapshot(_ context.Context, r chat.GetConversationRequest) (chat.ChannelStatusSnapshot, error) {
	f.requests = append(f.requests, r)
	if err := f.refused[r.ConversationID]; err != nil {
		return chat.ChannelStatusSnapshot{}, err
	}
	return chat.ChannelStatusSnapshot{Status: chat.ChannelStatus{TenantID: r.TenantID, ConversationID: r.ConversationID, Status: chatpolicy.StatusOpen, Revision: 3}, CanPost: true}, nil
}

// TestTodo_CHATBUG_014_ChannelStatusBatch: the statuses of several
// conversations are answered in one request, each read as the signed-in
// person exactly as the single read does, with a refusal confined to the
// conversation it belongs to.
func TestTodo_CHATBUG_014_ChannelStatusBatch(t *testing.T) {
	f := &chatperf2BatchFixture{refused: map[string]error{"secret": chat.ErrPermissionDenied, "gone": chat.ErrNotFound}}
	h := ChannelStatusHandler{Service: f}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, chatstateHTTPRequest(t, "GET", ChannelStatusPath+"?conversations=general,secret,%20engineering%20,general,,gone", ""))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("batch = %d %s (cache %q)", w.Code, w.Body, w.Header().Get("Cache-Control"))
	}
	var items []struct {
		ConversationID string                      `json:"conversation_id"`
		Code           string                      `json:"code"`
		Snapshot       *chat.ChannelStatusSnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	// One answer per conversation named, once each, in the order asked.
	want := []string{"general", "secret", "engineering", "gone"}
	if len(items) != len(want) || len(f.requests) != len(want) {
		t.Fatalf("%d answers from %d reads, want %d of each: %s", len(items), len(f.requests), len(want), w.Body)
	}
	for i, id := range want {
		item, request := items[i], f.requests[i]
		if item.ConversationID != id || request.ConversationID != id {
			t.Fatalf("answer %d is for %q (read %q), want %q", i, item.ConversationID, request.ConversationID, id)
		}
		// Every read is made as the admitted person in their own tenant.
		if request.Principal.SubjectID != "admin" || request.Principal.TenantID != "tenant-a" || request.TenantID != "tenant-a" {
			t.Fatalf("conversation %q was read as %+v", id, request)
		}
	}
	for _, i := range []int{0, 2} {
		if items[i].Code != "" || items[i].Snapshot == nil || items[i].Snapshot.Status.ConversationID != want[i] || items[i].Snapshot.Status.Revision != 3 || !items[i].Snapshot.CanPost {
			t.Fatalf("answer for %q = %+v", want[i], items[i])
		}
	}
	// A refusal stays with its conversation and carries nothing of it.
	if items[1].Code != "permission_denied" || items[1].Snapshot != nil || items[3].Code != "not_found" || items[3].Snapshot != nil {
		t.Fatalf("refusals = %+v, %+v", items[1], items[3])
	}
	// The batch answers what the single read answers.
	single := httptest.NewRecorder()
	h.ServeHTTP(single, chatstateHTTPRequest(t, "GET", ChannelStatusPath+"general", ""))
	var one chat.ChannelStatusSnapshot
	if err := json.Unmarshal(single.Body.Bytes(), &one); err != nil || one.Status != items[0].Snapshot.Status || one.CanPost != items[0].Snapshot.CanPost {
		t.Fatalf("single read %s differs from the batch answer %+v (%v)", single.Body, items[0].Snapshot, err)
	}

	// No ids, or more than the limit, is a bad request and reads nothing.
	before := len(f.requests)
	many := make([]string, channelStatusBatchLimit+1)
	for i := range many {
		many[i] = "room-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	for _, query := range []string{"?conversations=", "?conversations=,%20,", "?conversations=" + strings.Join(many, ",")} {
		refused := httptest.NewRecorder()
		h.ServeHTTP(refused, chatstateHTTPRequest(t, "GET", ChannelStatusPath+query, ""))
		if refused.Code != 400 || len(f.requests) != before {
			t.Fatalf("%.40s = %d after %d reads, want 400 and none", query, refused.Code, len(f.requests)-before)
		}
	}
	// It is not a way round admission, and the search form is still there.
	anonymous := httptest.NewRecorder()
	h.ServeHTTP(anonymous, httptest.NewRequest("GET", ChannelStatusPath+"?conversations=general", nil))
	if anonymous.Code != 401 || len(f.requests) != before {
		t.Fatalf("an unauthenticated batch = %d", anonymous.Code)
	}
	search := httptest.NewRecorder()
	h.ServeHTTP(search, chatstateHTTPRequest(t, "GET", ChannelStatusPath+"?archived=maybe", ""))
	if search.Code == 200 || len(f.requests) != before {
		t.Fatalf("the search form answered %d to a bad request", search.Code)
	}

	// A service without snapshots answers each conversation with its status.
	plain := &chatstateHTTPFixture{}
	w = httptest.NewRecorder()
	ChannelStatusHandler{Service: plain}.ServeHTTP(w, chatstateHTTPRequest(t, "GET", ChannelStatusPath+"?conversations=a,b", ""))
	if w.Code != 200 || plain.calls != 2 || !strings.Contains(w.Body.String(), `{"conversation_id":"a","snapshot":{`) || !strings.Contains(w.Body.String(), `{"conversation_id":"b","snapshot":{`) {
		t.Fatalf("plain batch = %d after %d reads: %s", w.Code, plain.calls, w.Body)
	}
}
