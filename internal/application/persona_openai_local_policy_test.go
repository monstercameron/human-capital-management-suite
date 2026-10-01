package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

func TestTodo_AGENT_021_LocalOpenAIPolicyReference(t *testing.T) {
	ref, raw := LocalPersonaOpenAIModelPolicyReference()
	second, again := LocalPersonaOpenAIModelPolicyReference()
	digest := sha256.Sum256(raw)
	if ref != second || !bytes.Equal(raw, again) || ref.Digest != "sha256:"+hex.EncodeToString(digest[:]) || ref.ID == "" || ref.Version != 2 || ref.SchemaVersion != 1 {
		t.Fatalf("semantic policy reference is not canonical: %+v", ref)
	}
	canonical, publishedDigest, err := agentstore.CanonicalPersonaModelRoutePayload(raw)
	if err != nil || !bytes.Equal(raw, canonical) || publishedDigest != ref.Digest {
		t.Fatalf("registered policy bytes differ from publisher authority: %s %v", publishedDigest, err)
	}
	var contract struct {
		Identity struct {
			ProviderID string `json:"provider_id"`
			ModelID    string `json:"model_id"`
			Version    string `json:"version"`
		} `json:"identity"`
		Budget struct {
			MaxCostMicros   uint64 `json:"max_cost_micros"`
			MaxInputTokens  uint64 `json:"max_input_tokens"`
			MaxOutputTokens uint64 `json:"max_output_tokens"`
		} `json:"budget"`
		RetentionNS int64 `json:"retention_ns"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil || contract.Identity.Version != LocalPersonaOpenAIModelVersion || contract.Budget.MaxCostMicros != uint64(LocalPersonaOpenAIMaxCostMicros) || contract.RetentionNS != int64(LocalPersonaOpenAIRetention) {
		t.Fatalf("semantic policy does not describe local model defaults: %+v %v", contract, err)
	}
	if bytes.Contains(raw, []byte("AgentVersionDigest")) || bytes.Contains(raw, []byte("ProfileDigest")) {
		t.Fatal("semantic policy contains a circular runtime pin")
	}
}

func TestTodo_AGENT_021_LocalOpenAIPolicyReference_Golden(t *testing.T) {
	_, raw := LocalPersonaOpenAIModelPolicyReference()
	const expected = `{"budget":{"max_concurrent_runs":2,"max_cost_micros":10000,"max_input_tokens":8192,"max_output_tokens":2048},"cached_input_micros_per_million":25000,"data_classes":["PUBLIC","INTERNAL"],"id":"hcmnext.local-openai-persona-policy","identity":{"model_id":"gpt-5-mini","provider_id":"openai","version":"gpt-5-mini-2025-08-07"},"input_micros_per_million":250000,"logging":"allowed","max_latency_ns":120000000000,"output_micros_per_million":2000000,"output_schema_digest":"sha256:2d381ce0f49738a7040285e57a84c43c3c43793fe3d325e5cb5a4783e7c1290f","processing_region":"global","purpose":"persona.reply","retention_mode":"BOUNDED","retention_ns":2592000000000000,"schema_version":1,"synthetic_tenants":["harborcare-demo","ironridge-demo"],"tool_ceiling":[{"digest":"sha256:ca6172826c3f867139cd4e540767cb76683a216e176d5883ea48ce944578fc29","id":"hcmnext.skill.knowledge_search_with_citations","schema_version":1,"version":1},{"digest":"sha256:1f7e3a8ac15c6530bdfecab8a76c3e68208aefbeba89f9bc2426cbb3b2347790","id":"persona.chat_reply","schema_version":1,"version":1}],"tool_schema_digest":"sha256:3df97e955059f650e235a550e05820645ae911e76aab2d8ec885326665f7d6e4","training_use":"denied","version":2}`
	if string(raw) != expected {
		t.Fatalf("local processing contract changed:\n%s", raw)
	}
}
