package project

import (
	"errors"
	"strings"
	"time"
)

const MaxReleaseScopeTasks = 1000

var (
	ErrInvalidRelease  = errors.New("project: invalid release tracking record")
	ErrReleaseConflict = errors.New("project: release revision conflict")
	ErrReleaseAccess   = errors.New("project: release access denied")
)

type ReleaseStatus string

const (
	ReleaseTracked             ReleaseStatus = "TRACKED"
	ReleaseVerificationPending ReleaseStatus = "VERIFICATION_PENDING"
	ReleaseVerifiedExternally  ReleaseStatus = "VERIFIED_EXTERNALLY"
)

// Release is a project-owned tracking record. It stores evidence supplied by
// an owning release system; it cannot execute or verify a deployment itself.
type Release struct {
	ID                           string
	TenantID                     string
	ProjectID                    ProjectID
	ScopeRevision                uint64
	ScopeTaskIDs                 []TaskID
	TargetMilestoneID            string
	OwningReleaseSystem          string
	Status                       ReleaseStatus
	VerificationEvidence         []ReleaseEvidence
	ExternalDeploymentReferences []ExternalDeploymentReference
	Revision                     uint64
}

type ReleaseEvidence struct {
	ID         string
	Kind       string
	Summary    string
	RecordedBy string
	RecordedAt time.Time
}

type ExternalDeploymentReference struct {
	System     string
	Reference  string
	ObservedAt time.Time
}

func NewRelease(id, tenantID string, projectID ProjectID, scopeRevision uint64, taskIDs []TaskID, targetMilestoneID, owningSystem string) (Release, error) {
	release := Release{ID: id, TenantID: tenantID, ProjectID: projectID, ScopeRevision: scopeRevision, ScopeTaskIDs: append([]TaskID(nil), taskIDs...), TargetMilestoneID: targetMilestoneID, OwningReleaseSystem: owningSystem, Status: ReleaseTracked, Revision: 1}
	if err := release.Validate(); err != nil {
		return Release{}, err
	}
	return release, nil
}

func (r Release) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(string(r.ProjectID)) == "" || r.ScopeRevision == 0 || strings.TrimSpace(r.TargetMilestoneID) == "" || strings.TrimSpace(r.OwningReleaseSystem) == "" || r.Revision == 0 || len(r.ScopeTaskIDs) > MaxReleaseScopeTasks {
		return ErrInvalidRelease
	}
	if r.Status != ReleaseTracked && r.Status != ReleaseVerificationPending && r.Status != ReleaseVerifiedExternally {
		return ErrInvalidRelease
	}
	seen := make(map[TaskID]bool, len(r.ScopeTaskIDs))
	for _, taskID := range r.ScopeTaskIDs {
		if strings.TrimSpace(string(taskID)) == "" || seen[taskID] {
			return ErrInvalidRelease
		}
		seen[taskID] = true
	}
	for _, evidence := range r.VerificationEvidence {
		if strings.TrimSpace(evidence.ID) == "" || strings.TrimSpace(evidence.Kind) == "" || strings.TrimSpace(evidence.Summary) == "" || strings.TrimSpace(evidence.RecordedBy) == "" || evidence.RecordedAt.IsZero() {
			return ErrInvalidRelease
		}
	}
	for _, reference := range r.ExternalDeploymentReferences {
		if strings.TrimSpace(reference.System) == "" || strings.TrimSpace(reference.Reference) == "" || reference.ObservedAt.IsZero() {
			return ErrInvalidRelease
		}
	}
	if r.Status == ReleaseVerifiedExternally && (len(r.VerificationEvidence) == 0 || len(r.ExternalDeploymentReferences) == 0) {
		return ErrInvalidRelease
	}
	return nil
}

// RecordVerification records an external observation. The returned status is
// deliberately named VERIFIED_EXTERNALLY, never DEPLOYED or EXECUTED.
func (r Release) RecordVerification(expected uint64, actor string, evidence ReleaseEvidence, reference ExternalDeploymentReference) (Release, error) {
	if expected == 0 || expected != r.Revision {
		return Release{}, ErrReleaseConflict
	}
	if strings.TrimSpace(actor) == "" || evidence.RecordedBy != actor || evidence.RecordedAt.IsZero() || reference.ObservedAt.IsZero() || strings.TrimSpace(reference.System) == "" || strings.TrimSpace(reference.Reference) == "" {
		return Release{}, ErrInvalidRelease
	}
	r.VerificationEvidence = append(append([]ReleaseEvidence(nil), r.VerificationEvidence...), evidence)
	r.ExternalDeploymentReferences = append(append([]ExternalDeploymentReference(nil), r.ExternalDeploymentReferences...), reference)
	r.Status = ReleaseVerifiedExternally
	r.Revision++
	if err := r.Validate(); err != nil {
		return Release{}, err
	}
	return r, nil
}

type ReleaseAccess interface {
	CanViewRelease(string) bool
	CanViewTask(ProjectID, TaskID) bool
}

type ReleaseCard struct {
	ID                   string
	TargetMilestoneID    string
	Status               ReleaseStatus
	ScopeTaskIDs         []TaskID
	VerificationEvidence []ReleaseEvidence
	ExternalReferences   []ExternalDeploymentReference
	ExecutionAuthority   string
	DeploymentClaim      string
}

type ReleaseBoard struct {
	TenantID  string
	ProjectID ProjectID
	Releases  []ReleaseCard
}

func BuildReleaseBoard(tenantID string, projectID ProjectID, releases []Release, access ReleaseAccess) (ReleaseBoard, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(string(projectID)) == "" || access == nil {
		return ReleaseBoard{}, ErrReleaseAccess
	}
	board := ReleaseBoard{TenantID: tenantID, ProjectID: projectID, Releases: make([]ReleaseCard, 0, len(releases))}
	for _, release := range releases {
		if release.TenantID != tenantID || release.ProjectID != projectID || !access.CanViewRelease(release.ID) {
			continue
		}
		if err := release.Validate(); err != nil {
			return ReleaseBoard{}, err
		}
		visibleScope := make([]TaskID, 0, len(release.ScopeTaskIDs))
		for _, taskID := range release.ScopeTaskIDs {
			if access.CanViewTask(projectID, taskID) {
				visibleScope = append(visibleScope, taskID)
			}
		}
		board.Releases = append(board.Releases, ReleaseCard{ID: release.ID, TargetMilestoneID: release.TargetMilestoneID, Status: release.Status, ScopeTaskIDs: visibleScope, VerificationEvidence: append([]ReleaseEvidence(nil), release.VerificationEvidence...), ExternalReferences: append([]ExternalDeploymentReference(nil), release.ExternalDeploymentReferences...), ExecutionAuthority: release.OwningReleaseSystem, DeploymentClaim: "TRACKING_ONLY"})
	}
	return board, nil
}
