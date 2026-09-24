// Package health publishes a read-only connector health projection.
package health

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Version is the contract version of this projection.
func Version() int { return 1 }

// Status is the normalized health state visible to operations and workflows.
type Status string

const (
	Healthy  Status = "HEALTHY"
	Degraded Status = "DEGRADED"
	Incident Status = "INCIDENT"
	Unknown  Status = "UNKNOWN"
)

// Kind names the dependency signal that contributed to a projection.
type Kind string

const (
	Authentication Kind = "AUTHENTICATION"
	Permission     Kind = "PERMISSION"
	Schema         Kind = "SCHEMA"
	Queue          Kind = "QUEUE"
	Capacity       Kind = "CAPACITY"
	Error          Kind = "ERROR"
	Webhook        Kind = "WEBHOOK"
	Sync           Kind = "SYNC"
	Observation    Kind = "OBSERVATION"
)

// Signal is one bounded, non-secret observation from a connector dependency.
type Signal struct {
	Kind       Kind      `json:"kind"`
	Status     Status    `json:"status"`
	Cause      string    `json:"cause"`
	Watermark  time.Time `json:"watermark"`
	FreshUntil time.Time `json:"fresh_until"`
	Capability string    `json:"capability,omitempty"`
	Workflow   string    `json:"workflow,omitempty"`
	Deadline   time.Time `json:"deadline,omitempty"`
}

// Input is the immutable source view used to create a health projection.
type Input struct {
	TenantID     string
	ConnectionID string
	Now          time.Time
	Signals      []Signal
}

// Cause is a stable explanation suitable for a UI or incident intake.
type Cause struct {
	Kind      Kind      `json:"kind"`
	Code      string    `json:"code"`
	Detail    string    `json:"detail"`
	Watermark time.Time `json:"watermark"`
}

// Impact identifies a downstream capability, workflow, or deadline affected
// by the dependency condition.
type Impact struct {
	Capability string    `json:"capability,omitempty"`
	Workflow   string    `json:"workflow,omitempty"`
	Deadline   time.Time `json:"deadline,omitempty"`
}

// Report is a deterministic health projection. It never grants or changes
// connection authority.
type Report struct {
	TenantID     string    `json:"tenant_id"`
	ConnectionID string    `json:"connection_id"`
	Status       Status    `json:"status"`
	Causes       []Cause   `json:"causes"`
	Watermark    time.Time `json:"watermark"`
	Impacts      []Impact  `json:"impacts"`
	Digest       string    `json:"digest"`
}

var (
	ErrMissingConnection = errors.New("health: tenant and connection are required")
	ErrInvalidSignal     = errors.New("health: invalid signal")
)

// Project folds dependency observations into one stable health view.
func Project(in Input) (Report, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ConnectionID) == "" {
		return Report{}, ErrMissingConnection
	}
	if in.Now.IsZero() {
		return Report{}, fmt.Errorf("%w: now is required", ErrInvalidSignal)
	}
	out := Report{TenantID: in.TenantID, ConnectionID: in.ConnectionID, Status: Healthy}
	seenCause := make(map[string]bool)
	seenImpact := make(map[string]bool)
	for _, s := range in.Signals {
		if !validKind(s.Kind) || !validStatus(s.Status) {
			return Report{}, fmt.Errorf("%w: kind=%q status=%q", ErrInvalidSignal, s.Kind, s.Status)
		}
		status := s.Status
		if !s.FreshUntil.IsZero() && in.Now.After(s.FreshUntil) {
			status = Unknown
		}
		if status != Healthy {
			if out.Status == Healthy || (out.Status == Degraded && status == Incident) {
				out.Status = status
			} else if out.Status != Incident && status == Unknown {
				out.Status = Unknown
			}
			cause := Cause{Kind: s.Kind, Code: statusCode(status, s.Kind), Detail: safeDetail(s.Cause, status), Watermark: s.Watermark}
			causeKey := fmt.Sprintf("%s|%s|%s|%s", cause.Kind, cause.Code, cause.Detail, cause.Watermark.UTC().Format(time.RFC3339Nano))
			if !seenCause[causeKey] {
				seenCause[causeKey] = true
				out.Causes = append(out.Causes, cause)
			}
		}
		impact := Impact{Capability: strings.TrimSpace(s.Capability), Workflow: strings.TrimSpace(s.Workflow), Deadline: s.Deadline}
		if impact.Capability != "" || impact.Workflow != "" || !impact.Deadline.IsZero() {
			key := fmt.Sprintf("%s|%s|%s", impact.Capability, impact.Workflow, impact.Deadline.UTC().Format(time.RFC3339Nano))
			if !seenImpact[key] {
				seenImpact[key] = true
				out.Impacts = append(out.Impacts, impact)
			}
		}
		if s.Watermark.After(out.Watermark) {
			out.Watermark = s.Watermark
		}
	}
	if len(in.Signals) == 0 {
		out.Status = Unknown
		out.Causes = append(out.Causes, Cause{Kind: Observation, Code: "NO_OBSERVATION", Detail: "no dependency observation is available"})
	}
	if len(out.Causes) == 0 && out.Status == Healthy {
		out.Causes = []Cause{{Kind: Observation, Code: "ALL_CHECKS_PASSED", Detail: "all dependency observations are healthy"}}
	}
	sort.Slice(out.Causes, func(i, j int) bool { return causeSortKey(out.Causes[i]) < causeSortKey(out.Causes[j]) })
	sort.Slice(out.Impacts, func(i, j int) bool { return impactSortKey(out.Impacts[i]) < impactSortKey(out.Impacts[j]) })
	out.Digest = digest(out)
	return out, nil
}

// Explain returns a stable human-readable projection without provider data.
func Explain(r Report) string {
	return fmt.Sprintf("%s connector=%s causes=%d impacts=%d watermark=%s", r.Status, r.ConnectionID, len(r.Causes), len(r.Impacts), r.Watermark.UTC().Format(time.RFC3339Nano))
}

func validKind(k Kind) bool {
	switch k {
	case Authentication, Permission, Schema, Queue, Capacity, Error, Webhook, Sync, Observation:
		return true
	default:
		return false
	}
}

func validStatus(s Status) bool {
	return s == Healthy || s == Degraded || s == Incident || s == Unknown
}

func statusCode(s Status, k Kind) string {
	if s == Unknown {
		return "UNKNOWN_" + string(k)
	}
	return string(s) + "_" + string(k)
}

func safeDetail(detail string, status Status) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return strings.ToLower(string(status)) + " dependency signal"
	}
	if credentialMaterial.MatchString(detail) {
		return "dependency reported sensitive diagnostic detail"
	}
	return detail
}

var credentialMaterial = regexp.MustCompile(`(?i)(bearer\s+\S+|(?:access[_-]?token|refresh[_-]?token|client[_-]?secret|password|api[_-]?key|token|secret|credential)\s*[:=]\s*[^&\s,;]+|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|\b[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b)`)

func causeSortKey(c Cause) string { return fmt.Sprintf("%s|%s|%s", c.Kind, c.Code, c.Detail) }
func impactSortKey(i Impact) string {
	return fmt.Sprintf("%s|%s|%s", i.Capability, i.Workflow, i.Deadline.UTC().Format(time.RFC3339Nano))
}

func digest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
