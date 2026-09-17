package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ALIGN-045: default routes register against admitted capabilities. Every
// route binds an absolute path to the capability it requires and the page
// it renders; resolving a path checks the admitted set first, so an
// unadmitted route is refused before any page identity leaks, and an
// unknown path is refused without guessing.

// Route errors.
var (
	ErrRouteInvalid = errors.New("defaultproduct: default route is invalid")
	ErrRouteUnknown = errors.New("defaultproduct: route is not registered")
	ErrRouteDenied  = errors.New("defaultproduct: capability does not admit this route")
)

// Route is one seeded path binding.
type Route struct {
	Path       string `json:"path"`
	Capability string `json:"capability"`
	PageID     string `json:"page_id"`
}

// RouteTable is the seeded route set with its digest.
type RouteTable struct {
	Routes []Route `json:"routes"`
	Digest string  `json:"digest"`
}

// DefaultRoutes seeds the default routes against the seeded pages.
func DefaultRoutes() RouteTable {
	table := RouteTable{Routes: []Route{
		{Path: "/home", Capability: "product.view", PageID: "promotion.list.page"},
		{Path: "/promotion", Capability: "promotion.view", PageID: "promotion.list.page"},
		{Path: "/promotion/execute", Capability: "promotion.execute", PageID: "promotion.execute.page"},
		{Path: "/operations/repair", Capability: "operations.repair", PageID: "operations.repair.page"},
	}}
	table.Digest = table.computeDigest()
	return table
}

// Validate enforces unique absolute clean paths, required capabilities,
// and page references.
func (t RouteTable) Validate() error {
	seen := make(map[string]bool)
	for _, route := range t.Routes {
		if !strings.HasPrefix(route.Path, "/") || strings.Contains(route.Path, "..") || strings.TrimSpace(route.Path) != route.Path {
			return fmt.Errorf("%w: path %q is not absolute and clean", ErrRouteInvalid, route.Path)
		}
		if seen[route.Path] {
			return fmt.Errorf("%w: duplicate path %q", ErrRouteInvalid, route.Path)
		}
		seen[route.Path] = true
		if !safeID(route.Capability) || !safeID(route.PageID) {
			return fmt.Errorf("%w: route %q needs a capability and a page", ErrRouteInvalid, route.Path)
		}
	}
	return nil
}

// Resolve binds a path to its page for admitted capabilities. Unknown
// paths and unadmitted routes are refused with typed errors; the refusal
// for an unadmitted route names the route but never its page.
func (t RouteTable) Resolve(path string, admitted []string) (Route, error) {
	grants := make(map[string]bool, len(admitted))
	for _, capability := range admitted {
		grants[capability] = true
	}
	for _, route := range t.Routes {
		if route.Path != path {
			continue
		}
		if grants[route.Capability] {
			return route, nil
		}
		return Route{Path: route.Path}, fmt.Errorf("%w: %q", ErrRouteDenied, path)
	}
	return Route{}, fmt.Errorf("%w: %q", ErrRouteUnknown, path)
}

func (t RouteTable) computeDigest() string {
	routes := append([]Route(nil), t.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].Path < routes[j].Path })
	b, err := json.Marshal(routes)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the digest matches the seeded content.
func (t RouteTable) VerifyDigest() error {
	if t.Digest == "" || t.Digest != t.computeDigest() {
		return fmt.Errorf("%w: route table digest does not match its content", ErrRouteInvalid)
	}
	return nil
}
