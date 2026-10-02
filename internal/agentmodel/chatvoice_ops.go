package agentmodel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// This file is the audio port of the agent model gateway (CHATVOICE-003 and
// CHATVOICE-006). SchemaFlux v1.2.0 has no audio operation, so speech to text
// and text to speech go straight to OpenAI's audio endpoints, with the same
// API key source, base-URL rules and metering as the typed model calls: every
// call is admitted by an AudioGuard before a byte leaves and is settled with
// its cost afterwards. A port without a guard, a key or a pinned model refuses;
// it never substitutes another model.

var (
	// ErrAudioNotConfigured marks a missing key, model pin, price or guard.
	ErrAudioNotConfigured = errors.New("agentmodel: audio port is not configured")
	// ErrAudioRefused marks a call the guard did not admit; nothing was sent.
	ErrAudioRefused = errors.New("agentmodel: audio call refused by policy")
	// ErrAudioUnavailable marks a provider that did not answer usefully.
	ErrAudioUnavailable = errors.New("agentmodel: audio provider unavailable")
	// ErrAudioInvalid marks a request or a provider answer that breaks the contract.
	ErrAudioInvalid = errors.New("agentmodel: invalid audio request or response")
)

const (
	audioDefaultBaseURL = "https://api.openai.com/v1"
	// AudioMaxBytes is the largest recording the port sends: the chat voice limit.
	AudioMaxBytes = 4 << 20
	// AudioMaxSpeechCharacters is OpenAI's input limit for one speech request.
	AudioMaxSpeechCharacters = 4096
	audioMaxResponse         = 16 << 20
	// DefaultTranscribeModel reports per-segment timings and confidence, which a
	// voice transcript stores; a model that reports none is refused, not guessed.
	DefaultTranscribeModel = "whisper-1"
	DefaultSpeechModel     = "gpt-4o-mini-tts"
	DefaultSpeechVoice     = "alloy"
)

// AudioKind names the two operations.
type AudioKind string

const (
	AudioTranscribe AudioKind = "transcribe"
	AudioSpeak      AudioKind = "speak"
)

// AudioPrice is the approved rate card. Zero rates are refused so that cost can
// never be silently erased.
type AudioPrice struct {
	TranscribeMicrosPerSecond    int64
	SpeechMicrosPerThousandChars int64
}

// AudioCall describes one call to the guard: who asked, where, and how much.
type AudioCall struct {
	CallID                   string
	Kind                     AudioKind
	TenantID, ConversationID string
	Actor                    string
	Model                    ModelIdentity
	Seconds                  float64
	Characters               int
	EstimatedCostMicros      int64
}

// AudioGuard carries policy and metering. Admit runs before any bytes leave
// (the channel's outside-service switch, the budget); Settle records the cost
// of a call that was made, successful or not.
type AudioGuard interface {
	Admit(context.Context, AudioCall) error
	Settle(context.Context, AudioCall, int64) error
}

// AudioConfig pins the models and the credential of an AudioPort.
type AudioConfig struct {
	APIKey          string
	BaseURL         string
	HTTPClient      *http.Client
	TranscribeModel ModelIdentity
	SpeechModel     ModelIdentity
	Voice           string
	Price           AudioPrice
	Guard           AudioGuard
}

// AudioPort calls OpenAI's audio endpoints through the gateway's rules.
type AudioPort struct {
	apiKey, base, voice string
	client              *http.Client
	transcribe, speech  ModelIdentity
	price               AudioPrice
	guard               AudioGuard
}

// NewAudioPort validates the configuration. A port that cannot name its models,
// price or guard is not built.
func NewAudioPort(cfg AudioConfig) (*AudioPort, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || cfg.Guard == nil || cfg.Price.TranscribeMicrosPerSecond <= 0 || cfg.Price.SpeechMicrosPerThousandChars <= 0 {
		return nil, ErrAudioNotConfigured
	}
	for _, id := range []ModelIdentity{cfg.TranscribeModel, cfg.SpeechModel} {
		if id.ProviderID != "openai" || !validIdentity(id) {
			return nil, ErrAudioNotConfigured
		}
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = audioDefaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" && audioLoopback(parsed.Hostname()))) {
		return nil, fmt.Errorf("%w: invalid API base URL", ErrAudioNotConfigured)
	}
	client := &http.Client{}
	if cfg.HTTPClient != nil {
		copied := *cfg.HTTPClient
		client = &copied
	}
	// The credential must never follow a redirect to another authority.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	voice := strings.TrimSpace(cfg.Voice)
	if voice == "" {
		voice = DefaultSpeechVoice
	}
	return &AudioPort{apiKey: cfg.APIKey, base: base, voice: voice, client: client, transcribe: cfg.TranscribeModel, speech: cfg.SpeechModel, price: cfg.Price, guard: cfg.Guard}, nil
}

