package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"strings"
	"testing"
	"time"
)

func proactiveLive2Leases(t *testing.T, r *AgentAnnouncementRuntime) {
	t.Helper()
	ctx := context.Background()
	directory := chatrouting.NewMemoryDirectory()
	if _, err := directory.Reserve(ctx, chatrouting.ReserveRequest{ConversationID: "general", HostTenantID: "tenant-a", ShardID: "test-shard", IdempotencyKey: "fixture-route"}); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Activate(ctx, "general", "tenant-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET route_shard='test-shard',route_epoch=1,route_state='ACTIVE' WHERE tenant_id=$1 AND id=$2`, "tenant-a", "general")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r.Routes, r.RouteCache = directory, chatrouting.NewRouteCache(5*time.Second)
}

func TestAgentUXProactiveLive_PostedMessage(t *testing.T) {
	surface, _, _, owner, model, delivery, draft := proactiveControls(t)
	ctx := context.Background()
	preview, err := surface.PreviewAnnouncement(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.Cadence, draft.Weekdays = "NOW", nil
	draft.PreviewDigest = preview.Preview.Digest
	// A provider that changes every answer must still post the saved preview.
	if _, err := surface.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || delivery.calls != 1 {
		t.Fatalf("preview replay invoked provider: %d/%d", model.calls, delivery.calls)
	}
	actor := owner.actor
	prepared, _ := transportAnnouncementDraft(actor, draft, surface.Now())
	prepared.ID = announcementID(actor, draft.IdempotencyKey)
	foreign := actor
	foreign.SubjectID = "another-owner"
	if _, ok := surface.withSavedPreview(ctx, foreign, prepared, draft.PreviewDigest).Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok {
		t.Fatal("preview crossed owner")
	}
	alternate := prepared
	alternate.ID = "another-definition"
	if _, ok := surface.withSavedPreview(ctx, actor, alternate, draft.PreviewDigest).Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok {
		t.Fatal("preview crossed definition ID")
	}
	prepared.Instruction = "Different instruction"
	if _, ok := surface.withSavedPreview(ctx, actor, prepared, draft.PreviewDigest).Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok {
		t.Fatal("preview crossed definition")
	}
	surface.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 11, 0, 0, time.UTC) }
	prepared.Instruction = draft.Instruction
	if _, ok := surface.withSavedPreview(ctx, actor, prepared, draft.PreviewDigest).Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok {
		t.Fatal("expired preview accepted")
	}
	// Eviction bounds memory even when an owner requests many distinct previews.
	prepared.Instruction = draft.Instruction
	saved := surface.previews[draft.PreviewDigest].Result
	for index := range announcementPreviewLimit + 20 {
		prepared.Instruction = fmt.Sprintf("Upcoming holidays %d", index)
		surface.savePreview(actor, prepared, saved, true)
	}
	if len(surface.previews) != announcementPreviewLimit {
		t.Fatalf("unbounded preview cache: %d", len(surface.previews))
	}
	surface.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 22, 0, 0, time.UTC) }
	surface.savePreview(actor, prepared, saved, true)
	if len(surface.previews) != 1 {
		t.Fatalf("expired cache retained: %d", len(surface.previews))
	}
	if announcementReplyRule("Labor Day — Sep 7", "2026-10-01", "holidays coming up in the rest of 2026") == "" {
		t.Fatal("exact observed past-date regression accepted")
	}
	if announcementReplyRule("Thanksgiving — Nov 26\nChristmas — Dec 25", "2026-10-01", "holidays coming up in the rest of 2026") != "" {
		t.Fatal("future dates refused")
	}
	if announcementReplyRule("Labor Day was Sep 7", "2026-10-01", "Describe holidays earlier this year") != "" {
		t.Fatal("historical dates refused")
	}
}

