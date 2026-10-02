package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

// VoiceAccess is resolved from current workspace, channel and person settings
// by the composition root, before bytes are decoded, uploaded or posted.
type VoiceAccess interface {
	AuthorizeVoice(context.Context, chat.Principal, string, string, string) (chat.VoicePolicy, chat.ConversationKind, error)
}
type VoiceMediaUploader interface {
	Upload(context.Context, chatmedia.UploadRequest) (chatmedia.Reference, error)
}
type VoiceMessageWriter interface {
	SendPostWithReferences(context.Context, chat.SendPostWithReferencesRequest) (chat.Post, error)
}
type VoiceService struct {
	Access      VoiceAccess
	Decoder     chat.VoiceDecoder
	Media       VoiceMediaUploader
	Messages    VoiceMessageWriter
	Transcripts chat.VoiceStore
	// Speaker serves Listen, Switches stores the voice switches and Admin decides
	// who may change a workspace's or a channel's.
	Speaker  *VoiceSpeaker
	Switches VoiceSwitchStore
	Admin    chatfilter.Authority
	// Barred says where Listen is absent because the channel bars outside services.
	Barred VoiceBarredSource
	// SwitchValues and Engine feed the workspace settings page.
	SwitchValues VoiceSwitchReader
	Engine       VoiceEngineInfo
}
type VoiceSendRequest struct {
	TenantID, ConversationID, IdempotencyKey, ContentType, Note, Locale string
	Content                                                             []byte
	Waveform                                                            []float64
}

func voiceMessageLabel(locale string) string {
	if strings.HasPrefix(locale, "de") {
		return "Sprachnachricht"
	}
	if strings.HasPrefix(locale, "ar") {
		return "رسالة صوتية"
	}
	return "Voice message"
}

type VoiceSendResult struct {
	Post  chat.Post
	Voice chat.VoiceRecord
}

func (s VoiceService) Send(ctx context.Context, p chat.Principal, r VoiceSendRequest) (VoiceSendResult, error) {
	var out VoiceSendResult
	if p.TenantID == "" || p.SubjectID == "" {
		return out, chat.ErrPermissionDenied
	}
	if r.TenantID == "" || r.ConversationID == "" || r.IdempotencyKey == "" || len(r.IdempotencyKey) > 256 || len(r.Note) > 16000 {
		return out, chat.ErrInvalidArgument
	}
	if s.Access == nil || s.Media == nil || s.Messages == nil || s.Transcripts == nil {
		return out, chat.ErrVoiceUnavailable
	}
	policy, kind, err := s.Access.AuthorizeVoice(ctx, p, r.TenantID, r.ConversationID, "send")
	if err != nil {
		return out, err
	}
	if !policy.Allows(kind) {
		return out, chat.ErrPermissionDenied
	}
	a, err := chat.ValidateVoiceUpload(ctx, s.Decoder, r.ContentType, r.Content, r.Waveform)
	if err != nil {
		return out, err
	}
	ref, err := s.Media.Upload(ctx, chatmedia.UploadRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, PrincipalID: p.SubjectID, Filename: "voice-message", DeclaredType: a.ContentType, Content: r.Content, AltText: r.Note})
	if err != nil {
		return out, err
	}
	if ref.State != chatmedia.StateAdmitted || ref.TenantID != r.TenantID || ref.ConversationID != r.ConversationID || string(ref.MediaType) != a.ContentType || ref.Size != a.Bytes {
		return out, chat.ErrInvalidArgument
	}
	a.ArtifactID = ref.ArtifactID
	label := voiceMessageLabel(r.Locale)
	body := r.Note
	if strings.TrimSpace(body) == "" {
		// Chat's canonical writer requires a body. A localized media caption
		// lets the typed note remain optional without fabricating a transcript.
		body = label
	}
	post, err := s.Messages.SendPostWithReferences(ctx, chat.SendPostWithReferencesRequest{SendPostRequest: chat.SendPostRequest{Principal: p, TenantID: r.TenantID, ConversationID: r.ConversationID, IdempotencyKey: r.IdempotencyKey, Body: body}, References: []chat.Reference{{Kind: chat.MediaAttachment, TenantID: r.TenantID, ConversationID: r.ConversationID, ID: ref.ArtifactID, Display: label, ContentType: a.ContentType, ByteSize: uint64(a.Bytes)}}})
	if err != nil {
		return out, err
	}
	out.Post = post
	out.Voice, err = s.Transcripts.RequestVoiceTranscript(ctx, p, chat.VoiceRecord{TranscriptionRequest: chat.TranscriptionRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: post.ID, ArtifactID: ref.ArtifactID, DurationMS: a.DurationMS}, Attachment: a})
	return out, err
}

// CompleteVoiceTranscript processes an already durable request. A crash leaves
// the pending row retryable; revision CAS prevents late workers overwriting
// corrections. No missing engine silently substitutes an external provider.
func CompleteVoiceTranscript(ctx context.Context, store chat.VoiceStore, worker chat.Principal, r chat.TranscriptionRequest, engine chat.Transcriber) (chat.VoiceRecord, error) {
	if store == nil {
		return chat.VoiceRecord{}, chat.ErrVoiceUnavailable
	}
	current, err := store.ReadVoiceTranscript(ctx, worker, r)
	if err != nil {
		return chat.VoiceRecord{}, err
	}
	if engine == nil {
		engine = chat.UnavailableTranscriber{}
	}
	value, engineErr := engine.Transcribe(ctx, current.TranscriptionRequest)
	if errors.Is(engineErr, chat.ErrVoiceUnavailable) {
		value = chat.VoiceTranscript{State: chat.TranscriptUnavailable}
	} else if engineErr != nil {
		value = chat.VoiceTranscript{State: chat.TranscriptFailed}
	}
	out, err := store.SaveVoiceTranscript(ctx, worker, r, current.Transcript.Revision, value)
	if err != nil {
		return chat.VoiceRecord{}, err
	}
	return out, engineErr
}
