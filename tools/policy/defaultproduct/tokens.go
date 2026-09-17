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

// ALIGN-041: platform semantic tokens and floorplans are seeded, owned, and
// digest-pinned. Tokens name the closed visual language (color, spacing,
// type, radius, elevation); floorplans name the page regions a shell may
// place. Neither carries product data, only the contracts surfaces agree
// on before any page renders.

// Token errors.
var (
	ErrTokenInvalid = errors.New("defaultproduct: semantic token is invalid")
	ErrTokenUnknown = errors.New("defaultproduct: semantic token is not seeded")
)

// TokenKind is the closed set of semantic token kinds.
type TokenKind string

// Admitted token kinds.
const (
	TokenColor     TokenKind = "color"
	TokenSpacing   TokenKind = "spacing"
	TokenType      TokenKind = "type"
	TokenRadius    TokenKind = "radius"
	TokenElevation TokenKind = "elevation"
)

// SemanticToken is one named design contract: its value is the canonical
// reference (hex, scale step, type ramp, radius, elevation) every surface
// implements.
type SemanticToken struct {
	ID    string    `json:"id"`
	Kind  TokenKind `json:"kind"`
	Value string    `json:"value"`
	Owner string    `json:"owner"`
}

// Floorplan names the regions of one page composition.
type Floorplan struct {
	ID      string   `json:"id"`
	Regions []string `json:"regions"`
	Owner   string   `json:"owner"`
}

// TokenRegistry is the seeded token and floorplan set with its digest.
type TokenRegistry struct {
	Tokens     []SemanticToken `json:"tokens"`
	Floorplans []Floorplan     `json:"floorplans"`
	Digest     string          `json:"digest"`
}

func safeID(s string) bool {
	if len(s) == 0 || len(s) > 128 || strings.TrimSpace(s) != s {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != '-' && c != '_' && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validTokenKind(kind TokenKind) bool {
	switch kind {
	case TokenColor, TokenSpacing, TokenType, TokenRadius, TokenElevation:
		return true
	default:
		return false
	}
}

// DefaultTokens seeds the platform token set.
func DefaultTokens() TokenRegistry {
	registry := TokenRegistry{
		Tokens: []SemanticToken{
			{ID: "color.text.primary", Kind: TokenColor, Value: "#16181d", Owner: "platform.design"},
			{ID: "color.surface.base", Kind: TokenColor, Value: "#ffffff", Owner: "platform.design"},
			{ID: "color.action.primary", Kind: TokenColor, Value: "#1a56db", Owner: "platform.design"},
			{ID: "spacing.page.gutter", Kind: TokenSpacing, Value: "24", Owner: "platform.design"},
			{ID: "spacing.card.gap", Kind: TokenSpacing, Value: "16", Owner: "platform.design"},
			{ID: "type.body.base", Kind: TokenType, Value: "400 16/24 system", Owner: "platform.design"},
			{ID: "type.heading.page", Kind: TokenType, Value: "600 24/32 system", Owner: "platform.design"},
			{ID: "radius.card", Kind: TokenRadius, Value: "8", Owner: "platform.design"},
			{ID: "elevation.raised", Kind: TokenElevation, Value: "2", Owner: "platform.design"},
		},
		Floorplans: []Floorplan{
			{ID: "floorplan.detail", Regions: []string{"header", "summary", "timeline", "actions"}, Owner: "platform.design"},
			{ID: "floorplan.list", Regions: []string{"filters", "table", "pagination"}, Owner: "platform.design"},
			{ID: "floorplan.failure", Regions: []string{"notice", "recovery", "support"}, Owner: "platform.design"},
		},
	}
	registry.Digest = registry.computeDigest()
	return registry
}

// Validate enforces unique safe IDs, admitted kinds, owned rows, and
// non-empty floorplan regions.
func (r TokenRegistry) Validate() error {
	seen := make(map[string]bool)
	for _, token := range r.Tokens {
		if !safeID(token.ID) {
			return fmt.Errorf("%w: token id %q", ErrTokenInvalid, token.ID)
		}
		if seen[token.ID] {
			return fmt.Errorf("%w: duplicate token %q", ErrTokenInvalid, token.ID)
		}
		seen[token.ID] = true
		if !validTokenKind(token.Kind) {
			return fmt.Errorf("%w: token %q kind %q", ErrTokenInvalid, token.ID, token.Kind)
		}
		if strings.TrimSpace(token.Value) == "" || strings.TrimSpace(token.Owner) == "" {
			return fmt.Errorf("%w: token %q needs a value and an owner", ErrTokenInvalid, token.ID)
		}
	}
	seenPlans := make(map[string]bool)
	for _, plan := range r.Floorplans {
		if !safeID(plan.ID) {
			return fmt.Errorf("%w: floorplan id %q", ErrTokenInvalid, plan.ID)
		}
		if seenPlans[plan.ID] {
			return fmt.Errorf("%w: duplicate floorplan %q", ErrTokenInvalid, plan.ID)
		}
		seenPlans[plan.ID] = true
		if len(plan.Regions) == 0 || strings.TrimSpace(plan.Owner) == "" {
			return fmt.Errorf("%w: floorplan %q needs regions and an owner", ErrTokenInvalid, plan.ID)
		}
		for _, region := range plan.Regions {
			if !safeID(region) {
				return fmt.Errorf("%w: floorplan %q region %q", ErrTokenInvalid, plan.ID, region)
			}
		}
	}
	return nil
}

// Resolve returns the token for an ID, or a typed refusal.
func (r TokenRegistry) Resolve(id string) (SemanticToken, error) {
	for _, token := range r.Tokens {
		if token.ID == id {
			return token, nil
		}
	}
	return SemanticToken{}, fmt.Errorf("%w: %q", ErrTokenUnknown, id)
}

func (r TokenRegistry) computeDigest() string {
	tokens := append([]SemanticToken(nil), r.Tokens...)
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].ID < tokens[j].ID })
	plans := append([]Floorplan(nil), r.Floorplans...)
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	for i := range plans {
		regions := append([]string(nil), plans[i].Regions...)
		sort.Strings(regions)
		plans[i].Regions = regions
	}
	b, err := json.Marshal(struct {
		Tokens     []SemanticToken `json:"tokens"`
		Floorplans []Floorplan     `json:"floorplans"`
	}{tokens, plans})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the digest matches the seeded content.
func (r TokenRegistry) VerifyDigest() error {
	if r.Digest == "" || r.Digest != r.computeDigest() {
		return fmt.Errorf("%w: token registry digest does not match its content", ErrTokenInvalid)
	}
	return nil
}
