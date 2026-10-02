package agentmodel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

type audioGuardFake struct {
	mu       sync.Mutex
	refuse   error
	admitted []AudioCall
	settled  []int64
}

func (g *audioGuardFake) Admit(_ context.Context, c AudioCall) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refuse != nil {
		return g.refuse
	}
	g.admitted = append(g.admitted, c)
	return nil
}
func (g *audioGuardFake) Settle(_ context.Context, _ AudioCall, cost int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settled = append(g.settled, cost)
	return nil
}

func audioFixture(t *testing.T, handler http.HandlerFunc, guard AudioGuard) *AudioPort {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	port, err := NewAudioPort(AudioConfig{APIKey: "fixture-key", BaseURL: server.URL, HTTPClient: server.Client(),
		TranscribeModel: ModelIdentity{ProviderID: "openai", ModelID: "whisper-1", Version: "2026-01"},
		SpeechModel:     ModelIdentity{ProviderID: "openai", ModelID: "gpt-4o-mini-tts", Version: "2026-01"},
		Price:           AudioPrice{TranscribeMicrosPerSecond: 100, SpeechMicrosPerThousandChars: 15000}, Guard: guard})
	if err != nil {
		t.Fatal(err)
	}
	return port
}

const audioTranscriptionJSON = `{"language":"german","duration":3.0,"text":"Guten Morgen zusammen","segments":[{"start":0,"end":1.5,"text":" Guten Morgen","avg_logprob":-0.1},{"start":1.4,"end":9.0,"text":" zusammen","avg_logprob":-1.2}]}`

func TestChatvoiceOpsTranscribe(t *testing.T) {
	guard := &audioGuardFake{}
	var seen struct{ auth, model, format, filename string }
	port := audioFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		seen.auth, seen.model, seen.format = r.Header.Get("Authorization"), r.FormValue("model"), r.FormValue("response_format")
		_, header, _ := r.FormFile("file")
		seen.filename = header.Filename
		_, _ = io.WriteString(w, audioTranscriptionJSON)
	}, guard)
	out, err := port.Transcribe(context.Background(), TranscribeRequest{Scope: AudioScope{TenantID: "t", ConversationID: "c", Actor: "worker"}, Audio: []byte("opus bytes"), ContentType: "audio/webm;codecs=opus", DurationMS: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if seen.auth != "Bearer fixture-key" || seen.model != "whisper-1" || seen.format != "verbose_json" || seen.filename != "voice.webm" {
		t.Fatalf("request %+v", seen)
	}
	if out.Language != "de" || len(out.Segments) != 2 || out.Model.ModelID != "whisper-1" || out.Model.Version != "2026-01" {
		t.Fatalf("result %+v", out)
	}
	if out.Segments[1].StartMS != 1500 || out.Segments[1].EndMS != 3000 {
		t.Fatalf("segments are not ordered and clamped to the decoded duration: %+v", out.Segments)
	}
	if out.Segments[0].Confidence < 0.9 || out.Segments[1].Confidence > 0.31 {
		t.Fatalf("confidence is not the provider's figure: %+v", out.Segments)
	}
	if len(guard.admitted) != 1 || guard.admitted[0].Kind != AudioTranscribe || guard.admitted[0].TenantID != "t" || guard.admitted[0].EstimatedCostMicros != 300 {
		t.Fatalf("admitted %+v", guard.admitted)
	}
	if len(guard.settled) != 1 || guard.settled[0] != 300 || out.CostMicros != 300 {
		t.Fatalf("settled %+v cost %d", guard.settled, out.CostMicros)
	}
}

func TestChatvoiceOpsSpeak(t *testing.T) {
	guard := &audioGuardFake{}
	var body string
	port := audioFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3 speech"))
	}, guard)
	out, err := port.Speak(context.Background(), SpeakRequest{Scope: AudioScope{TenantID: "t", ConversationID: "c", Actor: "alice"}, Text: "Hello there"})
	if err != nil || out.ContentType != "audio/mpeg" || string(out.Audio) != "ID3 speech" {
		t.Fatalf("speech %+v err %v", out, err)
	}
	if !strings.Contains(body, `"model":"gpt-4o-mini-tts"`) || !strings.Contains(body, `"response_format":"mp3"`) || !strings.Contains(body, `"voice":"alloy"`) {
		t.Fatalf("body %s", body)
	}
	if out.CostMicros != 165 || len(guard.settled) != 1 {
		t.Fatalf("cost %d settled %v", out.CostMicros, guard.settled)
	}
}

