package reliability

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	StatusHealthy  = "HEALTHY"
	StatusAtRisk   = "AT_RISK"
	StatusBreached = "BREACHED"
	StatusUnknown  = "UNKNOWN"

	DefaultManifestPath = "definitions/operations/pilot-reliability.yaml"
)

// Manifest is an immutable, versioned set of pilot reliability definitions.
type Manifest struct {
	Version     int            `yaml:"version"`
	Module      string         `yaml:"module"`
	EffectiveAt string         `yaml:"effective_at"`
	Owner       string         `yaml:"owner"`
	SLIs        []SLI          `yaml:"slis"`
	SLOs        []SLO          `yaml:"slos"`
	Actions     []BudgetAction `yaml:"error_budget_actions"`
}

// SLI fixes the query and measurement semantics; it does not define a
// customer promise. Availability is expressed as good/valid events.
type SLI struct {
	ID                 string  `yaml:"id"`
	Version            string  `yaml:"version"`
	Capability         string  `yaml:"capability"`
	Query              string  `yaml:"query"`
	Denominator        string  `yaml:"denominator"`
	Window             string  `yaml:"window"`
	StalenessBound     string  `yaml:"staleness_bound"`
	Owner              string  `yaml:"owner"`
	AvailabilityTarget float64 `yaml:"availability_target"`
	LatencyTargetMs    int64   `yaml:"latency_target_ms"`
}

// SLO is the versioned objective and its consequence policy.
type SLO struct {
	ID              string  `yaml:"id"`
	Version         string  `yaml:"version"`
	SLI             string  `yaml:"sli"`
	Target          float64 `yaml:"target"`
	LatencyTargetMs int64   `yaml:"latency_target_ms"`
	Window          string  `yaml:"window"`
	Owner           string  `yaml:"owner"`
	BreachAction    string  `yaml:"breach_action"`
	AtRiskAction    string  `yaml:"at_risk_action"`
	Contractual     bool    `yaml:"contractual"`
}

// BudgetAction names the bounded response at a burn threshold.
type BudgetAction struct {
	ID        string  `yaml:"id"`
	Version   string  `yaml:"version"`
	Threshold float64 `yaml:"threshold"` // fraction of budget consumed
	Action    string  `yaml:"action"`
	Owner     string  `yaml:"owner"`
}

// Measurement is a telemetry aggregate supplied by an observation system.
type Measurement struct {
	AsOf         time.Time
	WindowStart  time.Time
	WindowEnd    time.Time
	ObservedAt   time.Time
	Good         int64
	Valid        int64
	Total        int64
	LatencyP95Ms int64
}

type Diagnostic struct{ Entry, Field, State, Version, Reason string }

func (d Diagnostic) String() string {
	return fmt.Sprintf("entry=%s field=%s state=%s version=%s: %s", d.Entry, d.Field, d.State, d.Version, d.Reason)
}

type Readiness struct {
	Status      string
	Diagnostics []Diagnostic
}

func (r Readiness) Ready() bool { return len(r.Diagnostics) == 0 }

type Result struct {
	Capability, SLI, SLO, Status, Reason, BreachAction string
	Availability, ErrorBudgetRemaining                 float64
	LatencyP95Ms                                       int64
	Actions                                            []string
}

func Load(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reliability: reading %s: %w", path, err)
	}
	defer f.Close()
	var m Manifest
	section := ""
	var currentSLI *SLI
	var currentSLO *SLO
	var currentAction *BudgetAction
	flush := func() {
		if currentSLI != nil {
			m.SLIs = append(m.SLIs, *currentSLI)
			currentSLI = nil
		}
		if currentSLO != nil {
			m.SLOs = append(m.SLOs, *currentSLO)
			currentSLO = nil
		}
		if currentAction != nil {
			m.Actions = append(m.Actions, *currentAction)
			currentAction = nil
		}
	}
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			flush()
			line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("reliability: parsing %s line %d: expected key", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		value = scalar(strings.TrimSpace(value))
		if value == "" {
			section = key
			continue
		}
		if section == "" {
			switch key {
			case "version":
				m.Version, err = strconv.Atoi(value)
			case "module":
				m.Module = value
			case "effective_at":
				m.EffectiveAt = value
			case "owner":
				m.Owner = value
			default:
				err = fmt.Errorf("unknown manifest field %q", key)
			}
		} else {
			err = setManifestField(section, key, value, &currentSLI, &currentSLO, &currentAction)
		}
		if err != nil {
			return nil, fmt.Errorf("reliability: parsing %s line %d: %w", path, lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reliability: reading %s: %w", path, err)
	}
	flush()
	return &m, nil
}

// LoadDefault finds the checked-in pilot manifest from the current working
// directory or one of its parents, which supports both repository commands
// and package tests without depending on their current directory.
func LoadDefault() (*Manifest, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("reliability: locating working directory: %w", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, DefaultManifestPath)
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reliability: locating %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("reliability: locating %s from %s: file not found", DefaultManifestPath, wd)
		}
	}
}

