package commercialstore_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/data/commercialstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var testAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 340); err != nil {
		t.Fatalf("apply migrations through 00340: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4)`, id, key, key, testAt)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func contract(tenant uuid.UUID, revision uint64) commercial.ContractRevision {
	return commercial.ContractRevision{
		TenantID: tenant.String(), ContractID: "contract-pilot", Revision: revision,
		EffectiveFrom: testAt, EffectiveTo: testAt.Add(90 * 24 * time.Hour),
		Capabilities: []string{"promotion.simulate", "worker.explain"},
		Bound:        commercial.EntitlementBound{Seats: 10}, PriceCents: 2500000, Currency: "USD",
	}
}

func applicationFixture(t *testing.T) (partnerapp.PartnerApplication, partnerapp.ApplicationVersion) {
	t.Helper()
	app := partnerapp.PartnerApplication{
		PartnerRef: "partner:acme", ApplicationID: "app-payroll", AgreementRef: "agreement:payroll",
		DeclaredCapabilities: []string{"payroll.read"}, DataClasses: []string{"PAYROLL"},
		RedirectEndpoints: []partnerapp.EndpointRef{{Ref: "redirect:payroll", Digest: "digest:redirect"}},
		CallbackEndpoints: []partnerapp.EndpointRef{{Ref: "callback:payroll", Digest: "digest:callback"}},
		ContactRef:        "contact:payroll", LegalRef: "legal:payroll",
	}
	agreement := partnerapp.PartnerAgreement{Ref: app.AgreementRef, Capabilities: app.DeclaredCapabilities, DataClasses: app.DataClasses}
	draft, err := partnerapp.NewDraft(app, partnerapp.VersionRequest{Version: "1.0.0", Requester: "requester-1"}, agreement)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("reviewer-1", true, "approved")
	if err != nil {
		t.Fatal(err)
	}
	active, err := reviewed.Activate()
	if err != nil {
		t.Fatal(err)
	}
	return app, active
}

func seedCatalog(t *testing.T, db *pgtest.DB, app partnerapp.PartnerApplication, version partnerapp.ApplicationVersion) {
	t.Helper()
	capabilities, _ := json.Marshal(app.DeclaredCapabilities)
	classes, _ := json.Marshal(app.DataClasses)
	redirects, _ := json.Marshal(app.RedirectEndpoints)
	callbacks, _ := json.Marshal(app.CallbackEndpoints)
	db.Exec(t, `INSERT INTO partner_application (row_id,partner_ref,application_id,agreement_ref,declared_capabilities,data_classes,redirect_endpoints,callback_endpoints,contact_ref,legal_ref) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10)`, uuid.New(), app.PartnerRef, app.ApplicationID, app.AgreementRef, capabilities, classes, redirects, callbacks, app.ContactRef, app.LegalRef)
	payload := map[string]any{
		"requester": version.Requester, "agreement_ref": version.AgreementRef,
		"declared_capabilities": version.DeclaredCapabilities, "data_classes": version.DataClasses,
		"redirect_endpoints": version.RedirectEndpoints, "callback_endpoints": version.CallbackEndpoints,
		"contact_ref": version.ContactRef, "legal_ref": version.LegalRef, "review": version.Review,
	}
	review, _ := json.Marshal(payload)
	db.Exec(t, `INSERT INTO partner_application_version_revision (row_id,application_id,version,revision,state,review,successor_version,digest) VALUES ($1,$2,$3,$4,$5,$6::jsonb,NULL,$7)`, uuid.New(), version.ApplicationID, version.Version, int64(version.Revision), version.State, review, version.Digest)
}

func processingReview(t *testing.T, version partnerapp.ApplicationVersion, refs []string) partnerapp.DataProcessingReview {
	t.Helper()
	controls := []partnerapp.ReviewItem{
		{Control: partnerapp.ControlDataMinimization, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:min"},
		{Control: partnerapp.ControlRetention, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:retention"},
		{Control: partnerapp.ControlSubProcessors, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:processors"},
		{Control: partnerapp.ControlBreachNotification, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:breach"},
		{Control: partnerapp.ControlEncryptionTransit, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:transit"},
		{Control: partnerapp.ControlEncryptionAtRest, Finding: partnerapp.FindingPass, EvidenceRef: "evidence:rest"},
	}
	review, err := partnerapp.NewSecurityReview(version, partnerapp.SecurityReviewRequest{
		Submitter: "submitter-1", OwnerRef: "owner:payroll", Reviewer: "reviewer-2", SignatureRef: "signature:review",
		ReviewedAt: testAt, ValidFor: 24 * time.Hour, ScopeRefs: refs, DestinationRefs: []string{"destination:payroll"},
		ProcessorRefs: []string{"processor:acme"}, ResidencyRefs: []string{"region:us"}, RetentionPolicyRef: "retention:30d",
		ThreatModelRef: "threat:payroll", VulnerabilityRef: "vulnerability:payroll", Items: controls,
	})
	if err != nil {
		t.Fatal(err)
	}
	return review
}

func installationFixture(t *testing.T, tenant uuid.UUID, version partnerapp.ApplicationVersion) (partnerapp.Installation, partnerapp.InstallationEvent, partnerapp.Installation, partnerapp.InstallationEvent, partnerapp.Installation, partnerapp.InstallationEvent, partnerapp.DataProcessingReview) {
	t.Helper()
	refs := []string{tenant.String(), "org:payroll", "population:pilot", "purpose:payroll-read", "field:salary", "payroll.read"}
	requested, event, err := partnerapp.RequestGrant(version, partnerapp.InstallationRequest{
		InstallationID: "installation-payroll", TenantScope: tenant.String(), OrganizationScope: "org:payroll", PopulationScope: "population:pilot",
		DataClasses: []string{"PAYROLL"}, FieldScopes: []string{"field:salary"}, Purpose: "purpose:payroll-read", Capabilities: []string{"payroll.read"},
		Requester: "requester-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, approvedEvent, err := requested.Approve("approver-1", "evidence:approval")
	if err != nil {
		t.Fatal(err)
	}
	review := processingReview(t, version, refs)
	active, activeEvent, err := approved.ActivateWithReview(review, testAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return requested, event, approved, approvedEvent, active, activeEvent, review
}

func TestTodo_PERSIST_COMMERCIAL_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "commercial-primary")
	app, version := applicationFixture(t)
	seedCatalog(t, db, app, version)
	store := commercialstore.New(appConn(t, db))
	c := contract(tenant, 1)
	if err := store.PutContractRevision(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutEntitlementSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetContractRevision(context.Background(), tenant.String(), c.ContractID, 1)
	if err != nil || got.Bound != c.Bound || got.Capabilities[0] != c.Capabilities[0] {
		t.Fatalf("contract=%+v err=%v", got, err)
	}
	gotSnapshot, err := store.GetEntitlementSnapshot(context.Background(), tenant.String(), c.ContractID, 1)
	if err != nil || gotSnapshot.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("snapshot=%q err=%v", gotSnapshot.Fingerprint(), err)
	}
	loadedApp, err := store.GetApplication(context.Background(), app.ApplicationID)
	if err != nil || loadedApp.ApplicationID != app.ApplicationID {
		t.Fatalf("application=%+v err=%v", loadedApp, err)
	}
	loadedVersion, err := store.GetApplicationVersion(context.Background(), version.ApplicationID, version.Version, version.Revision)
	if err != nil || loadedVersion.Digest != version.Digest {
		t.Fatalf("version=%+v err=%v", loadedVersion, err)
	}
	requested, requestedEvent, approved, approvedEvent, installation, event, review := installationFixture(t, tenant, version)
	for _, item := range []struct {
		installation partnerapp.Installation
		event        partnerapp.InstallationEvent
	}{{requested, requestedEvent}, {approved, approvedEvent}, {installation, event}} {
		if err := store.PutInstallation(context.Background(), tenant, item.installation, item.event); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PutDataProcessingReview(context.Background(), tenant, review); err != nil {
		t.Fatal(err)
	}
	loadedInstallation, err := store.GetInstallation(context.Background(), tenant, installation.InstallationID, installation.Revision)
	if err != nil || loadedInstallation.Digest != installation.Digest {
		t.Fatalf("installation=%+v err=%v", loadedInstallation, err)
	}
	if events, err := store.GetInstallationEvents(context.Background(), tenant, installation.InstallationID); err != nil || len(events) != 3 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestTodo_PERSIST_COMMERCIAL_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "commercial-fault")
	store := commercialstore.New(appConn(t, db))
	c := contract(tenant, 1)
	if err := store.PutContractRevision(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := store.PutContractRevision(context.Background(), c); !errors.Is(err, commercialstore.ErrDuplicate) || commercialstore.CodeDuplicateRevision != err.(*commercialstore.Error).Code {
		t.Fatalf("duplicate=%v", err)
	}
	gap := contract(tenant, 3)
	if err := store.PutContractRevision(context.Background(), gap, 1); !errors.Is(err, commercialstore.ErrVersionConflict) {
		t.Fatalf("stale CAS=%v", err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutEntitlementSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO entitlement_snapshot (tenant_id,row_id,contract_id,revision,fingerprint,frozen_at) VALUES ($1,$2,$3,1,$4,CURRENT_TIMESTAMP + interval '1 hour')`, tenant, uuid.New(), c.ContractID, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if _, err := store.GetEntitlementSnapshot(context.Background(), tenant.String(), c.ContractID, 1); !errors.Is(err, commercialstore.ErrFingerprintMismatch) {
		t.Fatalf("fingerprint fault=%v", err)
	}
}

func TestTodo_PERSIST_COMMERCIAL_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "commercial-integration")
	store := commercialstore.New(appConn(t, db))
	first := contract(tenant, 1)
	if err := store.PutContractRevision(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := contract(tenant, 2)
	second.Capabilities = append(second.Capabilities, "promotion.observe")
	if err := store.PutContractRevision(context.Background(), second, 1); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.ListContractRevisions(context.Background(), tenant.String(), first.ContractID)
	if err != nil || len(revisions) != 2 || revisions[1].Revision != 2 {
		t.Fatalf("revisions=%+v err=%v", revisions, err)
	}
}

func TestTodo_PERSIST_COMMERCIAL_001_Security(t *testing.T) {
	db := newDB(t)
	primary := insertTenant(t, db, "commercial-security-a")
	other := insertTenant(t, db, "commercial-security-b")
	store := commercialstore.New(appConn(t, db))
	c := contract(primary, 1)
	if err := store.PutContractRevision(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetContractRevision(context.Background(), other.String(), c.ContractID, 1); !errors.Is(err, commercialstore.ErrNotFound) {
		t.Fatalf("cross-tenant contract=%v", err)
	}
	if _, err := store.GetInstallation(context.Background(), other, "installation-payroll", 1); !errors.Is(err, commercialstore.ErrNotFound) {
		t.Fatalf("cross-tenant installation=%v", err)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM commercial_contract_revision WHERE tenant_id=$1`, primary).Scan(&count); err != nil || count != 1 {
		t.Fatalf("primary rows=%d err=%v", count, err)
	}
}

func TestTodo_PERSIST_COMMERCIAL_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "commercial-recovery")
	first := contract(tenant, 1)
	writer := commercialstore.New(appConn(t, db))
	if err := writer.PutContractRevision(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.PutEntitlementSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	fresh := commercialstore.New(appConn(t, db))
	got, err := fresh.GetEntitlementSnapshot(context.Background(), tenant.String(), first.ContractID, 1)
	if err != nil || got.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("recovered=%q err=%v", got.Fingerprint(), err)
	}
}

func TestTodo_REV_047_02_CommercialStoreSuspension(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "industry-pack-suspended-entitlement")
	store := commercialstore.New(appConn(t, db))
	c := contract(tenant, 1)
	c.Status = commercial.StatusSuspended
	c.Capabilities = []string{"hcmnext.industrypack.healthcare/v1"}
	if err := store.PutContractRevision(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutEntitlementSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetEntitlementSnapshot(context.Background(), tenant.String(), c.ContractID, 1)
	if err != nil || loaded.Status() != commercial.StatusSuspended || loaded.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("suspended snapshot=%+v status=%s err=%v", loaded.Contract(), loaded.Status(), err)
	}
	decision := loaded.Resolve(commercial.EntitlementRequest{TenantID: tenant.String(), Capability: c.Capabilities[0], At: testAt.Add(time.Hour)})
	if decision.Code != commercial.CodeContractSuspended {
		t.Fatalf("suspended entitlement decision=%s, want %s", decision.Code, commercial.CodeContractSuspended)
	}
}

type rev047PublicationEffects struct{ calls int }

func (s *rev047PublicationEffects) SavePublication(context.Context, string, industrypack.PublicationReport) (industrypack.ActivationEffects, error) {
	s.calls++
	return industrypack.ActivationEffects{AuthoritativeRows: 1}, nil
}

type rev047ActivationEffects struct{ calls int }

func (s *rev047ActivationEffects) SaveActivation(context.Context, industrypack.ActivationReceipt) (industrypack.ActivationEffects, error) {
	s.calls++
	return industrypack.ActivationEffects{AuthoritativeRows: 1, OutboxEntries: 1}, nil
}

func TestTodo_REV_047_02_CommercialStoreComposition(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	tenant := insertTenant(t, db, "industry-pack-entitlement-composition")
	commercialRepo := commercialstore.New(appConn(t, db))
	contractRevision := contract(tenant, 1)
	contractRevision.Capabilities = []string{industrypack.IndustryEntitlementCapability(industrypack.IndustryHealthcare)}
	if err := commercialRepo.PutContractRevision(ctx, contractRevision); err != nil {
		t.Fatal(err)
	}
	snapshot, err := commercial.NewEntitlementSnapshot(contractRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := commercialRepo.PutEntitlementSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	authority, err := industrypack.NewIndustryEntitlementAuthority(commercialRepo,
		industrypack.ContractRevisionResolverFunc(func(_ context.Context, got string) (industrypack.ContractRevisionRef, error) {
			if got != tenant.String() {
				return industrypack.ContractRevisionRef{}, commercial.ErrStoreNotFound
			}
			return industrypack.ContractRevisionRef{ContractID: contractRevision.ContractID, Revision: contractRevision.Revision}, nil
		}), industrypack.EntitlementClockFunc(func() time.Time { return testAt }), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := industrypack.NewIndustryPack(industrypack.IndustryPack{PackID: "healthcare-base", Version: 1, Industry: industrypack.IndustryHealthcare,
		Owner: "hcmnext", Scope: "healthcare-us", Support: "maintained",
		Compatibility: []industrypack.CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	publicationStore := &rev047PublicationEffects{}
	report, pubEffects, err := industrypack.PublishChecked(ctx, publicationStore, authority, tenant.String(), industrypack.PublicationCheck{
		TenantID: tenant.String(), Candidate: pack, Installed: map[string]string{"hcmnext": "1.8"},
	})
	if err != nil || publicationStore.calls != 1 || pubEffects.AuthoritativeRows != 1 || report.Entitlement.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("publication report=%+v effects=%+v calls=%d err=%v", report, pubEffects, publicationStore.calls, err)
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{23}, ed25519.SeedSize))
	target := industrypack.PackTarget{Tenant: tenant.String(), Cell: "cell-local"}
	envelope := industrypack.SignPackVersion(industrypack.SignedPackVersion{PackID: pack.PackID, Industry: pack.Industry, Version: pack.Version,
		BundleDigest: "sha256:healthcare-bundle", Target: target, EffectiveAt: testAt.Add(time.Minute), RollbackVersion: 0,
		Publisher: "publisher:one", Approver: "approver:two", SignedAt: testAt.Add(-time.Minute)}, "key-1", private)
	activationStore := &rev047ActivationEffects{}
	receipt, effects, err := industrypack.ActivateSigned(ctx, activationStore, authority, industrypack.ActivationRequest{Envelope: envelope,
		Target: target, TrustedKeys: map[string]ed25519.PublicKey{"key-1": private.Public().(ed25519.PublicKey)}})
	if err != nil || activationStore.calls != 1 || effects.AuthoritativeRows != 1 || receipt.Entitlement.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("activation receipt=%+v effects=%+v calls=%d err=%v", receipt, effects, activationStore.calls, err)
	}
}

func TestTodo_PERSIST_COMMERCIAL_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "commercial-mutation")
	store := commercialstore.New(appConn(t, db))
	first := contract(tenant, 1)
	if err := store.PutContractRevision(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	_, err := store.GetContractRevision(context.Background(), tenant.String(), first.ContractID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE commercial_contract_revision SET price_cents=1 WHERE tenant_id=$1 AND contract_id=$2 AND revision=1`,
		`DELETE FROM commercial_contract_revision WHERE tenant_id=$1 AND contract_id=$2 AND revision=1`,
	} {
		if err := db.ExecErr(statement, tenant, first.ContractID); err == nil {
			t.Fatalf("contract mutation succeeded: %s", statement)
		}
	}
	app, version := applicationFixture(t)
	seedCatalog(t, db, app, version)
	requested, requestedEvent, approved, approvedEvent, installation, event, _ := installationFixture(t, tenant, version)
	if err := store.PutInstallation(context.Background(), tenant, requested, requestedEvent); err != nil {
		t.Fatal(err)
	}
	if err := store.PutInstallation(context.Background(), tenant, approved, approvedEvent); err != nil {
		t.Fatal(err)
	}
	if err := store.PutInstallation(context.Background(), tenant, installation, event); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE partner_installation_event SET actor='tampered' WHERE tenant_id=$1 AND installation_id=$2 AND event_sequence=1`,
		`DELETE FROM partner_installation_event WHERE tenant_id=$1 AND installation_id=$2 AND event_sequence=1`,
	} {
		if err := db.ExecErr(statement, tenant, installation.InstallationID); err == nil {
			t.Fatalf("event mutation succeeded: %s", statement)
		}
	}
}
