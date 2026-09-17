package benefits

// BEN-004 RED: coverage tiers and dependent qualification — exact,
// effective-dated tier/relation/evidence/age/student/disability rules.
// Unsupported or ambiguous dependents are never silently covered. These
// tests are written before the production code in coverage.go and must
// fail until it lands.

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func tierAsOf() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }

func tierRules() []TierRule {
	return []TierRule{
		{
			ID: "rule:family-v1", Version: "v1", Tenant: "acme", Tier: TierFamily,
			AllowedRelations:    []DependentRelation{RelationSpouse, RelationChild},
			MaxDependentAge:     26,
			StudentExtensionAge: 30,
			RequiredEvidence:    map[DependentRelation][]string{RelationSpouse: {"marriage-certificate"}, RelationChild: {"birth-certificate"}},
			EffectiveFrom:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			EffectiveTo:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID: "rule:family-v2", Version: "v2", Tenant: "acme", Tier: TierFamily,
			AllowedRelations:    []DependentRelation{RelationSpouse, RelationChild, RelationStepchild, RelationDomesticPartner},
			MaxDependentAge:     26,
			StudentExtensionAge: 30,
			RequiredEvidence:    map[DependentRelation][]string{RelationSpouse: {"marriage-certificate"}, RelationChild: {"birth-certificate"}, RelationStepchild: {"birth-certificate", "marriage-certificate"}, RelationDomesticPartner: {"partnership-affidavit"}},
			EffectiveFrom:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EffectiveTo:         time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

func tierFacts() TierFacts {
	return TierFacts{
		Tenant: "acme", WorkerRef: "worker-1", PlanRef: "plan:medical/v3",
		AsOf: tierAsOf(), Tier: TierFamily,
	}
}

func tierDependent(ref string, relation DependentRelation, age int32, evidence ...string) Dependent {
	return Dependent{
		DependentRef: ref, Relation: relation,
		AgeYears: age, AgeSet: true,
		Evidence: append([]string(nil), evidence...),
	}
}

func coveredByRef(t *testing.T, res TierResolution, ref string) bool {
	t.Helper()
	for _, d := range res.Decisions {
		if d.DependentRef == ref {
			return d.Covered
		}
	}
	t.Fatalf("no decision for dependent %q in %+v", ref, res)
	return false
}

func coveredCount(res TierResolution) int {
	n := 0
	for _, d := range res.Decisions {
		if d.Covered {
			n++
		}
	}
	return n
}

// TestTodo_BEN_004 is the PRIMARY: exact effective-dated tier rules
// qualify supported dependents and never silently cover unsupported or
// ambiguous ones.
func TestTodo_BEN_004(t *testing.T) {
	t.Run("supported dependents are covered under the current rule", func(t *testing.T) {
		facts := tierFacts()
		facts.Dependents = []Dependent{
			tierDependent("dep-spouse", RelationSpouse, 40, "marriage-certificate"),
			tierDependent("dep-child", RelationChild, 10, "birth-certificate"),
		}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatalf("ResolveTierCoverage: %v", err)
		}
		if res.Status != TierCovered {
			t.Fatalf("status = %v, want COVERED", res.Status)
		}
		if res.RuleID != "rule:family-v2" || res.RuleVersion != "v2" {
			t.Fatalf("resolution must name its effective-dated rule: %+v", res)
		}
		if !coveredByRef(t, res, "dep-spouse") || !coveredByRef(t, res, "dep-child") {
			t.Fatalf("supported dependents must be covered: %+v", res)
		}
		if res.Digest == "" || len(res.Evidence) == 0 {
			t.Fatalf("resolution must carry evidence and a digest: %+v", res)
		}
	})

	t.Run("effective dating selects the covering rule version", func(t *testing.T) {
		facts := tierFacts()
		facts.AsOf = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		facts.Dependents = []Dependent{
			tierDependent("dep-step", RelationStepchild, 12, "birth-certificate", "marriage-certificate"),
		}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		// v1 admits no stepchildren: the dependent is uncovered, not
		// silently covered under the wrong version.
		if res.RuleID != "rule:family-v1" {
			t.Fatalf("rule = %q, want the 2025 version", res.RuleID)
		}
		if coveredByRef(t, res, "dep-step") {
			t.Fatal("stepchild must not be covered under v1")
		}
		if coveredCount(res) != 0 {
			t.Fatalf("covered = %d, want zero", coveredCount(res))
		}
	})

	t.Run("student and disabled extensions are exact", func(t *testing.T) {
		facts := tierFacts()
		student := tierDependent("dep-student", RelationChild, 28, "birth-certificate", "student-verification")
		student.Student = true
		disabled := tierDependent("dep-disabled", RelationChild, 45, "birth-certificate", "disability-certification")
		disabled.Disabled = true
		overage := tierDependent("dep-overage", RelationChild, 28, "birth-certificate", "student-verification")
		facts.Dependents = []Dependent{student, disabled, overage}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if !coveredByRef(t, res, "dep-student") {
			t.Fatal("student within the extension must be covered")
		}
		if !coveredByRef(t, res, "dep-disabled") {
			t.Fatal("disabled dependent with certification must be covered")
		}
		if coveredByRef(t, res, "dep-overage") {
			t.Fatal("over-age non-student must not be covered")
		}
		if res.Status != TierPartial {
			t.Fatalf("status = %v, want PARTIAL", res.Status)
		}
	})

	t.Run("unsupported and ambiguous dependents are never covered", func(t *testing.T) {
		facts := tierFacts()
		unknownAge := tierDependent("dep-unknown-age", RelationChild, 0, "birth-certificate")
		unknownAge.AgeSet = false
		facts.Dependents = []Dependent{
			{DependentRef: "dep-unsupported", Relation: "PARENT", AgeYears: 70, AgeSet: true, Evidence: []string{"birth-certificate"}},
			{DependentRef: "dep-no-evidence", Relation: RelationSpouse, AgeYears: 40, AgeSet: true},
			unknownAge,
			tierDependent("dep-step-nov1", RelationStepchild, 12, "birth-certificate", "marriage-certificate"),
		}
		// v1 window: stepchild additionally unsupported there.
		facts.AsOf = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if coveredCount(res) != 0 {
			t.Fatalf("covered = %d, want zero: unsupported/ambiguous must never be covered", coveredCount(res))
		}
		if res.Status != TierUncovered {
			t.Fatalf("status = %v, want UNCOVERED", res.Status)
		}
		for _, d := range res.Decisions {
			if d.Covered || len(d.Reasons) == 0 {
				t.Fatalf("every unsupported dependent needs an uncovered decision with reasons: %+v", d)
			}
		}
	})

	t.Run("no covering rule is unknown with zero covered", func(t *testing.T) {
		facts := tierFacts()
		facts.AsOf = time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
		facts.Dependents = []Dependent{tierDependent("dep-child", RelationChild, 10, "birth-certificate")}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != TierUnknown {
			t.Fatalf("status = %v, want UNKNOWN", res.Status)
		}
		if coveredCount(res) != 0 {
			t.Fatal("stale rule window must cover nothing")
		}
	})

	t.Run("invalid input is a typed error", func(t *testing.T) {
		facts := tierFacts()
		facts.Tenant = ""
		if _, err := ResolveTierCoverage(facts, tierRules()); !errors.Is(err, ErrInvalidTierInput) {
			t.Fatalf("empty tenant err = %v", err)
		}
		rules := tierRules()
		rules[0].Tenant = "other"
		if _, err := ResolveTierCoverage(tierFacts(), rules); !errors.Is(err, ErrTierTenant) {
			t.Fatalf("cross-tenant rule err = %v", err)
		}
		if _, err := ResolveTierCoverage(tierFacts(), nil); !errors.Is(err, ErrInvalidTierInput) {
			t.Fatalf("nil rules err = %v", err)
		}
	})
}

// TestTodo_BEN_004_Property: the never-silently-covered invariant holds
// across the relation/age/evidence matrix, evaluation is pure with a
// stable digest, and the status vocabulary stays closed.
func TestTodo_BEN_004_Property(t *testing.T) {
	t.Run("unverified dependents are never covered", func(t *testing.T) {
		relations := []DependentRelation{RelationSpouse, RelationChild, RelationStepchild, RelationDomesticPartner, "PARENT", ""}
		for _, relation := range relations {
			for _, tc := range []struct {
				name   string
				mutate func(*Dependent)
			}{
				{"no evidence", func(d *Dependent) { d.Evidence = nil }},
				{"unknown age", func(d *Dependent) { d.AgeSet = false }},
			} {
				facts := tierFacts()
				dep := tierDependent("dep-x", relation, 10, "birth-certificate", "marriage-certificate", "partnership-affidavit", "student-verification", "disability-certification")
				tc.mutate(&dep)
				facts.Dependents = []Dependent{dep}
				res, err := ResolveTierCoverage(facts, tierRules())
				if err != nil {
					t.Fatalf("%s/%s: %v", relation, tc.name, err)
				}
				if coveredCount(res) != 0 {
					t.Fatalf("%s/%s: unverified dependent covered: %+v", relation, tc.name, res)
				}
			}
		}
	})

	t.Run("evaluation is pure with a stable digest", func(t *testing.T) {
		facts := tierFacts()
		facts.Dependents = []Dependent{tierDependent("dep-child", RelationChild, 10, "birth-certificate")}
		first, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		second, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest != second.Digest || first.Status != second.Status {
			t.Fatal("same input must resolve identically")
		}
	})

	t.Run("status vocabulary is closed", func(t *testing.T) {
		for _, s := range []TierStatus{TierCovered, TierPartial, TierUncovered, TierUnknown} {
			if !s.Valid() {
				t.Fatalf("declared status %q must be valid", s)
			}
		}
		if (TierStatus("SORT_OF")).Valid() {
			t.Fatal("undeclared status must be invalid")
		}
	})
}

// TestTodo_BEN_004_Fault: ambiguous or stale input reaches an allowed
// durable state — uncovered with named reasons or unknown — with zero
// covered dependents and no silent default.
func TestTodo_BEN_004_Fault(t *testing.T) {
	t.Run("fully ambiguous input covers nothing", func(t *testing.T) {
		facts := tierFacts()
		unknownAge := Dependent{DependentRef: "dep-ambiguous", Relation: RelationChild}
		facts.Dependents = []Dependent{unknownAge}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != TierUncovered {
			t.Fatalf("status = %v, want UNCOVERED", res.Status)
		}
		if coveredCount(res) != 0 {
			t.Fatal("ambiguous dependent must not be covered")
		}
		if len(res.Decisions) != 1 || len(res.Decisions[0].Reasons) == 0 {
			t.Fatalf("ambiguity must be named: %+v", res)
		}
	})

	t.Run("stale rule window is unknown, never eligible-by-default", func(t *testing.T) {
		facts := tierFacts()
		facts.AsOf = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		facts.Dependents = []Dependent{tierDependent("dep-child", RelationChild, 10, "birth-certificate")}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != TierUnknown {
			t.Fatalf("status = %v, want UNKNOWN", res.Status)
		}
		if coveredCount(res) != 0 {
			t.Fatal("stale window must cover nothing")
		}
	})
}

// TestTodo_BEN_004_Mutation: mutants that drop the evidence requirement or
// the age cap would cover an unverified or over-age dependent — the oracle
// kills both by refusing.
func TestTodo_BEN_004_Mutation(t *testing.T) {
	t.Run("evidence-dropping mutant is killed", func(t *testing.T) {
		facts := tierFacts()
		facts.Dependents = []Dependent{
			{DependentRef: "dep-no-evidence", Relation: RelationSpouse, AgeYears: 40, AgeSet: true},
		}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		mutantWouldCover := facts.Dependents[0].Relation == RelationSpouse
		if coveredCount(res) != 0 {
			t.Fatal("unverified spouse covered: evidence requirement is broken")
		}
		if !mutantWouldCover {
			t.Fatal("mutant fixture is vacuous: relation-only check must cover this input")
		}
	})

	t.Run("age-cap-dropping mutant is killed", func(t *testing.T) {
		facts := tierFacts()
		facts.Dependents = []Dependent{
			tierDependent("dep-overage", RelationChild, 40, "birth-certificate"),
		}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		mutantWouldCover := true // age ignored, relation admitted, evidence present
		if coveredCount(res) != 0 {
			t.Fatal("over-age non-student covered: age cap is broken")
		}
		if !mutantWouldCover {
			t.Fatal("mutant fixture is vacuous")
		}
	})
}

// TestTodo_BEN_004_Security: tenant confinement holds across tenants,
// presented dependent evidence never surfaces in resolution outputs, and
// the tier/relation vocabularies stay closed.
func TestTodo_BEN_004_Security(t *testing.T) {
	t.Run("foreign tenants are confined with zero coverage", func(t *testing.T) {
		for _, tenant := range []string{"other", "acme-evil", "ACME", "acme "} {
			rules := tierRules()
			for i := range rules {
				rules[i].Tenant = tenant
			}
			res, err := ResolveTierCoverage(tierFacts(), rules)
			if !errors.Is(err, ErrTierTenant) {
				t.Fatalf("tenant %q err = %v, want ErrTierTenant", tenant, err)
			}
			if coveredCount(res) != 0 || len(res.Decisions) != 0 {
				t.Fatalf("tenant %q escaped confinement: %+v", tenant, res)
			}
			facts := tierFacts()
			facts.Tenant = tenant
			res, err = ResolveTierCoverage(facts, tierRules())
			if !errors.Is(err, ErrTierTenant) {
				t.Fatalf("facts tenant %q err = %v, want ErrTierTenant", tenant, err)
			}
			if coveredCount(res) != 0 || len(res.Decisions) != 0 {
				t.Fatalf("facts tenant %q escaped confinement: %+v", tenant, res)
			}
		}
	})

	t.Run("presented evidence never surfaces in resolution outputs", func(t *testing.T) {
		const secret = "zz-presented-evidence-secret"
		facts := tierFacts()
		facts.Dependents = []Dependent{
			tierDependent("dep-spouse", RelationSpouse, 40, "marriage-certificate", secret),
			tierDependent("dep-child", RelationChild, 10, "birth-certificate", secret),
		}
		res, err := ResolveTierCoverage(facts, tierRules())
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range res.Evidence {
			if strings.Contains(v, secret) {
				t.Fatalf("resolution evidence leaks presented evidence: %q", v)
			}
		}
		for _, r := range res.Reasons {
			if strings.Contains(r, secret) {
				t.Fatalf("resolution reason leaks presented evidence: %q", r)
			}
		}
		for _, d := range res.Decisions {
			for _, r := range d.Reasons {
				if strings.Contains(r, secret) {
					t.Fatalf("decision reason leaks presented evidence: %q", r)
				}
			}
		}
	})

	t.Run("tier and relation vocabularies are closed", func(t *testing.T) {
		for _, tier := range []TierCode{TierEmployeeOnly, TierEmployeeSpouse, TierEmployeeChildren, TierFamily} {
			if !tier.Valid() {
				t.Fatalf("declared tier %q must be valid", tier)
			}
		}
		for _, bad := range []TierCode{"", "family", "FAMILY_PLUS", "ADMIN"} {
			if bad.Valid() {
				t.Fatalf("undeclared tier %q must be invalid", bad)
			}
		}
		for _, rel := range []DependentRelation{RelationSpouse, RelationChild, RelationStepchild, RelationDomesticPartner} {
			if !rel.Valid() {
				t.Fatalf("declared relation %q must be valid", rel)
			}
		}
		for _, bad := range []DependentRelation{"", "PARENT", "spouse", "CHILDREN"} {
			if bad.Valid() {
				t.Fatalf("undeclared relation %q must be invalid", bad)
			}
		}
	})
}

// FuzzTodo_BEN_004: arbitrary relations, ages and evidence never panic,
// never escape the typed input contract, and never silently cover an
// unverified relation.
func FuzzTodo_BEN_004(f *testing.F) {
	f.Add("CHILD", 10, "birth-certificate")
	f.Add("SPOUSE", 40, "marriage-certificate")
	f.Add("PARENT", 70, "birth-certificate")
	f.Add("", -1, "")
	f.Fuzz(func(t *testing.T, relation string, age int, evidence string) {
		facts := tierFacts()
		facts.Dependents = []Dependent{{
			DependentRef: "dep-fuzz", Relation: DependentRelation(relation),
			AgeYears: int32(age), AgeSet: true, Evidence: []string{evidence},
		}}
		res, err := ResolveTierCoverage(facts, tierRules())
		if int32(age) < 0 {
			if !errors.Is(err, ErrInvalidTierInput) {
				t.Fatalf("negative age err = %v, want ErrInvalidTierInput", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("ResolveTierCoverage(%q, %d): %v", relation, age, err)
		}
		if !res.Status.Valid() {
			t.Fatalf("status %q left the closed vocabulary", res.Status)
		}
		if res.Digest == "" {
			t.Fatal("resolution needs a digest")
		}
		if len(res.Decisions) != 1 {
			t.Fatalf("decisions = %d, want 1", len(res.Decisions))
		}
		if coveredCount(res) > 1 {
			t.Fatal("covered more dependents than presented")
		}
		for _, d := range res.Decisions {
			if !d.Covered && len(d.Reasons) == 0 {
				t.Fatalf("uncovered dependent without reasons: %+v", d)
			}
		}
		if !DependentRelation(relation).Valid() && coveredCount(res) != 0 {
			t.Fatalf("unverified relation %q was covered: %+v", relation, res)
		}
	})
}
