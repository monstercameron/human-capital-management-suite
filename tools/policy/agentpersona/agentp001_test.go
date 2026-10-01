package agentpersona

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const repoRoot = "../../.."

type decision struct {
	SchemaVersion int              `yaml:"schema_version"`
	DecisionID    string           `yaml:"decision_id"`
	Title         string           `yaml:"title"`
	Status        string           `yaml:"status"`
	Date          string           `yaml:"decision_date"`
	Owner         string           `yaml:"owner"`
	Refs          []string         `yaml:"refs"`
	Defaults      []map[string]any `yaml:"defaults"`
}

func loadDecision(t *testing.T) (decision, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "definitions", "planning", "agent-persona-decisions.yaml"))
	if err != nil {
		t.Fatalf("read persona decision: %v", err)
	}
	var d decision
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("parse persona decision: %v", err)
	}
	return d, b
}

func canonicalDigest(t *testing.T, b []byte) string {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(b, &value); err != nil {
		t.Fatalf("parse YAML for digest: %v", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func values(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func byID(t *testing.T, d decision, id string) map[string]any {
	t.Helper()
	for _, item := range d.Defaults {
		if item["id"] == id {
			return item
		}
	}
	t.Fatalf("missing decision default %q", id)
	return nil
}

func TestTodo_AGENTP_001(t *testing.T) {
	d, _ := loadDecision(t)
	if d.SchemaVersion != 1 || d.DecisionID != "AGENTP-001" || d.Status != "DECIDED" || d.Date != "2026-09-28" {
		t.Fatalf("decision identity invalid: %+v", d)
	}
	want := []string{"authority", "invocation", "context", "audience_floor", "writes_and_approvals", "ownership_and_lifecycle", "placement", "limits", "stop", "ux", "audit", "output_safety", "release_gate"}
	got := make([]string, 0, len(d.Defaults))
	owners := map[string]bool{}
	todoBytes, err := os.ReadFile(filepath.Join(repoRoot, "planning", "todos.md"))
	if err != nil {
		t.Fatalf("read owner todo index: %v", err)
	}
	for _, item := range d.Defaults {
		id, _ := item["id"].(string)
		got = append(got, id)
		if len(values(item["owner_todos"])) == 0 {
			t.Errorf("default %s has no owning todos", id)
		}
		for _, owner := range values(item["owner_todos"]) {
			if !strings.HasPrefix(owner, "AGENTP-") {
				t.Errorf("default %s has non-persona owner %s", id, owner)
			}
			if !strings.Contains(string(todoBytes), "`"+owner+"`") {
				t.Errorf("default %s maps to missing todo %s", id, owner)
			}
			owners[owner] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default order = %v, want %v", got, want)
	}
	for _, ref := range d.Refs {
		path := strings.SplitN(ref, "#", 2)[0]
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil {
			t.Errorf("source link %q does not resolve: %v", ref, err)
		}
	}
	if len(owners) < 10 {
		t.Errorf("only %d distinct owning todos mapped", len(owners))
	}
	authority := byID(t, d, "authority")
	if authority["principal"] != "INVOKER" || authority["mode"] != "ON_BEHALF_OF" || authority["authority_decided_per_call_by"] != "AGENT2-005" {
		t.Errorf("authority default is not invoker-delegated: %+v", authority)
	}
	if !reflect.DeepEqual(values(authority["authority_intersection"]), []string{"persona_version_skill_pins", "installation_grant", "channel_policy_ceiling", "invoker_current_authority"}) {
		t.Errorf("authority intersection = %v", authority["authority_intersection"])
	}
	invocation := byID(t, d, "invocation")
	if invocation["implicit_persona_routing"] != false || invocation["agent_discovery"] != false || !hasAll(values(invocation["rejected_triggers"]), []string{"quoted_text", "forwarded_text", "edited_text", "bot_post", "agent_post", "persona_to_persona_mention"}) {
		t.Errorf("unsafe invocation defaults: %+v", invocation)
	}
	context := byID(t, d, "context")
	if context["default_scope"] != "INVOKING_POST_AND_THREAD" || context["maximum_recent_posts"] != 50 || context["peer_content_taint"] != "UNTRUSTED_PEER" {
		t.Errorf("context bounds/taint invalid: %+v", context)
	}
	if len(d.Refs) < 5 {
		t.Errorf("source links incomplete: %v", d.Refs)
	}
}

func TestTodo_AGENTP_001_Golden(t *testing.T) {
	d, b := loadDecision(t)
	goldenBytes, err := os.ReadFile("testdata/agentp001.golden.json")
	if err != nil {
		t.Fatalf("read decision golden: %v", err)
	}
	var golden struct {
		Digest     string   `json:"canonical_digest"`
		ID         string   `json:"decision_id"`
		DefaultIDs []string `json:"default_ids"`
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse decision golden: %v", err)
	}
	if got := canonicalDigest(t, b); got != golden.Digest {
		t.Errorf("canonical digest = %s, want %s", got, golden.Digest)
	}
	ids := make([]string, 0, len(d.Defaults))
	for _, item := range d.Defaults {
		ids = append(ids, item["id"].(string))
	}
	if d.DecisionID != golden.ID || !reflect.DeepEqual(ids, golden.DefaultIDs) {
		t.Errorf("golden identity/defaults changed: %s %v", d.DecisionID, ids)
	}
}

func TestTodo_AGENTP_001_Security(t *testing.T) {
	d, _ := loadDecision(t)
	a := byID(t, d, "authority")
	if a["principal"] != "INVOKER" || a["sponsored_mode_from_mention"] != false || a["persona_service_credential"] != "NONE" {
		t.Errorf("persona can escape invoker authority: %+v", a)
	}
	if hasAll(values(a["prohibited_authorities"]), []string{"author", "owner", "installer", "administrator", "persona"}) == false {
		t.Errorf("prohibited principals incomplete: %v", a["prohibited_authorities"])
	}
	audience := byID(t, d, "audience_floor")
	if !hasAll(values(audience["shared_post_requires"]), []string{"EVERY_SOURCE_RECORD_FIELD_READABLE_BY_AUDIENCE", "EVERY_DATA_CLASS_ALLOWED_BY_CHANNEL_POLICY"}) || audience["check_timing"] != "COMMIT" {
		t.Errorf("shared output lacks audience-floor commit check: %+v", audience)
	}
	writes := byID(t, d, "writes_and_approvals")
	card := writes["approval_card"].(map[string]any)
	if card["audience"] != "INVOKER_ONLY" || card["only_invoker_may_approve"] != true {
		t.Errorf("approval card is not invoker-only: %+v", card)
	}
	for _, key := range []string{"other_member_may_approve", "channel_owner_may_approve", "reaction_or_reply_may_approve", "persona_may_approve"} {
		if card[key] != false {
			t.Errorf("unsafe approval rule %s=%v", key, card[key])
		}
	}
	if !hasAll(values(card["binds"]), []string{"intent_digest", "invocation", "invoker"}) {
		t.Errorf("approval is not bound to exact invocation and digest: %v", card["binds"])
	}
	release := byID(t, d, "release_gate")
	if release["gate_id"] != "G-AGENT-PERSONA" || release["requires_gate"] != "G-AGENT-OBO" {
		t.Errorf("release gate is not chained to OBO: %+v", release)
	}
	first := release["first_release"].(map[string]any)
	if first["t3_persona_surface"] != "ONE_TO_ONE_DM_ONLY" || first["t4_persona_enabled"] != false {
		t.Errorf("first release permits unsafe write surface: %+v", first)
	}
}

func hasAll(got, want []string) bool {
	set := map[string]bool{}
	for _, value := range got {
		set[value] = true
	}
	for _, value := range want {
		if !set[value] {
			return false
		}
	}
	return true
}
