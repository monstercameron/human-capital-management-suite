package otel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// ExporterKind selects which exporter backend a signal's Config builds when
// no explicit test override (TraceConfig.Exporter, MetricConfig.Reader) is
// supplied.
type ExporterKind string

// Published exporter kinds. The zero value behaves as ExporterKindStdout so
// a Config left otherwise unset is still runnable locally without an
// explicit choice.
const (
	ExporterKindStdout ExporterKind = "stdout"
	ExporterKindOTLP   ExporterKind = "otlp"
)

// Bounded defaults applied whenever a Config leaves the corresponding field
// at its zero value. These exist so "leave it unset" means "a sane bounded
// default", never "unbounded" (OBS-002 RED: "uses unbounded batching/
// retry").
const (
	defaultMaxQueueSize    = 2048
	defaultBatchTimeout    = 5 * time.Second
	defaultExportTimeout   = 10 * time.Second
	defaultMetricInterval  = 30 * time.Second
	meterAndTracerNameSelf = "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// TraceConfig configures the trace signal's exporter and the bounded batch
// processor in front of it.
type TraceConfig struct {
	// Kind selects the exporter built when Exporter is nil.
	Kind ExporterKind
	// Endpoint, Insecure and Headers configure the OTLP/HTTP exporter when
	// Kind is ExporterKindOTLP.
	Endpoint string
	Insecure bool
	Headers  map[string]string
	// StdoutWriter overrides os.Stdout when Kind is ExporterKindStdout.
	StdoutWriter io.Writer
	// Exporter, when set, is used verbatim instead of building one from
	// Kind — the test-only path (e.g. go.opentelemetry.io/otel/sdk/trace/
	// tracetest.NewInMemoryExporter()).
	Exporter sdktrace.SpanExporter

	// MaxQueueSize, BatchTimeout and ExportTimeout bound the batch span
	// processor in front of the exporter. Zero takes the package default;
	// negative is rejected by NewProvider as an attempt at unbounded
	// batching/retry.
	MaxQueueSize  int
	BatchTimeout  time.Duration
	ExportTimeout time.Duration
}

// MetricConfig configures the metric signal's exporter/reader.
type MetricConfig struct {
	// Kind selects the exporter built when Reader is nil.
	Kind ExporterKind
	// Endpoint, Insecure and Headers configure the OTLP/HTTP exporter when
	// Kind is ExporterKindOTLP.
	Endpoint string
	Insecure bool
	Headers  map[string]string
	// StdoutWriter overrides os.Stdout when Kind is ExporterKindStdout.
	StdoutWriter io.Writer
	// Reader, when set, is used verbatim instead of building one from Kind
	// — the test-only path (e.g. sdkmetric.NewManualReader()). Interval is
	// ignored when Reader is set.
	Reader sdkmetric.Reader

	// Interval bounds the periodic reader wrapping a stdout/OTLP exporter.
	// Zero takes the package default; negative is rejected by NewProvider.
	Interval time.Duration
}

// Config builds one Provider. Resource and Evaluator are required: a
// Provider with no Evaluator would have no policy to enforce, which this
// package refuses to default to "allow everything" (OBS-004's own
// fail-closed rule, applied here at construction rather than only per
// attribute).
type Config struct {
	Resource        telemetry.Resource
	Evaluator       *telemetry.Evaluator
	Trace           TraceConfig
	Metric          MetricConfig
	ShutdownTimeout time.Duration
}

// Provider owns one process's tracer and meter providers, both wired
// through the frozen telemetry policy, plus the typed Metrics recorder for
// the P1A cell's catalog instruments.
type Provider struct {
	res     *sdkresource.Resource
	eval    *telemetry.Evaluator
	tp      *sdktrace.TracerProvider
	mp      *sdkmetric.MeterProvider
	tracer  trace.TracerProvider // filtering wrapper around tp
	metrics *Metrics

	contracts RuntimeContracts

	shutdownTimeout time.Duration
}

// NewProvider validates cfg and builds a Provider. Every failure mode is a
// plain error returned before any SDK object is constructed: there is no
// partially-built Provider a caller could accidentally use.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if err := cfg.Resource.Validate(); err != nil {
		return nil, err
	}
	if cfg.Evaluator == nil {
		return nil, errors.New("otel: Config.Evaluator is required (a Provider must not default to an unpoliced attribute pipeline)")
	}
	if cfg.ShutdownTimeout <= 0 {
		return nil, errors.New("otel: Config.ShutdownTimeout must be positive (a bounded flush deadline is required)")
	}
	if cfg.Trace.MaxQueueSize < 0 {
		return nil, errors.New("otel: Config.Trace.MaxQueueSize must not be negative (unbounded batching is prohibited)")
	}
	if cfg.Trace.BatchTimeout < 0 {
		return nil, errors.New("otel: Config.Trace.BatchTimeout must not be negative")
	}
	if cfg.Trace.ExportTimeout < 0 {
		return nil, errors.New("otel: Config.Trace.ExportTimeout must not be negative")
	}
	if cfg.Metric.Interval < 0 {
		return nil, errors.New("otel: Config.Metric.Interval must not be negative")
	}
	contracts, err := newServingContracts()
	if err != nil {
		return nil, fmt.Errorf("otel: compose runtime contracts: %w", err)
	}

	res, err := buildResource(cfg.Resource)
	if err != nil {
		return nil, err
	}

	tp, err := buildTracerProvider(ctx, res, cfg.Trace)
	if err != nil {
		return nil, err
	}

	mp, meter, err := buildMeterProvider(ctx, res, cfg.Metric)
	if err != nil {
		return nil, err
	}

	metrics, err := newMetrics(meter, cfg.Evaluator)
	if err != nil {
		return nil, err
	}

	return &Provider{
		res:             res,
		eval:            cfg.Evaluator,
		tp:              tp,
		mp:              mp,
		tracer:          newTracerProvider(tp, cfg.Evaluator),
		metrics:         metrics,
		contracts:       contracts,
		shutdownTimeout: cfg.ShutdownTimeout,
	}, nil
}

