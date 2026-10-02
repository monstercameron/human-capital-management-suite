package application

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// Configuration of the voice engine. The key is the model gateway's own
// (MODEL_API_KEY); everything else has a visible default and none of it falls
// back to another model: a missing key leaves transcripts "unavailable" and
// Listen absent, with the reason logged once at start-up.
const (
	EnvChatVoiceTranscribeModel = "HCMNEXT_CHATVOICE_TRANSCRIBE_MODEL"
	EnvChatVoiceSpeechModel     = "HCMNEXT_CHATVOICE_SPEECH_MODEL"
	EnvChatVoiceModelVersion    = "HCMNEXT_CHATVOICE_MODEL_VERSION"
	EnvChatVoiceVoice           = "HCMNEXT_CHATVOICE_VOICE"
	EnvChatVoiceBaseURL         = "HCMNEXT_CHATVOICE_BASE_URL"
	EnvChatVoiceTranscribePrice = "HCMNEXT_CHATVOICE_TRANSCRIBE_MICROS_PER_SECOND"
	EnvChatVoiceSpeechPrice     = "HCMNEXT_CHATVOICE_SPEECH_MICROS_PER_1000_CHARS"
	EnvChatVoiceMonthlyBudget   = "HCMNEXT_CHATVOICE_MONTHLY_SPEND_MICROS"
	EnvChatVoiceDailyBudget     = "HCMNEXT_CHATVOICE_USER_DAILY_SPEND_MICROS"
	// EnvChatVoiceEngine selects the deterministic engine in the local
	// development profile ("fixture"): it transcribes and speaks nothing real,
	// costs nothing and calls no one, so the page can be driven without a model.
	EnvChatVoiceEngine = "HCMNEXT_CHATVOICE_ENGINE"
	// chatVoiceModelVersionDefault records that no provider snapshot is pinned.
	chatVoiceModelVersionDefault = "provider-current"
)

type chatVoiceServeInput struct {
	Config  ServeConfig
	Chat    composedChat
	Media   ChatMediaConfig
	Env     func(string) string
	Now     func() time.Time
	Tenants []string
}

// ChatVoiceRuntime is the composed voice feature.
type ChatVoiceRuntime struct {
	Service VoiceService
	Worker  *VoiceTranscriptionRuntime
	// Engine says what transcribes: the model name, or why nothing does.
	Engine string
}

type chatVoiceDecoder struct{}

func (chatVoiceDecoder) InspectVoice(_ context.Context, content []byte) (chat.VoiceInspection, error) {
	facts, err := chatmedia.InspectVoice(content)
	if err != nil {
		return chat.VoiceInspection{}, chat.ErrInvalidArgument
	}
	return chat.VoiceInspection{ContentType: string(facts.MediaType), DurationMS: facts.Duration.Milliseconds(), AudioOnly: true, Opus: true}, nil
}

// chatVoiceUploader admits a recording through the media service's voice path.
type chatVoiceUploader struct{ media *chatmedia.Service }

func (u chatVoiceUploader) Upload(ctx context.Context, r chatmedia.UploadRequest) (chatmedia.Reference, error) {
	return u.media.UploadVoice(ctx, r)
}

// voiceFilterScreen runs the typed-message content policy over a transcript.
type voiceFilterScreen struct {
	Filter        *chat.FilterContentPolicy
	Conversations chat.ConversationService
}

func (s voiceFilterScreen) ScreenVoiceText(ctx context.Context, tenant, conversation, home, author, text string) error {
	if s.Filter == nil || s.Conversations == nil {
		return chat.ErrVoiceUnavailable
	}
	principal := chat.Principal{TenantID: home, SubjectID: author}
	c, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: tenant, ConversationID: conversation})
	if err != nil {
		return err
	}
	return s.Filter.CheckFilterText(ctx, principal, c, text)
}

