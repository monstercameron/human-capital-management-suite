package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChatvoiceSwitches(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	policy, err := s.VoicePolicy(ctx, p.TenantID, p.SubjectID, r.ConversationID)
	if err != nil || !policy.WorkspaceEnabled || !policy.PersonalEnabled || policy.ChannelEnabled {
		t.Fatalf("defaults %+v err=%v", policy, err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", VoiceScopeChannel, r.ConversationID, true); err != nil {
		t.Fatal(err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, p.SubjectID, VoiceScopePerson, p.SubjectID, false); err != nil {
		t.Fatal(err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", VoiceScopeWorkspace, "", false); err != nil {
		t.Fatal(err)
	}
	policy, _ = s.VoicePolicy(ctx, p.TenantID, p.SubjectID, r.ConversationID)
	if policy.WorkspaceEnabled || policy.PersonalEnabled || !policy.ChannelEnabled || policy.Allows(chat.Direct) {
		t.Fatalf("stored %+v", policy)
	}
	other, _ := s.VoicePolicy(ctx, p.TenantID, "bob", "elsewhere")
	if other.WorkspaceEnabled || other.ChannelEnabled || !other.PersonalEnabled {
		t.Fatalf("another person and room %+v", other)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", VoiceScopeWorkspace, "x", true); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("malformed scope: %v", err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", "everyone", "", true); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown scope: %v", err)
	}
	if foreign, err := s.VoicePolicy(ctx, "tenant-b", p.SubjectID, r.ConversationID); err != nil || !foreign.WorkspaceEnabled {
		t.Fatalf("switches leaked across tenants: %+v %v", foreign, err)
	}
}

// The server's worker lists, reads and completes transcripts without being a
// member of the room, and nobody else can use that path.
func TestChatvoiceWorkerNeedsNoMembership(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	err := s.Store.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3`, p.TenantID, r.ConversationID, chat.VoiceWorkerSubject)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	wc, worker := chatvoiceWorker(t)
	pending, err := s.PendingVoiceTranscriptsAll(wc, worker, p.TenantID, 10)
	if err != nil || len(pending) != 1 || pending[0].ConversationID != r.ConversationID || pending[0].PostID != r.PostID {
		t.Fatalf("pending %+v err=%v", pending, err)
	}
	out, err := s.SaveVoiceTranscript(wc, worker, pending[0], 1, chatvoiceReady("worker result"))
	if err != nil || out.Transcript.Text() != "worker result" {
		t.Fatalf("save %+v err=%v", out, err)
	}
	if _, err = s.PendingVoiceTranscriptsAll(ctx, p, p.TenantID, 10); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a person listed the worker queue: %v", err)
	}
	if _, err = s.PendingVoiceTranscriptsAll(wc, worker, "tenant-b", 10); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross-tenant queue: %v", err)
	}
	if _, err = s.SaveVoiceTranscript(ctx, p, r.TranscriptionRequest, 2, chatvoiceReady("forged")); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a person saved an automatic result: %v", err)
	}
}

// A transcript is never returned for a post the reader cannot read.
func TestChatvoiceRecordsForPostsFollowVisibility(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	if _, err := s.RequestVoiceTranscript(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.VoiceRecordsForPosts(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "bob"}, p.TenantID, r.ConversationID, []string{r.PostID})
	if err != nil || got[r.PostID].Transcript.State != chat.TranscriptPending {
		t.Fatalf("member read %+v err=%v", got, err)
	}
	got, err = s.VoiceRecordsForPosts(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "mallory"}, p.TenantID, r.ConversationID, []string{r.PostID})
	if err != nil || len(got) != 0 {
		t.Fatalf("non-member read %+v err=%v", got, err)
	}
	if _, err = s.DeletePost(ctx, chat.DeletePostRequest{Principal: p, TenantID: p.TenantID, ConversationID: r.ConversationID, PostID: r.PostID, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	got, err = s.VoiceRecordsForPosts(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "bob"}, p.TenantID, r.ConversationID, []string{r.PostID})
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted message still yields a transcript: %+v err=%v", got, err)
	}
}

// Listen is barred where the channel says "Never use an outside service", and
// the answer names only conversations the person is in.
func TestChatvoiceListenBarred(t *testing.T) {
	s, p, r := chatvoiceDB(t)
	ctx := context.Background()
	workspace, rooms, err := s.Store.VoiceListenBarred(ctx, p.TenantID, p.TenantID, p.SubjectID)
	if err != nil || workspace || len(rooms) != 0 {
		t.Fatalf("nothing set: %v %v %v", workspace, rooms, err)
	}
	if err = s.Store.PutChatlangChannel(ctx, p.TenantID, r.ConversationID, "admin", chatlang.Channel{Translation: chatlang.Inherit, External: chatlang.ExternalBarred}); err != nil {
		t.Fatal(err)
	}
	if _, rooms, err = s.Store.VoiceListenBarred(ctx, p.TenantID, p.TenantID, p.SubjectID); err != nil || len(rooms) != 1 || rooms[0] != r.ConversationID {
		t.Fatalf("barred channel: %v %v", rooms, err)
	}
	if _, rooms, err = s.Store.VoiceListenBarred(ctx, p.TenantID, p.TenantID, "mallory"); err != nil || len(rooms) != 0 {
		t.Fatalf("a conversation the person is not in was named: %v %v", rooms, err)
	}
	if _, err = s.Store.PutChatlangWorkspace(ctx, p.TenantID, "admin", chatlang.Workspace{ExternalAllowed: false}); err != nil {
		t.Fatal(err)
	}
	if workspace, _, err = s.Store.VoiceListenBarred(ctx, p.TenantID, p.TenantID, p.SubjectID); err != nil || !workspace {
		t.Fatalf("workspace bar: %v %v", workspace, err)
	}
}

// The settings pages read the workspace and personal switches without a
// conversation, with the same defaults and the same tenant isolation.
func TestChatvoiceSwitchValues(t *testing.T) {
	s, p, _ := chatvoiceDB(t)
	ctx := context.Background()
	workspace, personal, err := s.VoiceSwitchValues(ctx, p.TenantID, p.SubjectID)
	if err != nil || !workspace || !personal {
		t.Fatalf("defaults: %v %v %v", workspace, personal, err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, p.SubjectID, VoiceScopePerson, p.SubjectID, false); err != nil {
		t.Fatal(err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", VoiceScopeChannel, "room", true); err != nil {
		t.Fatal(err)
	}
	if workspace, personal, err = s.VoiceSwitchValues(ctx, p.TenantID, p.SubjectID); err != nil || !workspace || personal {
		t.Fatalf("personal off: %v %v %v", workspace, personal, err)
	}
	if err = s.PutVoiceSwitch(ctx, p.TenantID, "admin", VoiceScopeWorkspace, "", false); err != nil {
		t.Fatal(err)
	}
	if workspace, personal, err = s.VoiceSwitchValues(ctx, p.TenantID, p.SubjectID); err != nil || workspace || personal {
		t.Fatalf("workspace off: %v %v %v", workspace, personal, err)
	}
	if workspace, personal, err = s.VoiceSwitchValues(ctx, p.TenantID, "bob"); err != nil || workspace || !personal {
		t.Fatalf("another person's own switch leaked: %v %v %v", workspace, personal, err)
	}
	if workspace, personal, err = s.VoiceSwitchValues(ctx, "tenant-b", p.SubjectID); err != nil || !workspace || !personal {
		t.Fatalf("switches leaked across tenants: %v %v %v", workspace, personal, err)
	}
	if _, _, err = s.VoiceSwitchValues(ctx, "", p.SubjectID); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no tenant: %v", err)
	}
}
