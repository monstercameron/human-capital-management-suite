package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3920Port struct {
	reads, puts int
	principal   chat.Principal
	hours       chatrecipient.QuietHours
	saved       chatrecipient.QuietSchedule
	expected    uint64
	err         error
	putErr      error
}

func (f *issue3920Port) QuietHours(_ context.Context, p chat.Principal) (chatrecipient.QuietHours, error) {
	f.reads++
	f.principal = p
	return f.hours, f.err
}
func (f *issue3920Port) QuietSchedule(_ context.Context, p chat.Principal, _ time.Time) (chatrecipient.QuietSchedule, error) {
	f.principal = p
	return f.hours.Schedule, f.err
}
func (f *issue3920Port) PutQuietSchedule(_ context.Context, p chat.Principal, s chatrecipient.QuietSchedule, expected uint64, _ time.Time) (chatrecipient.QuietSchedule, error) {
	f.puts++
	f.principal, f.saved, f.expected = p, s, expected
	if f.putErr != nil {
		return chatrecipient.QuietSchedule{}, f.putErr
	}
	s.Revision = expected + 1
	f.hours.Schedule = s
	return s, nil
}

func issue3920Hours() chatrecipient.QuietHours {
	week := chatrecipient.QuietWeek{Active: true}
	for i := range week.Days {
		week.Days[i] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayNone}
	}
	week.Days[time.Friday] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	week.Days[time.Saturday] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayAll}
	return chatrecipient.QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 1320, EndMinute: 420, Revision: 4,
		Schedule: chatrecipient.QuietSchedule{Week: week, Revision: 3}}
}

func issue3920Handler(port ChatQuietSchedulePort) ChatQuietScheduleHandler {
	return ChatQuietScheduleHandler{Service: port, Now: func() time.Time { return time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC) }}
}

func TestIssue3920_QuietScheduleHTTP_ReadsTheSessionsOwnSchedule(t *testing.T) {
	port := &issue3920Port{hours: issue3920Hours()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, ChatQuietSchedulePath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3920Handler(port).ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if port.principal.SubjectID != "person" {
		t.Fatalf("identity not taken from the session: %+v", port.principal)
	}
	var reply ChatQuietScheduleReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if !reply.WeekActive || len(reply.Days) != 7 || reply.Revision != 3 || reply.Timezone != "UTC" || !reply.Enabled || reply.StartMinute != 1320 {
		t.Fatalf("reply=%s", w.Body.String())
	}
	if d := reply.Days[time.Friday]; d.Mode != "interval" || d.Start != 22*60 || d.End != 7*60 {
		t.Fatalf("friday=%+v", d)
	}
	if reply.Days[time.Saturday].Mode != "all" || reply.Days[time.Tuesday].Mode != "none" {
		t.Fatalf("days=%+v", reply.Days)
	}
	if reply.QuietNow || reply.Next == nil || !reply.Next.Quiet || !reply.Next.At.Equal(time.Date(2026, 3, 6, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("next pause = %+v quietNow=%v", reply.Next, reply.QuietNow)
	}
}

func TestIssue3920_QuietScheduleHTTP_SavesWithTheRevisionTheClientRead(t *testing.T) {
	port := &issue3920Port{hours: issue3920Hours()}
	w := httptest.NewRecorder()
	body := `{"expected_revision":3,"week_active":true,"days":[{"mode":"none"},{"mode":"interval","start":1320,"end":420},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"all"}],"pause":{"kind":"pause","until":"2026-03-05T12:00:00Z"}}`
	r := httptest.NewRequest(http.MethodPost, ChatQuietSchedulePath, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3920Handler(port).ServeHTTP(w, r)
	if w.Code != 200 || port.puts != 1 || port.expected != 3 || port.principal.SubjectID != "person" {
		t.Fatalf("status=%d body=%s port=%+v", w.Code, w.Body.String(), port)
	}
	if !port.saved.Week.Active || port.saved.Week.Days[time.Monday].Mode != chatrecipient.QuietDayInterval || port.saved.Week.Days[time.Saturday].Mode != chatrecipient.QuietDayAll ||
		port.saved.Pause.Kind != chatrecipient.PausePause || !port.saved.Pause.Until.Equal(time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("saved=%+v", port.saved)
	}
	var reply ChatQuietScheduleReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Revision != 4 || reply.Pause.Kind != "pause" || !reply.QuietNow {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
}

func TestIssue3920_QuietScheduleHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	set := `{"expected_revision":1,"week_active":false,"days":[{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"}]}`
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		body   string
		port   *issue3920Port
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, "", &issue3920Port{}, 401, "unauthenticated"},
		{"service account reads", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, "", &issue3920Port{}, 403, "permission_denied"},
		{"service account writes", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, set, &issue3920Port{}, 403, "permission_denied"},
		{"delete method", human, http.MethodDelete, "", &issue3920Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, "", nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, "", &issue3920Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"stale revision", human, http.MethodPost, set, &issue3920Port{putErr: chat.ErrConflict}, 409, "conflict"},
		{"ambiguous week", human, http.MethodPost, set, &issue3920Port{putErr: &chatrecipient.WeekError{Day: time.Tuesday, Reason: chatrecipient.WeekOverlap}}, 400, "invalid_argument"},
		{"six days", human, http.MethodPost, `{"expected_revision":1,"days":[{"mode":"none"}]}`, &issue3920Port{}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, `{"expected_revision":1,"subject":"x"}`, &issue3920Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, set + set, &issue3920Port{}, 400, "invalid_argument"},
		{"bad pause time", human, http.MethodPost, `{"expected_revision":1,"days":[],"pause":{"kind":"pause","until":"tomorrow"}}`, &issue3920Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		var service ChatQuietSchedulePort
		if c.port != nil {
			service = c.port
		}
		issue3920Handler(service).ServeHTTP(w, httptest.NewRequest(c.method, ChatQuietSchedulePath, strings.NewReader(c.body)).WithContext(c.ctx))
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
	}
	// The refusal names the day and the reason, never a cause.
	w := httptest.NewRecorder()
	port := &issue3920Port{putErr: &chatrecipient.WeekError{Day: time.Tuesday, Reason: chatrecipient.WeekOverlap}}
	issue3920Handler(port).ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatQuietSchedulePath, strings.NewReader(set)).WithContext(human))
	if !strings.Contains(w.Body.String(), `"reason":"overlap"`) || !strings.Contains(w.Body.String(), `"day":2`) {
		t.Fatalf("refusal does not say which day and why: %s", w.Body.String())
	}
	// Nothing reached the service from a refused body.
	port = &issue3920Port{}
	w = httptest.NewRecorder()
	issue3920Handler(port).ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatQuietSchedulePath, strings.NewReader(`{"junk":1}`)).WithContext(human))
	if port.puts+port.reads != 0 {
		t.Fatal("a refused request reached the service")
	}
}
