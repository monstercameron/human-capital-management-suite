package people_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

const (
	testPolicyVersion = "authz.people/2026.1"
	testPurpose       = "promotion_preflight"
)

// asOf is the bitemporal coordinate every test asks at: effective 2026-06-01,
// known as of 2026-05-15T00:00:00Z.
func asOf(t *testing.T) people.AsOf {
	t.Helper()
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("effective date: %v", err)
	}
	at, err := time.Parse(time.RFC3339, "2026-05-15T00:00:00Z")
	if err != nil {
		t.Fatalf("known timestamp: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatalf("known at: %v", err)
	}
	return people.AsOf{EffectiveOn: effective, KnownAt: known}
}

func reader(t *testing.T) *fixtures.MemoryWorkerFacts {
	t.Helper()
	r, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("worker facts fixture: %v", err)
	}
	return r
}

func workerRef(t *testing.T, key string) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef(key)
	if err != nil {
		t.Fatalf("worker %q: %v", key, err)
	}
	return ref
}

// promotionFields is the projection a promotion preflight needs. Keeping the
// tests on a narrow projection is deliberate: it is what proves the reader is
// never asked for a compartmentalised field just because the subject is being
// promoted.
var promotionFields = []people.FieldID{
	people.FieldLifecycleStatus,
	people.FieldEmploymentStatus,
	people.FieldHireDate,
	people.FieldJobCode,
	people.FieldGrade,
	people.FieldOrgUnit,
	people.FieldPayZone,
}

func TestExplainWorkerStateReturnsAuthorizedBitemporalFactsAndProvenance(t *testing.T) {
	ctx := context.Background()
	worker := workerRef(t, "jane-doe")
	coordinate := asOf(t)

	explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
		Tenant:        fixtures.Tenant,
		Worker:        worker,
		AsOf:          coordinate,
		Fields:        promotionFields,
		Authorization: fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields),
	})
	if err != nil {
		t.Fatalf("ExplainWorkerState: %v", err)
	}

	if explanation.Disclosure != people.DisclosureFull {
		t.Errorf("disclosure = %s, want FULL", explanation.Disclosure)
	}
	if explanation.Presence != people.SubjectPresent {
		t.Errorf("presence = %s, want PRESENT", explanation.Presence)
	}
	if len(explanation.Fields) != len(promotionFields) {
		t.Fatalf("returned %d fields, want %d", len(explanation.Fields), len(promotionFields))
	}
	if !sort.SliceIsSorted(explanation.Fields, func(i, j int) bool {
		return explanation.Fields[i].Field < explanation.Fields[j].Field
	}) {
		t.Error("fields must be returned in sorted field order so the result digests stably")
	}

	// Every authorized fact must carry all four evidence coordinates. This is
	// the RED clause: an explanation that omits effective-at, known-at or
	// source authority is not an explanation.
	for _, f := range explanation.Fields {
		if f.Access != people.AccessAuthorized {
			t.Fatalf("field %s was not authorized in an allow-all decision", f.Field)
		}
		if f.Effective.Validate() != nil {
			t.Errorf("field %s has no effective interval", f.Field)
		}
		if f.KnownAt.Canonical() == nil {
			t.Errorf("field %s has no known-at", f.Field)
		}
		if !f.Revision.IsSpecified() {
			t.Errorf("field %s has no revision", f.Field)
		}
		if f.Authority.Validate() != nil {
			t.Errorf("field %s has no source authority", f.Field)
		}
		if f.Provenance.Validate() != nil {
			t.Errorf("field %s has no provenance", f.Field)
		}
		if f.Provenance.RecordedAt.Canonical() == nil {
			t.Errorf("field %s provenance has no recorded-at", f.Field)
		}
	}

	if grade, ok := explanation.Value(people.FieldGrade); !ok || grade != "P3" {
		t.Errorf("grade = %q (present %v), want P3", grade, ok)
	}
	if !explanation.Watermark.IsSpecified() {
		t.Error("a disclosed explanation must pin the read watermark")
	}
	if explanation.AsOf != coordinate {
		t.Error("the explanation must echo the bitemporal coordinate it answered at")
	}
	if explanation.PolicyVersion != testPolicyVersion {
		t.Errorf("policy version = %q, want %q", explanation.PolicyVersion, testPolicyVersion)
	}
	if explanation.RulePackVersion != people.ExplainRulePackVersion {
		t.Errorf("rule pack version = %q, want %q", explanation.RulePackVersion, people.ExplainRulePackVersion)
	}

	if !explanation.Effects.IsZero() {
		t.Errorf("a governed read must produce zero effects, got %v", explanation.Effects.NonZero())
	}
	if err := explanation.Receipt.Validate(); err != nil {
		t.Errorf("receipt: %v", err)
	}
	if explanation.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Errorf("execution state = %q, want NOT_PLANNED", explanation.Receipt.ExecutionState)
	}
	if explanation.Receipt.InputsDigest != explanation.InputsDigest ||
		explanation.Receipt.ResultDigest != explanation.ResultDigest {
		t.Error("the receipt must cite the same digests as the explanation")
	}
	if len(explanation.Narrative) == 0 || len(explanation.Narrative) > people.MaxNarrativeLines {
		t.Errorf("narrative has %d lines, want 1..%d", len(explanation.Narrative), people.MaxNarrativeLines)
	}
}

