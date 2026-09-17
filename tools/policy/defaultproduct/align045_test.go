package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_045 proves default routes register against admitted
// capabilities: admitted paths resolve, unadmitted paths are refused
// without leaking their page, and unknown paths are refused outright.
func TestTodo_ALIGN_045(t *testing.T) {
	table := DefaultRoutes()
	if err := table.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := table.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	route, err := table.Resolve("/promotion/execute", []string{"promotion.execute"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if route.PageID != "promotion.execute.page" {
		t.Fatalf("resolved to %q", route.PageID)
	}
	if _, err := table.Resolve("/no/such/path", alignAllCapabilities); !errors.Is(err, ErrRouteUnknown) {
		t.Fatalf("Resolve(unknown) = %v, want ErrRouteUnknown", err)
	}
}

func TestTodo_ALIGN_045_Property(t *testing.T) {
	first, second := DefaultRoutes(), DefaultRoutes()
	if first.Digest != second.Digest {
		t.Fatalf("route seed is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	// Resolution is stable across repeated calls.
	for i := 0; i < 2; i++ {
		route, err := first.Resolve("/promotion", []string{"promotion.view"})
		if err != nil || route.PageID != "promotion.list.page" {
			t.Fatalf("Resolve pass %d = %+v, %v", i, route, err)
		}
	}
}

func TestTodo_ALIGN_045_Golden(t *testing.T) {
	table := DefaultRoutes()
	const wantDigest = "sha256:b755ad41da19eb949ce14c296c6b76ef674074240bc24793ae967fb3b0b0f180"
	if table.Digest != wantDigest {
		t.Fatalf("route digest=%q want=%q", table.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_045_Security(t *testing.T) {
	table := DefaultRoutes()
	// An unadmitted route is refused, and the refusal carries no page.
	refused, err := table.Resolve("/operations/repair", []string{"promotion.view"})
	if !errors.Is(err, ErrRouteDenied) {
		t.Fatalf("Resolve(unadmitted) = %+v, %v, want ErrRouteDenied", refused, err)
	}
	if refused.PageID != "" {
		t.Fatalf("denied route leaked page %q", refused.PageID)
	}
	// Dirty paths never register: traversal and relative paths are
	// refused at validation.
	for _, path := range []string{"promotion", "/../etc", "/x/../y", ""} {
		dirty := DefaultRoutes()
		dirty.Routes = append(dirty.Routes, Route{Path: path, Capability: "promotion.view", PageID: "promotion.list.page"})
		if err := dirty.Validate(); !errors.Is(err, ErrRouteInvalid) {
			t.Fatalf("Validate(%q) = %v, want ErrRouteInvalid", path, err)
		}
	}
	// Duplicate paths are refused.
	duplicated := DefaultRoutes()
	duplicated.Routes = append(duplicated.Routes, duplicated.Routes[0])
	if err := duplicated.Validate(); !errors.Is(err, ErrRouteInvalid) {
		t.Fatalf("Validate(duplicate) = %v, want ErrRouteInvalid", err)
	}
}

func TestTodo_ALIGN_045_Conformance(t *testing.T) {
	table := DefaultRoutes()
	pages := DefaultPages()
	seeded := make(map[string]bool)
	for _, page := range pages.Pages {
		seeded[page.ID] = true
	}
	// Every default route renders a seeded work-loop page: no route
	// points outside the governed page set.
	for _, route := range table.Routes {
		if !seeded[route.PageID] {
			t.Fatalf("route %q page %q is not seeded", route.Path, route.PageID)
		}
	}
	// Every route resolves under the full capability set.
	for _, route := range table.Routes {
		resolved, err := table.Resolve(route.Path, alignAllCapabilities)
		if err != nil {
			t.Fatalf("Resolve(%q, full): %v", route.Path, err)
		}
		if resolved.PageID != route.PageID {
			t.Fatalf("Resolve(%q) = %q, want %q", route.Path, resolved.PageID, route.PageID)
		}
	}
}

func FuzzTodo_ALIGN_045_Fuzz(f *testing.F) {
	f.Add("/promotion", "promotion.view")
	f.Fuzz(func(t *testing.T, path, capability string) {
		table := DefaultRoutes()
		first, firstErr := table.Resolve(path, []string{capability})
		second, secondErr := table.Resolve(path, []string{capability})
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("resolution is not deterministic for %q", path)
		}
	})
}
