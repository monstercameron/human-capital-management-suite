package application

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestPersonaEvaluationKeyConfig_ParsesScopedRotationKeyring(t *testing.T) {
	oldPublic, newPublic := personaEvaluationConfigPublicKey("old"), personaEvaluationConfigPublicKey("new")
	entries := []PersonaEvaluationVerificationKeyConfig{
		{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "key-v1", PublicKey: base64.StdEncoding.EncodeToString(oldPublic)},
		{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "key-v2", PublicKey: base64.StdEncoding.EncodeToString(newPublic)},
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParsePersonaEvaluationVerificationKeys(string(raw))
	if err != nil {
		t.Fatalf("ParsePersonaEvaluationVerificationKeys: %v", err)
	}
	oldID, _ := PersonaEvaluationVerificationKeyID("tenant-a", "suite-1", "key-v1")
	newID, _ := PersonaEvaluationVerificationKeyID("tenant-a", "suite-1", "key-v2")
	if len(keys) != 2 || string(keys[oldID]) != string(oldPublic) || string(keys[newID]) != string(newPublic) {
		t.Fatalf("parsed keys = %#v, want both scoped rotation keys", keys)
	}
	keys[oldID][0] ^= 0xff
	if string(keys[newID]) != string(newPublic) {
		t.Fatal("rotated keys unexpectedly share mutable key bytes")
	}
}

func TestPersonaEvaluationKeyConfig_RejectsMissingMalformedAndPrivateMaterial(t *testing.T) {
	private := ed25519.NewKeyFromSeed(personaEvaluationConfigSeed("private"))
	cases := []struct {
		name    string
		entries []PersonaEvaluationVerificationKeyConfig
		raw     string
	}{
		{name: "empty config", raw: "  "},
		{name: "empty keyring", raw: "[]"},
		{name: "unknown private field", raw: `[{"tenant_id":"tenant-a","suite_id":"suite-1","key_id":"v1","private_key":"secret"}]`},
		{name: "private key material", entries: []PersonaEvaluationVerificationKeyConfig{{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(private)}}},
		{name: "wrong key size", entries: []PersonaEvaluationVerificationKeyConfig{{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize-1))}}},
		{name: "invalid scope separator", entries: []PersonaEvaluationVerificationKeyConfig{{TenantID: "tenant/a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(personaEvaluationConfigPublicKey("key"))}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			if tc.entries != nil {
				encoded, err := json.Marshal(tc.entries)
				if err != nil {
					t.Fatal(err)
				}
				raw = string(encoded)
			}
			if keys, err := ParsePersonaEvaluationVerificationKeys(raw); keys != nil || !errors.Is(err, ErrPersonaEvaluationKeyConfiguration) {
				t.Fatalf("parse invalid key config = (%v, %v)", keys, err)
			}
		})
	}
}

func TestPersonaEvaluationKeyConfig_RejectsDuplicateIDsAndMaterial(t *testing.T) {
	key := personaEvaluationConfigPublicKey("same")
	cases := []struct {
		name    string
		entries []PersonaEvaluationVerificationKeyConfig
	}{
		{name: "duplicate scoped id", entries: []PersonaEvaluationVerificationKeyConfig{
			{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(key)},
			{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(personaEvaluationConfigPublicKey("other"))},
		}},
		{name: "same material under new rotation id", entries: []PersonaEvaluationVerificationKeyConfig{
			{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(key)},
			{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v2", PublicKey: base64.StdEncoding.EncodeToString(key)},
		}},
		{name: "same material across suites", entries: []PersonaEvaluationVerificationKeyConfig{
			{TenantID: "tenant-a", SuiteID: "suite-1", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(key)},
			{TenantID: "tenant-a", SuiteID: "suite-2", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(key)},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.entries)
			if err != nil {
				t.Fatal(err)
			}
			if keys, err := ParsePersonaEvaluationVerificationKeys(string(raw)); keys != nil || !errors.Is(err, ErrPersonaEvaluationKeyConfiguration) {
				t.Fatalf("parse conflicting keys = (%v, %v)", keys, err)
			}
		})
	}
}

func TestPersonaEvaluationKeyConfig_QualifiedIDIsDeterministicAndScoped(t *testing.T) {
	first, err := PersonaEvaluationVerificationKeyID("tenant-a", "suite-1", "v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PersonaEvaluationVerificationKeyID("tenant-a", "suite-2", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first != "persona-eval/v1/tenant-a/suite-1/v1" {
		t.Fatalf("qualified IDs = %q and %q, want deterministic suite-scoped IDs", first, second)
	}
	if _, err := PersonaEvaluationVerificationKeyID("tenant-a", " ", "v1"); !errors.Is(err, ErrPersonaEvaluationKeyConfiguration) {
		t.Fatalf("invalid key ID scope error = %v", err)
	}
}

func personaEvaluationConfigPublicKey(label string) ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(personaEvaluationConfigSeed(label)).Public().(ed25519.PublicKey)
}

func personaEvaluationConfigSeed(label string) []byte {
	digest := sha256.Sum256([]byte(label))
	return digest[:]
}
