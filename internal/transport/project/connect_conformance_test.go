package project_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	transportproject "github.com/monstercameron/human-capital-management-suite/internal/transport/project"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type conformanceService struct {
	projectservice.Service
	principal *trust.Principal
	key       string
}

func (s *conformanceService) GetProject(_ context.Context, principal *trust.Principal, id string) (projectservice.ProjectRecord, error) {
	s.principal = principal
	return projectservice.ProjectRecord{ID: id, TenantID: transporttest.Tenant, Name: "Parity", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 9, WorkflowRevision: 4}, nil
}

func (s *conformanceService) CreateProject(_ context.Context, principal *trust.Principal, req projectservice.CreateProjectRequest) (projectservice.ProjectRecord, error) {
	s.principal, s.key = principal, req.IdempotencyKey
	return projectservice.ProjectRecord{ID: "created-project", TenantID: transporttest.Tenant, OwnerID: transporttest.Subject, Name: req.Name, Timezone: req.Timezone, State: domainproject.LifecycleActive, Revision: 1, WorkflowRevision: 1}, nil
}

func (s *conformanceService) UpdateProjectSettings(_ context.Context, principal *trust.Principal, req projectservice.UpdateProjectSettingsRequest) (projectservice.ProjectRecord, error) {
	s.principal, s.key = principal, req.IdempotencyKey
	return projectservice.ProjectRecord{ID: req.ProjectID, TenantID: transporttest.Tenant, OwnerID: transporttest.Subject, Name: req.Name, Timezone: req.Timezone, State: domainproject.LifecycleActive, Revision: req.ExpectedProjectRevision + 1}, nil
}

func (s *conformanceService) ArchiveProject(_ context.Context, principal *trust.Principal, req projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error) {
	s.principal, s.key = principal, req.IdempotencyKey
	return projectservice.ProjectRecord{ID: req.ProjectID, TenantID: transporttest.Tenant, OwnerID: transporttest.Subject, State: domainproject.LifecycleArchived, Revision: req.ExpectedProjectRevision + 1}, nil
}

func (s *conformanceService) RestoreProject(_ context.Context, principal *trust.Principal, req projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error) {
	s.principal, s.key = principal, req.IdempotencyKey
	return projectservice.ProjectRecord{ID: req.ProjectID, TenantID: transporttest.Tenant, OwnerID: transporttest.Subject, State: domainproject.LifecycleActive, Revision: req.ExpectedProjectRevision + 1}, nil
}

// pm027Serve runs the production Connect edge over HTTP with a stub
// application service and returns the server plus a valid bearer token.
func pm027Serve(t *testing.T, deps transportproject.Dependencies) (*httptest.Server, string) {
	t.Helper()
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "pm027-request", nil)
	h, err := edge.NewHandler(edge.Options{Config: cfg, Project: &deps})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, token
}

type pm027PagingService struct {
	projectservice.Service
	cursor string
	limit  int
	calls  int
}

func (s *pm027PagingService) ListProjects(_ context.Context, _ *trust.Principal, cursor string, limit int) (projectservice.ProjectListPage, error) {
	s.cursor, s.limit, s.calls = cursor, limit, s.calls+1
	return projectservice.ProjectListPage{
		Projects: []projectservice.ProjectRecord{
			{ID: "p1", TenantID: transporttest.Tenant, Name: "One", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 2},
			{ID: "p2", TenantID: transporttest.Tenant, Name: "Two", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 3},
		},
		Next: &projectservice.IDCursor{AfterID: "p2"},
	}, nil
}

