package application

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrPersonaEvaluationKeyConfiguration identifies invalid trusted verifier-key configuration.
var ErrPersonaEvaluationKeyConfiguration = errors.New("application: invalid persona evaluation key configuration")

// PersonaEvaluationVerificationKeyConfig is one public evaluator key pinned to
// one tenant and evaluation suite. PublicKey is standard-base64 encoded; this
// type deliberately has no private-key field.
type PersonaEvaluationVerificationKeyConfig struct {
	TenantID  string `json:"tenant_id"`
	SuiteID   string `json:"suite_id"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

// PersonaEvaluationVerificationKeyID returns the key identifier that evaluator
// claims must carry for the configured tenant, suite, and rotation key.
func PersonaEvaluationVerificationKeyID(tenantID, suiteID, keyID string) (string, error) {
	if !validPersonaEvaluationKeyPart(tenantID) || !validPersonaEvaluationKeyPart(suiteID) || !validPersonaEvaluationKeyPart(keyID) {
		return "", fmt.Errorf("%w: tenant_id, suite_id and key_id must be 1-128 ASCII letters, digits, '.', '_' or '-'", ErrPersonaEvaluationKeyConfiguration)
	}
	return "persona-eval/v1/" + tenantID + "/" + suiteID + "/" + keyID, nil
}

// ParsePersonaEvaluationVerificationKeys parses the production JSON keyring
// into the flat key map consumed by NewPersonaPublicationEvidenceServices.
// Keep retired keys in the keyring until every evidence claim they signed has
// expired; during rotation, include old and new key IDs together.
func ParsePersonaEvaluationVerificationKeys(raw string) (map[string]ed25519.PublicKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: at least one public key is required", ErrPersonaEvaluationKeyConfiguration)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var entries []PersonaEvaluationVerificationKeyConfig
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%w: decode keyring: %v", ErrPersonaEvaluationKeyConfiguration, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: keyring must contain one JSON value", ErrPersonaEvaluationKeyConfiguration)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: at least one public key is required", ErrPersonaEvaluationKeyConfiguration)
	}

	keys := make(map[string]ed25519.PublicKey, len(entries))
	seenMaterial := make(map[string]string, len(entries))
	for index, entry := range entries {
		qualifiedID, err := PersonaEvaluationVerificationKeyID(entry.TenantID, entry.SuiteID, entry.KeyID)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d has invalid scope or ID", ErrPersonaEvaluationKeyConfiguration, index+1)
		}
		if _, exists := keys[qualifiedID]; exists {
			return nil, fmt.Errorf("%w: duplicate key ID %q", ErrPersonaEvaluationKeyConfiguration, qualifiedID)
		}
		encoded := strings.TrimSpace(entry.PublicKey)
		publicKey, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: key %q must be a %d-byte standard-base64 Ed25519 public key", ErrPersonaEvaluationKeyConfiguration, qualifiedID, ed25519.PublicKeySize)
		}
		materialID := string(publicKey)
		if priorID, exists := seenMaterial[materialID]; exists {
			return nil, fmt.Errorf("%w: public key material is reused by %q and %q", ErrPersonaEvaluationKeyConfiguration, priorID, qualifiedID)
		}
		seenMaterial[materialID] = qualifiedID
		keys[qualifiedID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	return keys, nil
}

func validPersonaEvaluationKeyPart(value string) bool {
	if len(value) == 0 || len(value) > 128 || value == "." || value == ".." || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
