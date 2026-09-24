package access_test

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/access"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func derivationInstant(t testing.TB, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(at)
}

func derivationInterval(t testing.TB, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewInstantInterval(derivationInstant(t, start), derivationInstant(t, end))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return interval
}

func derivationMetadata(t testing.TB) (values.RevisionToken, values.KnownAt, evidence.Provenance) {
	t.Helper()
	revision, err := values.NewSequenceRevision("access.derivation", 1)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(derivationInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(derivationInstant(t, "2026-09-01T00:00:01Z"))
	if err != nil {
		t.Fatal(err)
	}
	return revision, known, evidence.Provenance{Source: "workforce", EvidenceRef: "evidence/access-002", RecordedAt: recorded}
}

func derivationRequest(t testing.TB) access.EntitlementDerivationRequest {
	t.Helper()
	ten := values.TenantId("tenant-a")
	effective := derivationInterval(t, "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z")
	revision, known, provenance := derivationMetadata(t)
	graph := access.Graph{
		Tenant: ten,
		Identities: []access.WorkforceIdentity{{
			ID: "identity-a", Tenant: ten, Subject: "subject-a", System: "hcm",
			WorkerRef: values.EntityRef{Tenant: ten, Kind: "worker", Id: "11111111-1111-4111-8111-111111111111"},
			Revision:  revision, Authority: access.AuthorityNative, Effective: effective,
			KnownAt: known, Provenance: provenance, Lifecycle: access.LifecycleActive,
		}},
		Entitlements: []access.EntitlementDefinition{{
			ID: "entitlement-github", Tenant: ten, Subject: "team/platform", System: "github",
			Application: "github", Code: "platform-read", Version: "v1", RiskClass: access.RiskModerate,
			Owner: "security", Revision: revision, Authority: access.AuthorityNative,
			Effective: effective, KnownAt: known, Provenance: provenance, Lifecycle: access.LifecycleActive,
		}},
	}
	return access.EntitlementDerivationRequest{
		Graph: graph, AsOf: derivationInstant(t, "2026-09-05T12:00:00Z"),
		Employment: []access.EmploymentPeriod{{Tenant: ten, Ref: "employment-a", WorkforceIdentityID: "identity-a", Effective: effective}},
		Positions:  []access.PositionAssignment{{Tenant: ten, Ref: "position-a", WorkforceIdentityID: "identity-a", PositionID: "position-code-a", OrgUnitRef: "org/engineering", Effective: effective}},
		Policies:   []access.DeclaredAccessPolicy{{Tenant: ten, ID: "policy/engineering", Version: "3", EntitlementID: "entitlement-github", EmploymentRef: "employment-a", PositionRef: "position-a", OrgUnitRef: "org/engineering", Effect: access.AccessPolicyAllow, Effective: effective}},
		Revision:   revision, KnownAt: known, Provenance: provenance,
	}
}

func TestExpectedEntitlementCalculationReturnsExplainableUnknownSafeGraph(t *testing.T) {
	request := derivationRequest(t)
	calculation, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if len(calculation.Expected) != 1 || calculation.Decisions[0].Status != access.DerivationExpected {
		t.Fatalf("calculation = %+v", calculation)
	}
	if calculation.Expected[0].EmploymentRef != "employment-a" || calculation.Expected[0].PositionRef != "position-a" || calculation.Expected[0].PolicyRef != "policy/engineering@3" {
		t.Fatalf("basis was not retained: %+v", calculation.Expected[0])
	}
	if calculation.Digest() == "" {
		t.Fatal("calculation has no digest")
	}
	explanation := calculation.Explain()
	if explanation.ExpectedCount != 1 || explanation.UnknownCount != 0 || len(explanation.Lines) != 1 {
		t.Fatalf("explanation = %+v", explanation)
	}
	if strings.Contains(explanation.Lines[0], "engineering@3") || strings.Contains(explanation.Lines[0], "secret") {
		t.Fatalf("explanation disclosed a sensitive basis reference: %q", explanation.Lines[0])
	}

	missing := request
	missing.Positions = nil
	unknown, err := access.CalculateExpectedEntitlements(missing)
	if err != nil {
		t.Fatalf("missing fact calculation: %v", err)
	}
	if unknown.Decisions[0].Status != access.DerivationUnknown || len(unknown.Expected) != 0 {
		t.Fatalf("missing governed fact was not safe/unknown: %+v", unknown)
	}

	denied := request
	denied.Policies = append(append([]access.DeclaredAccessPolicy(nil), request.Policies...), access.DeclaredAccessPolicy{
		Tenant: request.Graph.Tenant, ID: "policy/deny", Version: "1", EntitlementID: "entitlement-github", Effect: access.AccessPolicyDeny, Effective: request.Policies[0].Effective,
	})
	deniedResult, err := access.CalculateExpectedEntitlements(denied)
	if err != nil {
		t.Fatalf("deny calculation: %v", err)
	}
	if deniedResult.Decisions[0].Status != access.DerivationNotExpected || len(deniedResult.Expected) != 0 {
		t.Fatalf("deny did not dominate allow: %+v", deniedResult.Decisions[0])
	}

	observed := request
	observed.Observations = []access.ExternalAccessObservation{{ID: "provider-observation"}}
	if !errors.Is(func() error { _, err := access.CalculateExpectedEntitlements(observed); return err }(), access.ErrObservationInput) {
		t.Fatal("observation was accepted as a derivation fact")
	}

	current := request.Graph
	current.Expected = append([]access.ExpectedEntitlement(nil), calculation.Expected...)
	currentResult := request
	currentResult.Graph = current
	currentResult.Policies = nil
	next, err := access.CalculateExpectedEntitlements(currentResult)
	if err != nil {
		t.Fatalf("next calculation: %v", err)
	}
	deltas, err := access.DiffExpectedEntitlements(current.Expected, next)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("expected revoke delta, got %+v", deltas)
	}
}