func buildTracerProvider(ctx context.Context, res *sdkresource.Resource, cfg TraceConfig) (*sdktrace.TracerProvider, error) {
	exporter := cfg.Exporter
	if exporter == nil {
		built, err := buildTraceExporter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		exporter = built
	}

	maxQueue := cfg.MaxQueueSize
	if maxQueue == 0 {
		maxQueue = defaultMaxQueueSize
	}
	batchTimeout := cfg.BatchTimeout
	if batchTimeout == 0 {
		batchTimeout = defaultBatchTimeout
	}
	exportTimeout := cfg.ExportTimeout
	if exportTimeout == 0 {
		exportTimeout = defaultExportTimeout
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithMaxQueueSize(maxQueue),
		sdktrace.WithBatchTimeout(batchTimeout),
		sdktrace.WithExportTimeout(exportTimeout),
	)

	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	), nil
}

func buildTraceExporter(ctx context.Context, cfg TraceConfig) (sdktrace.SpanExporter, error) {
	switch cfg.Kind {
	case ExporterKindOTLP:
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("otel: building OTLP trace exporter: %w", err)
		}
		return exp, nil
	case ExporterKindStdout, "":
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stdout
		}
		exp, err := stdouttrace.New(stdouttrace.WithWriter(w))
		if err != nil {
			return nil, fmt.Errorf("otel: building stdout trace exporter: %w", err)
		}
		return exp, nil
	default:
		return nil, fmt.Errorf("otel: unknown trace exporter kind %q", cfg.Kind)
	}
}

func buildMeterProvider(ctx context.Context, res *sdkresource.Resource, cfg MetricConfig) (*sdkmetric.MeterProvider, metric.Meter, error) {
	reader := cfg.Reader
	if reader == nil {
		built, err := buildMetricReader(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		reader = built
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	return mp, mp.Meter(meterAndTracerNameSelf), nil
}

func buildMetricReader(ctx context.Context, cfg MetricConfig) (sdkmetric.Reader, error) {
	interval := cfg.Interval
	if interval == 0 {
		interval = defaultMetricInterval
	}

	switch cfg.Kind {
	case ExporterKindOTLP:
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
		}
		exp, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("otel: building OTLP metric exporter: %w", err)
		}
		return sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)), nil
	case ExporterKindStdout, "":
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stdout
		}
		return sdkmetric.NewPeriodicReader(newStdoutMetricExporter(w), sdkmetric.WithInterval(interval)), nil
	default:
		return nil, fmt.Errorf("otel: unknown metric exporter kind %q", cfg.Kind)
	}
}

