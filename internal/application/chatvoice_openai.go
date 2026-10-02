package application

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// This file puts OpenAI behind the two voice ports (CHATVOICE-003 and -006)
// through the agent model gateway's audio port: transcription of a recording
// and text to speech for Listen. The owner's decision of 2026-10-02 replaces
// the original in-deployment placement of the speech model; every call is still
// governed (the channel's outside-service switch, the budget) before it leaves.

type VoiceAudioSource interface {
	ReadVoice(ctx context.Context, tenant, conversation, artifact string) ([]byte, chatmedia.MediaType, error)
}
type VoiceSpeechToText interface {
	Transcribe(context.Context, agentmodel.TranscribeRequest) (agentmodel.AudioTranscript, error)
}
type VoiceTextToSpeech interface {
	Speak(context.Context, agentmodel.SpeakRequest) (agentmodel.AudioSpeech, error)
}
type VoiceAuthors interface {
	VoicePostAuthor(context.Context, chat.Principal, chat.TranscriptionRequest) (homeTenant, author string, err error)
}

// VoiceTextScreen runs the content policy and data-loss checks that run on typed
// messages over a transcript. A refusal withholds the transcript: the message
// was already posted, so the hit cannot stop the post, and the text a rule
// blocks is never stored or shown.
type VoiceTextScreen interface {
	ScreenVoiceText(ctx context.Context, tenant, conversation, homeTenant, author, text string) error
}

// OpenAIVoiceTranscriber is the chat.Transcriber of the product.
type OpenAIVoiceTranscriber struct {
	Audio   VoiceAudioSource
	Port    VoiceSpeechToText
	Authors VoiceAuthors
	Screen  VoiceTextScreen
	Worker  chat.Principal
}

var _ chat.Transcriber = OpenAIVoiceTranscriber{}

// Transcribe reads the recording as the worker for this one conversation and
// sends it to the pinned model. A refusal by policy and a missing model are the
// visible "unavailable" state and are retryable; a provider failure is "failed".
// Neither ever becomes an empty or invented transcript.
func (t OpenAIVoiceTranscriber) Transcribe(ctx context.Context, r chat.TranscriptionRequest) (chat.VoiceTranscript, error) {
	if t.Audio == nil || t.Port == nil || t.Authors == nil || t.Screen == nil {
		return chat.VoiceTranscript{State: chat.TranscriptUnavailable}, chat.ErrVoiceUnavailable
	}
	home, author, err := t.Authors.VoicePostAuthor(ctx, t.Worker, r)
	if err != nil {
		return chat.VoiceTranscript{State: chat.TranscriptFailed}, err
	}
	audio, kind, err := t.Audio.ReadVoice(ctx, r.TenantID, r.ConversationID, r.ArtifactID)
	if err != nil {
		return chat.VoiceTranscript{State: chat.TranscriptFailed}, err
	}
	out, err := t.Port.Transcribe(ctx, agentmodel.TranscribeRequest{Scope: agentmodel.AudioScope{TenantID: r.TenantID, ConversationID: r.ConversationID, Actor: author}, Audio: audio, ContentType: string(kind), DurationMS: r.DurationMS})
	if errors.Is(err, agentmodel.ErrAudioRefused) || errors.Is(err, agentmodel.ErrAudioNotConfigured) {
		return chat.VoiceTranscript{State: chat.TranscriptUnavailable}, chat.ErrVoiceUnavailable
	}
	if err != nil {
		return chat.VoiceTranscript{State: chat.TranscriptFailed}, err
	}
	value := chat.VoiceTranscript{State: chat.TranscriptReady, Language: out.Language, Model: out.Model.ModelID, Version: out.Model.Version}
	parts := make([]string, 0, len(out.Segments))
	for _, s := range out.Segments {
		value.Segments = append(value.Segments, chat.TranscriptSegment{StartMS: s.StartMS, EndMS: s.EndMS, Text: s.Text, Confidence: s.Confidence})
		parts = append(parts, s.Text)
	}
	if err = value.Validate(r.DurationMS); err != nil {
		return chat.VoiceTranscript{State: chat.TranscriptFailed}, err
	}
	if err = t.Screen.ScreenVoiceText(ctx, r.TenantID, r.ConversationID, home, author, strings.Join(parts, " ")); err != nil {
		return chat.VoiceTranscript{State: chat.TranscriptUnavailable}, chat.ErrVoiceUnavailable
	}
	return value, nil
}

