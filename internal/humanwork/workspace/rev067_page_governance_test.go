package workspace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// governedScope is the OrganizationScopeID the shell fixture credential
// carries (journey_shell_test.go), so it is the scope rollouts must name.
const governedScope = "org-north-america"

func publishGovernedPage(t *testing.T, h *Handler, page productui.PageID, version int64) productui.PageDefinitionRevision {
	t.Helper()
	definition, ok := productui.LookupPage(page)
	if !ok {
		t.Fatalf("LookupPage(%q) = not found", page)
	}
	revision, err := h.pages.PublishRevision(page, productui.SnapshotPageDefinition(definition), version)
	if err != nil {
		t.Fatalf("PublishRevision(%q, v%d): %v", page, version, err)
	}
	if err := h.pages.PublishRollout(productui.PageRollout{
		Page:    page,
		Version: version,
		Digest:  revision.Digest,
		Scopes:  []productui.RolloutScope{{Scope: governedScope}},
	}); err != nil {
		t.Fatalf("PublishRollout(%q, v%d): %v", page, version, err)
	}
	return revision
}

func getProductPage(t *testing.T, h *Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

func TestTodo_REV_067_01(t *testing.T) {
	h, token := newShellHandler(t, false)
	revision := publishGovernedPage(t, h, productui.PageHome, 1)

	response := getProductPage(t, h, token, PathProductHome)
	if response.Code != http.StatusOK {
		t.Fatalf("GET governed home = %d, want 200: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Page-Revision-Digest"); got != revision.Digest {
		t.Fatalf("served revision digest = %q, want rolled-out %q", got, revision.Digest)
	}

	ungoverned, governed, servable, _ := h.pages.Resolve(productui.PageWork, governedScope, shellNow.Unix())
	if governed || !servable {
		t.Fatalf("ungoverned page fallback = governed=%v servable=%v, want ungoverned fallback serve", governed, servable)
	}
	if governed && ungoverned.Digest != "" {
		t.Fatalf("ungoverned page returned a digest %q, want empty fallback", ungoverned.Digest)
	}

	if err := h.pages.PublishRetirement(productui.PageRetirement{Page: productui.PageHome, Reason: "rev-067-01 fault probe"}); err != nil {
		t.Fatalf("PublishRetirement: %v", err)
	}
	if response := getProductPage(t, h, token, PathProductHome); response.Code == http.StatusOK {
		t.Fatalf("GET retired home = 200, want refusal (digest %q)", response.Header().Get("X-Page-Revision-Digest"))
	}
}

func TestTodo_REV_067_01_Golden(t *testing.T) {
	h, token := newShellHandler(t, false)
	definition, _ := productui.LookupPage(productui.PageHome)
	snapshot := productui.SnapshotPageDefinition(definition)
	revision := publishGovernedPage(t, h, productui.PageHome, 1)

	if want := productui.DigestRevision(snapshot, 1); revision.Digest != want {
		t.Fatalf("recorded digest = %q, want recomputed %q", revision.Digest, want)
	}
	response := getProductPage(t, h, token, PathProductHome)
	if response.Code != http.StatusOK {
		t.Fatalf("GET governed home = %d, want 200", response.Code)
	}
	if got := response.Header().Get("X-Page-Revision-Digest"); got != revision.Digest {
		t.Fatalf("served digest = %q, want golden %q", got, revision.Digest)
	}

	tampered := productui.PageRollout{Page: productui.PageHome, Version: 1, Digest: "deadbeef", Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	if err := h.pages.PublishRollout(tampered); err == nil {
		t.Fatal("PublishRollout with a tampered digest succeeded, want digest-mismatch refusal")
	}
	served, governed, ok, _ := h.pages.Resolve(productui.PageHome, governedScope, shellNow.Unix())
	if !governed || !ok || served.Digest != revision.Digest {
		t.Fatalf("post-tamper resolve = digest %q governed=%v servable=%v, want golden %q", served.Digest, governed, ok, revision.Digest)
	}
}

func TestTodo_REV_067_01_Fault(t *testing.T) {
	h, token := newShellHandler(t, false)
	first := publishGovernedPage(t, h, productui.PageHome, 1)
	second := publishGovernedPage(t, h, productui.PageHome, 2)
	if first.Digest == second.Digest {
		t.Fatal("v1 and v2 digests collide, fault setup cannot distinguish them")
	}

	if response := getProductPage(t, h, token, PathProductHome); response.Header().Get("X-Page-Revision-Digest") != second.Digest {
		t.Fatalf("pre-rollback served digest = %q, want live v2 %q", response.Header().Get("X-Page-Revision-Digest"), second.Digest)
	}
	live := productui.PageRollout{Page: productui.PageHome, Version: 2, Digest: second.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	destination := productui.PageRollout{Page: productui.PageHome, Version: 1, Digest: first.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	if err := h.pages.PublishRollout(destination); err == nil {
		t.Fatal("backward rollout published as a plain rollout, want explicit-rollback refusal")
	}
	if err := h.pages.PublishRollback(live, destination); err != nil {
		t.Fatalf("PublishRollback to v1: %v", err)
	}
	rollback := getProductPage(t, h, token, PathProductHome)
	if rollback.Code != http.StatusOK {
		t.Fatalf("GET rolled-back home = %d, want 200 serving v1", rollback.Code)
	}
	if got := rollback.Header().Get("X-Page-Revision-Digest"); got != first.Digest {
		t.Fatalf("post-rollback served digest = %q, want v1 %q (v2 %q must never serve)", got, first.Digest, second.Digest)
	}

	if err := h.pages.PublishRetirement(productui.PageRetirement{Page: productui.PageHome, Reason: "rev-067-01 retirement fault"}); err != nil {
		t.Fatalf("PublishRetirement: %v", err)
	}
	if response := getProductPage(t, h, token, PathProductHome); response.Code == http.StatusOK {
		t.Fatal("GET retired home = 200, a retired page must never be served")
	}
}

func TestTodo_REV_067_01_Recovery(t *testing.T) {
	h, token := newShellHandler(t, false)
	rolledBack := publishGovernedPage(t, h, productui.PageHome, 1)
	rolledOver := publishGovernedPage(t, h, productui.PageHome, 2)
	live := productui.PageRollout{Page: productui.PageHome, Version: 2, Digest: rolledOver.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	destination := productui.PageRollout{Page: productui.PageHome, Version: 1, Digest: rolledBack.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	if err := h.pages.PublishRollback(live, destination); err != nil {
		t.Fatalf("PublishRollback to v1: %v", err)
	}

	recovered := publishGovernedPage(t, h, productui.PageHome, 3)
	response := getProductPage(t, h, token, PathProductHome)
	if response.Code != http.StatusOK {
		t.Fatalf("GET recovered home = %d, want 200 serving v3", response.Code)
	}
	if got := response.Header().Get("X-Page-Revision-Digest"); got != recovered.Digest {
		t.Fatalf("recovered digest = %q, want v3 %q", got, recovered.Digest)
	}
}
