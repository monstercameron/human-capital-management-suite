package qual_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

func TestALIGN063QualificationProbesRejectRegressions(t *testing.T) {
	doc := `<!doctype html><html lang="de-DE" dir="rtl"><body><main>
<h1 id="title">مراجعة الزيادة</h1><label for="pay">Grundgehalt</label>
<input id="pay" aria-describedby="pay-help"><button aria-label="Speichern">✓</button>
</main></body></html>`
	if got := qual.CheckDocumentLocale(doc, "de-DE", "rtl"); !got.Pass {
		t.Fatalf("valid locale metadata rejected: %s", got.Detail)
	}
	if got := qual.CheckAssistiveNames(doc); !got.Pass {
		t.Fatalf("valid assistive names rejected: %s", got.Detail)
	}
	if got := qual.CheckLocalizedValues(doc, map[string]string{"salary": "93.000,00 €", "date": "1. Juni 2026", "percent": "+5,4 %"}); got.Pass {
		t.Fatal("missing localized values unexpectedly passed")
	}
	if got := qual.CheckDocumentLocale(`<html lang="en"><body><main></main></body></html>`, "en-US", "ltr"); got.Pass {
		t.Fatal("generic language and missing direction unexpectedly passed")
	}
	if got := qual.CheckAssistiveNames(`<html><body><main><button></button></main></body></html>`); got.Pass {
		t.Fatal("unnamed button unexpectedly passed")
	}
}

func TestALIGN063QualificationStylesheetProbes(t *testing.T) {
	css := `:where(.panel){max-width:100%;overflow-wrap:anywhere;}
.table{overflow-x:auto;} .actions{display:flex;flex-wrap:wrap;}
@media (prefers-reduced-motion:reduce){*{animation-duration:.001ms !important;animation-iteration-count:1 !important;scroll-behavior:auto !important;}}`
	if got := qual.CheckResponsiveAtWidths(css, 1440, 390, 320); !got.Pass {
		t.Fatalf("responsive matrix rejected: %s", got.Detail)
	}
	if got := qual.CheckReducedMotion(css); !got.Pass {
		t.Fatalf("reduced-motion rule rejected: %s", got.Detail)
	}
	if got := qual.CheckResponsiveAtWidths(`.panel{width:480px}`, 1440, 390, 320); got.Pass {
		t.Fatal("fixed-width regression unexpectedly passed")
	}
	if got := qual.CheckReducedMotion(`.panel{animation:spin 2s}`); got.Pass {
		t.Fatal("animation without reduced-motion override unexpectedly passed")
	}
}
