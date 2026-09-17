package learning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testCaller = Caller{ID: "lms-ops", Tenants: []string{"tenant-acme"}}

func mustCourse(t *testing.T, r *Registry) Course {
	t.Helper()
	c, err := r.DefineCourse(testCaller, Course{
		ID: "crs-safety-101", Title: "Workplace Safety", Provider: "acme-academy",
		Tenant: "tenant-acme",
		Locales: []CourseLocale{
			{Language: "en", Region: "US", Formats: []string{"video-captioned", "text-screen-reader"}},
		},
	})
	if err != nil {
		t.Fatalf("DefineCourse: %v", err)
	}
	return c
}

func mustVersion(t *testing.T, r *Registry, courseID string) CourseVersion {
	t.Helper()
	v, err := r.DefineCourseVersion(testCaller, CourseVersion{
		CourseID: courseID, Version: 2, Tenant: "tenant-acme",
		ContentRef: "content-safety-101-v2", PrerequisiteRefs: []string{"crs-orientation@1"},
		AssessmentRefs: []string{"quiz-safety-101"}, CompletionRule: "all-modules-and-quiz",
		CredentialTemplate: "safety-cert-v1", ExpiryPolicy: "valid-730-days",
		ExternalAuthority: "osha-outreach", Locale: "en-US",
		PassingScore: "80", MaxAttempts: 3,
		InternalNotes: "do-not-publish-draft-comment",
	})
	if err != nil {
		t.Fatalf("DefineCourseVersion: %v", err)
	}
	r.RegisterProvider("acme-lms")
	return v
}

func mustPath(t *testing.T, r *Registry) LearningPath {
	t.Helper()
	p, err := r.DefinePath(testCaller, LearningPath{
		ID: "path-newhire-safety", Tenant: "tenant-acme",
		Steps:     []PathStep{{CourseID: "crs-safety-101", Version: 2}},
		Rationale: "new-hire safety requirement",
	})
	if err != nil {
		t.Fatalf("DefinePath: %v", err)
	}
	return p
}

func competentLearner() LearnerEvidence {
	return LearnerEvidence{
		LearnerID:           "worker-7",
		CompletedCourseRefs: []string{"crs-orientation@1"},
	}
}

func dueAt() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}
