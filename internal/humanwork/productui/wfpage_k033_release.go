package productui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

type WorkflowPagePermission string

const (
	WorkflowPageDesignPermission  WorkflowPagePermission = "workflow.page.design"
	WorkflowPagePublishPermission WorkflowPagePermission = "workflow.page.publish"
	WorkflowPageStartPermission   WorkflowPagePermission = "workflow.page.start"
)

type WorkflowPagePrincipal struct {
	Tenant      string
	ID          string
	Permissions map[WorkflowPagePermission]bool
}

func (principal WorkflowPagePrincipal) Allows(tenant string, permission WorkflowPagePermission) bool {
	return strings.TrimSpace(principal.Tenant) != "" && principal.Tenant == tenant && strings.TrimSpace(principal.ID) != "" && principal.Permissions[permission]
}

type WorkflowPageReleaseDraft struct {
	Tenant        string
	WorkflowOwner string
	Author        WorkflowPagePrincipal
	Plan          *workflow.CompiledWorkflow
	Page          pagedef.WorkflowPageDefinition
	Version       uint32
	FixtureDigest string
	PreviewPassed bool
}

type WorkflowPageReleaseVersion struct {
	Tenant        string
	WorkflowOwner string
	Page          pagedef.WorkflowPageDefinition
	Version       uint32
	Digest        string
	Author        string
	ApprovedBy    string
	Active        bool
}

type WorkflowPageReleaseEvent struct {
	Tenant  string
	Version uint32
	Kind    string
	Owner   string
}

type WorkflowPageReleaseService struct {
	mu       sync.Mutex
	tenant   string
	versions map[uint32]WorkflowPageReleaseVersion
	active   uint32
	events   []WorkflowPageReleaseEvent
}

func NewWorkflowPageReleaseService(tenant string) (*WorkflowPageReleaseService, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, errors.New("productui: workflow page release tenant is required")
	}
	return &WorkflowPageReleaseService{tenant: tenant, versions: make(map[uint32]WorkflowPageReleaseVersion)}, nil
}

// SubmitWorkflowPageDraft is the design-authority step. It does not publish
// or activate anything and requires a page binding proven by the compiler.
func (service *WorkflowPageReleaseService) SubmitWorkflowPageDraft(draft WorkflowPageReleaseDraft) (WorkflowPageReleaseVersion, error) {
	if service == nil || draft.Tenant != service.tenant || !draft.Author.Allows(service.tenant, WorkflowPageDesignPermission) {
		return WorkflowPageReleaseVersion{}, errors.New("productui: workflow page design permission denied")
	}
	if draft.Version == 0 || strings.TrimSpace(draft.WorkflowOwner) == "" || strings.TrimSpace(draft.FixtureDigest) == "" || !draft.PreviewPassed {
		return WorkflowPageReleaseVersion{}, errors.New("productui: workflow page draft is not release-ready")
	}
	violations := draft.Page.ValidateAgainst(draft.Plan)
	if len(violations) > 0 {
		return WorkflowPageReleaseVersion{}, fmt.Errorf("productui: workflow page draft invalid at %s: %s", violations[0].Path, violations[0].Reason)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, exists := service.versions[draft.Version]; exists {
		return WorkflowPageReleaseVersion{}, fmt.Errorf("productui: workflow page version %d already exists", draft.Version)
	}
	version := WorkflowPageReleaseVersion{Tenant: service.tenant, WorkflowOwner: draft.WorkflowOwner, Page: cloneWorkflowPageDefinition(draft.Page), Version: draft.Version, Digest: draft.FixtureDigest, Author: draft.Author.ID}
	service.versions[draft.Version] = version
	return cloneWorkflowPageReleaseVersion(version), nil
}

// ApproveWorkflowPageVersion is separate from design and refuses self-approval.
func (service *WorkflowPageReleaseService) ApproveWorkflowPageVersion(principal WorkflowPagePrincipal, version uint32) error {
	if service == nil || !principal.Allows(service.tenant, WorkflowPagePublishPermission) {
		return errors.New("productui: workflow page publish permission denied")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	record, exists := service.versions[version]
	if !exists {
		return fmt.Errorf("productui: workflow page version %d is unknown", version)
	}
	if record.Author == principal.ID {
		return errors.New("productui: workflow page author cannot approve their own draft")
	}
	record.ApprovedBy = principal.ID
	service.versions[version] = record
	return nil
}

// ActivateWorkflowPageVersion changes the served version under the same lock
// that writes the owner notification, so readers cannot observe a half switch.
func (service *WorkflowPageReleaseService) ActivateWorkflowPageVersion(principal WorkflowPagePrincipal, version uint32) error {
	if service == nil || !principal.Allows(service.tenant, WorkflowPagePublishPermission) {
		return errors.New("productui: workflow page publish permission denied")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	record, exists := service.versions[version]
	if !exists || record.ApprovedBy == "" {
		return errors.New("productui: workflow page version is not approved")
	}
	for currentVersion, current := range service.versions {
		current.Active = currentVersion == version
		service.versions[currentVersion] = current
	}
	service.active = version
	service.events = append(service.events, WorkflowPageReleaseEvent{Tenant: service.tenant, Version: version, Kind: "activated", Owner: record.WorkflowOwner})
	return nil
}

// AuthorizeWorkflowPageStart is deliberately independent of design and
// publish checks: starting authority does not grant either editing power.
func (service *WorkflowPageReleaseService) AuthorizeWorkflowPageStart(principal WorkflowPagePrincipal) bool {
	return service != nil && principal.Allows(service.tenant, WorkflowPageStartPermission)
}

func (service *WorkflowPageReleaseService) RollbackWorkflowPageVersion(principal WorkflowPagePrincipal, version uint32) error {
	if service == nil || !principal.Allows(service.tenant, WorkflowPagePublishPermission) {
		return errors.New("productui: workflow page publish permission denied")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.active == 0 || version >= service.active {
		return errors.New("productui: workflow page rollback must target an older active version")
	}
	record, exists := service.versions[version]
	if !exists || record.ApprovedBy == "" {
		return errors.New("productui: workflow page rollback target is not approved")
	}
	for currentVersion, current := range service.versions {
		current.Active = currentVersion == version
		service.versions[currentVersion] = current
	}
	service.active = version
	service.events = append(service.events, WorkflowPageReleaseEvent{Tenant: service.tenant, Version: version, Kind: "rollback", Owner: record.WorkflowOwner})
	return nil
}

func (service *WorkflowPageReleaseService) ServedPage() (WorkflowPageReleaseVersion, bool) {
	if service == nil {
		return WorkflowPageReleaseVersion{}, false
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	record, ok := service.versions[service.active]
	return cloneWorkflowPageReleaseVersion(record), ok
}

func (service *WorkflowPageReleaseService) Events() []WorkflowPageReleaseEvent {
	if service == nil {
		return nil
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	return append([]WorkflowPageReleaseEvent(nil), service.events...)
}

func cloneWorkflowPageReleaseVersion(version WorkflowPageReleaseVersion) WorkflowPageReleaseVersion {
	version.Page = cloneWorkflowPageDefinition(version.Page)
	return version
}
