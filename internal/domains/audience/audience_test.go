package audience

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func testRef(t *testing.T, number int) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: fmt.Sprintf("00000000-0000-4000-8000-%012d", number)}
}

type testOrgFacts struct {
	set OrgUnitMemberSet
}

func (f testOrgFacts) OrgUnitMembersAt(_ context.Context, _ OrgUnitQuery) (OrgUnitMemberSet, error) {
	return f.set, nil
}

type testRoleFacts struct {
	set RoleHolderSet
}

func (f testRoleFacts) RoleHoldersAt(_ context.Context, _ RoleQuery) (RoleHolderSet, error) {
	return f.set, nil
}

type testManagerFacts struct {
	sets map[string]org.WorkerFactSet
}

func (f testManagerFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	set, ok := f.sets[q.Worker.String()]
	if !ok {
		return org.WorkerFactSet{Worker: q.Worker, PolicyVersion: "org.policy/v1"}, nil
	}
	return set, nil
}

type testPreferenceFacts struct {
	snapshot PreferencesSnapshot
}

func (f testPreferenceFacts) PreferencesAt(_ context.Context, _ PreferencesQuery) (PreferencesSnapshot, error) {
	return f.snapshot, nil
}

func allowAll(_ values.EntityRef, _ SourceKind) DisclosureDecision {
	return DisclosureDecision{Allowed: true, PolicyVersion: "authz/v1"}
}

func denyTwoAndThree(ref values.EntityRef, _ SourceKind) DisclosureDecision {
	if ref == (values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: fmt.Sprintf("00000000-0000-4000-8000-%012d", 2)}) || ref == (values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: fmt.Sprintf("00000000-0000-4000-8000-%012d", 3)}) {
		return DisclosureDecision{Reason: "scope.message.recipient_denied", PolicyVersion: "authz/v2"}
	}
	return DisclosureDecision{Allowed: true, PolicyVersion: "authz/v2"}
}

