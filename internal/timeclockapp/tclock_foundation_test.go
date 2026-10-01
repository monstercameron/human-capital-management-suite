package timeclockapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

func TestTodo_TCLOCK_016_FoundationContracts(t *testing.T) {
	q := Queue{{Sequence: 2, OccurredAt: time.Unix(20, 0)}, {Sequence: 1, OccurredAt: time.Unix(10, 0)}}
	if q.Sorted()[0].Sequence != 1 || q.LastSequence() != 2 || len(q.Trim(1)) != 1 || q.OldestAge(time.Unix(30, 0)) != 20*time.Second {
		t.Fatalf("queue contract failed")
	}
	if len(q.Proto()) != 2 {
		t.Fatalf("proto queue length = %d", len(q.Proto()))
	}
	store := &tclockStorage{values: map[string]string{keyDevice: "{", keySequence: "bad", keyQueue: "[]"}}
	if !loadState(store).corrupt {
		t.Fatal("corrupt storage was trusted")
	}
	if err := saveDevice(&tclockStorage{fail: true}, DeviceRecord{DeviceID: "d"}); !errors.Is(err, ErrStorage) {
		t.Fatalf("saveDevice error = %v", err)
	}
	if err := saveQueue(&tclockStorage{fail: true}, nil, 1); !errors.Is(err, ErrStorage) {
		t.Fatalf("saveQueue error = %v", err)
	}
	if LocaleDeDE.Face(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), time.UTC).HourMinute != "03:04" {
		t.Fatal("24-hour face failed")
	}
	if !strings.Contains(LocaleEnUS.Date(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), time.UTC), "January") {
		t.Fatal("date format failed")
	}
	if SiteLocation("bad/zone") != time.UTC {
		t.Fatal("bad timezone did not fall back")
	}
}

type tclockHTTPDoer struct {
	response *http.Response
	err      error
}

func (d tclockHTTPDoer) Do(*http.Request) (*http.Response, error) { return d.response, d.err }

func TestTodo_TCLOCK_016_HTTPBoundary(t *testing.T) {
	good := NewHTTPClient("http://kiosk", tclockHTTPDoer{response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}})
	if _, err := good.Heartbeat(context.Background(), &timev1.HeartbeatRequest{}); err != nil {
		t.Fatalf("success HTTP error = %v", err)
	}
	bad := NewHTTPClient("http://kiosk", tclockHTTPDoer{response: &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"code":"bad","message":"no"}`))}})
	if _, err := bad.Heartbeat(context.Background(), &timev1.HeartbeatRequest{}); err == nil {
		t.Fatal("bad HTTP response accepted")
	}
	client := NewHTTPClient("http://kiosk/", tclockHTTPDoer{response: &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}})
	if _, err := client.Heartbeat(context.Background(), &timev1.HeartbeatRequest{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("offline HTTP error = %v", err)
	}
	if _, err := client.URL("Admin"); err == nil {
		t.Fatal("admin URL accepted")
	}
	client = NewHTTPClient("http://kiosk", tclockHTTPDoer{err: errors.New("network")})
	if _, err := client.Heartbeat(context.Background(), &timev1.HeartbeatRequest{}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("network HTTP error = %v", err)
	}
}
