package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatroutingadapter"
)

type auditConversation struct {
	chatcore.ConversationService
	fail bool
}
type denyPreflight struct{ chatcore.ConversationService }

func (denyPreflight) ValidateCreate(context.Context, chatcore.CreateConversationRequest) error {
	return chatcore.ErrPermissionDenied
}

type countRoutes struct {
	chatrouting.Directory
	reserved int
}

func (d *countRoutes) Reserve(ctx context.Context, r chatrouting.ReserveRequest) (chatrouting.Route, error) {
	d.reserved++
	return d.Directory.Reserve(ctx, r)
}

func (a auditConversation) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, OwnerID: r.Principal.SubjectID, Revision: 1}, nil
}
func (a auditConversation) SendPost(_ context.Context, r chatcore.SendPostRequest) (chatcore.Post, error) {
	if a.fail {
		return chatcore.Post{}, chatcore.ErrPermissionDenied
	}
	return chatcore.Post{ID: "post", TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.Principal.SubjectID, Revision: 1}, nil
}
func (a auditConversation) ReadAuthorizedReference(_ context.Context, p chatcore.Principal, tenant, conversation, postID string) (chatcore.Conversation, *chatcore.Post, error) {
	room := chatcore.Conversation{ID: conversation, TenantID: tenant, OwnerID: p.SubjectID, Revision: 1}
	if postID == "" {
		return room, nil, nil
	}
	return room, &chatcore.Post{ID: postID, ConversationID: conversation, TenantID: tenant, AuthorID: p.SubjectID, Body: "reference preview", Revision: 1}, nil
}

func TestTodo_CHAT_047_AuditPostAndReplay(t *testing.T) {
	repo := chatrecords.NewMemoryRepository()
	s := &auditedChatService{ConversationService: auditConversation{}, records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	r := chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "owner"}, TenantID: "tenant", ConversationID: "conv", IdempotencyKey: "once"}
	if _, err := s.SendPost(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendPost(context.Background(), r); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	rows, err := repo.List(context.Background(), "tenant")
	if err != nil || len(rows) != 1 || rows[0].RecordID != "post:post" || rows[0].Kind != chatrecords.KindPost {
		t.Fatalf("audit inventory: %#v %v", rows, err)
	}
}
func TestTodo_CHAT_047_FailedMutationHasNoAudit(t *testing.T) {
	repo := chatrecords.NewMemoryRepository()
	s := &auditedChatService{ConversationService: auditConversation{fail: true}, records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	r := chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "owner"}, TenantID: "tenant", ConversationID: "conv"}
	if _, err := s.SendPost(context.Background(), r); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("wrong outcome: %v", err)
	}
	rows, _ := repo.List(context.Background(), "tenant")
	if len(rows) != 0 {
		t.Fatalf("audited failed mutation: %#v", rows)
	}
}

func TestProjectReferenceReadDoesNotCreateShareAudit(t *testing.T) {
	repo := chatrecords.NewMemoryRepository()
	s := &auditedChatService{ConversationService: auditConversation{}, records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	room, post, err := s.ReadAuthorizedReference(context.Background(), chatcore.Principal{TenantID: "tenant", SubjectID: "viewer"}, "tenant", "conv", "post")
	if err != nil || room.ID != "conv" || post == nil || post.ID != "post" {
		t.Fatalf("authorized reference read: room=%+v post=%+v err=%v", room, post, err)
	}
	rows, err := repo.List(context.Background(), "tenant")
	if err != nil || len(rows) != 0 {
		t.Fatalf("read created Chat audit records: %#v err=%v", rows, err)
	}
	if pending, dropped := s.PendingAudits(); pending != 0 || dropped != 0 {
		t.Fatalf("read queued Chat audit: pending=%d dropped=%d", pending, dropped)
	}
}

// TestTodo_CHAT_047_AuditFailureIsQueuedAfterCommit pins the ordering this
// decorator actually has. The audit append runs after the durable commit, so a
// failure there cannot be reported as a failed send: the post exists. The
// committed post is returned, the append is queued, and RetryAudits drains it
// once the record store recovers.
func TestTodo_CHAT_047_AuditFailureIsQueuedAfterCommit(t *testing.T) {
	repo := &faultyAuditRepo{Repository: chatrecords.NewMemoryRepository(), fail: true}
	s := &auditedChatService{ConversationService: auditConversation{}, records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	r := chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "owner"}, TenantID: "tenant", ConversationID: "conv"}
	post, err := s.SendPost(context.Background(), r)
	if err != nil || post.ID != "post" {
		t.Fatalf("committed send reported as failed: post=%+v err=%v", post, err)
	}
	if pending, dropped := s.PendingAudits(); pending != 1 || dropped != 0 {
		t.Fatalf("pending=%d dropped=%d, want one queued retry", pending, dropped)
	}
	if appended, err := s.RetryAudits(context.Background()); appended != 0 || err == nil {
		t.Fatalf("retry against a still-broken store: appended=%d err=%v", appended, err)
	}
	if pending, _ := s.PendingAudits(); pending != 1 {
		t.Fatalf("failed retry lost the queued record: pending=%d", pending)
	}
	repo.fail = false
	appended, err := s.RetryAudits(context.Background())
	if appended != 1 || err != nil {
		t.Fatalf("retry after recovery: appended=%d err=%v", appended, err)
	}
	if pending, dropped := s.PendingAudits(); pending != 0 || dropped != 0 {
		t.Fatalf("queue after drain: pending=%d dropped=%d", pending, dropped)
	}
	rows, listErr := repo.List(context.Background(), "tenant")
	if listErr != nil || len(rows) != 1 || rows[0].RecordID != "post:post" {
		t.Fatalf("retried audit inventory: %#v %v", rows, listErr)
	}
}