func scalar(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func setManifestField(section, key, value string, sli **SLI, slo **SLO, action **BudgetAction) error {
	switch section {
	case "slis":
		if *sli == nil {
			*sli = &SLI{}
		}
		s := *sli
		switch key {
		case "id":
			s.ID = value
		case "version":
			s.Version = value
		case "capability":
			s.Capability = value
		case "query":
			s.Query = value
		case "denominator":
			s.Denominator = value
		case "window":
			s.Window = value
		case "staleness_bound":
			s.StalenessBound = value
		case "owner":
			s.Owner = value
		case "availability_target":
			return parseFloat(value, &s.AvailabilityTarget)
		case "latency_target_ms":
			return parseInt(value, &s.LatencyTargetMs)
		default:
			return fmt.Errorf("unknown SLI field %q", key)
		}
	case "slos":
		if *slo == nil {
			*slo = &SLO{}
		}
		s := *slo
		switch key {
		case "id":
			s.ID = value
		case "version":
			s.Version = value
		case "sli":
			s.SLI = value
		case "target":
			return parseFloat(value, &s.Target)
		case "latency_target_ms":
			return parseInt(value, &s.LatencyTargetMs)
		case "window":
			s.Window = value
		case "owner":
			s.Owner = value
		case "breach_action":
			s.BreachAction = value
		case "at_risk_action":
			s.AtRiskAction = value
		case "contractual":
			return parseBool(value, &s.Contractual)
		default:
			return fmt.Errorf("unknown SLO field %q", key)
		}
	case "error_budget_actions":
		if *action == nil {
			*action = &BudgetAction{}
		}
		a := *action
		switch key {
		case "id":
			a.ID = value
		case "version":
			a.Version = value
		case "threshold":
			return parseFloat(value, &a.Threshold)
		case "action":
			a.Action = value
		case "owner":
			a.Owner = value
		default:
			return fmt.Errorf("unknown error budget action field %q", key)
		}
	default:
		return fmt.Errorf("unknown manifest section %q", section)
	}
	return nil
}

func parseFloat(value string, target *float64) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err == nil {
		*target = parsed
	}
	return err
}

func parseInt(value string, target *int64) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		*target = parsed
	}
	return err
}

func parseBool(value string, target *bool) error {
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		*target = parsed
	}
	return err
}

