package chatmedia

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// VoiceMaxBytes bounds one recording; two minutes of speech-rate Opus is far
// below it, so a larger file is not a voice message.
const VoiceMaxBytes = 4 << 20

// UploadVoice admits a voice message. It is the same path as Upload (same
// authorization, quarantine and scanner, the artifact reachable only once the
// verdict is ADMITTED) with one difference: the container is decoded here and
// the type, the codec and the duration come from the stream. The declared type
// and the client's duration decide nothing. The text alternative an audio
// artifact needs is the transcript, which arrives later from the worker.
func (s *Service) UploadVoice(ctx context.Context, req UploadRequest) (Reference, error) {
	if s == nil || s.store == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.PrincipalID) == "" || len(req.Content) == 0 {
		return Reference{}, ErrInvalid
	}
	if len(req.Content) > VoiceMaxBytes || int64(len(req.Content)) > s.maxBytes {
		return Reference{}, fmt.Errorf("%w: a voice message is at most %d bytes", ErrInvalid, VoiceMaxBytes)
	}
	declared, ok := voiceBaseType(req.DeclaredType)
	if !ok {
		return Reference{}, ErrUnsupported
	}
	facts, err := InspectVoice(req.Content)
	if err != nil || facts.MediaType != declared || facts.Duration <= 0 || facts.Duration > MaxVoiceDuration {
		return Reference{}, ErrUnsupported
	}
	id := scopedArtifactID(req.TenantID, req.ConversationID, req.Content)
	if s.authorize == nil {
		return Reference{}, ErrUnauthorized
	}
	if err := s.authorize(ctx, AccessRequest{TenantID: req.TenantID, ConversationID: req.ConversationID, PrincipalID: req.PrincipalID, ArtifactID: id}); err != nil {
		return Reference{}, ErrUnauthorized
	}
	ref := Reference{ArtifactID: id, TenantID: req.TenantID, ConversationID: req.ConversationID, MediaType: declared, Size: int64(len(req.Content)), State: StateQuarantined, AltText: req.AltText}
	if err := s.store.Quarantine(ctx, Artifact{Reference: ref, Content: append([]byte(nil), req.Content...), Reason: "awaiting scanner verdict"}); err != nil {
		return Reference{}, err
	}
	if s.scanner == nil {
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, "scanner unavailable", "")
		return Reference{}, ErrScannerUnavailable
	}
	verdict, err := s.scanner.Scan(ctx, id, bytes.NewReader(req.Content))
	if err != nil {
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, err.Error(), "")
		return Reference{}, ErrScannerUnavailable
	}
	if !verdict.Safe {
		reason := verdict.Reason
		if reason == "" {
			reason = "scanner reported unsafe content"
		}
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, reason, "")
		return Reference{}, ErrQuarantined
	}
	if err := s.store.SetVerdict(ctx, id, req.TenantID, StateAdmitted, "", ""); err != nil {
		return Reference{}, err
	}
	ref.State = StateAdmitted
	return ref, nil
}

// ReadVoice returns an admitted voice artifact's bytes for the transcription
// worker. It has no grant: the caller is the server's own worker, which has
// already proved that this tenant, conversation and artifact are one pending
// transcript request, and the artifact must belong to that conversation.
func (s *Service) ReadVoice(ctx context.Context, tenant, conversation, artifact string) ([]byte, MediaType, error) {
	if s == nil || s.store == nil || tenant == "" || conversation == "" || artifact == "" {
		return nil, "", ErrInvalid
	}
	a, err := s.store.Get(ctx, tenant, artifact)
	if err != nil {
		return nil, "", err
	}
	if a.TenantID != tenant || a.ConversationID != conversation {
		return nil, "", ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return nil, "", ErrQuarantined
	}
	if a.MediaType != MediaWebM && a.MediaType != MediaOgg {
		return nil, "", ErrUnsupported
	}
	return append([]byte(nil), a.Content...), a.MediaType, nil
}

// VoiceScanner is the admission scan for voice recordings. The attachment
// scanner admits documents and images and refuses audio containers, so voice
// has its own: the whole stream must decode as audio-only Opus within the
// limits, and the deployment's external scanner, when configured, must also
// admit the original bytes.
type VoiceScanner struct{ External Scanner }

func (s VoiceScanner) Scan(ctx context.Context, id string, r io.Reader) (quarantine.Verdict, error) {
	data, err := io.ReadAll(io.LimitReader(r, VoiceMaxBytes+1))
	if err != nil {
		return quarantine.Verdict{}, err
	}
	if err := ctx.Err(); err != nil {
		return quarantine.Verdict{}, err
	}
	if len(data) > VoiceMaxBytes {
		return quarantine.Verdict{Reason: "recording too large"}, nil
	}
	facts, err := InspectVoice(data)
	if err != nil || facts.Duration > MaxVoiceDuration {
		return quarantine.Verdict{Reason: "not an audio-only Opus recording of at most two minutes"}, nil
	}
	if s.External != nil {
		return s.External.Scan(ctx, id, bytes.NewReader(data))
	}
	return quarantine.Verdict{Safe: true, Reason: "bounded structural audio check only"}, nil
}