func audioLoopback(host string) bool {
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// Models reports the pinned identities, for the administration page and for the
// model name and version stored with every transcript.
func (p *AudioPort) Models() (transcribe, speech ModelIdentity) {
	if p == nil {
		return ModelIdentity{}, ModelIdentity{}
	}
	return p.transcribe, p.speech
}

// AudioScope says whose call it is. Both values are checked by the guard.
type AudioScope struct{ TenantID, ConversationID, Actor string }

// AudioSegment is one timed run of speech.
type AudioSegment struct {
	StartMS, EndMS int64
	Text           string
	Confidence     float64
}

// AudioTranscript is a transcription the provider reported in full.
type AudioTranscript struct {
	Language   string
	DurationMS int64
	Segments   []AudioSegment
	Model      ModelIdentity
	CostMicros int64
}

// TranscribeRequest carries the bytes; DurationMS is the server's decoded
// duration, used for pricing and for clamping the provider's timings.
type TranscribeRequest struct {
	Scope       AudioScope
	Audio       []byte
	ContentType string
	DurationMS  int64
}

// Transcribe sends one recording to the pinned transcription model.
func (p *AudioPort) Transcribe(ctx context.Context, r TranscribeRequest) (AudioTranscript, error) {
	if p == nil || ctx == nil {
		return AudioTranscript{}, ErrAudioNotConfigured
	}
	if len(r.Audio) == 0 || len(r.Audio) > AudioMaxBytes || r.DurationMS <= 0 || r.DurationMS > 120000 {
		return AudioTranscript{}, ErrAudioInvalid
	}
	extension := ""
	switch strings.ToLower(strings.TrimSpace(strings.Split(r.ContentType, ";")[0])) {
	case "audio/webm":
		extension = "webm"
	case "audio/ogg":
		extension = "ogg"
	default:
		return AudioTranscript{}, ErrAudioInvalid
	}
	seconds := float64(r.DurationMS) / 1000
	call := AudioCall{CallID: audioCallID(), Kind: AudioTranscribe, TenantID: r.Scope.TenantID, ConversationID: r.Scope.ConversationID, Actor: r.Scope.Actor, Model: p.transcribe, Seconds: seconds, EstimatedCostMicros: int64(math.Ceil(seconds * float64(p.price.TranscribeMicrosPerSecond)))}
	if err := p.guard.Admit(ctx, call); err != nil {
		return AudioTranscript{}, errors.Join(ErrAudioRefused, err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("model", p.transcribe.ModelID)
	_ = form.WriteField("response_format", "verbose_json")
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="voice.`+extension+`"`)
	header.Set("Content-Type", strings.Split(r.ContentType, ";")[0])
	part, err := form.CreatePart(header)
	if err != nil {
		return AudioTranscript{}, err
	}
	if _, err = part.Write(r.Audio); err != nil {
		return AudioTranscript{}, err
	}
	if err = form.Close(); err != nil {
		return AudioTranscript{}, err
	}
	data, err := p.post(ctx, "/audio/transcriptions", form.FormDataContentType(), &body, call)
	if err != nil {
		return AudioTranscript{}, err
	}
	out, err := decodeTranscription(data, r.DurationMS)
	if err != nil {
		_ = p.guard.Settle(ctx, call, call.EstimatedCostMicros)
		return AudioTranscript{}, err
	}
	out.Model, out.CostMicros = p.transcribe, call.EstimatedCostMicros
	if err = p.guard.Settle(ctx, call, out.CostMicros); err != nil {
		return AudioTranscript{}, err
	}
	return out, nil
}

// AudioSpeech is synthesized speech, returned to the caller and stored nowhere.
type AudioSpeech struct {
	Audio       []byte
	ContentType string
	Model       ModelIdentity
	CostMicros  int64
	// Timings says when each sentence is spoken, when the engine reports it. The
	// speech endpoint the product uses returns audio only, so this stays empty
	// there and the page marks no sentence instead of guessing one.
	Timings []SpeechTiming
}

// SpeechTiming is one spoken sentence and the span of the audio it occupies.
type SpeechTiming struct {
	Text    string `json:"text"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

// SpeakRequest asks for text to be read aloud.
type SpeakRequest struct {
	Scope AudioScope
	Text  string
	// Opus asks for Ogg Opus instead of MP3. Only the live smoke run uses it, to
	// feed the transcription call a clip in a format the port accepts.
	Opus bool
}

// Speak turns one message's text into speech with the pinned speech model.
func (p *AudioPort) Speak(ctx context.Context, r SpeakRequest) (AudioSpeech, error) {
	if p == nil || ctx == nil {
		return AudioSpeech{}, ErrAudioNotConfigured
	}
	text := strings.TrimSpace(r.Text)
	characters := len([]rune(text))
	if characters == 0 || characters > AudioMaxSpeechCharacters {
		return AudioSpeech{}, ErrAudioInvalid
	}
	cost := (int64(characters)*p.price.SpeechMicrosPerThousandChars + 999) / 1000
	call := AudioCall{CallID: audioCallID(), Kind: AudioSpeak, TenantID: r.Scope.TenantID, ConversationID: r.Scope.ConversationID, Actor: r.Scope.Actor, Model: p.speech, Characters: characters, EstimatedCostMicros: cost}
	if err := p.guard.Admit(ctx, call); err != nil {
		return AudioSpeech{}, errors.Join(ErrAudioRefused, err)
	}
	format, contentType := "mp3", "audio/mpeg"
	if r.Opus {
		format, contentType = "opus", "audio/ogg"
	}
	raw, err := json.Marshal(map[string]string{"model": p.speech.ModelID, "input": text, "voice": p.voice, "response_format": format})
	if err != nil {
		return AudioSpeech{}, err
	}
	data, err := p.post(ctx, "/audio/speech", "application/json", bytes.NewReader(raw), call)
	if err != nil {
		return AudioSpeech{}, err
	}
	if len(data) < 4 {
		_ = p.guard.Settle(ctx, call, cost)
		return AudioSpeech{}, ErrAudioInvalid
	}
	if err = p.guard.Settle(ctx, call, cost); err != nil {
		return AudioSpeech{}, err
	}
	return AudioSpeech{Audio: data, ContentType: contentType, Model: p.speech, CostMicros: cost}, nil
}

// post sends one admitted call. A failed call is settled at its estimate: the
// provider may have started work, and the ledger must not undercount.
func (p *AudioPort) post(ctx context.Context, path, contentType string, body io.Reader, call AudioCall) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, AudioTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", contentType)
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = p.guard.Settle(ctx, call, call.EstimatedCostMicros)
		return nil, ErrAudioUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		_ = p.guard.Settle(ctx, call, 0)
		return nil, fmt.Errorf("%w: status %d", ErrAudioUnavailable, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, audioMaxResponse+1))
	if err != nil || len(data) > audioMaxResponse {
		_ = p.guard.Settle(ctx, call, call.EstimatedCostMicros)
		return nil, ErrAudioUnavailable
	}
	return data, nil
}

type transcriptionWire struct {
	Language string  `json:"language"`
	Duration float64 `json:"duration"`
	Text     string  `json:"text"`
	Segments []struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Text       string  `json:"text"`
		AvgLogprob float64 `json:"avg_logprob"`
	} `json:"segments"`
}

// decodeTranscription maps the provider's verbose answer onto timed segments.
// A confidence is the exponential of the segment's average log probability, the
// provider's own figure. An answer with no segments is refused: inventing one
// timing and one confidence for the whole text would be a fabricated transcript.
func decodeTranscription(data []byte, durationMS int64) (AudioTranscript, error) {
	var wire transcriptionWire
	if err := json.Unmarshal(data, &wire); err != nil || len(wire.Segments) == 0 {
		return AudioTranscript{}, ErrAudioInvalid
	}
	out := AudioTranscript{Language: audioLanguageCode(wire.Language), DurationMS: durationMS}
	var end int64
	for _, s := range wire.Segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		start, stop := int64(math.Round(s.Start*1000)), int64(math.Round(s.End*1000))
		// The decoded duration is the server's; a timing past it is clamped, and
		// segments are kept in order without overlap.
		if start < end {
			start = end
		}
		if stop > durationMS {
			stop = durationMS
		}
		if start >= durationMS || stop <= start {
			continue
		}
		confidence := math.Exp(s.AvgLogprob)
		if math.IsNaN(confidence) || confidence > 1 {
			confidence = 1
		}
		if confidence < 0 {
			confidence = 0
		}
		out.Segments = append(out.Segments, AudioSegment{StartMS: start, EndMS: stop, Text: text, Confidence: confidence})
		end = stop
	}
	if len(out.Segments) == 0 || out.Language == "" {
		return AudioTranscript{}, ErrAudioInvalid
	}
	return out, nil
}

var audioLanguageNames = map[string]string{
	"english": "en", "german": "de", "arabic": "ar", "spanish": "es", "french": "fr", "hindi": "hi", "japanese": "ja", "portuguese": "pt",
	"italian": "it", "dutch": "nl", "turkish": "tr", "russian": "ru", "polish": "pl", "chinese": "zh", "korean": "ko",
}

// audioLanguageCode turns the provider's language name into a code; a name this
// table does not know is kept as the provider spelled it rather than dropped.
func audioLanguageCode(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if code, ok := audioLanguageNames[name]; ok {
		return code
	}
	return name
}

// AudioTimeout bounds one provider call.
const AudioTimeout = 60 * time.Second

// audioCallID names one call so that a guard can tie its settlement to its
// admission.
func audioCallID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
