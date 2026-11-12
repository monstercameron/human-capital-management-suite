package main

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestChatProjectPreview3750ReadFailureState pins that a refusal stays the
// neutral answer while a missing record, a transient failure and a cancelled
// batch each say what happened.
func TestChatProjectPreview3750ReadFailureState(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"permission denied is restricted", status.Error(codes.PermissionDenied, "no grant"), "restricted"},
		{"not found is missing", status.Error(codes.NotFound, "deleted"), "missing"},
		{"unavailable is retryable failure", status.Error(codes.Unavailable, "down"), "failed"},
		{"deadline is retryable failure", status.Error(codes.DeadlineExceeded, "slow"), "failed"},
		{"cancelled batch is retryable failure", status.Error(codes.Canceled, "cancelled"), "failed"},
		{"plain local error is retryable failure", errors.New("socket closed"), "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatProjectReadFailureState(tc.err); got != tc.want {
				t.Fatalf("chatProjectReadFailureState(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestChatProjectPreview3750FinishKeepsSettledAnswers pins which resolved
// answers the cache stores: settled states survive, while a ready card without
// a readable title and an answer for a different task are refused.
func TestChatProjectPreview3750FinishKeepsSettledAnswers(t *testing.T) {
	ref := chatui.ProjectTaskReference{ProjectID: "proj-7", TaskID: "task-19"}
	key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
	cases := []struct {
		name   string
		answer chatui.ProjectTaskPreview
		want   string
	}{
		{"missing kept", chatProjectTaskAnswer(ref, "missing"), "missing"},
		{"archived kept", chatProjectTaskAnswer(ref, "archived"), "archived"},
		{"failed kept", chatProjectTaskAnswer(ref, "failed"), "failed"},
		{"ready with title kept", chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Draft", Readable: true, State: "ready"}, "ready"},
		{"ready without readable flag refused", chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Draft", State: "ready"}, "restricted"},
		{"unknown state refused", chatProjectTaskAnswer(ref, "loading"), "restricted"},
		{"answer for another task refused", chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: "task-99", State: "missing"}, "restricted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			cache := &chatProjectTaskPreviewCache{}
			_, epoch, _ := cache.claim("viewer", []chatui.ProjectTaskReference{ref}, now)
			if !cache.finish("viewer", epoch, map[string]chatui.ProjectTaskPreview{key: tc.answer}, now) {
				t.Fatal("finish refused the current epoch")
			}
			got := cache.projection("viewer", []chatui.ProjectTaskReference{ref}, now)[key]
			if got.State != tc.want {
				t.Fatalf("cached state = %q, want %q", got.State, tc.want)
			}
		})
	}
}