// TestTodo_CHAT_047_AuditConflictUsesKeyedLookup proves an idempotent replay is
// settled with a keyed read of the one record, not a tenant-wide listing.
func TestTodo_CHAT_047_AuditConflictUsesKeyedLookup(t *testing.T) {
	repo := &keyedAuditRepo{Repository: chatrecords.NewMemoryRepository()}
	s := &auditedChatService{ConversationService: auditConversation{}, records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	r := chatcore.SendPostRequest{Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "owner"}, TenantID: "tenant", ConversationID: "conv", IdempotencyKey: "once"}
	if _, err := s.SendPost(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendPost(context.Background(), r); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if pending, _ := s.PendingAudits(); pending != 0 {
		t.Fatalf("idempotent replay queued a retry: pending=%d", pending)
	}
	if repo.keyed != 1 {
		t.Fatalf("keyed lookups=%d, want one", repo.keyed)
	}
	if repo.listed != 0 {
		t.Fatalf("tenant listing used to settle a conflict: %d", repo.listed)
	}
	// A different record under the same id is a real conflict, not a replay.
	repo.collide = true
	if _, err := s.SendPost(context.Background(), r); err != nil {
		t.Fatalf("collision must still commit the post: %v", err)
	}
	if pending, _ := s.PendingAudits(); pending != 1 {
		t.Fatalf("unresolved conflict was not queued: pending=%d", pending)
	}
}

type faultyAuditRepo struct {
	chatrecords.Repository
	fail bool
}

func (r *faultyAuditRepo) Append(ctx context.Context, rec chatrecords.Record, e chatrecords.AuditEvent, o chatrecords.OutboxEvent) error {
	if r.fail {
		return errors.New("record store unavailable")
	}
	return r.Repository.Append(ctx, rec, e, o)
}

type keyedAuditRepo struct {
	chatrecords.Repository
	keyed, listed int
	collide       bool
}

func (r *keyedAuditRepo) Append(ctx context.Context, rec chatrecords.Record, e chatrecords.AuditEvent, o chatrecords.OutboxEvent) error {
	if err := r.Repository.Append(ctx, rec, e, o); err != nil {
		return err
	}
	return nil
}

func (r *keyedAuditRepo) Record(_ context.Context, tenantID, recordID string) (chatrecords.Record, bool, error) {
	r.keyed++
	if r.collide {
		return chatrecords.Record{TenantID: tenantID, RecordID: recordID, Kind: chatrecords.KindDerived, SourceID: "other", Revision: 99}, true, nil
	}
	return chatrecords.Record{TenantID: tenantID, RecordID: recordID, Kind: chatrecords.KindPost, SourceID: "post", Revision: 1}, true, nil
}

func (r *keyedAuditRepo) List(ctx context.Context, tenantID string) ([]chatrecords.Record, error) {
	r.listed++
	return r.Repository.List(ctx, tenantID)
}
func TestTodo_CHAT_047_PreflightRejectsBeforeRouteReservation(t *testing.T) {
	d := &countRoutes{Directory: chatrouting.NewMemoryDirectory()}
	routed, e := chatroutingadapter.New(&auditedChatService{ConversationService: denyPreflight{}}, chatroutingadapter.Options{Directory: d, DefaultShard: "s1"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = routed.CreateConversation(context.Background(), chatcore.CreateConversationRequest{TenantID: "tenant", Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "invalid"}, Kind: chatcore.PublicChannel, Name: "general"})
	if !errors.Is(e, chatcore.ErrPermissionDenied) || d.reserved != 0 {
		t.Fatalf("route reserved after failed preflight: %v, %d", e, d.reserved)
	}
}
