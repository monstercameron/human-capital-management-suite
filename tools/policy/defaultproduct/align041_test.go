package defaultproduct

import (
	"errors"
	"testing"
)

// TestTodo_ALIGN_041 proves the platform semantic tokens and floorplans
// seed with owners, a closed kind set, and a pinned digest.
func TestTodo_ALIGN_041(t *testing.T) {
	registry := DefaultTokens()
	if err := registry.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := registry.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	if len(registry.Tokens) < 9 || len(registry.Floorplans) < 3 {
		t.Fatalf("seeded %d tokens and %d floorplans, want the platform set", len(registry.Tokens), len(registry.Floorplans))
	}
	token, err := registry.Resolve("color.text.primary")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if token.Kind != TokenColor || token.Value == "" || token.Owner == "" {
		t.Fatalf("token = %+v", token)
	}
}

func TestTodo_ALIGN_041_Property(t *testing.T) {
	first, second := DefaultTokens(), DefaultTokens()
	if first.Digest != second.Digest {
		t.Fatalf("token seed is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	for _, token := range first.Tokens {
		if !validTokenKind(token.Kind) {
			t.Fatalf("token %q has kind %q outside the closed set", token.ID, token.Kind)
		}
	}
}

func TestTodo_ALIGN_041_Golden(t *testing.T) {
	registry := DefaultTokens()
	const wantDigest = "sha256:1728b0ee6800e1f5fc762a8f5bdc1039434e5decba0f795e13024c01c533aad9"
	if registry.Digest != wantDigest {
		t.Fatalf("token digest=%q want=%q", registry.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_041_Security(t *testing.T) {
	registry := DefaultTokens()
	// Unknown tokens are refused by name.
	if _, err := registry.Resolve("color.evil.custom"); !errors.Is(err, ErrTokenUnknown) {
		t.Fatalf("Resolve(unknown) = %v, want ErrTokenUnknown", err)
	}
	// Ownerless and duplicate seeds are refused.
	ownerless := DefaultTokens()
	ownerless.Tokens[0].Owner = ""
	ownerless.Digest = ownerless.computeDigest()
	if err := ownerless.Validate(); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Validate(ownerless) = %v, want ErrTokenInvalid", err)
	}
	duplicated := DefaultTokens()
	duplicated.Tokens = append(duplicated.Tokens, duplicated.Tokens[0])
	duplicated.Digest = duplicated.computeDigest()
	if err := duplicated.Validate(); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Validate(duplicate) = %v, want ErrTokenInvalid", err)
	}
	// A tampered value breaks the digest.
	tampered := DefaultTokens()
	tampered.Tokens[0].Value = "#000000"
	if err := tampered.VerifyDigest(); err == nil {
		t.Fatal("tampered token registry verified")
	}
}

func TestTodo_ALIGN_041_Conformance(t *testing.T) {
	registry := DefaultTokens()
	// Every token kind in the closed set is exercised by the seed, so no
	// kind is dead contract.
	seen := make(map[TokenKind]bool)
	for _, token := range registry.Tokens {
		seen[token.Kind] = true
	}
	for _, kind := range []TokenKind{TokenColor, TokenSpacing, TokenType, TokenRadius, TokenElevation} {
		if !seen[kind] {
			t.Fatalf("token kind %q has no seeded token", kind)
		}
	}
	// Every floorplan carries at least one region and an owner.
	for _, plan := range registry.Floorplans {
		if len(plan.Regions) == 0 || plan.Owner == "" {
			t.Fatalf("floorplan %+v is incomplete", plan)
		}
	}
}

func FuzzTodo_ALIGN_041_Fuzz(f *testing.F) {
	f.Add("color.text.primary")
	f.Fuzz(func(t *testing.T, id string) {
		registry := DefaultTokens()
		first, firstErr := registry.Resolve(id)
		second, secondErr := registry.Resolve(id)
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("resolve is not deterministic for %q", id)
		}
	})
}