func TestTodo_ACCESS_002_Property(t *testing.T) {
	base := derivationRequest(t)
	first, err := access.CalculateExpectedEntitlements(base)
	if err != nil {
		t.Fatal(err)
	}
	permuted := base
	permuted.Employment = append([]access.EmploymentPeriod(nil), base.Employment...)
	permuted.Positions = append([]access.PositionAssignment(nil), base.Positions...)
	permuted.Policies = append([]access.DeclaredAccessPolicy(nil), base.Policies...)
	permuted.Employment = append(permuted.Employment, access.EmploymentPeriod{Tenant: base.Graph.Tenant, Ref: "employment-unused", WorkforceIdentityID: "identity-a", Effective: base.Employment[0].Effective})
	permuted.Employment[0], permuted.Employment[1] = permuted.Employment[1], permuted.Employment[0]
	second, err := access.CalculateExpectedEntitlements(permuted)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("irrelevant input changed digest: %s vs %s", first.Digest(), second.Digest())
	}
}

func TestTodo_ACCESS_002_Golden(t *testing.T) {
	first, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() == "" || first.Digest() != second.Digest() {
		t.Fatalf("golden digest is unstable: %q %q", first.Digest(), second.Digest())
	}
}

func TestTodo_ACCESS_002_Security(t *testing.T) {
	request := derivationRequest(t)
	request.Observations = []access.ExternalAccessObservation{{ID: "observation", Authority: access.AuthorityExternalObservation}}
	if _, err := access.CalculateExpectedEntitlements(request); !errors.Is(err, access.ErrObservationInput) {
		t.Fatalf("observation error = %v", err)
	}
	request = derivationRequest(t)
	request.Graph.Identities[0].Lifecycle = access.LifecycleRevoked
	result, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].Status != access.DerivationNotExpected || len(result.Expected) != 0 {
		t.Fatalf("revoked identity received access: %+v", result)
	}
	if strings.Contains(strings.Join(result.Explain().Lines, "\n"), "secret") {
		t.Fatal("sensitive basis leaked")
	}
}

func TestTodo_ACCESS_002_Mutation(t *testing.T) {
	base, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutated := derivationRequest(t)
	mutated.Policies[0].Effect = access.AccessPolicyDeny
	changed, err := access.CalculateExpectedEntitlements(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest() == changed.Digest() {
		t.Fatal("policy mutation did not change digest")
	}
	deltas, err := access.DiffExpectedEntitlements(base.Expected, changed)
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("mutation delta = %+v", deltas)
	}
}

