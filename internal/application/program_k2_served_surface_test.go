package application_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/program"
)

func servedProgramCatalog(t *testing.T) (*program.Catalog, program.Revision) {
	t.Helper()
	c := program.NewCatalog()
	caller := program.Caller{ID: "served-program-test", Tenants: []string{"tenant-acme"}}
	if err := c.RecordConformance(program.SignedConformance{
		TodoID: "PROGRAM-CONF-001", Digest: "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Signer: "conformance-board",
	}); err != nil {
		t.Fatalf("RecordConformance: %v", err)
	}
	definition, err := c.Define(caller, program.Definition{
		ID: "served-bonus", Name: "Served Bonus", Type: program.ProgramBonus,
		Owner: "total-rewards", Scope: []string{"org:acme"},
		Funding: program.FundingEmployer, Outcomes: []string{"payout"},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	revision, err := c.AppendRevision(caller, program.Revision{
		ProgramID: definition.ID, Version: 1, Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	return c, revision
}

func TestTodo_PROGRAM_005_Served(t *testing.T) {
	surface := application.NewServedProgramSurface()
	if surface.ContractID != program.ServingContractID || surface.NewCatalog == nil || surface.Calculate == nil {
		t.Fatal("application omitted served program outcome capability")
	}
	c, revision := servedProgramCatalog(t)
	c.RegisterFormula("served-bonus-v1", func(map[string]string) (string, error) { return "1250.50", nil })
	result, err := surface.Calculate(c, program.OutcomeInput{
		Participant: "worker-7", ProgramID: "served-bonus", RevisionDigest: revision.Digest,
		PopulationRef: "population:served", EligibilityRef: "eligibility:served", CycleRef: "cycle:served",
		FundingRef: "funding:served", FormulaID: "served-bonus-v1", FormulaVersion: "v1",
		Inputs: map[string]string{"base": "25000"}, At: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || result.Value != "1250.5" || result.Digest == "" {
		t.Fatalf("served outcome = %+v, err=%v", result, err)
	}
	if (&application.App{}).Program().Calculate == nil {
		t.Fatal("composed application omitted served program outcome capability")
	}
}

func TestTodo_PROGRAM_006_Served(t *testing.T) {
	surface := application.NewServedProgramSurface()
	if surface.Reconcile == nil || surface.SealReconciliation == nil {
		t.Fatal("application omitted served program reconciliation capability")
	}
	c := surface.NewCatalog()
	expected := []program.Expectation{{EnrollmentID: "enrollment-1", ExpectedState: string(program.EnrollmentEnrolled), ExpectedOutcomeDigest: "outcome-1"}}
	observed := []program.Observation{{EnrollmentID: "enrollment-1", Source: "payroll", ObservedState: string(program.EnrollmentEnrolled), ObservedOutcomeDigest: "outcome-1", At: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}}
	digest, err := surface.SealReconciliation(c, expected, observed)
	if err != nil || digest == "" {
		t.Fatalf("served reconciliation = %q, err=%v", digest, err)
	}
	observed[0].ObservedOutcomeDigest = "stale-outcome"
	if _, err := surface.SealReconciliation(c, expected, observed); err == nil {
		t.Fatal("served reconciliation accepted stale outcome")
	} else if rejection, ok := program.AsRejection(err); !ok || rejection.Code != program.CodeRejected006 {
		t.Fatalf("served stale reconciliation error = %v", err)
	}
	if (&application.App{}).Program().SealReconciliation == nil {
		t.Fatal("composed application omitted served reconciliation capability")
	}
}
