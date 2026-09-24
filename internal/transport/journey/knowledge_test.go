package journey_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/contentregistrystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type knowledgeSourceFixture struct {
	article    knowledge.SearchableArticle
	candidates []knowledge.SearchableArticle
	tenant     string
	roles      []string
	calls      int
}

func (s *knowledgeSourceFixture) SearchCandidates(_ context.Context, tenant, _, _, _ string, roles []string, _ time.Time) ([]knowledge.SearchableArticle, error) {
	s.tenant, s.roles, s.calls = tenant, append([]string(nil), roles...), s.calls+1
	if len(s.candidates) != 0 {
		return append([]knowledge.SearchableArticle(nil), s.candidates...), nil
	}
	return []knowledge.SearchableArticle{s.article}, nil
}

func transportKnowledgeArticle(role string) knowledge.SearchableArticle {
	instant := func(value string) values.Instant {
		at, _ := time.Parse(time.RFC3339, value)
		return values.NewInstant(at)
	}
	return knowledge.SearchableArticle{Article: knowledge.ArticleRevision{
		ArticleID: "leave-policy", Revision: 4, Locale: "en-US", AudienceScope: "EMPLOYEES",
		AuthorizedRoles: []string{role}, RetentionScheduleRef: "records:knowledge/current",
		Classification: "INTERNAL", Owner: "policy-owner", SourceAuthority: "policy-authority",
		SourceRefs:   []knowledge.SourceRef{{System: "policy", Identifier: "leave", Authority: "hr-policy"}},
		Jurisdiction: "US", EffectiveInterval: knowledge.EffectiveInterval{EffectiveFrom: instant("2026-01-01T00:00:00Z")},
		KnownInterval: knowledge.KnownInterval{KnownFrom: instant("2026-01-01T00:00:00Z")},
		BodyDigest:    "sha256:leave", Title: "Annual leave policy", Summary: "How to request leave",
		Review: knowledge.ReviewMetadata{ReviewedBy: "reviewer", ReviewedAt: instant("2026-01-01T00:00:00Z"), ExpiresAt: instant("2027-01-01T00:00:00Z"), ApprovalRef: "approval"},
	}}
}

func knowledgePageGrant(role string) roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles:              roleaccess.DefaultRoles(),
		PagePermissions:    []roleaccess.PagePermission{{Version: 1, RoleID: role, PageID: "knowledge-search", View: true}},
		FeaturePermissions: []roleaccess.FeaturePermission{{Version: 1, RoleID: role, PageID: "knowledge-search", FeatureID: "content", View: true}},
	}
}

func TestTodo_REV_077_02_SearchKnowledgeAuthorizedRPC(t *testing.T) {
	const now = "2026-06-01T00:00:00Z"
	source := &knowledgeSourceFixture{article: transportKnowledgeArticle("worker_self")}
	client := startRT2Server(t, journey.Dependencies{
		RoleAccess: &rt2Store{snapshot: knowledgePageGrant("worker_self")},
		Knowledge:  knowledge.SearchService{Source: source},
		Now:        func() time.Time { value, _ := time.Parse(time.RFC3339, now); return value },
	})
	response, err := client.SearchKnowledge(rt2CallContext(t, rt2WorkerToken), &journeyv1.SearchKnowledgeRequest{Query: "leave policy", Locale: "en-US"})
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	if len(response.GetMatches()) != 1 || response.GetMatches()[0].GetArticleId() != "leave-policy" || response.GetMatches()[0].GetRevision() != 4 {
		t.Fatalf("search response = %+v", response.GetMatches())
	}
	if source.tenant != fixtureTenant || len(source.roles) != 1 || source.roles[0] != "worker_self" {
		t.Fatalf("search scope tenant=%q roles=%v", source.tenant, source.roles)
	}
}

func TestTodo_REV_077_02_SearchKnowledgeSecurity(t *testing.T) {
	t.Run("page grant is required before source access", func(t *testing.T) {
		source := &knowledgeSourceFixture{article: transportKnowledgeArticle("worker_self")}
		client := startRT2Server(t, journey.Dependencies{RoleAccess: &rt2Store{snapshot: roleaccess.Snapshot{Roles: roleaccess.DefaultRoles()}}, Knowledge: knowledge.SearchService{Source: source}})
		_, err := client.SearchKnowledge(rt2CallContext(t, rt2WorkerToken), &journeyv1.SearchKnowledgeRequest{Query: "leave", Locale: "en-US"})
		if status.Code(err) != codes.PermissionDenied || source.calls != 0 {
			t.Fatalf("ungranted search error=%v source calls=%d", err, source.calls)
		}
	})
	t.Run("durable role assignment overrides credential role", func(t *testing.T) {
		source := &knowledgeSourceFixture{article: transportKnowledgeArticle("comp_admin")}
		snapshot := knowledgePageGrant("worker_self")
		snapshot.Assignments = []roleaccess.Assignment{{Version: 1, WorkerRef: "rbac-rex", RoleIDs: []string{"worker_self"}}}
		client := startRT2Server(t, journey.Dependencies{RoleAccess: &rt2Store{snapshot: snapshot}, Knowledge: knowledge.SearchService{Source: source}})
		response, err := client.SearchKnowledge(rt2CallContext(t, rt2RevokedToken), &journeyv1.SearchKnowledgeRequest{Query: "leave", Locale: "en-US"})
		if err != nil || len(response.GetMatches()) != 0 {
			t.Fatalf("revoked-role results=%v error=%v", response.GetMatches(), err)
		}
		if len(source.roles) != 1 || source.roles[0] != "worker_self" {
			t.Fatalf("source received credential roles, got %v", source.roles)
		}
	})
}