// Validate is deterministic: diagnostics are sorted by entry, field, state,
// and version, making release reports stable across process runs.
func Validate(m *Manifest, now time.Time) Readiness {
	if m == nil {
		return Readiness{Status: StatusUnknown, Diagnostics: []Diagnostic{{Field: "manifest", State: "MISSING", Reason: "manifest is required"}}}
	}
	var ds []Diagnostic
	add := func(e, f, s, v, r string) { ds = append(ds, Diagnostic{e, f, s, v, r}) }
	if m.Version != 1 {
		add("manifest", "version", "UNSUPPORTED", fmt.Sprint(m.Version), "manifest version must be 1")
	}
	for f, v := range map[string]string{"module": m.Module, "effective_at": m.EffectiveAt, "owner": m.Owner} {
		if strings.TrimSpace(v) == "" {
			add("manifest", f, "MISSING", "", f+" is required")
		}
	}
	if m.EffectiveAt != "" {
		if effectiveAt, err := time.Parse(time.RFC3339, m.EffectiveAt); err != nil {
			add("manifest", "effective_at", "INVALID", "", "effective_at must be RFC3339")
		} else if effectiveAt.After(now) {
			add("manifest", "effective_at", "FUTURE", "", "manifest is not effective yet")
		}
	}
	if len(m.SLIs) == 0 {
		add("manifest", "slis", "MISSING", "", "at least one SLI is required")
	}
	if len(m.SLOs) == 0 {
		add("manifest", "slos", "MISSING", "", "at least one SLO is required")
	}
	slis := map[string]SLI{}
	for _, s := range m.SLIs {
		if strings.TrimSpace(s.ID) == "" {
			add("", "id", "MISSING", s.Version, "id is required")
		}
		if _, ok := slis[s.ID]; ok {
			add(s.ID, "id", "DUPLICATE", s.Version, "SLI id is duplicated")
		}
		slis[s.ID] = s
		validateSLI(s, add, now)
	}
	seenSLOs := map[string]bool{}
	for _, s := range m.SLOs {
		if strings.TrimSpace(s.ID) == "" {
			add("", "id", "MISSING", s.Version, "id is required")
		}
		if seenSLOs[s.ID] {
			add(s.ID, "id", "DUPLICATE", s.Version, "SLO id is duplicated")
		}
		seenSLOs[s.ID] = true
		if _, ok := slis[s.SLI]; !ok {
			add(s.ID, "sli", "UNKNOWN", s.Version, "SLO references an unknown SLI")
		} else if sli := slis[s.SLI]; sli.Window != s.Window {
			add(s.ID, "window", "MISMATCH", s.Version, "SLO window must match its SLI measurement window")
		}
		validateSLO(s, add)
	}
	seenActions := map[string]bool{}
	for _, action := range m.Actions {
		if action.ID == "" || action.Version == "" || action.Owner == "" || strings.TrimSpace(action.Action) == "" {
			add(action.ID, "error_budget_action", "MISSING", action.Version, "id, version, action, and owner are required")
		}
		if seenActions[action.ID] {
			add(action.ID, "error_budget_action", "DUPLICATE", action.Version, "error budget action id is duplicated")
		}
		seenActions[action.ID] = true
		if action.Threshold <= 0 || action.Threshold > 1 {
			add(action.ID, "threshold", "INVALID", action.Version, "threshold must be > 0 and <= 1")
		}
	}
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		for _, p := range [][2]string{{a.Entry, b.Entry}, {a.Field, b.Field}, {a.State, b.State}, {a.Version, b.Version}, {a.Reason, b.Reason}} {
			if p[0] != p[1] {
				return p[0] < p[1]
			}
		}
		return false
	})
	if len(ds) > 0 {
		return Readiness{Status: StatusUnknown, Diagnostics: ds}
	}
	return Readiness{Status: StatusHealthy}
}

func validateSLI(s SLI, add func(string, string, string, string, string), now time.Time) {
	for f, v := range map[string]string{"version": s.Version, "capability": s.Capability, "query": s.Query, "denominator": s.Denominator, "window": s.Window, "staleness_bound": s.StalenessBound, "owner": s.Owner} {
		if strings.TrimSpace(v) == "" {
			add(s.ID, f, "MISSING", s.Version, f+" is required")
		}
	}
	if s.AvailabilityTarget <= 0 || s.AvailabilityTarget > 1 {
		add(s.ID, "availability_target", "INVALID", s.Version, "availability target must be > 0 and <= 1")
	}
	if s.LatencyTargetMs <= 0 {
		add(s.ID, "latency_target_ms", "INVALID", s.Version, "latency target must be positive")
	}
	if s.StalenessBound != "" {
		if d, e := time.ParseDuration(s.StalenessBound); e != nil || d <= 0 {
			add(s.ID, "staleness_bound", "INVALID", s.Version, "staleness bound must be a positive duration")
		}
	}
	if s.Window != "" {
		if d, e := time.ParseDuration(s.Window); e != nil || d <= 0 {
			add(s.ID, "window", "INVALID", s.Version, "window must be a positive duration")
		}
	}
	_ = now
}
func validateSLO(s SLO, add func(string, string, string, string, string)) {
	for f, v := range map[string]string{"version": s.Version, "sli": s.SLI, "window": s.Window, "owner": s.Owner, "breach_action": s.BreachAction, "at_risk_action": s.AtRiskAction} {
		if strings.TrimSpace(v) == "" {
			add(s.ID, f, "MISSING", s.Version, f+" is required")
		}
	}
	if s.Target <= 0 || s.Target > 1 {
		add(s.ID, "target", "INVALID", s.Version, "SLO target must be > 0 and <= 1")
	}
	if s.LatencyTargetMs <= 0 {
		add(s.ID, "latency_target_ms", "INVALID", s.Version, "latency target must be positive")
	}
	if s.Window != "" {
		if d, e := time.ParseDuration(s.Window); e != nil || d <= 0 {
			add(s.ID, "window", "INVALID", s.Version, "window must be a positive duration")
		}
	}
}

