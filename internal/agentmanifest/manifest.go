package agentmanifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// CurrentSchemaVersion is the first version of the canonical agent manifest.
	CurrentSchemaVersion uint32 = 1
	maxManifestBytes            = 1 << 20
	maxTextBytes                = 16 << 10
)

// ErrInvalidManifest identifies a malformed, unsupported, or noncanonical manifest.
var ErrInvalidManifest = errors.New("invalid agent manifest")

// Manifest is the provider-neutral immutable input to validating and publishing
// one version of a tenant-owned agent. References pin separately governed records.
type Manifest struct {
	SchemaVersion      uint32      `json:"schema_version"`
	ID                 string      `json:"id"`
	Version            uint64      `json:"version"`
	OwnerID            string      `json:"owner_id"`
	Purpose            string      `json:"purpose"`
	InstructionsDigest string      `json:"instructions_digest"`
	SourceCeiling      []Reference `json:"source_ceiling"`
	ToolCeiling        []Reference `json:"tool_ceiling"`
	ModelPolicy        Reference   `json:"model_policy"`
	AutonomyCeiling    string      `json:"autonomy_ceiling"`
	Budget             Budget      `json:"budget"`
	OutputSchema       Reference   `json:"output_schema"`
	ContextGrants      []Reference `json:"context_grants"`
	EvaluationRefs     []Reference `json:"evaluation_refs"`
}

// Reference pins an immutable external record and its own schema version.
type Reference struct {
	ID            string `json:"id"`
	Version       uint64 `json:"version"`
	SchemaVersion uint32 `json:"schema_version"`
	Digest        string `json:"digest"`
}

// Budget contains explicit upper bounds carried by an agent version.
type Budget struct {
	MaxCostMicros     uint64 `json:"max_cost_micros"`
	MaxInputTokens    uint64 `json:"max_input_tokens"`
	MaxOutputTokens   uint64 `json:"max_output_tokens"`
	MaxConcurrentRuns uint32 `json:"max_concurrent_runs"`
}

// ManifestRef is the immutable identity expected by a consumer such as a persona.
type ManifestRef struct {
	ID            string
	Version       uint64
	SchemaVersion uint32
	Digest        string
}

// Validate checks required declarations and canonical reference ordering.
// Diagnostics are sorted by field name so callers can display stable results.
func (m Manifest) Validate() error {
	var problems []string
	if m.SchemaVersion != CurrentSchemaVersion {
		problems = append(problems, "schema_version: unsupported")
	}
	requiredText(&problems, "id", m.ID, 256)
	if m.Version == 0 {
		problems = append(problems, "version: must be positive")
	}
	requiredText(&problems, "owner_id", m.OwnerID, 256)
	requiredText(&problems, "purpose", m.Purpose, maxTextBytes)
	if !validDigest(m.InstructionsDigest) {
		problems = append(problems, "instructions_digest: expected sha256 digest")
	}
	validateReferences(&problems, "source_ceiling", m.SourceCeiling, true)
	validateReferences(&problems, "tool_ceiling", m.ToolCeiling, true)
	validateReference(&problems, "model_policy", m.ModelPolicy)
	requiredText(&problems, "autonomy_ceiling", m.AutonomyCeiling, 128)
	if m.Budget.MaxCostMicros == 0 {
		problems = append(problems, "budget.max_cost_micros: must be positive")
	}
	if m.Budget.MaxInputTokens == 0 {
		problems = append(problems, "budget.max_input_tokens: must be positive")
	}
	if m.Budget.MaxOutputTokens == 0 {
		problems = append(problems, "budget.max_output_tokens: must be positive")
	}
	if m.Budget.MaxConcurrentRuns == 0 {
		problems = append(problems, "budget.max_concurrent_runs: must be positive")
	}
	validateReference(&problems, "output_schema", m.OutputSchema)
	validateReferences(&problems, "context_grants", m.ContextGrants, false)
	validateReferences(&problems, "evaluation_refs", m.EvaluationRefs, true)
	if len(m.EvaluationRefs) == 0 {
		problems = append(problems, "evaluation_refs: at least one evaluation suite is required")
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%w: %s", ErrInvalidManifest, strings.Join(problems, "; "))
}

// CanonicalJSON returns stable JSON bytes for a valid manifest.
func (m Manifest) CanonicalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %v", ErrInvalidManifest, err)
	}
	return data, nil
}

