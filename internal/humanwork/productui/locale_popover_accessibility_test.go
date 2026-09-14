package productui

import (
	"strings"
	"testing"
)

func TestLocalePopoverNamesEveryDestination(t *testing.T) {
	for _, selected := range SupportedProductLocales() {
		t.Run(selected, func(t *testing.T) {
			view := testView(PageHome)
			view.Locale = ResolveProductLocale(selected)
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, destination := range SupportedProductLocales() {
				name := ResolveProductLocale(destination).Text(productLocaleLabelKey(destination))
				for _, want := range []string{
					`aria-label="` + name + `"`,
					`lang="` + destination + `"`,
					`locale=` + destination,
				} {
					if !strings.Contains(doc, want) {
						t.Errorf("%s language destination missing %q", destination, want)
					}
				}
			}
		})
	}
}
