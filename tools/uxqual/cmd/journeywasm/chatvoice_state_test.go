package main

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatvoiceFakeRecorder struct {
	starts, pauses, resumes, stops, cancels int
	err                                     error
}

type chatvoiceRoundTripper func(*http.Request) (*http.Response, error)

func (f chatvoiceRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (f *chatvoiceFakeRecorder) Start() error  { f.starts++; return f.err }
func (f *chatvoiceFakeRecorder) Pause() error  { f.pauses++; return f.err }
func (f *chatvoiceFakeRecorder) Resume() error { f.resumes++; return f.err }
func (f *chatvoiceFakeRecorder) Stop() error   { f.stops++; return f.err }
func (f *chatvoiceFakeRecorder) Cancel()       { f.cancels++ }
func TestTodo_CHATVOICE_002_Browser(t *testing.T) {
	f := &chatvoiceFakeRecorder{}
	s := voiceRecordingSession{Recorder: f, State: voiceIdle}
	if !errors.Is(s.Start(), errVoiceRecorderState) || f.starts != 0 {
		t.Fatal("prompt before explanation")
	}
	s.Explain()
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if s.State != voiceRequesting {
		t.Fatal("permission request presented as recording")
	}
	if err := s.Tick(5000, 0.5); err != nil || s.ElapsedMS != 0 {
		t.Fatal("permission time counted as recorded audio")
	}
	if err := s.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(30000, 0.3); err != nil {
		t.Fatal(err)
	}
	if err := s.Pause(); err != nil {
		t.Fatal(err)
	}
	_ = s.Tick(60000, 1)
	if s.ElapsedMS != 30000 || s.State != voicePaused {
		t.Fatal("paused time advanced")
	}
	if err := s.Pause(); err != nil {
		t.Fatal(err)
	}
	_ = s.Tick(90000, 0.7)
	if s.State != voicePreview || s.ElapsedMS != 120000 || f.stops != 1 || s.RemainingMS() != 0 {
		t.Fatalf("maximum not enforced %+v %+v", s, f)
	}
	if f.pauses != 1 || f.resumes != 1 {
		t.Fatal("pause/resume not delegated")
	}
	s.Cancel()
	if s.State != voiceIdle || s.ElapsedMS != 0 || s.Waveform != nil || f.cancels != 1 {
		t.Fatal("cancel retained recording")
	}
}
func TestTodo_CHATVOICE_002_Accessibility(t *testing.T) {
	s := voiceRecordingSession{}
	s.Explain()
	if !errors.Is(s.Start(), chat.ErrVoiceUnavailable) || s.State != voiceNoMicrophone {
		t.Fatalf("missing device state=%s", s.State)
	}
	f := &chatvoiceFakeRecorder{err: errors.New("device refused")}
	s.Recorder = f
	s.State = voiceDenied
	if err := s.Start(); err == nil || s.State != voiceFailed {
		t.Fatal("refusal not visible")
	}
}
func TestTodo_CHATVOICE_004(t *testing.T) {
	p := newVoicePlayback()
	if !p.Collapsed || p.Speed != 1 || p.Active != "" {
		t.Fatal("unsafe default playback")
	}
	if previous := p.Play("first"); previous != "" {
		t.Fatal(previous)
	}
	if err := p.Seek("first", 20, 120); err != nil {
		t.Fatal(err)
	}
	if previous := p.Play("second"); previous != "first" {
		t.Fatal("one active player lost")
	}
	p.Pause("first")
	if p.Active != "second" {
		t.Fatal("inactive pause stops active message")
	}
	if p.Positions["first"] != 20 {
		t.Fatal("position reset on virtualization")
	}
	if err := p.SetSpeed(1.5); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSpeed(4); !errors.Is(err, errVoiceRecorderState) || p.Speed != 1.5 {
		t.Fatal("invalid speed accepted")
	}
	if err := p.Seek("second", 121, 120); !errors.Is(err, errVoiceRecorderState) {
		t.Fatal("invalid seek accepted")
	}
	for _, tc := range []struct {
		position      float64
		playing, want bool
	}{{0, true, true}, {999, true, true}, {1000, true, false}, {500, false, false}, {math.NaN(), true, false}} {
		if voiceSegmentActive(tc.position, 0, 1000, tc.playing) != tc.want {
			t.Fatalf("segment highlight at %v playing=%v", tc.position, tc.playing)
		}
	}
	encoded := voicePreferenceEncode(p)
	if prefs := voicePreferenceDecode(encoded); prefs.Speed != 1.5 || !prefs.Collapsed {
		t.Fatalf("preference round-trip=%+v", prefs)
	}
	for _, raw := range []string{"", "malformed", `{"Speed":9,"Collapsed":false}`} {
		if prefs := voicePreferenceDecode(raw); prefs.Speed != 1 || !prefs.Collapsed {
			t.Fatalf("invalid preference=%+v", prefs)
		}
	}
}
func TestTodo_CHATVOICE_002_HTTPContract(t *testing.T) {
	var path string
	client := &http.Client{Transport: chatvoiceRoundTripper(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Method != "POST" || r.URL.RawQuery != "" {
			t.Error("session or method missing")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Post":{"ID":"voice-1"}}`))}, nil
	})}
	var out voiceSendReply
	err := voiceRequest(context.Background(), client, journeyclient.Config{TunnelURL: "http://fixture.invalid/rpc?spoofed=1", Bearer: "fixture"}, "send", voiceSendInput{Content: []byte("audio")}, &out)
	if err != nil || out.Post.ID != "voice-1" || path != "/api/chat/voice/send" {
		t.Fatalf("reply=%+v path=%s err=%v", out, path, err)
	}
}
func TestTodo_CHATVOICE_004_Security(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, expires := range []time.Time{{}, now, now.Add(-time.Second)} {
		if _, valid := voiceGrantTTL(now, expires); valid {
			t.Fatal("expired grant accepted")
		}
	}
	if ttl, valid := voiceGrantTTL(now, now.Add(time.Minute)); !valid || ttl != time.Minute {
		t.Fatal("live grant refused")
	}
	for _, raw := range []string{"https://untrusted.example/audio", "javascript:alert(1)", "data:audio/webm;base64,a"} {
		if voiceOriginSafe(raw) {
			t.Fatal("external source accepted")
		}
	}
	client := &http.Client{Transport: chatvoiceRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	err := voiceRequest(context.Background(), client, journeyclient.Config{TunnelURL: "http://fixture.invalid", Bearer: "fixture"}, "read", nil, new(any))
	if !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal(err)
	}
	if err = voiceRequest(context.Background(), client, journeyclient.Config{TunnelURL: "file:///private", Bearer: "fixture"}, "read", nil, nil); !errors.Is(err, chat.ErrVoiceUnavailable) {
		t.Fatal(err)
	}
	if !voiceOriginSafe("blob:fixture") || !voiceOriginSafe("/v1/chat/media/id") {
		t.Fatal("protected media sources refused")
	}
}