func TestExplainWorkerStateIsReproducibleForIdenticalInputs(t *testing.T) {
	ctx := context.Background()
	req := people.ExplainWorkerStateRequest{
		Tenant:        fixtures.Tenant,
		Worker:        workerRef(t, "jane-doe"),
		AsOf:          asOf(t),
		Fields:        promotionFields,
		Authorization: fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields),
	}
	first, err := people.ExplainWorkerState(ctx, reader(t), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := people.ExplainWorkerState(ctx, reader(t), req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical inputs produced different canonical bytes")
	}
	if first.ResultDigest != second.ResultDigest {
		t.Fatalf("result digest drifted: %s vs %s", first.ResultDigest, second.ResultDigest)
	}
}

// TestTodo_PEOPLE_005_Conformance checks the whole returned contract against
// the request projection: one sorted result per requested field, every
// authorized value backed by evidence, denied values never backed by evidence,
// and the effect-free receipt bound to the result.
func TestTodo_PEOPLE_005_Conformance(t *testing.T) {
	fields := append([]people.FieldID(nil), promotionFields...)
	fields = append(fields, people.FieldLegalName)
	req := people.ExplainWorkerStateRequest{
		Tenant: fixtures.Tenant, Worker: workerRef(t, "jane-doe"), AsOf: asOf(t),
		Fields: fields,
		Authorization: fixtures.DenyFields(fixtures.AllowAll(testPolicyVersion, testPurpose, fields), "policy_denied",
			people.FieldLifecycleStatus, people.FieldEmploymentStatus, people.FieldHireDate, people.FieldJobCode, people.FieldOrgUnit, people.FieldPayZone),
	}
	got, err := people.ExplainWorkerState(context.Background(), reader(t), req)
	if err != nil {
		t.Fatalf("ExplainWorkerState: %v", err)
	}
	if got.Disclosure != people.DisclosurePartial || got.Presence != people.SubjectPresent {
		t.Fatalf("disclosure/presence = %s/%s, want PARTIAL/PRESENT", got.Disclosure, got.Presence)
	}
	if len(got.Fields) != len(fields) {
		t.Fatalf("fields = %d, want %d", len(got.Fields), len(fields))
	}
	denied, authorized := 0, 0
	for i, f := range got.Fields {
		if i > 0 && got.Fields[i-1].Field >= f.Field {
			t.Fatalf("fields not unique and sorted at %q", f.Field)
		}
		switch f.Access {
		case people.AccessAuthorized:
			authorized++
			if _, ok := f.Value.Get(); !ok {
				t.Errorf("authorized %s has no value", f.Field)
			}
			if f.Provenance.Validate() != nil || f.Authority.Validate() != nil || f.KnownAt.Canonical() == nil || !f.Revision.IsSpecified() || f.Effective.Validate() != nil {
				t.Errorf("authorized %s lacks complete evidence", f.Field)
			}
		case people.AccessDenied:
			denied++
			if _, ok := f.Value.Get(); ok {
				t.Errorf("denied %s leaked a value", f.Field)
			}
			if f.Provenance.Validate() == nil || f.Authority.Validate() == nil || f.KnownAt.Canonical() != nil || f.Revision.IsSpecified() {
				t.Errorf("denied %s leaked source evidence", f.Field)
			}
		default:
			t.Errorf("field %s has invalid access %s", f.Field, f.Access)
		}
	}
	if authorized != 2 || denied != len(fields)-2 {
		t.Fatalf("authorized/denied = %d/%d", authorized, denied)
	}
	if !got.Effects.IsZero() || got.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned || got.Receipt.ResultDigest != got.ResultDigest {
		t.Fatalf("nonconforming effect receipt: effects=%v receipt=%+v", got.Effects.NonZero(), got.Receipt)
	}
	if err := got.Receipt.Validate(); err != nil {
		t.Fatalf("receipt invalid: %v", err)
	}
}

