package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var issue3920Config = journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}

const issue3920Days = `[{"mode":"none","start":0,"end":0},{"mode":"interval","start":1320,"end":420},{"mode":"none","start":0,"end":0},{"mode":"none","start":0,"end":0},{"mode":"none","start":0,"end":0},{"mode":"none","start":0,"end":0},{"mode":"all","start":0,"end":0}]`

func issue3920Reply(extra string) string {
	return `{"revision":3,"week_active":true,"days":` + issue3920Days + `,"pause":{"kind":""},"quiet_now":false,"next":{"at":"2026-03-09T22:00:00Z","quiet":true}` + extra + `}`
}

func TestIssue3920_ClientReadsOnlyTheSignedInPersonsSchedule(t *testing.T) {
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/quiet-schedule" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.RawQuery != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(issue3920Reply(""))), Header: http.Header{}}, nil
	})}
	view, err := chatQuietScheduleRead(context.Background(), client, issue3920Config)
	if err != nil || !view.Loaded || !view.WeekActive || view.Revision != 3 || view.Days[time.Monday].Mode != "interval" ||
		view.Days[time.Monday].StartMinute != 1320 || view.Days[time.Saturday].Mode != "all" || !view.HasNext || !view.NextQuiet ||
		!view.NextAt.Equal(time.Date(2026, 3, 9, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatQuietScheduleRead(context.Background(), client, issue3920Config); err == nil {
		t.Fatal("a refusal must not read as an empty schedule")
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"revision":1,"days":[]}`))}, nil
	})
	if _, err := chatQuietScheduleRead(context.Background(), client, issue3920Config); err == nil {
		t.Fatal("an answer without seven days was accepted")
	}
	if _, err := chatQuietScheduleRead(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3920_PauseCarriesAnExplicitEnd(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	edit := chatui.QuietScheduleEdit{WeekActive: true, PauseKind: chatui.QuietPausePause, PauseMinutes: 240}
	body := chatQuietScheduleBody(edit, 7, now)
	if body.ExpectedRevision != 7 || !body.WeekActive || len(body.Days) != 7 || body.Days[0].Mode != "none" {
		t.Fatalf("body=%+v", body)
	}
	if body.Pause.Kind != "pause" || body.Pause.Until == nil || !body.Pause.Until.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("a new pause runs from now for the chosen length: %+v", body.Pause)
	}
	// A pause already running keeps its own end; an ended or cleared one is none.
	running := chatQuietScheduleBody(chatui.QuietScheduleEdit{PauseKind: chatui.QuietPauseNotify, PauseUntil: now.Add(30 * time.Minute)}, 1, now)
	if running.Pause.Kind != "notify" || !running.Pause.Until.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("running pause = %+v", running.Pause)
	}
	for name, e := range map[string]chatui.QuietScheduleEdit{
		"ended":   {PauseKind: chatui.QuietPausePause, PauseUntil: now.Add(-time.Minute)},
		"cleared": {},
		"no end":  {PauseKind: chatui.QuietPausePause},
	} {
		if got := chatQuietScheduleBody(e, 1, now); got.Pause.Kind != "" || got.Pause.Until != nil {
			t.Errorf("%s: sent a pause %+v", name, got.Pause)
		}
	}
}

func TestIssue3920_SaveIsSentWithTheRevisionAndSortsTheAnswer(t *testing.T) {
	var got map[string]any
	status, answer := 200, issue3920Reply("")
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/quiet-schedule" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		got = nil
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(answer)), Header: http.Header{}}, nil
	})}
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	edit := chatui.QuietScheduleEdit{WeekActive: true}
	view, outcome, _ := chatQuietScheduleWrite(context.Background(), client, issue3920Config, edit, 2, now)
	if outcome != "saved" || view.Revision != 3 || got["expected_revision"] != float64(2) || got["week_active"] != true {
		t.Fatalf("saved: %s %+v body=%v", outcome, view, got)
	}
	for _, field := range []string{"subject", "tenant", "host", "person"} {
		if _, sent := got[field]; sent {
			t.Fatalf("the request names %q: the server takes the person from the session", field)
		}
	}
	status, answer = 409, `{"code":"conflict"}`
	if _, outcome, _ = chatQuietScheduleWrite(context.Background(), client, issue3920Config, edit, 2, now); outcome != "conflict" {
		t.Fatalf("stale revision = %s", outcome)
	}
	status, answer = 400, `{"code":"invalid_argument","day":2,"reason":"overlap"}`
	if _, outcome, day := chatQuietScheduleWrite(context.Background(), client, issue3920Config, edit, 2, now); outcome != "overlap" || day != 2 {
		t.Fatalf("overlap = %s day %d", outcome, day)
	}
	status, answer = 400, `{"code":"invalid_argument","day":3,"reason":"empty_interval"}`
	if _, outcome, day := chatQuietScheduleWrite(context.Background(), client, issue3920Config, edit, 2, now); outcome != "empty_interval" || day != 3 {
		t.Fatalf("empty interval = %s day %d", outcome, day)
	}
	for _, s := range []int{400, 403, 503} {
		status, answer = s, `{"code":"unavailable"}`
		if _, outcome, _ = chatQuietScheduleWrite(context.Background(), client, issue3920Config, edit, 2, now); outcome != "failed" {
			t.Fatalf("status %d = %s, want failed", s, outcome)
		}
	}
	if _, outcome, _ = chatQuietScheduleWrite(context.Background(), client, journeyclient.Config{}, edit, 2, now); outcome != "failed" {
		t.Fatalf("no tunnel = %s", outcome)
	}
}

func TestIssue3920_SoundFollowsTheWeeklyScheduleAndPause(t *testing.T) {
	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) // a Saturday
	daily := chatui.Preferences{}                       // the daily interval is off
	view := chatui.QuietScheduleView{Loaded: true, WeekActive: true, QuietNow: true, HasNext: true, NextAt: now.Add(12 * time.Hour), NextQuiet: false}
	model := chatui.Model{Preferences: daily, QuietSchedule: view}
	if !chatQuietNow(model, now) {
		t.Fatal("a quiet Saturday still plays sounds")
	}
	// Once the next resume has passed, sounds are back without waiting for a reload.
	if chatQuietNow(model, now.Add(13*time.Hour)) {
		t.Fatal("sounds stayed off after the schedule resumed")
	}
	// A pause silences sounds even with no recurring schedule, until its end.
	model.QuietSchedule = chatui.QuietScheduleView{Loaded: true, PauseKind: chatui.QuietPausePause, PauseUntil: now.Add(time.Hour), QuietNow: true, HasNext: true, NextAt: now.Add(time.Hour)}
	if !chatQuietNow(model, now) || chatQuietNow(model, now.Add(2*time.Hour)) {
		t.Fatal("a pause did not silence sounds until its end")
	}
	// A temporary override lifts the schedule's quiet time.
	model.QuietSchedule = chatui.QuietScheduleView{Loaded: true, WeekActive: true, PauseKind: chatui.QuietPauseNotify, PauseUntil: now.Add(time.Hour), QuietNow: false, HasNext: true, NextAt: now.Add(time.Hour), NextQuiet: true}
	if chatQuietNow(model, now) {
		t.Fatal("an override did not lift quiet time")
	}
	// Without a weekly schedule or pause, the daily interval decides as before.
	model = chatui.Model{Preferences: chatui.Preferences{QuietHours: true, QuietTimezone: "UTC", QuietStartMinute: 11 * 60, QuietEndMinute: 13 * 60}, QuietSchedule: chatui.QuietScheduleView{Loaded: true}}
	if !chatQuietNow(model, now) {
		t.Fatal("the daily interval stopped applying")
	}
	// A schedule that never loaded changes nothing.
	model.QuietSchedule = chatui.QuietScheduleView{}
	if !chatQuietNow(model, now) {
		t.Fatal("an unloaded schedule changed the daily answer")
	}
}
