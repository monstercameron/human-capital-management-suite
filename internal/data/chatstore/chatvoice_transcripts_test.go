package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func chatvoiceDB(t *testing.T) (*Adapter, chat.Principal, chat.VoiceRecord) {
	t.Helper()
	s := adapterDB(t)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	ctx := context.Background()
	members := []chat.Membership{}
	for _, id := range []string{"alice", "bob", chat.VoiceWorkerSubject} {
		members = append(members, chat.Membership{TenantID: p.TenantID, HomeTenantID: p.TenantID, ConversationID: "voice-room", SubjectID: id, Role: chat.Member, HistoryVisibility: chat.FullHistory})
	}
	members[0].Role = chat.Manager
	if _, err := s.CreateConversation(ctx, chat.Conversation{ID: "voice-room", TenantID: p.TenantID, Kind: chat.Direct, OwnerID: p.SubjectID, Revision: 1}, members, ""); err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "voice-room", IdempotencyKey: "voice-send"}, chat.Post{AuthorID: p.SubjectID, Body: "optional note", References: []chat.Reference{{Kind: chat.MediaAttachment, TenantID: p.TenantID, ConversationID: "voice-room", ID: "audio-1", ContentType: "audio/webm", ByteSize: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	r := chat.VoiceRecord{TranscriptionRequest: chat.TranscriptionRequest{TenantID: p.TenantID, ConversationID: "voice-room", PostID: post.ID, ArtifactID: "audio-1", DurationMS: 1000}, Attachment: chat.VoiceAttachment{ArtifactID: "audio-1", ContentType: "audio/webm", Bytes: 7, DurationMS: 1000, Waveform: []float64{0.2, 0.4}, TranscriptState: chat.TranscriptPending}}
	return s, p, r
}
func chatvoiceWorker(t *testing.T) (context.Context, chat.Principal) {
	t.Helper()
	now := time.Now()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: chat.VoiceWorkerSubject, SubjectKind: trust.SubjectKindIntegration, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "voice-fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), identity), chat.Principal{TenantID: "tenant-a", SubjectID: chat.VoiceWorkerSubject}
}
func chatvoiceReady(text string) chat.VoiceTranscript {
	return chat.VoiceTranscript{State: chat.TranscriptReady, Language: "en-US", Model: "fixture", Version: "1", Segments: []chat.TranscriptSegment{{StartMS: 0, EndMS: 1000, Text: text, Confidence: 0.9}}}
}
func TestTodo_CHATVOICE_003_Integration(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	out, err := s.RequestVoiceTranscript(ctx, p, r)
	if err != nil || out.Transcript.State != chat.TranscriptPending || out.Transcript.Revision != 1 {
		t.Fatalf("request=%+v err=%v", out, err)
	}
	retry, err := s.RequestVoiceTranscript(ctx, p, r)
	if err != nil || retry.Transcript.Revision != 1 {
		t.Fatalf("idempotent=%+v err=%v", retry, err)
	}
	workerCtx, worker := chatvoiceWorker(t)
	out, err = s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 1, chatvoiceReady("meeting tomorrow"))
	if err != nil || out.Transcript.Text() != "meeting tomorrow" {
		t.Fatalf("complete=%+v err=%v", out, err)
	}
	results, err := s.SearchVoiceTranscripts(ctx, p, p.TenantID, "tomorrow", 10)
	if err != nil || len(results) != 1 || results[0].PostID != r.PostID {
		t.Fatalf("search=%+v err=%v", results, err)
	}
	out, err = s.CorrectVoiceTranscript(ctx, p, r.TranscriptionRequest, 2, "meeting Friday")
	if err != nil || out.Transcript.Correction != "meeting Friday" || out.Transcript.Revision != 3 {
		t.Fatalf("correct=%+v err=%v", out, err)
	}
	out, err = s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 3, chatvoiceReady("model replacement"))
	if err != nil || out.Transcript.Text() != "meeting Friday" {
		t.Fatalf("retranscription lost correction %+v,%v", out, err)
	}
	results, err = s.SearchVoiceTranscripts(ctx, p, p.TenantID, "Friday", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("corrected search=%+v,%v", results, err)
	}
	out, err = s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 4, chat.VoiceTranscript{State: chat.TranscriptFailed})
	if err != nil || out.Transcript.State != chat.TranscriptFailed {
		t.Fatalf("failed replacement=%+v,%v", out, err)
	}
	out, err = s.RetryVoiceTranscript(ctx, p, r.TranscriptionRequest, 5)
	if err != nil || out.Transcript.State != chat.TranscriptPending {
		t.Fatalf("retry replacement=%+v,%v", out, err)
	}
	out, err = s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 6, chatvoiceReady("new engine text"))
	if err != nil || out.Transcript.Text() != "meeting Friday" {
		t.Fatalf("failed replacement lost correction=%+v,%v", out, err)
	}
	if err = s.ReportVoiceTranscript(ctx, p, r.TranscriptionRequest); err != nil {
		t.Fatal(err)
	}
	if err = s.ReportVoiceTranscript(ctx, p, r.TranscriptionRequest); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.voiceTx(ctx, p, r.TranscriptionRequest, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT jsonb_array_length(reports) FROM chat_voice_transcript WHERE tenant_id=$1 AND post_id=$2`, p.TenantID, r.PostID).Scan(&count)
	}); err != nil || count != 1 {
		t.Fatalf("report idempotency=%d,%v", count, err)
	}
}
func TestTodo_CHATVOICE_003_Fault(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	workerCtx, worker := chatvoiceWorker(t)
	out, err := s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 1, chat.VoiceTranscript{State: chat.TranscriptUnavailable})
	if err != nil || out.Transcript.State != chat.TranscriptUnavailable {
		t.Fatalf("unavailable=%+v,%v", out, err)
	}
	out, err = s.RetryVoiceTranscript(ctx, p, r.TranscriptionRequest, 2)
	if err != nil || out.Transcript.State != chat.TranscriptPending || out.Transcript.Revision != 3 {
		t.Fatalf("retry=%+v,%v", out, err)
	}
	if _, err = s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 1, chatvoiceReady("late worker")); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("late worker=%v", err)
	}
	out, err = s.ReadVoiceTranscript(ctx, p, r.TranscriptionRequest)
	if err != nil || out.Transcript.State != chat.TranscriptPending {
		t.Fatalf("pending durable after stale worker=%+v,%v", out, err)
	}
}
func TestTodo_CHATVOICE_004_Security(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	bob := chat.Principal{TenantID: p.TenantID, SubjectID: "bob"}
	if _, err := s.CorrectVoiceTranscript(ctx, bob, r.TranscriptionRequest, 1, "forged"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("reader correction=%v", err)
	}
	if _, err := s.SaveVoiceTranscript(ctx, p, r.TranscriptionRequest, 1, chatvoiceReady("fake engine")); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("fake engine=%v", err)
	}
	outsider := chat.Principal{TenantID: "other", SubjectID: p.SubjectID}
	if _, err := s.ReadVoiceTranscript(ctx, outsider, r.TranscriptionRequest); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross tenant=%v", err)
	}
	workerCtx, worker := chatvoiceWorker(t)
	if _, err := s.SaveVoiceTranscript(workerCtx, worker, r.TranscriptionRequest, 1, chatvoiceReady("secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveMembership(ctx, p, p.TenantID, "voice-room", p.TenantID, "bob", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadVoiceTranscript(ctx, bob, r.TranscriptionRequest); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("removed reader=%v", err)
	}
	results, err := s.SearchVoiceTranscripts(ctx, bob, p.TenantID, "secret", 10)
	if err != nil || len(results) != 0 {
		t.Fatalf("removed search leak=%+v,%v", results, err)
	}
}
func TestTodo_CHATVOICE_005_Property(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "erased", true: "held"}[held], func(t *testing.T) {
			s, p, r := chatvoiceDB(t)
			ctx := context.Background()
			if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
				t.Fatal(err)
			}
			wc, worker := chatvoiceWorker(t)
			if _, err := s.SaveVoiceTranscript(wc, worker, r.TranscriptionRequest, 1, chatvoiceReady("secret")); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CorrectVoiceTranscript(ctx, p, r.TranscriptionRequest, 2, "corrected secret"); err != nil {
				t.Fatal(err)
			}
			if held {
				if err := s.PlaceRecordHold(ctx, p.TenantID, "hold", "matter", "fixture", p.SubjectID, []string{"post:" + r.PostID}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "voice-room", PostID: r.PostID, ExpectedRevision: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadVoiceTranscript(ctx, p, r.TranscriptionRequest); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("tombstone readable %v", err)
			}
			results, err := s.SearchVoiceTranscripts(ctx, p, p.TenantID, "secret", 10)
			if err != nil || len(results) != 0 {
				t.Fatalf("tombstone search=%+v,%v", results, err)
			}
			var count int
			err = s.Store.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM chat_voice_transcript WHERE tenant_id=$1 AND post_id=$2`, p.TenantID, r.PostID).Scan(&count)
			})
			want := 0
			if held {
				want = 1
			}
			if err != nil || count != want {
				t.Fatalf("retained parts=%d want %d err=%v", count, want, err)
			}
		})
	}
}

func TestTodo_CHATVOICE_005_Integration_Export(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := t.Context()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	wc, worker := chatvoiceWorker(t)
	if _, err := s.SaveVoiceTranscript(wc, worker, r.TranscriptionRequest, 1, chatvoiceReady("original wording")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CorrectVoiceTranscript(ctx, p, r.TranscriptionRequest, 2, "author wording"); err != nil {
		t.Fatal(err)
	}
	out, err := s.ExportVoiceTranscript(ctx, p, r.TranscriptionRequest)
	if err != nil || out.Record.ArtifactID != r.ArtifactID || out.Record.Transcript.Text() != "author wording" || len(out.Record.Transcript.Segments) != 1 || len(out.Corrections) != 1 || out.Corrections[0].Author != p.SubjectID || out.Corrections[0].Text != "author wording" || out.Corrections[0].Revision != 3 {
		t.Fatalf("voice export=%+v,%v", out, err)
	}
	if _, err = s.ExportVoiceTranscript(ctx, chat.Principal{TenantID: "other", SubjectID: p.SubjectID}, r.TranscriptionRequest); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross-tenant export=%v", err)
	}
}
