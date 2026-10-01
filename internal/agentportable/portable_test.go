package agentportable

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

func TestTodo_AGENT_045(t *testing.T) {
	manifest := fixtureManifest()
	data, err := Export(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tenant-a") || strings.Contains(string(data), "owner-a") || strings.Contains(string(data), "context_grants") {
		t.Fatalf("authority leaked: %s", data)
	}
	draft, err := Import(data, ImportRequest{TenantID: "tenant-b", AgentID: "agent-b", OwnerID: "owner-b"}, func(kind ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
		ref.ID = "dest." + string(kind) + "." + ref.ID
		return ref, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != "DRAFT" || draft.TenantID != "tenant-b" || draft.Manifest.ID != "agent-b" || draft.Manifest.OwnerID != "owner-b" {
		t.Fatalf("not a destination draft: %+v", draft)
	}
	if draft.Manifest.ContextGrants == nil || len(draft.Manifest.ContextGrants) != 0 {
		t.Fatalf("draft has grants: %#v", draft.Manifest.ContextGrants)
	}
}

func TestTodo_AGENT_045_Golden(t *testing.T) {
	data, err := Export(fixtureManifest())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format_version":1,"manifest_schema_version":1,"purpose":"answer policy questions with citations","instructions_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_ceiling":[{"id":"docs.handbook","version":2,"schema_version":1,"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"tool_ceiling":[{"id":"documents.search","version":3,"schema_version":2,"digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],"model_policy":{"id":"model-policy.default","version":1,"schema_version":1,"digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"autonomy_ceiling":"private_answer","budget":{"max_cost_micros":100000,"max_input_tokens":4000,"max_output_tokens":1000,"max_concurrent_runs":1},"output_schema":{"id":"agent.answer","version":1,"schema_version":1,"digest":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},"evaluation_refs":[{"id":"eval.policy-guide","version":1,"schema_version":1,"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}]}`
	if string(data) != want {
		t.Fatalf("canonical bytes changed\n got %s\nwant %s", data, want)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := parsed.Digest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q, err=%v", digest, err)
	}
}

func TestTodo_AGENT_045_Security(t *testing.T) {
	data, err := Export(fixtureManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, mutation string }{
		{"unknown authority", strings.Replace(string(data), `"purpose":`, `"tenant_id":"tenant-a","purpose":`, 1)},
		{"duplicate field", strings.Replace(string(data), `"purpose":`, `"format_version":1,"purpose":`, 1)},
		{"nested credential", strings.Replace(string(data), `"id":"docs.handbook"`, `"id":"docs.handbook","credential":"secret"`, 1)},
		{"noncanonical", strings.Replace(string(data), `"format_version":1`, `"format_version":1 `, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.mutation)); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("accepted unsafe input: %v", err)
			}
		})
	}
	if _, err := Export(agentmanifest.Manifest{}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("invalid export error = %v", err)
	}
}

func TestTodo_AGENT_045_Conformance(t *testing.T) {
	data, err := Export(fixtureManifest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(data, ImportRequest{TenantID: "t", AgentID: "a", OwnerID: "o"}, nil); !errors.Is(err, ErrMappingRequired) {
		t.Fatalf("nil mapper error = %v", err)
	}
	_, err = Import(data, ImportRequest{TenantID: "t", AgentID: "a", OwnerID: "o"}, func(kind ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
		if kind == CapabilityReference {
			return ref, errors.New("unavailable")
		}
		return ref, nil
	})
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("unavailable capability error = %v", err)
	}
	definition, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	definition.FormatVersion++
	if _, err := definition.CanonicalJSON(); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func fixtureManifest() agentmanifest.Manifest {
	ref := func(id string, n byte) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat(string(n), 64)}
	}
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "agent.private", Version: 9, OwnerID: "owner-a", Purpose: "answer policy questions with citations", InstructionsDigest: "sha256:" + strings.Repeat("a", 64), SourceCeiling: []agentmanifest.Reference{{ID: "docs.handbook", Version: 2, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("b", 64)}}, ToolCeiling: []agentmanifest.Reference{{ID: "documents.search", Version: 3, SchemaVersion: 2, Digest: "sha256:" + strings.Repeat("c", 64)}}, ModelPolicy: ref("model-policy.default", 'd'), AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 100000, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1}, OutputSchema: ref("agent.answer", 'e'), ContextGrants: []agentmanifest.Reference{ref("private.context", 'f')}, EvaluationRefs: []agentmanifest.Reference{ref("eval.policy-guide", '0')}}
}
