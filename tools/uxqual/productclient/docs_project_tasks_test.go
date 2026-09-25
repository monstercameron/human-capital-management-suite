package productclient

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestResolveDocsProjectTaskPreviewsUsesAuthorizedCanonicalTargets(t *testing.T) {
	markdown := "[ready](/workspace/app/project?project=proj-a&task=task-1) " +
		"[denied](/workspace/app/project?project=proj-a&task=task-2) " +
		"[bare](task:task-3)"
	wantRefs := []productui.DocsProjectTaskReference{{ProjectID: "proj-a", TaskID: "task-1"}, {ProjectID: "proj-a", TaskID: "task-2"}}
	var calls []productui.DocsProjectTaskReference
	var callsMu sync.Mutex
	got := resolveDocsProjectTaskPreviews(context.Background(), markdown, func(_ context.Context, ref productui.DocsProjectTaskReference) (productui.DocsProjectTaskPreview, bool, error) {
		callsMu.Lock()
		calls = append(calls, ref)
		callsMu.Unlock()
		if ref.TaskID == "task-2" {
			return productui.DocsProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Private", Status: "Blocked"}, false, nil
		}
		return productui.DocsProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Ready", Status: "In progress"}, true, nil
	})
	sort.Slice(calls, func(i, j int) bool { return calls[i].TaskID < calls[j].TaskID })
	if !reflect.DeepEqual(calls, wantRefs) {
		t.Fatalf("resolved references = %+v, want %+v", calls, wantRefs)
	}
	want := []productui.DocsProjectTaskPreview{{ProjectID: "proj-a", TaskID: "task-1", Title: "Ready", Status: "In progress", Authorized: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("authorized previews = %+v, want %+v", got, want)
	}
}

func TestResolveDocsProjectTaskPreviewsDiscardsRevokedAndMismatchedResponses(t *testing.T) {
	markdown := "[revoked](/workspace/app/project?project=proj-a&task=task-1) " +
		"[mismatch](/workspace/app/project?project=proj-a&task=task-2)"
	got := resolveDocsProjectTaskPreviews(context.Background(), markdown, func(_ context.Context, ref productui.DocsProjectTaskReference) (productui.DocsProjectTaskPreview, bool, error) {
		if ref.TaskID == "task-1" {
			return productui.DocsProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Revoked", Status: "Blocked"}, false, nil
		}
		return productui.DocsProjectTaskPreview{ProjectID: "proj-other", TaskID: ref.TaskID, Title: "Wrong task", Status: "Blocked"}, true, nil
	})
	if len(got) != 0 {
		t.Fatalf("restricted or mismatched projections escaped the resolver: %+v", got)
	}
}