// composeServedChatVoice puts voice messages together over the composed chat:
// admission of recordings, the switches, the transcription worker and Listen.
// It returns a nil runtime and a plain reason when Chat or its media store is
// not composed. A missing model key is not that: recording, sending and
// playing work, and transcripts say they are unavailable until a key is set.
func composeServedChatVoice(in chatVoiceServeInput) (*ChatVoiceRuntime, string, error) {
	cfg, runtime := in.Config, in.Chat
	if !cfg.ChatEnabled || runtime.store == nil || runtime.service == nil || runtime.extensions == nil || runtime.moderationStore == nil {
		return nil, "chat is not composed", nil
	}
	if in.Env == nil {
		in.Env = os.Getenv
	}
	local := cfg.Profile == ServeProfileLocalDev
	media := in.Media.WithDefaults(cfg.ChatMediaRoot, cfg.ArtifactRoot)
	if !local && media.Scanner == nil {
		return nil, "no malware scanner is configured for chat media", nil
	}
	store, err := chatmedia.NewFilesystemStore(media.ArtifactRoot)
	if err != nil {
		return nil, "", err
	}
	service := chatmedia.New(chatmedia.Config{Store: store, Scanner: chatmedia.VoiceScanner{External: media.Scanner}, Authorize: runtime.extensions.AuthorizeMedia, MaxBytes: chatmedia.VoiceMaxBytes})
	voice := VoiceService{
		Access:   VoiceStoreAccess{Store: runtime.store, Conversations: runtime.service},
		Decoder:  chatVoiceDecoder{},
		Media:    chatVoiceUploader{media: service},
		Switches: runtime.store,
		Barred:   runtime.store,
		Admin:    nil,
		// The settings page names the engine; until one is chosen below, it says
		// plainly that there is none.
		SwitchValues: runtime.store,
		Engine:       VoiceEngineInfo{Kind: "none", Note: "set " + EnvChatVoiceEngine + " or MODEL_API_KEY"},
	}
	if runtime.renderings != nil && runtime.renderings.Languages != nil {
		voice.Admin = runtime.renderings.Languages.Admins
	}
	now := in.Now
	if now == nil {
		now = time.Now
	}
	worker := &VoiceTranscriptionRuntime{Store: runtime.moderationStore, Tenants: in.Tenants, Now: now}
	engine := "no engine: " + EnvChatVoiceEngine + " and MODEL_API_KEY are not set"
	screen := voiceFilterScreen{Filter: runtime.voiceFilter, Conversations: runtime.service}
	authors := runtime.moderationStore
	switch {
	case in.Env(EnvChatVoiceEngine) == "fixture":
		if !local {
			return nil, "the fixture voice engine is only available in the local development profile", nil
		}
		worker.Engine = chatVoiceFixtureEngine{}
		voice.Speaker = &VoiceSpeaker{Reader: runtime.service, Filter: runtime.voiceFilter, Port: chatVoiceFixtureSpeech{}}
		engine = "fixture (local development)"
		voice.Engine = VoiceEngineInfo{Kind: "fixture", Transcriber: "fixture", Speech: "fixture"}
	case in.Env("MODEL_API_KEY") != "":
		if runtime.renderings == nil || runtime.renderings.Languages == nil {
			return nil, "the channel settings that bar outside services are not composed", nil
		}
		guard, err := NewVoiceAudioGuard(ChatlangVoiceExternal{Governance: runtime.renderings.Languages}, chatVoiceBudget(in.Env), now)
		if err != nil {
			return nil, "", err
		}
		port, err := newChatVoicePort(in.Env, guard)
		if err != nil {
			return nil, "", err
		}
		transcribeModel, speechModel := port.Models()
		voice.Engine = VoiceEngineInfo{Kind: "outside", Transcriber: transcribeModel.ModelID, Speech: speechModel.ModelID}
		worker.Engine = OpenAIVoiceTranscriber{Audio: service, Port: port, Authors: authors, Screen: screen, Worker: chat.Principal{SubjectID: chat.VoiceWorkerSubject}}
		voice.Speaker = &VoiceSpeaker{Reader: runtime.service, Filter: runtime.voiceFilter, Port: port}
		engine = "OpenAI " + transcribeModel.ModelID + " through the model gateway's key"
	}
	return &ChatVoiceRuntime{Service: voice, Worker: worker, Engine: engine}, "", nil
}

