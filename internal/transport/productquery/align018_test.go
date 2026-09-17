package productquery_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/queryenvelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func compileScope(t *testing.T) authz.RepositoryScope {
	t.Helper()
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	s, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
		Principal:   p,
		Purpose:     authz.PurposeCompensationReview,
		EffectiveAt: testEffective,
		Tenant:      values.TenantId("acme"),
		Candidates: []authz.ScopeInput{{
			Subject:     subject("acme", "00000000-0000-4000-8000-000000000001"),
			EffectiveAt: testEffective,
		}},
		Fields: []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func compileEnvelope(t *testing.T) queryenvelope.Envelope {
	t.Helper()
	env, err := queryenvelope.New(queryenvelope.Request{
		SliceID: "promotion", SliceVersion: 1, QueryID: "promotion.worker.list", Resource: "worker",
		ReadAt: testEffective, MaxRows: 50, Scope: compileScope(t),
		Freshness: queryenvelope.Freshness{State: queryenvelope.FreshnessCurrent, SourceVersion: "ledger:v1", Watermark: "42"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func compiledFixtureEnvelope() queryenvelope.Envelope {
	return queryenvelope.Envelope{
		SchemaVersion: 1,
		SliceID:       "promotion",
		SliceVersion:  1,
		QueryID:       "promotion.worker.list",
		Resource:      "worker",
		Tenant:        values.TenantId("acme"),
		Purpose:       authz.PurposeCompensationReview,
		ReadAt:        testEffective,
		MaxRows:       50,
		Subjects:      []values.EntityRef{{Tenant: values.TenantId("acme"), Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}},
		Fields: []queryenvelope.FieldDisposition{
			{Field: authz.FieldWorkerNumber, Effect: authz.EffectAllow, RuleID: "rule-1"},
			{Field: authz.FieldBaseSalary, Effect: authz.EffectDenied, RuleID: "rule-2"},
			{Field: authz.FieldBankAccountNumber, Effect: authz.EffectRedacted, RuleID: "rule-3", Obligations: []string{"mask-all-but-last-4"}},
		},
		PolicyVersion: "authz.p1a.bootstrap.v2",
		EvidenceID:    "evidence-1",
		Freshness:     queryenvelope.Freshness{State: queryenvelope.FreshnessCurrent, SourceVersion: "ledger:v1", Watermark: "42"},
	}
}

// TestTodo_ALIGN_018 proves field dispositions compile into one bounded,
// tenant-pinned product query: allowed fields become reads, redacted fields
// become metadata-only fetches, and denied fields are dropped uniformly.
func TestTodo_ALIGN_018(t *testing.T) {
	q, err := productquery.Compile(compiledFixtureEnvelope(), projection(testNow.Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	if q.Tenant != values.TenantId("acme") || len(q.Subjects) != 1 {
		t.Fatalf("compiled query identity = %+v", q)
	}
	if len(q.ReadFields) != 1 || q.ReadFields[0] != authz.FieldWorkerNumber {
		t.Fatalf("compiled reads = %+v", q.ReadFields)
	}
	if len(q.RedactedFields) != 1 || q.RedactedFields[0] != authz.FieldBankAccountNumber {
		t.Fatalf("compiled redacted = %+v", q.RedactedFields)
	}
	for _, id := range append(append([]authz.FieldID{}, q.ReadFields...), q.RedactedFields...) {
		if id == authz.FieldBaseSalary {
			t.Fatalf("denied field reached the compiled query: %+v", q)
		}
	}
	if q.Explain() == "" || q.Digest() == "" {
		t.Fatal("compiled query has no bounded explanation or digest")
	}
}

func TestTodo_ALIGN_018_Property(t *testing.T) {
	env := compiledFixtureEnvelope()
	shuffled := compiledFixtureEnvelope()
	shuffled.Subjects = append([]values.EntityRef(nil), env.Subjects...)
	shuffled.Fields = []queryenvelope.FieldDisposition{env.Fields[2], env.Fields[0], env.Fields[1]}
	proj := projection(testNow.Add(-time.Minute))
	one, err := productquery.Compile(env, proj)
	if err != nil {
		t.Fatal(err)
	}
	two, err := productquery.Compile(shuffled, proj)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest() != two.Digest() {
		t.Fatalf("disposition order changed compiled digest: %s != %s", one.Digest(), two.Digest())
	}
}

func TestTodo_ALIGN_018_Golden(t *testing.T) {
	q, err := productquery.Compile(compiledFixtureEnvelope(), projection(testNow.Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "a6fe29bcc04c1ecb3f01556c9b3651e6e86085557356f6490002e1ba81f9fd6b"
	if q.Digest() != wantDigest {
		t.Fatalf("compiled digest=%q want=%q", q.Digest(), wantDigest)
	}
	if len(q.Subjects) != 1 || q.Subjects[0].Id != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("golden subjects = %+v", q.Subjects)
	}
}

func TestTodo_ALIGN_018_Security(t *testing.T) {
	proj := projection(testNow.Add(-time.Minute))
	// A zero-subject envelope is no grant: nothing to compile.
	empty := compiledFixtureEnvelope()
	empty.Subjects = nil
	if _, err := productquery.Compile(empty, proj); err == nil {
		t.Fatal("unauthorized envelope was compiled")
	}
	// A foreign subject fails closed.
	foreign := compiledFixtureEnvelope()
	foreign.Subjects = []values.EntityRef{subject("other", "00000000-0000-4000-8000-000000000002")}
	if _, err := productquery.Compile(foreign, proj); err == nil {
		t.Fatal("cross-tenant envelope was compiled")
	}
	// The compiled bytes name no denied field and carry no values by
	// construction.
	q, err := productquery.Compile(compiledFixtureEnvelope(), proj)
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), string(authz.FieldBaseSalary)) {
		t.Fatalf("denied field leaked into compiled bytes: %s", b)
	}
}

func TestTodo_ALIGN_018_Integration(t *testing.T) {
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	env := compileEnvelope(t)
	proj := projection(testNow.Add(-time.Minute))
	q, err := productquery.Compile(env, proj)
	if err != nil {
		t.Fatal(err)
	}
	// The repository adapter fetches only compiled reads; redacted and
	// denied fields are never read as values.
	candidates := make([]productquery.Candidate, 0, len(q.Subjects))
	for _, s := range q.Subjects {
		cells := make(map[authz.FieldID]productquery.Cell, len(q.ReadFields))
		for _, id := range q.ReadFields {
			cells[id] = productquery.Cell{State: productquery.ValuePresent, Value: "adapter:" + string(id)}
		}
		candidates = append(candidates, productquery.Candidate{Subject: s, Fields: cells})
	}
	fields := append(append([]authz.FieldID{}, q.ReadFields...), q.RedactedFields...)
	got, err := productquery.Project(productquery.Request{
		Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective,
		Fields: fields, Candidates: candidates, Projection: proj, ObservedNow: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != len(q.Subjects) {
		t.Fatalf("projected %d rows for %d compiled subjects", len(got.Rows), len(q.Subjects))
	}
	for _, row := range got.Rows {
		for _, f := range row.Fields {
			if f.Disposition == authz.EffectAllow && f.Value == "" {
				t.Fatalf("compiled read returned no value: %+v", row)
			}
			if f.Disposition == authz.EffectRedacted && f.Value != "" {
				t.Fatalf("redacted field carried a value: %+v", row)
			}
		}
	}
}

func TestTodo_ALIGN_018_Fault(t *testing.T) {
	proj := projection(testNow.Add(-time.Minute))
	// A projection whose watermark is ahead of its source is unusable.
	bad := proj
	bad.Watermark = bad.SourceSequence + 1
	if _, err := productquery.Compile(compiledFixtureEnvelope(), bad); err == nil {
		t.Fatal("unusable projection was compiled")
	}
	// More authorized subjects than the product contract can execute.
	many := compiledFixtureEnvelope()
	many.Subjects = nil
	for i := 0; i <= productquery.MaxCandidates; i++ {
		many.Subjects = append(many.Subjects, subject("acme", fmt.Sprintf("00000000-0000-4000-8000-%012d", i)))
	}
	many.MaxRows = len(many.Subjects)
	if _, err := productquery.Compile(many, proj); err == nil {
		t.Fatal("oversized subject set was compiled")
	}
	// A redacted disposition without its obligation is inconsistent.
	unobligated := compiledFixtureEnvelope()
	unobligated.Fields[2].Obligations = nil
	if _, err := productquery.Compile(unobligated, proj); err == nil {
		t.Fatal("envelope with an unobligated redaction was compiled")
	}
}

func TestTodo_ALIGN_018_Conformance(t *testing.T) {
	if productquery.Version() != 1 {
		t.Fatalf("product contract version = %d, want 1", productquery.Version())
	}
	if productquery.MaxCandidates <= 0 || productquery.MaxFields <= 0 {
		t.Fatal("compiled query bounds are not positive")
	}
	q, err := productquery.Compile(compiledFixtureEnvelope(), projection(testNow.Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Subjects) > productquery.MaxCandidates || len(q.ReadFields)+len(q.RedactedFields) > productquery.MaxFields {
		t.Fatalf("compiled query exceeds contract bounds: %+v", q)
	}
}