// Tracer returns a trace.Tracer that filters every span/event attribute
// through this Provider's Evaluator before it reaches the real SDK span
// (see trace.go). name should uniquely identify the instrumented library or
// component, per trace.TracerProvider's own documented convention.
func (p *Provider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return p.tracer.Tracer(name, opts...)
}

// Metrics returns the P1A cell's typed metric recorder.
func (p *Provider) Metrics() *Metrics { return p.metrics }

// Resource returns the OTel Resource this Provider's signals carry.
func (p *Provider) Resource() *sdkresource.Resource { return p.res }

// TracerProvider returns the underlying, unwrapped SDK tracer provider —
// for shutdown/flush composition and tests that need direct SDK access
// (e.g. asserting exported spans through an injected in-memory exporter).
// Instrumentation code should use Tracer, not this, so attribute policy is
// never bypassed.
func (p *Provider) TracerProvider() *sdktrace.TracerProvider { return p.tp }

// MeterProvider returns the underlying SDK meter provider, for the same
// reason TracerProvider does. Instrumentation code should use Metrics, not
// this.
func (p *Provider) MeterProvider() *sdkmetric.MeterProvider { return p.mp }

// ShutdownReport is Shutdown's independent account of whether the flush
// completed cleanly within the configured deadline (OBS-002 GREEN: "expose
// drops/degraded export independently"). It never affects the caller's own
// business result — Shutdown returns it as a value, not folded into an
// error the caller must handle to keep the process running.
type ShutdownReport struct {
	// TraceErr and MetricErr are whatever the underlying SDK providers
	// returned from Shutdown, if anything.
	TraceErr  error
	MetricErr error
	// DeadlineExceeded is true when the bounded shutdown deadline
	// (Config.ShutdownTimeout) elapsed before both providers finished:
	// remaining buffered spans/metrics were dropped rather than exported,
	// exactly as structured-logging-and-opentelemetry.md's "Export, failure
	// and shutdown" describes ("Crashes cannot promise lossless
	// telemetry").
	DeadlineExceeded bool
}

// Err joins TraceErr and MetricErr for a caller that just wants a single
// error to log, without inspecting the individual fields.
func (r ShutdownReport) Err() error { return errors.Join(r.TraceErr, r.MetricErr) }

// Shutdown drains and flushes both providers within the configured
// ShutdownTimeout and reports the outcome. It is the shutdown order
// structured-logging-and-opentelemetry.md names for the telemetry-producer
// stage of process shutdown: "stop telemetry producers; flush within
// configured deadline; record dropped/unfinished counts ...; close
// exporters". Shutdown never panics and never returns a value a caller
// could mistake for a business-logic error — its own failure mode is
// reported entirely through ShutdownReport.
func (p *Provider) Shutdown(ctx context.Context) ShutdownReport {
	deadline, cancel := context.WithTimeout(ctx, p.shutdownTimeout)
	defer cancel()

	report := ShutdownReport{
		TraceErr:  p.tp.Shutdown(deadline),
		MetricErr: p.mp.Shutdown(deadline),
	}
	report.DeadlineExceeded = errors.Is(report.TraceErr, context.DeadlineExceeded) ||
		errors.Is(report.MetricErr, context.DeadlineExceeded)
	return report
}

// ForceFlush flushes both providers within the configured ShutdownTimeout
// without shutting them down, for a caller that wants buffered signals
// exported (e.g. before a health check) without ending the Provider's
// lifetime.
func (p *Provider) ForceFlush(ctx context.Context) ShutdownReport {
	deadline, cancel := context.WithTimeout(ctx, p.shutdownTimeout)
	defer cancel()

	report := ShutdownReport{
		TraceErr:  p.tp.ForceFlush(deadline),
		MetricErr: p.mp.ForceFlush(deadline),
	}
	report.DeadlineExceeded = errors.Is(report.TraceErr, context.DeadlineExceeded) ||
		errors.Is(report.MetricErr, context.DeadlineExceeded)
	return report
}
