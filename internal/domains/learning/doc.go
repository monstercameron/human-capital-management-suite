// Package learning owns the governed learning vocabulary: immutable
// Course and CourseVersion revisions, LearningPaths, prerequisite
// eligibility, assignment and enrollment, authoritative completion intake,
// assessment validation and credential issuance, expiry with renewal as a
// new credential version, and explicit LMS reconciliation with repair.
//
// A mutable definition or a stale LMS completion never becomes credential
// truth: version digests are resealed at every credential boundary, and
// acceptance of a completion stays distinct from verified completion.
package learning
