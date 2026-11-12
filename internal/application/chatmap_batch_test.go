package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// issue3676MessageSurface answers the per-message location read that the batch
// form repeats, counting how often each post was looked up.
type issue3676MessageSurface struct {
	chatmapSurfaceFixture
	lookups map[string]int
	denied  string
}

func (f *issue3676MessageSurface) MessageLocations(_ context.Context, _ chat.Principal, _, _, postID string) ([]chat.LocationShare, error) {
	if f.lookups == nil {
		f.lookups = map[string]int{}
	}
	f.lookups[postID]++
	if postID == f.denied {
		return nil, chat.ErrPermissionDenied
	}
	return []chat.LocationShare{{Version: 1, PostID: postID}}, nil
}

func TestTodo_ISSUE3676_MessageBatch(t *testing.T) {
	f := &issue3676MessageSurface{}
	h := ChatmapHTTP{Surface: f}
	body := `{"TenantID":"chatmap-tenant","ConversationID":"c","PostIDs":["m1","m2","m1"]}`
	req := httptest.NewRequest(http.MethodPost, ChatmapPath+"/message", strings.NewReader(body)).WithContext(chatmapHTTPContext(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var answers map[string][]chat.LocationShare
	if err := json.Unmarshal(w.Body.Bytes(), &answers); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if len(answers) != 2 || len(answers["m1"]) != 1 || answers["m1"][0].PostID != "m1" || len(answers["m2"]) != 1 || answers["m2"][0].PostID != "m2" {
		t.Fatal(answers)
	}
	if f.lookups["m1"] != 1 || f.lookups["m2"] != 1 {
		t.Fatal("each distinct post is looked up once", f.lookups)
	}
}

func TestTodo_ISSUE3676_MessageBatch_SingleFormUnchanged(t *testing.T) {
	f := &issue3676MessageSurface{}
	h := ChatmapHTTP{Surface: f}
	req := httptest.NewRequest(http.MethodPost, ChatmapPath+"/message", strings.NewReader(`{"TenantID":"chatmap-tenant","ConversationID":"c","PostID":"m1"}`)).WithContext(chatmapHTTPContext(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var shares []chat.LocationShare
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &shares) != nil || len(shares) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestTodo_ISSUE3676_MessageBatch_Refusals(t *testing.T) {
	f := &issue3676MessageSurface{denied: "m2"}
	h := ChatmapHTTP{Surface: f}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"TenantID":"chatmap-tenant","ConversationID":"c","PostIDs":["m1","m2"]}`, http.StatusForbidden},
		{`{"TenantID":"chatmap-tenant","ConversationID":"c","PostIDs":[""]}`, http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatmapPath+"/message", strings.NewReader(tc.body)).WithContext(chatmapHTTPContext(t)))
		if w.Code != tc.status {
			t.Fatal(tc.body, w.Code, w.Body.String())
		}
	}
	ids := make([]string, chatmapMessageBatchLimit+1)
	for i := range ids {
		ids[i] = "p" + strings.Repeat("x", i%3)
	}
	raw, _ := json.Marshal(map[string]any{"TenantID": "chatmap-tenant", "ConversationID": "c", "PostIDs": ids})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatmapPath+"/message", strings.NewReader(string(raw))).WithContext(chatmapHTTPContext(t)))
	if w.Code != http.StatusBadRequest {
		t.Fatal("oversized batch", w.Code)
	}
}
