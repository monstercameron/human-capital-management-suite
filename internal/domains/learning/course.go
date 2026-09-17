// LEARN-001: immutable Course, CourseVersion and LearningPath revisions.
// A version seals every learning-model facet — provider and content,
// prerequisites, locale and accessibility, assessment, completion,
// credential, expiry and renewal, external authority — behind a canonical
// digest. ResolveVersionForCredential reseals that digest at the
// credential boundary, so a mutable definition or a stale LMS completion
// returns LEARN_001_REJECTED instead of becoming credential truth.
package learning

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CodeRejected001 is the typed LEARN-001 denial code.
const CodeRejected001 = "LEARN_001_REJECTED"

var (
	ErrInvalidCourse  = errors.New("learning: invalid course")
	ErrInvalidVersion = errors.New("learning: invalid course version")
	ErrInvalidPath    = errors.New("learning: invalid learning path")
	ErrDuplicateEntry = errors.New("learning: entry already recorded")
)

// CourseLocale binds one language/region to its accessible formats.
type CourseLocale struct {
	Language string
	Region   string
	Formats  []string
}

// Course is one immutable course definition.
type Course struct {
	ID       string
	Title    string
	Provider string
	Tenant   string
	Locales  []CourseLocale
	Digest   string
}

// CourseVersion is one immutable revision of a course.
type CourseVersion struct {
	CourseID           string
	Version            uint64
	Tenant             string
	ContentRef         string
	PrerequisiteRefs   []string
	AssessmentRefs     []string
	CompletionRule     string
	CredentialTemplate string
	ExpiryPolicy       string
	ExternalAuthority  string
	Locale             string
	PassingScore       string
	MaxAttempts        int
	InternalNotes      string
	Digest             string
}

// PathStep is one version-pinned course in a path.
type PathStep struct {
	CourseID string
	Version  uint64
}

// LearningPath is one immutable ordered path through versioned courses.
type LearningPath struct {
	ID        string
	Tenant    string
	Steps     []PathStep
	Rationale string
	Digest    string
}

// CourseStore is the course persistence port.
type CourseStore interface {
	SaveCourse(c Course) error
	SaveVersion(v CourseVersion) error
	LoadCourse(id string) (Course, bool)
	LoadVersion(courseID string, version uint64) (CourseVersion, bool)
}

type memoryCourseStore struct {
	courses  map[string]Course
	versions map[string]CourseVersion
}

func versionKey(courseID string, version uint64) string {
	return fmt.Sprintf("%s@%d", courseID, version)
}

// NewMemoryCourseStore returns an empty in-memory course store.
func NewMemoryCourseStore() *memoryCourseStore {
	return &memoryCourseStore{
		courses:  map[string]Course{},
		versions: map[string]CourseVersion{},
	}
}

func (s *memoryCourseStore) SaveCourse(c Course) error {
	s.courses[c.ID] = c
	return nil
}

func (s *memoryCourseStore) SaveVersion(v CourseVersion) error {
	s.versions[versionKey(v.CourseID, v.Version)] = v
	return nil
}

func (s *memoryCourseStore) LoadCourse(id string) (Course, bool) {
	c, ok := s.courses[id]
	return c, ok
}

func (s *memoryCourseStore) LoadVersion(courseID string, version uint64) (CourseVersion, bool) {
	v, ok := s.versions[versionKey(courseID, version)]
	return v, ok
}

