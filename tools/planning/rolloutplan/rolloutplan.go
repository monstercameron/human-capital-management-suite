// Package rolloutplan compiles the multi-artifact rollout plan
// (ROLLOUT-001): one version-pinned artifact plus its target expression,
// ordered stages with health windows, stop/expand/rollback/kill criteria,
// owner and expiry, with exact findings for every missing dimension and a
// deterministic digest over the canonical encoding. It is kernel-pure:
// pure validation plus digest compilation, no database, network or mutable
// global state.
package rolloutplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

// ArtifactType is one supported rollout artifact kind.
type ArtifactType string

// Supported artifact kinds.
const (
	ArtifactWorkflow   ArtifactType = "WORKFLOW"
	ArtifactRule       ArtifactType = "RULE"
	ArtifactCapability ArtifactType = "CAPABILITY"
	ArtifactConnector  ArtifactType = "CONNECTOR"
	ArtifactAgent      ArtifactType = "AGENT"
	ArtifactUI         ArtifactType = "UI"
	ArtifactSchema     ArtifactType = "SCHEMA"
)

// validArtifact reports whether a kind is a supported rollout artifact.
func validArtifact(kind ArtifactType) bool {
	switch kind {
	case ArtifactWorkflow, ArtifactRule, ArtifactCapability,
		ArtifactConnector, ArtifactAgent, ArtifactUI, ArtifactSchema:
		return true
	}
	return false
}

// Finding codes. MISSING_* mark required RED dimensions with no exact
// value; UNKNOWN_ARTIFACT and INVALID_VERSION mark values outside the
// supported contract.
const (
	MissingArtifact         = "MISSING_ARTIFACT"
	MissingVersion          = "MISSING_VERSION"
	InvalidVersion          = "INVALID_VERSION"
	MissingTarget           = "MISSING_TARGET"
	MissingStages           = "MISSING_STAGES"
	MissingStageName        = "MISSING_STAGE_NAME"
	DuplicateStage          = "DUPLICATE_STAGE"
	MissingHealthWindow     = "MISSING_HEALTH_WINDOW"
	MissingStopCriteria     = "MISSING_STOP_CRITERIA"
	MissingExpandCriteria   = "MISSING_EXPAND_CRITERIA"
	MissingRollbackCriteria = "MISSING_ROLLBACK_CRITERIA"
	MissingKillCriteria     = "MISSING_KILL_CRITERIA"
	MissingOwner            = "MISSING_OWNER"
	MissingExpiry           = "MISSING_EXPIRY"
	InvalidExpiry           = "INVALID_EXPIRY"
	UnknownArtifact         = "UNKNOWN_ARTIFACT"
)

// versionPattern pins artifact versions to exact vMAJOR.MINOR.PATCH so a
// rollout can never float to an unreviewed artifact.
var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Artifact is one version-pinned rollout artifact.
type Artifact struct {
	Type    ArtifactType `json:"type"`
	Version string       `json:"version"`
}

// HealthWindow bounds one stage: observe for DurationSeconds and tolerate
// at most MaxErrors before the stop criteria fire.
type HealthWindow struct {
	DurationSeconds int64 `json:"duration_seconds"`
	MaxErrors       int64 `json:"max_errors"`
}

// Stage is one ordered rollout step.
type Stage struct {
	Name         string       `json:"name"`
	HealthWindow HealthWindow `json:"health_window"`
}

// Plan is one multi-artifact rollout plan.
type Plan struct {
	Artifact         Artifact                      `json:"artifact"`
	KillTarget       configbundle.KillSwitchTarget `json:"kill_target"`
	Target           string                        `json:"target"`
	Stages           []Stage                       `json:"stages"`
	StopCriteria     string                        `json:"stop_criteria"`
	ExpandCriteria   string                        `json:"expand_criteria"`
	RollbackCriteria string                        `json:"rollback_criteria"`
	KillCriteria     string                        `json:"kill_criteria"`
	Owner            string                        `json:"owner"`
	Expiry           string                        `json:"expiry"`
}

// Finding is one exact validation failure.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

// String renders a finding deterministically for golden comparison.
func (f Finding) String() string {
	return f.Code + "|" + f.Field + "|" + f.Detail
}

