package project

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

type pm16BundleAuth struct{ tasks, comments, links map[string]bool }

func (a pm16BundleAuth) CanReadTask(id string) bool    { return a.tasks[id] }
func (a pm16BundleAuth) CanReadComment(id string) bool { return a.comments[id] }
func (a pm16BundleAuth) CanReadLink(id string) bool    { return a.links[id] }

func pm16Bundle() ProjectBundle {
	return ProjectBundle{
		TenantID: "tenant-1", ProjectID: "project-1", ConfigurationVersion: 7, Configuration: []byte(`{"statuses":["todo","done"]}`),
		Tasks:      []BundleTask{{ID: "task-1", ProjectID: "project-1", Title: "Visible", StatusID: "todo", TypeID: "task", Revision: 2, ConfigurationVersion: 7}, {ID: "task-secret", ProjectID: "project-1", Title: "Private", StatusID: "todo", TypeID: "task", Revision: 3, ConfigurationVersion: 7}},
		Comments:   []BundleComment{{ID: "comment-1", TaskID: "task-1", Revision: 1, AuthorID: "alice", Body: "decision"}},
		Links:      []BundleLink{{ID: "link-1", TaskID: "task-1", Revision: 1, Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "conversation-1"}}},
		Provenance: BundleProvenance{OperationID: "source-operation", ActorID: "source-user", Source: "project.seed", ExportedAt: time.Unix(10, 0).UTC()},
	}
}

func TestTodo_PM_061(t *testing.T) {
	bundle := pm16Bundle()
	exported, err := ExportProjectBundle(bundle, "viewer", pm16BundleAuth{tasks: map[string]bool{"task-1": true}, comments: map[string]bool{"comment-1": true}, links: map[string]bool{"link-1": true}}, time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Bundle.Tasks) != 1 || exported.Bundle.Tasks[0].ID != "task-1" || len(exported.Bundle.Comments) != 1 || len(exported.Bundle.Links) != 1 || exported.Manifest.Digest == "" {
		t.Fatalf("authorized export lost provenance or visible history: %+v", exported)
	}
	quarantine, err := QuarantineProjectImport(exported.Bundle, "tenant-1", "project-1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := quarantine.AdmitImport(map[string]BundleTask{}, map[string]BundleComment{}, map[string]BundleLink{}, 7, "importer")
	if err != nil || len(result.Tasks) != 1 || result.Manifest.Provenance.ActorID != "importer" {
		t.Fatalf("quarantined import = %+v, err=%v", result, err)
	}
	if _, err := quarantine.AdmitImport(map[string]BundleTask{"task-1": bundle.Tasks[0]}, nil, nil, 7, "importer"); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("import overwrite err=%v, want conflict", err)
	}
}

func FuzzTodo_PM_061(f *testing.F) {
	f.Add([]byte(`{"statuses":[]}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, config []byte) {
		bundle := pm16Bundle()
		bundle.Configuration = config
		_, _ = QuarantineProjectImport(bundle, "tenant-1", "project-1")
	})
}

func TestTodo_PM_061_Security(t *testing.T) {
	bundle := pm16Bundle()
	if _, err := QuarantineProjectImport(bundle, "other-tenant", "project-1"); !errors.Is(err, ErrInvalidProjectBundle) {
		t.Fatalf("cross-tenant quarantine err=%v", err)
	}
	bundle.Links[0].Reference = projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "doc-1"}
	if _, err := QuarantineProjectImport(bundle, "tenant-1", "project-1"); !errors.Is(err, ErrInvalidProjectBundle) {
		t.Fatalf("malformed private link err=%v", err)
	}
}
