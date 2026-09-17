package learning

import (
	"strings"
	"testing"
)

func TestTodo_LEARN_001(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	v := mustVersion(t, r, c.ID)
	p := mustPath(t, r)
	if c.Digest == "" || v.Digest == "" || p.Digest == "" {
		t.Fatal("course, version or path carries no digest")
	}
	// GREEN coverage: provider/content, prerequisites, locale and
	// accessibility, assignment/enrollment refs, assessment, completion,
	// credential, expiry/renewal and external authority.
	for _, field := range []string{
		c.Provider, v.ContentRef, v.Locale, v.AssessmentRefs[0],
		v.CompletionRule, v.CredentialTemplate, v.ExpiryPolicy, v.ExternalAuthority,
	} {
		if strings.TrimSpace(field) == "" {
			t.Fatal("version omits a required learning-model field")
		}
	}
	if len(c.Locales) == 0 || len(c.Locales[0].Formats) == 0 {
		t.Fatal("course omits locale/accessibility coverage")
	}
	if len(v.PrerequisiteRefs) == 0 {
		t.Fatal("version omits prerequisites")
	}
	// RED: a stale version digest never becomes credential truth.
	_, err := r.ResolveVersionForCredential("crs-safety-101", "sha256:"+strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("stale version digest resolved for credential truth")
	}
	rej, ok := AsRejection(err)
	if !ok || rej.Code != CodeRejected001 {
		t.Fatalf("stale resolve error = %v, want %s", err, CodeRejected001)
	}
	journalBefore := len(r.Journal())
	if _, err := r.ResolveVersionForCredential("crs-safety-101", "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("second stale resolve passed")
	}
	if len(r.Journal()) != journalBefore {
		t.Fatal("refused resolve left journal effects")
	}
	// The recorded digest resolves cleanly.
	got, err := r.ResolveVersionForCredential("crs-safety-101", v.Digest)
	if err != nil {
		t.Fatalf("recorded digest refused: %v", err)
	}
	if got.Digest != v.Digest {
		t.Fatal("resolved version digest mismatch")
	}
}

func TestTodo_LEARN_001_Property(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	first := mustVersion(t, r, c.ID)
	// Duplicate version is refused and leaves the original untouched.
	_, err := r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: c.ID, Version: 2, Tenant: "tenant-acme",
		ContentRef: "other", AssessmentRefs: []string{"q"},
		CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
		ExternalAuthority: "a", Locale: "en-US", PassingScore: "80", MaxAttempts: 3,
	})
	if err == nil {
		t.Fatal("duplicate version accepted")
	}
	got, err := r.ResolveVersionForCredential(c.ID, first.Digest)
	if err != nil || got.Digest != first.Digest {
		t.Fatal("refused duplicate disturbed the original")
	}
	// Successive versions seal distinct digests.
	second, err := r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: c.ID, Version: 3, Tenant: "tenant-acme",
		ContentRef: "content-v3", AssessmentRefs: []string{"q"},
		CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
		ExternalAuthority: "a", Locale: "en-US", PassingScore: "80", MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("DefineCourseVersion v3: %v", err)
	}
	if second.Digest == first.Digest {
		t.Fatal("distinct versions share a digest")
	}
}

func TestTodo_LEARN_001_Integration(t *testing.T) {
	log := NewMemoryCourseStore()
	r := NewRegistryWithCourseStore(log)
	c := mustCourse(t, r)
	v := mustVersion(t, r, c.ID)
	// A second registry over the same store sees the same typed result.
	r2 := NewRegistryWithCourseStore(log)
	got, err := r2.LoadCourseVersion(c.ID, 2)
	if err != nil {
		t.Fatalf("LoadCourseVersion: %v", err)
	}
	if got.Digest != v.Digest {
		t.Fatal("store round-trip changed the version digest")
	}
	if _, err := r2.ResolveVersionForCredential(c.ID, v.Digest); err != nil {
		t.Fatalf("cross-instance resolve: %v", err)
	}
}