// The real guide contains Labor Day as well as all three remaining holidays.
// Both the date guard and the served repair path see that exact document.
func TestAgentUXProactiveLive_UpcomingHolidays_Evaluation_Integration(t *testing.T) {
	surface, worker, store, model, _, draft := proactiveLiveFixture(t)
	model.texts = []string{"Company holidays coming up:\n- Labor Day — Sep 7\n- Thanksgiving Day — Nov 26\n- Day after Thanksgiving — Nov 27\n- Christmas Day — Dec 25", "Company holidays coming up:\n- Thanksgiving Day — Nov 26\n- Day after Thanksgiving — Nov 27\n- Christmas Day — Dec 25"}
	reply, err := surface.CreateAnnouncement(context.Background(), draft)
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted || model.calls != 2 {
		t.Fatalf("holiday repair %+v calls=%d %v", reply, model.calls, err)
	}
	requests := model.requests
	if !strings.Contains(fmt.Sprint(requests[0].Model.Messages), "Labor Day | Sep 7 | Monday") || !strings.Contains(fmt.Sprint(requests[1].Model.Messages), "upcoming items must have dates on or after 2026-10-01") {
		t.Fatalf("real document/repair rule missing: %+v", requests)
	}
	posts, err := store.ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 1 {
		t.Fatalf("posts %+v %v", posts, err)
	}
	announcement, ok := chatui.DecodeAnnouncementMessageBody(posts.Posts[0].Body)
	if !ok || strings.Contains(announcement.Text, "Labor Day") || strings.Contains(announcement.Text, "Sep 7") || !strings.Contains(announcement.Text, "Nov 26") || !strings.Contains(announcement.Text, "Nov 27") || !strings.Contains(announcement.Text, "Dec 25") {
		t.Fatalf("upcoming holiday evaluation posted incorrect dates: %+v", announcement)
	}
	// Output is sealed and persisted; a past date never reaches delivery.
	record, err := worker.Runtime.Store.Get(context.Background(), worker.Runtime.Work.tenantUUID("tenant-a"), reply.Snapshot.Rows[0].ID)
	if err != nil || record.LastMessageID != posts.Posts[0].ID {
		t.Fatalf("receipt missing %+v %v", record, err)
	}
}

