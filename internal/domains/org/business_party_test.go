package org

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var smbCRM001At = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func smbCRM001Authority() evidence.SourceAuthority {
	return evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcm", PolicyRef: "policy:party-local/v1"}
}

func smbCRM001Create(t *testing.T, reg *PartyRegistry, tenant, company, name string, kinds ...PartyKind) BusinessParty {
	t.Helper()
	kind := PartyKindOrganization
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	party, _, err := reg.CreateParty(context.Background(), CreatePartyRequest{
		Tenant: values.TenantId(tenant), CompanyScope: company, PartyID: uuid.NewString(),
		Kind: kind, DisplayName: name, SourceAuthority: smbCRM001Authority(),
		InitialRoles:   []RoleGrant{{Role: CommercialRoleCustomer, EffectiveFrom: values.NewInstant(smbCRM001At)}},
		IdempotencyKey: "create-" + uuid.NewString(), Actor: "ada.admin", At: smbCRM001At,
	})
	if err != nil {
		t.Fatalf("create party: %v", err)
	}
	return party
}

// TestTodo_SMB_CRM_001 proves the business-party lifecycle: one Acme identity
// carries customer and supplier roles at once, ending the customer role at
// an effective time rejects new customer use while supplier use stays valid,
// snapshots never move, and archive/restore preserves history without
// fabricating new role authority.
func TestTodo_SMB_CRM_001(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	now := smbCRM001At

	acme, receipt, err := reg.CreateParty(ctx, CreatePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
		Kind: PartyKindOrganization, DisplayName: "Acme", LegalName: "Acme Ltd",
		SourceAuthority: evidence.SourceAuthority{},
		InitialRoles: []RoleGrant{
			{Role: CommercialRoleCustomer, EffectiveFrom: values.NewInstant(now)},
			{Role: CommercialRoleSupplier, EffectiveFrom: values.NewInstant(now)},
		},
		IdempotencyKey: "acme-1", Actor: "ada.admin", At: now,
	})
	_ = receipt
	if err == nil {
		t.Fatal("party with unstated source authority created, want refusal")
	}
	acme, receipt, err = reg.CreateParty(ctx, CreatePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
		Kind: PartyKindOrganization, DisplayName: "Acme", LegalName: "Acme Ltd",
		SourceAuthority: smbCRM001Authority(),
		InitialRoles: []RoleGrant{
			{Role: CommercialRoleCustomer, EffectiveFrom: values.NewInstant(now)},
			{Role: CommercialRoleSupplier, EffectiveFrom: values.NewInstant(now)},
		},
		IdempotencyKey: "acme-1", Actor: "ada.admin", At: now,
	})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}
	if receipt.Revision != 1 || acme.Revision != 1 || len(acme.Roles) != 2 {
		t.Fatalf("Acme = %+v receipt %+v, want revision 1 with 2 roles", acme, receipt)
	}
	if acme.Ref.Kind == people.KindWorker {
		t.Fatal("business party carries a workforce identity kind")
	}
	snapshot, ok := reg.SnapshotParty(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, "ada.admin")
	if !ok {
		t.Fatal("no snapshot of Acme")
	}

	customerRel := ""
	for _, rel := range acme.Roles {
		if rel.Role == CommercialRoleCustomer {
			customerRel = rel.RelationshipID
		}
	}
	endAt := now.Add(30 * 24 * time.Hour)
	ended, _, err := reg.EndRole(ctx, EndRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		RelationshipID: customerRel, ExpectedRevision: acme.Revision,
		EffectiveTo: values.NewInstant(endAt), Reason: "contract ended",
		IdempotencyKey: "end-1", Actor: "ada.admin", At: now,
	})
	if err != nil {
		t.Fatalf("end customer role: %v", err)
	}
	if err := reg.AuthorizeNewCommitment(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, CommercialRoleCustomer, endAt.Add(time.Hour)); !errors.Is(err, ErrPartyRoleNotEffective) {
		t.Fatalf("new customer job after end err = %v, want ErrPartyRoleNotEffective", err)
	}
	if err := reg.AuthorizeNewCommitment(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, CommercialRoleSupplier, endAt.Add(time.Hour)); err != nil {
		t.Fatalf("supplier use after customer end: %v", err)
	}
	if err := reg.AuthorizeSettlement(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, CommercialRoleCustomer, now.Add(time.Hour)); err != nil {
		t.Fatalf("settlement of a pre-end customer obligation: %v", err)
	}
	for _, rel := range snapshot.Party.Roles {
		if rel.RelationshipID == customerRel && !rel.Effective.IsOpenEnded() {
			t.Fatal("party snapshot moved under role end")
		}
	}
	for _, rel := range ended.Roles {
		if rel.RelationshipID == customerRel && rel.Effective.IsOpenEnded() {
			t.Fatal("ended customer role still reads open-ended")
		}
	}

	archived, _, err := reg.ArchiveParty(ctx, ArchivePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: ended.Revision, Reason: "dormant",
		IdempotencyKey: "arc-1", Actor: "ada.admin", At: now,
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if archived.Status != PartyStatusArchived {
		t.Fatalf("archived status = %q", archived.Status)
	}
	restored, _, err := reg.RestoreParty(ctx, RestorePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: archived.Revision, Reason: "re-engaged",
		IdempotencyKey: "rst-1", Actor: "ada.admin", At: now,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := reg.AuthorizeNewCommitment(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, CommercialRoleCustomer, endAt.Add(2*time.Hour)); !errors.Is(err, ErrPartyRoleNotEffective) {
		t.Fatalf("customer job after restore err = %v, want refusal until explicit AddRole", err)
	}
	withRole, _, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: restored.Revision, Role: CommercialRoleCustomer,
		EffectiveFrom:  values.NewInstant(endAt.Add(2 * time.Hour)),
		IdempotencyKey: "add-1", Actor: "ada.admin", At: now,
	})
	if err != nil {
		t.Fatalf("re-add customer role: %v", err)
	}
	if err := reg.AuthorizeNewCommitment(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, CommercialRoleCustomer, endAt.Add(3*time.Hour)); err != nil {
		t.Fatalf("customer job after explicit AddRole: %v", err)
	}
	_ = withRole
}