// Digest returns the SHA-256 digest of CanonicalJSON.
func (m Manifest) Digest() (string, error) {
	data, err := m.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Parse rejects oversized, ambiguous, unknown-field, or invalid manifests.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) == 0 || len(data) > maxManifestBytes || !utf8.Valid(data) {
		return m, fmt.Errorf("%w: input size or encoding", ErrInvalidManifest)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := rejectUnknownKeys(data); err != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("%w: decode: %v", ErrInvalidManifest, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return m, fmt.Errorf("%w: trailing JSON", ErrInvalidManifest)
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

func rejectUnknownKeys(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	if root == nil {
		return errors.New("manifest must be a JSON object")
	}
	if err := requireKeys(root, map[string]struct{}{
		"schema_version": {}, "id": {}, "version": {}, "owner_id": {}, "purpose": {},
		"instructions_digest": {}, "source_ceiling": {}, "tool_ceiling": {}, "model_policy": {},
		"autonomy_ceiling": {}, "budget": {}, "output_schema": {}, "context_grants": {},
		"evaluation_refs": {},
	}); err != nil {
		return err
	}
	for _, key := range []string{"model_policy", "output_schema"} {
		if raw, ok := root[key]; ok {
			if err := checkReferenceKeys(raw); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	for _, key := range []string{"source_ceiling", "tool_ceiling", "context_grants", "evaluation_refs"} {
		raw, ok := root[key]
		if !ok {
			continue
		}
		var refs []json.RawMessage
		if err := json.Unmarshal(raw, &refs); err != nil {
			continue // The typed decoder reports the shape error with the usual diagnostic.
		}
		for i, ref := range refs {
			if err := checkReferenceKeys(ref); err != nil {
				return fmt.Errorf("%s[%d]: %w", key, i, err)
			}
		}
	}
	if raw, ok := root["budget"]; ok {
		var budget map[string]json.RawMessage
		if err := json.Unmarshal(raw, &budget); err == nil && budget != nil {
			if err := requireKeys(budget, map[string]struct{}{
				"max_cost_micros": {}, "max_input_tokens": {}, "max_output_tokens": {}, "max_concurrent_runs": {},
			}); err != nil {
				return fmt.Errorf("budget: %w", err)
			}
		}
	}
	return nil
}

func checkReferenceKeys(raw json.RawMessage) error {
	var ref map[string]json.RawMessage
	if err := json.Unmarshal(raw, &ref); err != nil || ref == nil {
		return nil // The typed decoder reports the shape error.
	}
	return requireKeys(ref, map[string]struct{}{
		"id": {}, "version": {}, "schema_version": {}, "digest": {},
	})
}

func requireKeys(fields map[string]json.RawMessage, allowed map[string]struct{}) error {
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return nil
}

// Compatible verifies that ref names this exact validated manifest version.
func Compatible(ref ManifestRef, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if ref.ID != manifest.ID || ref.Version != manifest.Version || ref.SchemaVersion != manifest.SchemaVersion || ref.Digest != digest {
		return fmt.Errorf("%w: manifest reference does not match immutable version", ErrInvalidManifest)
	}
	return nil
}

func requiredText(problems *[]string, field, value string, limit int) {
	if strings.TrimSpace(value) == "" {
		*problems = append(*problems, field+": required")
		return
	}
	if len(value) > limit || strings.TrimSpace(value) != value {
		*problems = append(*problems, field+": invalid length or surrounding whitespace")
	}
}

func validateReferences(problems *[]string, field string, refs []Reference, required bool) {
	if refs == nil && required {
		*problems = append(*problems, field+": required (use an empty array for an explicit empty ceiling)")
		return
	}
	previous := ""
	seen := make(map[string]struct{}, len(refs))
	for i, ref := range refs {
		label := fmt.Sprintf("%s[%d]", field, i)
		validateReference(problems, label, ref)
		if _, ok := seen[ref.ID]; ok {
			*problems = append(*problems, label+": duplicate id")
		}
		seen[ref.ID] = struct{}{}
		if i > 0 && ref.ID <= previous {
			*problems = append(*problems, label+": references must be sorted by id")
		}
		previous = ref.ID
	}
}

func validateReference(problems *[]string, field string, ref Reference) {
	requiredText(problems, field+".id", ref.ID, 256)
	if ref.Version == 0 {
		*problems = append(*problems, field+".version: must be positive")
	}
	if ref.SchemaVersion == 0 {
		*problems = append(*problems, field+".schema_version: must be positive")
	}
	if !validDigest(ref.Digest) {
		*problems = append(*problems, field+".digest: expected sha256 digest")
	}
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	return value == strings.ToLower(value)
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