func TestTodo_LEARN_001_Security(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	_, err := r.DefineCourseVersion(rival, CourseVersion{
		CourseID: c.ID, Version: 9, Tenant: "tenant-acme",
		ContentRef: "evil", AssessmentRefs: []string{"q"},
		CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
		ExternalAuthority: "a", Locale: "en-US", PassingScore: "80", MaxAttempts: 3,
	})
	if err == nil {
		t.Fatal("cross-tenant version accepted")
	}
	if strings.Contains(err.Error(), "tenant-acme") {
		t.Fatalf("denial leaks tenant existence: %v", err)
	}
}

func TestTodo_LEARN_001_Conformance(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	v := mustVersion(t, r, c.ID)
	if !VerifyCourseVersion(v) {
		t.Fatal("version fails shared-oracle verification")
	}
	mut := v
	mut.ContentRef = "swapped-content"
	if VerifyCourseVersion(mut) {
		t.Fatal("tampered version verifies")
	}
}

func TestTodo_LEARN_001_Browser(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	v := mustVersion(t, r, c.ID)
	sum := SummarizeForDisplay(c, v)
	// Rendered result matches API semantics.
	if sum.Title != c.Title || sum.Provider != c.Provider || sum.Locale != v.Locale {
		t.Fatalf("summary diverges from API: %+v", sum)
	}
	if len(sum.AccessibleFormats) == 0 {
		t.Fatal("summary omits accessibility formats")
	}
	if len(sum.Prerequisites) != len(v.PrerequisiteRefs) {
		t.Fatal("summary omits prerequisites")
	}
	// Privacy: internal notes never reach the rendered surface.
	if strings.Contains(sum.DebugString(), "do-not-publish") {
		t.Fatal("summary leaks internal notes")
	}
}

func FuzzTodo_LEARN_001(f *testing.F) {
	f.Add([]byte("crs-safety-101"), []byte("content-v2"), uint64(2))
	f.Fuzz(func(t *testing.T, courseID, content []byte, version uint64) {
		v := CourseVersion{
			CourseID: string(courseID), Version: version, Tenant: "tenant-acme",
			ContentRef: string(content), AssessmentRefs: []string{"q"},
			CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
			ExternalAuthority: "a", Locale: "en-US", PassingScore: "80", MaxAttempts: 3,
		}
		// Must never panic; empty refs must never validate.
		if err := ValidateCourseVersion(v); err == nil &&
			(len(courseID) == 0 || len(content) == 0 || version == 0) {
			t.Fatal("empty course version validated")
		}
	})
}

func TestTodo_LEARN_001_Mutation(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	// Mutant A: version without assessment refs must be killed.
	_, err := r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: c.ID, Version: 5, Tenant: "tenant-acme",
		ContentRef: "content", CompletionRule: "x", CredentialTemplate: "t",
		ExpiryPolicy: "e", ExternalAuthority: "a", Locale: "en-US",
		PassingScore: "80", MaxAttempts: 3,
	})
	if err == nil {
		t.Fatal("assessment-less mutant survived")
	}
	// Mutant B: unparseable passing score must be killed.
	_, err = r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: c.ID, Version: 6, Tenant: "tenant-acme",
		ContentRef: "content", AssessmentRefs: []string{"q"},
		CompletionRule: "x", CredentialTemplate: "t", ExpiryPolicy: "e",
		ExternalAuthority: "a", Locale: "en-US", PassingScore: "high", MaxAttempts: 3,
	})
	if err == nil {
		t.Fatal("bad-score mutant survived")
	}
	// Mutant C: mutated content behind a recorded digest must be killed
	// at the credential boundary.
	v := mustVersion(t, r, c.ID)
	mut := v
	mut.ContentRef = "content-forged"
	if VerifyCourseVersion(mut) {
		t.Fatal("forged-content mutant survived")
	}
}
