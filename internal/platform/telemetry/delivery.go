// Delivery evidence and outage runbooks: OBS-008 publishes the
// observability delivery manifest with its evidence.
//
// A delivery manifest is incomplete without config, rule, dashboard
// and backend digests, a cardinality/cost report, a restore test and
// rehearsed collector, backend, privacy, cardinality and noisy-neighbor
// runbooks. A missing or failed telemetry signal rejects with
// OBS_008_REJECTED naming the offending field, state and version. The
// publisher is pure: it persists zero authoritative rows, business
// events, outbox entries, human work or provider requests.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DeliveryRejectedCode is the stable machine-readable refusal code.
const DeliveryRejectedCode = "OBS_008_REJECTED"

// DeliveryError names the offending field, state and version.
type DeliveryError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *DeliveryError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsDeliveryRejected unwraps an OBS_008_REJECTED refusal.
func AsDeliveryRejected(err error) (*DeliveryError, bool) {
	if err == nil {
		return nil, false
	}
	if rejected, ok := err.(*DeliveryError); ok && rejected.Code == DeliveryRejectedCode {
		return rejected, true
	}
	return nil, false
}

// CardinalityReport bounds series cardinality and cost.
type CardinalityReport struct {
	Series int
	Limit  int
	Cost   string
}

// Runbook is one rehearsed outage runbook.
type Runbook struct {
	Name        string
	Version     int
	RehearsedAt time.Time
}

// RequiredRunbooks is the closed rehearsed set.
var RequiredRunbooks = []string{"collector", "backend", "privacy", "cardinality", "noisy-neighbor"}

// DeliveryManifest is the complete delivery inventory.
type DeliveryManifest struct {
	Version         int
	ConfigDigest    string
	RuleDigest      string
	DashboardDigest string
	BackendDigest   string
	Cardinality     CardinalityReport
	RestoreTest     string
	Runbooks        []Runbook
}

// DeliveryEvidence is the published evidence receipt.
type DeliveryEvidence struct {
	ManifestVersion int
	Health          HealthState
	Runbooks        int
	Digest          string
}

func reject(field, state string, version int) *DeliveryError {
	return &DeliveryError{Code: DeliveryRejectedCode, Field: field, State: state, Version: version}
}

// publishEvidence admits one complete delivery. It writes nothing:
// the receipt is the entire effect.
func publishEvidence(manifest DeliveryManifest, signals Report, now time.Time) (DeliveryEvidence, error) {
	if manifest.Version <= 0 {
		return DeliveryEvidence{}, reject("manifest.version", "missing", manifest.Version)
	}
	for _, digest := range []struct{ field, value string }{
		{"manifest.config_digest", manifest.ConfigDigest},
		{"manifest.rule_digest", manifest.RuleDigest},
		{"manifest.dashboard_digest", manifest.DashboardDigest},
		{"manifest.backend_digest", manifest.BackendDigest},
	} {
		if strings.TrimSpace(digest.value) == "" {
			return DeliveryEvidence{}, reject(digest.field, "missing", manifest.Version)
		}
	}
	if manifest.Cardinality.Series <= 0 || manifest.Cardinality.Limit <= 0 {
		return DeliveryEvidence{}, reject("manifest.cardinality", "missing", manifest.Version)
	}
	if manifest.Cardinality.Series > manifest.Cardinality.Limit {
		return DeliveryEvidence{}, reject("manifest.cardinality", "over-limit", manifest.Version)
	}
	if strings.TrimSpace(manifest.Cardinality.Cost) == "" {
		return DeliveryEvidence{}, reject("manifest.cost", "missing", manifest.Version)
	}
	if strings.TrimSpace(manifest.RestoreTest) == "" {
		return DeliveryEvidence{}, reject("manifest.restore_test", "missing", manifest.Version)
	}
	rehearsed := map[string]bool{}
	for _, runbook := range manifest.Runbooks {
		if runbook.Version <= 0 || runbook.RehearsedAt.IsZero() || runbook.RehearsedAt.After(now) {
			return DeliveryEvidence{}, reject("manifest.runbooks."+runbook.Name, "unrehearsed", manifest.Version)
		}
		rehearsed[runbook.Name] = true
	}
	for _, name := range RequiredRunbooks {
		if !rehearsed[name] {
			return DeliveryEvidence{}, reject("manifest.runbooks."+name, "missing", manifest.Version)
		}
	}
	if signals.Health != HealthHealthy {
		field := "signals.health"
		if len(signals.MissingMetrics) > 0 {
			field = "signals.metric." + signals.MissingMetrics[0]
		} else if len(signals.MissingLogEvents) > 0 {
			field = "signals.log_event." + signals.MissingLogEvents[0]
		}
		return DeliveryEvidence{}, reject(field, string(signals.Health), signals.RequiredVersion)
	}
	return DeliveryEvidence{
		ManifestVersion: manifest.Version,
		Health:          signals.Health,
		Runbooks:        len(manifest.Runbooks),
		Digest:          digestDelivery(manifest, signals),
	}, nil
}

func digestDelivery(manifest DeliveryManifest, signals Report) string {
	runbooks := make([]string, 0, len(manifest.Runbooks))
	for _, runbook := range manifest.Runbooks {
		runbooks = append(runbooks, strings.Join([]string{
			runbook.Name, fmt.Sprint(runbook.Version), runbook.RehearsedAt.UTC().Format(time.RFC3339),
		}, "\x00"))
	}
	sort.Strings(runbooks)
	parts := []string{"obs008-delivery", fmt.Sprint(manifest.Version),
		manifest.ConfigDigest, manifest.RuleDigest, manifest.DashboardDigest, manifest.BackendDigest,
		fmt.Sprint(manifest.Cardinality.Series), fmt.Sprint(manifest.Cardinality.Limit), manifest.Cardinality.Cost,
		manifest.RestoreTest, string(signals.Health), fmt.Sprint(signals.RequiredVersion)}
	parts = append(parts, runbooks...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