func managerFact(t *testing.T, id string, worker, manager values.EntityRef) org.ManagerRelationshipFact {
	t.Helper()
	start := testInstant(t, "2026-01-01T00:00:00Z")
	effective, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(testInstant(t, "2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(testInstant(t, "2026-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("audience-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	return org.ManagerRelationshipFact{RelationshipID: id, Type: org.RelationshipDirectManager, Worker: worker, Manager: manager, AssignmentID: "assignment-" + worker.Id, Effective: effective, KnownAt: known, Revision: revision, Authority: evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "audience-test", PolicyRef: "audience-test/v1"}, Provenance: evidence.Provenance{Source: "audience-test", EvidenceRef: "evidence-" + id, RecordedAt: recorded}}
}

func managerSet(t *testing.T, worker values.EntityRef, fact ...org.ManagerRelationshipFact) org.WorkerFactSet {
	t.Helper()
	revision, err := values.NewSequenceRevision("audience-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	return org.WorkerFactSet{Worker: worker, Exists: true, Relationships: fact, Watermark: revision, PolicyVersion: "org.policy/v1"}
}

func baseRequest(t *testing.T, scope DisclosureScope) Request {
	t.Helper()
	return Request{Tenant: "tenant-a", AsOf: testInstant(t, "2026-06-01T00:00:00Z"), ResolvedAt: testInstant(t, "2026-06-01T12:00:00Z"), Scope: scope}
}

// TestTodo_MSG_002 is the primary MSG-002 contract and golden-style fixture.
func TestTodo_MSG_002(t *testing.T) {
	one, two, three := testRef(t, 1), testRef(t, 2), testRef(t, 3)
	request := baseRequest(t, denyTwoAndThree)
	request.Spec = AudienceSpec{ExplicitSubjects: []values.EntityRef{one, one}, OrgUnits: []OrgUnitSelector{{OrgUnit: "finance"}}, RoleHolders: []RoleSelector{{Role: "PayrollAdmin", Scope: "pay_group_17"}}}
	request.OrgUnits = testOrgFacts{set: OrgUnitMemberSet{Members: []values.EntityRef{one, two}, PolicyVersion: "org.scope/v3"}}
	request.Roles = testRoleFacts{set: RoleHolderSet{Holders: []values.EntityRef{two, three}, PolicyVersion: "role.policy/v5"}}
	first, err := ResolveAudience(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveAudience(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Principals) != 1 || first.Principals[0] != one || len(first.Candidates) != 3 || len(first.Exclusions) != 2 {
		t.Fatalf("resolution = %+v", first)
	}
	if first.ResultDigest == "" || first.ResultDigest != second.ResultDigest || first.ExclusionCounts["scope.message.recipient_denied"] != 2 {
		t.Fatalf("digest/counts = %q/%v", first.ResultDigest, first.ExclusionCounts)
	}
	if first.Explain().CandidateCount != 3 || first.Explain().ExcludedCount != 2 {
		t.Fatalf("explanation = %+v", first.Explain())
	}
	if strings.Contains(string(first.Canonical()), two.String()) || strings.Contains(string(first.Canonical()), three.String()) {
		t.Fatal("denied principals leaked into canonical result")
	}
}

func FuzzTodo_MSG_002(f *testing.F) {
	f.Add("role")
	f.Add("")
	f.Fuzz(func(t *testing.T, expression string) {
		request := baseRequest(t, allowAll)
		request.Spec = AudienceSpec{ExplicitSubjects: []values.EntityRef{testRef(t, 1)}}
		result, err := ResolveAudience(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Principals) != 1 || result.ResultDigest == "" {
			t.Fatalf("expression %q result = %+v", expression, result)
		}
	})
}

func TestTodo_MSG_002_Race(t *testing.T) {
	request := baseRequest(t, allowAll)
	request.Spec = AudienceSpec{ExplicitSubjects: []values.EntityRef{testRef(t, 1)}}
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := ResolveAudience(context.Background(), request)
			if err != nil || len(result.Principals) != 1 {
				t.Errorf("result=%+v err=%v", result, err)
			}
		}()
	}
	wait.Wait()
}

func TestTodo_MSG_002_Integration(t *testing.T) {
	one, two, three := testRef(t, 1), testRef(t, 2), testRef(t, 3)
	request := baseRequest(t, allowAll)
	request.Spec = AudienceSpec{ManagementChains: []ManagementChainSelector{{Subject: one, MaxDepth: 2}}}
	request.Managers = testManagerFacts{sets: map[string]org.WorkerFactSet{one.String(): managerSet(t, one, managerFact(t, "one-two", one, two)), two.String(): managerSet(t, two, managerFact(t, "two-three", two, three)), three.String(): managerSet(t, three)}}
	result, err := ResolveAudience(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Principals) != 2 || result.Principals[0] != two || result.Principals[1] != three || len(result.RelationshipVersions) != 1 {
		t.Fatalf("manager audience = %+v", result)
	}
}

func TestTodo_MSG_002_Fault(t *testing.T) {
	request := baseRequest(t, allowAll)
	request.Spec = AudienceSpec{OrgUnits: []OrgUnitSelector{{OrgUnit: "missing"}}}
	request.OrgUnits = failingOrgFacts{}
	_, err := ResolveAudience(context.Background(), request)
	if !errors.Is(err, ErrFactsReadFailed) {
		t.Fatalf("error = %v", err)
	}
}

type failingOrgFacts struct{}

func (failingOrgFacts) OrgUnitMembersAt(context.Context, OrgUnitQuery) (OrgUnitMemberSet, error) {
	return OrgUnitMemberSet{}, errors.New("snapshot unavailable")
}

func TestTodo_MSG_002_Security(t *testing.T) {
	request := baseRequest(t, func(values.EntityRef, SourceKind) DisclosureDecision {
		return DisclosureDecision{Reason: "no.address.authority", PolicyVersion: "authz/v1"}
	})
	request.Spec = AudienceSpec{ExplicitSubjects: []values.EntityRef{testRef(t, 9)}}
	result, err := ResolveAudience(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Principals) != 0 || result.ExclusionCounts["no.address.authority"] != 1 || len(result.Exclusions[0].PrincipalDigest) == 0 {
		t.Fatalf("security result = %+v", result)
	}
	if strings.Contains(string(result.Canonical()), testRef(t, 9).String()) {
		t.Fatal("withheld subject reference was disclosed")
	}
}

func digest(char byte) string { return strings.Repeat(string(char), 64) }

func endpoint(t *testing.T, principal values.EntityRef, channel Channel, value string, verified bool) Endpoint {
	t.Helper()
	return Endpoint{Principal: principal, Channel: channel, EndpointDigest: value, Verified: verified, Ownership: "BUSINESS", Locale: "en-US", Status: "ACTIVE"}
}

func deliveryAudience(t *testing.T, principal values.EntityRef) Resolution {
	t.Helper()
	return Resolution{Principals: []values.EntityRef{principal}, ResultDigest: "sha256:audience"}
}

// TestTodo_MSG_003 is the primary endpoint/preference plan fixture.
func TestTodo_MSG_003(t *testing.T) {
	principal := testRef(t, 1)
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "APPROVAL", AllowedChannels: []Channel{ChannelEmail}, AllowedOwnership: []string{"BUSINESS"}}, At: testInstant(t, "2026-06-01T12:00:00Z"), Preferences: testPreferenceFacts{snapshot: PreferencesSnapshot{Endpoints: []Endpoint{
		endpoint(t, principal, ChannelEmail, digest('a'), true),
		endpoint(t, principal, ChannelSMS, digest('b'), true),
		endpoint(t, principal, ChannelPush, digest('c'), true),
		endpoint(t, principal, ChannelInbox, digest('d'), false),
	}, Preferences: []Preference{{Principal: principal, Channel: ChannelPush, Enabled: true, OptOut: true, Version: "pref/v2"}, {Principal: principal, Channel: ChannelEmail, Enabled: true, Version: "pref/v2", Locale: "fr-FR"}}}}}
	plan, err := ResolveDelivery(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plans) != 1 || len(plan.Plans[0].Endpoints) != 1 || plan.Plans[0].Endpoints[0].Channel != ChannelEmail || plan.Plans[0].Endpoints[0].Locale != "fr-FR" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Plans[0].Excluded) != 3 || plan.Plans[0].Excluded[0].Reason != "PURPOSE_CHANNEL_FORBIDDEN" || plan.ResultDigest == "" {
		t.Fatalf("excluded/digest = %+v", plan.Plans[0])
	}
}