// TestTodo_PM_027 covers the HTTP projection of paged reads: items,
// cursors, forwarded limits, defaults, and page-size bounds.
func TestTodo_PM_027(t *testing.T) {
	service := &pm027PagingService{}
	srv, token := pm027Serve(t, transportproject.Dependencies{Service: service})
	list := connect.NewClient[projectv1.ListProjectsRequest, projectv1.ListProjectsResponse](srv.Client(), srv.URL+projectv1.ProjectService_ListProjects_FullMethodName, connect.WithProtoJSON())
	req := connect.NewRequest(&projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 2}})
	req.Header().Set(transport.AuthorizationMetadataKey, token)
	got, err := list.CallUnary(context.Background(), req)
	if err != nil {
		t.Fatalf("ListProjects HTTP: %v", err)
	}
	if len(got.Msg.GetProjects()) != 2 || got.Msg.GetProjects()[1].GetRevision() != 3 || got.Msg.GetPage().GetNextCursor() != "p2" {
		t.Fatalf("paged projection = %+v", got.Msg)
	}
	if service.cursor != "" || service.limit != 2 || service.calls != 1 {
		t.Fatalf("service saw cursor=%q limit=%d calls=%d", service.cursor, service.limit, service.calls)
	}
	defReq := connect.NewRequest(&projectv1.ListProjectsRequest{})
	defReq.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := list.CallUnary(context.Background(), defReq); err != nil {
		t.Fatalf("default page: %v", err)
	}
	if service.limit != 50 {
		t.Fatalf("default page size forwarded = %d, want 50", service.limit)
	}
	badReq := connect.NewRequest(&projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 101}})
	badReq.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := list.CallUnary(context.Background(), badReq); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversize page HTTP code = %v", connect.CodeOf(err))
	}
}

type pm027DenyService struct {
	projectservice.Service
}

func (s *pm027DenyService) GetProject(context.Context, *trust.Principal, string) (projectservice.ProjectRecord, error) {
	return projectservice.ProjectRecord{}, projectaccess.ErrUnauthorized
}

func (s *pm027DenyService) UpdateProjectSettings(context.Context, *trust.Principal, projectservice.UpdateProjectSettingsRequest) (projectservice.ProjectRecord, error) {
	return projectservice.ProjectRecord{}, domainproject.ErrRevisionConflict
}

// TestTodo_PM_027_Security covers edge authentication and the HTTP
// projection of application denials and revision conflicts.
func TestTodo_PM_027_Security(t *testing.T) {
	srv, token := pm027Serve(t, transportproject.Dependencies{Service: &pm027DenyService{}})
	get := connect.NewClient[projectv1.GetProjectRequest, projectv1.GetProjectResponse](srv.Client(), srv.URL+projectv1.ProjectService_GetProject_FullMethodName, connect.WithProtoJSON())
	if _, err := get.CallUnary(context.Background(), connect.NewRequest(&projectv1.GetProjectRequest{ProjectId: "p1"})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("missing token code = %v", connect.CodeOf(err))
	}
	forged := connect.NewRequest(&projectv1.GetProjectRequest{ProjectId: "p1"})
	forged.Header().Set(transport.AuthorizationMetadataKey, "Bearer not-a-token")
	if _, err := get.CallUnary(context.Background(), forged); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("forged token code = %v", connect.CodeOf(err))
	}
	denied := connect.NewRequest(&projectv1.GetProjectRequest{ProjectId: "p1"})
	denied.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := get.CallUnary(context.Background(), denied); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("denied read code = %v", connect.CodeOf(err))
	}
	update := connect.NewClient[projectv1.UpdateProjectSettingsRequest, projectv1.UpdateProjectSettingsResponse](srv.Client(), srv.URL+projectv1.ProjectService_UpdateProjectSettings_FullMethodName, connect.WithProtoJSON())
	stale := connect.NewRequest(&projectv1.UpdateProjectSettingsRequest{ProjectId: "p1", Name: "Stale", ProjectTimezone: "UTC", ExpectedProjectRevision: 1, IdempotencyKey: "stale-once"})
	stale.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := update.CallUnary(context.Background(), stale); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("conflicted write code = %v", connect.CodeOf(err))
	}
}

type pm027ReplayService struct {
	projectservice.Service
	keys       []string
	principals []*trust.Principal
}

func (s *pm027ReplayService) observe(p *trust.Principal) {
	s.principals = append(s.principals, p)
}

