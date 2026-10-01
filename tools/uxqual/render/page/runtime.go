package page

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/ssrshell"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

// RuntimePageContract records the deterministic renderer evidence for one
// governed Promotion PageDefinition. It is metadata only: the live server's
// authorized projection remains the source of all displayed business data.
type RuntimePageContract struct {
	PageID               string `json:"page_id"`
	PageDefinitionDigest string `json:"page_definition_digest"`
	SemanticShellDigest  string `json:"semantic_shell_digest"`
	GWCPageDigest        string `json:"gwc_page_digest"`
}

// RuntimeContract is the small fail-closed bridge between the renderer
// qualification packages and the served Promotion GWC document. Keeping the
// bridge here means the server consumes the same governed widget, SSR shell,
// page renderer, and mode stylesheet contracts that their qualification
// suites exercise, without introducing a second business-data authority.
type RuntimeContract struct {
	Pages                []RuntimePageContract `json:"pages"`
	WidgetRegistryDigest string                `json:"widget_registry_digest"`
	StylesheetDigest     string                `json:"stylesheet_digest"`
	Digest               string                `json:"digest"`
}

type runtimeContractResult struct {
	Contract RuntimeContract
	Err      error
}

var promotionRuntimeContract = sync.OnceValue(func() runtimeContractResult {
	return buildPromotionRuntimeContract()
})

// BuildPromotionRuntimeContract returns the immutable renderer contract used
// by the served Promotion GWC document. A renderer drift or an unresolvable
// widget is an error, so the caller can fail closed instead of serving a
// page that was never admitted by the governed contracts.
func BuildPromotionRuntimeContract() (RuntimeContract, error) {
	result := promotionRuntimeContract()
	if result.Err != nil {
		return RuntimeContract{}, result.Err
	}
	contract := result.Contract
	contract.Pages = append([]RuntimePageContract(nil), contract.Pages...)
	return contract, nil
}

func buildPromotionRuntimeContract() runtimeContractResult {
	widgetRegistry := widgetreg.PromotionRegistry()
	pageRegistry := PromotionWidgetRegistry()
	floorplanRegistry := floorplan.PromotionRegistry()
	definitions := []pagedef.PageDefinition{
		pagedef.PromotionListPageDefinition(),
		pagedef.PromotionDetailPageDefinition(),
	}

	pages := make([]RuntimePageContract, 0, len(definitions))
	for _, definition := range definitions {
		if _, err := widgetRegistry.ResolvePage(definition); err != nil {
			return runtimeContractResult{Err: fmt.Errorf("page: governed widget resolution for %q: %w", definition.PageID, err)}
		}
		resolution, err := floorplanRegistry.Resolve(definition)
		if err != nil {
			return runtimeContractResult{Err: fmt.Errorf("page: governed floorplan resolution for %q: %w", definition.PageID, err)}
		}

		node, err := Render(resolution, pageRegistry)
		if err != nil {
			return runtimeContractResult{Err: fmt.Errorf("page: GWC render for %q: %w", definition.PageID, err)}
		}
		gwcBytes, err := ui.RenderToString(node)
		if err != nil {
			return runtimeContractResult{Err: fmt.Errorf("page: GWC serialization for %q: %w", definition.PageID, err)}
		}
		shell, err := ssrshell.Render(definition)
		if err != nil {
			return runtimeContractResult{Err: fmt.Errorf("page: semantic SSR render for %q: %w", definition.PageID, err)}
		}
		pages = append(pages, RuntimePageContract{
			PageID:               definition.PageID,
			PageDefinitionDigest: definition.Digest(),
			SemanticShellDigest:  shell.Digest,
			GWCPageDigest:        digestBytes([]byte(gwcBytes)),
		})
	}

	stylesheet := tokens.WorkspaceCSS()
	if stylesheet == "" {
		return runtimeContractResult{Err: fmt.Errorf("page: governed mode stylesheet is empty")}
	}
	for _, marker := range []string{
		"@media (prefers-contrast:more)",
		"@media (forced-colors:active)",
		"@media print",
	} {
		if !strings.Contains(stylesheet, marker) {
			return runtimeContractResult{Err: fmt.Errorf("page: governed mode stylesheet is missing %q", marker)}
		}
	}
	contract := RuntimeContract{
		Pages:                pages,
		WidgetRegistryDigest: widgetRegistry.Digest(),
		StylesheetDigest:     digestBytes([]byte(stylesheet)),
	}
	canonical, err := json.Marshal(struct {
		Schema  string          `json:"schema"`
		Version int             `json:"version"`
		Value   RuntimeContract `json:"value"`
	}{Schema: "hcmnext.uxqual.render.page.runtime", Version: 1, Value: contract})
	if err != nil {
		return runtimeContractResult{Err: fmt.Errorf("page: encode runtime contract: %w", err)}
	}
	contract.Digest = digestBytes(canonical)
	return runtimeContractResult{Contract: contract}
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