// TestTodo_PEOPLE_005_Mutation proves the result does not retain caller-owned
// projection memory and that a changed authorization decision changes the
// committed output instead of reusing stale disclosure data.
func TestTodo_PEOPLE_005_Mutation(t *testing.T) {
	ctx := context.Background()
	fields := append([]people.FieldID(nil), promotionFields...)
	req := people.ExplainWorkerStateRequest{Tenant: fixtures.Tenant, Worker: workerRef(t, "jane-doe"), AsOf: asOf(t), Fields: fields,
		Authorization: fixtures.AllowAll(testPolicyVersion, testPurpose, fields)}
	first, err := people.ExplainWorkerState(ctx, reader(t), req)
	if err != nil {
		t.Fatalf("first ExplainWorkerState: %v", err)
	}
	fields[0] = people.FieldLegalName
	if first.Fields[0].Field == people.FieldLegalName {
		t.Fatal("result aliased caller's mutated request slice")
	}
	deniedReq := req
	deniedReq.Fields = append([]people.FieldID(nil), promotionFields...)
	deniedReq.Authorization = fixtures.DenyFields(fixtures.AllowAll(testPolicyVersion, testPurpose, deniedReq.Fields), "policy_denied", people.FieldGrade)
	second, err := people.ExplainWorkerState(ctx, reader(t), deniedReq)
	if err != nil {
		t.Fatalf("mutated ExplainWorkerState: %v", err)
	}
	if bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("changing a field authorization left result byte-identical")
	}
	if !first.Effects.IsZero() || !second.Effects.IsZero() {
		t.Fatal("a read mutation produced an effect")
	}
}

func TestTodo_PEOPLE_005_Security(t *testing.T) {
	ctx := context.Background()
	worker := workerRef(t, "jane-doe")
	allow := fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields)

	t.Run("a denied field is named and redacted, never dropped", func(t *testing.T) {
		decision := fixtures.DenyFields(allow, "compensation_compartment", people.FieldGrade)
		explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Fields: promotionFields, Authorization: decision,
		})
		if err != nil {
			t.Fatalf("ExplainWorkerState: %v", err)
		}
		if explanation.Disclosure != people.DisclosurePartial {
			t.Errorf("disclosure = %s, want PARTIAL", explanation.Disclosure)
		}
		if len(explanation.Fields) != len(promotionFields) {
			t.Fatalf("a denial dropped the field: got %d of %d", len(explanation.Fields), len(promotionFields))
		}
		denied := explanation.DeniedFields()
		if len(denied) != 1 || denied[0].Field != people.FieldGrade {
			t.Fatalf("denied fields = %+v, want exactly assignment.grade", denied)
		}
		if denied[0].Value.State() != values.PresenceRedacted {
			t.Errorf("denied value state = %s, want REDACTED", denied[0].Value.State())
		}
		if _, ok := denied[0].Value.Get(); ok {
			t.Fatal("a denied field must not carry a readable value")
		}
		if denied[0].Provenance.Validate() == nil {
			t.Fatal("a denied field must not carry provenance; provenance is itself information about the value")
		}
		if _, ok := explanation.Value(people.FieldGrade); ok {
			t.Fatal("Value() must refuse to return a denied field")
		}
		if strings.Contains(strings.Join(explanation.Narrative, "\n"), "P3") {
			t.Fatal("the narrative leaked a field value")
		}
	})

	t.Run("a non-disclosable subject reveals neither existence nor facts", func(t *testing.T) {
		decision := fixtures.WithheldSubject(allow, "outside_population_scope")
		explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Fields: promotionFields, Authorization: decision,
		})
		if err != nil {
			t.Fatalf("ExplainWorkerState: %v", err)
		}
		if explanation.Disclosure != people.DisclosureWithheld {
			t.Fatalf("disclosure = %s, want WITHHELD", explanation.Disclosure)
		}
		if explanation.Presence != people.SubjectPresenceUnspecified {
			t.Fatalf("presence = %s; a withheld explanation must not confirm existence", explanation.Presence)
		}
		if len(explanation.Fields) != 0 {
			t.Fatalf("a withheld explanation returned %d fields", len(explanation.Fields))
		}
		if explanation.WithheldReason != "outside_population_scope" {
			t.Errorf("withheld reason = %q", explanation.WithheldReason)
		}
		if !explanation.Effects.IsZero() {
			t.Error("a withheld explanation must still count zero effects")
		}
	})

	t.Run("a subject that does not exist looks the same as one that does when withheld", func(t *testing.T) {
		absent := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker,
			Id: "99999999-9999-4999-8999-999999999999"}
		decision := fixtures.WithheldSubject(fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields),
			"outside_population_scope")

		present, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Fields: promotionFields, Authorization: decision,
		})
		if err != nil {
			t.Fatalf("present subject: %v", err)
		}
		missing, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: absent, AsOf: asOf(t),
			Fields: promotionFields, Authorization: decision,
		})
		if err != nil {
			t.Fatalf("absent subject: %v", err)
		}
		if present.Disclosure != missing.Disclosure || present.Presence != missing.Presence {
			t.Fatal("a withheld explanation distinguished an existing subject from a missing one")
		}
	})

	t.Run("an unruled field is refused rather than defaulted", func(t *testing.T) {
		partial := fixtures.AllowAll(testPolicyVersion, testPurpose,
			[]people.FieldID{people.FieldJobCode, people.FieldGrade})
		_, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Fields: promotionFields, Authorization: partial,
		})
		if !errors.Is(err, people.ErrAuthorizationIncomplete) {
			t.Fatalf("error = %v, want ErrAuthorizationIncomplete", err)
		}
	})
}

