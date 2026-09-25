package projectrefs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatFixture struct {
	allowed      bool
	revokeOnRead bool
	reads        int
	room         chat.Conversation
	post         chat.Post
}

func (f *chatFixture) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, postID string) (chat.Conversation, *chat.Post, error) {
	f.reads++
	if !f.allowed || p.TenantID != tenant || tenant != f.room.TenantID || conversation != f.room.ID {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	if f.revokeOnRead && f.reads == 2 {
		f.allowed = false
	}
	if postID == "" {
		return f.room, nil, nil
	}
	if postID != f.post.ID || f.post.Deleted {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	post := f.post
	return f.room, &post, nil
}

type documentFixture struct {
	allowed            bool
	withdrawn          bool
	revokeOnTargetRead bool
	calls              int
	deployment         document.Deployment
}

func (f *documentFixture) GetDocumentPlacement(_ context.Context, _, _, id, scope string) (document.Deployment, error) {
	f.calls++
	if !f.allowed {
		return document.Deployment{}, envelope.New(envelope.CodePermissionDenied, "document.denied", "not available")
	}
	if f.withdrawn {
		return document.Deployment{}, envelope.New(envelope.CodeNotFound, "document.withdrawn", "not available")
	}
	if id != f.deployment.DocumentID || scope != f.deployment.ScopeID {
		return document.Deployment{}, envelope.New(envelope.CodeNotFound, "document.missing", "not available")
	}
	got := f.deployment
	if f.revokeOnTargetRead && f.calls == 2 {
		f.allowed = false // revocation immediately after the authorized target read
	}
	return got, nil
}

func projectrefsContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "viewer-a", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-a", IssuedAt: now, ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestAdapterRevokesChatPreviewAfterExactPostRead(t *testing.T) {
	ctx := projectrefsContext(t)
	chatService := &chatFixture{
		allowed: true, revokeOnRead: true,
		room: chat.Conversation{ID: "c-1", TenantID: "tenant-a", Name: "Team"},
		post: chat.Post{ID: "p-1", ConversationID: "c-1", TenantID: "tenant-a", Body: "private update"},
	}
	resolver := projectlink.Resolver{Authorization: Adapter{Chat: chatService}, Targets: Adapter{Chat: chatService}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.ChatPost, ID: "p-1", ConversationID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != projectlink.Restricted || got.Preview != nil {
		t.Fatalf("revoked chat post preview escaped: %#v", got)
	}
}

func TestAdapterResolvesCurrentlyAuthorizedChatPost(t *testing.T) {
	ctx := projectrefsContext(t)
	chatService := &chatFixture{
		allowed: true,
		room:    chat.Conversation{ID: "c-1", TenantID: "tenant-a", Name: "Team"},
		post:    chat.Post{ID: "p-1", ConversationID: "c-1", TenantID: "tenant-a", Body: "approved update"},
	}
	resolver := projectlink.Resolver{Authorization: Adapter{Chat: chatService}, Targets: Adapter{Chat: chatService}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.ChatPost, ID: "p-1", ConversationID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != projectlink.Available || got.Preview == nil || got.Preview.Snippet != "approved update" {
		t.Fatalf("authorized Chat post = %#v", got)
	}
	if chatService.reads != 3 {
		t.Fatalf("project preview did not recheck authorization around read: reads=%d", chatService.reads)
	}
}

func TestAdapterRestrictsInaccessiblePrivateConversationWithoutDisclosure(t *testing.T) {
	ctx := projectrefsContext(t)
	chatService := &chatFixture{room: chat.Conversation{ID: "private-1", TenantID: "tenant-a", Name: "Executive planning"}, post: chat.Post{ID: "p-secret", ConversationID: "private-1", TenantID: "tenant-a", Body: "secret"}}
	resolver := projectlink.Resolver{Authorization: Adapter{Chat: chatService}, Targets: Adapter{Chat: chatService}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.ChatPost, ID: "p-secret", ConversationID: "private-1"})
	if err != nil || got.State != projectlink.Restricted || got.Preview != nil {
		t.Fatalf("inaccessible private post result=%#v err=%v", got, err)
	}
}

func TestAdapterUsesTrustedTenantForChatReference(t *testing.T) {
	ctx := projectrefsContext(t)
	chatService := &chatFixture{allowed: true, room: chat.Conversation{ID: "same-id", TenantID: "tenant-b", Name: "Other tenant"}}
	resolver := projectlink.Resolver{Authorization: Adapter{Chat: chatService}, Targets: Adapter{Chat: chatService}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.ChatConversation, ID: "same-id"})
	if err != nil || got.State != projectlink.Restricted || got.Preview != nil {
		t.Fatalf("cross-tenant conversation result=%#v err=%v", got, err)
	}
}

func TestAdapterRevokesDocumentPreviewAfterPlacementRead(t *testing.T) {
	ctx := projectrefsContext(t)
	documents := &documentFixture{allowed: true, revokeOnTargetRead: true, deployment: document.Deployment{
		DocumentID: "d-1", VersionID: "v-3", ScopeKind: "placement", ScopeID: "team-1", Official: true,
	}}
	resolver := projectlink.Resolver{Authorization: Adapter{Documents: documents}, Targets: Adapter{Documents: documents}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "d-1", Version: "v-3", ScopeID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != projectlink.Restricted || got.Preview != nil || documents.calls != 3 {
		t.Fatalf("revoked document preview escaped: result=%#v placement checks=%d", got, documents.calls)
	}
}

func TestAdapterResolvesCurrentlyAuthorizedDeployedDocument(t *testing.T) {
	ctx := projectrefsContext(t)
	documents := &documentFixture{allowed: true, deployment: document.Deployment{
		DocumentID: "d-1", VersionID: "v-3", ScopeKind: "placement", ScopeID: "team-1", Official: true,
	}}
	resolver := projectlink.Resolver{Authorization: Adapter{Documents: documents}, Targets: Adapter{Documents: documents}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "d-1", Version: "v-3", ScopeID: "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != projectlink.Available || got.Preview == nil || got.Preview.Version != "v-3" || got.Preview.ScopeID != "team-1" {
		t.Fatalf("authorized deployed document = %#v", got)
	}
}

func TestAdapterRestrictsWithdrawnDocumentPlacement(t *testing.T) {
	ctx := projectrefsContext(t)
	documents := &documentFixture{withdrawn: true, deployment: document.Deployment{DocumentID: "d-1", VersionID: "v-3", ScopeKind: "placement", ScopeID: "team-1", Official: true}}
	resolver := projectlink.Resolver{Authorization: Adapter{Documents: documents}, Targets: Adapter{Documents: documents}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "d-1", Version: "v-3", ScopeID: "team-1"})
	if err != nil || got.State != projectlink.Restricted || got.Preview != nil {
		t.Fatalf("withdrawn placement result=%#v err=%v", got, err)
	}
}

func TestAdapterDoesNotReadHCMWorkItemsWithoutSafeProjection(t *testing.T) {
	adapter := Adapter{}
	allowed, err := adapter.Authorize(projectrefsContext(t), "viewer-a", projectlink.Reference{Kind: projectlink.WorkItem, ID: "work-1"})
	if err != nil || allowed {
		t.Fatalf("uncomposed WorkItem authority = (%v, %v), want denial", allowed, err)
	}
	if _, found, err := adapter.ReadAuthorized(projectrefsContext(t), projectlink.Reference{Kind: projectlink.WorkItem, ID: "work-1"}); err != nil || found {
		t.Fatalf("uncomposed WorkItem read = (found %v, err %v), want no target", found, err)
	}
}

type workOrderProjectionFixture struct {
	preview  projectlink.Preview
	found    bool
	sequence []bool
	seen     []*trust.Principal
}

func (f *workOrderProjectionFixture) ReadAuthorizedWorkOrder(_ context.Context, principal *trust.Principal, _ string) (projectlink.Preview, bool, error) {
	f.seen = append(f.seen, principal)
	found := f.found
	if len(f.sequence) >= len(f.seen) {
		found = f.sequence[len(f.seen)-1]
	}
	return f.preview, found, nil
}

func TestAdapterResolvesWorkOrderThroughOwningAuthorization(t *testing.T) {
	ctx := projectrefsContext(t)
	projection := &workOrderProjectionFixture{
		preview: projectlink.Preview{Kind: projectlink.WorkOrder, ID: "wo-1"}, found: true,
	}
	resolver := projectlink.Resolver{Authorization: Adapter{WorkOrders: projection}, Targets: Adapter{WorkOrders: projection}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.WorkOrder, ID: "wo-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != projectlink.Available || got.Preview == nil || *got.Preview != projection.preview {
		t.Fatalf("authorized work order preview = %#v", got)
	}
	if len(projection.seen) != 3 || projection.seen[0] == nil || projection.seen[0].Tenant().String() != "tenant-a" {
		t.Fatalf("owning authorization was not checked around the target read: %#v", projection.seen)
	}
}

func TestAdapterKeepsDeniedWorkOrderRestricted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		found bool
	}{
		{name: "denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projection := &workOrderProjectionFixture{found: tc.found}
			resolver := projectlink.Resolver{Authorization: Adapter{WorkOrders: projection}, Targets: Adapter{WorkOrders: projection}}
			got, err := resolver.Resolve(projectrefsContext(t), "viewer-a", projectlink.Reference{Kind: projectlink.WorkOrder, ID: "wo-1"})
			if err != nil || got.State != projectlink.Restricted || got.Preview != nil {
				t.Fatalf("work order resolution=%#v err=%v", got, err)
			}
		})
	}
}