func TestAgentUXProactiveLive_PreviewChanged_Security_Integration(t *testing.T) {
	for _, change := range []string{"document", "version", "audience", "authority", "expired"} {
		t.Run(change, func(t *testing.T) {
			surface, worker, store, model, now, draft := proactiveLiveFixture(t)
			ctx := context.Background()
			preview, err := surface.PreviewAnnouncement(ctx, draft)
			if err != nil {
				t.Fatal(err)
			}
			draft.PreviewDigest = preview.Preview.Digest
			switch change {
			case "document":
				hub := worker.Runtime.DocumentAuthority.Store
				_, err = hub.ShareDocument(ctx, "tenant-a", draft.Documents[0].DocumentID, "owner", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "employee", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny})
			case "version":
				hub := worker.Runtime.DocumentAuthority.Store
				id := draft.Documents[0].DocumentID
				previous, loadErr := hub.CurrentPlacement(ctx, "tenant-a", id, "placement", "general")
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				version, submitErr := hub.SubmitCandidate(ctx, "tenant-a", documenthubstore.Version{DocumentID: id, CreatorID: "owner", Title: "2026 holiday guide", Markdown: localAgentDemoHolidayMarkdown + "\nUpdated holiday guidance.", Classification: "INTERNAL"}, previous.VersionID)
				if submitErr != nil {
					t.Fatal(submitErr)
				}
				if _, err = hub.RecordReview(ctx, "tenant-a", documenthubstore.ReviewInput{DocumentID: id, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "employee", Authority: "policy-owner", Decision: documenthubstore.ReviewApproved}); err == nil {
					_, err = hub.PlaceDocument(ctx, "tenant-a", documenthubstore.PlaceInput{DocumentID: id, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", CustodianID: "owner", ExpectedLive: previous.VersionID, ReviewDueAt: now.AddDate(1, 0, 0)})
				}
			case "audience":
				err = worker.Runtime.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
					_, err := tx.Exec(ctx, `UPDATE chat_conversation SET audience_revision=audience_revision+1 WHERE tenant_id=$1 AND id=$2`, "tenant-a", "general")
					return err
				})
			case "authority":
				surface.Actors.(*proactiveOwner).denied = true
			case "expired":
				*now = now.Add(announcementPreviewLifetime + time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			reply, err := surface.CreateAnnouncement(ctx, draft)
			if err == nil && (len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode == agentstore.AnnouncementPosted) {
				t.Fatalf("changed %s posted: %+v", change, reply)
			}
			if model.calls != 1 {
				t.Fatalf("changed preview called provider %d times", model.calls)
			}
			posts, readErr := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
			if readErr != nil || len(posts.Posts) != 0 {
				t.Fatalf("changed preview leaked %+v %v", posts, readErr)
			}
		})
	}
}

type proactiveLive2CommittedFailure struct{ *AgentAnnouncementRuntime }

func (d proactiveLive2CommittedFailure) PostAnnouncement(ctx context.Context, request AgentAnnouncementRunRequest, result AgentAnnouncementRunResult, key string) (string, error) {
	message, err := d.AgentAnnouncementRuntime.PostAnnouncement(ctx, request, result, key)
	if err != nil {
		return message, err
	}
	return message, errors.New("fixture crash after committed message before occurrence record")
}

func TestAgentUXProactiveLive_PostedMessage_Fault(t *testing.T) {
	surface, worker, store, model, _, draft := proactiveLiveFixture(t)
	proactiveLive2Leases(t, worker.Runtime)
	surface.Runner.Delivery = proactiveLive2CommittedFailure{worker.Runtime}
	first, err := surface.CreateAnnouncement(context.Background(), draft)
	if err != nil || first.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementFailed {
		t.Fatalf("crash projection %+v %v", first, err)
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, false)
	row := first.Snapshot.Rows[0]
	for _, key := range []string{"retry-after-crash-1", "retry-after-crash-2"} {
		reply, err := surface.ControlAnnouncement(context.Background(), agentcontrols.AnnouncementCommand{ID: row.ID, Action: "POST_NOW", ExpectedRevision: row.Revision, IdempotencyKey: key, RetryOccurrence: announcementManualOccurrence(row.ID, draft.IdempotencyKey)})
		if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted || model.calls != 1 {
			t.Fatalf("retry duplicated committed write %+v calls=%d %v", reply, model.calls, err)
		}
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, false)
	attempts, err := worker.Runtime.AnnouncementAttempts(context.Background(), worker.Runtime.Work.tenantUUID("tenant-a"), row.ID)
	if err != nil || len(attempts) != 2 || attempts[0].MessageID == "" && attempts[1].MessageID == "" {
		t.Fatalf("history %+v %v", attempts, err)
	}
}

func TestAgentUXProactiveLive_ExistingPreview_Integration(t *testing.T) {
	surface, worker, store, model, _, draft := proactiveLiveFixture(t)
	proactiveLive2Leases(t, worker.Runtime)
	ctx := context.Background()
	initial, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	row := initial.Snapshot.Rows[0]
	draft.ID, draft.ExpectedRevision, draft.IdempotencyKey = row.ID, row.Revision, "existing-preview-123"
	model.texts = []string{"already posted", "Remaining holidays:\n- Thanksgiving Day \u2014 Nov 26\n- Christmas Day \u2014 Dec 25", "a different unpreviewed answer"}
	preview, err := surface.PreviewAnnouncement(ctx, draft)
	if err != nil || preview.Preview == nil {
		t.Fatalf("existing preview %+v %v", preview, err)
	}
	reply, err := surface.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, ExpectedRevision: row.Revision, Action: "POST_NOW", IdempotencyKey: "post-existing-preview-123", PreviewDigest: preview.Preview.Digest})
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted || model.calls != 2 {
		t.Fatalf("existing preview became a new definition or called model again: %+v calls=%d %v", reply, model.calls, err)
	}
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 2 {
		t.Fatalf("existing posts %+v %v", posts, err)
	}
	var found bool
	for _, post := range posts.Posts {
		body, ok := chatui.DecodeAnnouncementMessageBody(post.Body)
		found = found || ok && body.Text == preview.Preview.Text
	}
	if !found || len(reply.Snapshot.Rows[0].Attempts) != 2 {
		t.Fatalf("existing preview/history lost: %+v", reply.Snapshot.Rows)
	}
}
