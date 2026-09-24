package workspace_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// TestTodo_UX_002_Integration is the integration-level evidence for
// planning/todos.md's UX-002: the document a composed cell over a live
// PostgreSQL actually serves is the GoWebComponents renderer's output, built
// from the server's workspace contract, and when the progressive-enhancement
// bundle is built in, the data island the browser renderer reads carries the
// very masked contract the rendered tree shows.
func TestTodo_UX_002_Integration(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	page := c.get(promotionURL, compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200\n%s", promotionURL, page.Status, page.Body)
	}
	_ = page.sessionCookie(t)

	t.Run("the served document carries the GWC renderer's provenance", func(t *testing.T) {
		// GWC emits its attributes in alphabetical order, so the bound request
		// form is action, id, method. The frozen template's binding writes id
		// first. The two shapes are mutually exclusive, which makes this a
		// discriminator between the two qualified renderers.
		gwcForm := regexp.MustCompile(`<form action="` + regexp.QuoteMeta(workspace.PathSimulate) + `" id="[^"]+" method="post">`)
		if !gwcForm.MatchString(page.Body) {
			t.Error("the served page does not carry the GWC renderer's bound request form")
		}
		if regexp.MustCompile(`<form id="[^"]+" method="post" action="`).MatchString(page.Body) {
			t.Error("the served page carries the frozen template's bound request form")
		}
		if strings.Contains(page.Body, `action="#"`) {
			t.Error("the served page still carries the renderer's placeholder form action")
		}

		// The session the page belongs to is bound without script: the served
		// csrf token and the addressed worker are in the request form.
		if !strings.Contains(page.Body, `name="`+workspace.ParamCSRF+`" value="`+page.csrfToken(t)+`"`) {
			t.Error("the served page does not carry the session csrf token in the request form")
		}
		if !strings.Contains(page.Body, `name="`+workspace.ParamWorker+`" value="`+testWorker+`"`) {
			t.Error("the served page does not carry the addressed worker in the request form")
		}

		if workspace.BundleBuilt() {
			_, raw := ux002IslandFromBody(t, page.Body)
			_ = raw
		}
	})

	t.Run("the live document scores against UX-QUAL-001's own criteria", func(t *testing.T) {
		result := qual.RunDocumentChecks("served-workspace", page.Body, workspace.MaskedActionNeedles())
		for _, criterion := range result.All() {
			if criterion.Name == "" {
				continue
			}
			if !criterion.Pass {
				t.Errorf("%s: FAIL %s", criterion.Name, criterion.Detail)
			}
		}
	})

	t.Run("the data island carries the same masked contract the tree renders", func(t *testing.T) {
		if !workspace.BundleBuilt() {
			t.Skip("the enhancement bundle is not built into this binary")
		}
		island, raw := ux002IslandFromBody(t, page.Body)

		if island.Binding.Action != workspace.PathSimulate {
			t.Errorf("the island's binding targets %q, want %q", island.Binding.Action, workspace.PathSimulate)
		}
		if island.Binding.FormID == "" {
			t.Error("the island's binding names no request form")
		}
		if island.Binding.Hidden[workspace.ParamCSRF] != page.csrfToken(t) {
			t.Error("the island's binding does not carry the session csrf token the page was bound with")
		}
		if island.Binding.Hidden[workspace.ParamWorker] != testWorker {
			t.Errorf("the island's binding carries worker %q, want %q", island.Binding.Hidden[workspace.ParamWorker], testWorker)
		}
		// The form the island names is the one the document actually bound,
		// and the document's action controls name it as their form owner.
		if !strings.Contains(page.Body, `id="`+island.Binding.FormID+`"`) {
			t.Error("the document does not contain the request form the island names")
		}
		if !strings.Contains(page.Body, `form="`+island.Binding.FormID+`"`) {
			t.Error("the document's action controls do not name the island's request form as their form owner")
		}

		// The island is masked exactly as the rendered tree is.
		for _, needle := range workspace.MaskedActionNeedles() {
			if strings.Contains(raw, needle) {
				t.Errorf("the island leaks the masked action string %q", needle)
			}
		}

		// A principal the policy will not disclose compensation to receives an
		// island without the compensation half, the way the tree omits it.
		masked := c.get(promotionURL, operations.name)
		if masked.Status != http.StatusOK {
			t.Fatalf("GET %s as %s answered %d, want 200\n%s", promotionURL, operations.name, masked.Status, masked.Body)
		}
		_, maskedRaw := ux002IslandFromBody(t, masked.Body)
		for _, needle := range []string{
			workspace.FieldProposedComp, "93000.00", "BAND-OPS-P3-USEAST",
		} {
			if strings.Contains(maskedRaw, needle) {
				t.Errorf("the island leaks %q to a principal without the compensation grant", needle)
			}
		}
		if !strings.Contains(masked.Body, "OPS-HRBP2") {
			t.Error("masking is not refusal: the worker's placement is still disclosed")
		}
	})

	// Nothing above may have written anything.
	if after := c.fingerprint(); after != before {
		t.Fatalf("the workspace changed the database\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestTodo_UX_002_Browser checks what a browser actually evaluates from the
// served document: the complete script inventory, and that the document never
// references a runtime it was not served. By the workspace's testing
// convention (see TestTodo_UX_004_Browser) this is a Go-side check of the
// live response, so it needs no JS engine to run.
func TestTodo_UX_002_Browser(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	page := c.get(promotionURL, compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200\n%s", promotionURL, page.Status, page.Body)
	}

	// The complete script inventory of the served document.
	blocks := scriptRE.FindAllString(page.Body, -1)
	if !workspace.BundleBuilt() {
		if len(blocks) != 0 {
			t.Errorf("the unbundled document carries %d script elements, want none", len(blocks))
		}
	} else {
		// Exactly two: the contract data island and the pinned loader.
		if len(blocks) != 2 {
			t.Fatalf("the served document carries %d script elements, want exactly 2 (the data island and the pinned loader):\n%s", len(blocks), page.Body)
		}
		var (
			islands, loaders int
		)
		for _, block := range blocks {
			if strings.HasPrefix(block, ux002IslandOpen) {
				islands++
				continue
			}
			if strings.Contains(block, `src=`) {
				t.Error("the served document references an external script; the runtime is self-contained")
			}
			if !strings.Contains(block, "WebAssembly.instantiateStreaming") {
				t.Error("the served document's second script element is not the progressive-enhancement loader")
			}
			if !strings.Contains(block, workspace.PathWasm) {
				t.Error("the loader does not instantiate this workspace's own bundle")
			}
			loaders++
		}
		if islands != 1 || loaders != 1 {
			t.Errorf("the served document carries %d data island(s) and %d loader script(s), want one of each", islands, loaders)
		}
	}

	// Nothing in the document reaches outside itself: no external URL of any
	// kind, on any element or in any embedded value.
	if m := externalURLEnabledRE.FindString(page.Body); m != "" {
		t.Errorf("the served document references %q; the document must not reach another origin", m)
	}

	// No masked action is offered, masked or not.
	for _, needle := range workspace.MaskedActionNeedles() {
		if strings.Contains(page.Body, needle) {
			t.Errorf("the rendered workspace leaks the masked action string %q", needle)
		}
	}

	// The form works with no runtime at all: it posts to the workspace's own
	// route and carries its session inputs.
	if !strings.Contains(page.Body, `action="`+workspace.PathSimulate+`"`) {
		t.Error("the served form does not post to the workspace simulate route")
	}
	if !strings.Contains(page.Body, `name="`+workspace.ParamCSRF+`"`) {
		t.Error("the served form does not carry the session csrf input")
	}

	if after := c.fingerprint(); after != before {
		t.Fatalf("the workspace changed the database\nbefore: %s\nafter:  %s", before, after)
	}
}

// scriptRE isolates every script element of a document.
var scriptRE = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)

// externalURLEnabledRE names a scheme the document would fetch from outside
// itself.
var externalURLEnabledRE = regexp.MustCompile(`(https?|file|data|blob|ftp)://[^\s"'<>]`)

// ux002Island is the data-island protocol the enhanced document must carry:
// the same masked contract the rendered tree shows, plus exactly what the
// live renderer needs to re-bind that tree to this workspace's routes. The
// field names are the wire contract.
type ux002Island struct {
	Contract contract.WorkspaceContract `json:"contract"`
	Binding  struct {
		Action string            `json:"action"`
		FormID string            `json:"form_id"`
		Hidden map[string]string `json:"hidden"`
	} `json:"binding"`
}

const ux002IslandOpen = `<script type="application/json" id="gwc-contract">`

// ux002IslandFromBody reads the data island out of a served document and
// decodes it, returning the island and its raw JSON.
func ux002IslandFromBody(t *testing.T, doc string) (ux002Island, string) {
	t.Helper()
	idx := strings.Index(doc, ux002IslandOpen)
	if idx < 0 {
		t.Fatalf("the served document carries no contract data island\n%s", doc)
	}
	start := idx + len(ux002IslandOpen)
	end := strings.Index(doc[start:], "</script>")
	if end < 0 {
		t.Fatal("the data island is not closed")
	}
	raw := doc[start : start+end]
	var island ux002Island
	if err := json.Unmarshal([]byte(raw), &island); err != nil {
		t.Fatalf("decode the data island: %v\n%s", err, raw)
	}
	return island, raw
}
