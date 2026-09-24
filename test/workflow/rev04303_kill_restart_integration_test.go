package workflow_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/configbundlekill"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/rolloutplan"
)

func TestTodo_REV_043_03_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'rollout-restart',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rollout-restart-"+tenantID.String(), "rollout restart")
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}

	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := configbundle.NewEd25519ReceiptSigner("rollout-restart", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	public := private.Public().(ed25519.PublicKey)
	issued := time.Now().UTC().Truncate(time.Second)
	killStore := configbundle.NewKillSwitchStore(signer, public)
	killStore.SetClock(func() time.Time { return issued })
	persistent := configbundlekill.New(conn, public)
	killStore.SetAppliedWriter(func(switchValue configbundle.SignedKillSwitch, receipt configbundle.AppliedKillSwitchReceipt) error {
		return persistent.PutApplied(ctx, switchValue, receipt)
	})

	plan := rolloutplan.Plan{
		Artifact:   rolloutplan.Artifact{Type: rolloutplan.ArtifactWorkflow, Version: "v1.2.0"},
		KillTarget: configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "promotion.execute"},
		Target:     "promotion.execute", Stages: []rolloutplan.Stage{
			{Name: "canary", HealthWindow: rolloutplan.HealthWindow{DurationSeconds: 600}},
			{Name: "broad", HealthWindow: rolloutplan.HealthWindow{DurationSeconds: 1800}},
		},
		StopCriteria: "security findings stop", ExpandCriteria: "healthy window expands",
		RollbackCriteria: "restore verified prior", KillCriteria: "applied CP-008 stop",
		Owner: "rollout-operator", Expiry: "2027-01-01T00:00:00Z",
	}
	compiled, err := rolloutplan.Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	cohorts, err := rolloutplan.FreezeCohorts(rolloutplan.CohortRequest{
		Plan: plan,
		Targets: []rolloutplan.StageTarget{
			{Stage: "canary", Tenants: []string{tenantID.String()}, Orgs: []string{"org:rollout"}, Residencies: []string{"US"}},
			{Stage: "broad", Tenants: []string{tenantID.String()}, Orgs: []string{"org:rollout"}, Residencies: []string{"US"}},
		},
		Population: population.Snapshot{DefinitionID: "pop/rollout", DefinitionDigest: "sha256:popdef", RevisionVersion: "1", SubjectIDs: []string{"subject:1"}, Digest: "sha256:popsnap"},
		Placements: []rolloutplan.MemberPlacement{{SubjectID: "subject:1", Tenant: tenantID.String(), Org: "org:rollout", Residency: "US"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cohortDigest := func(stage string) string {
		t.Helper()
		for _, cohort := range cohorts.Cohorts {
			if cohort.Stage == stage {
				return cohort.Digest
			}
		}
		t.Fatalf("missing %s cohort", stage)
		return ""
	}
	target := plan.KillTarget
	ledger := rolloutplan.NewActivationLedger(killStore, target)
	if _, err := ledger.Activate(rolloutplan.ActivationRequest{
		Plan: plan, Cohorts: cohorts, Stage: "canary", Artifact: plan.Artifact,
		PlanDigest: compiled.Digest, CohortDigest: cohortDigest("canary"), Epoch: 1,
	}); err != nil {
		t.Fatalf("initial canary activation: %v", err)
	}
	decision, err := rolloutplan.EvaluateHealth(plan, "canary", rolloutplan.Telemetry{
		PlanDigest: compiled.Digest, Stage: "canary", Source: "telemetry/canary",
		ObservedSeconds: 600, Requests: 1000, Complete: true,
	}, rolloutplan.Thresholds{})
	if err != nil {
		t.Fatal(err)
	}

	switchValue, err := killStore.Issue(configbundle.KillSwitchRequest{
		SwitchID: "rollout-restart", Target: target, Priority: 4, Reason: "stop promotion",
		IncidentRef: "incident/restart", Operator: "operator-a", Approver: "operator-b",
		EvidenceRef: "evidence/restart", IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), PropagationSLO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := killStore.Apply(switchValue); err != nil {
		t.Fatal(err)
	}

	// A restarted receiver has no process-local map state. Reload the durable
	// CP-008 record and give that exact store to rollout's expansion gate.
	restarted := configbundle.NewKillSwitchStore(signer, public)
	restarted.SetClock(func() time.Time { return issued.Add(time.Minute) })
	items, err := persistent.ListApplied(ctx, tenantID.String())
	if err != nil || len(items) != 1 {
		t.Fatalf("durable kill records=%d err=%v", len(items), err)
	}
	if err := restarted.RestoreApplied(items[0].Switch, items[0].Receipt); err != nil {
		t.Fatal(err)
	}
	// Guard the rollout against the durable store so another process applying
	// CP-008 uses the same database lock through stage advancement.
	restartedLedger := rolloutplan.NewActivationLedger(persistent, target)
	_, err = restartedLedger.Expand(rolloutplan.ExpansionRequest{
		Plan: plan, Cohorts: cohorts, Decision: decision, Artifact: plan.Artifact,
		CohortDigest: cohortDigest("broad"), Epoch: 2,
	})
	if !rolloutplan.HasActivationCode(err, rolloutplan.CapabilityKilled) {
		t.Fatalf("expand after restart = %v, want CAPABILITY_KILLED", err)
	}
	if _, err := restartedLedger.Explain("broad", ""); !rolloutplan.HasActivationCode(err, rolloutplan.UnknownActivationStage) {
		t.Fatalf("restart kill changed rollout stage: %v", err)
	}
}
