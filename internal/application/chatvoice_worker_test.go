package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatvoiceQueueFixture struct {
	chatvoiceStoreFixture
	request chat.TranscriptionRequest
	err     error
	queried bool
}

func (f *chatvoiceQueueFixture) PendingVoiceTranscripts(_ context.Context, p chat.Principal, tenant, conversation string, limit int) ([]chat.TranscriptionRequest, error) {
	f.queried = true
	if p.SubjectID != chat.VoiceWorkerSubject || tenant != "tenant" || conversation != "room" || limit != 1 {
		return nil, chat.ErrPermissionDenied
	}
	return []chat.TranscriptionRequest{f.request}, f.err
}

func TestTodo_CHATVOICE_003(t *testing.T) {
	r := chat.TranscriptionRequest{TenantID: "tenant", ConversationID: "room", PostID: "post", ArtifactID: "audio", DurationMS: 1000}
	worker := chat.Principal{TenantID: "tenant", SubjectID: chat.VoiceWorkerSubject}
	for _, state := range []chat.TranscriptState{chat.TranscriptReady, chat.TranscriptUnavailable, chat.TranscriptFailed} {
		f := &chatvoiceQueueFixture{request: r, chatvoiceStoreFixture: chatvoiceStoreFixture{record: chat.VoiceRecord{TranscriptionRequest: r, Transcript: chat.VoiceTranscript{State: chat.TranscriptPending, Revision: 1}}}}
		var engine chat.Transcriber
		if state == chat.TranscriptReady {
			engine = chat.FixtureTranscriber{Result: chat.VoiceTranscript{State: state, Language: "de-DE", Model: "fixture", Version: "1", Segments: []chat.TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: "Besprechung", Confidence: 1}}}}
		} else if state == chat.TranscriptFailed {
			engine = chat.FixtureTranscriber{Failure: errors.New("worker fixture failure")}
		}
		out, err := ProcessVoiceTranscriptBatch(t.Context(), f, worker, "tenant", "room", 1, engine)
		if err != nil || f.record.Transcript.State != state || f.saves != 1 || out.Ready+out.Unavailable+out.Failed != 1 {
			t.Fatalf("batch state=%s result=%+v record=%+v err=%v", state, out, f.record, err)
		}
	}
}

func TestTodo_CHATVOICE_003_Security(t *testing.T) {
	f := &chatvoiceQueueFixture{request: chat.TranscriptionRequest{TenantID: "other", ConversationID: "room"}}
	worker := chat.Principal{TenantID: "tenant", SubjectID: chat.VoiceWorkerSubject}
	if _, err := ProcessVoiceTranscriptBatch(t.Context(), f, worker, "tenant", "room", 1, nil); !errors.Is(err, chat.ErrPermissionDenied) || f.saves != 0 {
		t.Fatalf("cross-tenant queue=%v saves=%d", err, f.saves)
	}
	f.queried = false
	if _, err := ProcessVoiceTranscriptBatch(t.Context(), f, worker, "tenant", "room", 101, nil); !errors.Is(err, chat.ErrInvalidArgument) || f.queried {
		t.Fatalf("unbounded backlog=%v queried=%v", err, f.queried)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ProcessVoiceTranscriptBatch(ctx, f, worker, "tenant", "room", 1, nil); !errors.Is(err, context.Canceled) || f.saves != 0 {
		t.Fatalf("canceled batch=%v saves=%d", err, f.saves)
	}
}