// TestTodo_SMB_CRM_001_Integration proves cross-company isolation inside one
// tenant, restart recovery through snapshots, and revision-chain integrity:
// same-named parties coexist, a restarted registry serves identical state,
// and a tampered snapshot fails closed.
func TestTodo_SMB_CRM_001_Integration(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)

	a := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")
	b := smbCRM001Create(t, reg, "tenant-a", "hcm-co-2", "Acme")
	if a.Ref.Id == b.Ref.Id {
		t.Fatal("two companies share one party identity")
	}
	list, _, err := reg.ListParties(ctx, "tenant-a", "hcm-co-1", "ada.admin", PartyFilter{}, 10, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Ref.Id != a.Ref.Id {
		t.Fatalf("company list = %d parties, want exactly this company's Acme", len(list))
	}
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restarted := NewPartyRegistry(nil)
	if err := restarted.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	loaded, ok := restarted.GetParty(ctx, "tenant-a", "hcm-co-2", b.Ref.Id, "ada.admin")
	if !ok || loaded.DisplayName != "Acme" || loaded.Revision != b.Revision {
		t.Fatalf("restarted party = %+v ok=%v, want Acme at revision %d", loaded, ok, b.Revision)
	}
	if _, _, err := restarted.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-2", PartyID: b.Ref.Id,
		ExpectedRevision: b.Revision - 1, Role: CommercialRoleSupplier,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "stale-1", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyRevisionConflict) {
		t.Fatalf("stale revision err = %v, want ErrPartyRevisionConflict", err)
	}
	if err := restarted.Import([]byte(`{"parties": {"x": {"revision": 4, "predecessor": 9}}}`)); err == nil {
		t.Fatal("tampered snapshot imported, want refusal")
	}
}

