package lineageconformance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	lc "github.com/monstercameron/human-capital-management-suite/tools/planning/lineageconformance"
)

func TestTodo_REV_057_02(t *testing.T) {
	in, err := lc.LoadInput(repoRoot(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	report, err := lc.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	promotion := caseResult(t, report, lc.PromotionDefinition)
	if linkState(promotion, lc.LinkReconciliation) != lc.StateUnknown {
		t.Fatalf("metadata-only Promotion reconciliation = %s, want UNKNOWN without a persisted job", linkState(promotion, lc.LinkReconciliation))
	}
	if reportErr := lc.VerifyReport(report); reportErr != nil {
		t.Fatalf("report with RECON-001 producer does not verify: %v", reportErr)
	}
}

func reconciliationEvidence(result lc.CaseResult) string {
	for _, link := range result.Links {
		if link.Link == lc.LinkReconciliation && len(link.Evidence) > 0 {
			return link.Evidence[0]
		}
	}
	return ""
}

func TestTodo_REV_057_02_Conformance(t *testing.T) {
	producers, err := lc.DefaultProducers()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range producers {
		if p.ID != "RECON-001/effect_reconciliation_job" {
			continue
		}
		found = true
		if p.Todo != "RECON-001" || p.Package != "internal/operations/reconcile" || p.Case != lc.PromotionDefinition ||
			len(p.Links) != 1 || p.Links[0] != lc.LinkReconciliation || len(p.Tests) == 0 {
			t.Fatalf("reconciliation producer is not bound to the durable owner: %+v", p)
		}
	}
	if !found {
		t.Fatal("DefaultProducers has no RECON-001 source for Promotion reconciliation")
	}
	input, err := lc.LoadInput(repoRoot(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range producers {
		if p.ID == "RECON-001/effect_reconciliation_job" {
			if err := lc.ValidateProducer(p, input.Todos, input.TestExists); err != nil {
				t.Fatalf("producer does not validate against closed RECON-001 evidence: %v", err)
			}
		}
	}
}

func TestTodo_REV_057_02_Golden(t *testing.T) {
	tenant := uuid.MustParse("2d91b969-5ef3-4d56-9ae7-c29f087e8a3e")
	job := reconcile.Job{TenantID: tenant, EffectRef: "promotion.effect:evt-1", PolicyRef: "policy/a", Status: reconcile.StatusPass, Version: 2}
	record, err := lc.ReconciliationRecord(lc.PromotionDefinition, tenant, job.EffectRef, []reconcile.Job{job})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if got := hex.EncodeToString(digest[:]); got != "3e226fc95ddaaef7759dc5e10f9e29e48050bb7aac81eb1bccc06ad0d8cf112b" {
		t.Fatalf("reconciliation lineage bytes changed: sha256 %s\n%s", got, raw)
	}
}

func TestTodo_REV_057_02_Fault(t *testing.T) {
	tenant := uuid.New()
	valid := reconcile.Job{TenantID: tenant, EffectRef: "effect/1", PolicyRef: "policy/a", Status: reconcile.StatusUnknown, Version: 1}
	if _, err := lc.ReconciliationRecord(lc.PromotionDefinition, tenant, valid.EffectRef, []reconcile.Job{valid}); err != nil {
		t.Fatalf("valid UNKNOWN job was refused: %v", err)
	}
	wrongTenant := valid
	wrongTenant.TenantID = uuid.New()
	if _, err := lc.ReconciliationRecord(lc.PromotionDefinition, tenant, valid.EffectRef, []reconcile.Job{wrongTenant}); err == nil {
		t.Fatal("cross-tenant reconciliation row was published")
	}
	unknownState := valid
	unknownState.Status = "MADE_UP"
	if _, err := lc.ReconciliationRecord(lc.PromotionDefinition, tenant, valid.EffectRef, []reconcile.Job{unknownState}); err == nil {
		t.Fatal("unrecognized persisted job state was published")
	}
	if _, err := lc.ReconciliationRecord(lc.PromotionDefinition, tenant, valid.EffectRef, nil); err == nil {
		t.Fatal("empty reconciliation evidence produced a record")
	}
	store := newPromotionStore(t)
	input, err := lc.LoadInput(repoRoot(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	var missingReport lc.Report
	store.inTx(t, func(tx dbport.Tx) error {
		loaded, loadErr := lc.WithReconciliationRecords(context.Background(), tx, input, store.tenant,
			map[string]string{lc.PromotionDefinition: "effect/with/no/job"})
		if loadErr != nil {
			return loadErr
		}
		missingReport, loadErr = lc.Compile(loaded)
		return loadErr
	})
	missing := caseResult(t, missingReport, lc.PromotionDefinition)
	if linkState(missing, lc.LinkReconciliation) != lc.StateUnknown || !hasFinding(missing, lc.CodeLinkMissing, lc.LinkReconciliation) {
		t.Fatalf("missing durable row compiled as %+v; wanted UNKNOWN with LINK_MISSING", missing)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, found, err := lc.ReadReconciliationRecord(ctx, store.db.Conn, store.tenant, lc.PromotionDefinition, valid.EffectRef)
	if err == nil || found || errors.Is(err, reconcile.ErrNotFound) {
		t.Fatalf("cancelled durable read returned found=%t err=%v, want storage failure", found, err)
	}
}
