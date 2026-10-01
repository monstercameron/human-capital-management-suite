package main

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
)

func TestTodo_AGENT_045_PortableDefinitionMappingFields(t *testing.T) {
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("instructions")))
	ref := func(id string) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: digest}
	}
	definition := agentportable.Definition{FormatVersion: 1, ManifestSchema: 1, Purpose: "Help", InstructionsDigest: digest, Instructions: "instructions", SourceCeiling: []agentmanifest.Reference{ref("hris")}, ToolCeiling: []agentmanifest.Reference{ref("read_people")}, ModelPolicy: ref("safe"), OutputSchema: ref("summary"), EvaluationRefs: []agentmanifest.Reference{ref("eval-1")}, AutonomyCeiling: "T0", Budget: agentmanifest.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxConcurrentRuns: 1}}
	data, err := definition.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	fields, err := portableDefinitionMappingFields(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 5 || fields[0].Kind != "source" || fields[4].Kind != "evaluation_suite" {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].SourceID != "hris" || fields[1].SourceID != "read_people" || fields[2].SourceID != "safe" {
		t.Fatalf("nonidentity field mapped: %+v", fields)
	}
	if _, err = portableDefinitionMappingFields([]byte(`{"sources":["hris"]}`)); err == nil {
		t.Fatal("unknown noncanonical definition accepted")
	}
}
