package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3947Fixture(t *testing.T, tick string) (*Store, chat.Chatcmd002Request, chat.Post) {
	t.Helper()
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "cards", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}, {MemberID: "third", Role: "member", State: "active"}, {MemberID: "gone", Role: "member", State: "left"}})
	card := chat.Chatcmd002Card{Kind: "todo", Title: "Launch checklist", Todo: &chat.Chatcmd002Todo{Tick: tick, Items: []chat.Chatcmd002Task{
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "task-1", Text: "Book the room", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}, AssigneeID: "worker", AssigneeHomeTenantID: "tenant-a"},
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "task-2", Text: "Order food", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}, AssigneeID: "worker", AssigneeHomeTenantID: "tenant-a"},
	}}}
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "card-3947", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	post, err := chatPost(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, chat.Chatcmd002Request{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, TenantID: "tenant-a", ConversationID: "cards", PostID: post.ID, ExpectedRevision: post.Revision}, post
}

func issue3947As(r chat.Chatcmd002Request, subject string, revision uint64, m chat.Chatcmd002Mutation) chat.Chatcmd002Request {
	r.Principal.SubjectID, r.ExpectedRevision, r.Mutation = subject, revision, m
	return r
}

func reassign(item, home, person string) chat.Chatcmd002Mutation {
	return chat.Chatcmd002Mutation{Operation: "CHANGE_TASK", ItemID: item, SetAssignee: true, AssigneeHomeTenantID: home, AssigneeID: person}
}