var errVoiceEngineCrashed = errors.New("application: the transcription engine crashed")

// VoiceWorkerStore is what the worker needs of the chat store.
type VoiceWorkerStore interface {
	chat.VoiceStore
	PendingVoiceTranscriptsAll(context.Context, chat.Principal, string, int) ([]chat.TranscriptionRequest, error)
}

// VoiceTranscriptionRuntime drains pending transcripts for the served tenants.
// It shares the shape of the translation worker: a durable backlog read in
// bounded batches, completion by revision compare-and-set, and nothing held in
// memory that a restart would lose, so a crash leaves requests pending.
type VoiceTranscriptionRuntime struct {
	Store       VoiceWorkerStore
	Engine      chat.Transcriber
	Tenants     []string
	Now         func() time.Time
	Poll        time.Duration
	Batch       int
	Concurrency int
}

func voiceWorkerContext(ctx context.Context, tenant string, now func() time.Time) (context.Context, chat.Principal, error) {
	at := now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: chat.VoiceWorkerSubject, SubjectKind: trust.SubjectKindIntegration, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "chat-voice-worker", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "in-process"})
	if err != nil {
		return ctx, chat.Principal{}, err
	}
	return trust.WithPrincipal(ctx, identity), chat.Principal{TenantID: tenant, SubjectID: chat.VoiceWorkerSubject}, nil
}

// Drain transcribes everything pending for one tenant, a bounded batch at a
// time, and returns the outcome counts. An engine's failure is recorded as a
// visible state on the message; only a store failure ends the drain.
func (r *VoiceTranscriptionRuntime) Drain(ctx context.Context, tenant string) (VoiceBatchResult, error) {
	var total VoiceBatchResult
	if r == nil || r.Store == nil || tenant == "" {
		return total, chat.ErrVoiceUnavailable
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	batch, workers := r.Batch, r.Concurrency
	if batch < 1 || batch > 100 {
		batch = 20
	}
	if workers < 1 {
		workers = 4
	}
	for ctx.Err() == nil {
		wctx, worker, err := voiceWorkerContext(ctx, tenant, now)
		if err != nil {
			return total, err
		}
		pending, err := r.Store.PendingVoiceTranscriptsAll(wctx, worker, tenant, batch)
		if err != nil || len(pending) == 0 {
			return total, err
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		var storeErr error
		slots := make(chan struct{}, workers)
		for _, request := range pending {
			slots <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-slots; wg.Done() }()
				// An engine that crashes leaves its request pending and untouched
				// (nothing was saved), so the message stays readable and the next
				// poll retries it; the drain stops rather than spin on it.
				defer func() {
					if recover() != nil {
						mu.Lock()
						storeErr = errors.Join(storeErr, errVoiceEngineCrashed)
						mu.Unlock()
					}
				}()
				record, err := CompleteVoiceTranscript(wctx, r.Store, worker, request, r.Engine)
				mu.Lock()
				defer mu.Unlock()
				switch record.Transcript.State {
				case chat.TranscriptReady:
					total.Ready++
				case chat.TranscriptUnavailable:
					total.Unavailable++
				case chat.TranscriptFailed:
					total.Failed++
				default:
					storeErr = errors.Join(storeErr, err)
				}
			}()
		}
		wg.Wait()
		if storeErr != nil {
			return total, storeErr
		}
	}
	return total, ctx.Err()
}