func (s *pm027ReplayService) CreateProject(_ context.Context, p *trust.Principal, r projectservice.CreateProjectRequest) (projectservice.ProjectRecord, error) {
	s.observe(p)
	s.keys = append(s.keys, r.IdempotencyKey)
	return projectservice.ProjectRecord{ID: "replay-" + r.IdempotencyKey, TenantID: transporttest.Tenant, OwnerID: transporttest.Subject, Name: r.Name, Timezone: r.Timezone, State: domainproject.LifecycleActive, Revision: 1}, nil
}

func (s *pm027ReplayService) GetProject(_ context.Context, p *trust.Principal, id string) (projectservice.ProjectRecord, error) {
	s.observe(p)
	return projectservice.ProjectRecord{ID: id, TenantID: transporttest.Tenant, Name: "Parity", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 1}, nil
}

func (s *pm027ReplayService) UpdateProjectSettings(_ context.Context, p *trust.Principal, r projectservice.UpdateProjectSettingsRequest) (projectservice.ProjectRecord, error) {
	s.observe(p)
	return projectservice.ProjectRecord{ID: r.ProjectID, TenantID: transporttest.Tenant, Name: r.Name, Timezone: r.Timezone, State: domainproject.LifecycleActive, Revision: r.ExpectedProjectRevision + 1}, nil
}

// TestTodo_PM_027_Integration covers replayed idempotency keys and the
// shared application path: every HTTP operation reaches the same service
// with the same verified principal and revision results.
func TestTodo_PM_027_Integration(t *testing.T) {
	service := &pm027ReplayService{}
	srv, token := pm027Serve(t, transportproject.Dependencies{Service: service})
	create := connect.NewClient[projectv1.CreateProjectRequest, projectv1.CreateProjectResponse](srv.Client(), srv.URL+projectv1.ProjectService_CreateProject_FullMethodName, connect.WithProtoJSON())
	var first *projectv1.CreateProjectResponse
	for i := 0; i < 2; i++ {
		req := connect.NewRequest(&projectv1.CreateProjectRequest{Name: "Replay", ProjectTimezone: "UTC", IdempotencyKey: "replay-once"})
		req.Header().Set(transport.AuthorizationMetadataKey, token)
		got, err := create.CallUnary(context.Background(), req)
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if i == 0 {
			first = got.Msg
		} else if !proto.Equal(first, got.Msg) {
			t.Fatalf("replay results differ: first=%v second=%v", first, got.Msg)
		}
	}
	if len(service.keys) != 2 || service.keys[0] != "replay-once" || service.keys[1] != "replay-once" {
		t.Fatalf("service saw keys %q", service.keys)
	}
	get := connect.NewClient[projectv1.GetProjectRequest, projectv1.GetProjectResponse](srv.Client(), srv.URL+projectv1.ProjectService_GetProject_FullMethodName, connect.WithProtoJSON())
	getReq := connect.NewRequest(&projectv1.GetProjectRequest{ProjectId: "replay-replay-once"})
	getReq.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := get.CallUnary(context.Background(), getReq); err != nil {
		t.Fatalf("get after create: %v", err)
	}
	update := connect.NewClient[projectv1.UpdateProjectSettingsRequest, projectv1.UpdateProjectSettingsResponse](srv.Client(), srv.URL+projectv1.ProjectService_UpdateProjectSettings_FullMethodName, connect.WithProtoJSON())
	updateReq := connect.NewRequest(&projectv1.UpdateProjectSettingsRequest{ProjectId: "replay-replay-once", Name: "Replayed", ProjectTimezone: "UTC", ExpectedProjectRevision: 1, IdempotencyKey: "update-once"})
	updateReq.Header().Set(transport.AuthorizationMetadataKey, token)
	updated, err := update.CallUnary(context.Background(), updateReq)
	if err != nil || updated.Msg.GetProject().GetRevision() != 2 {
		t.Fatalf("update revision = %+v err=%v", updated, err)
	}
	if len(service.principals) != 4 {
		t.Fatalf("service saw %d principals, want 4", len(service.principals))
	}
	for i, p := range service.principals {
		if p == nil || p.Subject() != transporttest.Subject || p.Tenant() != transporttest.Tenant {
			t.Fatalf("principal %d = %+v", i, p)
		}
	}
}