// TestIssue3947_ChangeTask_Integration: the author reassigns one task and moves
// another's date; the card keeps its other task, its completion, one new
// revision per change with the author recorded, and one edit event each.
func TestIssue3947_ChangeTask_Integration(t *testing.T) {
	ctx := context.Background()
	s, r, post := issue3947Fixture(t, "anyone")
	// The assignee finishes task 1 first; handing it on later must not undo that.
	done, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "worker", post.Revision, chat.Chatcmd002Mutation{Operation: "TICK", ItemID: "task-1", Completed: true}), todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", done.Revision, reassign("task-1", "tenant-a", "third")), todoAllow)
	if err != nil || first.Revision != done.Revision+1 {
		t.Fatalf("reassign %+v %v", first, err)
	}
	due := time.Date(2027, 2, 3, 23, 59, 0, 0, time.UTC)
	second, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", first.Revision, chat.Chatcmd002Mutation{Operation: "CHANGE_TASK", ItemID: "task-2", SetDue: true, DueAt: &due}), todoAllow)
	if err != nil || second.Revision != first.Revision+1 {
		t.Fatalf("set due %+v %v", second, err)
	}
	card, ok := chat.Chatcmd002Decode(second.Body)
	if !ok {
		t.Fatal("card no longer decodes")
	}
	one, two := card.Todo.Items[0], card.Todo.Items[1]
	if one.AssigneeID != "third" || !one.Completed || one.CompletedBySubjectID != "worker" || one.ChangedBySubjectID != "owner" || one.Changed != chat.Chatcmd002ChangedAssignee || !card.Interacted {
		t.Fatalf("task 1 %+v", one)
	}
	if two.AssigneeID != "worker" || two.DueAt == nil || !two.DueAt.Equal(due) || two.Completed || two.ChangedBySubjectID != "owner" {
		t.Fatalf("task 2 %+v", two)
	}
	view, err := s.Chatcmd002Read(ctx, issue3947As(r, "third", 0, chat.Chatcmd002Mutation{}), todoAllow)
	if err != nil || view.CanManage || !view.CanTick["task-2"] {
		t.Fatalf("read as the new assignee %+v %v", view, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var revisions, events int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND author_id='owner' AND revision IN ($3,$4)`, "tenant-a", post.ID, first.Revision, second.Revision).Scan(&revisions); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='post.edited'`, "tenant-a", post.ID).Scan(&events); err != nil {
			return err
		}
		if revisions != 2 || events != 3 {
			t.Errorf("revisions %d events %d", revisions, events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestIssue3947_ChangeTask_Security: only the list's author may change a task;
// the new assignee must be a current member; a stale revision is a conflict; a
// closed list and an assignee-only list's last assignee are refused; none of
// the refusals writes a revision.
func TestIssue3947_ChangeTask_Security(t *testing.T) {
	ctx := context.Background()
	s, r, post := issue3947Fixture(t, "anyone")
	revision := func() uint64 {
		var n uint64
		if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT revision FROM chat_post WHERE tenant_id=$1 AND id=$2`, "tenant-a", post.ID).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	refuse := func(name string, req chat.Chatcmd002Request, want error) {
		t.Helper()
		if _, err := s.Chatcmd002Mutate(ctx, req, todoAllow); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
		if got := revision(); got != post.Revision {
			t.Fatalf("%s wrote revision %d", name, got)
		}
	}
	refuse("a member reassigning", issue3947As(r, "worker", post.Revision, reassign("task-1", "tenant-a", "third")), chat.ErrPermissionDenied)
	refuse("the assignee moving their own date", issue3947As(r, "worker", post.Revision, chat.Chatcmd002Mutation{Operation: "CHANGE_TASK", ItemID: "task-1", SetDue: true}), chat.ErrPermissionDenied)
	refuse("someone who left", issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "gone")), chat.ErrPermissionDenied)
	refuse("someone who never joined", issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "stranger")), chat.ErrPermissionDenied)
	refuse("a person of another tenant", issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-b", "worker")), chat.ErrPermissionDenied)
	refuse("a missing task", issue3947As(r, "owner", post.Revision, reassign("nope", "tenant-a", "third")), chat.ErrNotFound)
	refuse("a stale revision", issue3947As(r, "owner", post.Revision+5, reassign("task-1", "tenant-a", "third")), chat.ErrConflict)
	other := issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "third"))
	other.ConversationID = "elsewhere"
	refuse("another conversation", other, chat.ErrNotFound)
	closed, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", post.Revision, chat.Chatcmd002Mutation{Operation: "CLOSE"}), todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", closed.Revision, reassign("task-1", "tenant-a", "third")), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Errorf("a closed list: %v", err)
	}

	s, r, post = issue3947Fixture(t, "assignee")
	refuse("taking the last assignee off", issue3947As(r, "owner", post.Revision, reassign("task-1", "", "")), chat.ErrInvalidArgument)
	handed, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "third")), todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := s.Chatcmd002Read(ctx, issue3947As(r, "worker", 0, chat.Chatcmd002Mutation{}), todoAllow)
	if view.CanTick["task-1"] || !view.CanTick["task-2"] {
		t.Fatalf("the right to tick did not move with the task: %v", view.CanTick)
	}
	view, _ = s.Chatcmd002Read(ctx, issue3947As(r, "third", 0, chat.Chatcmd002Mutation{}), todoAllow)
	if !view.CanTick["task-1"] || handed.Revision != post.Revision+1 {
		t.Fatalf("the new assignee cannot tick: %v", view.CanTick)
	}
}

// TestIssue3947_ChangeTask_HistoryAndAudience_Security: a member who cannot read
// the list (no history, or joined after it was posted) is never given a task on
// it, and a list addressed to segments takes no new assignee at all.
func TestIssue3947_ChangeTask_HistoryAndAudience_Security(t *testing.T) {
	ctx := context.Background()
	s, r, post := issue3947Fixture(t, "anyone")
	setVisibility := func(member, visibility string, joined time.Time) {
		t.Helper()
		if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility=$1,joined_at=$2 WHERE tenant_id='tenant-a' AND conversation_id='cards' AND member_id=$3`, visibility, joined, member)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	setVisibility("third", "NO_HISTORY", time.Now().Add(-time.Hour))
	if _, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "third")), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Errorf("a member with no history: %v", err)
	}
	setVisibility("third", "FROM_JOIN", time.Now().Add(time.Hour))
	if _, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "third")), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Errorf("a member who joined after the post: %v", err)
	}
	setVisibility("third", "FULL_HISTORY", time.Now())
	if _, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", post.Revision, reassign("task-1", "tenant-a", "third")), todoAllow); err != nil {
		t.Errorf("a member who can read the list: %v", err)
	}

	s, r, _ = issue3947Fixture(t, "anyone")
	card := chat.Chatcmd002Card{Kind: "todo", Title: "Addressed", Audience: []string{"segment-1"}, Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "t-1", Text: "Do it", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}, AssigneeID: "worker", AssigneeHomeTenantID: "tenant-a"}}}}
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "card-3947-aud", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	r.PostID, r.ExpectedRevision = raw.ID, uint64(raw.Revision)
	if _, err := s.Chatcmd002Mutate(ctx, issue3947As(r, "owner", uint64(raw.Revision), reassign("t-1", "tenant-a", "third")), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Errorf("a list addressed to segments: %v", err)
	}
}
