package agentpersona

import (
	"errors"
	"testing"
)

// TestTodo_AGENTP_003_ManifestCompatibilityRejectsUnrecognizedRef proves a
// persona cannot be sealed when its referenced agent manifest is incompatible.
func TestTodo_AGENTP_003_ManifestCompatibilityRejectsUnrecognizedRef(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	validator.Manifests = personaManifestCompatibility{failure: errors.New("manifest schema is retired")}

	_, err := validator.Build(profile)
	if !errors.Is(err, ErrInvalidProfile) || !errors.Is(err, ErrManifestCompatibility) {
		t.Fatalf("Build error = %v, want invalid profile and manifest compatibility errors", err)
	}
}

func TestTodo_AGENTP_003_SecurityManifestLinkFailsClosed(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	validator.Manifests = nil
	if _, err := validator.Build(profile); !errors.Is(err, ErrManifestCompatibility) {
		t.Fatalf("Build without manifest compatibility = %v, want fail-closed compatibility error", err)
	}
}

func TestTodo_AGENTP_003_SecurityManifestLinkRejectsStaleIdentityAndDigest(t *testing.T) {
	profile, catalog := personaFixture(t)
	base := personaManifest()
	tests := []struct {
		name   string
		change func(*PersonaProfile)
	}{
		{name: "id", change: func(p *PersonaProfile) { p.Manifest.ID = "agent.other" }},
		{name: "version", change: func(p *PersonaProfile) { p.Manifest.Version++ }},
		{name: "schema", change: func(p *PersonaProfile) { p.Manifest.SchemaVersion++ }},
		{name: "digest", change: func(p *PersonaProfile) {
			p.Manifest.Digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := profile
			tc.change(&changed)
			validator := personaValidator(catalog)
			validator.Manifests = ManifestCompatibilityAdapter{Resolver: personaManifestResolver{manifest: base}}
			if _, err := validator.Build(changed); !errors.Is(err, ErrManifestCompatibility) {
				t.Fatalf("Build with stale %s = %v, want manifest compatibility error", tc.name, err)
			}
		})
	}
}

func TestTodo_AGENTP_003_SecurityManifestResolverFailureIsPreserved(t *testing.T) {
	profile, catalog := personaFixture(t)
	lookupFailure := errors.New("manifest version unavailable")
	validator := personaValidator(catalog)
	validator.Manifests = ManifestCompatibilityAdapter{Resolver: personaManifestResolver{failure: lookupFailure}}
	if _, err := validator.Build(profile); !errors.Is(err, ErrManifestCompatibility) || !errors.Is(err, lookupFailure) {
		t.Fatalf("Build resolver failure = %v, want compatibility and lookup errors", err)
	}
}
