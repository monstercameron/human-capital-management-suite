package contentregistrystore_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/data/commercialstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/contentregistrystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var contentRegistryTime = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 341); err != nil {
		t.Fatalf("apply migrations through 00341: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTxErr(conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
func manifest(t *testing.T, id string, version int) industrypack.IndustryPack {
	t.Helper()
	pack, err := industrypack.NewIndustryPack(industrypack.IndustryPack{
		PackID: id, Version: version, Industry: industrypack.IndustryHealthcare,
		Owner: "hcmnext", Scope: "healthcare-us", Support: "maintained",
		Compatibility: []industrypack.CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}},
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return pack
}

func contentFixture(t *testing.T) industrypack.Content {
	t.Helper()
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	return industrypack.Content{
		Ref:       industrypack.ContentRef{Kind: industrypack.ContentReferenceData, Namespace: "healthcare", ID: "job-family", Version: "1"},
		Effective: industrypack.OpenWindow(start), Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}
}

func bindingFixture(t *testing.T, pack industrypack.IndustryPack, content industrypack.Content) industrypack.Binding {
	t.Helper()
	binding, err := industrypack.Bind(industrypack.BindingSpec{Packs: []industrypack.Pack{{
		Manifest: pack, References: []industrypack.ContentRef{content.Ref}, Contents: []industrypack.Content{content},
	}}})
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return binding
}

func TestTodo_REV_047_02_PostgreSQLPublicationActivation(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tenant := insertTenant(t, db, "industry-pack-pg-lifecycle")
	contractRevision := commercial.ContractRevision{
		TenantID: tenant.String(), ContractID: "industry-pilot", Revision: 1,
		EffectiveFrom: contentRegistryTime.Add(-time.Hour), EffectiveTo: contentRegistryTime.Add(30 * 24 * time.Hour),
		Capabilities: []string{industrypack.IndustryEntitlementCapability(industrypack.IndustryHealthcare)},
		Bound:        commercial.EntitlementBound{Seats: 5}, PriceCents: 10000, Currency: "USD",
	}
	contracts := commercialstore.New(appConn(t, db))
	if err := contracts.PutContractRevision(ctx, contractRevision); err != nil {
		t.Fatal(err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(contractRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.PutEntitlementSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	authority, err := industrypack.NewIndustryEntitlementAuthority(contracts,
		industrypack.ContractRevisionResolverFunc(func(_ context.Context, tenantID string) (industrypack.ContractRevisionRef, error) {
			if tenantID != tenant.String() {
				return industrypack.ContractRevisionRef{}, commercial.ErrStoreNotFound
			}
			return industrypack.ContractRevisionRef{ContractID: contractRevision.ContractID, Revision: contractRevision.Revision}, nil
		}), industrypack.EntitlementClockFunc(func() time.Time { return contentRegistryTime }), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := contentregistrystore.New(appConn(t, db))
	pack := manifest(t, "healthcare-pg", 1)
	report, effects, err := industrypack.PublishChecked(ctx, store, authority, tenant.String(), industrypack.PublicationCheck{
		TenantID: tenant.String(), Candidate: pack, Installed: map[string]string{"hcmnext": "1.8"},
	})
	if err != nil || effects.AuthoritativeRows != 1 || report.Entitlement.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("publication report=%+v effects=%+v err=%v", report, effects, err)
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{47}, ed25519.SeedSize))
	target := industrypack.PackTarget{Tenant: tenant.String(), Cell: "cell-local"}
	bundleDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	envelope := industrypack.SignPackVersion(industrypack.SignedPackVersion{
		PackID: pack.PackID, Industry: pack.Industry, Version: pack.Version, BundleDigest: bundleDigest,
		Target: target, EffectiveAt: contentRegistryTime.Add(time.Minute), RollbackVersion: 0,
		Publisher: "publisher:one", Approver: "approver:two", SignedAt: contentRegistryTime.Add(-time.Minute),
	}, "industry-test-key", private)
	receipt, effects, err := industrypack.ActivateSigned(ctx, store, authority, industrypack.ActivationRequest{
		Envelope: envelope, Target: target, TrustedKeys: map[string]ed25519.PublicKey{"industry-test-key": private.Public().(ed25519.PublicKey)},
	})
	if err != nil || effects.AuthoritativeRows != 1 || receipt.Entitlement.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("activation receipt=%+v effects=%+v err=%v", receipt, effects, err)
	}
	var publicationCount, activationCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM industry_pack_publication WHERE tenant_id=$1 AND pack_id=$2`, tenant, pack.PackID).Scan(&publicationCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM industry_pack_activation WHERE tenant_id=$1 AND pack_id=$2`, tenant, pack.PackID).Scan(&activationCount); err != nil {
		t.Fatal(err)
	}
	if publicationCount != 1 || activationCount != 1 {
		t.Fatalf("durable lifecycle rows publication=%d activation=%d", publicationCount, activationCount)
	}
}

func articleFixture(t *testing.T, id string) knowledge.ArticleRevision {
	t.Helper()
	instant := func(text string) values.Instant {
		at, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatal(err)
		}
		return values.NewInstant(at)
	}
	return knowledge.ArticleRevision{
		ArticleID: id, Revision: 1, Locale: "en-US", AudienceScope: "EMPLOYEES",
		AuthorizedRoles: []string{"worker_self"}, RetentionScheduleRef: "records:knowledge/current",
		Classification: "INTERNAL", Owner: "policy-owner", SourceAuthority: "policy-authority",
		SourceRefs:   []knowledge.SourceRef{{System: "policy", Identifier: "source-1", Authority: "authority-1"}},
		Jurisdiction: "US", EffectiveInterval: knowledge.EffectiveInterval{EffectiveFrom: instant("2026-01-01T00:00:00Z"), EffectiveTo: instant("2027-01-01T00:00:00Z")},
		KnownInterval: knowledge.KnownInterval{KnownFrom: instant("2025-12-01T00:00:00Z"), KnownTo: instant("2026-01-01T00:00:00Z")},
		BodyDigest:    "sha256:2222222222222222222222222222222222222222222222222222222222222222", Title: "Leave policy", Summary: "Summary",
		Review: knowledge.ReviewMetadata{ReviewedBy: "reviewer", ReviewedAt: instant("2026-01-02T00:00:00Z"), ExpiresAt: instant("2027-01-02T00:00:00Z"), ApprovalRef: "approval-1"},
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001(t *testing.T) {
	db := newDB(t)
	adminStore := contentregistrystore.New(db.Conn)
	pack := manifest(t, "healthcare-primary", 1)
	content := contentFixture(t)
	if err := adminStore.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := adminStore.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	if _, err := adminStore.LoadManifest(context.Background(), pack.PackID, pack.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := adminStore.LoadContent(context.Background(), content.Ref); err != nil {
		t.Fatal(err)
	}

	tenantID := insertTenant(t, db, "content-primary")
	store := contentregistrystore.New(appConn(t, db))
	binding := bindingFixture(t, pack, content)
	article := articleFixture(t, "article-primary")
	if err := store.SaveBinding(context.Background(), tenantID.String(), binding); err != nil {
		t.Fatalf("save binding: %v", err)
	}
	if err := store.SaveArticle(context.Background(), tenantID.String(), article); err != nil {
		t.Fatalf("save article: %v", err)
	}
	localized := knowledge.LocalizedRevision{ArticleID: article.ArticleID, Revision: 1, Locale: "es-US", Reviewer: "translator", SourceRevisionDigest: article.Digest(), TranslationProvenance: knowledge.ProvenanceHuman, BodyDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333", Title: "Política de licencia", Summary: "Resumen", CreatedAt: contentRegistryTime}
	if err := store.SaveLocalized(context.Background(), tenantID.String(), localized); err != nil {
		t.Fatalf("save localized: %v", err)
	}
	event := knowledge.LifecycleEvent{EventID: "event-primary", Kind: knowledge.EventDrafted, State: knowledge.StateDraft, ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, At: contentRegistryTime}
	if err := store.AppendLifecycleEvent(context.Background(), tenantID.String(), event, 1); err != nil {
		t.Fatalf("append event: %v", err)
	}
	activation := knowledge.ActivationBinding{ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, BundleID: "bundle-1", BundleDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444", ActivationEpoch: 1, ActivatedAt: contentRegistryTime}
	if err := store.SaveKnowledgeActivation(context.Background(), tenantID.String(), activation); err != nil {
		t.Fatalf("save activation: %v", err)
	}
	gotBinding, err := store.LoadBinding(context.Background(), tenantID.String(), binding.CanonicalDigest)
	if err != nil || gotBinding.CanonicalDigest != binding.CanonicalDigest {
		t.Fatalf("load binding=%+v err=%v", gotBinding, err)
	}
	gotArticle, err := store.LoadArticle(context.Background(), tenantID.String(), article.ArticleID, 1)
	if err != nil || gotArticle.Digest() != article.Digest() {
		t.Fatalf("load article digest=%s err=%v want=%s", gotArticle.Digest(), err, article.Digest())
	}
	gotEvents, err := store.ListLifecycleEvents(context.Background(), tenantID.String(), article.ArticleID)
	if err != nil || len(gotEvents) != 1 {
		t.Fatalf("events=%+v err=%v", gotEvents, err)
	}
	gotActivation, err := store.LoadActivation(context.Background(), tenantID.String(), article.ArticleID, article.Locale)
	if err != nil || gotActivation.ActivationEpoch != 1 {
		t.Fatalf("activation=%+v err=%v", gotActivation, err)
	}
	candidates, err := store.SearchCandidates(context.Background(), tenantID.String(), article.Locale, "leave", article.AudienceScope, []string{"worker_self"}, contentRegistryTime)
	if err != nil || len(candidates) != 1 || candidates[0].Article.ArticleID != article.ArticleID {
		t.Fatalf("authorized active search candidates=%+v err=%v", candidates, err)
	}
	results, err := (knowledge.SearchService{Source: store}).Search(context.Background(), knowledge.SearchRequest{
		TenantID: tenantID.String(), Query: "leave", Locale: article.Locale,
		Audience: article.AudienceScope, Roles: []string{"worker_self"}, At: contentRegistryTime,
	})
	if err != nil || len(results) != 1 || results[0].ArticleID != article.ArticleID {
		t.Fatalf("authorized durable search results=%+v err=%v", results, err)
	}
	candidates, err = store.SearchCandidates(context.Background(), tenantID.String(), article.Locale, "leave", article.AudienceScope, []string{"comp_admin"}, contentRegistryTime)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("role-mismatched search candidates=%+v err=%v", candidates, err)
	}
	otherTenant := insertTenant(t, db, "content-search-other-tenant")
	otherStore := contentregistrystore.New(appConn(t, db))
	candidates, err = otherStore.SearchCandidates(context.Background(), otherTenant.String(), article.Locale, "leave", article.AudienceScope, []string{"worker_self"}, contentRegistryTime)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("foreign tenant search candidates=%+v err=%v", candidates, err)
	}
}

func TestTodo_REV_077_02_Integration_StaleCandidatesDoNotStarveCurrentArticle(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "knowledge-stale-candidate-search")
	store := contentregistrystore.New(appConn(t, db))
	current := articleFixture(t, "current-leave-article")
	current.Title = "Z Leave policy current"
	if err := store.SaveArticle(context.Background(), tenantID.String(), current); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveKnowledgeActivation(context.Background(), tenantID.String(), knowledge.ActivationBinding{
		ArticleID: current.ArticleID, Revision: current.Revision, Locale: current.Locale,
		BundleID: "current-bundle", BundleDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		ActivationEpoch: 1, ActivatedAt: contentRegistryTime,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		stale := articleFixture(t, fmt.Sprintf("stale-leave-%03d", i))
		stale.Title = fmt.Sprintf("A stale leave article %03d", i)
		stale.EffectiveInterval.EffectiveTo = values.NewInstant(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
		stale.Review.ExpiresAt = values.NewInstant(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
		if err := store.SaveArticle(context.Background(), tenantID.String(), stale); err != nil {
			t.Fatalf("save stale article %d: %v", i, err)
		}
		if err := store.SaveKnowledgeActivation(context.Background(), tenantID.String(), knowledge.ActivationBinding{
			ArticleID: stale.ArticleID, Revision: stale.Revision, Locale: stale.Locale,
			BundleID: fmt.Sprintf("stale-bundle-%03d", i), BundleDigest: "sha256:5555555555555555555555555555555555555555555555555555555555555555",
			ActivationEpoch: 1, ActivatedAt: contentRegistryTime,
		}); err != nil {
			t.Fatalf("activate stale article %d: %v", i, err)
		}
	}
	results, err := (knowledge.SearchService{Source: store}).Search(context.Background(), knowledge.SearchRequest{
		TenantID: tenantID.String(), Query: "leave", Locale: current.Locale,
		Audience: current.AudienceScope, Roles: []string{"worker_self"}, At: contentRegistryTime,
	})
	if err != nil || len(results) != 1 || results[0].ArticleID != current.ArticleID {
		t.Fatalf("current article after more than 200 stale matching candidates=%+v err=%v", results, err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Integration(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-integration", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-integration")
	store := contentregistrystore.New(appConn(t, db))
	if err := store.SaveBinding(context.Background(), tenantID.String(), bindingFixture(t, pack, content)); err != nil {
		t.Fatal(err)
	}
	fresh := contentregistrystore.New(appConn(t, db))
	if _, err := fresh.LoadBinding(context.Background(), tenantID.String(), bindingFixture(t, pack, content).CanonicalDigest); err != nil {
		t.Fatalf("fresh connection load: %v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Security(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-security", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	alpha, beta := insertTenant(t, db, "content-alpha"), insertTenant(t, db, "content-beta")
	binding := bindingFixture(t, pack, content)
	store := contentregistrystore.New(appConn(t, db))
	if err := store.SaveBinding(context.Background(), alpha.String(), binding); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tenantTxErr(appConn(t, db), beta, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM industry_pack_binding WHERE tenant_id=$1`, alpha).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant binding count=%d", count)
	}
	if _, err := store.LoadBinding(context.Background(), beta.String(), binding.CanonicalDigest); !errors.Is(err, contentregistrystore.ErrNotFound) {
		t.Fatalf("cross-tenant load=%v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Recovery(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-recovery", 1)
	content := contentFixture(t)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	if err := admin.SaveContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-recovery")
	binding := bindingFixture(t, pack, content)
	if err := contentregistrystore.New(appConn(t, db)).SaveBinding(context.Background(), tenantID.String(), binding); err != nil {
		t.Fatal(err)
	}
	if _, err := contentregistrystore.New(appConn(t, db)).LoadBinding(context.Background(), tenantID.String(), binding.CanonicalDigest); err != nil {
		t.Fatalf("reload after fresh connection: %v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Fault(t *testing.T) {
	db := newDB(t)
	pack := manifest(t, "healthcare-fault", 1)
	admin := contentregistrystore.New(db.Conn)
	if err := admin.SaveManifest(context.Background(), pack); err != nil {
		t.Fatal(err)
	}
	tenantID := insertTenant(t, db, "content-fault")
	store := contentregistrystore.New(appConn(t, db))
	article := articleFixture(t, "article-fault")
	if err := store.SaveArticle(context.Background(), tenantID.String(), article); err != nil {
		t.Fatal(err)
	}
	err := store.SaveArticle(context.Background(), tenantID.String(), article)
	var typed *contentregistrystore.Error
	if !errors.Is(err, contentregistrystore.ErrDuplicate) || !errors.As(err, &typed) || typed.Code != contentregistrystore.CodeDuplicate {
		t.Fatalf("duplicate=%v typed=%+v", err, typed)
	}
	activation := knowledge.ActivationBinding{ArticleID: article.ArticleID, Revision: 1, Locale: article.Locale, ActivationEpoch: 2, ActivatedAt: contentRegistryTime}
	if err := store.SaveKnowledgeActivation(context.Background(), tenantID.String(), activation); err != nil {
		t.Fatal(err)
	}
	activation.ActivationEpoch = 1
	err = store.SaveKnowledgeActivation(context.Background(), tenantID.String(), activation)
	if !errors.Is(err, contentregistrystore.ErrStaleCAS) {
		t.Fatalf("stale activation=%v", err)
	}
}

func TestTodo_PERSIST_CONTENTREGISTRY_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "content-mutation")
	store := contentregistrystore.New(appConn(t, db))
	event := knowledge.LifecycleEvent{EventID: "event-mutation", Kind: knowledge.EventDrafted, State: knowledge.StateDraft, ArticleID: "article-mutation", Revision: 1, At: contentRegistryTime}
	if err := store.AppendLifecycleEvent(context.Background(), tenantID.String(), event, 1); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	for _, statement := range []string{
		`UPDATE knowledge_lifecycle_event SET state='RETIRED' WHERE tenant_id=$1`,
		`DELETE FROM knowledge_lifecycle_event WHERE tenant_id=$1`,
	} {
		if err := tenantTxErr(conn, tenantID, func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), statement, tenantID); return err }); err == nil {
			t.Fatalf("mutation accepted: %s", statement)
		}
	}
}
