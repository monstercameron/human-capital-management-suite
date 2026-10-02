package chat

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"
)

const VoiceMaxDurationMS int64 = 120000
const VoiceMaxBytes = 4 << 20
const VoiceWorkerSubject = "chat-voice-worker"

var ErrVoiceUnavailable = errors.New("chat: voice engine unavailable")

type TranscriptState string

const (
	TranscriptNone        TranscriptState = "none"
	TranscriptPending     TranscriptState = "pending"
	TranscriptReady       TranscriptState = "ready"
	TranscriptUnavailable TranscriptState = "unavailable"
	TranscriptFailed      TranscriptState = "failed"
)

// VoiceInspection is produced by a server decoder, never by upload metadata.
type VoiceInspection struct {
	ContentType     string
	DurationMS      int64
	AudioOnly, Opus bool
}

type VoiceDecoder interface {
	InspectVoice(context.Context, []byte) (VoiceInspection, error)
}

type UnavailableVoiceDecoder struct{}

func (UnavailableVoiceDecoder) InspectVoice(context.Context, []byte) (VoiceInspection, error) {
	return VoiceInspection{}, ErrVoiceUnavailable
}

// VoicePlaybackURL permits only local preview blobs or protected media paths.
// Path traversal and encoded separators must not escape the media boundary.
func VoicePlaybackURL(raw string) bool {
	if strings.ContainsAny(raw, "\r\n\\") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "blob" {
		return u.Opaque != "" && u.Fragment == ""
	}
	if u.Scheme != "" || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/v1/chat/media/") {
		return false
	}
	id := strings.TrimPrefix(u.Path, "/v1/chat/media/")
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
}

type VoiceAttachment struct {
	ArtifactID      string
	ContentType     string
	Bytes           int64
	DurationMS      int64
	Waveform        []float64
	TranscriptState TranscriptState
}

func (a VoiceAttachment) Validate() error {
	if a.ContentType != "audio/webm" && a.ContentType != "audio/ogg" || a.Bytes <= 0 || a.Bytes > VoiceMaxBytes || a.DurationMS <= 0 || a.DurationMS > VoiceMaxDurationMS || len(a.Waveform) > 128 {
		return ErrInvalidArgument
	}
	for _, level := range a.Waveform {
		if math.IsNaN(level) || math.IsInf(level, 0) || level < 0 || level > 1 {
			return ErrInvalidArgument
		}
	}
	return nil
}

// ValidateVoiceUpload fails closed when no decoder is wired. Client duration
// and waveform are rendering hints and cannot authorize admission.
func ValidateVoiceUpload(ctx context.Context, decoder VoiceDecoder, declared string, content []byte, waveform []float64) (VoiceAttachment, error) {
	if len(content) == 0 || len(content) > VoiceMaxBytes || len(waveform) > 128 {
		return VoiceAttachment{}, ErrInvalidArgument
	}
	for _, level := range waveform {
		if math.IsNaN(level) || math.IsInf(level, 0) || level < 0 || level > 1 {
			return VoiceAttachment{}, ErrInvalidArgument
		}
	}
	declared = strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	if declared != "audio/webm" && declared != "audio/ogg" {
		return VoiceAttachment{}, ErrInvalidArgument
	}
	if decoder == nil {
		return VoiceAttachment{}, ErrVoiceUnavailable
	}
	facts, err := decoder.InspectVoice(ctx, content)
	if err != nil {
		return VoiceAttachment{}, err
	}
	if facts.ContentType != declared || !facts.AudioOnly || !facts.Opus || facts.DurationMS <= 0 || facts.DurationMS > VoiceMaxDurationMS {
		return VoiceAttachment{}, ErrInvalidArgument
	}
	return VoiceAttachment{ContentType: facts.ContentType, Bytes: int64(len(content)), DurationMS: facts.DurationMS, Waveform: append([]float64(nil), waveform...), TranscriptState: TranscriptPending}, nil
}

// VoicePolicy gives workspace and personal switches precedence over room
// defaults. Channels require an explicit administrator enablement.
type VoicePolicy struct{ WorkspaceEnabled, PersonalEnabled, ChannelEnabled bool }

