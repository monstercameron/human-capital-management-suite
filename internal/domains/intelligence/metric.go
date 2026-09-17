package intelligence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrMetricDefinition rejects a metric definition with an implicit
	// denominator, population, time, null, zero or rounding policy.
	ErrMetricDefinition = errors.New("intelligence: invalid metric definition")
	// ErrMetricExecution rejects execution against the wrong definition.
	ErrMetricExecution = errors.New("intelligence: invalid metric execution")
)

// MetricKind is the closed METRIC-001 formula vocabulary.
type MetricKind string

const (
	MetricCount MetricKind = "COUNT"
	MetricSum   MetricKind = "SUM"
	MetricMean  MetricKind = "MEAN"
	MetricRatio MetricKind = "RATIO"
)

func (k MetricKind) Valid() bool {
	switch k {
	case MetricCount, MetricSum, MetricMean, MetricRatio:
		return true
	default:
		return false
	}
}

// NullPolicy states what skipped nulls do to quality. SKIP degrades to
// PARTIAL; UNKNOWN_IF_ANY degrades the whole result to UNKNOWN.
type NullPolicy string

const (
	NullSkip         NullPolicy = "SKIP"
	NullUnknownIfAny NullPolicy = "UNKNOWN_IF_ANY"
)

func (p NullPolicy) Valid() bool { return p == NullSkip || p == NullUnknownIfAny }

// MetricQuality is the closed result-quality vocabulary. Missing data is
// UNKNOWN, never a silent zero.
type MetricQuality string

const (
	QualityOK      MetricQuality = "OK"
	QualityPartial MetricQuality = "PARTIAL"
	QualityUnknown MetricQuality = "UNKNOWN"
)

// MetricDefinition is one immutable, effective-dated semantic metric. Every
// policy the RED contract names is an explicit field: formula, null
// handling, rounding, scale and effective window.
type MetricDefinition struct {
	ID            string
	Version       string
	Kind          MetricKind
	NullPolicy    NullPolicy
	Rounding      values.RoundingMode
	Scale         int32
	Unit          string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
}

func (d MetricDefinition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" {
		return fmt.Errorf("%w: metric identity and version are required", ErrMetricDefinition)
	}
	if !d.Kind.Valid() {
		return fmt.Errorf("%w: formula kind is not declared", ErrMetricDefinition)
	}
	if !d.NullPolicy.Valid() {
		return fmt.Errorf("%w: null policy is not declared", ErrMetricDefinition)
	}
	if d.Rounding == values.RoundingUnspecified {
		return fmt.Errorf("%w: rounding mode is required", ErrMetricDefinition)
	}
	if d.Scale < 0 || d.Scale > 12 {
		return fmt.Errorf("%w: scale must be 0..12", ErrMetricDefinition)
	}
	if d.EffectiveFrom.IsZero() || d.EffectiveTo.IsZero() || !d.EffectiveFrom.Before(d.EffectiveTo) {
		return fmt.Errorf("%w: a valid effective window is required", ErrMetricDefinition)
	}
	return nil
}

// MetricRow is one population member. Included rows outside the population
// are ignored; included rows without set components are nulls.
type MetricRow struct {
	Included       bool
	Numerator      values.Decimal
	Denominator    values.Decimal
	NumeratorSet   bool
	DenominatorSet bool
	Watermark      string
}

// MetricResult is the typed METRIC-001 answer.
type MetricResult struct {
	Numerator         values.Decimal
	Denominator       values.Decimal
	Value             values.Decimal
	SampleSize        int
	NullCount         int
	Quality           MetricQuality
	QualityReason     string
	SourceWatermarks  []string
	PopulationDigest  string
	CalculationDigest string
}

// CompiledMetric is a validated, immutable definition ready to execute.
type CompiledMetric struct {
	definition MetricDefinition
}

// CompileMetric freezes one metric definition. Compilation is where implicit
// policies die: anything undeclared is refused before any data is read.
func CompileMetric(definition MetricDefinition) (CompiledMetric, error) {
	if err := definition.Validate(); err != nil {
		return CompiledMetric{}, err
	}
	return CompiledMetric{definition: definition}, nil
}

// Definition returns the frozen definition.
func (c CompiledMetric) Definition() MetricDefinition { return c.definition }

func zeroAt(scale int32, mode values.RoundingMode) values.Decimal {
	return values.MustDecimal("0", scale, mode)
}