func TestTodo_REV_051_02(t *testing.T) {
	request := derivationRequest(t)
	allow, err := access.DeriveExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(allow.Expected) != 1 || allow.Decisions[0].Status != access.DerivationExpected {
		t.Fatalf("shared eligibility/rules engines did not derive allow: %+v", allow.Decisions)
	}
	request.Policies = append(request.Policies, access.DeclaredAccessPolicy{Tenant: request.Graph.Tenant, ID: "policy/deny", Version: "1", EntitlementID: "entitlement-github", Effect: access.AccessPolicyDeny, Effective: request.Policies[0].Effective})
	denied, err := access.DeriveExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(denied.Expected) != 0 || denied.Decisions[0].Status != access.DerivationNotExpected {
		t.Fatalf("deny did not dominate through rules composition: %+v", denied.Decisions)
	}
	deltas, err := access.DiffExpectedEntitlements(allow.Expected, denied)
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("deny did not produce a revoke: %+v", deltas)
	}
}

func TestTodo_REV_051_02_Security(t *testing.T) {
	request := derivationRequest(t)
	request.Graph.Identities[0].WorkerRef.Tenant = "tenant-b"
	if _, err := access.DeriveExpectedEntitlements(request); !errors.Is(err, access.ErrTenantMismatch) {
		t.Fatalf("foreign-tenant worker reference error = %v, want ErrTenantMismatch", err)
	}
	request = derivationRequest(t)
	request.Employment[0].Tenant = "tenant-b"
	if _, err := access.DeriveExpectedEntitlements(request); !errors.Is(err, access.ErrTenantMismatch) {
		t.Fatalf("foreign employment tenant error = %v, want ErrTenantMismatch", err)
	}
	request = derivationRequest(t)
	request.Positions[0].Tenant = "tenant-b"
	if _, err := access.DeriveExpectedEntitlements(request); !errors.Is(err, access.ErrTenantMismatch) {
		t.Fatalf("foreign position tenant error = %v, want ErrTenantMismatch", err)
	}
	request = derivationRequest(t)
	request.Policies[0].Tenant = "tenant-b"
	if _, err := access.DeriveExpectedEntitlements(request); !errors.Is(err, access.ErrTenantMismatch) {
		t.Fatalf("foreign policy tenant error = %v, want ErrTenantMismatch", err)
	}
}

func TestTodo_REV_051_02_Mutation(t *testing.T) {
	request := derivationRequest(t)
	allowed, err := access.DeriveExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Policies[0].Effect = access.AccessPolicyDeny
	denied, err := access.DeriveExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Digest() == denied.Digest() {
		t.Fatal("changing policy effect did not change the derivation digest")
	}
	deltas, err := access.DiffExpectedEntitlements(allowed.Expected, denied)
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("effect mutation delta = %+v", deltas)
	}
}

func TestTodo_REV_051_02_PositionOrgUnitCorrelation(t *testing.T) {
	request := derivationRequest(t)
	request.Positions = []access.PositionAssignment{
		{Tenant: request.Graph.Tenant, Ref: "position-a", WorkforceIdentityID: "identity-a", PositionID: "p1", OrgUnitRef: "org/other", Effective: request.Positions[0].Effective},
		{Tenant: request.Graph.Tenant, Ref: "position-b", WorkforceIdentityID: "identity-a", PositionID: "p2", OrgUnitRef: "org/engineering", Effective: request.Positions[0].Effective},
	}
	calculation, err := access.DeriveExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(calculation.Expected) != 0 || calculation.Decisions[0].Status != access.DerivationNotExpected {
		t.Fatalf("separate position/org unit matches incorrectly granted access: %+v", calculation.Decisions[0])
	}
}

func TestTodo_REV_051_02_Golden(t *testing.T) {
	calculation, err := access.DeriveExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:441c4a97d8a4f917d7b384301d715fa35e623a1a3dd5838045cbe66121f5311f"
	if got := calculation.Digest(); got != wantDigest {
		t.Fatalf("derivation digest = %q, want pinned golden %q", got, wantDigest)
	}
}

func FuzzTodo_ACCESS_002(f *testing.F) {
	f.Add("identity-a", "entitlement-github")
	f.Fuzz(func(t *testing.T, identityID, entitlementID string) {
		request := derivationRequest(t)
		request.Graph.Identities[0].ID = identityID
		request.Employment[0].WorkforceIdentityID = identityID
		request.Positions[0].WorkforceIdentityID = identityID
		request.Graph.Entitlements[0].ID = entitlementID
		request.Policies[0].EntitlementID = entitlementID
		_, _ = access.CalculateExpectedEntitlements(request)
	})
}