func (p VoicePolicy) Allows(kind ConversationKind) bool {
	return p.WorkspaceEnabled && p.PersonalEnabled && (kind == Direct || kind == Group || ((kind == PublicChannel || kind == PrivateChannel) && p.ChannelEnabled))
}

type TranscriptSegment struct {
	StartMS, EndMS int64
	Text           string
	Confidence     float64
}
type VoiceTranscript struct {
	State                    TranscriptState
	Language, Model, Version string
	Segments                 []TranscriptSegment
	Correction               string
	Revision                 uint64
}

func (t VoiceTranscript) Text() string {
	if t.Correction != "" {
		return t.Correction
	}
	parts := make([]string, len(t.Segments))
	for i, segment := range t.Segments {
		parts[i] = segment.Text
	}
	return strings.Join(parts, " ")
}
func (t VoiceTranscript) Validate(durationMS int64) error {
	switch t.State {
	case TranscriptNone, TranscriptPending, TranscriptUnavailable, TranscriptFailed:
		if len(t.Segments) != 0 || t.Correction != "" {
			return ErrInvalidArgument
		}
		return nil
	case TranscriptReady:
	default:
		return ErrInvalidArgument
	}
	if t.Language == "" || t.Model == "" || t.Version == "" || len(t.Segments) == 0 || len(t.Segments) > 1000 || len(t.Correction) > 16000 {
		return ErrInvalidArgument
	}
	var end int64
	total := 0
	for _, s := range t.Segments {
		total += len(s.Text)
		if s.StartMS < end || s.EndMS <= s.StartMS || s.EndMS > durationMS || strings.TrimSpace(s.Text) == "" || math.IsNaN(s.Confidence) || math.IsInf(s.Confidence, 0) || s.Confidence < 0 || s.Confidence > 1 || total > 16000 {
			return ErrInvalidArgument
		}
		end = s.EndMS
	}
	return nil
}

// The worker receives only an admitted artifact identity. Implementations read
// bytes via the protected media port under a conversation-scoped worker identity.
type TranscriptionRequest struct {
	TenantID, ConversationID, PostID, ArtifactID string
	DurationMS                                   int64
}
type Transcriber interface {
	Transcribe(context.Context, TranscriptionRequest) (VoiceTranscript, error)
}
type UnavailableTranscriber struct{}

func (UnavailableTranscriber) Transcribe(context.Context, TranscriptionRequest) (VoiceTranscript, error) {
	return VoiceTranscript{State: TranscriptUnavailable}, ErrVoiceUnavailable
}

// FixtureTranscriber is explicit composition for deterministic tests, never a
// fallback model. Results are copied so a caller cannot alter later responses.
type FixtureTranscriber struct {
	Result  VoiceTranscript
	Failure error
}

func (f FixtureTranscriber) Transcribe(_ context.Context, r TranscriptionRequest) (VoiceTranscript, error) {
	if f.Failure != nil {
		return VoiceTranscript{State: TranscriptFailed}, f.Failure
	}
	out := f.Result
	out.Segments = append([]TranscriptSegment(nil), out.Segments...)
	if err := out.Validate(r.DurationMS); err != nil {
		return VoiceTranscript{}, err
	}
	return out, nil
}

type VoiceRecord struct {
	TranscriptionRequest
	Attachment VoiceAttachment
	Transcript VoiceTranscript
}

type VoiceStore interface {
	RequestVoiceTranscript(context.Context, Principal, VoiceRecord) (VoiceRecord, error)
	ReadVoiceTranscript(context.Context, Principal, TranscriptionRequest) (VoiceRecord, error)
	SaveVoiceTranscript(context.Context, Principal, TranscriptionRequest, uint64, VoiceTranscript) (VoiceRecord, error)
	CorrectVoiceTranscript(context.Context, Principal, TranscriptionRequest, uint64, string) (VoiceRecord, error)
	RetryVoiceTranscript(context.Context, Principal, TranscriptionRequest, uint64) (VoiceRecord, error)
	ReportVoiceTranscript(context.Context, Principal, TranscriptionRequest) error
	SearchVoiceTranscripts(context.Context, Principal, string, string, int) ([]VoiceRecord, error)
}
