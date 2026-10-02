package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

type chatvoiceAccessFixture struct {
	allow bool
	calls int
}

func (f *chatvoiceAccessFixture) AuthorizeVoice(context.Context, chat.Principal, string, string, string) (chat.VoicePolicy, chat.ConversationKind, error) {
	f.calls++
	if !f.allow {
		return chat.VoicePolicy{}, chat.Direct, chat.ErrPermissionDenied
	}
	return chat.VoicePolicy{WorkspaceEnabled: true, PersonalEnabled: true}, chat.Direct, nil
}

type chatvoiceDecoderFixture struct {
	duration int64
	calls    int
}

func (f *chatvoiceDecoderFixture) InspectVoice(context.Context, []byte) (chat.VoiceInspection, error) {
	f.calls++
	return chat.VoiceInspection{ContentType: "audio/webm", DurationMS: f.duration, AudioOnly: true, Opus: true}, nil
}

type chatvoiceMediaFixture struct {
	calls int
	err   error
}

func (f *chatvoiceMediaFixture) Upload(_ context.Context, r chatmedia.UploadRequest) (chatmedia.Reference, error) {
	f.calls++
	if f.err != nil {
		return chatmedia.Reference{}, f.err
	}
	return chatmedia.Reference{TenantID: r.TenantID, ConversationID: r.ConversationID, ArtifactID: "audio", MediaType: chatmedia.MediaType("audio/webm"), Size: int64(len(r.Content)), State: chatmedia.StateAdmitted}, nil
}

type chatvoiceWriterFixture struct {
	calls   int
	request chat.SendPostWithReferencesRequest
}

func (f *chatvoiceWriterFixture) SendPostWithReferences(_ context.Context, r chat.SendPostWithReferencesRequest) (chat.Post, error) {
	f.calls++
	f.request = r
	return chat.Post{ID: "post", TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.Principal.SubjectID, References: r.References}, nil
}

type chatvoiceStoreFixture struct {
	record          chat.VoiceRecord
	requests, saves int
}

