//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_REV_069_03_Browser(t *testing.T) {
	global := js.Global()
	previous := global.Get("matchMedia")
	media := global.Get("Object").New()
	width := 1024
	media.Set("matches", false)
	var change js.Value
	addListener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			change = args[1]
		}
		return nil
	})
	removeListener := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	media.Set("addEventListener", addListener)
	media.Set("removeEventListener", removeListener)
	matchMedia := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "(max-width: 760px)" {
			t.Fatalf("unexpected responsive query: %v", args)
		}
		media.Set("matches", width <= 760)
		return media
	})
	global.Set("matchMedia", matchMedia)
	t.Cleanup(func() {
		global.Set("matchMedia", previous)
		matchMedia.Release()
		addListener.Release()
		removeListener.Release()
	})

	props := rev06903ResponsiveWorkProps(productui.ResolveProductLocale("en-US"))
	props.Layout = productui.ListDetailWide
	props.HasSelection = false
	props.ShowList, props.ShowDetail = true, true
	props.Collection.Rows[0].Href = "/workspace/app/work?selected=work-1"
	selected := false
	var navigated string
	navigate := func(href string) { navigated = href }
	props.Collection.Rows[0].Navigate = navigate
	props.BackToList.Navigate = navigate
	fixture := render.New(t)
	defer fixture.Cleanup()
	fixture.Render(ui.CreateElement(productui.WorkPage, props))
	fixture.Stabilize()
	if !strings.Contains(fixture.Text(), "Promotion journeys") {
		t.Fatal("desktop list did not render an actionable row")
	}
	var row *render.QueryNode
	for _, link := range fixture.AllByRole("link") {
		if strings.Contains(link.Text(), "Jordan Lee") {
			row = link
			break
		}
	}
	if row == nil {
		t.Fatalf("could not find actionable Jordan Lee row: %s", fixture.Text())
	}
	row.Click()
	if navigated != "/workspace/app/work?selected=work-1" {
		t.Fatalf("clicking the actionable row navigated to %q", navigated)
	}
	selected = true
	props.HasSelection = selected
	props.Collection.Rows[0].Selected = selected
	fixture.Rerender(ui.CreateElement(productui.WorkPage, props))
	fixture.Stabilize()

	if change.IsUndefined() || change.IsNull() {
		t.Fatal("responsive component did not subscribe to matchMedia changes")
	}
	width = 390
	media.Set("matches", true)
	change.Invoke()
	fixture.Stabilize()
	if strings.Contains(fixture.Text(), "Promotion journeys") || !strings.Contains(fixture.Text(), "Selected assignment") {
		t.Fatalf("390px selected state did not leave only the detail pane: %s", fixture.Text())
	}
	back := fixture.ByRole("link", "Back to work")
	if back == nil || back.Attr("href") != "/workspace/app/work?filter=" {
		t.Fatalf("390px detail has no keyboard return path: %#v", back)
	}
	back.Click()
	if navigated != "/workspace/app/work?filter=" {
		t.Fatalf("back-to-list click navigated to %q", navigated)
	}
	selected = false
	props.HasSelection = selected
	props.Collection.Rows[0].Selected = selected
	fixture.Rerender(ui.CreateElement(productui.WorkPage, props))
	fixture.Stabilize()
	if !strings.Contains(fixture.Text(), "Promotion journeys") || strings.Contains(fixture.Text(), "Selected assignment") {
		t.Fatalf("narrow return action did not restore only the list pane: %s", fixture.Text())
	}

	// Reopen the detail by clicking the same actionable row at 390px; the
	// viewport decides pane visibility while selection continues to come from
	// the route callback, just as it does in the product router.
	row = nil
	for _, link := range fixture.AllByRole("link") {
		if strings.Contains(link.Text(), "Jordan Lee") {
			row = link
			break
		}
	}
	if row == nil {
		t.Fatalf("390px list did not restore the actionable row: %s", fixture.Text())
	}
	row.Click()
	selected = true
	props.HasSelection = selected
	props.Collection.Rows[0].Selected = selected
	fixture.Rerender(ui.CreateElement(productui.WorkPage, props))
	fixture.Stabilize()

	width = 320
	// Both narrow widths satisfy the same media query, so no breakpoint
	// transition is expected here; selection must still keep the list absent.
	if !media.Get("matches").Bool() || strings.Contains(fixture.Text(), "Promotion journeys") || !strings.Contains(fixture.Text(), "Selected assignment") {
		t.Fatalf("320px selected state did not leave only the detail pane: %s", fixture.Text())
	}
	width = 1024
	media.Set("matches", false)
	change.Invoke()
	fixture.Stabilize()
	if !strings.Contains(fixture.Text(), "Promotion journeys") || !strings.Contains(fixture.Text(), "Selected assignment") {
		t.Fatalf("desktop resize did not restore both panes after row selection: %s", fixture.Text())
	}
	if fixture.ByRole("link", "Back to work") != nil {
		t.Fatal("desktop layout kept the narrow-only return control")
	}
}

func rev06903ResponsiveWorkProps(locale productui.LocaleContext) productui.WorkPageProps {
	return productui.WorkPageProps{
		I18nProps: productui.I18nProps{Locale: locale},
		Collection: productui.WorkCollectionProps{
			Title: "Promotion journeys", Kind: "action-queue", CountLabel: "1 item",
			Rows: []productui.WorkRowProps{{ID: "work-1", Title: "Promotion", Person: "Jordan Lee", Href: "/workspace/app/work?selected=work-1"}},
		},
		Preview: productui.WorkPreviewProps{ID: "work-1", Title: "Promotion", Person: "Jordan Lee"},
		Layout:  productui.ListDetailWide, HasSelection: false, ShowList: true, ShowDetail: true,
		PaneVisibilityResolved: true,
		BackToList:             productui.ActionLinkProps{Label: "Back to work", Href: "/workspace/app/work?filter="},
	}
}
