package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

var (
	ErrInvalidProjectBundle = errors.New("project: invalid import/export bundle")
	ErrImportConflict       = errors.New("project: import would overwrite current project state")
	ErrImportQuarantined    = errors.New("project: import must be admitted from quarantine")
)

type BundleProvenance struct {
	OperationID string
	ActorID     string
	Source      string
	ExportedAt  time.Time
}

type BundleComment struct {
	ID       string
	TaskID   string
	Revision uint64
	AuthorID string
	Body     string
}

type BundleLink struct {
	ID        string
	TaskID    string
	Revision  uint64
	Reference projectlink.Reference
}

type BundleTask struct {
	ID                   string
	ProjectID            string
	Title                string
	StatusID             string
	TypeID               string
	Revision             uint64
	ConfigurationVersion uint64
	Fields               map[string]json.RawMessage
}

type ProjectBundle struct {
	TenantID             string
	ProjectID            string
	ConfigurationVersion uint64
	Configuration        json.RawMessage
	Tasks                []BundleTask
	Comments             []BundleComment
	Links                []BundleLink
	Provenance           BundleProvenance
}

type BundleManifest struct {
	FormatVersion        uint64
	TenantID             string
	ProjectID            string
	ConfigurationVersion uint64
	TaskIDs              []string
	CommentIDs           []string
	LinkIDs              []string
	Provenance           BundleProvenance
	Digest               string
}

type ExportedProjectBundle struct {
	Bundle   ProjectBundle
	Manifest BundleManifest
}

type BundleAuthorizer interface {
	CanReadTask(string) bool
	CanReadComment(string) bool
	CanReadLink(string) bool
}

// ExportProjectBundle filters before it builds the manifest. Consequently
// private task IDs, comments, and link references cannot affect the export or
// its counts.
func ExportProjectBundle(bundle ProjectBundle, actor string, auth BundleAuthorizer, at time.Time) (ExportedProjectBundle, error) {
	if auth == nil || strings.TrimSpace(actor) == "" || at.IsZero() {
		return ExportedProjectBundle{}, ErrInvalidProjectBundle
	}
	if err := validateBundle(bundle); err != nil {
		return ExportedProjectBundle{}, err
	}
	out := bundle
	out.Tasks = nil
	out.Comments = nil
	out.Links = nil
	out.Provenance = BundleProvenance{OperationID: stableBundleID(bundle.TenantID, bundle.ProjectID, actor, at), ActorID: actor, Source: "project.export", ExportedAt: at.UTC()}
	visibleTasks := map[string]bool{}
	for _, task := range bundle.Tasks {
		if auth.CanReadTask(task.ID) {
			out.Tasks = append(out.Tasks, cloneBundleTask(task))
			visibleTasks[task.ID] = true
		}
	}
	for _, comment := range bundle.Comments {
		if visibleTasks[comment.TaskID] && auth.CanReadComment(comment.ID) {
			out.Comments = append(out.Comments, comment)
		}
	}
	for _, link := range bundle.Links {
		if visibleTasks[link.TaskID] && auth.CanReadLink(link.ID) {
			out.Links = append(out.Links, link)
		}
	}
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].ID < out.Tasks[j].ID })
	sort.Slice(out.Comments, func(i, j int) bool { return out.Comments[i].ID < out.Comments[j].ID })
	sort.Slice(out.Links, func(i, j int) bool { return out.Links[i].ID < out.Links[j].ID })
	return ExportedProjectBundle{Bundle: out, Manifest: makeBundleManifest(out)}, nil
}

type QuarantinedImport struct {
	bundle   ProjectBundle
	manifest BundleManifest
}

func QuarantineProjectImport(bundle ProjectBundle, expectedTenant, expectedProject string) (QuarantinedImport, error) {
	if bundle.TenantID != expectedTenant || bundle.ProjectID != expectedProject {
		return QuarantinedImport{}, ErrInvalidProjectBundle
	}
	if err := validateBundle(bundle); err != nil {
		return QuarantinedImport{}, err
	}
	return QuarantinedImport{bundle: cloneBundle(bundle), manifest: makeBundleManifest(bundle)}, nil
}

type ImportResult struct {
	Tasks    []BundleTask
	Comments []BundleComment
	Links    []BundleLink
	Manifest BundleManifest
}