func TestTodo_MSG_003_Race(t *testing.T) {
	principal := testRef(t, 1)
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "NOTICE", AllowedChannels: []Channel{ChannelEmail}}, At: testInstant(t, "2026-06-01T12:00:00Z"), Preferences: testPreferenceFacts{snapshot: PreferencesSnapshot{Endpoints: []Endpoint{endpoint(t, principal, ChannelEmail, digest('e'), true)}, Preferences: []Preference{{Principal: principal, Channel: ChannelEmail, Enabled: true, Version: "pref/v1"}}}}}
	want, err := ResolveDelivery(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, err := ResolveDelivery(context.Background(), request)
			if err != nil {
				errs <- err
				return
			}
			if got.ResultDigest != want.ResultDigest {
				errs <- errors.New("concurrent delivery plan digest changed")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_MSG_003_Integration(t *testing.T) {
	principal := testRef(t, 1)
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "LEGAL_NOTICE", AllowedChannels: []Channel{ChannelInbox, ChannelEmail}, Mandatory: true, AllowQuietHoursOverride: true}, At: testInstant(t, "2026-06-01T12:00:00Z"), Preferences: testPreferenceFacts{snapshot: PreferencesSnapshot{Endpoints: []Endpoint{endpoint(t, principal, ChannelInbox, digest('f'), true)}, Preferences: []Preference{{Principal: principal, Channel: ChannelInbox, Enabled: true, Version: "pref/v9"}}}}}
	plan, err := ResolveDelivery(context.Background(), request)
	if err != nil || len(plan.Plans[0].Endpoints) != 1 || plan.PreferenceVersions[0] != "pref/v9" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestTodo_MSG_003_Fault(t *testing.T) {
	principal := testRef(t, 1)
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "NOTICE", AllowedChannels: []Channel{ChannelEmail}}, At: testInstant(t, "2026-06-01T12:00:00Z"), Preferences: failingPreferenceFacts{}}
	_, err := ResolveDelivery(context.Background(), request)
	if !errors.Is(err, ErrFactsReadFailed) {
		t.Fatalf("error = %v", err)
	}
}

type failingPreferenceFacts struct{}

func (failingPreferenceFacts) PreferencesAt(context.Context, PreferencesQuery) (PreferencesSnapshot, error) {
	return PreferencesSnapshot{}, errors.New("preference read failed")
}

func TestTodo_MSG_003_Security(t *testing.T) {
	principal := testRef(t, 1)
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "NOTICE", AllowedChannels: []Channel{ChannelEmail}}, At: testInstant(t, "2026-06-01T12:00:00Z"), Preferences: testPreferenceFacts{snapshot: PreferencesSnapshot{Endpoints: []Endpoint{endpoint(t, principal, ChannelEmail, strings.Repeat("0", 63)+"1", true)}, Preferences: []Preference{{Principal: principal, Channel: ChannelEmail, Enabled: false, Version: "pref/v3"}}}}}
	plan, err := ResolveDelivery(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plans[0].Endpoints) != 0 || len(plan.Plans[0].Excluded) != 1 || plan.Plans[0].Excluded[0].Reason != "DISABLED" {
		t.Fatalf("security plan = %+v", plan)
	}
}

func TestQuietHoursAreHonoredUnlessMandatoryOverride(t *testing.T) {
	principal := testRef(t, 1)
	snapshot := PreferencesSnapshot{Endpoints: []Endpoint{endpoint(t, principal, ChannelEmail, digest('a'), true)}, Preferences: []Preference{{Principal: principal, Channel: ChannelEmail, Enabled: true, Version: "pref/v1", QuietHours: []QuietHours{{StartMinute: 600, EndMinute: 720}}}}}
	request := DeliveryRequest{Audience: deliveryAudience(t, principal), Policy: DeliveryPolicy{Purpose: "NOTICE", AllowedChannels: []Channel{ChannelEmail}}, At: testInstant(t, "2026-06-01T11:00:00Z"), Preferences: testPreferenceFacts{snapshot: snapshot}}
	plan, err := ResolveDelivery(context.Background(), request)
	if err != nil || len(plan.Plans[0].Endpoints) != 0 || plan.Plans[0].Excluded[0].Reason != "QUIET_HOURS" {
		t.Fatalf("quiet plan=%+v err=%v", plan, err)
	}
	request.Policy.Mandatory = true
	request.Policy.AllowQuietHoursOverride = true
	plan, err = ResolveDelivery(context.Background(), request)
	if err != nil || len(plan.Plans[0].Endpoints) != 1 {
		t.Fatalf("override plan=%+v err=%v", plan, err)
	}
}
