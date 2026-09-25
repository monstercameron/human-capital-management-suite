package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatProjectTaskPreviewCacheReplacesTitleAfterAccessRevocation(t *testing.T) {
	cache := &chatProjectTaskPreviewCache{}
	ref := chatui.ProjectTaskReference{ProjectID: "proj-7", TaskID: "task-19"}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	claims, epoch, shown := cache.claim("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, now)
	if len(claims) != 1 || shown[chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)].State != "loading" {
		t.Fatalf("first claim = claims:%+v shown:%+v", claims, shown)
	}
	ready := chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "Confidential launch", Readable: true, State: "ready"}
	key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
	if !cache.finish("tenant-a\x00reader-a", epoch, map[string]chatui.ProjectTaskPreview{key: ready}, now.Add(time.Second)) {
		t.Fatal("authorized read was discarded")
	}
	if got := cache.projection("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, now.Add(2*time.Second)); got[key].Title != ready.Title {
		t.Fatalf("fresh projection omitted authorized title: %+v", got)
	}
	if got := cache.projection("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, now.Add(chatProjectTaskPreviewFreshFor+2*time.Second)); len(got) != 0 {
		t.Fatalf("expired title survived render projection: %+v", got)
	}

	revokedAt := now.Add(chatProjectTaskPreviewFreshFor + time.Second)
	claims, epoch, shown = cache.claim("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, revokedAt)
	if len(claims) != 1 || shown[key].State != "loading" || shown[key].Title != "" {
		t.Fatalf("revalidation claim = claims:%+v shown:%+v", claims, shown)
	}
	denied := restrictedChatProjectTaskAnswer(ref)
	if !cache.finish("tenant-a\x00reader-a", epoch, map[string]chatui.ProjectTaskPreview{key: denied}, revokedAt.Add(time.Second)) {
		t.Fatal("revocation read was discarded")
	}
	claims, _, shown = cache.claim("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, revokedAt.Add(2*time.Second))
	if len(claims) != 0 || shown[key].State != "restricted" || shown[key].Title != "" || shown[key].Readable {
		t.Fatalf("revoked task preview retained title/access: claims:%+v preview:%+v", claims, shown[key])
	}
	model := chatui.Model{EmbedOrigin: "https://hcm.example", ProjectTaskPreviews: shown}
	nodes := chatui.ProjectTaskReferenceBody("https://hcm.example"+chatui.ProjectTaskReferenceURL(ref.ProjectID, ref.TaskID), model.EmbedOrigin, "Project task unavailable", model.ProjectTaskPreviews)
	markup, err := ui.RenderToString(html.Div(html.Props{}, nodes...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Confidential launch") || strings.Contains(markup, ref.TaskID) || !strings.Contains(markup, "Project task unavailable") {
		t.Fatalf("revoked preview leaked title or id: %s", markup)
	}
}

func TestChatProjectTaskPreviewCacheDropsTitlesWhenViewerChanges(t *testing.T) {
	cache := &chatProjectTaskPreviewCache{}
	ref := chatui.ProjectTaskReference{ProjectID: "proj-7", TaskID: "task-19"}
	key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
	now := time.Now()
	_, epoch, _ := cache.claim("tenant-a\x00reader-a", []chatui.ProjectTaskReference{ref}, now)
	cache.finish("tenant-a\x00reader-a", epoch, map[string]chatui.ProjectTaskPreview{key: {ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: "A title", Readable: true, State: "ready"}}, now)
	_, _, shown := cache.claim("tenant-a\x00reader-b", []chatui.ProjectTaskReference{ref}, now.Add(time.Second))
	if got := shown[key]; got.Title != "" || got.State != "loading" {
		t.Fatalf("new viewer inherited prior preview: %+v", got)
	}
	cache.reset()
	if got := cache.projection("tenant-a\x00reader-b", []chatui.ProjectTaskReference{ref}, now.Add(2*time.Second)); len(got) != 0 {
		t.Fatalf("reset cache exposed a previous projection: %+v", got)
	}
}

func TestChatProjectTaskHrefUsesPersistentInAppRoute(t *testing.T) {
	href := "https://hcm.example" + chatui.ProjectTaskReferenceURL("proj-7", "task-19")
	got, ok := productLinkFallbackTarget(productLinkFallbackAnchor{Href: href, Origin: "https://hcm.example", Current: "/workspace/app/chat"})
	want := "/workspace/app/project?project=proj-7&task=task-19"
	if !ok || got != want {
		t.Fatalf("task link route = %q, %v; want %q, true", got, ok, want)
	}
}