func TestTodo_ACCESS_002_DeterministicAcrossGoroutines(t *testing.T) {
	request := derivationRequest(t)
	want, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	got := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := access.CalculateExpectedEntitlements(request)
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			got[i] = result.Digest()
		}(i)
	}
	wg.Wait()
	for i, digest := range got {
		if digest != want.Digest() {
			t.Fatalf("worker %d digest = %q, want %q", i, digest, want.Digest())
		}
	}
	if !reflect.DeepEqual(got, append([]string(nil), got...)) {
		t.Fatal("digest result was not stable")
	}
	_ = sort.Strings
}

func TestEntitlementDerivation_CanonicalAndExplanationBoundaries(t *testing.T) {
	request := derivationRequest(t)
	calculation, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if calculation.Digest() == "" || len(calculation.Canonical()) == 0 || calculation.Digest() != calculation.CanonicalDigest {
		t.Fatalf("calculation canonical evidence = %#v", calculation)
	}
	if calculation.Explain().ExpectedCount != len(calculation.Expected) || access.ExplainExpectedEntitlements(calculation).CanonicalDigest != calculation.Digest() {
		t.Fatalf("explanation = %#v", calculation.Explain())
	}
	if (access.ExpectedEntitlementCalculation{}).Canonical() != nil {
		t.Fatal("invalid calculation has canonical bytes")
	}
	for _, basis := range []access.EntitlementBasisRef{{Kind: "policy", Ref: "policy@1"}, {Kind: "employment", Ref: "employment-1"}} {
		if len(basis.Canonical()) == 0 {
			t.Fatalf("valid basis has no canonical bytes: %#v", basis)
		}
	}
	for _, basis := range []access.EntitlementBasisRef{{}, {Kind: "policy"}, {Ref: "policy@1"}} {
		if basis.Canonical() != nil {
			t.Fatalf("invalid basis has canonical bytes: %#v", basis)
		}
	}
	alias, err := access.DeriveExpectedEntitlements(request)
	if err != nil || alias.Digest() != calculation.Digest() {
		t.Fatalf("derive alias = %#v, %v", alias, err)
	}
}

