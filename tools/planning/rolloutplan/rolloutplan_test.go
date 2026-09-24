package rolloutplan

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

func validPlan() Plan {
	return Plan{
		Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		KillTarget: configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"},
		Target:     "fleet=prod-eu AND ring<=2",
		Stages: []Stage{
			{Name: "canary", HealthWindow: HealthWindow{DurationSeconds: 600, MaxErrors: 0}},
			{Name: "broad", HealthWindow: HealthWindow{DurationSeconds: 1800, MaxErrors: 2}},
		},
		StopCriteria:     "error_rate > 0.01",
		ExpandCriteria:   "error_rate < 0.001 AND latency_p99_ms < 250",
		RollbackCriteria: "error_rate > 0.05 OR health_window_breached",
		KillCriteria:     "data_loss_detected OR rollback_failed",
		Owner:            "release-captains",
		Expiry:           "2030-01-01T00:00:00Z",
	}
}

func codes(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Code)
	}
	return out
}

func hasCode(findings []Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestTodo_ROLLOUT_001(t *testing.T) {
	if findings := Validate(validPlan()); len(findings) != 0 {
		t.Fatalf("valid plan rejected: %+v", findings)
	}
	for _, kind := range []ArtifactType{
		ArtifactWorkflow, ArtifactRule, ArtifactCapability,
		ArtifactConnector, ArtifactAgent, ArtifactUI, ArtifactSchema,
	} {
		p := validPlan()
		p.Artifact.Type = kind
		if findings := Validate(p); len(findings) != 0 {
			t.Errorf("artifact kind %q rejected: %+v", kind, findings)
		}
	}
	missing := []struct {
		name   string
		mutate func(*Plan)
		code   string
	}{
		{"artifact type", func(p *Plan) { p.Artifact.Type = "" }, MissingArtifact},
		{"artifact version", func(p *Plan) { p.Artifact.Version = "" }, MissingVersion},
		{"target expression", func(p *Plan) { p.Target = "" }, MissingTarget},
		{"stages", func(p *Plan) { p.Stages = nil }, MissingStages},
		{"health window", func(p *Plan) { p.Stages[0].HealthWindow.DurationSeconds = 0 }, MissingHealthWindow},
		{"stop criteria", func(p *Plan) { p.StopCriteria = "" }, MissingStopCriteria},
		{"expand criteria", func(p *Plan) { p.ExpandCriteria = "" }, MissingExpandCriteria},
		{"rollback criteria", func(p *Plan) { p.RollbackCriteria = "" }, MissingRollbackCriteria},
		{"kill criteria", func(p *Plan) { p.KillCriteria = "" }, MissingKillCriteria},
		{"owner", func(p *Plan) { p.Owner = "" }, MissingOwner},
		{"expiry", func(p *Plan) { p.Expiry = "" }, MissingExpiry},
	}
	for _, tc := range missing {
		t.Run("missing_"+strings.ReplaceAll(tc.name, " ", "_"), func(t *testing.T) {
			p := validPlan()
			tc.mutate(&p)
			findings := Validate(p)
			if !hasCode(findings, tc.code) {
				t.Fatalf("missing %s accepted, findings=%v", tc.name, codes(findings))
			}
		})
	}
	t.Run("unknown artifact kind rejected", func(t *testing.T) {
		p := validPlan()
		p.Artifact.Type = "CRONJOB"
		if findings := Validate(p); !hasCode(findings, UnknownArtifact) {
			t.Fatalf("unknown artifact accepted, findings=%v", codes(findings))
		}
	})
	t.Run("malformed version rejected", func(t *testing.T) {
		p := validPlan()
		p.Artifact.Version = "latest"
		if findings := Validate(p); !hasCode(findings, InvalidVersion) {
			t.Fatalf("malformed version accepted, findings=%v", codes(findings))
		}
	})
	t.Run("digest deterministic", func(t *testing.T) {
		a, err := Compile(validPlan())
		if err != nil {
			t.Fatal(err)
		}
		b, err := Compile(validPlan())
		if err != nil {
			t.Fatal(err)
		}
		if a.Digest != b.Digest || !strings.HasPrefix(a.Digest, "sha256:") {
			t.Fatalf("nondeterministic digest: %q vs %q", a.Digest, b.Digest)
		}
	})
	t.Run("invalid plan has no digest", func(t *testing.T) {
		p := validPlan()
		p.Owner = ""
		if _, err := Compile(p); err == nil {
			t.Fatal("invalid plan compiled without error")
		}
	})
}
