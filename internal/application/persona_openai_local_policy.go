package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// LocalPersonaOpenAIModelPolicyReference returns the exact semantic contract
// selected before a manifest is sealed. The trusted policy publisher must
// retain these canonical bytes; returning a reference does not publish it.
// Version 2 records the schema-qualified tool digests; retained version 1
// remains immutable after the digest-format correction.
// Route payloads independently bind the resulting manifest and measured model
// profile, so their hashes cannot be part of this pre-manifest contract.
func LocalPersonaOpenAIModelPolicyReference() (agentmanifest.Reference, []byte) {
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok {
		return agentmanifest.Reference{}, nil
	}
	tools := make([]agentmanifest.Reference, 0, len(starter.SkillPins))
	for _, pin := range starter.SkillPins {
		tools = append(tools, agentmanifest.Reference{ID: pin.ID, Version: uint64(pin.Version), SchemaVersion: 1, Digest: personaRuntimeToolAdmissionDigest(pin.Digest)})
	}
	contract := struct {
		ID                 string                    `json:"id"`
		SchemaVersion      uint32                    `json:"schema_version"`
		Version            uint64                    `json:"version"`
		Identity           agentmodel.ModelIdentity  `json:"identity"`
		ProcessingRegion   string                    `json:"processing_region"`
		SyntheticTenants   []string                  `json:"synthetic_tenants"`
		Purpose            string                    `json:"purpose"`
		DataClasses        []string                  `json:"data_classes"`
		RetentionNS        int64                     `json:"retention_ns"`
		RetentionMode      agentegress.RetentionMode `json:"retention_mode"`
		TrainingUse        agentmodel.ProcessingUse  `json:"training_use"`
		Logging            agentmodel.ProcessingUse  `json:"logging"`
		InputPerMillion    int64                     `json:"input_micros_per_million"`
		CachedPerMillion   int64                     `json:"cached_input_micros_per_million"`
		OutputPerMillion   int64                     `json:"output_micros_per_million"`
		Budget             agentmanifest.Budget      `json:"budget"`
		MaxLatencyNS       int64                     `json:"max_latency_ns"`
		OutputSchemaDigest string                    `json:"output_schema_digest"`
		ToolCeiling        []agentmanifest.Reference `json:"tool_ceiling"`
		ToolSchemaDigest   string                    `json:"tool_schema_digest"`
	}{ID: "hcmnext.local-openai-persona-policy", SchemaVersion: 1, Version: 2, Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion},
		ProcessingRegion: LocalPersonaOpenAIRegion, SyntheticTenants: []string{"harborcare-demo", "ironridge-demo"}, Purpose: LocalPersonaOpenAIPurpose, DataClasses: []string{"PUBLIC", "INTERNAL"},
		RetentionNS: int64(LocalPersonaOpenAIRetention), RetentionMode: agentegress.RetentionBounded, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseAllowed,
		InputPerMillion: 250000, CachedPerMillion: 25000, OutputPerMillion: 2000000,
		Budget: agentmanifest.Budget{MaxCostMicros: uint64(LocalPersonaOpenAIMaxCostMicros), MaxInputTokens: 8192, MaxOutputTokens: 2048, MaxConcurrentRuns: 2}, MaxLatencyNS: int64(LocalPersonaOpenAIMaxLatency), OutputSchemaDigest: PersonaChatReplySchemaDigest,
		ToolCeiling: tools, ToolSchemaDigest: LocalPersonaOpenAIToolSchemaDigest()}
	raw, _ := json.Marshal(contract)
	canonical, digest, err := agentstore.CanonicalPersonaModelRoutePayload(raw)
	if err != nil {
		return agentmanifest.Reference{}, nil
	}
	return agentmanifest.Reference{ID: contract.ID, Version: contract.Version, SchemaVersion: contract.SchemaVersion, Digest: digest}, canonical
}

// LocalPersonaOpenAIToolSchemaDigest binds the runtime Policy Helper projection
// independently of a manifest or model evaluation, so both can pin it exactly.
func LocalPersonaOpenAIToolSchemaDigest() string {
	tools := []agentmodel.ToolSchema{{Name: personaDocumentSearchTool, Description: "Search official policy document placements in this conversation readable by the invoker and current installation. Returns deployed policy text with exact version, placement and content-digest citations.", InputSchema: personaDocumentSearchSchema}}
	raw, _ := json.Marshal(tools)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}