func (f *chatvoiceStoreFixture) RequestVoiceTranscript(_ context.Context, _ chat.Principal, r chat.VoiceRecord) (chat.VoiceRecord, error) {
	f.requests++
	r.Transcript = chat.VoiceTranscript{State: chat.TranscriptPending, Revision: 1}
	f.record = r
	return r, nil
}
func (f *chatvoiceStoreFixture) ReadVoiceTranscript(context.Context, chat.Principal, chat.TranscriptionRequest) (chat.VoiceRecord, error) {
	return f.record, nil
}
func (f *chatvoiceStoreFixture) SaveVoiceTranscript(_ context.Context, _ chat.Principal, _ chat.TranscriptionRequest, rev uint64, value chat.VoiceTranscript) (chat.VoiceRecord, error) {
	if rev != f.record.Transcript.Revision {
		return chat.VoiceRecord{}, chat.ErrInvalidArgument
	}
	f.saves++
	value.Revision = rev + 1
	f.record.Transcript = value
	return f.record, nil
}
func (f *chatvoiceStoreFixture) CorrectVoiceTranscript(context.Context, chat.Principal, chat.TranscriptionRequest, uint64, string) (chat.VoiceRecord, error) {
	return chat.VoiceRecord{}, chat.ErrPermissionDenied
}
func (f *chatvoiceStoreFixture) RetryVoiceTranscript(context.Context, chat.Principal, chat.TranscriptionRequest, uint64) (chat.VoiceRecord, error) {
	return chat.VoiceRecord{}, chat.ErrPermissionDenied
}
func (f *chatvoiceStoreFixture) ReportVoiceTranscript(context.Context, chat.Principal, chat.TranscriptionRequest) error {
	return chat.ErrPermissionDenied
}
func (f *chatvoiceStoreFixture) SearchVoiceTranscripts(context.Context, chat.Principal, string, string, int) ([]chat.VoiceRecord, error) {
	return nil, nil
}
func TestTodo_CHATVOICE_002_SendPorts(t *testing.T) {
	access := &chatvoiceAccessFixture{allow: true}
	decoder := &chatvoiceDecoderFixture{duration: 1000}
	media := &chatvoiceMediaFixture{}
	writer := &chatvoiceWriterFixture{}
	store := &chatvoiceStoreFixture{}
	service := VoiceService{Access: access, Decoder: decoder, Media: media, Messages: writer, Transcripts: store}
	out, err := service.Send(context.Background(), chat.Principal{TenantID: "tenant", SubjectID: "alice"}, VoiceSendRequest{TenantID: "tenant", ConversationID: "room", IdempotencyKey: "one", ContentType: "audio/webm", Content: []byte("fixture"), Note: "typed note", Waveform: []float64{0.1, 0.4}})
	if err != nil || out.Post.ID != "post" || out.Voice.Transcript.State != chat.TranscriptPending || out.Voice.Attachment.DurationMS != 1000 {
		t.Fatalf("send=%+v,%v", out, err)
	}
	if access.calls != 1 || decoder.calls != 1 || media.calls != 1 || writer.calls != 1 || store.requests != 1 {
		t.Fatal("pipeline incomplete")
	}
	if len(writer.request.References) != 1 || writer.request.References[0].Kind != chat.MediaAttachment || writer.request.Body != "typed note" {
		t.Fatal("not a media message")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		_, err := service.Send(t.Context(), chat.Principal{TenantID: "tenant", SubjectID: "alice"}, VoiceSendRequest{TenantID: "tenant", ConversationID: "room", IdempotencyKey: locale, ContentType: "audio/webm", Content: []byte("fixture"), Locale: locale})
		if err != nil || writer.request.Body != voiceMessageLabel(locale) || writer.request.References[0].Display != voiceMessageLabel(locale) {
			t.Fatalf("optional note locale=%s request=%+v err=%v", locale, writer.request, err)
		}
	}
}
func TestTodo_CHATVOICE_002_Security(t *testing.T) {
	access := &chatvoiceAccessFixture{}
	decoder := &chatvoiceDecoderFixture{duration: 1000}
	media := &chatvoiceMediaFixture{}
	writer := &chatvoiceWriterFixture{}
	store := &chatvoiceStoreFixture{}
	service := VoiceService{Access: access, Decoder: decoder, Media: media, Messages: writer, Transcripts: store}
	request := VoiceSendRequest{TenantID: "tenant", ConversationID: "room", IdempotencyKey: "one", ContentType: "audio/webm", Content: []byte("fixture")}
	if _, err := service.Send(context.Background(), chat.Principal{}, request); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal(err)
	}
	if decoder.calls != 0 || media.calls != 0 || writer.calls != 0 || store.requests != 0 {
		t.Fatal("side effect before authorization")
	}
	access.allow = true
	decoder.duration = 120001
	p := chat.Principal{TenantID: "tenant", SubjectID: "alice"}
	if _, err := service.Send(context.Background(), p, request); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal(err)
	}
	if media.calls != 0 {
		t.Fatal("overlong upload reached quarantine")
	}
	decoder.duration = 1000
	media.err = chatmedia.ErrQuarantined
	if _, err := service.Send(context.Background(), p, request); !errors.Is(err, chatmedia.ErrQuarantined) {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal("quarantined recording posted")
	}
}
func TestTodo_CHATVOICE_003_Fault(t *testing.T) {
	store := &chatvoiceStoreFixture{record: chat.VoiceRecord{TranscriptionRequest: chat.TranscriptionRequest{DurationMS: 1000}, Transcript: chat.VoiceTranscript{State: chat.TranscriptPending, Revision: 1}}}
	out, err := CompleteVoiceTranscript(context.Background(), store, chat.Principal{}, chat.TranscriptionRequest{}, nil)
	if !errors.Is(err, chat.ErrVoiceUnavailable) || out.Transcript.State != chat.TranscriptUnavailable || store.saves != 1 {
		t.Fatalf("missing engine not durable %+v,%v", out, err)
	}
	engine := chat.FixtureTranscriber{Result: chat.VoiceTranscript{State: chat.TranscriptReady, Language: "ar", Model: "fixture", Version: "1", Segments: []chat.TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: "موعد", Confidence: 1}}}}
	out, err = CompleteVoiceTranscript(context.Background(), store, chat.Principal{}, chat.TranscriptionRequest{}, engine)
	if err != nil || out.Transcript.Text() != "موعد" {
		t.Fatalf("fixture result=%+v,%v", out, err)
	}
}
func TestTodo_CHATVOICE_002(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, ChatVoicePath+"send", nil)
	recorder := httptest.NewRecorder()
	VoiceHTTP{}.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || recorder.Body.String() != "{\"code\":\"permission_denied\"}\n" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("untrusted HTTP=%d %s", recorder.Code, recorder.Body.String())
	}
	out := httptest.NewRecorder()
	voiceError(out, chat.ErrInvalidArgument)
	if out.Code != http.StatusBadRequest {
		t.Fatal(out.Code)
	}
	out = httptest.NewRecorder()
	voiceError(out, chat.ErrNotFound)
	if out.Code != http.StatusNotFound {
		t.Fatal(out.Code)
	}
	out = httptest.NewRecorder()
	voiceError(out, chat.ErrVoiceUnavailable)
	if out.Code != http.StatusServiceUnavailable {
		t.Fatal(out.Code)
	}
}