func TestCalculateExpectedEntitlements_RejectsMalformedGovernedInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*access.EntitlementDerivationRequest)
		want   error
	}{
		{"invalid graph", func(r *access.EntitlementDerivationRequest) { r.Graph = access.Graph{} }, access.ErrInvalidGraph},
		{"invalid as of", func(r *access.EntitlementDerivationRequest) { r.AsOf = values.Instant{} }, access.ErrInvalidDerivation},
		{"graph observation", func(r *access.EntitlementDerivationRequest) {
			r.Graph.Observations = []access.ExternalAccessObservation{{ID: "observation"}}
		}, access.ErrObservationMutation},
		{"request observation", func(r *access.EntitlementDerivationRequest) {
			r.Observations = []access.ExternalAccessObservation{{ID: "observation"}}
		}, access.ErrObservationInput},
		{"missing revision", func(r *access.EntitlementDerivationRequest) { r.Revision = values.RevisionToken{} }, access.ErrInvalidDerivation},
		{"missing known at", func(r *access.EntitlementDerivationRequest) { r.KnownAt = values.KnownAt{} }, access.ErrInvalidDerivation},
		{"invalid provenance", func(r *access.EntitlementDerivationRequest) { r.Provenance = evidence.Provenance{} }, access.ErrInvalidDerivation},
		{"knowledge order", func(r *access.EntitlementDerivationRequest) {
			r.KnownAt, _ = values.NewKnownAt(derivationInstant(t, "2026-09-02T00:00:00Z"))
		}, access.ErrInvalidDerivation},
		{"employment missing ref", func(r *access.EntitlementDerivationRequest) { r.Employment[0].Ref = "" }, access.ErrInvalidDerivation},
		{"employment missing identity", func(r *access.EntitlementDerivationRequest) { r.Employment[0].WorkforceIdentityID = "" }, access.ErrInvalidDerivation},
		{"employment unknown identity", func(r *access.EntitlementDerivationRequest) { r.Employment[0].WorkforceIdentityID = "missing" }, access.ErrMissingGovernedFact},
		{"employment invalid interval", func(r *access.EntitlementDerivationRequest) { r.Employment[0].Effective = values.EffectiveInterval{} }, access.ErrInvalidDerivation},
		{"duplicate employment", func(r *access.EntitlementDerivationRequest) { r.Employment = append(r.Employment, r.Employment[0]) }, access.ErrInvalidDerivation},
		{"position missing ref", func(r *access.EntitlementDerivationRequest) { r.Positions[0].Ref = "" }, access.ErrInvalidDerivation},
		{"position missing identity", func(r *access.EntitlementDerivationRequest) { r.Positions[0].WorkforceIdentityID = "" }, access.ErrInvalidDerivation},
		{"position missing position id", func(r *access.EntitlementDerivationRequest) { r.Positions[0].PositionID = "" }, access.ErrInvalidDerivation},
		{"position unknown identity", func(r *access.EntitlementDerivationRequest) { r.Positions[0].WorkforceIdentityID = "missing" }, access.ErrMissingGovernedFact},
		{"position invalid interval", func(r *access.EntitlementDerivationRequest) { r.Positions[0].Effective = values.EffectiveInterval{} }, access.ErrInvalidDerivation},
		{"duplicate position", func(r *access.EntitlementDerivationRequest) { r.Positions = append(r.Positions, r.Positions[0]) }, access.ErrInvalidDerivation},
		{"policy missing id", func(r *access.EntitlementDerivationRequest) { r.Policies[0].ID = "" }, access.ErrInvalidDerivationPolicy},
		{"policy missing version", func(r *access.EntitlementDerivationRequest) { r.Policies[0].Version = "" }, access.ErrInvalidDerivationPolicy},
		{"policy unknown entitlement", func(r *access.EntitlementDerivationRequest) { r.Policies[0].EntitlementID = "missing" }, access.ErrInvalidDerivationPolicy},
		{"policy invalid effect", func(r *access.EntitlementDerivationRequest) { r.Policies[0].Effect = "UNKNOWN" }, access.ErrInvalidDerivationPolicy},
		{"policy invalid interval", func(r *access.EntitlementDerivationRequest) { r.Policies[0].Effective = values.EffectiveInterval{} }, access.ErrInvalidDerivation},
		{"duplicate policy", func(r *access.EntitlementDerivationRequest) { r.Policies = append(r.Policies, r.Policies[0]) }, access.ErrInvalidDerivationPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := derivationRequest(t)
			tt.mutate(&req)
			_, err := access.CalculateExpectedEntitlements(req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCalculateExpectedEntitlements_AllDecisionStatusesAndEffectiveSelection(t *testing.T) {
	base := derivationRequest(t)
	allow, err := access.CalculateExpectedEntitlements(base)
	if err != nil || len(allow.Expected) != 1 || allow.Decisions[0].Status != access.DerivationExpected {
		t.Fatalf("allow = %#v, %v", allow, err)
	}
	if allow.Decisions[0].OrgUnitRef != "org/engineering" || allow.Expected[0].AccountLinkID != "" {
		// The fixture intentionally has no account link; org scope and the
		// absence of a provisioned account are both meaningful outputs.
		if allow.Decisions[0].OrgUnitRef != "org/engineering" || allow.Expected[0].AccountLinkID != "" {
			t.Fatalf("selected basis/account = %#v / %#v", allow.Decisions[0], allow.Expected[0])
		}
	}

	for _, tc := range []struct {
		name     string
		mutate   func(*access.EntitlementDerivationRequest)
		status   access.DerivationStatus
		expected int
	}{
		{"inactive identity", func(r *access.EntitlementDerivationRequest) {
			r.Graph.Identities[0].Lifecycle = access.LifecycleRevoked
		}, access.DerivationNotExpected, 0},
		{"inactive entitlement", func(r *access.EntitlementDerivationRequest) {
			r.Graph.Entitlements[0].Lifecycle = access.LifecycleRetired
		}, access.DerivationNotExpected, 0},
		{"conditional policy", func(r *access.EntitlementDerivationRequest) { r.Policies[0].Effect = access.AccessPolicyConditional }, access.DerivationConditional, 0},
		{"no matching policy", func(r *access.EntitlementDerivationRequest) { r.Policies = nil }, access.DerivationNotExpected, 0},
		{"expired policy", func(r *access.EntitlementDerivationRequest) {
			r.Policies[0].Effective = derivationInterval(t, "2020-01-01T00:00:00Z", "2021-01-01T00:00:00Z")
		}, access.DerivationNotExpected, 0},
		{"missing required position", func(r *access.EntitlementDerivationRequest) { r.Positions = nil }, access.DerivationUnknown, 0},
		{"deny dominates", func(r *access.EntitlementDerivationRequest) {
			r.Policies = append(r.Policies, access.DeclaredAccessPolicy{Tenant: r.Graph.Tenant, ID: "deny", Version: "1", EntitlementID: "entitlement-github", Effect: access.AccessPolicyDeny, Effective: r.Policies[0].Effective})
		}, access.DerivationNotExpected, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Graph.Identities = append([]access.WorkforceIdentity(nil), base.Graph.Identities...)
			req.Graph.Entitlements = append([]access.EntitlementDefinition(nil), base.Graph.Entitlements...)
			req.Graph.Accounts = append([]access.AccountLink(nil), base.Graph.Accounts...)
			req.Employment = append([]access.EmploymentPeriod(nil), base.Employment...)
			req.Positions = append([]access.PositionAssignment(nil), base.Positions...)
			req.Policies = append([]access.DeclaredAccessPolicy(nil), base.Policies...)
			tc.mutate(&req)
			got, err := access.CalculateExpectedEntitlements(req)
			if err != nil || len(got.Decisions) != 1 || got.Decisions[0].Status != tc.status || len(got.Expected) != tc.expected {
				t.Fatalf("result = %#v, %v", got, err)
			}
		})
	}
}

func TestDiffExpectedEntitlements_GrantRevokeChangeAndValidation(t *testing.T) {
	base, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if deltas, err := access.DiffExpectedEntitlements(nil, base); err != nil || len(deltas) != 1 || deltas[0].Kind != access.EntitlementGrant || deltas[0].After.ID == "" {
		t.Fatalf("grant delta = %#v, %v", deltas, err)
	}
	empty := base
	empty.Expected = nil
	if deltas, err := access.DiffExpectedEntitlements(base.Expected, empty); err != nil || len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke || deltas[0].Before.ID == "" {
		t.Fatalf("revoke delta = %#v, %v", deltas, err)
	}
	if deltas, err := base.Diff(base.Expected); err != nil || len(deltas) != 0 {
		t.Fatalf("equal delta = %#v, %v", deltas, err)
	}
	changed := base
	changed.Expected = append([]access.ExpectedEntitlement(nil), base.Expected...)
	changed.Expected[0].PolicyRef = "different-policy@2"
	deltas, err := access.DiffExpectedEntitlements(base.Expected, changed)
	if err != nil || len(deltas) != 2 || deltas[0].Kind != access.EntitlementRevoke || deltas[1].Kind != access.EntitlementGrant {
		t.Fatalf("changed basis delta = %#v, %v", deltas, err)
	}
	if _, err := access.DiffExpectedEntitlements(nil, access.ExpectedEntitlementCalculation{}); !errors.Is(err, access.ErrInvalidEntitlementDelta) {
		t.Fatalf("missing next digest = %v", err)
	}
	if _, err := access.DiffExpectedEntitlements([]access.ExpectedEntitlement{{}}, base); !errors.Is(err, access.ErrIncompleteRevision) {
		t.Fatalf("invalid current edge = %v", err)
	}
	duplicate := append([]access.ExpectedEntitlement(nil), base.Expected...)
	duplicate = append(duplicate, base.Expected[0])
	if _, err := access.DiffExpectedEntitlements(duplicate, base); !errors.Is(err, access.ErrInvalidEntitlementDelta) {
		t.Fatalf("duplicate current edge = %v", err)
	}
	duplicateNext := base
	duplicateNext.Expected = duplicate
	if _, err := access.DiffExpectedEntitlements(nil, duplicateNext); !errors.Is(err, access.ErrInvalidEntitlementDelta) {
		t.Fatalf("duplicate calculated edge = %v", err)
	}
}
