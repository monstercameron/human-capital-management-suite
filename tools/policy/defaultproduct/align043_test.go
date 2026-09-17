package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_043 proves the governed work-loop pages route intent
// actions to pages for admitted capabilities, and refuse everything else.
func TestTodo_ALIGN_043(t *testing.T) {
	pages := DefaultPages()
	if err := pages.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := pages.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	page, err := pages.RouteFor("promotion.execute", []string{"promotion.execute"})
	if err != nil {
		t.Fatalf("RouteFor: %v", err)
	}
	if page.ID != "promotion.execute.page" {
		t.Fatalf("routed to %q", page.ID)
	}
	// Without the capability the action routes to its fallback, refused.
	fallback, err := pages.RouteFor("promotion.execute", []string{"promotion.view"})
	if !errors.Is(err, ErrPageDenied) {
		t.Fatalf("RouteFor(denied) = %+v, %v, want ErrPageDenied", fallback, err)
	}
	if fallback.ID != "promotion.detail.page" {
		t.Fatalf("denied fallback = %q", fallback.ID)
	}
	// Unknown actions are refused without a page.
	if _, err := pages.RouteFor("payroll.forge", alignAllCapabilities); !errors.Is(err, ErrPageUnknown) {
		t.Fatalf("RouteFor(unknown) = %v, want ErrPageUnknown", err)
	}
}

func TestTodo_ALIGN_043_Property(t *testing.T) {
	first, second := DefaultPages(), DefaultPages()
	if first.Digest != second.Digest {
		t.Fatalf("page seed is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	// Routing is stable across repeated calls.
	for i := 0; i < 2; i++ {
		page, err := first.RouteFor("promotion.list", []string{"promotion.view"})
		if err != nil || page.ID != "promotion.list.page" {
			t.Fatalf("RouteFor pass %d = %+v, %v", i, page, err)
		}
	}
}

func TestTodo_ALIGN_043_Golden(t *testing.T) {
	pages := DefaultPages()
	const wantDigest = "sha256:52b2ebef2c24912f7b1cd84822433cbe9650e60df27262077ceb8d3203530683"
	if pages.Digest != wantDigest {
		t.Fatalf("page digest=%q want=%q", pages.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_043_Security(t *testing.T) {
	pages := DefaultPages()
	// A denied routing never returns the protected page itself.
	page, err := pages.RouteFor("operations.repair", []string{"promotion.view"})
	if !errors.Is(err, ErrPageDenied) {
		t.Fatalf("RouteFor = %+v, %v, want ErrPageDenied", page, err)
	}
	if page.ID == "operations.repair.page" {
		t.Fatal("protected page returned on a denied routing")
	}
	// Duplicate page IDs are refused at validation.
	duplicated := DefaultPages()
	duplicated.Pages = append(duplicated.Pages, duplicated.Pages[0])
	if err := duplicated.Validate(); !errors.Is(err, ErrPageInvalid) {
		t.Fatalf("Validate(duplicate) = %v, want ErrPageInvalid", err)
	}
	// A dangling fallback is refused at validation.
	dangling := DefaultPages()
	dangling.Pages[0].FallbackPage = "ghost.page"
	if err := dangling.Validate(); !errors.Is(err, ErrPageInvalid) {
		t.Fatalf("Validate(dangling) = %v, want ErrPageInvalid", err)
	}
}

func TestTodo_ALIGN_043_Conformance(t *testing.T) {
	pages := DefaultPages()
	tokens := DefaultTokens()
	floorplans := make(map[string]bool)
	for _, plan := range tokens.Floorplans {
		floorplans[plan.ID] = true
	}
	// Every work-loop page composes onto a seeded floorplan: no page
	// renders outside the platform layout contract.
	for _, page := range pages.Pages {
		if !floorplans[page.Floorplan] {
			t.Fatalf("page %q floorplan %q is not seeded", page.ID, page.Floorplan)
		}
	}
	// Every fallback terminates at a seeded page or the failure root.
	seeded := make(map[string]bool)
	for _, page := range pages.Pages {
		seeded[page.ID] = true
	}
	for _, page := range pages.Pages {
		if !seeded[page.FallbackPage] && page.FallbackPage != "failure.unauthorized.page" {
			t.Fatalf("page %q fallback %q does not terminate", page.ID, page.FallbackPage)
		}
	}
}

func FuzzTodo_ALIGN_043_Fuzz(f *testing.F) {
	f.Add("promotion.execute", "promotion.execute")
	f.Fuzz(func(t *testing.T, action, capability string) {
		pages := DefaultPages()
		first, firstErr := pages.RouteFor(action, []string{capability})
		second, secondErr := pages.RouteFor(action, []string{capability})
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("routing is not deterministic for %q", action)
		}
	})
}