// Evaluate never treats missing, empty, invalid, or stale telemetry as good.
func Evaluate(m Manifest, measurements map[string]Measurement, now time.Time) []Result {
	slis := map[string]SLI{}
	for _, s := range m.SLIs {
		slis[s.ID] = s
	}
	out := make([]Result, 0, len(m.SLOs))
	for _, slo := range m.SLOs {
		sli := slis[slo.SLI]
		r := Result{Capability: sli.Capability, SLI: sli.ID, SLO: slo.ID, BreachAction: slo.BreachAction, Status: StatusUnknown}
		x, ok := measurements[sli.ID]
		if !ok || x.Total <= 0 || x.Valid <= 0 || x.Good < 0 || x.Good > x.Valid || x.Valid > x.Total {
			r.Reason = "measurement denominator or validity is unavailable"
			out = append(out, r)
			continue
		}
		if x.WindowStart.IsZero() || x.WindowEnd.IsZero() || !x.WindowEnd.After(x.WindowStart) {
			r.Reason = "measurement window is unavailable"
			out = append(out, r)
			continue
		}
		window, windowErr := time.ParseDuration(slo.Window)
		if windowErr != nil || x.WindowEnd.Sub(x.WindowStart) != window {
			r.Reason = "measurement window does not match the SLO version"
			out = append(out, r)
			continue
		}
		if x.WindowEnd.After(now) || x.ObservedAt.After(now) {
			r.Reason = "measurement is from the future or still open"
			out = append(out, r)
			continue
		}
		stale := false
		if d, e := time.ParseDuration(sli.StalenessBound); e != nil || now.Sub(x.ObservedAt) > d {
			stale = true
		}
		if stale {
			r.Reason = "telemetry exceeds declared staleness bound"
			out = append(out, r)
			continue
		}
		r.Availability = float64(x.Good) / float64(x.Valid)
		r.LatencyP95Ms = x.LatencyP95Ms
		if slo.Target == 1 {
			if r.Availability == 1 {
				r.ErrorBudgetRemaining = 1
			}
		} else {
			r.ErrorBudgetRemaining = (r.Availability - slo.Target) / (1 - slo.Target)
		}
		if r.ErrorBudgetRemaining < 0 {
			r.ErrorBudgetRemaining = 0
		} else if r.ErrorBudgetRemaining > 1 {
			r.ErrorBudgetRemaining = 1
		}
		addAction := func(action string) {
			action = strings.TrimSpace(action)
			if action == "" {
				return
			}
			for _, existing := range r.Actions {
				if existing == action {
					return
				}
			}
			r.Actions = append(r.Actions, action)
		}
		if r.Availability < slo.Target || x.LatencyP95Ms > slo.LatencyTargetMs {
			r.Status = StatusBreached
			r.Reason = "availability or latency target breached"
			addAction(slo.BreachAction)
		} else if r.Availability < slo.Target+(1-slo.Target)*0.5 {
			r.Status = StatusAtRisk
			r.Reason = "more than half of error budget is consumed"
			addAction(slo.AtRiskAction)
		} else {
			r.Status = StatusHealthy
			r.Reason = "measurement satisfies objective"
		}
		consumed := 1 - r.ErrorBudgetRemaining
		for _, action := range m.Actions {
			if action.Threshold <= consumed && action.ID != "" && strings.TrimSpace(action.Action) != "" {
				addAction(action.Action)
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SLO < out[j].SLO })
	return out
}
