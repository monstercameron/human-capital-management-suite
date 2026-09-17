package intelligence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrMetricCorrection refuses a metric correction without a known
	// original, a complete governed record, or a forward-only supersession.
	// It never overwrites the original value.
	ErrMetricCorrection = errors.New("intelligence: invalid metric correction")
)

// DependentKind is the closed METRIC-002 consumer vocabulary.
type DependentKind string

const (
	DependentReport    DependentKind = "REPORT"
	DependentDashboard DependentKind = "DASHBOARD"
	DependentDecision  DependentKind = "DECISION"
)

func (k DependentKind) Valid() bool {
	switch k {
	case DependentReport, DependentDashboard, DependentDecision:
		return true
	default:
		return false
	}
}

// DependentStatus is the closed freshness vocabulary. A dependent on a
// superseded value is STALE until it is relinked to the correction.
type DependentStatus string

const (
	DependentFresh   DependentStatus = "FRESH"
	DependentStale   DependentStatus = "STALE"
	DependentUnknown DependentStatus = "UNKNOWN"
)

// MetricValue is one immutable revision of a metric result. Corrections
// append new revisions; no method mutates a published value.
type MetricValue struct {
	MetricID         string
	Version          string
	Value            values.Decimal
	Quality          MetricQuality
	WindowStart      time.Time
	WindowEnd        time.Time
	PopulationDigest string
	Authority        string
	Reason           string
	Revision         uint64
	Supersedes       string
	Digest           string
}

// CorrectionRequest appends one governed correction. ObservedAt is an
// injected reference time: this contract never reads a clock.
type CorrectionRequest struct {
	IdempotencyKey   string
	OriginalDigest   string
	NewValue         values.Decimal
	Quality          MetricQuality
	WindowStart      time.Time
	WindowEnd        time.Time
	PopulationDigest string
	Authority        string
	Reason           string
	ObservedAt       time.Time
}

type metricDependent struct {
	kind   DependentKind
	bound  string
	status DependentStatus
}

// CorrectionLog is the METRIC-002 append-only history: published values,
// their corrections, and the freshness of every linked dependent. It is pure
// memory with a mutex: no database, no clock, no network.
type CorrectionLog struct {
	mu         sync.Mutex
	values     map[string]MetricValue
	successors map[string]string
	replays    map[string]string
	dependents map[string]*metricDependent
}

// NewCorrectionLog builds an empty correction history.
func NewCorrectionLog() *CorrectionLog {
	return &CorrectionLog{
		values:     map[string]MetricValue{},
		successors: map[string]string{},
		replays:    map[string]string{},
		dependents: map[string]*metricDependent{},
	}
}

func correctionRefusal(reason string) error {
	return fmt.Errorf("%w: %s", ErrMetricCorrection, reason)
}

func metricValueDigest(value MetricValue) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		value.MetricID, value.Version, value.Value.String(), string(value.Quality),
		value.WindowStart.UTC().Format(time.RFC3339Nano), value.WindowEnd.UTC().Format(time.RFC3339Nano),
		value.PopulationDigest, value.Authority, value.Reason,
		fmt.Sprintf("%d", value.Revision), value.Supersedes,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (v MetricValue) validateRecord() error {
	if strings.TrimSpace(v.MetricID) == "" || strings.TrimSpace(v.Version) == "" {
		return correctionRefusal("metric identity and version are required")
	}
	if err := v.Value.Validate(); err != nil {
		return correctionRefusal(fmt.Sprintf("metric value is required: %v", err))
	}
	if v.Quality != QualityOK && v.Quality != QualityPartial && v.Quality != QualityUnknown {
		return correctionRefusal("metric quality is not declared")
	}
	if v.WindowStart.IsZero() || v.WindowEnd.IsZero() || !v.WindowStart.Before(v.WindowEnd) {
		return correctionRefusal("a valid metric window is required")
	}
	if strings.TrimSpace(v.PopulationDigest) == "" {
		return correctionRefusal("population digest is required")
	}
	if strings.TrimSpace(v.Authority) == "" {
		return correctionRefusal("correction authority is required")
	}
	if strings.TrimSpace(v.Reason) == "" {
		return correctionRefusal("correction reason is required")
	}
	return nil
}