func TestAdapterDiscardsWorkOrderPreviewAfterAuthorizationRevoked(t *testing.T) {
	ctx := projectrefsContext(t)
	projection := &workOrderProjectionFixture{
		preview:  projectlink.Preview{Kind: projectlink.WorkOrder, ID: "wo-1"},
		sequence: []bool{true, true, false},
	}
	resolver := projectlink.Resolver{Authorization: Adapter{WorkOrders: projection}, Targets: Adapter{WorkOrders: projection}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.WorkOrder, ID: "wo-1"})
	if err != nil || got.State != projectlink.Restricted || got.Preview != nil || len(projection.seen) != 3 {
		t.Fatalf("work order preview survived revoked authorization: result=%#v reads=%d err=%v", got, len(projection.seen), err)
	}
}

type workItemSourceFixture struct {
	item       workitem.WorkItem
	err        error
	calls      int
	revokeRead int
	afterRead  func()
}

func (f *workItemSourceFixture) ReadWorkItem(_ context.Context, tenant values.TenantId, id uuid.UUID) (workitem.WorkItem, error) {
	f.calls++
	if f.err != nil {
		return workitem.WorkItem{}, f.err
	}
	if tenant.String() != "tenant-a" || id != f.item.WorkItemID {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound}
	}
	item := f.item
	if f.afterRead != nil && f.calls == f.revokeRead {
		f.afterRead()
	}
	return item, nil
}

