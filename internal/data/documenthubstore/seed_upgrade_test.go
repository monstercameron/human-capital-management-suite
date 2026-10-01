package documenthubstore

import (
	"context"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestUpgradePristineSeed_ExactAndHistoryGuards(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	legacy := "# Legacy\n\nHarborCare field notes.\n"
	adapted := "# Field notes\n\nIronridge Builders jobsite notes.\n"
	doc, first, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Legacy", legacy)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: doc, OwnerID: "u-owner", LegacyTitle: "Legacy", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
	if err != nil || !upgraded {
		t.Fatalf("pristine upgrade=%v err=%v", upgraded, err)
	}
	expectedMarkdown := NormalizeMarkdown(adapted)
	var versionCount int
	var latestID string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", doc).Scan(&versionCount); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, "tenant-a", doc).Scan(&latestID)
	}); err != nil {
		t.Fatal(err)
	}
	if versionCount != 2 || latestID == first.ID {
		t.Fatalf("upgrade versions=%d latest=%q first=%q", versionCount, latestID, first.ID)
	}
	stored, err := s.ReadVersion(ctx, "tenant-a", doc, latestID, "person", "u-owner")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Field notes" || stored.Markdown != expectedMarkdown || stored.Hash != HashContent(expectedMarkdown) {
		t.Fatalf("persisted adapted version=%+v want title=%q markdown=%q hash=%q", stored, "Field notes", expectedMarkdown, HashContent(expectedMarkdown))
	}
	upgraded, err = s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: doc, OwnerID: "u-owner", LegacyTitle: "Legacy", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
	if err != nil || upgraded {
		t.Fatalf("idempotent upgrade=%v err=%v", upgraded, err)
	}
	wrongOwner, _, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Wrong owner", legacy)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err = s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: wrongOwner, OwnerID: "u-other", LegacyTitle: "Wrong owner", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
	if err != nil || upgraded {
		t.Fatalf("wrong owner upgrade=%v err=%v", upgraded, err)
	}
	withdrawn, firstWithdrawn, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Withdrawn", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id) VALUES('dep-seed-history','tenant-a',$1,$2,'default','','u-owner')`, withdrawn, firstWithdrawn.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	upgraded, err = s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: withdrawn, OwnerID: "u-owner", LegacyTitle: "Withdrawn", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
	if err != nil || upgraded {
		t.Fatalf("withdrawn-history upgrade=%v err=%v", upgraded, err)
	}
	wrongTenant, _, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Wrong tenant", legacy)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err = s.UpgradePristineSeed(ctx, "tenant-b", SeedUpgradeInput{DocumentID: wrongTenant, OwnerID: "u-owner", LegacyTitle: "Wrong tenant", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
	if err != nil || upgraded {
		t.Fatalf("wrong tenant upgrade=%v err=%v", upgraded, err)
	}
	for _, tc := range []struct {
		name, scope, scopeID string
	}{
		{name: "default pointer", scope: "default"},
		{name: "placement pointer", scope: "placement", scopeID: "team-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocked, firstBlocked, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", tc.name, legacy)
			if err != nil {
				t.Fatal(err)
			}
			deploymentID := "dep-seed-" + tc.scope
			if tc.scope == "placement" {
				deploymentID += "-placement"
			}
			if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
				if _, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id) VALUES($1,'tenant-a',$2,$3,$4,$5,'u-owner')`, deploymentID, blocked, firstBlocked.ID, tc.scope, tc.scopeID); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES('tenant-a',$1,$2,$3,$4,$5)`, blocked, tc.scope, tc.scopeID, deploymentID, firstBlocked.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			upgraded, err := s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: blocked, OwnerID: "u-owner", LegacyTitle: tc.name, LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Field notes", AdaptedMarkdown: NormalizeMarkdown(adapted)})
			if err != nil || upgraded {
				t.Fatalf("pointer upgrade=%v err=%v", upgraded, err)
			}
		})
	}
}

func TestUpgradePristineSeed_RejectsWrongCreator(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	legacy := "# Legacy\n"
	doc, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: doc, CreatorID: "u-imported", Title: "Legacy", Markdown: legacy}, ""); err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: doc, OwnerID: "u-owner", LegacyTitle: "Legacy", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Adapted", AdaptedMarkdown: "# Adapted\n"})
	if err != nil || upgraded {
		t.Fatalf("wrong creator upgrade=%v err=%v", upgraded, err)
	}
}

func TestUpgradePristineSeed_RejectsMalformedDeployedStatusWithoutHistory(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	legacy := "# Legacy\n"
	doc, first, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Legacy", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_version SET status='deployed' WHERE tenant_id=$1 AND id=$2`, "tenant-a", first.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{
		DocumentID: doc, OwnerID: "u-owner", LegacyTitle: "Legacy", LegacyHash: HashContent(NormalizeMarkdown(legacy)),
		AdaptedTitle: "Adapted", AdaptedMarkdown: "# Adapted\n",
	})
	if err != nil || upgraded {
		t.Fatalf("malformed deployed status upgrade=%v err=%v", upgraded, err)
	}
}

func TestUpgradePristineSeed_DeployWithdrawAndRace(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	legacy := "# Legacy\n"
	adapted := "# Adapted\n"

	withdrawn, first, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Withdrawn", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: withdrawn, VersionID: first.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionDeploy, ActionRetire} {
		if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: withdrawn, SubjectKind: "person", SubjectID: "u-owner", Action: action, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: withdrawn, VersionID: first.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-owner", ExpectedLive: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: withdrawn, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", ExpectedLive: first.ID, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.UpgradePristineSeed(ctx, "tenant-a", SeedUpgradeInput{DocumentID: withdrawn, OwnerID: "u-owner", LegacyTitle: "Withdrawn", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Adapted", AdaptedMarkdown: adapted})
	if err != nil || upgraded {
		t.Fatalf("deploy-withdraw upgrade=%v err=%v", upgraded, err)
	}

	doc, first, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Race", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: doc, VersionID: first.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: "u-owner", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	in := SeedUpgradeInput{DocumentID: doc, OwnerID: "u-owner", LegacyTitle: "Race", LegacyHash: HashContent(NormalizeMarkdown(legacy)), AdaptedTitle: "Adapted", AdaptedMarkdown: adapted}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var raceUpgraded bool
	var upgradeErr, deployErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		raceUpgraded, upgradeErr = s.UpgradePristineSeed(ctx, "tenant-a", in)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, deployErr = s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: doc, VersionID: first.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-owner", ExpectedLive: ""})
	}()
	close(start)
	wg.Wait()
	if upgradeErr != nil || deployErr != nil {
		t.Fatalf("race upgrade=%v err=%v deployErr=%v", raceUpgraded, upgradeErr, deployErr)
	}
	var versions, deployments, pointers int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", doc).Scan(&versions); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", doc).Scan(&deployments); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", doc).Scan(&pointers)
	}); err != nil {
		t.Fatal(err)
	}
	if deployments != 1 || pointers != 1 || (versions != 1 && versions != 2) || (raceUpgraded != (versions == 2)) {
		t.Fatalf("race final state versions=%d deployments=%d pointers=%d upgraded=%v", versions, deployments, pointers, raceUpgraded)
	}
}