// Publish records the first revision of a metric result. The stored revision
// is always 1 with no predecessor: history starts here and only appends.
func (l *CorrectionLog) Publish(value MetricValue) (MetricValue, error) {
	if err := value.validateRecord(); err != nil {
		return MetricValue{}, err
	}
	value.Revision = 1
	value.Supersedes = ""
	value.Digest = metricValueDigest(value)
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.values[value.Digest]; ok {
		return existing, nil
	}
	l.values[value.Digest] = value
	return value, nil
}

// LinkDependent binds one report, dashboard or decision link to the metric
// revision it was built on. The link starts FRESH.
func (l *CorrectionLog) LinkDependent(id string, kind DependentKind, metricDigest string) error {
	if strings.TrimSpace(id) == "" {
		return correctionRefusal("dependent identity is required")
	}
	if !kind.Valid() {
		return correctionRefusal(fmt.Sprintf("dependent kind %q is not declared", kind))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.values[metricDigest]; !ok {
		return correctionRefusal("dependent must bind a published metric revision")
	}
	l.dependents[id] = &metricDependent{kind: kind, bound: metricDigest, status: DependentFresh}
	return nil
}

// DependentStatus reads one dependent's freshness. Unknown ids read UNKNOWN,
// never a silent FRESH.
func (l *CorrectionLog) DependentStatus(id string) DependentStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	dependent, ok := l.dependents[id]
	if !ok {
		return DependentUnknown
	}
	return dependent.status
}

// Get returns one published revision by digest. The returned value is a
// copy: callers cannot mutate history through it.
func (l *CorrectionLog) Get(digest string) (MetricValue, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.values[digest]
	return value, ok
}

// SupersessionChain walks the history from the oldest ancestor to the named
// revision. It is empty for an unknown digest.
func (l *CorrectionLog) SupersessionChain(digest string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.values[digest]
	if !ok {
		return nil
	}
	chain := []string{current.Digest}
	for current.Supersedes != "" {
		parent, ok := l.values[current.Supersedes]
		if !ok {
			break
		}
		chain = append([]string{parent.Digest}, chain...)
		current = parent
	}
	return chain
}

// AppendCorrection appends one governed correction: the original is preserved
// untouched, the new revision chains it, every dependent on a superseded
// revision goes stale, and the same idempotency key replays the same
// revision instead of appending a duplicate.
func (l *CorrectionLog) AppendCorrection(req CorrectionRequest) (MetricValue, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return MetricValue{}, correctionRefusal("idempotency key is required")
	}
	if strings.TrimSpace(req.OriginalDigest) == "" {
		return MetricValue{}, correctionRefusal("original digest is required")
	}
	if req.ObservedAt.IsZero() {
		return MetricValue{}, correctionRefusal("reference time is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if digest, ok := l.replays[req.IdempotencyKey]; ok {
		return l.values[digest], nil
	}
	original, ok := l.values[req.OriginalDigest]
	if !ok {
		return MetricValue{}, correctionRefusal("correction must name a published original")
	}
	if _, superseded := l.successors[req.OriginalDigest]; superseded {
		return MetricValue{}, correctionRefusal("superseded values cannot be corrected again: history appends forward only")
	}
	corrected := MetricValue{
		MetricID: original.MetricID, Version: original.Version,
		Value: req.NewValue, Quality: req.Quality,
		WindowStart: req.WindowStart, WindowEnd: req.WindowEnd,
		PopulationDigest: req.PopulationDigest,
		Authority:        req.Authority, Reason: req.Reason,
		Revision:   original.Revision + 1,
		Supersedes: original.Digest,
	}
	if err := corrected.validateRecord(); err != nil {
		return MetricValue{}, err
	}
	corrected.Digest = metricValueDigest(corrected)
	if existing, ok := l.values[corrected.Digest]; ok {
		l.replays[req.IdempotencyKey] = existing.Digest
		return existing, nil
	}
	l.values[corrected.Digest] = corrected
	l.successors[original.Digest] = corrected.Digest
	l.replays[req.IdempotencyKey] = corrected.Digest
	superseded := map[string]bool{}
	for digest := corrected.Digest; digest != ""; {
		superseded[digest] = true
		value := l.values[digest]
		digest = value.Supersedes
	}
	for _, dependent := range l.dependents {
		if superseded[dependent.bound] && dependent.bound != corrected.Digest {
			dependent.status = DependentStale
		}
	}
	return corrected, nil
}
