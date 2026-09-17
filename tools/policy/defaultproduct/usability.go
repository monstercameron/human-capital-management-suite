package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ALIGN-046: the default product is usable with zero overrides. The
// composition assembles the seeded tokens, shell, pages, failure pages,
// and routes for one admitted capability set and proves every required
// slot is filled. Optional overrides may replace labels or routes, but an
// override that drops a required slot or admits an unadmitted capability
// is refused, so customization cannot break usability.

// Composition errors.
var (
	ErrCompositionInvalid = errors.New("defaultproduct: product composition is invalid")
)

// Composition is the assembled default product for one capability set.
type Composition struct {
	Capabilities []string        `json:"capabilities"`
	Shell        Shell           `json:"shell"`
	Pages        PageRegistry    `json:"pages"`
	Failures     FailureRegistry `json:"failures"`
	Routes       RouteTable      `json:"routes"`
	TokensDigest string          `json:"tokens_digest"`
	Digest       string          `json:"digest"`
}

// CompositionOverride replaces one shell label or one route page. Empty
// fields leave the default in place; a replacement must still satisfy the
// composition rules.
type CompositionOverride struct {
	ShellEntryID string `json:"shell_entry_id"`
	Label        string `json:"label"`
	RoutePath    string `json:"route_path"`
	PageID       string `json:"page_id"`
}

// DefaultComposition assembles the zero-override product for admitted
// capabilities and proves it is usable.
func DefaultComposition(admitted []string) (Composition, error) {
	tokens := DefaultTokens()
	if err := tokens.Validate(); err != nil {
		return Composition{}, err
	}
	pages := DefaultPages()
	if err := pages.Validate(); err != nil {
		return Composition{}, err
	}
	failures := DefaultFailurePages()
	if err := failures.Validate(); err != nil {
		return Composition{}, err
	}
	routes := DefaultRoutes()
	if err := routes.Validate(); err != nil {
		return Composition{}, err
	}
	shell, err := ResolveShell(DefaultShellEntries(), admitted)
	if err != nil {
		return Composition{}, err
	}
	composition := Composition{
		Capabilities: append([]string(nil), admitted...),
		Shell:        shell,
		Pages:        pages,
		Failures:     failures,
		Routes:       routes,
		TokensDigest: tokens.Digest,
	}
	composition.Digest = composition.computeDigest()
	if err := composition.Validate(); err != nil {
		return Composition{}, err
	}
	return composition, nil
}

// Validate proves usability: the shell renders at least one entry, every
// admitted route resolves to a seeded page, and the token pin matches the
// seed.
func (c Composition) Validate() error {
	if len(c.Shell.Entries) == 0 {
		return fmt.Errorf("%w: resolved shell renders nothing", ErrCompositionInvalid)
	}
	if err := c.Pages.VerifyDigest(); err != nil {
		return err
	}
	if err := c.Failures.VerifyDigest(); err != nil {
		return err
	}
	if err := c.Routes.VerifyDigest(); err != nil {
		return err
	}
	if c.TokensDigest != DefaultTokens().Digest {
		return fmt.Errorf("%w: token pin does not match the seed", ErrCompositionInvalid)
	}
	pages := make(map[string]bool)
	for _, page := range c.Pages.Pages {
		pages[page.ID] = true
	}
	for _, route := range c.Routes.Routes {
		if !pages[route.PageID] {
			return fmt.Errorf("%w: route %q page %q is not seeded", ErrCompositionInvalid, route.Path, route.PageID)
		}
	}
	return nil
}

// WithOverride applies one optional override and re-proves usability. An
// override that empties a label, points a route at an unseeded page, or
// drops the last shell entry is refused.
func (c Composition) WithOverride(override CompositionOverride) (Composition, error) {
	out := c
	if override.ShellEntryID != "" {
		found := false
		for i, entry := range out.Shell.Entries {
			if entry.ID == override.ShellEntryID {
				if override.Label != "" {
					out.Shell.Entries[i].Label = override.Label
				}
				found = true
			}
		}
		if !found {
			return Composition{}, fmt.Errorf("%w: shell entry %q is not rendered", ErrCompositionInvalid, override.ShellEntryID)
		}
	}
	if override.RoutePath != "" {
		found := false
		for i, route := range out.Routes.Routes {
			if route.Path == override.RoutePath {
				if override.PageID != "" {
					out.Routes.Routes[i].PageID = override.PageID
				}
				found = true
			}
		}
		if !found {
			return Composition{}, fmt.Errorf("%w: route %q is not registered", ErrCompositionInvalid, override.RoutePath)
		}
		out.Routes.Digest = out.Routes.computeDigest()
	}
	out.Digest = out.computeDigest()
	if err := out.Validate(); err != nil {
		return Composition{}, err
	}
	return out, nil
}

func (c Composition) computeDigest() string {
	b, err := json.Marshal(struct {
		Capabilities []string        `json:"capabilities"`
		Shell        Shell           `json:"shell"`
		Pages        PageRegistry    `json:"pages"`
		Failures     FailureRegistry `json:"failures"`
		Routes       RouteTable      `json:"routes"`
		TokensDigest string          `json:"tokens_digest"`
	}{c.Capabilities, c.Shell, c.Pages, c.Failures, c.Routes, c.TokensDigest})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
