package custom

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func recordTestDefinition() CustomObjectDefinition {
	classification := FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}
	return CustomObjectDefinition{
		Kind: "CaseFile", Namespace: "tenant.cases", Version: 1,
		Fields: map[string]FieldDefinition{
			"title":    {Type: "string", Classification: classification},
			"owner_id": {Type: "string", Classification: classification},
			"org_unit": {Type: "string", Classification: classification},
		},
		RecordRules: []RecordRule{
			{ID: "owner-read", Effect: EffectAllow, OwnerField: "owner_id"},
			{ID: "org-read", Effect: EffectAllow, ScopeField: "org_unit"},
			{ID: "manager-read", Effect: EffectAllow, OwnerField: "owner_id", Relationship: RelationshipManagerOf},
			{ID: "containment-deny", Effect: EffectDeny, ScopeField: "org_unit", ScopeValues: []string{"containment"}},
		},
	}
}

func seedRecordStore(t *testing.T, def CustomObjectDefinition, rows ...[3]string) (*EventStore, values.TenantId) {
	t.Helper()
	store, tenant := NewEventStore(), values.TenantId("tenant-a")
	for i, row := range rows {
		title, owner, org := row[0], row[1], row[2]
		at := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		interval, err := values.NewOpenInstantInterval(at)
		if err != nil {
			t.Fatal(err)
		}
		record := CustomRecordRevision{
			ObjectID: fmt.Sprintf("case-%d", i+1), ObjectKind: def.Kind, Namespace: def.Namespace,
			DefinitionVersion: def.Version, Effective: interval,
			FieldValues: map[string]TypedValue{
				"title":    {FieldName: "title", Type: "string", Value: title},
				"owner_id": {FieldName: "owner_id", Type: "string", Value: owner},
				"org_unit": {FieldName: "org_unit", Type: "string", Value: org},
			},
		}
		req := MutationRequest{Tenant: tenant, ObjectID: record.ObjectID, Definition: def, Record: record, Operation: OperationCreate,
			ExpectedHead: 0, ExpectedRevision: 0, Actor: "actor-1", Purpose: "cases.operations", EvidenceDigest: "evidence-1",
			GrantedDomains: map[string]bool{"worker.core": true}, OccurredAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
		if _, err := store.Commit(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	return store, tenant
}

func recordTestClosure() *MemoryClosure {
	closure := NewMemoryClosure()
	closure.ManagedBy["carol"] = []string{"alice", "bob"} // carol manages alice and bob
	return closure
}

// TestTodo_WF_DATA_026 proves the PRIMARY matrix of WF-DATA-026: a type
// declares record rules by owner, scope and relationship; rules combine with
// platform policy under security accumulation; and search filters before
// paging.
func TestTodo_WF_DATA_026(t *testing.T) {
	ctx := context.Background()
	def := recordTestDefinition()
	if err := def.Validate(); err != nil {
		t.Fatalf("definition: %v", err)
	}
	store, tenant := seedRecordStore(t, def,
		[3]string{"alpha", "alice", "org-1"},
		[3]string{"beta", "bob", "org-2"},
		[3]string{"gamma", "mallory", "containment"},
	)
	deps := &AccessDeps{Closure: recordTestClosure(), Platform: AllowPlatformPolicy()}

	search := func(principal Principal, limit, offset int) SearchReport {
		t.Helper()
		report, err := store.Search(ctx, SearchRequest{
			Tenant: tenant, Definition: def, Purpose: "cases.operations",
			GrantedDomains:  map[string]bool{"worker.core": true},
			RequestedFields: []string{"title", "owner_id", "org_unit"},
			Principal:       principal, Access: deps, Limit: limit, Offset: offset,
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		return report
	}

	// Owner reads her own record.
	alice := Principal{ID: "alice", Scopes: []string{"org-1"}}
	report := search(alice, 0, 0)
	if report.Count != 1 || report.Records[0].ObjectID != "case-1" {
		t.Fatalf("owner report = %+v, want case-1 only", report)
	}
	// Manager of the owner reads through the closure relationship.
	carol := Principal{ID: "carol", Scopes: []string{"org-9"}}
	report = search(carol, 0, 0)
	if report.Count != 2 {
		t.Fatalf("manager report count = %d, want alice's and bob's records", report.Count)
	}
	// Same-org scope grants without ownership.
	dave := Principal{ID: "dave", Scopes: []string{"org-2"}}
	report = search(dave, 0, 0)
	if report.Count != 1 || report.Records[0].ObjectID != "case-2" {
		t.Fatalf("scope report = %+v, want case-2 only", report)
	}
	// Deny wins over every allow, including containment scope grants.
	warden := Principal{ID: "warden", Scopes: []string{"containment"}}
	report = search(warden, 0, 0)
	if report.Count != 0 {
		t.Fatalf("containment report count = %d, want deny to win", report.Count)
	}

	// Filtering happens before paging: the page fills from allowed records
	// only, and the count is the filtered total.
	report = search(carol, 1, 1)
	if report.Count != 2 || len(report.Records) != 1 || report.Records[0].ObjectID != "case-2" {
		t.Fatalf("paged report = %+v, want the second allowed record with count 2", report)
	}

	// Security accumulation: a platform deny removes records no rule can
	// re-grant.
	denied := &AccessDeps{Closure: recordTestClosure(), Platform: DenyPrincipalPolicy("carol")}
	deniedReport, err := store.Search(ctx, SearchRequest{
		Tenant: tenant, Definition: def, Purpose: "cases.operations",
		GrantedDomains: map[string]bool{"worker.core": true},
		Principal:      carol, Access: denied,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if deniedReport.Count != 0 {
		t.Fatalf("platform-denied count = %d, want accumulation to hold the deny", deniedReport.Count)
	}
}

// TestTodo_WF_DATA_026_Security proves no query, rollup or export returns a
// record the principal may not read: search filters, the shared Filter all
// rollup/export consumers must call removes the same records, and the
// default is deny.
func TestTodo_WF_DATA_026_Security(t *testing.T) {
	ctx := context.Background()
	def := recordTestDefinition()
	store, tenant := seedRecordStore(t, def,
		[3]string{"alpha", "alice", "org-1"},
		[3]string{"beta", "bob", "org-2"},
	)
	deps := &AccessDeps{Closure: recordTestClosure(), Platform: AllowPlatformPolicy()}
	stranger := Principal{ID: "stranger", Scopes: []string{"org-9"}}

	report, err := store.Search(ctx, SearchRequest{
		Tenant: tenant, Definition: def, Purpose: "cases.operations",
		GrantedDomains: map[string]bool{"worker.core": true},
		Principal:      stranger, Access: deps,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if report.Count != 0 {
		t.Fatalf("stranger query returned %d records", report.Count)
	}

	// Rollup and export inputs pass the same Filter: only allowed records
	// survive, so no rollup or export can emit what the principal may not
	// read. The closure below knows no managers, so only ownership grants.
	rollupInput := []CustomRecordRevision{
		{ObjectID: "case-1", FieldValues: map[string]TypedValue{"owner_id": {Value: "alice"}, "org_unit": {Value: "org-1"}}},
		{ObjectID: "case-2", FieldValues: map[string]TypedValue{"owner_id": {Value: "bob"}}},
	}
	rollupDeps := &AccessDeps{Closure: NewMemoryClosure(), Platform: AllowPlatformPolicy()}
	kept, err := Filter(ctx, def, Principal{ID: "alice"}, rollupInput, rollupDeps)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(kept) != 1 || kept[0].ObjectID != "case-1" {
		t.Fatalf("rollup/export filter kept %+v, want case-1 only", kept)
	}

	// Default deny: no allow rule matches, no platform grant invents one.
	decision, err := Evaluate(ctx, def, stranger, CustomRecordRevision{
		ObjectID: "case-9", FieldValues: map[string]TypedValue{"owner_id": {Value: "alice"}, "org_unit": {Value: "org-1"}},
	}, deps)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("default decision = %+v, want deny with a reason", decision)
	}
	if decision.Reason == "" {
		t.Fatal("deny carries no reason")
	}
}

// TestTodo_WF_DATA_026_Property proves the combination laws: deny wins over
// allow in any rule order, a platform deny can only remove access, and
// filtering is idempotent.
func TestTodo_WF_DATA_026_Property(t *testing.T) {
	ctx := context.Background()
	def := recordTestDefinition()
	record := CustomRecordRevision{
		ObjectID: "case-1", FieldValues: map[string]TypedValue{"owner_id": {Value: "alice"}, "org_unit": {Value: "containment"}},
	}
	alice := Principal{ID: "alice", Scopes: []string{"containment"}}
	deps := &AccessDeps{Closure: recordTestClosure(), Platform: AllowPlatformPolicy()}

	// Owner-allow plus containment-deny in both orders: deny always wins.
	for _, rules := range [][]RecordRule{def.RecordRules, reversedRules(def.RecordRules)} {
		ordered := def
		ordered.RecordRules = rules
		decision, err := Evaluate(ctx, ordered, alice, record, deps)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if decision.Allowed {
			t.Fatal("allow beat deny")
		}
	}

	// Accumulation is monotone: allowed under the platform stays allowed or
	// becomes denied, never the reverse.
	allowed, err := Evaluate(ctx, def, Principal{ID: "alice"}, CustomRecordRevision{
		ObjectID: "case-1", FieldValues: map[string]TypedValue{"owner_id": {Value: "alice"}}},
		&AccessDeps{Closure: recordTestClosure(), Platform: AllowPlatformPolicy()})
	if err != nil || !allowed.Allowed {
		t.Fatalf("owner allow = %+v/%v", allowed, err)
	}
	denied, err := Evaluate(ctx, def, Principal{ID: "alice"}, CustomRecordRevision{
		ObjectID: "case-1", FieldValues: map[string]TypedValue{"owner_id": {Value: "alice"}}},
		&AccessDeps{Closure: recordTestClosure(), Platform: DenyPrincipalPolicy("alice")})
	if err != nil || denied.Allowed {
		t.Fatalf("platform deny override = %+v/%v", denied, err)
	}

	// Filtering is idempotent.
	records := []CustomRecordRevision{record}
	once, err := Filter(ctx, def, alice, records, deps)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	twice, err := Filter(ctx, def, alice, once, deps)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(once) != len(twice) {
		t.Fatal("filter is not idempotent")
	}
}

func reversedRules(in []RecordRule) []RecordRule {
	out := append([]RecordRule(nil), in...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// TestTodo_WF_DATA_026_Benchmark holds filtered search latency at fixture
// size: relationship closure answers are cached per request, so closure work
// grows with distinct owners, never with records.
func TestTodo_WF_DATA_026_Benchmark(t *testing.T) {
	ctx := context.Background()
	def := recordTestDefinition()
	closure := recordTestClosure()
	owners := []string{"alice", "bob", "carol", "dave", "erin", "fred", "gina", "hank"}
	rows := make([][3]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		rows = append(rows, [3]string{fmt.Sprintf("file-%d", i), owners[i%len(owners)], fmt.Sprintf("org-%d", i%len(owners))})
	}
	store, tenant := seedRecordStore(t, def, rows...)
	deps := &AccessDeps{Closure: closure, Platform: AllowPlatformPolicy()}

	start := time.Now()
	report, err := store.Search(ctx, SearchRequest{
		Tenant: tenant, Definition: def, Purpose: "cases.operations",
		GrantedDomains: map[string]bool{"worker.core": true},
		Principal:      Principal{ID: "carol", Scopes: []string{"org-9"}},
		Access:         deps,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if report.Count == 0 {
		t.Fatal("filtered search returned nothing")
	}
	// carol manages alice and bob; every other owner resolves once through
	// the cached closure: 8 owners, at most 8 closure answers.
	if closure.Calls() > len(owners) {
		t.Fatalf("closure calls = %d for %d owners: the filter refetches per record", closure.Calls(), len(owners))
	}
	t.Logf("filtered search over %d records took %s with %d closure calls", 2000, elapsed, closure.Calls())
	if elapsed > 60*time.Second {
		t.Fatalf("filtered search took %s, want under a minute at fixture size", elapsed)
	}
}