// Execute runs the frozen formula over one population snapshot. It is pure
// and replayable: the same rows always produce the same digests.
func (c CompiledMetric) Execute(rows []MetricRow) (MetricResult, error) {
	def := c.definition
	decScale := def.Scale
	if decScale < 4 {
		decScale = 4
	}
	zero := zeroAt(decScale, def.Rounding)
	numerator, denominator := zero, zero
	sample, nulls := 0, 0
	watermarks := map[string]bool{}
	var populationParts []string
	for i, row := range rows {
		if !row.Included {
			continue
		}
		populationParts = append(populationParts, fmt.Sprintf("%d:%s:%s:%v:%v", i, row.Numerator.String(), row.Denominator.String(), row.NumeratorSet, row.DenominatorSet))
		if row.Watermark != "" {
			watermarks[row.Watermark] = true
		}
		complete := true
		switch def.Kind {
		case MetricCount:
			// Presence counts; no components are needed.
		case MetricSum, MetricMean:
			complete = row.NumeratorSet
		case MetricRatio:
			complete = row.NumeratorSet && row.DenominatorSet
		}
		if !complete {
			nulls++
			continue
		}
		var err error
		sample++
		switch def.Kind {
		case MetricCount:
			numerator, err = numerator.Add(values.MustDecimal("1", decScale, def.Rounding))
		case MetricSum, MetricMean:
			numerator, err = numerator.Add(rescale(row.Numerator, decScale, def.Rounding))
		case MetricRatio:
			numerator, err = numerator.Add(rescale(row.Numerator, decScale, def.Rounding))
		}
		if err != nil {
			return MetricResult{}, fmt.Errorf("%w: %v", ErrMetricExecution, err)
		}
		if def.Kind == MetricRatio {
			denominator, err = denominator.Add(rescale(row.Denominator, decScale, def.Rounding))
			if err != nil {
				return MetricResult{}, fmt.Errorf("%w: %v", ErrMetricExecution, err)
			}
		}
	}
	marks := make([]string, 0, len(watermarks))
	for mark := range watermarks {
		marks = append(marks, mark)
	}
	sort.Strings(marks)
	populationSum := sha256.Sum256([]byte(strings.Join(populationParts, "\x00")))
	populationDigest := "sha256:" + hex.EncodeToString(populationSum[:])
	unknown := func(reason string) (MetricResult, error) {
		return MetricResult{
			Numerator: zero, Denominator: zero, Value: zero,
			SampleSize: sample, NullCount: nulls, Quality: QualityUnknown,
			QualityReason: reason, SourceWatermarks: marks,
			PopulationDigest:  populationDigest,
			CalculationDigest: calculationDigest(def, populationDigest, zero, "UNKNOWN"),
		}, nil
	}
	if sample == 0 {
		return unknown("no usable population rows; missing data is UNKNOWN, never zero")
	}
	if nulls > 0 && def.NullPolicy == NullUnknownIfAny {
		return unknown("null members under UNKNOWN_IF_ANY degrade the result to UNKNOWN")
	}
	value := zero
	switch def.Kind {
	case MetricCount, MetricSum:
		value = numerator
	case MetricMean:
		count := values.MustDecimal(fmt.Sprintf("%d", sample), decScale, def.Rounding)
		mean, err := numerator.Div(count, decScale, def.Rounding)
		if err != nil {
			return MetricResult{}, fmt.Errorf("%w: %v", ErrMetricExecution, err)
		}
		value = mean
	case MetricRatio:
		if denominator.IsZero() {
			return unknown("zero denominator; the ratio is UNKNOWN, never an error or a zero")
		}
		ratio, err := numerator.Div(denominator, decScale, def.Rounding)
		if err != nil {
			return MetricResult{}, fmt.Errorf("%w: %v", ErrMetricExecution, err)
		}
		value = ratio
	}
	final, err := value.Quantize(def.Scale, def.Rounding)
	if err != nil {
		return MetricResult{}, fmt.Errorf("%w: %v", ErrMetricExecution, err)
	}
	quality := QualityOK
	reason := "complete population"
	if nulls > 0 {
		quality = QualityPartial
		reason = fmt.Sprintf("%d null members skipped under SKIP", nulls)
	}
	return MetricResult{
		Numerator: numerator, Denominator: denominator, Value: final,
		SampleSize: sample, NullCount: nulls, Quality: quality, QualityReason: reason,
		SourceWatermarks: marks, PopulationDigest: populationDigest,
		CalculationDigest: calculationDigest(def, populationDigest, final, string(quality)),
	}, nil
}

// rescale converts an input decimal to the working scale exactly when it is
// already exact, rounding only through the declared policy otherwise.
func rescale(d values.Decimal, scale int32, mode values.RoundingMode) values.Decimal {
	converted, err := d.Quantize(scale, mode)
	if err != nil {
		return d
	}
	return converted
}

func calculationDigest(def MetricDefinition, population string, value values.Decimal, quality string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		def.ID, def.Version, string(def.Kind), string(def.NullPolicy),
		value.String(), quality, population,
		def.EffectiveFrom.UTC().Format(time.RFC3339Nano), def.EffectiveTo.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
