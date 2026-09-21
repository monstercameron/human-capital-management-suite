package readiness_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/readiness"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type fakeReader struct {
	mu          sync.Mutex
	descriptors []readiness.EvidenceDescriptor
	err         error
	lastQuery   readiness.EvidenceQuery
}

func (f *fakeReader) Read(_ context.Context, query readiness.EvidenceQuery) ([]readiness.EvidenceDescriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	return append([]readiness.EvidenceDescriptor(nil), f.descriptors...), f.err
}

func instant(t *testing.T, day int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, time.January, day, 12, 0, 0, 0, time.UTC))
}

func knownAt(t *testing.T, at values.Instant) values.KnownAt {
	t.Helper()
	got, err := values.NewKnownAt(at)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func recordedAt(t *testing.T, at values.Instant) values.RecordedAt {
	t.Helper()
	got, err := values.NewRecordedAt(at)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func revision(t *testing.T, seq uint64) values.RevisionToken {
	t.Helper()
	got, err := values.NewSequenceRevision("readiness.evidence", seq)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	got, err := values.NewInstantInterval(instant(t, 1), instant(t, 31))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func subject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func requirement(t *testing.T, kinds ...readiness.EvidenceKind) readiness.ReadinessRequirement {
	t.Helper()
	got, err := readiness.NewReadinessRequirement(readiness.ReadinessRequirement{
		RequirementID: "return-to-work.medical-clearance",
		Revision:      3,
		Origin: readiness.Origin{
			Domain: readiness.DomainReturnToWork, Capability: "workforce.return_to_work", Version: "v1",
		},
		Owner:         "workforce-readiness",
		EvidenceKinds: kinds,
		Effective:     interval(t),
		Policy:        readiness.PolicyBlocking,
		Extension:     readiness.DomainExtension{Kind: "leave_restriction", Version: "v1", Ref: "restriction-policy/v3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func descriptor(t *testing.T, state readiness.EvidenceState) readiness.EvidenceDescriptor {
	t.Helper()
	return readiness.EvidenceDescriptor{
		Tenant: "tenant-a", Subject: subject(), RequirementID: "return-to-work.medical-clearance",
		EvidenceRef: "evidence:clearance:1", Kind: readiness.EvidenceAuthorization,
		Effective: interval(t), KnownAt: knownAt(t, instant(t, 2)), Revision: revision(t, 7),
		ObservedAt: instant(t, 2), FreshUntil: instant(t, 20), SourceAuthority: "clinic-system/v2",
		Provenance:     readiness.Provenance{Source: "clinic-system", EvidenceRef: "artifact:clearance:1", RecordedAt: recordedAt(t, instant(t, 2))},
		Classification: readiness.ClassificationRestricted, Access: readiness.EvidenceAuthorized,
		Trust: readiness.EvidenceTrusted, State: state,
	}
}

func request(t *testing.T, r readiness.ReadinessRequirement) readiness.ResolutionRequest {
	t.Helper()
	return readiness.ResolutionRequest{Requirement: r, Tenant: "tenant-a", Subject: subject(), AsOf: instant(t, 2), KnownAt: knownAt(t, instant(t, 2))}
}

func TestVersionAndExplainContract(t *testing.T) {
	if readiness.Version() <= 0 {
		t.Fatal("Version() must be positive")
	}
	r := requirement(t, readiness.EvidenceAuthorization)
	got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{descriptor(t, readiness.EvidenceSatisfied)}}, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readiness.Explain(got); err != nil {
		t.Fatalf("Explain: %v", err)
	}
}

func TestCompileAndEvaluateContract(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	r.CanonicalDigest = ""
	compiled, err := readiness.Compile(r)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.CanonicalDigest == "" {
		t.Fatal("Compile returned no canonical digest")
	}
	resolution := resolveForEvaluation(t, compiled, descriptor(t, readiness.EvidenceSatisfied))
	if _, err := readiness.Evaluate(compiled, resolution); err != nil {
		t.Fatalf("Evaluate(compiled): %v", err)
	}
}

func TestTodo_READINESS_CONF_001(t *testing.T) {
	report, err := readiness.ProveConformance(readiness.DefaultConformanceProfiles())
	if err != nil {
		t.Fatalf("ProveConformance: %v", err)
	}
	if !report.StableKernel || len(report.Profiles) != 4 || report.Digest == "" {
		t.Fatalf("unexpected conformance report: %+v", report)
	}
	if len(report.RequirementFields) != 5 || len(report.Statuses) != 4 {
		t.Fatalf("common kernel = fields %d/statuses %d, want 5/4", len(report.RequirementFields), len(report.Statuses))
	}
}

func TestTodo_READINESS_CONF_001_Golden(t *testing.T) {
	profiles := readiness.DefaultConformanceProfiles()
	one, err := readiness.ProveConformance(profiles)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(profiles)-1; i < j; i, j = i+1, j-1 {
		profiles[i], profiles[j] = profiles[j], profiles[i]
	}
	two, err := readiness.ProveConformance(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest {
		t.Fatalf("profile order changed digest: %q != %q", one.Digest, two.Digest)
	}
}

func TestTodo_READINESS_CONF_001_Race(t *testing.T) {
	profiles := readiness.DefaultConformanceProfiles()
	const workers = 8
	results := make(chan string, workers)
	for range workers {
		go func() {
			report, err := readiness.ProveConformance(profiles)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- report.Digest
		}()
	}
	var want string
	for range workers {
		got := <-results
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent proof diverged: %q != %q", got, want)
		}
	}
}

func TestTodo_READINESS_CONF_001_Fault(t *testing.T) {
	profiles := readiness.DefaultConformanceProfiles()[:3]
	if _, err := readiness.ProveConformance(profiles); err == nil {
		t.Fatal("three profiles must not prove a four-domain kernel")
	}
	profiles = readiness.DefaultConformanceProfiles()
	profiles[0].RequirementFields = profiles[0].RequirementFields[:1]
	if _, err := readiness.ProveConformance(profiles); err == nil {
		t.Fatal("missing common fields must refuse proof")
	}
}

func TestTodo_READINESS_CONF_001_Conformance(t *testing.T) {
	report, err := readiness.ProveConformance(readiness.DefaultConformanceProfiles())
	if err != nil {
		t.Fatal(err)
	}
	domains := make(map[readiness.Domain]bool, len(report.Profiles))
	for _, profile := range report.Profiles {
		domains[profile.Domain] = true
		if profile.Extension.Ref == "" {
			t.Fatalf("domain %s lost its extension point", profile.Domain)
		}
	}
	if len(domains) != 4 {
		t.Fatalf("domains = %d, want four domain-owned extensions", len(domains))
	}
}

func TestTodo_READINESS_001(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization, readiness.EvidenceQualification)
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.CanonicalDigest == "" {
		t.Fatal("requirement must carry a canonical digest")
	}
	if got, err := r.Digest(); err != nil || got != r.CanonicalDigest {
		t.Fatalf("Digest() = %q, %v; want %q", got, err, r.CanonicalDigest)
	}
}

func TestTodo_READINESS_001_Golden(t *testing.T) {
	one := requirement(t, readiness.EvidenceAuthorization, readiness.EvidenceQualification)
	two := requirement(t, readiness.EvidenceQualification, readiness.EvidenceAuthorization)
	if one.CanonicalDigest != two.CanonicalDigest {
		t.Fatalf("evidence-kind ordering changed digest: %q != %q", one.CanonicalDigest, two.CanonicalDigest)
	}
}

func TestTodo_READINESS_001_Conformance(t *testing.T) {
	for _, profile := range readiness.DefaultConformanceProfiles() {
		got, err := readiness.NewReadinessRequirement(readiness.ReadinessRequirement{
			RequirementID: "req-" + strings.ToLower(string(profile.Domain)), Revision: 1,
			Origin: readiness.Origin{Domain: profile.Domain, Capability: profile.Capability, Version: profile.Version},
			Owner:  "domain-owner", EvidenceKinds: []readiness.EvidenceKind{readiness.EvidenceExternal}, Effective: interval(t),
			Policy: readiness.PolicyConditional, Extension: profile.Extension,
		})
		if err != nil {
			t.Fatalf("%s requirement: %v", profile.Domain, err)
		}
		if got.Origin.Domain != profile.Domain || got.Extension != profile.Extension {
			t.Fatalf("%s origin/extension was not retained", profile.Domain)
		}
	}
}

func TestTodo_READINESS_002(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	reader := &fakeReader{descriptors: []readiness.EvidenceDescriptor{descriptor(t, readiness.EvidenceSatisfied)}}
	got, err := readiness.Resolve(context.Background(), reader, request(t, r))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != readiness.ResolutionSatisfied || len(got.Evidence) != 1 || got.Evidence[0].EvidenceRef != "evidence:clearance:1" {
		t.Fatalf("resolution = %+v, want one authorized ready reference", got)
	}
	if got.Evidence[0].Provenance.EvidenceRef == "" || got.Evidence[0].FreshUntil.Validate() != nil {
		t.Fatal("authorized result must retain provenance and freshness")
	}
	if reader.lastQuery.KnownAt != request(t, r).KnownAt {
		t.Fatal("reader was not called with the pinned known-at")
	}
}

func TestTodo_READINESS_002_Golden(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	first := descriptor(t, readiness.EvidenceSatisfied)
	second := descriptor(t, readiness.EvidenceConditional)
	second.EvidenceRef = "evidence:clearance:0"
	reader := &fakeReader{descriptors: []readiness.EvidenceDescriptor{first, second}}
	one, err := readiness.Resolve(context.Background(), reader, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	reader.descriptors = []readiness.EvidenceDescriptor{second, first}
	two, err := readiness.Resolve(context.Background(), reader, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest || one.Status != readiness.ResolutionConditional {
		t.Fatalf("non-deterministic resolution: %+v / %+v", one, two)
	}
}

func TestTodo_READINESS_002_DeniedEvidenceIsUnknownWithoutLeak(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	denied := descriptor(t, readiness.EvidenceSatisfied)
	denied.Access = readiness.EvidenceDenied
	got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{denied}}, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != readiness.ResolutionUnknown || len(got.Evidence) != 0 {
		t.Fatalf("denied evidence resolution = %+v, want UNKNOWN with no refs", got)
	}
	explanation, err := readiness.Explain(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(explanation.String(), denied.EvidenceRef) || strings.Contains(strings.Join(explanation.Reasons, ","), denied.EvidenceRef) {
		t.Fatal("explanation leaked denied evidence reference")
	}
}

func TestTodo_READINESS_002_StaleUntrustedAndQuarantinedDoNotSatisfy(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	cases := []struct {
		name string
		edit func(*readiness.EvidenceDescriptor)
	}{
		{name: "stale", edit: func(e *readiness.EvidenceDescriptor) {
			e.ObservedAt = instant(t, 1)
			e.FreshUntil = instant(t, 1)
		}},
		{name: "untrusted", edit: func(e *readiness.EvidenceDescriptor) { e.Trust = readiness.EvidenceUntrusted }},
		{name: "quarantined", edit: func(e *readiness.EvidenceDescriptor) { e.Trust = readiness.EvidenceQuarantined }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := descriptor(t, readiness.EvidenceSatisfied)
			tc.edit(&e)
			got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{e}}, request(t, r))
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != readiness.ResolutionUnsatisfied || len(got.Evidence) != 0 {
				t.Fatalf("resolution = %+v, want NOT_READY without evidence", got)
			}
		})
	}
}

func TestTodo_READINESS_002_PinnedVersionMismatchIsUnknown(t *testing.T) {
	r := requirement(t, readiness.EvidenceAuthorization)
	e := descriptor(t, readiness.EvidenceSatisfied)
	e.KnownAt = knownAt(t, instant(t, 3))
	got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{e}}, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != readiness.ResolutionUnknown || len(got.Evidence) != 0 {
		t.Fatalf("resolution = %+v, want UNKNOWN without unpinned evidence", got)
	}
	e = descriptor(t, readiness.EvidenceSatisfied)
	e.Provenance.RecordedAt = recordedAt(t, instant(t, 3))
	got, err = readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{e}}, request(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != readiness.ResolutionUnknown || len(got.Evidence) != 0 {
		t.Fatalf("future-provenance resolution = %+v, want UNKNOWN without unpinned evidence", got)
	}
}

func FuzzTodo_READINESS_002(f *testing.F) {
	f.Add("evidence:seed")
	f.Fuzz(func(t *testing.T, ref string) {
		r := requirement(t, readiness.EvidenceAuthorization)
		e := descriptor(t, readiness.EvidenceSatisfied)
		e.EvidenceRef = ref
		got, err := readiness.Resolve(context.Background(), &fakeReader{descriptors: []readiness.EvidenceDescriptor{e}}, request(t, r))
		if err == nil && got.Status != readiness.ResolutionSatisfied {
			t.Fatalf("valid descriptor with ref %q resolved as %s", ref, got.Status)
		}
		if err != nil && !errors.Is(err, readiness.ErrInvalidResolution) {
			t.Fatalf("unexpected error for ref %q: %v", ref, err)
		}
	})
}
