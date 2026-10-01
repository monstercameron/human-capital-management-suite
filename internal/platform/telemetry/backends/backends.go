// Package backends describes the portable, protocol-level telemetry stack.
// It does not import Prometheus, Loki, Tempo or Grafana SDKs: deployment and
// composition roots can bind these contracts to compatible OSS services.
package backends

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

func Explain() string { return "portable OSS metrics logs traces and dashboard backend contract" }

type Kind string

const (
	KindMetrics    Kind = "PROMETHEUS"
	KindLogs       Kind = "LOKI"
	KindTraces     Kind = "TEMPO"
	KindDashboards Kind = "GRAFANA"
)

// Spec is the deployment evidence for one backend.
type Spec struct {
	Kind             Kind
	Protocol         string
	License          string
	OpenSource       bool
	Private          bool
	TenantScoped     bool
	Retention        time.Duration
	RestoreSupported bool
	CostBudgetBytes  int64
}

// Signal is metadata only; payloads are redacted and content addressed before
// they reach a backend.
type Signal struct {
	ID          string
	TenantToken string
	Kind        Kind
	Region      string
	Digest      string
	ObservedAt  time.Time
	RetainUntil time.Time
	Bytes       int64
}

type Snapshot struct {
	Signals []Signal
}

type Qualification struct {
	Complete  bool
	Missing   []Kind
	Unsafe    []string
	LicenseOK bool
	CostOK    bool
	RestoreOK bool
}

type Stack struct {
	mu      sync.RWMutex
	specs   map[Kind]Spec
	signals map[signalKey]Signal
}

type signalKey struct {
	tenant string
	id     string
}

var (
	ErrInvalidSpec   = errors.New("telemetry backends: invalid backend specification")
	ErrInvalidSignal = errors.New("telemetry backends: invalid signal")
	ErrOverBudget    = errors.New("telemetry backends: cost budget exceeded")
	ErrExpired       = errors.New("telemetry backends: signal is outside retention")
)

// DefaultSpecs is the portable four-service baseline for Gate A.
func DefaultSpecs() []Spec {
	return []Spec{
		{Kind: KindMetrics, Protocol: "prometheus-remote-write", License: "Apache-2.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 7 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 1 << 30},
		{Kind: KindLogs, Protocol: "otlp-http", License: "AGPL-3.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 30 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 2 << 30},
		{Kind: KindTraces, Protocol: "otlp-http", License: "Apache-2.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 14 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 2 << 30},
		{Kind: KindDashboards, Protocol: "grafana-http-api", License: "AGPL-3.0", OpenSource: true, Private: true, TenantScoped: true, Retention: 365 * 24 * time.Hour, RestoreSupported: true, CostBudgetBytes: 1 << 30},
	}
}

func New(specs []Spec) (*Stack, Qualification, error) {
	stack := &Stack{specs: make(map[Kind]Spec), signals: make(map[signalKey]Signal)}
	for _, spec := range specs {
		if _, ok := stack.specs[spec.Kind]; ok {
			return nil, Qualification{}, fmt.Errorf("%w: duplicate kind %s", ErrInvalidSpec, spec.Kind)
		}
		stack.specs[spec.Kind] = spec
	}
	qualification := stack.Qualify()
	if !qualification.Complete {
		return nil, qualification, ErrInvalidSpec
	}
	return stack, qualification, nil
}

func Default() (*Stack, Qualification, error) { return New(DefaultSpecs()) }

func (s *Stack) Qualify() Qualification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := Qualification{LicenseOK: true, CostOK: true, RestoreOK: true}
	for _, kind := range []Kind{KindMetrics, KindLogs, KindTraces, KindDashboards} {
		spec, ok := s.specs[kind]
		if !ok {
			q.Missing = append(q.Missing, kind)
			continue
		}
		licenseIssue := false
		for _, reason := range specCompatibilityIssues(spec) {
			q.Unsafe = append(q.Unsafe, string(kind)+":"+reason)
			if reason == "license" {
				licenseIssue = true
				q.LicenseOK = false
			}
		}
		if !spec.OpenSource && !licenseIssue {
			q.LicenseOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":license")
		}
		if !spec.Private || !spec.TenantScoped || spec.Retention <= 0 {
			q.Unsafe = append(q.Unsafe, string(kind)+":isolation-or-retention")
		}
		if !spec.RestoreSupported {
			q.RestoreOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":restore")
		}
		if spec.CostBudgetBytes <= 0 {
			q.CostOK = false
			q.Unsafe = append(q.Unsafe, string(kind)+":cost")
		}
	}
	for kind := range s.specs {
		if !knownKind(kind) {
			q.Unsafe = append(q.Unsafe, string(kind)+":kind")
		}
	}
	sort.Strings(q.Unsafe)
	q.Complete = len(q.Missing) == 0 && len(q.Unsafe) == 0 && q.LicenseOK && q.CostOK && q.RestoreOK
	return q
}

func (s *Stack) Ingest(signal Signal) error {
	if err := validateSignal(signal); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.specs[signal.Kind]
	if !ok || !spec.Private || !spec.TenantScoped {
		return ErrInvalidSpec
	}
	return ingestInto(s.signals, s.specs, signal)
}

