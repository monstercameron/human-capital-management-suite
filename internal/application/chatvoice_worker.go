package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type VoiceTranscriptQueue interface {
	chat.VoiceStore
	PendingVoiceTranscripts(context.Context, chat.Principal, string, string, int) ([]chat.TranscriptionRequest, error)
}

type VoiceBatchResult struct {
	Ready, Unavailable, Failed int
}

// ProcessVoiceTranscriptBatch is an explicit deployment worker seam. The queue
// checks the worker's current conversation access; completion checks it again.
// A canceled or interrupted batch leaves untouched durable requests pending.
func ProcessVoiceTranscriptBatch(ctx context.Context, store VoiceTranscriptQueue, worker chat.Principal, tenant, conversation string, limit int, engine chat.Transcriber) (VoiceBatchResult, error) {
	var out VoiceBatchResult
	if store == nil {
		return out, chat.ErrVoiceUnavailable
	}
	if tenant == "" || conversation == "" || limit < 1 || limit > 100 {
		return out, chat.ErrInvalidArgument
	}
	requests, err := store.PendingVoiceTranscripts(ctx, worker, tenant, conversation, limit)
	if err != nil {
		return out, err
	}
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if request.TenantID != tenant || request.ConversationID != conversation {
			return out, chat.ErrPermissionDenied
		}
		record, err := CompleteVoiceTranscript(ctx, store, worker, request, engine)
		// Engine errors are already persisted as visible retryable states. Store
		// errors leave the request pending and stop this batch for a later retry.
		if record.Transcript.Revision == 0 || (err != nil && record.Transcript.State != chat.TranscriptUnavailable && record.Transcript.State != chat.TranscriptFailed) {
			return out, errors.Join(chat.ErrVoiceUnavailable, err)
		}
		switch record.Transcript.State {
		case chat.TranscriptReady:
			out.Ready++
		case chat.TranscriptUnavailable:
			out.Unavailable++
		case chat.TranscriptFailed:
			out.Failed++
		default:
			return out, errors.Join(chat.ErrInvalidArgument, err)
		}
	}
	return out, nil
}