// AdmitImport adds only new stable IDs. It never replaces an existing task,
// comment, or link, so a malformed or repeated import cannot overwrite live
// project state.
func (q QuarantinedImport) AdmitImport(existingTasks map[string]BundleTask, existingComments map[string]BundleComment, existingLinks map[string]BundleLink, expectedConfigurationVersion uint64, actor string) (ImportResult, error) {
	if q.bundle.TenantID == "" || q.manifest.Digest == "" || strings.TrimSpace(actor) == "" || expectedConfigurationVersion != q.bundle.ConfigurationVersion {
		return ImportResult{}, ErrImportQuarantined
	}
	for _, task := range q.bundle.Tasks {
		if _, exists := existingTasks[task.ID]; exists {
			return ImportResult{}, fmt.Errorf("%w: task %s", ErrImportConflict, task.ID)
		}
	}
	for _, comment := range q.bundle.Comments {
		if _, exists := existingComments[comment.ID]; exists {
			return ImportResult{}, fmt.Errorf("%w: comment %s", ErrImportConflict, comment.ID)
		}
	}
	for _, link := range q.bundle.Links {
		if _, exists := existingLinks[link.ID]; exists {
			return ImportResult{}, fmt.Errorf("%w: link %s", ErrImportConflict, link.ID)
		}
	}
	result := ImportResult{Tasks: append([]BundleTask(nil), q.bundle.Tasks...), Comments: append([]BundleComment(nil), q.bundle.Comments...), Links: append([]BundleLink(nil), q.bundle.Links...), Manifest: q.manifest}
	result.Manifest.Provenance = BundleProvenance{OperationID: q.manifest.Provenance.OperationID, ActorID: actor, Source: "project.import", ExportedAt: q.manifest.Provenance.ExportedAt}
	return result, nil
}

func validateBundle(bundle ProjectBundle) error {
	if strings.TrimSpace(bundle.TenantID) == "" || strings.TrimSpace(bundle.ProjectID) == "" || bundle.ConfigurationVersion == 0 || !json.Valid(bundle.Configuration) || bundle.Provenance.OperationID == "" || bundle.Provenance.ActorID == "" || bundle.Provenance.Source == "" || bundle.Provenance.ExportedAt.IsZero() {
		return ErrInvalidProjectBundle
	}
	tasks := map[string]bool{}
	for _, task := range bundle.Tasks {
		if task.ID == "" || task.ProjectID != bundle.ProjectID || task.Title == "" || task.StatusID == "" || task.TypeID == "" || task.Revision == 0 || task.ConfigurationVersion != bundle.ConfigurationVersion || tasks[task.ID] {
			return ErrInvalidProjectBundle
		}
		tasks[task.ID] = true
	}
	comments := map[string]bool{}
	for _, comment := range bundle.Comments {
		if comment.ID == "" || !tasks[comment.TaskID] || comment.Revision == 0 || comment.AuthorID == "" || comment.Body == "" || comments[comment.ID] {
			return ErrInvalidProjectBundle
		}
		comments[comment.ID] = true
	}
	links := map[string]bool{}
	for _, link := range bundle.Links {
		if link.ID == "" || !tasks[link.TaskID] || link.Revision == 0 || link.Reference.Validate() != nil || links[link.ID] {
			return ErrInvalidProjectBundle
		}
		links[link.ID] = true
	}
	return nil
}

func makeBundleManifest(bundle ProjectBundle) BundleManifest {
	manifest := BundleManifest{FormatVersion: 1, TenantID: bundle.TenantID, ProjectID: bundle.ProjectID, ConfigurationVersion: bundle.ConfigurationVersion, Provenance: bundle.Provenance}
	for _, task := range bundle.Tasks {
		manifest.TaskIDs = append(manifest.TaskIDs, task.ID)
	}
	for _, comment := range bundle.Comments {
		manifest.CommentIDs = append(manifest.CommentIDs, comment.ID)
	}
	for _, link := range bundle.Links {
		manifest.LinkIDs = append(manifest.LinkIDs, link.ID)
	}
	raw, _ := json.Marshal(struct {
		Bundle   ProjectBundle
		Manifest BundleManifest
	}{bundle, manifest})
	sum := sha256.Sum256(raw)
	manifest.Digest = hex.EncodeToString(sum[:])
	return manifest
}

func cloneBundle(bundle ProjectBundle) ProjectBundle {
	out := bundle
	out.Configuration = append(json.RawMessage(nil), bundle.Configuration...)
	out.Tasks = make([]BundleTask, len(bundle.Tasks))
	for i, task := range bundle.Tasks {
		out.Tasks[i] = cloneBundleTask(task)
	}
	out.Comments = append([]BundleComment(nil), bundle.Comments...)
	out.Links = append([]BundleLink(nil), bundle.Links...)
	return out
}

func cloneBundleTask(task BundleTask) BundleTask {
	task.Fields = make(map[string]json.RawMessage, len(task.Fields))
	for key, value := range task.Fields {
		task.Fields[key] = append(json.RawMessage(nil), value...)
	}
	return task
}

func stableBundleID(tenantID, projectID, actor string, at time.Time) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenantID, projectID, actor, at.UTC().Format(time.RFC3339Nano)}, "\x00")))
	return hex.EncodeToString(sum[:])
}