// A refused call never reaches the provider; a provider failure never becomes a
// transcript, and nothing falls back to another model.
func TestChatvoiceOpsRefusalsAndFailures(t *testing.T) {
	calls := 0
	refusing := &audioGuardFake{refuse: errors.New("channel barred external services")}
	port := audioFixture(t, func(http.ResponseWriter, *http.Request) { calls++ }, refusing)
	_, err := port.Transcribe(context.Background(), TranscribeRequest{Audio: []byte("x"), ContentType: "audio/ogg", DurationMS: 1000})
	if !errors.Is(err, ErrAudioRefused) || calls != 0 {
		t.Fatalf("refusal err=%v calls=%d", err, calls)
	}
	if _, err = port.Speak(context.Background(), SpeakRequest{Text: "hi"}); !errors.Is(err, ErrAudioRefused) || calls != 0 {
		t.Fatalf("speak refusal err=%v calls=%d", err, calls)
	}
	guard := &audioGuardFake{}
	failing := audioFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, guard)
	if _, err = failing.Transcribe(context.Background(), TranscribeRequest{Audio: []byte("x"), ContentType: "audio/ogg", DurationMS: 1000}); !errors.Is(err, ErrAudioUnavailable) {
		t.Fatalf("provider failure err=%v", err)
	}
	empty := audioFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"language":"english","text":"just text"}`)
	}, guard)
	if _, err = empty.Transcribe(context.Background(), TranscribeRequest{Audio: []byte("x"), ContentType: "audio/ogg", DurationMS: 1000}); !errors.Is(err, ErrAudioInvalid) {
		t.Fatalf("an answer without segments must be refused, got %v", err)
	}
	for _, bad := range []TranscribeRequest{{Audio: nil, ContentType: "audio/ogg", DurationMS: 1000}, {Audio: []byte("x"), ContentType: "audio/mpeg", DurationMS: 1000}, {Audio: []byte("x"), ContentType: "audio/ogg", DurationMS: 120001}} {
		if _, err = failing.Transcribe(context.Background(), bad); !errors.Is(err, ErrAudioInvalid) {
			t.Fatalf("request %+v err=%v", bad, err)
		}
	}
	if _, err = failing.Speak(context.Background(), SpeakRequest{Text: strings.Repeat("x", AudioMaxSpeechCharacters+1)}); !errors.Is(err, ErrAudioInvalid) {
		t.Fatalf("oversize text err=%v", err)
	}
}

func TestChatvoiceOpsConfiguration(t *testing.T) {
	good := AudioConfig{APIKey: "k", TranscribeModel: ModelIdentity{ProviderID: "openai", ModelID: "m", Version: "1"}, SpeechModel: ModelIdentity{ProviderID: "openai", ModelID: "s", Version: "1"}, Price: AudioPrice{1, 1}, Guard: &audioGuardFake{}}
	if _, err := NewAudioPort(good); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AudioConfig){
		"no key":        func(c *AudioConfig) { c.APIKey = " " },
		"no guard":      func(c *AudioConfig) { c.Guard = nil },
		"no price":      func(c *AudioConfig) { c.Price = AudioPrice{} },
		"no model":      func(c *AudioConfig) { c.TranscribeModel = ModelIdentity{} },
		"other vendor":  func(c *AudioConfig) { c.SpeechModel.ProviderID = "other" },
		"plain http":    func(c *AudioConfig) { c.BaseURL = "http://api.example.com/v1" },
		"credentials":   func(c *AudioConfig) { c.BaseURL = "https://user:pw@api.example.com/v1" },
		"unversioned":   func(c *AudioConfig) { c.TranscribeModel.Version = "" },
		"no models set": func(c *AudioConfig) { c.TranscribeModel, c.SpeechModel = ModelIdentity{}, ModelIdentity{} },
	} {
		cfg := good
		mutate(&cfg)
		if _, err := NewAudioPort(cfg); !errors.Is(err, ErrAudioNotConfigured) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	var nilPort *AudioPort
	if _, err := nilPort.Speak(context.Background(), SpeakRequest{Text: "x"}); !errors.Is(err, ErrAudioNotConfigured) {
		t.Fatal("nil port spoke")
	}
}

// The credential never follows a redirect to another authority.
func TestChatvoiceOpsDoesNotFollowRedirects(t *testing.T) {
	hits := 0
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer elsewhere.Close()
	port := audioFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}, &audioGuardFake{})
	if _, err := port.Speak(context.Background(), SpeakRequest{Text: "hi"}); !errors.Is(err, ErrAudioUnavailable) || hits != 0 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

// TestChatvoiceLiveSmoke is the owner's live check. It does nothing unless
// HCMNEXT_CHATVOICE_LIVE_SMOKE=1 and MODEL_API_KEY are set, and it calls the
// real provider through the same port the product uses: it speaks one short
// sentence as Ogg Opus, then transcribes it.
func TestChatvoiceLiveSmoke(t *testing.T) {
	key := os.Getenv("MODEL_API_KEY")
	if os.Getenv("HCMNEXT_CHATVOICE_LIVE_SMOKE") != "1" || key == "" {
		t.Skip("live smoke is opt-in: set HCMNEXT_CHATVOICE_LIVE_SMOKE=1 and MODEL_API_KEY")
	}
	transcribe, speech := os.Getenv("HCMNEXT_CHATVOICE_TRANSCRIBE_MODEL"), os.Getenv("HCMNEXT_CHATVOICE_SPEECH_MODEL")
	if transcribe == "" {
		transcribe = DefaultTranscribeModel
	}
	if speech == "" {
		speech = DefaultSpeechModel
	}
	guard := &audioGuardFake{}
	port, err := NewAudioPort(AudioConfig{APIKey: key, BaseURL: os.Getenv("HCMNEXT_CHATVOICE_BASE_URL"),
		TranscribeModel: ModelIdentity{ProviderID: "openai", ModelID: transcribe, Version: "live"},
		SpeechModel:     ModelIdentity{ProviderID: "openai", ModelID: speech, Version: "live"},
		Price:           AudioPrice{TranscribeMicrosPerSecond: 100, SpeechMicrosPerThousandChars: 15000}, Guard: guard})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scope := AudioScope{TenantID: "smoke", ConversationID: "smoke", Actor: "smoke"}
	clip, err := port.Speak(ctx, SpeakRequest{Scope: scope, Text: "The quarterly review is on Thursday morning.", Opus: true})
	if err != nil || len(clip.Audio) == 0 {
		t.Fatalf("speak: %v", err)
	}
	out, err := port.Transcribe(ctx, TranscribeRequest{Scope: scope, Audio: clip.Audio, ContentType: clip.ContentType, DurationMS: 6000})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	var text []string
	for _, s := range out.Segments {
		text = append(text, s.Text)
	}
	t.Logf("language=%s model=%s segments=%d text=%q cost_micros=%d", out.Language, out.Model.ModelID, len(out.Segments), strings.Join(text, " "), out.CostMicros+clip.CostMicros)
	if !strings.Contains(strings.ToLower(strings.Join(text, " ")), "thursday") {
		t.Fatalf("round trip lost the sentence: %q", text)
	}
}