func chatVoiceInt(env func(string) string, name string, fallback int64) int64 {
	if raw := strings.TrimSpace(env(name)); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func chatVoiceBudget(env func(string) string) VoiceBudgetPolicy {
	d := DefaultVoiceBudgetPolicy()
	return VoiceBudgetPolicy{TenantMonthlyMicros: chatVoiceInt(env, EnvChatVoiceMonthlyBudget, d.TenantMonthlyMicros), UserDailyMicros: chatVoiceInt(env, EnvChatVoiceDailyBudget, d.UserDailyMicros)}
}

// newChatVoicePort builds the audio port from the environment. The models are
// the configured ones or the visible defaults of agentmodel; the key is the
// model gateway's.
func newChatVoicePort(env func(string) string, guard agentmodel.AudioGuard) (*agentmodel.AudioPort, error) {
	pick := func(name, fallback string) string {
		if v := strings.TrimSpace(env(name)); v != "" {
			return v
		}
		return fallback
	}
	version := pick(EnvChatVoiceModelVersion, chatVoiceModelVersionDefault)
	return agentmodel.NewAudioPort(agentmodel.AudioConfig{
		APIKey: env("MODEL_API_KEY"), BaseURL: env(EnvChatVoiceBaseURL), Voice: env(EnvChatVoiceVoice),
		TranscribeModel: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: pick(EnvChatVoiceTranscribeModel, agentmodel.DefaultTranscribeModel), Version: version},
		SpeechModel:     agentmodel.ModelIdentity{ProviderID: "openai", ModelID: pick(EnvChatVoiceSpeechModel, agentmodel.DefaultSpeechModel), Version: version},
		Price:           agentmodel.AudioPrice{TranscribeMicrosPerSecond: chatVoiceInt(env, EnvChatVoiceTranscribePrice, 100), SpeechMicrosPerThousandChars: chatVoiceInt(env, EnvChatVoiceSpeechPrice, 15000)},
		Guard:           guard,
	})
}

// chatVoiceFixtureEngine is the local development transcriber: one segment that
// says plainly that no speech model was called.
type chatVoiceFixtureEngine struct{}

func (chatVoiceFixtureEngine) Transcribe(_ context.Context, r chat.TranscriptionRequest) (chat.VoiceTranscript, error) {
	return chat.VoiceTranscript{State: chat.TranscriptReady, Language: "en", Model: "fixture", Version: "local-development", Segments: []chat.TranscriptSegment{{StartMS: 0, EndMS: r.DurationMS, Text: "Fixture transcript. No speech model was called.", Confidence: 0.95}}}, nil
}

// chatVoiceFixtureSpeech is the local development Listen: silence as long as the
// sentences would take, with their timings, so the player, its controls and the
// sentence mark can be driven without a model.
type chatVoiceFixtureSpeech struct{}

func (chatVoiceFixtureSpeech) Speak(_ context.Context, r agentmodel.SpeakRequest) (agentmodel.AudioSpeech, error) {
	const rate = 8000
	timings := chatVoiceFixtureTimings(r.Text)
	samples := 2400
	if len(timings) > 0 {
		samples = int(timings[len(timings)-1].EndMS) * rate / 1000
	}
	wav := make([]byte, 44+2*samples)
	copy(wav, "RIFF")
	put := func(at int, v uint32, n int) {
		for i := 0; i < n; i++ {
			wav[at+i] = byte(v >> (8 * i))
		}
	}
	put(4, uint32(len(wav)-8), 4)
	copy(wav[8:], "WAVEfmt ")
	put(16, 16, 4)
	put(20, 1, 2)
	put(22, 1, 2)
	put(24, rate, 4)
	put(28, rate*2, 4)
	put(32, 2, 2)
	put(34, 16, 2)
	copy(wav[36:], "data")
	put(40, uint32(2*samples), 4)
	return agentmodel.AudioSpeech{Audio: wav, ContentType: "audio/wav", Timings: timings, Model: agentmodel.ModelIdentity{ProviderID: "fixture", ModelID: "fixture", Version: "local-development"}}, nil
}

// chatVoiceBackgroundWorkload runs the transcription worker until shutdown.
func chatVoiceBackgroundWorkload(runtime *ChatVoiceRuntime) *bootstrap.Workload {
	if runtime == nil || runtime.Worker == nil || len(runtime.Worker.Tenants) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "chat-voice-transcription", Run: func(ctx context.Context) error {
		_ = runtime.Worker.Run(ctx)
		return nil
	}}
}

// logChatVoiceComposition states once, at start-up, which engine transcribes
// voice messages and why none does when none does.
func logChatVoiceComposition(logger interface {
	Info(string, ...any)
	Error(string, ...any)
}, runtime *ChatVoiceRuntime, reason string, err error) {
	if isNilPersonaOutputPort(logger) {
		return
	}
	switch {
	case err != nil:
		logger.Error("hcmnext.chat_voice_unavailable", "error", err.Error())
	case runtime == nil:
		logger.Info("hcmnext.chat_voice_unavailable", "reason", reason)
	default:
		logger.Info("hcmnext.chat_voice_ready", "engine", fmt.Sprint(runtime.Engine))
	}
}