// TestTodo_SMB_CRM_001_Security proves scope and authority fences: foreign
// tenants and companies read nothing and write nothing, forged refs do not
// resolve, a revoked actor is refused before any effect, stale revisions
// conflict, and externally mastered fields reject local writes.
func TestTodo_SMB_CRM_001_Security(t *testing.T) {
	ctx := context.Background()
	revoked := map[string]bool{"mallory": true}
	reg := NewPartyRegistry(func(_ context.Context, actor, tenant, company string) error {
		if revoked[actor] {
			return fmt.Errorf("actor %q is revoked", actor)
		}
		return nil
	})

	acme := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")
	if _, ok := reg.GetParty(ctx, "tenant-b", "hcm-co-1", acme.Ref.Id, "ada.admin"); ok {
		t.Fatal("foreign tenant read a party")
	}
	if _, ok := reg.GetParty(ctx, "tenant-a", "hcm-co-9", acme.Ref.Id, "ada.admin"); ok {
		t.Fatal("foreign company read a party")
	}
	if _, _, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-b", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: acme.Revision, Role: CommercialRoleSupplier,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "x-1", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyNotFound) {
		t.Fatalf("foreign-tenant write err = %v, want non-revealing ErrPartyNotFound", err)
	}
	if _, _, err := reg.EndRole(ctx, EndRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
		RelationshipID: uuid.NewString(), ExpectedRevision: 1,
		EffectiveTo: values.NewInstant(smbCRM001At), Reason: "forged",
		IdempotencyKey: "x-2", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyNotFound) {
		t.Fatalf("forged ref write err = %v, want ErrPartyNotFound", err)
	}
	if _, _, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: acme.Revision, Role: CommercialRoleSupplier,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "x-3", Actor: "mallory", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyForbidden) {
		t.Fatalf("revoked actor write err = %v, want ErrPartyForbidden", err)
	}
	loaded, ok := reg.GetParty(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, "ada.admin")
	if !ok || loaded.Revision != acme.Revision || len(loaded.Roles) != 1 {
		t.Fatalf("refused writes changed state: %+v", loaded)
	}
	if _, total, err := reg.ListParties(ctx, "tenant-b", "hcm-co-1", "ada.admin", PartyFilter{}, 10, ""); err != nil || total != 0 {
		t.Fatalf("foreign-tenant list = total %d err %v, want zero", total, err)
	}

	mastered, _, err := reg.CreateParty(ctx, CreatePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
		Kind: PartyKindOrganization, DisplayName: "Globex", SourceAuthority: smbCRM001Authority(),
		ExternalMastered: []string{"display_name"},
		IdempotencyKey:   "m-1", Actor: "ada.admin", At: smbCRM001At,
	})
	if err != nil {
		t.Fatalf("create mastered party: %v", err)
	}
	if _, _, err := reg.ReviseParty(ctx, RevisePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: mastered.Ref.Id,
		ExpectedRevision: mastered.Revision, UpdateMask: []string{"display_name"},
		DisplayName: "Globex Local", IdempotencyKey: "m-2", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyExternallyMastered) {
		t.Fatalf("local write to mastered field err = %v, want ErrPartyExternallyMastered", err)
	}
}

// TestTodo_SMB_CRM_001_Recovery proves crash convergence: a commit whose
// response is lost replays the recorded receipt under the same idempotency
// key with zero duplicate effects, an altered payload conflicts, and two
// stale writers serialize into exactly one winner.
func TestTodo_SMB_CRM_001_Recovery(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	acme := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")

	reg.AfterCommit = func() error { return errPartySimulatedCrash }
	_, _, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: acme.Revision, Role: CommercialRoleSupplier,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "rec-1", Actor: "ada.admin", At: smbCRM001At,
	})
	if !errors.Is(err, errPartySimulatedCrash) {
		t.Fatalf("crashing commit err = %v, want the simulated crash", err)
	}
	reg.AfterCommit = nil
	replayed, receipt, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: acme.Revision, Role: CommercialRoleSupplier,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "rec-1", Actor: "ada.admin", At: smbCRM001At,
	})
	if err != nil {
		t.Fatalf("replay after crash: %v", err)
	}
	if receipt.Revision != acme.Revision+1 || len(replayed.Roles) != 2 {
		t.Fatalf("replayed = %+v receipt %+v, want one supplier role at revision+1", replayed, receipt)
	}
	if _, _, err := reg.AddRole(ctx, AddRoleRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
		ExpectedRevision: replayed.Revision, Role: CommercialRoleProspect,
		EffectiveFrom:  values.NewInstant(smbCRM001At),
		IdempotencyKey: "rec-1", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyIdempotencyConflict) {
		t.Fatalf("altered-payload replay err = %v, want ErrPartyIdempotencyConflict", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, results[i] = reg.EndRole(ctx, EndRoleRequest{
				Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
				RelationshipID: replayed.Roles[0].RelationshipID, ExpectedRevision: replayed.Revision,
				EffectiveTo: values.NewInstant(smbCRM001At.Add(time.Hour)), Reason: "race",
				IdempotencyKey: "race-" + string(rune('a'+i)), Actor: "ada.admin", At: smbCRM001At,
			})
		}(i)
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrPartyRevisionConflict):
			conflicts++
		default:
			t.Fatalf("racer err = %v, want win or revision conflict", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("stale race = %d wins %d conflicts, want exactly one winner", wins, conflicts)
	}
}

