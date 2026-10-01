package agentmanifest

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:      CurrentSchemaVersion,
		ID:                 "agent.policy-guide",
		Version:            1,
		OwnerID:            "user.owner",
		Purpose:            "answer policy questions with citations",
		InstructionsDigest: "sha256:" + strings.Repeat("a", 64),
		SourceCeiling:      []Reference{{ID: "docs.handbook", Version: 2, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("b", 64)}},
		ToolCeiling:        []Reference{{ID: "documents.search", Version: 3, SchemaVersion: 2, Digest: "sha256:" + strings.Repeat("c", 64)}},
		ModelPolicy:        Reference{ID: "model-policy.default", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("d", 64)},
		AutonomyCeiling:    "private_answer",
		Budget:             Budget{MaxCostMicros: 100000, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1},
		OutputSchema:       Reference{ID: "agent.answer", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("e", 64)},
		ContextGrants:      []Reference{{ID: "context.handbook", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("f", 64)}},
		EvaluationRefs:     []Reference{{ID: "eval.policy-guide", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("0", 64)}},
	}
}

func TestTodo_AGENT_007(t *testing.T) {
	manifest := validManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	encoded, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("parse canonical JSON: %v", err)
	}
	if parsed.ID != manifest.ID || parsed.Version != manifest.Version {
		t.Fatalf("round trip identity = %s v%d", parsed.ID, parsed.Version)
	}
	if _, err := manifest.Digest(); err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
}

func TestTodo_AGENT_007_Golden(t *testing.T) {
	manifest := validManifest()
	want := `{"schema_version":1,"id":"agent.policy-guide","version":1,"owner_id":"user.owner","purpose":"answer policy questions with citations","instructions_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_ceiling":[{"id":"docs.handbook","version":2,"schema_version":1,"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"tool_ceiling":[{"id":"documents.search","version":3,"schema_version":2,"digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],"model_policy":{"id":"model-policy.default","version":1,"schema_version":1,"digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"autonomy_ceiling":"private_answer","budget":{"max_cost_micros":100000,"max_input_tokens":4000,"max_output_tokens":1000,"max_concurrent_runs":1},"output_schema":{"id":"agent.answer","version":1,"schema_version":1,"digest":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},"context_grants":[{"id":"context.handbook","version":1,"schema_version":1,"digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}],"evaluation_refs":[{"id":"eval.policy-guide","version":1,"schema_version":1,"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}]}`
	got, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("canonical bytes mismatch\n got: %s\nwant: %s", got, want)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(digest, "sha256:") || len(strings.TrimPrefix(digest, "sha256:")) != 64 {
		t.Fatalf("digest is not a SHA-256 token: %q", digest)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:")); err != nil {
		t.Fatalf("digest is not hexadecimal: %v", err)
	}
}

func TestTodo_AGENT_007_Security(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "unknown authority field", json: `{"schema_version":1,"id":"a","version":1,"owner_id":"o","purpose":"p","instructions_digest":"sha256:` + strings.Repeat("a", 64) + `","source_ceiling":[],"tool_ceiling":[],"model_policy":{},"autonomy_ceiling":"x","budget":{},"output_schema":{},"context_grants":[],"evaluation_refs":[],"provider_grant":"all"}`},
		{name: "case alias", json: strings.Replace(validJSON(), `"id":"agent.policy-guide"`, `"ID":"agent.policy-guide"`, 1)},
		{name: "unknown nested security field", json: strings.Replace(validJSON(), `"max_cost_micros":100000`, `"max_cost_micros":100000,"provider_grant":"all"`, 1)},
		{name: "trailing object", json: `{}` + `{}`},
		{name: "duplicate field", json: strings.Replace(validJSON(), `"id":"agent.policy-guide"`, `"id":"one","id":"two"`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.json)); err == nil {
				t.Fatalf("unsafe JSON accepted: %s", tc.name)
			}
		})
	}
	manifest := validManifest()
	first, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Purpose = "changed purpose"
	second, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("material manifest change kept the same digest")
	}
}

func TestTodo_AGENT_007_FieldDiagnostics(t *testing.T) {
	manifest := validManifest()
	manifest.OwnerID = ""
	manifest.Purpose = " "
	manifest.SourceCeiling = nil
	manifest.ToolCeiling = nil
	manifest.ModelPolicy = Reference{}
	manifest.AutonomyCeiling = ""
	manifest.Budget.MaxCostMicros = 0
	manifest.OutputSchema = Reference{}
	manifest.InstructionsDigest = "bad"
	err := manifest.Validate()
	if err == nil {
		t.Fatal("incomplete manifest accepted")
	}
	for _, field := range []string{"owner_id", "purpose", "source_ceiling", "tool_ceiling", "model_policy", "autonomy_ceiling", "budget.max_cost_micros", "output_schema", "instructions_digest"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("diagnostics omit %q: %v", field, err)
		}
	}
	if again := manifest.Validate(); again == nil || again.Error() != err.Error() {
		t.Fatalf("diagnostics are not stable: first=%v second=%v", err, again)
	}
}

func TestTodo_AGENT_007_ReferenceBinding(t *testing.T) {
	manifest := validManifest()
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := Compatible(ManifestRef{ID: manifest.ID, Version: manifest.Version, SchemaVersion: manifest.SchemaVersion, Digest: digest}, manifest); err != nil {
		t.Fatalf("matching immutable reference rejected: %v", err)
	}
	ref := ManifestRef{ID: manifest.ID, Version: manifest.Version, SchemaVersion: manifest.SchemaVersion, Digest: digest}
	ref.Version++
	if err := Compatible(ref, manifest); err == nil {
		t.Fatal("reference to another version accepted")
	}
	ref.Version = manifest.Version
	ref.SchemaVersion++
	if err := Compatible(ref, manifest); err == nil {
		t.Fatal("reference to another manifest schema accepted")
	}
}

func TestTodo_AGENT_007_CanonicalDeterminism(t *testing.T) {
	manifest := validManifest()
	first, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	second, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same manifest produced different canonical bytes")
	}
	manifest.SourceCeiling = append(manifest.SourceCeiling, Reference{ID: "docs.aaa", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("1", 64)})
	if err := manifest.Validate(); err == nil {
		t.Fatal("non-canonical reference ordering accepted")
	}
}

func FuzzTodo_AGENT_007_Fuzz(f *testing.F) {
	valid, err := validManifest().CanonicalJSON()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schema_version":1,"unknown_security_field":true}`))
	f.Add([]byte{0xff, 0x00, '{'})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxManifestBytes {
			t.Skip()
		}
		_, err := Parse(data)
		if err != nil && errors.Is(err, ErrInvalidManifest) {
			return
		}
		if err == nil {
			parsed, parseErr := Parse(data)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			canonical, canonicalErr := parsed.CanonicalJSON()
			if canonicalErr != nil {
				t.Fatal(canonicalErr)
			}
			if len(canonical) == 0 {
				t.Fatal("accepted manifest has empty canonical form")
			}
		}
	})
}

func validJSON() string {
	data, _ := validManifest().CanonicalJSON()
	return string(data)
}
