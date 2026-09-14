// Package productcorrelation correlates product telemetry across surfaces
// without protected payloads (ALIGN-053).
//
// A product action leaves telemetry on several surfaces -- the UI that
// started it, the intent service, the workflow runtime, the ledger. An
// authorized operator reconstructs one action's path by its correlation id.
// Every event passes one fail-closed presentation boundary first: only
// attribute keys the platform telemetry allowlist admits for log or span
// signals survive, an unknown key or one admitted only elsewhere is dropped, and any surviving value
// shaped like a protected payload (an email address, a national identifier,
// an oversized blob) is dropped too. Drops are counted by reason, never
// echoed.
package productcorrelation

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

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Surface is where an event was emitted.
type Surface string

// Surfaces, in the order a product action normally crosses them.
const (
	SurfaceUI       Surface = "UI"
	SurfaceIntent   Surface = "INTENT"
	SurfaceWorkflow Surface = "WORKFLOW"
	SurfaceLedger   Surface = "LEDGER"
)

// Surfaces lists the closed surface vocabulary.
func Surfaces() []Surface { return []Surface{SurfaceUI, SurfaceIntent, SurfaceWorkflow, SurfaceLedger} }

func (s Surface) valid() bool {
	for _, v := range Surfaces() {
		if s == v {
			return true
		}
	}
	return false
}

// Drop reasons.
const (
	DropSignalNotAllowed = "SIGNAL_NOT_ALLOWED"
	DropUnknownKey       = "UNKNOWN_KEY"
	DropPayloadShape     = "PAYLOAD_SHAPE"
	DropUncorrelated     = "UNCORRELATED_EVENT"
	DropForeignTenant    = "FOREIGN_TENANT"
)

// MaxValueLen bounds an attribute value.
const MaxValueLen = 128

// Sentinels.
var (
	ErrInvalid      = errors.New("productcorrelation: invalid input")
	ErrUnauthorized = errors.New("productcorrelation: operator authorization required")
)

// Event is one telemetry event from a surface.
type Event struct {
	Surface       Surface
	Name          string
	Tenant        values.TenantId
	CorrelationID string
	TraceID       string
	At            time.Time
	Attributes    map[string]string
}

// Step is one sanitized event in a timeline.
type Step struct {
	Surface    Surface           `json:"surface"`
	Name       string            `json:"name"`
	At         time.Time         `json:"at"`
	TraceID    string            `json:"trace_id,omitempty"`
	Attributes map[string]string `json:"attributes"`
}

// Timeline is one correlated product action.
type Timeline struct {
	CorrelationID string    `json:"correlation_id"`
	Steps         []Step    `json:"steps"`
	Surfaces      []Surface `json:"surfaces"`
	// Complete reports that every surface in the closed vocabulary appears.
	Complete bool `json:"complete"`
}

// Report is the correlated view.
type Report struct {
	Tenant    values.TenantId `json:"tenant"`
	Timelines []Timeline      `json:"timelines"`
	Dropped   map[string]int  `json:"dropped"`
}

// Digest identifies a report.
func (r Report) Digest() string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var (
	emailShape    = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
	nationalShape = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
)

func payloadShaped(v string) bool {
	return len(v) > MaxValueLen || emailShape.MatchString(v) || nationalShape.MatchString(v) || strings.ContainsAny(v, "\n\r")
}

// Correlate builds the report for an operator from raw events.
func Correlate(principal *trust.Principal, allow *telemetry.Allowlist, events []Event) (Report, error) {
	if err := admin.RequireOperator(principal); err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}
	if allow == nil {
		return Report{}, fmt.Errorf("%w: allowlist is required", ErrInvalid)
	}
	report := Report{Tenant: principal.Tenant(), Timelines: []Timeline{}, Dropped: map[string]int{}}
	byID := map[string][]Step{}
	for i, e := range events {
		if !e.Surface.valid() || strings.TrimSpace(e.Name) == "" || e.At.IsZero() {
			return Report{}, fmt.Errorf("%w: event %d has no surface, name or instant", ErrInvalid, i)
		}
		if e.Tenant != principal.Tenant() {
			report.Dropped[DropForeignTenant]++
			continue
		}
		if strings.TrimSpace(e.CorrelationID) == "" {
			report.Dropped[DropUncorrelated]++
			continue
		}
		step := Step{Surface: e.Surface, Name: e.Name, At: e.At.UTC(), TraceID: e.TraceID, Attributes: map[string]string{}}
		for key, value := range e.Attributes {
			def, known := allow.Lookup(key)
			switch {
			case !known:
				report.Dropped[DropUnknownKey]++
			case def.Class == telemetry.ClassProhibited || !(allow.AllowsSignal(key, telemetry.SignalLog) || allow.AllowsSignal(key, telemetry.SignalSpan)):
				report.Dropped[DropSignalNotAllowed]++
			case payloadShaped(value):
				report.Dropped[DropPayloadShape]++
			default:
				step.Attributes[key] = value
			}
		}
		byID[e.CorrelationID] = append(byID[e.CorrelationID], step)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		steps := byID[id]
		sort.SliceStable(steps, func(i, j int) bool {
			if !steps[i].At.Equal(steps[j].At) {
				return steps[i].At.Before(steps[j].At)
			}
			if steps[i].Surface != steps[j].Surface {
				return surfaceRank(steps[i].Surface) < surfaceRank(steps[j].Surface)
			}
			return steps[i].Name < steps[j].Name
		})
		seen := map[Surface]bool{}
		for _, s := range steps {
			seen[s.Surface] = true
		}
		tl := Timeline{CorrelationID: id, Steps: steps, Surfaces: []Surface{}}
		for _, s := range Surfaces() {
			if seen[s] {
				tl.Surfaces = append(tl.Surfaces, s)
			}
		}
		tl.Complete = len(tl.Surfaces) == len(Surfaces())
		report.Timelines = append(report.Timelines, tl)
	}
	return report, nil
}

func surfaceRank(s Surface) int {
	for i, v := range Surfaces() {
		if v == s {
			return i
		}
	}
	return len(Surfaces())
}