// TestTodo_PM_027_Conformance exercises one authorized read and write over
// the production Connect edge. The same project handlers are also registered
// on gRPC, so their wire values and write idempotency key are asserted here.
func TestTodo_PM_027_Conformance(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	service := &conformanceService{}
	deps := transportproject.Dependencies{Service: service}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "pm027-request", nil)
	h, err := edge.NewHandler(edge.Options{
		Config:  cfg,
		Project: &deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(h)
	t.Cleanup(httpServer.Close)

	get := connect.NewClient[projectv1.GetProjectRequest, projectv1.GetProjectResponse](httpServer.Client(), httpServer.URL+projectv1.ProjectService_GetProject_FullMethodName, connect.WithProtoJSON())
	getReq := connect.NewRequest(&projectv1.GetProjectRequest{ProjectId: "project-a"})
	getReq.Header().Set(transport.AuthorizationMetadataKey, token)
	got, err := get.CallUnary(context.Background(), getReq)
	if err != nil {
		t.Fatalf("GetProject HTTP: %v", err)
	}
	if got.Msg.GetProject().GetProjectId() != "project-a" || got.Msg.GetProject().GetRevision() != 9 || got.Msg.GetProject().GetWorkflowRevision() != 4 {
		t.Fatalf("GetProject projection = %+v", got.Msg)
	}
	if service.principal == nil || service.principal.Subject() != transporttest.Subject || service.principal.Tenant().String() != transporttest.Tenant {
		t.Fatalf("HTTP read did not receive verified principal: %+v", service.principal)
	}

	create := connect.NewClient[projectv1.CreateProjectRequest, projectv1.CreateProjectResponse](httpServer.Client(), httpServer.URL+projectv1.ProjectService_CreateProject_FullMethodName, connect.WithProtoJSON())
	createReq := connect.NewRequest(&projectv1.CreateProjectRequest{Name: "Parity", ProjectTimezone: "UTC", IdempotencyKey: "create-once"})
	createReq.Header().Set(transport.AuthorizationMetadataKey, token)
	created, err := create.CallUnary(context.Background(), createReq)
	if err != nil {
		t.Fatalf("CreateProject HTTP: %v", err)
	}
	if created.Msg.GetProject().GetProjectId() != "created-project" || created.Msg.GetProject().GetRevision() != 1 || service.key != "create-once" {
		t.Fatalf("CreateProject projection/key = %+v, %q", created.Msg, service.key)
	}
	if service.principal == nil || service.principal.Subject() != transporttest.Subject || service.principal.Tenant().String() != transporttest.Tenant {
		t.Fatalf("HTTP write did not receive verified principal: %+v", service.principal)
	}
	update := connect.NewClient[projectv1.UpdateProjectSettingsRequest, projectv1.UpdateProjectSettingsResponse](httpServer.Client(), httpServer.URL+projectv1.ProjectService_UpdateProjectSettings_FullMethodName, connect.WithProtoJSON())
	updateReq := connect.NewRequest(&projectv1.UpdateProjectSettingsRequest{ProjectId: "created-project", Name: "Parity Updated", ProjectTimezone: "Europe/Paris", ExpectedProjectRevision: 1, IdempotencyKey: "settings-once"})
	updateReq.Header().Set(transport.AuthorizationMetadataKey, token)
	updated, err := update.CallUnary(context.Background(), updateReq)
	if err != nil || updated.Msg.GetProject().GetName() != "Parity Updated" || updated.Msg.GetProject().GetRevision() != 2 {
		t.Fatalf("UpdateProjectSettings HTTP: result=%v err=%v", updated, err)
	}
	archive := connect.NewClient[projectv1.SetProjectLifecycleRequest, projectv1.SetProjectLifecycleResponse](httpServer.Client(), httpServer.URL+projectv1.ProjectService_ArchiveProject_FullMethodName, connect.WithProtoJSON())
	archiveReq := connect.NewRequest(&projectv1.SetProjectLifecycleRequest{ProjectId: "created-project", ExpectedProjectRevision: 2, IdempotencyKey: "archive-once"})
	archiveReq.Header().Set(transport.AuthorizationMetadataKey, token)
	archived, err := archive.CallUnary(context.Background(), archiveReq)
	if err != nil || archived.Msg.GetProject().GetLifecycle() != projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED {
		t.Fatalf("ArchiveProject HTTP: result=%v err=%v", archived, err)
	}
	restore := connect.NewClient[projectv1.SetProjectLifecycleRequest, projectv1.SetProjectLifecycleResponse](httpServer.Client(), httpServer.URL+projectv1.ProjectService_RestoreProject_FullMethodName, connect.WithProtoJSON())
	restoreReq := connect.NewRequest(&projectv1.SetProjectLifecycleRequest{ProjectId: "created-project", ExpectedProjectRevision: 3, IdempotencyKey: "restore-once"})
	restoreReq.Header().Set(transport.AuthorizationMetadataKey, token)
	restored, err := restore.CallUnary(context.Background(), restoreReq)
	if err != nil || restored.Msg.GetProject().GetLifecycle() != projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE {
		t.Fatalf("RestoreProject HTTP: result=%v err=%v", restored, err)
	}

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	transportproject.Register(grpcServer, deps)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///pm027", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rpc := projectv1.NewProjectServiceClient(conn)
	rpcCtx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
	rpcRead, err := rpc.GetProject(rpcCtx, &projectv1.GetProjectRequest{ProjectId: "project-a"})
	if err != nil {
		t.Fatalf("GetProject gRPC: %v", err)
	}
	if !proto.Equal(got.Msg, rpcRead) {
		t.Fatalf("HTTP/gRPC read results differ: HTTP=%v gRPC=%v", got.Msg, rpcRead)
	}
	rpcWrite, err := rpc.CreateProject(rpcCtx, &projectv1.CreateProjectRequest{Name: "Parity", ProjectTimezone: "UTC", IdempotencyKey: "create-once"})
	if err != nil {
		t.Fatalf("CreateProject gRPC: %v", err)
	}
	if !proto.Equal(created.Msg, rpcWrite) {
		t.Fatalf("HTTP/gRPC write results differ: HTTP=%v gRPC=%v", created.Msg, rpcWrite)
	}
	rpcUpdated, err := rpc.UpdateProjectSettings(rpcCtx, &projectv1.UpdateProjectSettingsRequest{ProjectId: "created-project", Name: "Parity Updated", ProjectTimezone: "Europe/Paris", ExpectedProjectRevision: 1, IdempotencyKey: "settings-once"})
	if err != nil || !proto.Equal(updated.Msg, rpcUpdated) {
		t.Fatalf("HTTP/gRPC settings results differ: HTTP=%v gRPC=%v err=%v", updated.Msg, rpcUpdated, err)
	}
	rpcArchived, err := rpc.ArchiveProject(rpcCtx, &projectv1.SetProjectLifecycleRequest{ProjectId: "created-project", ExpectedProjectRevision: 2, IdempotencyKey: "archive-once"})
	if err != nil || !proto.Equal(archived.Msg, rpcArchived) {
		t.Fatalf("HTTP/gRPC archive results differ: HTTP=%v gRPC=%v err=%v", archived.Msg, rpcArchived, err)
	}
	rpcRestored, err := rpc.RestoreProject(rpcCtx, &projectv1.SetProjectLifecycleRequest{ProjectId: "created-project", ExpectedProjectRevision: 3, IdempotencyKey: "restore-once"})
	if err != nil || !proto.Equal(restored.Msg, rpcRestored) {
		t.Fatalf("HTTP/gRPC restore results differ: HTTP=%v gRPC=%v err=%v", restored.Msg, rpcRestored, err)
	}
}
