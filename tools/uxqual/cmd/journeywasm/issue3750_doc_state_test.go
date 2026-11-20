package main

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestIssue3750_DocumentReadFailureIsFailedNotNeutral pins that a read that
// errored answers failed for every claimed document, while a read that
// succeeded but did not mention one keeps the neutral unavailable card.
func TestIssue3750_DocumentReadFailureIsFailedNotNeutral(t *testing.T) {
	ids := []string{"a", "b"}
	for name, err := range map[string]error{"unavailable": status.Error(codes.Unavailable, "down"), "deadline": status.Error(codes.DeadlineExceeded, "slow"), "cancelled": status.Error(codes.Canceled, "x"), "local": errors.New("socket closed")} {
		for id, got := range chatDocDefaultAnswers(ids, err) {
			if got.State != "failed" || got.ID != id || got.Readable || got.Title != "" {
				t.Errorf("%s: %s answered %+v, want failed", name, id, got)
			}
		}
	}
	for id, got := range chatDocDefaultAnswers(ids, status.Error(codes.PermissionDenied, "no")) {
		if got.State != "unavailable" {
			t.Errorf("refused %s = %+v, want the neutral unavailable card", id, got)
		}
	}
	for id, got := range chatDocDefaultAnswers(ids, nil) {
		if got.State != "unavailable" {
			t.Errorf("unanswered %s = %+v, want the neutral unavailable card", id, got)
		}
	}
}

// TestIssue3750_DocumentFailedAnswersRetryQuickly pins that a failed answer is
// read again after a short wait and on demand, while a settled one is not, and
// that a read that never answered becomes failed rather than neutral.
func TestIssue3750_DocumentFailedAnswersRetryQuickly(t *testing.T) {
	cache := &chatDocPreviewCache{}
	now := time.Unix(1000, 0)
	claims, epoch, _ := cache.claim("u", []string{"a", "b"}, now)
	if len(claims) != 2 {
		t.Fatalf("claims = %v", claims)
	}
	cache.finish("u", epoch, map[string]chatui.DocPreview{"a": {ID: "a", State: "failed"}, "b": {ID: "b", State: "unavailable"}}, now)
	if again, _, shown := cache.claim("u", []string{"a", "b"}, now.Add(5*time.Second)); len(again) != 0 || shown["a"].State != "failed" {
		t.Fatalf("within the short wait nothing is reread: claims %v shown %v", again, shown)
	}
	if again, _, _ := cache.claim("u", []string{"a", "b"}, now.Add(chatDocFailedFreshFor+time.Second)); len(again) != 1 || again[0] != "a" {
		t.Fatalf("after the short wait only the failed card is read again, got %v", again)
	}
	cache.claim("u", []string{"a", "b"}, now.Add(time.Minute))
	cache.finish("u", epoch, map[string]chatui.DocPreview{"a": {ID: "a", State: "failed"}, "b": {ID: "b", State: "unavailable"}}, now.Add(time.Minute))
	cache.retryFailed("u")
	if again, _, _ := cache.claim("u", []string{"a", "b"}, now.Add(time.Minute+time.Second)); len(again) != 1 || again[0] != "a" {
		t.Fatalf("retry must reread the failed card at once and only it, got %v", again)
	}
	if chatDocPreviewFingerprint(chatui.Model{DocPreviews: map[string]chatui.DocPreview{"a": {State: "failed"}}}) == chatDocPreviewFingerprint(chatui.Model{}) {
		t.Fatal("a failed answer must change the fingerprint so the card is re-rendered")
	}
}

// TestIssue3750_TicketFailedAnswerRetriesOnDemand pins that retry makes only
// failed ticket answers stale.
func TestIssue3750_TicketFailedAnswerRetriesOnDemand(t *testing.T) {
	cache := &chatProjectTaskPreviewCache{}
	now := time.Unix(1000, 0)
	refs := []chatui.ProjectTaskReference{{ProjectID: "p", TaskID: "a"}, {ProjectID: "p", TaskID: "b"}}
	_, epoch, _ := cache.claim("u", refs, now)
	cache.finish("u", epoch, map[string]chatui.ProjectTaskPreview{
		chatui.ProjectTaskPreviewKey("p", "a"): chatProjectTaskAnswer(refs[0], "failed"),
		chatui.ProjectTaskPreviewKey("p", "b"): chatProjectTaskAnswer(refs[1], "missing"),
	}, now)
	cache.retryFailed("u")
	again, _, _ := cache.claim("u", refs, now.Add(time.Second))
	if len(again) != 1 || again[0].TaskID != "a" {
		t.Fatalf("retry must reread only the failed ticket, got %v", again)
	}
}