// Run polls until the context ends.
func (r *VoiceTranscriptionRuntime) Run(ctx context.Context) error {
	if r == nil {
		return chat.ErrVoiceUnavailable
	}
	poll := r.Poll
	if poll <= 0 {
		poll = 3 * time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		for _, tenant := range r.Tenants {
			_, _ = r.Drain(ctx, tenant)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ErrVoiceTooLong marks a message too long to read aloud in one request.
var ErrVoiceTooLong = errors.New("application: this message is too long to read aloud")

// VoiceSpeakRequest names one message to be read aloud.
//
// With ThreadID set it names a thread instead: the root and its replies are read
// in order, each led by its author's name taken from Names (author id to the
// name the page shows), which only ever becomes spoken text.
type VoiceSpeakRequest struct {
	TenantID, ConversationID, PostID string
	ThreadID                         string
	Names                            map[string]string
}

// VoiceSpeaker is Listen (CHATVOICE-006): it reads a message as the person who
// asked, so a message they cannot read is never spoken, speaks what a person
// would read (the filter's masked text, a mention as its name, a link as "link
// to", a citation as "source"), and returns the speech without storing it.
type VoiceSpeaker struct {
	Reader chat.ConversationService
	Filter *chat.FilterContentPolicy
	Port   VoiceTextToSpeech
}

// Speak returns the speech for one message.
func (s VoiceSpeaker) Speak(ctx context.Context, p chat.Principal, r VoiceSpeakRequest) (agentmodel.AudioSpeech, error) {
	if s.Reader == nil || s.Port == nil {
		return agentmodel.AudioSpeech{}, chat.ErrVoiceUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return agentmodel.AudioSpeech{}, chat.ErrPermissionDenied
	}
	var text string
	var err error
	if r.ThreadID != "" {
		text, err = s.threadText(ctx, p, r)
	} else {
		var post chat.Post
		if post, err = integrate2ReadPost(ctx, s.Reader, p, r.TenantID, r.ConversationID, r.PostID); err == nil {
			text, err = s.spokenPost(ctx, p, post)
		}
	}
	if err != nil {
		return agentmodel.AudioSpeech{}, err
	}
	if text == "" {
		return agentmodel.AudioSpeech{}, chat.ErrInvalidArgument
	}
	if len([]rune(text)) > agentmodel.AudioMaxSpeechCharacters {
		return agentmodel.AudioSpeech{}, ErrVoiceTooLong
	}
	out, err := s.Port.Speak(ctx, agentmodel.SpeakRequest{Scope: agentmodel.AudioScope{TenantID: r.TenantID, ConversationID: r.ConversationID, Actor: p.SubjectID}, Text: text})
	if errors.Is(err, agentmodel.ErrAudioRefused) || errors.Is(err, agentmodel.ErrAudioNotConfigured) {
		return agentmodel.AudioSpeech{}, errors.Join(chat.ErrVoiceUnavailable, err)
	}
	return out, err
}

var (
	spokenMarkdownLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	spokenCitation     = regexp.MustCompile(`\[\[(\d+)(?::[^\]]*)?\]\]`)
	spokenURL          = regexp.MustCompile(`https?://([^\s/<>"')]+)[^\s<>"')]*`)
	spokenMarks        = regexp.MustCompile("[*_`~]+|(?m)^\\s{0,3}(?:#{1,6}|>)\\s*")
	spokenMention      = regexp.MustCompile(`(^|\s)@(\w)`)
	spokenSpace        = regexp.MustCompile(`\s+`)
)

// VoiceSpokenText is what a person would read: the label of a link, "link to"
// and its host for a bare address, "source 1" for a citation, a mention as the
// name without its sign, and no formatting marks.
func VoiceSpokenText(body string) string {
	text := spokenCitation.ReplaceAllString(body, " source $1 ")
	text = spokenMarkdownLink.ReplaceAllStringFunc(text, func(m string) string {
		parts := spokenMarkdownLink.FindStringSubmatch(m)
		if label := strings.TrimSpace(parts[1]); label != "" {
			return label
		}
		return "link to " + spokenHost(parts[2])
	})
	text = spokenURL.ReplaceAllStringFunc(text, func(m string) string {
		// Sentence punctuation after an address belongs to the sentence.
		trimmed := strings.TrimRight(m, ".,;:!?")
		return "link to " + spokenURL.FindStringSubmatch(trimmed)[1] + m[len(trimmed):]
	})
	text = spokenMarks.ReplaceAllString(text, "")
	text = spokenMention.ReplaceAllString(text, "$1$2")
	return strings.TrimSpace(spokenSpace.ReplaceAllString(text, " "))
}

func spokenHost(raw string) string {
	if m := spokenURL.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return "a page"
}