// Query always requires one exact tenant token and never supports a wildcard.
func (s *Stack) Query(tenantToken string, kind Kind, now time.Time) []Signal {
	if strings.TrimSpace(tenantToken) == "" || strings.ContainsAny(tenantToken, " \t\r\n") {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Signal, 0)
	for _, signal := range s.signals {
		if signal.TenantToken == tenantToken && signal.Kind == kind && (now.IsZero() || now.Before(signal.RetainUntil)) {
			result = append(result, signal)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *Stack) ExportSnapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := Snapshot{Signals: make([]Signal, 0, len(s.signals))}
	for _, signal := range s.signals {
		result.Signals = append(result.Signals, signal)
	}
	sort.Slice(result.Signals, func(i, j int) bool {
		if result.Signals[i].ID != result.Signals[j].ID {
			return result.Signals[i].ID < result.Signals[j].ID
		}
		return result.Signals[i].TenantToken < result.Signals[j].TenantToken
	})
	return result
}

func (s *Stack) Restore(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	newSignals := make(map[signalKey]Signal, len(snapshot.Signals))
	for _, signal := range snapshot.Signals {
		if err := ingestInto(newSignals, s.specs, signal); err != nil {
			return err
		}
	}
	s.signals = newSignals
	return nil
}

func ingestInto(signals map[signalKey]Signal, specs map[Kind]Spec, signal Signal) error {
	if err := validateSignal(signal); err != nil {
		return err
	}
	spec, ok := specs[signal.Kind]
	if !ok || !spec.Private || !spec.TenantScoped {
		return ErrInvalidSpec
	}
	if len(specCompatibilityIssues(spec)) != 0 || !spec.OpenSource || spec.Retention <= 0 || spec.CostBudgetBytes <= 0 {
		return ErrInvalidSpec
	}
	if err := validateRetention(signal, spec); err != nil {
		return err
	}
	key := signalKey{tenant: signal.TenantToken, id: signal.ID}
	if existing, exists := signals[key]; exists {
		if sameSignal(existing, signal) {
			return nil
		}
		return ErrInvalidSignal
	}
	var used int64
	for _, existing := range signals {
		if existing.Kind == signal.Kind {
			used += existing.Bytes
		}
	}
	if signal.Bytes > spec.CostBudgetBytes-used {
		return ErrOverBudget
	}
	signals[key] = signal
	return nil
}

func validateSignal(signal Signal) error {
	if strings.TrimSpace(signal.ID) == "" || strings.TrimSpace(signal.TenantToken) == "" || strings.ContainsAny(signal.TenantToken, " \t\r\n") || signal.Kind == "" || strings.TrimSpace(signal.Region) == "" || strings.TrimSpace(signal.Digest) == "" || signal.ObservedAt.IsZero() || signal.RetainUntil.IsZero() || signal.Bytes < 0 {
		return ErrInvalidSignal
	}
	if len(signal.TenantToken) > 128 {
		return ErrInvalidSignal
	}
	return nil
}

func validateRetention(signal Signal, spec Spec) error {
	retention := signal.RetainUntil.Sub(signal.ObservedAt)
	if retention <= 0 || retention > spec.Retention {
		return ErrExpired
	}
	return nil
}

func sameSignal(first, second Signal) bool {
	return first.ID == second.ID && first.TenantToken == second.TenantToken && first.Kind == second.Kind && first.Region == second.Region && first.Digest == second.Digest && first.ObservedAt.Equal(second.ObservedAt) && first.RetainUntil.Equal(second.RetainUntil) && first.Bytes == second.Bytes
}

func knownKind(kind Kind) bool {
	switch kind {
	case KindMetrics, KindLogs, KindTraces, KindDashboards:
		return true
	default:
		return false
	}
}

func expectedProtocol(kind Kind) string {
	switch kind {
	case KindMetrics:
		return "prometheus-remote-write"
	case KindLogs, KindTraces:
		return "otlp-http"
	case KindDashboards:
		return "grafana-http-api"
	default:
		return ""
	}
}

func expectedLicense(kind Kind) string {
	switch kind {
	case KindMetrics, KindTraces:
		return "Apache-2.0"
	case KindLogs, KindDashboards:
		return "AGPL-3.0"
	default:
		return ""
	}
}

func specCompatibilityIssues(spec Spec) []string {
	if !knownKind(spec.Kind) {
		return []string{"kind"}
	}
	issues := make([]string, 0, 2)
	if strings.TrimSpace(spec.Protocol) == "" || spec.Protocol != expectedProtocol(spec.Kind) {
		issues = append(issues, "protocol")
	}
	if strings.TrimSpace(spec.License) == "" || spec.License != expectedLicense(spec.Kind) {
		issues = append(issues, "license")
	}
	return issues
}

func (q Qualification) Explain() string {
	unsafe := append([]string(nil), q.Unsafe...)
	sort.Strings(unsafe)
	return fmt.Sprintf("telemetry stack complete=%t license=%t cost=%t restore=%t missing=%d unsafe=%d", q.Complete, q.LicenseOK, q.CostOK, q.RestoreOK, len(q.Missing), len(unsafe))
}
