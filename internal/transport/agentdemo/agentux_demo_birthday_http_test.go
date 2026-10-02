package agentdemo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type agentuxDemoBirthdaySurfaceFixture struct {
	calls int
	pref  BirthdayPreference
}

func (f *agentuxDemoBirthdaySurfaceFixture) ReadBirthdayPreference(context.Context) (BirthdayPreference, error) {
	f.calls++
	return BirthdayPreference{ShareBirthday: true}, nil
}
func (f *agentuxDemoBirthdaySurfaceFixture) SaveBirthdayPreference(_ context.Context, p BirthdayPreference) (BirthdayPreference, error) {
	f.calls++
	f.pref = p
	p.Revision++
	return p, nil
}

func TestAgentUXDemo_BirthdayHTTP_Security(t *testing.T) {
	f := &agentuxDemoBirthdaySurfaceFixture{}
	h := BirthdayPreferenceHandler{Surface: f}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, BirthdayPreferencePath, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"share_birthday":true`) {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, BirthdayPreferencePath, strings.NewReader(`{"share_birthday":false,"revision":0}`)))
	if w.Code != 200 || f.pref.ShareBirthday || f.pref.Revision != 0 || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatalf("save false: %d %s", w.Code, w.Body)
	}
	before := f.calls
	for _, body := range []string{`{}`, `{"share_birthday":false}`, `{"share_birthday":true,"revision":0,"worker_key":"someone-else"}`, `{"share_birthday":false,"revision":0} {}`} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, BirthdayPreferencePath, strings.NewReader(body)))
		if w.Code != 400 || f.calls != before {
			t.Fatalf("forged self scope: %d %s", w.Code, w.Body)
		}
	}
}