func workItemProjectionFixture(t *testing.T, item workitem.WorkItem, source *workItemSourceFixture, authorize func(context.Context, *trust.Principal, string) bool) WorkItemProjector {
	t.Helper()
	tenantID := workItemFixtureTenant
	return WorkItemProjector{
		Source: source, Authorize: authorize,
		TenantID: func(tenant values.TenantId) uuid.UUID {
			if tenant.String() == "tenant-a" {
				return tenantID
			}
			return uuid.Nil
		},
		Now: func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) },
	}
}

func workItemFixture() workitem.WorkItem {
	return workitem.WorkItem{
		TenantID: workItemFixtureTenant, WorkItemID: uuid.New(), ItemVersion: 7,
		Status: workitem.StatusAssigned, OwnerKind: workitem.OwnerPrincipal, OwnerRef: "viewer-a",
		Visibility: workitem.VisibilityAssigneeOnly,
	}
}

var workItemFixtureTenant = uuid.MustParse("aaaaaaa1-aaaa-4aaa-8aaa-aaaaaaaaaaa1")

func TestAdapterProjectsOnlyCurrentlyAuthorizedWorkItemFields(t *testing.T) {
	ctx := projectrefsContext(t)
	item := workItemFixture()
	source := &workItemSourceFixture{item: item}
	var actions []string
	authorize := func(_ context.Context, principal *trust.Principal, action string) bool {
		if principal == nil || principal.Subject() != "viewer-a" || principal.Tenant().String() != "tenant-a" {
			t.Fatalf("unexpected principal in WorkItem authorization: %#v", principal)
		}
		actions = append(actions, action)
		return true
	}
	projector := workItemProjectionFixture(t, item, source, authorize)
	resolver := projectlink.Resolver{Authorization: Adapter{WorkItems: projector}, Targets: Adapter{WorkItems: projector}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.WorkItem, ID: item.WorkItemID.String()})
	if err != nil {
		t.Fatal(err)
	}
	want := &projectlink.Preview{Kind: projectlink.WorkItem, ID: item.WorkItemID.String(), Version: "7", Status: string(workitem.StatusAssigned), Freshness: "CURRENT"}
	if got.State != projectlink.Available || got.Preview == nil || *got.Preview != *want {
		t.Fatalf("work item preview = %#v, want only %#v", got, want)
	}
	if len(actions) != 6 || actions[0] != "get_work_item" || actions[1] != "work_item_governance_view" || actions[2] != "get_work_item" || actions[3] != "work_item_governance_view" || actions[4] != "get_work_item" || actions[5] != "work_item_governance_view" {
		t.Fatalf("authorization calls = %v, want capability and governance checks around the target read", actions)
	}
}