func courseDigest(c Course) (string, error) {
	w := canonicalbytes.New("learning-course", 1)
	w.String("id", c.ID)
	w.String("title", c.Title)
	w.String("provider", c.Provider)
	w.String("tenant", c.Tenant)
	for _, l := range c.Locales {
		n := canonicalbytes.New("learning-locale", 1)
		n.String("language", l.Language)
		n.String("region", l.Region)
		n.SortedStrings("formats", l.Formats)
		w.Nested("locale", n)
	}
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func courseVersionDigest(v CourseVersion) (string, error) {
	w := canonicalbytes.New("learning-course-version", 1)
	w.String("course_id", v.CourseID)
	w.Int("version", int64(v.Version))
	w.String("tenant", v.Tenant)
	w.String("content_ref", v.ContentRef)
	w.SortedStrings("prerequisites", v.PrerequisiteRefs)
	w.SortedStrings("assessments", v.AssessmentRefs)
	w.String("completion_rule", v.CompletionRule)
	w.String("credential_template", v.CredentialTemplate)
	w.String("expiry_policy", v.ExpiryPolicy)
	w.String("external_authority", v.ExternalAuthority)
	w.String("locale", v.Locale)
	w.String("passing_score", v.PassingScore)
	w.Int("max_attempts", int64(v.MaxAttempts))
	w.String("internal_notes", v.InternalNotes)
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

func pathDigest(p LearningPath) (string, error) {
	w := canonicalbytes.New("learning-path", 1)
	w.String("id", p.ID)
	w.String("tenant", p.Tenant)
	for _, s := range p.Steps {
		n := canonicalbytes.New("learning-path-step", 1)
		n.String("course_id", s.CourseID)
		n.Int("version", int64(s.Version))
		w.Nested("step", n)
	}
	w.String("rationale", p.Rationale)
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// isDecimalText reports whether s is a plain decimal literal.
func isDecimalText(s string) bool {
	if s == "" {
		return false
	}
	digits := 0
	dots := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9':
			digits++
		case s[i] == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

// ValidateCourseVersion checks a version without recording it.
func ValidateCourseVersion(v CourseVersion) error {
	if strings.TrimSpace(v.CourseID) == "" || v.Version == 0 || strings.TrimSpace(v.Tenant) == "" {
		return fmt.Errorf("%w: course, version and tenant are required", ErrInvalidVersion)
	}
	for _, ref := range []struct {
		name, value string
	}{
		{"content_ref", v.ContentRef},
		{"completion_rule", v.CompletionRule},
		{"credential_template", v.CredentialTemplate},
		{"expiry_policy", v.ExpiryPolicy},
		{"external_authority", v.ExternalAuthority},
		{"locale", v.Locale},
	} {
		if strings.TrimSpace(ref.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidVersion, ref.name)
		}
	}
	if len(v.AssessmentRefs) == 0 {
		return fmt.Errorf("%w: at least one assessment is required", ErrInvalidVersion)
	}
	if !isDecimalText(v.PassingScore) {
		return fmt.Errorf("%w: passing score is not a decimal", ErrInvalidVersion)
	}
	if v.MaxAttempts <= 0 {
		return fmt.Errorf("%w: max attempts must be positive", ErrInvalidVersion)
	}
	return nil
}

// VerifyCourseVersion reports whether v reseals against the shared oracle.
func VerifyCourseVersion(v CourseVersion) bool {
	if v.Digest == "" {
		return false
	}
	want, err := courseVersionDigest(v)
	if err != nil {
		return false
	}
	return want == v.Digest
}

// DefineCourse records an immutable course.
func (r *Registry) DefineCourse(caller Caller, c Course) (Course, error) {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Title) == "" ||
		strings.TrimSpace(c.Provider) == "" || strings.TrimSpace(c.Tenant) == "" {
		return Course{}, fmt.Errorf("%w: id, title, provider and tenant are required", ErrInvalidCourse)
	}
	if len(c.Locales) == 0 {
		return Course{}, fmt.Errorf("%w: at least one locale is required", ErrInvalidCourse)
	}
	for _, l := range c.Locales {
		if strings.TrimSpace(l.Language) == "" || len(l.Formats) == 0 {
			return Course{}, fmt.Errorf("%w: locale needs a language and formats", ErrInvalidCourse)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, c.Tenant); err != nil {
		return Course{}, err
	}
	if _, dup := r.courses.LoadCourse(c.ID); dup {
		return Course{}, fmt.Errorf("%w: course %s", ErrDuplicateEntry, c.ID)
	}
	c.Digest = ""
	digest, err := courseDigest(c)
	if err != nil {
		return Course{}, err
	}
	c.Digest = digest
	if err := r.courses.SaveCourse(c); err != nil {
		return Course{}, err
	}
	r.journal = append(r.journal, JournalEntry{Op: "define-course", Ref: c.ID, Detail: "provider=" + c.Provider})
	return c, nil
}

// DefineCourseVersion records an immutable course version.
func (r *Registry) DefineCourseVersion(caller Caller, v CourseVersion) (CourseVersion, error) {
	if err := ValidateCourseVersion(v); err != nil {
		return CourseVersion{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, v.Tenant); err != nil {
		return CourseVersion{}, err
	}
	if _, ok := r.courses.LoadCourse(v.CourseID); !ok {
		return CourseVersion{}, fmt.Errorf("%w: %s", ErrUnknownCourse, v.CourseID)
	}
	if _, dup := r.courses.LoadVersion(v.CourseID, v.Version); dup {
		return CourseVersion{}, fmt.Errorf("%w: %s", ErrDuplicateEntry, versionKey(v.CourseID, v.Version))
	}
	v.Digest = ""
	digest, err := courseVersionDigest(v)
	if err != nil {
		return CourseVersion{}, err
	}
	v.Digest = digest
	if err := r.courses.SaveVersion(v); err != nil {
		return CourseVersion{}, err
	}
	if v.Version > r.latest[v.CourseID] {
		r.latest[v.CourseID] = v.Version
	}
	r.journal = append(r.journal, JournalEntry{
		Op: "define-version", Ref: versionKey(v.CourseID, v.Version),
		Detail: "content=" + v.ContentRef,
	})
	return v, nil
}

// DefinePath records an immutable learning path over recorded versions.
func (r *Registry) DefinePath(caller Caller, p LearningPath) (LearningPath, error) {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Tenant) == "" ||
		strings.TrimSpace(p.Rationale) == "" || len(p.Steps) == 0 {
		return LearningPath{}, fmt.Errorf("%w: id, tenant, rationale and steps are required", ErrInvalidPath)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, p.Tenant); err != nil {
		return LearningPath{}, err
	}
	if _, dup := r.paths[p.ID]; dup {
		return LearningPath{}, fmt.Errorf("%w: path %s", ErrDuplicateEntry, p.ID)
	}
	for _, s := range p.Steps {
		v, ok := r.courses.LoadVersion(s.CourseID, s.Version)
		if !ok || v.Tenant != p.Tenant {
			return LearningPath{}, fmt.Errorf("%w: step %s", ErrUnknownCourse, versionKey(s.CourseID, s.Version))
		}
	}
	p.Digest = ""
	digest, err := pathDigest(p)
	if err != nil {
		return LearningPath{}, err
	}
	p.Digest = digest
	r.paths[p.ID] = p
	r.journal = append(r.journal, JournalEntry{Op: "define-path", Ref: p.ID, Detail: fmt.Sprintf("steps=%d", len(p.Steps))})
	return p, nil
}

// LookupVersion returns the recorded version, if any.
func (r *Registry) LookupVersion(courseID string, version uint64) (CourseVersion, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.courses.LoadVersion(courseID, version)
}

// LoadCourseVersion loads one version through the course store port,
// proving the store — not process memory — serves cross-instance reads.
func (r *Registry) LoadCourseVersion(courseID string, version uint64) (CourseVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.courses.LoadVersion(courseID, version)
	if !ok {
		return CourseVersion{}, fmt.Errorf("%w: %s", ErrUnknownCourse, versionKey(courseID, version))
	}
	return v, nil
}

// ResolveVersionForCredential reseals the claimed digest at the credential
// boundary. A digest that does not match the recorded immutable version —
// a mutation or a stale LMS completion — returns LEARN_001_REJECTED and
// records zero effects.
func (r *Registry) ResolveVersionForCredential(courseID, claimedDigest string) (CourseVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	latest, ok := r.latest[courseID]
	if !ok {
		// A registry sharing a store it did not write discovers the
		// latest recorded version through the store itself.
		for v := uint64(1); v <= 4096; v++ {
			if _, present := r.courses.LoadVersion(courseID, v); present {
				latest = v
				ok = true
			}
		}
		if !ok {
			return CourseVersion{}, &Rejection{Code: CodeRejected001, Field: "course_id", State: "unknown", Version: "v1"}
		}
		r.latest[courseID] = latest
	}
	v, ok := r.courses.LoadVersion(courseID, latest)
	if !ok {
		return CourseVersion{}, &Rejection{Code: CodeRejected001, Field: "course_id", State: "unknown", Version: "v1"}
	}
	if claimedDigest == "" || claimedDigest != v.Digest || !VerifyCourseVersion(v) {
		return CourseVersion{}, &Rejection{
			Code: CodeRejected001, Field: "version_digest", State: "stale-or-mutated", Version: "v1",
		}
	}
	return v, nil
}

// CourseSummary is the browser-safe render of a course version: full
// learning semantics, none of the internal record.
type CourseSummary struct {
	Title             string
	Provider          string
	Locale            string
	AccessibleFormats []string
	Prerequisites     []string
	AssessmentCount   int
}

// SummarizeForDisplay renders the course surface a learner UI may show.
func SummarizeForDisplay(c Course, v CourseVersion) CourseSummary {
	formats := []string{}
	for _, l := range c.Locales {
		formats = append(formats, l.Formats...)
	}
	return CourseSummary{
		Title:             c.Title,
		Provider:          c.Provider,
		Locale:            v.Locale,
		AccessibleFormats: formats,
		Prerequisites:     append([]string(nil), v.PrerequisiteRefs...),
		AssessmentCount:   len(v.AssessmentRefs),
	}
}

// DebugString renders the summary with no internal record content.
func (s CourseSummary) DebugString() string {
	return fmt.Sprintf("course %q provider %q locale %s formats %d prereqs %d assessments %d",
		s.Title, s.Provider, s.Locale, len(s.AccessibleFormats), len(s.Prerequisites), s.AssessmentCount)
}