// Validate checks every RED dimension of a plan and returns sorted exact
// findings; an empty slice means the plan is releasable.
func Validate(plan Plan) []Finding {
	var findings []Finding
	add := func(code, field, detail string) {
		findings = append(findings, Finding{Code: code, Field: field, Detail: detail})
	}
	if plan.Artifact.Type == "" {
		add(MissingArtifact, "artifact.type", "artifact type is required")
	} else if !validArtifact(plan.Artifact.Type) {
		add(UnknownArtifact, "artifact.type", fmt.Sprintf("unsupported artifact kind %q", plan.Artifact.Type))
	}
	if plan.Artifact.Version == "" {
		add(MissingVersion, "artifact.version", "artifact version is required")
	} else if !versionPattern.MatchString(plan.Artifact.Version) {
		add(InvalidVersion, "artifact.version", fmt.Sprintf("version %q is not vMAJOR.MINOR.PATCH", plan.Artifact.Version))
	}
	if strings.TrimSpace(plan.Target) == "" {
		add(MissingTarget, "target", "target expression is required")
	}
	if strings.TrimSpace(plan.KillTarget.TenantID) == "" || strings.TrimSpace(plan.KillTarget.TenantID) != plan.KillTarget.TenantID {
		add(MissingTarget, "kill_target.tenant_id", "kill target tenant is required and must be trimmed")
	}
	if strings.TrimSpace(plan.KillTarget.Capability) == "" || strings.TrimSpace(plan.KillTarget.Capability) != plan.KillTarget.Capability {
		add(MissingTarget, "kill_target.capability", "kill target capability is required and must be trimmed")
	}
	if len(plan.Stages) == 0 {
		add(MissingStages, "stages", "at least one stage is required")
	}
	seen := make(map[string]bool, len(plan.Stages))
	for i, stage := range plan.Stages {
		field := fmt.Sprintf("stages[%d]", i)
		if strings.TrimSpace(stage.Name) == "" {
			add(MissingStageName, field+".name", "stage name is required")
		} else if seen[stage.Name] {
			add(DuplicateStage, field+".name", fmt.Sprintf("duplicate stage %q", stage.Name))
		} else {
			seen[stage.Name] = true
		}
		if stage.HealthWindow.DurationSeconds <= 0 || stage.HealthWindow.MaxErrors < 0 {
			add(MissingHealthWindow, field+".health_window", "health window needs positive duration and non-negative error budget")
		}
	}
	if strings.TrimSpace(plan.StopCriteria) == "" {
		add(MissingStopCriteria, "stop_criteria", "stop criteria are required")
	}
	if strings.TrimSpace(plan.ExpandCriteria) == "" {
		add(MissingExpandCriteria, "expand_criteria", "expand criteria are required")
	}
	if strings.TrimSpace(plan.RollbackCriteria) == "" {
		add(MissingRollbackCriteria, "rollback_criteria", "rollback criteria are required")
	}
	if strings.TrimSpace(plan.KillCriteria) == "" {
		add(MissingKillCriteria, "kill_criteria", "kill criteria are required")
	}
	if strings.TrimSpace(plan.Owner) == "" {
		add(MissingOwner, "owner", "owner is required")
	}
	if strings.TrimSpace(plan.Expiry) == "" {
		add(MissingExpiry, "expiry", "expiry is required")
	} else if _, err := time.Parse(time.RFC3339, strings.TrimSpace(plan.Expiry)); err != nil {
		add(InvalidExpiry, "expiry", fmt.Sprintf("expiry %q is not RFC3339", plan.Expiry))
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].String() < findings[j].String()
	})
	return findings
}

// Compiled is one validated plan plus its canonical digest.
type Compiled struct {
	Plan   Plan   `json:"plan"`
	Digest string `json:"digest"`
}

// Compile validates a plan and returns its canonical digest. An invalid
// plan returns the exact findings and never a digest.
func Compile(plan Plan) (Compiled, error) {
	if findings := Validate(plan); len(findings) != 0 {
		parts := make([]string, 0, len(findings))
		for _, f := range findings {
			parts = append(parts, f.String())
		}
		return Compiled{}, fmt.Errorf("invalid rollout plan: %s", strings.Join(parts, "; "))
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return Compiled{}, err
	}
	sum := sha256.Sum256(raw)
	return Compiled{Plan: plan, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// MarshalReport renders a compiled plan deterministically.
func MarshalReport(compiled Compiled) ([]byte, error) {
	raw, err := json.MarshalIndent(compiled, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// DecodePlan parses one JSON-encoded plan for pipeline integration.
func DecodePlan(raw []byte) (Plan, error) {
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}