func TestAdapterRevokesWorkItemPreviewAfterTargetRead(t *testing.T) {
	ctx := projectrefsContext(t)
	item := workItemFixture()
	source := &workItemSourceFixture{item: item, revokeRead: 2}
	allowed := true
	source.afterRead = func() { allowed = false }
	authorize := func(_ context.Context, _ *trust.Principal, action string) bool {
		return allowed && action == "get_work_item"
	}
	projector := workItemProjectionFixture(t, item, source, authorize)
	resolver := projectlink.Resolver{Authorization: Adapter{WorkItems: projector}, Targets: Adapter{WorkItems: projector}}
	got, err := resolver.Resolve(ctx, "viewer-a", projectlink.Reference{Kind: projectlink.WorkItem, ID: item.WorkItemID.String()})
	if err != nil || got.State != projectlink.Restricted || got.Preview != nil {
		t.Fatalf("revoked work item preview escaped: result=%#v err=%v", got, err)
	}
}

func TestAdapterRestrictsWorkItemWithoutCurrentReadCapabilityOrVisibility(t *testing.T) {
	ctx := projectrefsContext(t)
	for _, tc := range []struct {
		name      string
		configure func(*workitem.WorkItem, *bool)
	}{
		{name: "revoked capability", configure: func(_ *workitem.WorkItem, allowed *bool) { *allowed = false }},
		{name: "not a member", configure: func(item *workitem.WorkItem, _ *bool) { item.OwnerRef = "someone-else" }},
		{name: "tenant mismatch", configure: func(item *workitem.WorkItem, _ *bool) { item.TenantID = uuid.New() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := workItemFixture()
			allowed := true
			tc.configure(&item, &allowed)
			source := &workItemSourceFixture{item: item}
			authorize := func(_ context.Context, _ *trust.Principal, action string) bool {
				return allowed && action == "get_work_item"
			}
			projector := workItemProjectionFixture(t, item, source, authorize)
			_, found, err := (Adapter{WorkItems: projector}).ReadAuthorized(ctx, projectlink.Reference{Kind: projectlink.WorkItem, ID: item.WorkItemID.String()})
			if err != nil || found {
				t.Fatalf("unauthorized WorkItem found=%v err=%v", found, err)
			}
		})
	}
}

func TestWorkItemProjectorPropagatesUnexpectedReadErrors(t *testing.T) {
	item := workItemFixture()
	source := &workItemSourceFixture{item: item, err: errors.New("database unavailable")}
	projector := workItemProjectionFixture(t, item, source, func(context.Context, *trust.Principal, string) bool { return true })
	if _, found, err := projector.ReadAuthorizedWorkItem(projectrefsContext(t), trustedPrincipal(projectrefsContext(t)), item.WorkItemID.String()); err == nil || found {
		t.Fatalf("unexpected read failure = (found %v, err %v), want propagated error", found, err)
	}
}
