package projectservice

import (
	"context"
	"errors"
	"testing"
)

type chatLinkRepoStub struct {
	taskLinkRepo
	rows        []ChatLinkedTaskRecord
	tenant, con string
}

func (r *chatLinkRepoStub) ListTasksLinkedToChatConversation(_ context.Context, tenant, conversation string, _ int) ([]ChatLinkedTaskRecord, error) {
	r.tenant, r.con = tenant, conversation
	return r.rows, nil
}

// Issue 3790: work made from a message is listed only from projects the viewer
// can read. A project they cannot read is omitted, never reported as hidden, so
// the message does not reveal that restricted work exists.
func TestIssue3790_ChatLinkedTasksOmitUnreadableProjects_Security(t *testing.T) {
	repo := &chatLinkRepoStub{rows: []ChatLinkedTaskRecord{
		{PostID: "p1", ProjectID: "project-visible", TaskID: "task-a", Title: "Swap laptop"},
		{PostID: "p1", ProjectID: "project-private", TaskID: "task-b", Title: "Confidential case"},
	}}
	svc := Service{Auth: workOrderReverseAuth{deniedProject: "project-private"}, Links: repo}
	got, err := svc.ListTasksLinkedToChatConversation(context.Background(), testPrincipal(t), "conv-1", 50)
	if err != nil || len(got) != 1 || got[0].TaskID != "task-a" {
		t.Fatalf("linked=%+v err=%v", got, err)
	}
	if repo.tenant != "tenant-a" || repo.con != "conv-1" {
		t.Fatalf("lookup was not scoped: tenant=%q conversation=%q", repo.tenant, repo.con)
	}
	if _, err := svc.ListTasksLinkedToChatConversation(context.Background(), testPrincipal(t), "", 50); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty conversation err=%v", err)
	}
	if _, err := (Service{Auth: workOrderReverseAuth{}, Links: &taskLinkRepo{}}).ListTasksLinkedToChatConversation(context.Background(), testPrincipal(t), "conv-1", 50); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsupported store err=%v", err)
	}
}