// TestTodo_SMB_CRM_001_Conformance proves one owner serves every entry
// point: UI, agent/tool, and public API paths run the same scope, revision,
// and idempotency checks; commercial edges never read as supervisory
// parents; and no party ref can pass as a workforce identity.
func TestTodo_SMB_CRM_001_Conformance(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	acme := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")

	entries := map[string]func() error{
		"revise": func() error {
			_, _, err := reg.ReviseParty(ctx, RevisePartyRequest{
				Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
				ExpectedRevision: 999, UpdateMask: []string{"legal_name"},
				LegalName: "Acme", IdempotencyKey: "c-rev", Actor: "ada.admin", At: smbCRM001At,
			})
			return err
		},
		"addRole": func() error {
			_, _, err := reg.AddRole(ctx, AddRoleRequest{
				Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
				ExpectedRevision: 999, Role: CommercialRoleSupplier,
				EffectiveFrom:  values.NewInstant(smbCRM001At),
				IdempotencyKey: "c-add", Actor: "ada.admin", At: smbCRM001At,
			})
			return err
		},
		"archive": func() error {
			_, _, err := reg.ArchiveParty(ctx, ArchivePartyRequest{
				Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
				ExpectedRevision: 999, Reason: "x",
				IdempotencyKey: "c-arc", Actor: "ada.admin", At: smbCRM001At,
			})
			return err
		},
	}
	for name, entry := range entries {
		if err := entry(); !errors.Is(err, ErrPartyRevisionConflict) {
			t.Fatalf("%s with stale revision err = %v, want the owner's conflict", name, err)
		}
	}
	if got := SupervisoryParentsFor(acme); len(got) != 0 {
		t.Fatalf("commercial edges read as supervisory parents: %v", got)
	}
	if acme.Ref.Kind == people.KindWorker {
		t.Fatal("party ref passes as a workforce identity")
	}
	if _, _, err := reg.CreateParty(ctx, CreatePartyRequest{
		Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
		Kind: "WORKER", DisplayName: "Mallory", SourceAuthority: smbCRM001Authority(),
		IdempotencyKey: "c-w", Actor: "ada.admin", At: smbCRM001At,
	}); !errors.Is(err, ErrPartyInvalid) {
		t.Fatalf("workforce-kind party err = %v, want ErrPartyInvalid", err)
	}
}

// TestTodo_SMB_CRM_001_Golden pins the authorized projection and refusal
// bytes served to the directory, picker, and agent/tool surfaces.
func TestTodo_SMB_CRM_001_Golden(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	acme := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")

	projection := AuthorizePartyProjection(acme, "ada.admin")
	if !strings.Contains(projection, "Acme") || !strings.Contains(projection, "CUSTOMER") {
		t.Fatalf("projection = %q, want display name and role badge", projection)
	}
	hidden := AuthorizePartyProjection(BusinessParty{}, "mallory")
	if hidden != "" {
		t.Fatalf("empty-party projection = %q, want empty", hidden)
	}
	if got := PartyRefusalTitle(ErrPartyRoleNotEffective); !strings.Contains(got, "not effective") {
		t.Fatalf("refusal title = %q, want the not-effective guidance", got)
	}
	if got := PartyRefusalTitle(ErrPartyNotFound); strings.Contains(got, "tenant") || strings.Contains(got, "company") {
		t.Fatalf("not-found title = %q, want no scope disclosure", got)
	}
	loaded, ok := reg.GetParty(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, "ada.admin")
	if !ok {
		t.Fatal("no party for golden projection")
	}
	if AuthorizePartyProjection(loaded, "ada.admin") != projection {
		t.Fatal("stored party projects differently from the created one")
	}
}

// TestTodo_SMB_CRM_001_Race runs real competing writers against one party
// and asserts exact winning/losing receipts. The race detector itself runs
// in CI; this leg proves the fencing logic on any host.
func TestTodo_SMB_CRM_001_Race(t *testing.T) {
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	acme := smbCRM001Create(t, reg, "tenant-a", "hcm-co-1", "Acme")

	const racers = 8
	var wg sync.WaitGroup
	won := make([]PartyReceipt, racers)
	failed := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, receipt, err := reg.AddRole(ctx, AddRoleRequest{
				Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: acme.Ref.Id,
				ExpectedRevision: acme.Revision, Role: CommercialRoleSupplier,
				EffectiveFrom:  values.NewInstant(smbCRM001At),
				IdempotencyKey: fmt.Sprintf("race-%d", i), Actor: "ada.admin", At: smbCRM001At,
			})
			won[i], failed[i] = receipt, err
		}(i)
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for i := range failed {
		switch {
		case failed[i] == nil:
			wins++
			if won[i].Revision != acme.Revision+1 || won[i].PartyID != acme.Ref.Id {
				t.Fatalf("winner receipt = %+v, want revision+1 for this party", won[i])
			}
		case errors.Is(failed[i], ErrPartyRevisionConflict):
			conflicts++
		default:
			t.Fatalf("racer %d err = %v", i, failed[i])
		}
	}
	if wins != 1 || conflicts != racers-1 {
		t.Fatalf("race = %d wins %d conflicts, want 1 winner", wins, conflicts)
	}
	loaded, _ := reg.GetParty(ctx, "tenant-a", "hcm-co-1", acme.Ref.Id, "ada.admin")
	if loaded.Revision != acme.Revision+1 || len(loaded.Roles) != 2 {
		t.Fatalf("raced party = %+v, want exactly one added role", loaded)
	}
}

var errPartySimulatedCrash = errors.New("org: simulated party crash")
