package portable

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

func TestTodo_AGENT_045(t *testing.T) {
	manifest := portableFixture()
	data, err := Export(manifest)
	if err != nil {
		t.Fatal(err)
	}
	mapper := func(kind ReferenceKind, source agentmanifest.Reference) (agentmanifest.Reference, error) {
		source.ID = "destination." + string(kind) + "." + source.ID
		return source, nil
	}
	draft, err := Import(data, ImportRequest{TenantID: "tenant-b", AgentID: "agent-b", OwnerID: "owner-b"}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != "DRAFT" || draft.TenantID != "tenant-b" {
		t.Fatalf("import state/tenant = %q/%q", draft.State, draft.TenantID)
	}
	got := draft.Manifest
	if got.ID != "agent-b" || got.OwnerID != "owner-b" || got.Version != 1 {
		t.Fatalf("destination identity was not applied: %#v", got)
	}
	if got.Purpose != manifest.Purpose || got.Budget != manifest.Budget || got.InstructionsDigest != manifest.InstructionsDigest {
		t.Fatalf("portable fields changed: %#v", got)
	}
	if len(got.ContextGrants) != 0 || got.ContextGrants == nil {
		t.Fatalf("imported draft carries grants or omitted the explicit empty set: %#v", got.ContextGrants)
	}
	if strings.Contains(string(data), "owner-a") || strings.Contains(string(data), "tenant-a") {
		t.Fatalf("source identity leaked into transfer: %s", data)
	}
}

func TestTodo_AGENT_045_Golden(t *testing.T) {
	data, err := Export(portableFixture())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format_version":1,"manifest_schema_version":1,"purpose":"answer policy questions with citations","instructions_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_ceiling":[{"id":"docs.handbook","version":2,"schema_version":1,"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"tool_ceiling":[{"id":"documents.search","version":3,"schema_version":2,"digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],"model_policy":{"id":"model-policy.default","version":1,"schema_version":1,"digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"autonomy_ceiling":"private_answer","budget":{"max_cost_micros":100000,"max_input_tokens":4000,"max_output_tokens":1000,"max_concurrent_runs":1},"output_schema":{"id":"agent.answer","version":1,"schema_version":1,"digest":"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},"evaluation_refs":[{"id":"eval.policy-guide","version":1,"schema_version":1,"digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}]}`
	if string(data) != want {
		t.Fatalf("portable bytes changed\n got: %s\nwant: %s", data, want)
	}
}

func TestTodo_AGENT_045_Security(t *testing.T) {
	manifest := portableFixture()
	data, err := Export(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tenant-a", "owner-a", "agent.private", "secret-value", "credential", "installation", "memory", "run_history", "context_grants", "private.context"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("export contains authority or tenant data %q: %s", forbidden, data)
		}
	}
	unsafe := []struct {
		name string
		data string
	}{
		{name: "tenant field injection", data: strings.Replace(string(data), `"purpose":`, `"tenant_id":"tenant-a","purpose":`, 1)},
		{name: "grant field injection", data: strings.Replace(string(data), `"tool_ceiling":[{`, `"tool_ceiling":[{"credential":"secret-value",`, 1)},
		{name: "installations field injection", data: strings.Replace(string(data), `"purpose":`, `"installations":["channel-a"],"purpose":`, 1)},
		{name: "memory field injection", data: strings.Replace(string(data), `"purpose":`, `"memory":["private history"],"purpose":`, 1)},
		{name: "run history field injection", data: strings.Replace(string(data), `"purpose":`, `"run_history":["private run"],"purpose":`, 1)},
		{name: "duplicate field", data: strings.Replace(string(data), `"purpose":`, `"format_version":1,"purpose":`, 1)},
		{name: "unknown nested reference field", data: strings.Replace(string(data), `"id":"docs.handbook"`, `"id":"docs.handbook","grant":"all"`, 1)},
		{name: "trailing object", data: string(data) + `{}`},
	}
	for _, tc := range unsafe {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.data)); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("unsafe portable input accepted or wrong error: %v", err)
			}
		})
	}
	if _, err := Export(agentmanifest.Manifest{}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("invalid source manifest export error = %v", err)
	}
}

func TestTodo_AGENT_045_Conformance(t *testing.T) {
	data, err := Export(portableFixture())
	if err != nil {
		t.Fatal(err)
	}
	request := ImportRequest{TenantID: "tenant-b", AgentID: "agent-b", OwnerID: "owner-b"}
	if _, err := Import(data, request, nil); !errors.Is(err, ErrMappingRequired) {
		t.Fatalf("nil destination resolver error = %v", err)
	}
	for _, tc := range []struct {
		name    string
		request ImportRequest
		mapper  ReferenceMapper
		wantErr error
	}{
		{name: "identity required", request: ImportRequest{AgentID: "a", OwnerID: "o"}, mapper: identityMapper},
		{name: "unavailable capability rejected", request: request, mapper: func(kind ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
			if kind == CapabilityReference {
				return agentmanifest.Reference{}, fmt.Errorf("capability unavailable")
			}
			return ref, nil
		}},
		{name: "invalid mapped ref rejected", request: request, mapper: func(kind ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
			if kind == SourceReference {
				ref.Digest = "bad"
			}
			return ref, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Import(data, tc.request, tc.mapper); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("invalid import accepted or wrong error: %v", err)
			}
		})
	}
	definition, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	definition.SourceCeiling = nil
	if err := definition.Validate(); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("omitted source ceiling was accepted: %v", err)
	}
	definition, err = Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	definition.FormatVersion++
	if _, err := definition.CanonicalJSON(); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("unsupported format version error = %v", err)
	}
}

func identityMapper(_ ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
	return ref, nil
}

func portableFixture() agentmanifest.Manifest {
	return agentmanifest.Manifest{
		SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: "agent.private", Version: 9,
		OwnerID: "owner-a", Purpose: "answer policy questions with citations",
		InstructionsDigest: "sha256:" + strings.Repeat("a", 64),
		SourceCeiling:      []agentmanifest.Reference{{ID: "docs.handbook", Version: 2, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("b", 64)}},
		ToolCeiling:        []agentmanifest.Reference{{ID: "documents.search", Version: 3, SchemaVersion: 2, Digest: "sha256:" + strings.Repeat("c", 64)}},
		ModelPolicy:        agentmanifest.Reference{ID: "model-policy.default", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("d", 64)},
		AutonomyCeiling:    "private_answer",
		Budget:             agentmanifest.Budget{MaxCostMicros: 100000, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1},
		OutputSchema:       agentmanifest.Reference{ID: "agent.answer", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("e", 64)},
		ContextGrants:      []agentmanifest.Reference{{ID: "private.context", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("f", 64)}},
		EvaluationRefs:     []agentmanifest.Reference{{ID: "eval.policy-guide", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("0", 64)}},
	}
}
