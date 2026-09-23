package journeyclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestParseConfigReadsTheShellIsland is the contract test between this
// client and the page that loads it: the island is marshalled from the
// server's own struct (internal/humanwork/workspace.JourneyConfig) and
// parsed by this package's, so a renamed json tag on either side fails here
// rather than in a browser.
func TestParseConfigReadsTheShellIsland(t *testing.T) {
	island, err := json.Marshal(workspace.JourneyConfig{
		TunnelURL: "wss://cell.example/grpc",
		Bearer:    "tok_abc123",
		Tenant:    "northwind",
		Subject:   "avery.okafor@northwind.example",
		Roles:     []string{"hr.business_partner", "promotion.approver"},
		LauncherActions: []workspace.LauncherActionConfig{{
			ID: "promote-worker", Availability: "available", Priority: 3,
		}},
		Purpose:      "promotion_review",
		JourneysPath: workspace.PathJourney,
		GiphyAPIKey:  "giphy-public-client-key",
	})
	if err != nil {
		t.Fatalf("marshalling the shell's own config: %v", err)
	}

	cfg, err := ParseConfig(island)
	if err != nil {
		t.Fatalf("ParseConfig of a document the shell produced: %v", err)
	}
	if cfg.TunnelURL != "wss://cell.example/grpc" {
		t.Errorf("TunnelURL = %q", cfg.TunnelURL)
	}
	if cfg.Bearer != "tok_abc123" {
		t.Errorf("Bearer = %q", cfg.Bearer)
	}
	if cfg.Tenant != "northwind" {
		t.Errorf("Tenant = %q", cfg.Tenant)
	}
	if cfg.Subject != "avery.okafor@northwind.example" {
		t.Errorf("Subject = %q", cfg.Subject)
	}
	if len(cfg.Roles) != 2 || cfg.Roles[0] != "hr.business_partner" {
		t.Errorf("Roles = %v", cfg.Roles)
	}
	if len(cfg.LauncherActions) != 1 || cfg.LauncherActions[0].ID != "promote-worker" || cfg.LauncherActions[0].Priority != 3 {
		t.Errorf("LauncherActions = %+v", cfg.LauncherActions)
	}
	if cfg.Purpose != "promotion_review" {
		t.Errorf("Purpose = %q", cfg.Purpose)
	}
	if cfg.JourneysPath != workspace.PathJourney {
		t.Errorf("JourneysPath = %q, want %q", cfg.JourneysPath, workspace.PathJourney)
	}
	if cfg.GiphyAPIKey != "giphy-public-client-key" {
		t.Errorf("GiphyAPIKey = %q", cfg.GiphyAPIKey)
	}
}

// TestDefaultJourneysPathMirrorsTheServer keeps the fallback address honest.
func TestDefaultJourneysPathMirrorsTheServer(t *testing.T) {
	if DefaultJourneysPath != workspace.PathJourney {
		t.Fatalf("DefaultJourneysPath = %q, want the server's %q", DefaultJourneysPath, workspace.PathJourney)
	}
	if WorkspacePath != workspace.PathPromotion {
		t.Fatalf("WorkspacePath = %q, want the server's %q", WorkspacePath, workspace.PathPromotion)
	}
}

func TestParseConfigRefusals(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"not JSON at all", "<!doctype html>", ErrConfigMalformed},
		{"a JSON array", `["tunnel_url"]`, ErrConfigMalformed},
		{"an empty document", "", ErrConfigMalformed},
		{"no tunnel", `{"bearer":"tok"}`, ErrConfigNoTunnel},
		{"an http tunnel", `{"tunnel_url":"https://cell.example/grpc","bearer":"tok"}`, ErrConfigNoTunnel},
		{"a tunnel with no host", `{"tunnel_url":"ws:///grpc","bearer":"tok"}`, ErrConfigNoTunnel},
		{"no bearer", `{"tunnel_url":"ws://cell.example/grpc"}`, ErrConfigNoBearer},
		{"a blank bearer", `{"tunnel_url":"ws://cell.example/grpc","bearer":"   "}`, ErrConfigNoBearer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(c.in))
			if !errors.Is(err, c.want) {
				t.Fatalf("ParseConfig(%q) error = %v, want %v", c.in, err, c.want)
			}
		})
	}
}

func TestParseConfigAcceptsBothSchemesAndDefaultsThePath(t *testing.T) {
	for _, raw := range []string{
		`{"tunnel_url":"ws://localhost:8080/grpc","bearer":"tok"}`,
		`{"tunnel_url":"wss://cell.example/grpc","bearer":"tok"}`,
	} {
		cfg, err := ParseConfig([]byte(raw))
		if err != nil {
			t.Fatalf("ParseConfig(%q): %v", raw, err)
		}
		if cfg.JourneysPath != DefaultJourneysPath {
			t.Errorf("JourneysPath = %q, want the default %q", cfg.JourneysPath, DefaultJourneysPath)
		}
	}
}

// TestParseConfigIgnoresFieldsItDoesNotKnow keeps the shell able to add a
// display fact without a lockstep client deployment.
func TestParseConfigIgnoresFieldsItDoesNotKnow(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"tunnel_url":"ws://cell.example/grpc","bearer":"tok","cell_id":"cell-us-east-1a"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Bearer != "tok" {
		t.Errorf("Bearer = %q", cfg.Bearer)
	}
}

func TestDialTarget(t *testing.T) {
	cases := map[string]string{
		"ws://localhost:8080/grpc":   "passthrough:///localhost:8080",
		"wss://cell.example/grpc":    "passthrough:///cell.example",
		"wss://cell.example:443/x/y": "passthrough:///cell.example:443",
		// A target that cannot be parsed is still handed through rather than
		// dropped: the dialer ignores it anyway, and an empty target would
		// fail the client construction for the wrong reason.
		"": "passthrough:///",
	}
	for tunnel, want := range cases {
		got := Config{TunnelURL: tunnel}.DialTarget()
		if got != want {
			t.Errorf("Config{TunnelURL:%q}.DialTarget() = %q, want %q", tunnel, got, want)
		}
	}
}

func TestTodo_WEB_241_ConfigPreservesAuthoritativeEmptyFeaturePolicy(t *testing.T) {
	raw, err := json.Marshal(workspace.JourneyConfig{
		TunnelURL: "ws://cell.example/grpc", Bearer: "tok",
		FeaturePermissions: []roleaccess.FeaturePermission{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"feature_permissions":[]`)) {
		t.Fatalf("explicit empty feature policy was omitted: %s", raw)
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FeaturePermissions == nil {
		t.Fatal("authoritative empty feature policy became rolling-upgrade nil")
	}
}

func TestTodo_WEB_241_ClientFeatureGateRequiresPageAndFeature(t *testing.T) {
	cfg := Config{
		PagePermissions:    []PagePermission{{PageID: "journeys", View: true, Create: true}},
		FeaturePermissions: []FeaturePermission{{PageID: "journeys", FeatureID: "promotion_request", View: true, Create: true}},
	}
	if !cfg.CanFeatureAction("journeys", "promotion_request", "create") {
		t.Fatal("matching page and feature grant denied")
	}
	cfg.PagePermissions[0].Create = false
	if cfg.CanFeatureAction("journeys", "promotion_request", "create") {
		t.Fatal("feature grant escaped its page boundary")
	}
}
