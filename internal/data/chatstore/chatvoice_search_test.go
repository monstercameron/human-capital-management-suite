package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestTodo_CHATVOICE_004_Integration_Search(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := t.Context()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	wc, worker := chatvoiceWorker(t)
	if _, err := s.SaveVoiceTranscript(wc, worker, r.TranscriptionRequest, 1, chatvoiceReady("meeting tomorrow")); err != nil {
		t.Fatal(err)
	}
	allowed := true
	authority := func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (bool, error) {
		if row.Text != "" {
			t.Fatal("text loaded before message authority")
		}
		return allowed, nil
	}
	registry := chatsearch.NewRegistry()
	if err := s.RegisterVoiceSearch(registry, authority); err != nil {
		t.Fatal(err)
	}
	source := &ChatvoiceSearchSource{Adapter: s, Kind: chatsearch.Voice, Authorize: authority}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: p.TenantID, HomeTenantID: p.TenantID, PersonID: p.SubjectID}, Query: "tomorrow"}
	rows, err := source.Search(ctx, q)
	if err != nil || len(rows) != 1 || rows[0].Target.MessageID != r.PostID || !rows[0].HasVoice {
		t.Fatalf("source=%+v,%v", rows, err)
	}
	if ok, err := source.CanOpen(ctx, q.Actor, rows[0]); err != nil || !ok {
		t.Fatalf("open=%v,%v", ok, err)
	}
	allowed = false
	if hidden, err := source.Search(ctx, q); err != nil || len(hidden) != 0 {
		t.Fatalf("authority leak=%+v,%v", hidden, err)
	}
	allowed = true
	if _, err := s.CorrectVoiceTranscript(ctx, p, r.TranscriptionRequest, 2, "meeting Friday"); err != nil {
		t.Fatal(err)
	}
	if ok, err := source.CanOpen(ctx, q.Actor, rows[0]); err != nil || ok {
		t.Fatalf("stale rendering=%v,%v", ok, err)
	}
	source.Kind = chatsearch.VoiceCorrection
	q.Query = "Friday"
	if corrected, err := source.Search(ctx, q); err != nil || len(corrected) != 1 || corrected[0].Text != "meeting Friday" {
		t.Fatalf("correction source=%+v,%v", corrected, err)
	}
	q.Actor.PersonID = "bob"
	if _, err := s.RemoveMembership(ctx, p, p.TenantID, r.ConversationID, p.TenantID, "bob", 1); err != nil {
		t.Fatal(err)
	}
	if hidden, err := source.Search(ctx, q); err != nil || len(hidden) != 0 {
		t.Fatalf("removed member search=%+v,%v", hidden, err)
	}
}