func TestTodo_REV_026_01_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(ctx, 341); err != nil {
		t.Fatalf("apply migrations through knowledge search authorization: %v", err)
	}
	tenantID := uuid.New()
	foreignTenantID := uuid.New()
	for _, tenant := range []uuid.UUID{tenantID, foreignTenantID} {
		db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, tenant.String(), tenant.String())
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	store := contentregistrystore.New(conn)
	primary := transportKnowledgeArticle("worker_self").Article
	primary.ArticleID = "rev026-visible"
	primary.BodyDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	foreign := transportKnowledgeArticle("worker_self").Article
	foreign.ArticleID = "rev026-foreign-tenant"
	foreign.BodyDigest = primary.BodyDigest
	denied := transportKnowledgeArticle("comp_admin").Article
	denied.ArticleID = "rev026-role-restricted"
	denied.BodyDigest = primary.BodyDigest
	denied.Title = "Leave policy for compensation administrators"
	for _, entry := range []struct {
		tenant  uuid.UUID
		article knowledge.ArticleRevision
	}{{tenantID, primary}, {tenantID, denied}, {foreignTenantID, foreign}} {
		if err := store.SaveArticle(ctx, entry.tenant.String(), entry.article); err != nil {
			t.Fatalf("save article %s: %v", entry.article.ArticleID, err)
		}
		if err := store.SaveKnowledgeActivation(ctx, entry.tenant.String(), knowledge.ActivationBinding{
			ArticleID: entry.article.ArticleID, Revision: entry.article.Revision, Locale: entry.article.Locale,
			BundleID: "rev026-bundle", BundleDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
			ActivationEpoch: 1, ActivatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("activate article %s: %v", entry.article.ArticleID, err)
		}
	}
	snapshot := knowledgePageGrant("worker_self")
	snapshot.Assignments = []roleaccess.Assignment{{Version: 1, WorkerRef: "rev026-worker", RoleIDs: []string{"worker_self"}}}
	client := startKnowledgeSearchServer(t, tenantID.String(), journey.Dependencies{
		RoleAccess: &rt2Store{snapshot: snapshot},
		Knowledge:  knowledge.SearchService{Source: store},
		Now:        func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) },
	})
	response, err := client.SearchKnowledge(rt2CallContext(t, "rev026-real-store-token"), &journeyv1.SearchKnowledgeRequest{Query: "leave policy", Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetMatches()) != 1 || response.GetMatches()[0].GetArticleId() != primary.ArticleID {
		t.Fatalf("served search matches = %+v", response.GetMatches())
	}
	match := response.GetMatches()[0]
	if match.GetSourceDigest() == "" || match.GetPolicyDigest() == "" || match.GetAuthorizationEvidence() == "" || match.GetScore() <= 0 {
		t.Fatalf("served hit omitted engine evidence: %+v", match)
	}
	if response.GetEnvelopeDigest() == "" || response.GetSuppressedDigest() == "" {
		t.Fatalf("served response omitted query proof: %+v", response)
	}
}

type knowledgeSearchVerifier struct{ tenant string }

func (v knowledgeSearchVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	if cred.Token != "rev026-real-store-token" {
		return nil, trust.ErrInvalidCredential
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(v.tenant), OrganizationScopeID: fixtureOrganization, Subject: "rev026-worker",
		SubjectKind: trust.SubjectKindHuman, Roles: []string{"comp_admin"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-rev026-real-store", Purposes: []string{"self_service_view"},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest:rev026-real-store",
	})
}

func startKnowledgeSearchServer(t *testing.T, tenant string, deps journey.Dependencies) journeyv1.JourneyServiceClient {
	t.Helper()
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(transport.Config{Verifier: knowledgeSearchVerifier{tenant: tenant}})))
	journey.Register(srv, deps)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return journeyv1.NewJourneyServiceClient(conn)
}

func TestTodo_REV_026_01_Security(t *testing.T) {
	allowed := transportKnowledgeArticle("worker_self")
	denied := transportKnowledgeArticle("comp_admin")
	denied.Article.ArticleID = "restricted-secret"
	denied.Article.Title = "Leave policy for compensation admins"
	source := &knowledgeSourceFixture{candidates: []knowledge.SearchableArticle{allowed, denied}}
	client := startRT2Server(t, journey.Dependencies{
		RoleAccess: &rt2Store{snapshot: knowledgePageGrant("worker_self")},
		Knowledge:  knowledge.SearchService{Source: source},
		Now:        func() time.Time { value, _ := time.Parse(time.RFC3339, "2026-06-01T00:00:00Z"); return value },
	})
	response, err := client.SearchKnowledge(rt2CallContext(t, rt2WorkerToken), &journeyv1.SearchKnowledgeRequest{Query: "leave policy", Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetMatches()) != 1 || response.GetMatches()[0].GetArticleId() != "leave-policy" {
		t.Fatalf("role-restricted hit reached caller: %+v", response.GetMatches())
	}
	if strings.Contains(response.GetSuppressedDigest(), "restricted-secret") || response.GetSuppressedDigest() == "" {
		t.Fatalf("suppression evidence is missing or identifies the denied article: %q", response.GetSuppressedDigest())
	}
}
