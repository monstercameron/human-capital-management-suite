package chatstore

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_024_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "reactions", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "sam", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: "host", SubjectID: "sam", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	members := []chat.Membership{m}
	for _, home := range []string{"home-a", "home-b"} {
		other := m
		other.HomeTenantID = home
		other.Role = chat.Member
		members = append(members, other)
	}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p"}, chat.Post{AuthorID: "sam", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	for _, home := range []string{"home-a", "home-b"} {
		x := chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, HomeTenantID: home, SubjectID: "sam", Emoji: "+1"}
		if _, err = s.PutReaction(ctx, x); err != nil {
			t.Fatal(err)
		}
		if _, err = s.PutReaction(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	check, checkErr := s.pool.Begin(ctx)
	if checkErr != nil {
		t.Fatal(checkErr)
	}
	defer check.Rollback(ctx)
	if checkErr = tenant(ctx, check, c.TenantID); checkErr != nil {
		t.Fatal(checkErr)
	}
	var events int
	if checkErr = check.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='reaction.added'`, c.TenantID, c.ID).Scan(&events); checkErr != nil {
		t.Fatal(checkErr)
	}
	if events != 2 {
		t.Fatalf("duplicate add emitted %d events", events)
	}
	if checkErr = check.Commit(ctx); checkErr != nil {
		t.Fatal(checkErr)
	}
	read := func(page chat.Page) chat.ListReactionsResponse {
		t.Helper()
		out, e := s.ListReactions(ctx, chat.Principal{TenantID: "host", SubjectID: "sam"}, c.TenantID, c.ID, p.ID, page)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	first := read(chat.Page{PageSize: 1})
	if len(first.Reactions) != 1 || first.NextCursor == "" {
		t.Fatalf("first=%+v", first)
	}
	second := read(chat.Page{PageSize: 1, Cursor: first.NextCursor})
	if len(second.Reactions) != 1 || second.NextCursor != "" || first.Reactions[0].HomeTenantID == second.Reactions[0].HomeTenantID {
		t.Fatalf("second=%+v first=%+v", second, first)
	}
	if _, err = s.ListReactions(ctx, chat.Principal{TenantID: "home-a", SubjectID: "sam"}, c.TenantID, c.ID, p.ID, chat.Page{Cursor: first.NextCursor}); err != chat.ErrInvalidArgument {
		t.Fatalf("foreign cursor=%v", err)
	}
	historyTx, historyErr := s.pool.Begin(ctx)
	if historyErr != nil {
		t.Fatal(historyErr)
	}
	defer historyTx.Rollback(ctx)
	if historyErr = tenant(ctx, historyTx, c.TenantID); historyErr != nil {
		t.Fatal(historyErr)
	}
	if _, historyErr = historyTx.Exec(ctx, `UPDATE chat_membership SET history_visibility='NO_HISTORY' WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id='host'`, c.TenantID, c.ID); historyErr != nil {
		t.Fatal(historyErr)
	}
	if historyErr = historyTx.Commit(ctx); historyErr != nil {
		t.Fatal(historyErr)
	}
	if got := read(chat.Page{}); len(got.Reactions) != 0 {
		t.Fatalf("no history=%+v", got)
	}
	restore, restoreErr := s.pool.Begin(ctx)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	defer restore.Rollback(ctx)
	if restoreErr = tenant(ctx, restore, c.TenantID); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if _, restoreErr = restore.Exec(ctx, `UPDATE chat_membership SET history_visibility='FULL_HISTORY' WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id='host'`, c.TenantID, c.ID); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if restoreErr = restore.Commit(ctx); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err = s.RemoveReaction(ctx, c.TenantID, c.ID, p.ID, "home-a", "sam", "+1"); err != nil {
		t.Fatal(err)
	}
	if got := read(chat.Page{}); len(got.Reactions) != 1 || got.Reactions[0].HomeTenantID != "home-b" {
		t.Fatalf("remaining=%+v", got)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id='host'`, c.TenantID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(chat.Page{}); len(got.Reactions) != 0 {
		t.Fatalf("revoked read=%+v", got)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, HomeTenantID: "host", SubjectID: "sam", Emoji: "heart"}); err != chat.ErrPermissionDenied {
		t.Fatalf("revoked add=%v", err)
	}
	if err = s.RemoveReaction(ctx, c.TenantID, c.ID, p.ID, "host", "sam", "heart"); err != chat.ErrPermissionDenied {
		t.Fatalf("revoked remove=%v", err)
	}
}
