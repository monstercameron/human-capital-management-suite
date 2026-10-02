package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatmapLiveFixture struct {
	chatmapSurfaceFixture
	updated    chat.LocationPlace
	attached   chat.AttachLocationRequest
	ended      string
	policy     chatmapPolicyWire
	subjectFor string
}

func (f *chatmapLiveFixture) Attach(_ context.Context, r chat.AttachLocationRequest) (chat.LocationShare, error) {
	f.attached = r
	return chat.LocationShare{Version: 1, Live: r.Live}, nil
}
func (f *chatmapLiveFixture) UpdateLive(_ context.Context, p chat.Principal, _ chat.LocationKey, place chat.LocationPlace) (chat.LocationShare, error) {
	f.updated, f.subjectFor = place, p.SubjectID
	return chat.LocationShare{Version: 1, Live: true}, nil
}
func (f *chatmapLiveFixture) MyShares(_ context.Context, p chat.Principal) ([]chat.LocationShare, error) {
	f.subjectFor = p.SubjectID
	return []chat.LocationShare{}, nil
}
func (f *chatmapLiveFixture) EndMine(_ context.Context, _ chat.Principal, _, reason string) (int64, error) {
	f.ended = reason
	return 1, nil
}
func (f *chatmapLiveFixture) LiveMap(context.Context, chat.Principal, string, string) (chat.LiveMapView, error) {
	return chat.LiveMapView{Shares: []chat.LocationShare{}, Sites: []chat.LocationSite{}}, nil
}
func (f *chatmapLiveFixture) LocationPolicyFor(context.Context, chat.Principal, string, string) (chatmapPolicyWire, error) {
	return chatmapPolicyToWire(chat.DefaultLocationPolicy()), nil
}
func (f *chatmapLiveFixture) SetLocationPolicy(_ context.Context, _ chat.Principal, _, _ string, w chatmapPolicyWire) error {
	f.policy = w
	return nil
}

func chatmapPost(t *testing.T, h ChatmapHTTP, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatmapPath+"/"+action, strings.NewReader(body)).WithContext(chatmapHTTPContext(t)))
	return w
}

func TestTodo_CHATMAP_005_HTTP(t *testing.T) {
	f := &chatmapLiveFixture{}
	h := ChatmapHTTP{Surface: f}
	scope := `"TenantID":"chatmap-tenant","ConversationID":"c","PostID":"m","ID":"s"`
	if w := chatmapPost(t, h, "attach", `{`+scope+`,"PostRevision":1,"Live":true,"LiveIntervalSeconds":30}`); w.Code != 200 || !f.attached.Live || f.attached.LiveInterval != 30*time.Second {
		t.Fatal("attach live", w.Code, w.Body.String())
	}
	// The principal always comes from the session, never from the body.
	if w := chatmapPost(t, h, "update", `{`+scope+`,"Place":{"Source":"device"}}`); w.Code != 200 || f.subjectFor != "alice" {
		t.Fatal("update", w.Code, w.Body.String())
	}
	if w := chatmapPost(t, h, "mine", `{}`); w.Code != 200 || f.subjectFor != "alice" || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("mine", w.Code, w.Body.String())
	}
	if w := chatmapPost(t, h, "endmine", `{"Reason":"signed_out"}`); w.Code != 200 || f.ended != "signed_out" {
		t.Fatal("endmine", w.Code, w.Body.String())
	}
	if w := chatmapPost(t, h, "map", `{`+scope+`}`); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("map", w.Code, w.Body.String())
	}
	if w := chatmapPost(t, h, "policy", `{`+scope+`}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"MaxLiveSeconds":28800`) {
		t.Fatal("policy", w.Code, w.Body.String())
	}
	if w := chatmapPost(t, h, "setpolicy", `{`+scope+`,"Policy":{"SharingEnabled":true,"MaxLiveSeconds":900,"MaxRetentionSeconds":3600}}`); w.Code != 200 || f.policy.MaxLiveSeconds != 900 {
		t.Fatal("setpolicy", w.Code, w.Body.String())
	}
	// A surface without the capability says so instead of succeeding.
	bare := ChatmapHTTP{Surface: &chatmapSurfaceFixture{}}
	for _, action := range []string{"update", "mine", "endmine", "map", "policy", "setpolicy", "setjurisdiction"} {
		if w := chatmapPost(t, bare, action, `{}`); w.Code != 503 {
			t.Fatal(action, "answered without the capability", w.Code)
		}
	}
}