func TestTodo_PEOPLE_005_Property(t *testing.T) {
	ctx := context.Background()
	worker := workerRef(t, "jane-doe")
	allow := fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields)

	t.Run("a missing subject returns ABSENT presence and no invented values", func(t *testing.T) {
		absent := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker,
			Id: "88888888-8888-4888-8888-888888888888"}
		explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: absent, AsOf: asOf(t),
			Fields: promotionFields, Authorization: allow,
		})
		if err != nil {
			t.Fatalf("ExplainWorkerState: %v", err)
		}
		if explanation.Presence != people.SubjectAbsent {
			t.Fatalf("presence = %s, want ABSENT", explanation.Presence)
		}
		for _, f := range explanation.Fields {
			if f.Value.State() != values.PresenceUnknown {
				t.Fatalf("field %s is %s; an absent subject must yield UNKNOWN, not an empty value",
					f.Field, f.Value.State())
			}
			if _, ok := f.Value.Get(); ok {
				t.Fatalf("field %s carries a value for a subject that does not exist", f.Field)
			}
		}
	})

	t.Run("an authorized field the record does not assert is UNKNOWN, not absent from the result", func(t *testing.T) {
		store := reader(t)
		store.Missing[people.FieldPayZone] = true
		explanation, err := people.ExplainWorkerState(ctx, store, people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Fields: promotionFields, Authorization: allow,
		})
		if err != nil {
			t.Fatalf("ExplainWorkerState: %v", err)
		}
		if len(explanation.Fields) != len(promotionFields) {
			t.Fatalf("returned %d fields, want %d", len(explanation.Fields), len(promotionFields))
		}
		var payZone people.ExplainedFact
		for _, f := range explanation.Fields {
			if f.Field == people.FieldPayZone {
				payZone = f
			}
		}
		if payZone.Access != people.AccessAuthorized {
			t.Fatal("an unasserted field is authorized, not denied")
		}
		if payZone.Value.State() != values.PresenceUnknown {
			t.Fatalf("unasserted field state = %s, want UNKNOWN", payZone.Value.State())
		}
		if payZone.Value.Reason() == "" {
			t.Error("an UNKNOWN value must say why it is unknown")
		}
	})

	t.Run("the default projection covers every defined field exactly once", func(t *testing.T) {
		all := people.AllFields()
		explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
			Authorization: fixtures.AllowAll(testPolicyVersion, testPurpose, all),
		})
		if err != nil {
			t.Fatalf("ExplainWorkerState: %v", err)
		}
		seen := map[people.FieldID]int{}
		for _, f := range explanation.Fields {
			seen[f.Field]++
		}
		if len(seen) != len(all) {
			t.Fatalf("default projection covered %d fields, want %d", len(seen), len(all))
		}
		for field, n := range seen {
			if n != 1 {
				t.Errorf("field %s appeared %d times", field, n)
			}
		}
	})

	t.Run("a malformed request is refused before the reader is touched", func(t *testing.T) {
		cases := map[string]people.ExplainWorkerStateRequest{
			"cross-tenant subject": {
				Tenant: fixtures.Tenant,
				Worker: values.EntityRef{Tenant: "other-tenant", Kind: people.KindWorker, Id: worker.Id},
				AsOf:   asOf(t), Fields: promotionFields, Authorization: allow,
			},
			"duplicate field": {
				Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
				Fields:        []people.FieldID{people.FieldGrade, people.FieldGrade},
				Authorization: allow,
			},
			"unknown field": {
				Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t),
				Fields:        []people.FieldID{"assignment.salary"},
				Authorization: allow,
			},
			"no policy version": {
				Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf(t), Fields: promotionFields,
				Authorization: people.AuthorizationDecision{Purpose: testPurpose, SubjectDisclosable: true},
			},
		}
		for name, req := range cases {
			if _, err := people.ExplainWorkerState(ctx, refusingReader{t}, req); err == nil {
				t.Errorf("%s: request was accepted", name)
			}
		}
	})
}

// refusingReader fails the test if it is ever consulted.
type refusingReader struct{ t *testing.T }

// WorkerFactsAt implements people.WorkerFacts and never should be called.
func (r refusingReader) WorkerFactsAt(context.Context, people.FactQuery) (people.FactSet, error) {
	r.t.Fatal("an invalid request reached the worker facts reader")
	return people.FactSet{}, nil
}

func TestTodo_PEOPLE_005_Golden(t *testing.T) {
	ctx := context.Background()
	allow := fixtures.AllowAll(testPolicyVersion, testPurpose, promotionFields)

	cases := []struct {
		name     string
		worker   string
		decision people.AuthorizationDecision
	}{
		{"full-disclosure", "jane-doe", allow},
		{"partial-disclosure", "jane-doe",
			fixtures.DenyFields(allow, "compensation_compartment", people.FieldGrade, people.FieldPayZone)},
		{"withheld-subject", "jane-doe", fixtures.WithheldSubject(allow, "outside_population_scope")},
		{"external-observation-authority", "lena-park", allow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			explanation, err := people.ExplainWorkerState(ctx, reader(t), people.ExplainWorkerStateRequest{
				Tenant: fixtures.Tenant, Worker: workerRef(t, tc.worker), AsOf: asOf(t),
				Fields: promotionFields, Authorization: tc.decision,
			})
			if err != nil {
				t.Fatalf("ExplainWorkerState: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", "golden", tc.name+".txt"), renderExplanation(explanation))
		})
	}
}

// renderExplanation prints the explanation in a stable, reviewable form. The
// digests are included: a golden file that did not pin them would pass while
// the canonical encoding silently changed.
func renderExplanation(e people.Explanation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "intent: %s/%s\n", e.IntentType, e.IntentVersion)
	fmt.Fprintf(&b, "worker: %s\n", e.Worker)
	fmt.Fprintf(&b, "disclosure: %s\n", e.Disclosure)
	fmt.Fprintf(&b, "presence: %s\n", e.Presence)
	if e.WithheldReason != "" {
		fmt.Fprintf(&b, "withheld_reason: %s\n", e.WithheldReason)
	}
	fmt.Fprintf(&b, "policy: %s\n", e.PolicyVersion)
	fmt.Fprintf(&b, "rule_pack: %s\n", e.RulePackVersion)
	for _, f := range e.Fields {
		fmt.Fprintf(&b, "field %s access=%s state=%s", f.Field, f.Access, f.Value.State())
		if v, ok := f.Value.Get(); ok {
			fmt.Fprintf(&b, " value=%q", v)
		}
		if f.DenialReason != "" {
			fmt.Fprintf(&b, " denial=%s", f.DenialReason)
		}
		if f.Access == people.AccessAuthorized && f.Provenance.Validate() == nil {
			fmt.Fprintf(&b, " authority=%s source=%s evidence=%s recorded=%s known=%s",
				f.Authority, f.Provenance.Source, f.Provenance.EvidenceRef,
				f.Provenance.RecordedAt, f.KnownAt)
		}
		b.WriteString("\n")
	}
	for _, line := range e.Narrative {
		fmt.Fprintf(&b, "narrative: %s\n", line)
	}
	fmt.Fprintf(&b, "effects_zero: %v\n", e.Effects.IsZero())
	fmt.Fprintf(&b, "receipt_mode: %s\n", e.Receipt.Mode)
	fmt.Fprintf(&b, "receipt_execution_state: %s\n", e.Receipt.ExecutionState)
	fmt.Fprintf(&b, "inputs_digest: %s\n", e.InputsDigest)
	fmt.Fprintf(&b, "result_digest: %s\n", e.ResultDigest)
	return b.String()
}

// compareGolden compares got against the golden file at path, or rewrites it
// under -update.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
